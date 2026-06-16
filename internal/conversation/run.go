package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/commands"
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
	"ccgo/internal/memory"
	msgs "ccgo/internal/messages"
	modelpkg "ccgo/internal/model"
	"ccgo/internal/outputstyles"
	"ccgo/internal/permissions"
	pluginpkg "ccgo/internal/plugins"
	"ccgo/internal/session"
	"ccgo/internal/skills"
	"ccgo/internal/tool"
)

func (r *Runner) RunTurn(ctx context.Context, history []contracts.Message, user contracts.Message) (Result, error) {
	if r == nil {
		return Result{}, fmt.Errorf("conversation runner is nil")
	}
	if r.Client == nil {
		return Result{}, fmt.Errorf("conversation runner missing client")
	}
	persistentModel := r.Model
	if user.Type == "" {
		user.Type = contracts.MessageUser
	}
	if user.UUID == "" {
		user.UUID = contracts.NewID()
	}
	if r.SessionID != "" {
		user.SessionID = r.SessionID
	}
	initialMessages, shouldQuery, localResult, err := r.initialUserMessages(user)
	if err != nil {
		return Result{}, err
	}
	originalHistory := append([]contracts.Message(nil), history...)
	for i := range initialMessages {
		history, initialMessages[i] = appendMessage(history, initialMessages[i])
		if err := r.appendTranscript(initialMessages[i]); err != nil {
			return Result{}, err
		}
		r.emit(Event{Type: EventUserMessage, Message: &initialMessages[i]})
	}
	result := Result{Messages: append([]contracts.Message(nil), initialMessages...)}
	if !shouldQuery {
		if localResult != nil && localResult.Type == commands.LocalCommandResultClear {
			result.Cleared = true
			return result, nil
		}
		if localResult != nil && localResult.Type == commands.LocalCommandResultCompact {
			startedAt := time.Now()
			compactResult, err := r.manualCompact(ctx, originalHistory, localResult.Value)
			result.APIDuration += time.Since(startedAt)
			if err != nil {
				return result, err
			}
			result.Compacted = true
			result.Compact = &compactResult
			result.Messages = append(result.Messages, compactResult.Plan.Boundary, compactResult.Plan.Summary)
			return result, nil
		}
		if localResult != nil && localResult.Type == commands.LocalCommandResultCost {
			return r.appendLocalTextResult(result, history, formatCostSummary(localResult.Value, originalHistory))
		}
		if localResult != nil && localResult.Type == commands.LocalCommandResultStatus {
			return r.appendLocalTextResult(result, history, r.formatStatusSummary(localResult.Value))
		}
		if localResult != nil && localResult.Type == commands.LocalCommandResultConfig {
			return r.appendLocalTextResult(result, history, r.formatConfigSummary(localResult.Value))
		}
		if localResult != nil && localResult.Type == commands.LocalCommandResultPlugin {
			return r.appendLocalTextResult(result, history, r.formatPluginSummary(localResult.Value))
		}
		if localResult != nil && localResult.Type == commands.LocalCommandResultModel {
			return r.appendLocalTextResult(result, history, r.formatModelSummary(localResult.Value))
		}
		if localResult != nil && localResult.Type == commands.LocalCommandResultMCP {
			return r.appendLocalTextResult(result, history, r.formatMCPCommandSummary(localResult.Value))
		}
		if localResult != nil && localResult.Type == commands.LocalCommandResultMemory {
			return r.appendLocalTextResult(result, history, r.formatMemorySummary(localResult.Value))
		}
		if localResult != nil && localResult.Type == commands.LocalCommandResultResume {
			text, err := r.formatResumeSummary(localResult.Value)
			if err != nil {
				return result, err
			}
			return r.appendLocalTextResult(result, history, text)
		}
		return result, nil
	}
	turnModel := r.Model
	r.Model = persistentModel
	runner := *r
	runner.Model = turnModel
	runner, closeMCP, err := runner.withConfiguredMCPTools(ctx)
	if err != nil {
		return result, err
	}
	if closeMCP != nil {
		defer func() { _ = closeMCP() }()
	}
	runner.maybeEmitTokenWarning(history)
	relevantMemoryPrefetch := runner.startRelevantMemoryPrefetch(ctx, history)
	if relevantMemoryPrefetch != nil {
		defer relevantMemoryPrefetch.cancel()
	}

	if compactedHistory, compactResult, ok, err := runner.maybeAutoCompact(ctx, history); err != nil {
		return result, err
	} else if ok {
		history = compactedHistory
		result.Compacted = true
		result.Compact = &compactResult
		result.Messages = append(result.Messages, compactResult.Plan.Boundary, compactResult.Plan.Summary)
		if err := runner.appendCompactTranscript(compactResult.Plan); err != nil {
			return result, err
		}
		runner.emit(Event{Type: EventCompact, Compact: &compactResult})
	}
	toolMetadata := runner.toolMetadata()
	for round := 0; ; round++ {
		if round >= runner.maxToolRounds() {
			return result, fmt.Errorf("maximum tool rounds exceeded: %d", runner.maxToolRounds())
		}
		var roundRelevantMemoryPrefetch *relevantMemoryPrefetchTask
		if round == 0 {
			roundRelevantMemoryPrefetch = relevantMemoryPrefetch
			relevantMemoryPrefetch = nil
		}

		request, attempts, response, apiDuration, err := runner.send(ctx, history, roundRelevantMemoryPrefetch)
		result.FinalRequest = request
		result.ModelsAttempt = append(result.ModelsAttempt, attempts...)
		result.APIDuration += apiDuration
		if err != nil {
			return result, err
		}

		assistant := messageFromResponse(runner.SessionID, response)
		history, assistant = appendMessage(history, assistant)
		result.Messages = append(result.Messages, assistant)
		result.Assistant = assistant
		result.StopReason = response.StopReason
		result.Usage = response.Usage
		if err := runner.appendTranscript(assistant); err != nil {
			return result, err
		}
		runner.emit(Event{Type: EventAssistantMessage, Message: &assistant, Model: response.Model})

		uses := ToolUses(assistant)
		if len(uses) == 0 {
			if err := runner.maybeExtractSessionMemory(ctx, result.Messages); err != nil {
				return result, err
			}
			return result, nil
		}
		toolMessages, toolResults := runner.executeToolUses(ctx, uses, toolMetadata, result.Messages)
		for i := range toolMessages {
			history, toolMessages[i] = appendMessage(history, toolMessages[i])
			result.Messages = append(result.Messages, toolMessages[i])
			if err := runner.appendTranscript(toolMessages[i]); err != nil {
				return result, err
			}
		}
		if commandPermissions := commands.CommandPermissionsFromMessages(toolMessages); commandPermissions.Model != "" {
			runner.Model = commandPermissions.Model
		}
		result.ToolResults = append(result.ToolResults, toolResults...)
	}
}

func (r *Runner) initialUserMessages(user contracts.Message) ([]contracts.Message, bool, *commands.LocalCommandResult, error) {
	text := msgs.TextContent(user)
	if text == "" {
		return []contracts.Message{user}, true, nil, nil
	}
	if !commands.IsSlashInput(text) {
		return []contracts.Message{user}, true, nil, nil
	}
	registry := commands.Load(commands.Options{CWD: r.WorkingDirectory, Settings: r.mergedSettings()})
	slash, handled, err := commands.ExecuteSlashCommand(registry, text, commands.SlashOptions{
		SessionID: r.SessionID,
		UUID:      user.UUID,
	})
	if err != nil {
		return nil, false, nil, err
	}
	if !handled {
		return []contracts.Message{user}, true, nil, nil
	}
	if slash.Model != "" {
		r.Model = slash.Model
	}
	return slash.Messages, slash.ShouldQuery, slash.LocalResult, nil
}

type relevantMemoryPrefetchTask struct {
	cancel func()
	done   <-chan relevantMemoryPrefetchOutcome
}

type relevantMemoryPrefetchOutcome struct {
	result memory.RelevantMemoryPrefetchResult
	err    error
}

func (r Runner) startRelevantMemoryPrefetch(ctx context.Context, history []contracts.Message) *relevantMemoryPrefetchTask {
	if r.RelevantMemoryDir == "" {
		return nil
	}
	prefetchCtx, cancel := context.WithCancel(ctx)
	done := make(chan relevantMemoryPrefetchOutcome, 1)
	historySnapshot := append([]contracts.Message(nil), history...)
	var agent *memory.Agent
	if r.MemoryAgentClient != nil {
		agent = &memory.Agent{
			Client:    r.MemoryAgentClient,
			Model:     r.model(),
			MaxTokens: r.CompactMaxTokens,
		}
	}
	go func() {
		result, err := memory.PrefetchRelevantMemories(prefetchCtx, historySnapshot, memory.RelevantMemoryPrefetchOptions{
			Root:  r.RelevantMemoryDir,
			Limit: r.relevantMemoryLimit(),
			Agent: agent,
		})
		done <- relevantMemoryPrefetchOutcome{result: result, err: err}
	}()
	return &relevantMemoryPrefetchTask{cancel: cancel, done: done}
}

func (t *relevantMemoryPrefetchTask) requestContext(ctx context.Context) (relevantMemoryRequestContext, error) {
	if t == nil {
		return relevantMemoryRequestContext{}, nil
	}
	select {
	case outcome := <-t.done:
		if outcome.err != nil {
			if errors.Is(outcome.err, context.Canceled) || errors.Is(outcome.err, context.DeadlineExceeded) {
				return relevantMemoryRequestContext{}, outcome.err
			}
			return relevantMemoryRequestContext{SkipSync: true}, nil
		}
		return relevantMemoryRequestContext{Prefetch: &outcome.result, SkipSync: true}, nil
	case <-ctx.Done():
		t.cancel()
		return relevantMemoryRequestContext{}, ctx.Err()
	}
}

func (r Runner) toolMetadata() map[string]any {
	metadata := map[string]any{
		tool.MetadataSettingsKey: r.mergedSettings(),
	}
	skillDirs := append([]string(nil), r.SkillDirs...)
	skillDirs = appendUniqueStrings(skillDirs, skills.UserSkillDirs()...)
	skillDirs = appendUniqueStrings(skillDirs, skills.UserLegacyCommandSkillDirs()...)
	if r.WorkingDirectory != "" {
		skillDirs = appendUniqueStrings(skillDirs, skills.ProjectSkillDirs(r.WorkingDirectory)...)
		skillDirs = appendUniqueStrings(skillDirs, skills.ProjectLegacyCommandSkillDirs(r.WorkingDirectory)...)
	}
	if r.RelevantMemoryDir != "" || len(skillDirs) > 0 {
		metadata[tool.MetadataInternalPathContextKey] = permissions.InternalPathContext{
			AutoMemoryDir: r.RelevantMemoryDir,
			SkillDirs:     skillDirs,
		}
	}
	return metadata
}

func appendUniqueStrings(base []string, items ...string) []string {
	seen := map[string]struct{}{}
	for _, item := range base {
		seen[item] = struct{}{}
	}
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		base = append(base, item)
	}
	return base
}

func (r Runner) maybeEmitTokenWarning(history []contracts.Message) {
	if r.AutoCompact == nil {
		return
	}
	config := *r.AutoCompact
	tokenUsage := config.TokenUsage
	if tokenUsage <= 0 {
		tokenUsage = compactpkg.EstimateTokens(history)
	}
	window := compactpkg.WindowConfigFromEnv(config.Window)
	window.AutoCompactEnabled = config.Enabled
	state := compactpkg.CalculateWarningState(tokenUsage, window)
	if !state.IsAboveWarningThreshold && !state.IsAboveErrorThreshold && !state.IsAboveAutoCompactThreshold && !state.IsAtBlockingLimit {
		return
	}
	warning := TokenWarning{
		TokenUsage: tokenUsage,
		Window:     window,
		State:      state,
	}
	r.emit(Event{Type: EventTokenWarning, TokenWarning: &warning})
}

func (r Runner) maybeAutoCompact(ctx context.Context, history []contracts.Message) ([]contracts.Message, compactpkg.Result, bool, error) {
	if r.AutoCompact == nil {
		return history, compactpkg.Result{}, false, nil
	}
	config := *r.AutoCompact
	if config.TokenUsage <= 0 {
		config.TokenUsage = compactpkg.EstimateTokens(history)
	}
	if !compactpkg.ShouldRun(history, config) {
		return history, compactpkg.Result{}, false, nil
	}
	client := r.CompactClient
	if client == nil {
		client = r.Client
	}
	result, err := compactpkg.Runner{
		Client:            client,
		Model:             r.model(),
		MaxTokens:         r.CompactMaxTokens,
		KeepLast:          config.KeepLast,
		ExtraInstructions: config.ExtraInstructions,
	}.Compact(ctx, history, compactpkg.TriggerAuto, config.TokenUsage, "")
	if err != nil {
		compactpkg.RecordFailure(r.AutoCompact)
		r.emit(Event{Type: EventCompact, Compact: &result, Error: err})
		return history, result, false, nil
	}
	compactpkg.RecordSuccess(r.AutoCompact)
	return result.Plan.Output, result, true, nil
}

func (r Runner) manualCompact(ctx context.Context, history []contracts.Message, userContext string) (compactpkg.Result, error) {
	client := r.CompactClient
	if client == nil {
		client = r.Client
	}
	keepLast := 0
	extraInstructions := ""
	if r.AutoCompact != nil {
		keepLast = r.AutoCompact.KeepLast
		extraInstructions = r.AutoCompact.ExtraInstructions
	}
	tokenUsage := compactpkg.EstimateTokens(history)
	result, err := compactpkg.Runner{
		Client:            client,
		Model:             r.model(),
		MaxTokens:         r.CompactMaxTokens,
		KeepLast:          keepLast,
		ExtraInstructions: extraInstructions,
	}.Compact(ctx, history, compactpkg.TriggerManual, tokenUsage, userContext)
	if err != nil {
		r.emit(Event{Type: EventCompact, Compact: &result, Error: err})
		return result, err
	}
	if err := r.appendCompactTranscript(result.Plan); err != nil {
		return result, err
	}
	r.emit(Event{Type: EventCompact, Compact: &result})
	return result, nil
}

func (r Runner) appendLocalTextResult(result Result, history []contracts.Message, text string) (Result, error) {
	message := msgs.UserText(text)
	if r.SessionID != "" {
		message.SessionID = r.SessionID
	}
	history, message = appendMessage(history, message)
	result.Messages = append(result.Messages, message)
	if err := r.appendTranscript(message); err != nil {
		return result, err
	}
	r.emit(Event{Type: EventUserMessage, Message: &message})
	return result, nil
}

func formatCostSummary(raw string, history []contracts.Message) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) > 0 {
		switch args[0] {
		case "summary", "total", "totals":
		case "show", "breakdown", "details", "detail":
			return formatCostBreakdown(history)
		default:
			return "Cost subcommand is not implemented in the Go runtime yet: " + strings.Join(args, " ")
		}
	}
	return formatCostTotals(history)
}

func formatCostTotals(history []contracts.Message) string {
	usage, found := historyUsage(history)
	if !found {
		return "No cost data available for this session."
	}
	return fmt.Sprintf(
		"Total cost: $%.6f\nInput tokens: %d\nOutput tokens: %d\nCache creation input tokens: %d\nCache read input tokens: %d\nWeb search requests: %d\nWeb fetch requests: %d",
		usage.CostUSD,
		usage.InputTokens,
		usage.OutputTokens,
		usage.CacheCreationInputTokens,
		usage.CacheReadInputTokens,
		usage.ServerToolUse.WebSearchRequests,
		usage.ServerToolUse.WebFetchRequests,
	)
}

func formatCostBreakdown(history []contracts.Message) string {
	total, found := historyUsage(history)
	if !found {
		return "No cost data available for this session."
	}
	lines := []string{
		"Cost breakdown",
		fmt.Sprintf("Total cost: $%.6f", total.CostUSD),
	}
	var withUsage int
	for index, message := range history {
		if message.Usage == nil || !usageHasValues(*message.Usage) {
			continue
		}
		withUsage++
		usage := *message.Usage
		lines = append(lines, fmt.Sprintf(
			"- %s: cost $%.6f, input %d, output %d, cache create %d, cache read %d, web search %d, web fetch %d",
			costMessageLabel(message, index),
			usage.CostUSD,
			usage.InputTokens,
			usage.OutputTokens,
			usage.CacheCreationInputTokens,
			usage.CacheReadInputTokens,
			usage.ServerToolUse.WebSearchRequests,
			usage.ServerToolUse.WebFetchRequests,
		))
		if withUsage == 20 {
			break
		}
	}
	lines = append(lines, fmt.Sprintf("Messages with usage: %d", countUsageMessages(history)))
	if countUsageMessages(history) > 20 {
		lines = append(lines, fmt.Sprintf("Showing 20 of %d messages with usage.", countUsageMessages(history)))
	}
	return strings.Join(lines, "\n")
}

func costMessageLabel(message contracts.Message, index int) string {
	messageType := strings.TrimSpace(string(message.Type))
	if messageType == "" {
		messageType = "message"
	}
	id := strings.TrimSpace(string(message.UUID))
	if id == "" {
		id = strings.TrimSpace(message.ID)
	}
	if id != "" {
		messageType += " " + id
	} else {
		messageType += fmt.Sprintf(" #%d", index+1)
	}
	if model := strings.TrimSpace(message.Model); model != "" {
		messageType += " (" + model + ")"
	}
	return messageType
}

func countUsageMessages(history []contracts.Message) int {
	count := 0
	for _, message := range history {
		if message.Usage != nil && usageHasValues(*message.Usage) {
			count++
		}
	}
	return count
}

func historyUsage(history []contracts.Message) (contracts.Usage, bool) {
	var total contracts.Usage
	var found bool
	for _, message := range history {
		if message.Usage == nil || !usageHasValues(*message.Usage) {
			continue
		}
		total = anthropic.AccumulateUsage(total, *message.Usage)
		found = true
	}
	return total, found
}

func usageHasValues(usage contracts.Usage) bool {
	return usage.InputTokens != 0 ||
		usage.OutputTokens != 0 ||
		usage.CacheCreationInputTokens != 0 ||
		usage.CacheReadInputTokens != 0 ||
		usage.CacheDeletedInputTokens != 0 ||
		usage.ServerToolUse.WebSearchRequests != 0 ||
		usage.ServerToolUse.WebFetchRequests != 0 ||
		usage.CacheCreation.Ephemeral1hInputTokens != 0 ||
		usage.CacheCreation.Ephemeral5mInputTokens != 0 ||
		usage.Iterations != 0 ||
		usage.CostUSD != 0
}

func (r Runner) formatStatusSummary(raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) > 0 {
		switch args[0] {
		case "show", "info":
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				return "Usage: /status " + args[0] + " <session|model|auth|tools|mcp|plugins>"
			}
			return r.formatStatusShow(args[1])
		case "session", "model", "auth", "tools", "mcp", "plugins":
			return r.formatStatusShow(args[0])
		default:
			return "Status section is not implemented in the Go runtime yet: " + strings.Join(args, " ")
		}
	}
	model := r.model()
	cwd := strings.TrimSpace(r.WorkingDirectory)
	if cwd == "" {
		cwd = "(unknown)"
	}
	sessionID := string(r.SessionID)
	if sessionID == "" {
		sessionID = "(none)"
	}
	toolCount := 0
	if r.Tools.Registry != nil {
		toolCount = len(r.Tools.Registry.Names())
	}
	mcpServers := r.mcpServerNames()
	mcpText := "none"
	if len(mcpServers) > 0 {
		mcpText = strings.Join(mcpServers, ", ")
	}
	return fmt.Sprintf(
		"Status\nSession ID: %s\nWorking directory: %s\nModel: %s\nOutput style: %s\nAuth source: %s\nPermission mode: %s\nFast mode: %s\nBetas: %s\nTools: %d\nMCP servers: %s",
		sessionID,
		cwd,
		model,
		r.effectiveOutputStyleName(),
		r.authSourceText(),
		r.permissionModeText(),
		boolEnabledText(r.FastMode),
		r.betaHeadersText(),
		toolCount,
		mcpText,
	)
}

func (r Runner) formatStatusShow(raw string) string {
	switch normalizeStatusSection(raw) {
	case "session":
		cwd := strings.TrimSpace(r.WorkingDirectory)
		if cwd == "" {
			cwd = "(unknown)"
		}
		sessionID := string(r.SessionID)
		if sessionID == "" {
			sessionID = "(none)"
		}
		sessionPath := strings.TrimSpace(r.SessionPath)
		if sessionPath == "" {
			sessionPath = "(not configured)"
		}
		return strings.Join([]string{
			"Status session",
			"Session ID: " + sessionID,
			"Working directory: " + cwd,
			"Transcript path: " + sessionPath,
		}, "\n")
	case "model":
		return strings.Join([]string{
			"Status model",
			"Model: " + r.model(),
			"Output style: " + r.effectiveOutputStyleName(),
			fmt.Sprintf("Max tokens: %d", r.MaxTokens),
			"Fast mode: " + boolEnabledText(r.FastMode),
			"Betas: " + r.betaHeadersText(),
		}, "\n")
	case "auth":
		return strings.Join([]string{
			"Status auth",
			"Auth source: " + r.authSourceText(),
			"Permission mode: " + r.permissionModeText(),
			"Fast mode: " + boolEnabledText(r.FastMode),
			"Betas: " + r.betaHeadersText(),
		}, "\n")
	case "tools":
		names := r.toolNames()
		lines := []string{
			"Status tools",
			fmt.Sprintf("Tools: %d", len(names)),
		}
		if len(names) > 0 {
			lines = append(lines, "Tool names: "+strings.Join(firstStrings(names, 40), ", "))
			if len(names) > 40 {
				lines = append(lines, fmt.Sprintf("Showing 40 of %d tools.", len(names)))
			}
		}
		return strings.Join(lines, "\n")
	case "mcp":
		servers := r.mcpServers()
		if len(servers) == 0 {
			return "No MCP servers configured."
		}
		lines := []string{
			"Status MCP servers",
			fmt.Sprintf("MCP servers: %d", len(servers)),
		}
		for _, server := range firstMCPSummaries(servers, 40) {
			status := "configured"
			if !server.Policy.Allowed {
				status = "blocked: " + server.Policy.Reason
			}
			lines = append(lines, fmt.Sprintf("- %s: %s (%s, %s)", server.Name, status, mcpServerTransport(server.Config), mcpServerSource(server.Config)))
		}
		if len(servers) > 40 {
			lines = append(lines, fmt.Sprintf("Showing 40 of %d MCP servers.", len(servers)))
		}
		return strings.Join(lines, "\n")
	case "plugins":
		merged := r.mergedSettings()
		localPlugins := pluginpkg.LoadPluginDirsWithSettings(pluginpkg.ProjectPluginDirs(r.WorkingDirectory), merged)
		lines := []string{
			"Status plugins",
			fmt.Sprintf("Enabled plugin entries: %d", len(merged.EnabledPlugins)),
			fmt.Sprintf("Enabled plugins: %d", countEnabledPlugins(merged.EnabledPlugins)),
			fmt.Sprintf("Plugin configs: %d", len(merged.PluginConfigs)),
			fmt.Sprintf("Local plugin manifests: %d", len(localPlugins)),
		}
		if len(merged.EnabledPlugins) > 0 {
			lines = append(lines, "Plugin enabled states:")
			for _, line := range firstStrings(pluginEnabledStateLines(merged.EnabledPlugins), 20) {
				lines = append(lines, "- "+line)
			}
			if len(merged.EnabledPlugins) > 20 {
				lines = append(lines, fmt.Sprintf("Showing 20 of %d plugin enabled states.", len(merged.EnabledPlugins)))
			}
		}
		if len(localPlugins) > 0 {
			lines = append(lines, "Local plugins:")
			for _, plugin := range firstLoadedPlugins(localPlugins, 20) {
				lines = append(lines, "- "+plugin.Name)
			}
			if len(localPlugins) > 20 {
				lines = append(lines, fmt.Sprintf("Showing 20 of %d local plugins.", len(localPlugins)))
			}
		}
		return strings.Join(lines, "\n")
	default:
		return "Unknown status section " + strings.TrimSpace(raw) + ". Available sections: session, model, auth, tools, mcp, plugins"
	}
}

func normalizeStatusSection(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "--")
	compact := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, "_", "-"), " ", "-"))
	switch compact {
	case "session", "conversation", "transcript":
		return "session"
	case "model", "models", "output-style", "outputstyle":
		return "model"
	case "auth", "authentication", "login", "permission", "permissions", "permission-mode":
		return "auth"
	case "tool", "tools":
		return "tools"
	case "mcp", "mcp-server", "mcp-servers", "mcpservers":
		return "mcp"
	case "plugin", "plugins":
		return "plugins"
	default:
		return compact
	}
}

func (r *Runner) formatConfigSummary(raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) > 0 {
		switch args[0] {
		case "show", "list":
			if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
				return r.formatConfigShow(args[1])
			}
		case "output-style", "outputStyle":
			return r.setOutputStyleSummary(args)
		case "fast-mode", "fastMode":
			return r.setFastModeSummary(args)
		case "model":
			return r.setConfigModelSummary(args)
		case "permission-mode", "permissionMode":
			return r.setPermissionModeSummary(args)
		default:
			return "Config subcommand is not implemented in the Go runtime yet: " + strings.Join(args, " ")
		}
	}
	cwd := strings.TrimSpace(r.WorkingDirectory)
	if cwd == "" {
		cwd = "."
	}
	merged := r.mergedSettings()
	permissionsText := settingsPermissionsSummary(merged.Permissions)
	lines := []string{
		"Config",
		"Working directory: " + cwd,
		"Model: " + r.model(),
		"Settings files:",
		fmt.Sprintf("- user: %s (%s)", config.UserSettingsPath(), fileStatusText(config.UserSettingsPath())),
		fmt.Sprintf("- project: %s (%s)", config.ProjectSettingsPath(cwd), fileStatusText(config.ProjectSettingsPath(cwd))),
		fmt.Sprintf("- local: %s (%s)", config.LocalSettingsPath(cwd), fileStatusText(config.LocalSettingsPath(cwd))),
		"Merged settings:",
		fmt.Sprintf("- env vars: %d", len(merged.Env)),
		fmt.Sprintf("- MCP servers: %d", len(merged.MCPServers)),
		"- output style: " + r.effectiveOutputStyleName(),
		"- auth source: " + r.authSourceText(),
		"- permission mode: " + r.permissionModeText(),
		"- fast mode: " + boolEnabledText(r.FastMode),
		fmt.Sprintf("- beta headers: %d", len(r.BetaHeaders)),
		"- permission rules: " + permissionsText,
		fmt.Sprintf("- hooks: %d", len(merged.Hooks)),
		fmt.Sprintf("- enabled plugins: %d", len(merged.EnabledPlugins)),
	}
	return strings.Join(lines, "\n")
}

func (r *Runner) formatConfigShow(raw string) string {
	target := normalizeConfigSection(raw)
	merged := r.mergedSettings()
	switch target {
	case "settings":
		cwd := strings.TrimSpace(r.WorkingDirectory)
		if cwd == "" {
			cwd = "."
		}
		return strings.Join([]string{
			"Config settings files",
			fmt.Sprintf("User: %s (%s)", config.UserSettingsPath(), fileStatusText(config.UserSettingsPath())),
			fmt.Sprintf("Project: %s (%s)", config.ProjectSettingsPath(cwd), fileStatusText(config.ProjectSettingsPath(cwd))),
			fmt.Sprintf("Local: %s (%s)", config.LocalSettingsPath(cwd), fileStatusText(config.LocalSettingsPath(cwd))),
		}, "\n")
	case "model":
		lines := []string{
			"Config model",
			"Current model: " + r.model(),
			"Configured model: " + unsetText(merged.Model),
			fmt.Sprintf("Available models: %d", len(merged.AvailableModels)),
			fmt.Sprintf("Model overrides: %d", len(merged.ModelOverrides)),
		}
		if len(merged.AvailableModels) > 0 {
			lines = append(lines, "Available model names: "+strings.Join(firstStrings(merged.AvailableModels, 20), ", "))
		}
		return strings.Join(lines, "\n")
	case "output-style":
		available := r.AvailableOutputStyleNames()
		lines := []string{
			"Config output style",
			"Effective output style: " + r.effectiveOutputStyleName(),
			"Configured output style: " + unsetText(merged.OutputStyle),
			fmt.Sprintf("Available output styles: %d", len(available)),
		}
		if len(available) > 0 {
			lines = append(lines, "Available output style names: "+strings.Join(firstStrings(available, 20), ", "))
		}
		return strings.Join(lines, "\n")
	case "auth":
		return strings.Join([]string{
			"Config auth",
			"Auth source: " + r.authSourceText(),
			"Force login method: " + unsetText(merged.ForceLoginMethod),
			"Force login org UUID: " + unsetText(merged.ForceLoginOrgUUID),
		}, "\n")
	case "fast-mode":
		return strings.Join([]string{
			"Config fast mode",
			"Runtime fast mode: " + boolEnabledText(r.FastMode),
			"Configured fast mode: " + boolPtrEnabledText(merged.FastMode),
			"Per-session opt-in: " + boolPtrEnabledText(merged.FastModePerSessionOptIn),
		}, "\n")
	case "betas":
		lines := []string{
			"Config betas",
			fmt.Sprintf("Beta headers: %d", len(r.BetaHeaders)),
			"Betas: " + r.betaHeadersText(),
		}
		return strings.Join(lines, "\n")
	case "env":
		lines := []string{
			"Config env",
			fmt.Sprintf("Env vars: %d", len(merged.Env)),
		}
		if len(merged.Env) > 0 {
			lines = append(lines, "Env names: "+strings.Join(sortedStringMapKeys(merged.Env), ", "))
		}
		return strings.Join(lines, "\n")
	case "permissions":
		lines := []string{
			"Config permissions",
			"Summary: " + settingsPermissionsSummary(merged.Permissions),
		}
		if merged.Permissions != nil {
			lines = append(lines,
				"Default mode: "+unsetText(string(merged.Permissions.DefaultMode)),
				fmt.Sprintf("Allow rules: %d", len(merged.Permissions.Allow)),
				fmt.Sprintf("Deny rules: %d", len(merged.Permissions.Deny)),
				fmt.Sprintf("Ask rules: %d", len(merged.Permissions.Ask)),
				fmt.Sprintf("Additional directories: %d", len(merged.Permissions.AdditionalDirectories)),
				"Disable bypass mode: "+configuredValueText(merged.Permissions.DisableBypassMode),
				"Disable auto mode: "+configuredValueText(merged.Permissions.DisableAutoMode),
			)
			appendConfigRuleSection := func(title string, values []string) {
				if len(values) == 0 {
					return
				}
				lines = append(lines, title+":")
				for _, value := range firstStrings(values, 20) {
					lines = append(lines, "- "+value)
				}
				if len(values) > 20 {
					lines = append(lines, fmt.Sprintf("Showing 20 of %d %s.", len(values), strings.ToLower(title)))
				}
			}
			appendConfigRuleSection("Allow", merged.Permissions.Allow)
			appendConfigRuleSection("Deny", merged.Permissions.Deny)
			appendConfigRuleSection("Ask", merged.Permissions.Ask)
		}
		return strings.Join(lines, "\n")
	case "mcp":
		servers := r.mcpServers()
		if len(servers) == 0 {
			return "No MCP servers configured."
		}
		lines := []string{
			"Config MCP servers",
			fmt.Sprintf("MCP servers: %d", len(servers)),
			fmt.Sprintf("Allowed MCP policy entries: %d", len(merged.AllowedMCPServers)),
			fmt.Sprintf("Denied MCP policy entries: %d", len(merged.DeniedMCPServers)),
			fmt.Sprintf("Enabled .mcp.json servers: %d", len(merged.EnabledMCPJSONServers)),
			fmt.Sprintf("Disabled .mcp.json servers: %d", len(merged.DisabledMCPJSONServers)),
		}
		for _, server := range firstMCPSummaries(servers, 20) {
			status := "configured"
			if !server.Policy.Allowed {
				status = "blocked: " + server.Policy.Reason
			}
			lines = append(lines, fmt.Sprintf("- %s (%s, %s, %s)", server.Name, mcpServerTransport(server.Config), status, mcpServerSource(server.Config)))
		}
		if len(servers) > 20 {
			lines = append(lines, fmt.Sprintf("Showing 20 of %d MCP servers.", len(servers)))
		}
		return strings.Join(lines, "\n")
	case "hooks":
		lines := []string{
			"Config hooks",
			fmt.Sprintf("Hooks: %d", len(merged.Hooks)),
			"Disable all hooks: " + boolPtrEnabledText(merged.DisableAllHooks),
			"Allow managed hooks only: " + boolPtrEnabledText(merged.AllowManagedHooksOnly),
			fmt.Sprintf("Allowed HTTP hook URLs: %d", len(merged.AllowedHTTPHookURLs)),
			fmt.Sprintf("HTTP hook env var names: %d", len(merged.HTTPHookAllowedEnvVars)),
		}
		if len(merged.Hooks) > 0 {
			lines = append(lines, "Hook events: "+strings.Join(sortedAnyMapKeys(merged.Hooks), ", "))
		}
		if len(merged.HTTPHookAllowedEnvVars) > 0 {
			lines = append(lines, "HTTP hook env names: "+strings.Join(merged.HTTPHookAllowedEnvVars, ", "))
		}
		return strings.Join(lines, "\n")
	case "plugins":
		names := pluginConfigNames(merged)
		lines := []string{
			"Config plugins",
			fmt.Sprintf("Enabled plugin entries: %d", len(merged.EnabledPlugins)),
			fmt.Sprintf("Enabled plugins: %d", countEnabledPlugins(merged.EnabledPlugins)),
			fmt.Sprintf("Plugin configs: %d", len(merged.PluginConfigs)),
			fmt.Sprintf("Legacy plugin settings: %d", len(merged.Plugins)),
			fmt.Sprintf("Configured plugin names: %d", len(names)),
		}
		if len(merged.EnabledPlugins) > 0 {
			lines = append(lines, "Plugin enabled states:")
			for _, line := range firstStrings(pluginEnabledStateLines(merged.EnabledPlugins), 20) {
				lines = append(lines, "- "+line)
			}
			if len(merged.EnabledPlugins) > 20 {
				lines = append(lines, fmt.Sprintf("Showing 20 of %d plugin enabled states.", len(merged.EnabledPlugins)))
			}
		}
		if len(names) > 0 {
			lines = append(lines, "Plugin config names: "+strings.Join(firstStrings(names, 20), ", "))
		}
		return strings.Join(lines, "\n")
	case "marketplaces":
		return r.formatPluginMarketplaces()
	case "sandbox":
		lines := []string{
			"Config sandbox",
			fmt.Sprintf("Sandbox keys: %d", len(merged.Sandbox)),
		}
		if len(merged.Sandbox) > 0 {
			lines = append(lines, "Keys: "+strings.Join(sortedAnyMapKeys(merged.Sandbox), ", "))
		}
		return strings.Join(lines, "\n")
	default:
		return "Unknown config section " + strings.TrimSpace(raw) + ". Available sections: settings, model, output-style, auth, fast-mode, betas, env, permissions, mcp, hooks, plugins, marketplaces, sandbox"
	}
}

func normalizeConfigSection(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "--")
	compact := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, "_", "-"), " ", "-"))
	switch compact {
	case "file", "files", "setting", "settings", "settings-file", "settings-files":
		return "settings"
	case "outputstyle", "output-style", "style", "styles":
		return "output-style"
	case "permission", "permissions", "permission-mode", "permissionmode":
		return "permissions"
	case "mcp", "mcp-server", "mcp-servers", "mcpservers":
		return "mcp"
	case "hook", "hooks":
		return "hooks"
	case "plugin", "plugins", "enabled-plugin", "enabled-plugins", "plugin-config", "plugin-configs":
		return "plugins"
	case "marketplace", "marketplaces":
		return "marketplaces"
	case "env", "environment", "environment-variables":
		return "env"
	case "beta", "betas", "beta-header", "beta-headers":
		return "betas"
	case "fast", "fastmode", "fast-mode":
		return "fast-mode"
	case "model", "models":
		return "model"
	case "auth", "authentication", "login":
		return "auth"
	case "sandbox":
		return "sandbox"
	default:
		return compact
	}
}

func unsetText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(unset)"
	}
	return value
}

func boolPtrEnabledText(value *bool) string {
	if value == nil {
		return "(unset)"
	}
	return boolEnabledText(*value)
}

func configuredValueText(value any) string {
	if value == nil {
		return "(unset)"
	}
	switch typed := value.(type) {
	case bool:
		return boolEnabledText(typed)
	case string:
		if strings.TrimSpace(typed) == "" {
			return "(unset)"
		}
		return "configured"
	default:
		return "configured"
	}
}

func (r *Runner) setOutputStyleSummary(args []string) string {
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		return "Usage: /config output-style <style-name>"
	}
	rawName := strings.TrimSpace(args[1])
	name, ok := resolveOutputStyleName(rawName, r.AvailableOutputStyleNames())
	if !ok {
		return fmt.Sprintf("Unknown output style %q. Available output styles: %s", rawName, strings.Join(r.AvailableOutputStyleNames(), ", "))
	}
	if err := setUserOutputStyle(name); err != nil {
		return fmt.Sprintf("Failed to set output style %s: %v", name, err)
	}
	if r.MCP != nil {
		r.MCP.UserSettings.OutputStyle = name
	}
	return "Output style set to " + name + "."
}

func resolveOutputStyleName(raw string, available []string) (string, bool) {
	for _, name := range available {
		if raw == name {
			return name, true
		}
	}
	for _, name := range available {
		if strings.EqualFold(raw, name) {
			return name, true
		}
	}
	return "", false
}

func (r *Runner) setFastModeSummary(args []string) string {
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		return "Usage: /config fast-mode <on|off>"
	}
	enabled, ok := parseOnOff(args[1])
	if !ok {
		return "Usage: /config fast-mode <on|off>"
	}
	if err := setUserFastMode(enabled); err != nil {
		return fmt.Sprintf("Failed to set fast mode: %v", err)
	}
	if r.MCP != nil {
		r.MCP.UserSettings.FastMode = &enabled
	}
	r.FastMode = enabled
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	return "Fast mode " + state + "."
}

func (r *Runner) setConfigModelSummary(args []string) string {
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		return "Usage: /config model <model-name>"
	}
	name, display := resolveModelSelection(args[1])
	if err := setUserModel(name); err != nil {
		return fmt.Sprintf("Failed to set model %s: %v", name, err)
	}
	r.Model = name
	if r.MCP != nil {
		r.MCP.UserSettings.Model = name
	}
	if display != "" && display != name {
		return fmt.Sprintf("Model set to %s.\nDisplay name: %s", name, display)
	}
	return "Model set to " + name + "."
}

func (r *Runner) setPermissionModeSummary(args []string) string {
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		return permissionModeUsage()
	}
	mode, ok := parsePermissionMode(args[1])
	if !ok {
		return permissionModeUsage()
	}
	if err := setUserPermissionMode(mode); err != nil {
		return fmt.Sprintf("Failed to set permission mode %s: %v", mode, err)
	}
	r.PermissionMode = mode
	if r.MCP != nil {
		if r.MCP.UserSettings.Permissions == nil {
			r.MCP.UserSettings.Permissions = &contracts.PermissionsSetting{}
		}
		r.MCP.UserSettings.Permissions.DefaultMode = mode
	}
	return "Permission mode set to " + string(mode) + "."
}

func permissionModeUsage() string {
	return "Usage: /config permission-mode <default|acceptEdits|bypassPermissions|dontAsk|plan|auto|bubble>"
}

func parsePermissionMode(raw string) (contracts.PermissionMode, bool) {
	switch strings.TrimSpace(raw) {
	case string(contracts.PermissionDefault):
		return contracts.PermissionDefault, true
	case string(contracts.PermissionAcceptEdits):
		return contracts.PermissionAcceptEdits, true
	case string(contracts.PermissionBypassPermissions):
		return contracts.PermissionBypassPermissions, true
	case string(contracts.PermissionDontAsk):
		return contracts.PermissionDontAsk, true
	case string(contracts.PermissionPlan):
		return contracts.PermissionPlan, true
	case string(contracts.PermissionAuto):
		return contracts.PermissionAuto, true
	case string(contracts.PermissionBubble):
		return contracts.PermissionBubble, true
	}
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "_", "-")) {
	case "accept-edits":
		return contracts.PermissionAcceptEdits, true
	case "bypass", "bypass-permissions":
		return contracts.PermissionBypassPermissions, true
	case "dontask", "dont-ask":
		return contracts.PermissionDontAsk, true
	}
	return "", false
}

func parseOnOff(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "true", "enabled", "enable", "1":
		return true, true
	case "off", "false", "disabled", "disable", "0":
		return false, true
	default:
		return false, false
	}
}

func (r Runner) authSourceText() string {
	if strings.TrimSpace(r.APIKeySource) == "" {
		return "(unknown)"
	}
	return r.APIKeySource
}

func (r Runner) permissionModeText() string {
	if strings.TrimSpace(string(r.PermissionMode)) == "" {
		return "(unknown)"
	}
	return string(r.PermissionMode)
}

func (r Runner) betaHeadersText() string {
	if len(r.BetaHeaders) == 0 {
		return "none"
	}
	return strings.Join(r.BetaHeaders, ", ")
}

func (r Runner) formatPluginSummary(raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) > 0 {
		switch args[0] {
		case "list", "status":
		case "show", "info":
			return r.formatPluginShow(args)
		case "search", "find":
			query := subcommandRemainder(raw, args[0])
			if strings.TrimSpace(query) == "" {
				return "Usage: /plugin " + args[0] + " <query>"
			}
			return r.formatPluginSearch(query)
		case "marketplaces", "marketplace":
			return r.formatPluginMarketplaces()
		case "config", "settings":
			return r.formatPluginConfig(args)
		case "enable", "disable":
			return r.setPluginEnabledSummary(args)
		default:
			return "Plugin subcommand is not implemented in the Go runtime yet: " + strings.Join(args, " ")
		}
	}
	merged := r.mergedSettings()
	registry := commands.Load(commands.Options{CWD: r.WorkingDirectory, Settings: merged})
	localPlugins := pluginpkg.LoadPluginDirsWithSettings(pluginpkg.ProjectPluginDirs(r.WorkingDirectory), merged)
	pluginSkills := pluginSkillNames(localPlugins)
	pluginCommands := pluginCommandNames(registry.Visible(), pluginSkills)
	pluginAgents := pluginAgentNames(localPlugins)
	pluginMCPServers := pluginMCPServerNames(localPlugins)
	pluginOutputStyles := pluginOutputStyleNames(localPlugins)
	pluginHookEvents := pluginHookEventLines(localPlugins)
	totalPluginHooks := pluginHookCount(localPlugins)
	lines := []string{
		"Plugins",
		fmt.Sprintf("Enabled plugins: %d", countEnabledPlugins(merged.EnabledPlugins)),
		fmt.Sprintf("Plugin configs: %d", len(merged.PluginConfigs)),
		fmt.Sprintf("Plugin settings entries: %d", len(merged.Plugins)),
		fmt.Sprintf("Extra known marketplaces: %d", len(merged.ExtraKnownMarketplaces)),
		fmt.Sprintf("Strict known marketplaces: %d", len(merged.StrictKnownMarketplaces)),
		fmt.Sprintf("Blocked marketplaces: %d", len(merged.BlockedMarketplaces)),
		fmt.Sprintf("Local plugin manifests: %d", len(localPlugins)),
		fmt.Sprintf("Registered plugin commands: %d", len(pluginCommands)),
		fmt.Sprintf("Plugin skills: %d", len(pluginSkills)),
		fmt.Sprintf("Plugin agents: %d", len(pluginAgents)),
		fmt.Sprintf("Plugin MCP servers: %d", len(pluginMCPServers)),
		fmt.Sprintf("Plugin output styles: %d", len(pluginOutputStyles)),
		fmt.Sprintf("Plugin hooks: %d", totalPluginHooks),
	}
	if len(merged.EnabledPlugins) > 0 {
		lines = append(lines, "Plugin enabled states:")
		for _, line := range firstStrings(pluginEnabledStateLines(merged.EnabledPlugins), 10) {
			lines = append(lines, "- "+line)
		}
		if len(merged.EnabledPlugins) > 10 {
			lines = append(lines, fmt.Sprintf("Showing 10 of %d plugin enabled states.", len(merged.EnabledPlugins)))
		}
	}
	if len(localPlugins) > 0 {
		lines = append(lines, "Local plugins:")
		for _, plugin := range firstLoadedPlugins(localPlugins, 10) {
			name := plugin.Name
			if plugin.Version != "" {
				name += "@" + plugin.Version
			}
			lines = append(lines, "- "+name)
		}
		if len(localPlugins) > 10 {
			lines = append(lines, fmt.Sprintf("Showing 10 of %d local plugins.", len(localPlugins)))
		}
	}
	if len(pluginCommands) > 0 {
		lines = append(lines, "Plugin commands:")
		for _, name := range firstStrings(pluginCommands, 10) {
			lines = append(lines, "- /"+name)
		}
		if len(pluginCommands) > 10 {
			lines = append(lines, fmt.Sprintf("Showing 10 of %d plugin commands.", len(pluginCommands)))
		}
	}
	if len(pluginSkills) > 0 {
		lines = append(lines, "Plugin skills:")
		for _, name := range firstStrings(pluginSkills, 10) {
			lines = append(lines, "- /"+name)
		}
		if len(pluginSkills) > 10 {
			lines = append(lines, fmt.Sprintf("Showing 10 of %d plugin skills.", len(pluginSkills)))
		}
	}
	if len(pluginAgents) > 0 {
		lines = append(lines, "Plugin agents:")
		for _, name := range firstStrings(pluginAgents, 10) {
			lines = append(lines, "- "+name)
		}
		if len(pluginAgents) > 10 {
			lines = append(lines, fmt.Sprintf("Showing 10 of %d plugin agents.", len(pluginAgents)))
		}
	}
	if len(pluginMCPServers) > 0 {
		lines = append(lines, "Plugin MCP servers:")
		for _, name := range firstStrings(pluginMCPServers, 10) {
			lines = append(lines, "- "+name)
		}
		if len(pluginMCPServers) > 10 {
			lines = append(lines, fmt.Sprintf("Showing 10 of %d plugin MCP servers.", len(pluginMCPServers)))
		}
	}
	if len(pluginOutputStyles) > 0 {
		lines = append(lines, "Plugin output styles:")
		for _, name := range firstStrings(pluginOutputStyles, 10) {
			lines = append(lines, "- "+name)
		}
		if len(pluginOutputStyles) > 10 {
			lines = append(lines, fmt.Sprintf("Showing 10 of %d plugin output styles.", len(pluginOutputStyles)))
		}
	}
	if len(pluginHookEvents) > 0 {
		lines = append(lines, "Plugin hook events:")
		for _, event := range firstStrings(pluginHookEvents, 10) {
			lines = append(lines, "- "+event)
		}
		if len(pluginHookEvents) > 10 {
			lines = append(lines, fmt.Sprintf("Showing 10 of %d plugin hook events.", len(pluginHookEvents)))
		}
	}
	return strings.Join(lines, "\n")
}

func (r Runner) formatPluginShow(args []string) string {
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		return "Usage: /plugin " + args[0] + " <plugin-name>"
	}
	name := strings.TrimSpace(args[1])
	merged := r.mergedSettings()
	localPlugins := pluginpkg.LoadPluginDirs(pluginpkg.ProjectPluginDirs(r.WorkingDirectory))
	plugin, ok := findLoadedPlugin(localPlugins, name)
	if !ok {
		return "Plugin " + name + " was not found in local plugin manifests."
	}
	state := "enabled"
	if !pluginpkg.PluginEnabled(plugin, merged.EnabledPlugins) {
		state = "disabled"
	}
	lines := []string{
		"Plugin " + plugin.Name,
		"State: " + state,
		"Path: " + plugin.Root,
	}
	if strings.TrimSpace(plugin.Version) != "" {
		lines = append(lines, "Version: "+plugin.Version)
	}
	if strings.TrimSpace(plugin.Description) != "" {
		lines = append(lines, "Description: "+plugin.Description)
	}
	commandNames := loadedPluginCommandNames(plugin)
	lines = append(lines,
		fmt.Sprintf("Commands: %d", len(commandNames)),
		fmt.Sprintf("Skills: %d", len(plugin.SkillCommands)),
		fmt.Sprintf("Agents: %d", len(plugin.Agents)),
		fmt.Sprintf("MCP servers: %d", len(plugin.MCPServers)),
		fmt.Sprintf("Output styles: %d", len(plugin.OutputStyles)),
		fmt.Sprintf("Hooks: %d", pluginHookCount([]pluginpkg.LoadedPlugin{plugin})),
	)
	appendPluginShowSection := func(title string, values []string) {
		if len(values) == 0 {
			return
		}
		lines = append(lines, title+":")
		for _, value := range firstStrings(values, 20) {
			lines = append(lines, "- "+value)
		}
		if len(values) > 20 {
			lines = append(lines, fmt.Sprintf("Showing 20 of %d %s.", len(values), strings.ToLower(title)))
		}
	}
	appendPluginShowSection("Commands", commandNames)
	appendPluginShowSection("Skills", loadedPluginSkillNames(plugin))
	appendPluginShowSection("Agents", loadedPluginAgentNames(plugin))
	appendPluginShowSection("MCP servers", loadedPluginMCPServerNames(plugin))
	appendPluginShowSection("Output styles", loadedPluginOutputStyleNames(plugin))
	appendPluginShowSection("Hook events", loadedPluginHookEventLines(plugin))
	return strings.Join(lines, "\n")
}

func (r Runner) formatPluginSearch(query string) string {
	query = strings.TrimSpace(query)
	merged := r.mergedSettings()
	plugins := pluginpkg.LoadPluginDirs(pluginpkg.ProjectPluginDirs(r.WorkingDirectory))
	results := pluginSearchResults(plugins, merged.EnabledPlugins, query)
	if len(results) == 0 {
		return "No plugins matched " + query + "."
	}
	lines := []string{
		"Plugin search: " + query,
		fmt.Sprintf("Matches: %d", len(results)),
	}
	for _, result := range firstPluginSearchResults(results, 20) {
		name := result.Plugin
		if result.Version != "" {
			name += "@" + result.Version
		}
		lines = append(lines, fmt.Sprintf("- %s (%s): %s", name, result.State, result.Match))
	}
	if len(results) > 20 {
		lines = append(lines, fmt.Sprintf("Showing 20 of %d plugin matches.", len(results)))
	}
	return strings.Join(lines, "\n")
}

func (r Runner) formatPluginMarketplaces() string {
	merged := r.mergedSettings()
	lines := []string{
		"Plugin marketplaces",
		fmt.Sprintf("Extra known marketplaces: %d", len(merged.ExtraKnownMarketplaces)),
		fmt.Sprintf("Strict known marketplaces: %d", len(merged.StrictKnownMarketplaces)),
		fmt.Sprintf("Blocked marketplaces: %d", len(merged.BlockedMarketplaces)),
	}
	if len(merged.ExtraKnownMarketplaces) > 0 {
		lines = append(lines, "Extra known marketplaces:")
		for _, name := range firstStrings(sortedAnyMapKeys(merged.ExtraKnownMarketplaces), 20) {
			lines = append(lines, "- "+name)
		}
		if len(merged.ExtraKnownMarketplaces) > 20 {
			lines = append(lines, fmt.Sprintf("Showing 20 of %d extra known marketplaces.", len(merged.ExtraKnownMarketplaces)))
		}
	}
	if len(merged.StrictKnownMarketplaces) > 0 {
		lines = append(lines, "Strict known marketplaces:")
		for _, name := range firstStrings(pluginAnyListLabels(merged.StrictKnownMarketplaces), 20) {
			lines = append(lines, "- "+name)
		}
		if len(merged.StrictKnownMarketplaces) > 20 {
			lines = append(lines, fmt.Sprintf("Showing 20 of %d strict known marketplaces.", len(merged.StrictKnownMarketplaces)))
		}
	}
	if len(merged.BlockedMarketplaces) > 0 {
		lines = append(lines, "Blocked marketplaces:")
		for _, name := range firstStrings(pluginAnyListLabels(merged.BlockedMarketplaces), 20) {
			lines = append(lines, "- "+name)
		}
		if len(merged.BlockedMarketplaces) > 20 {
			lines = append(lines, fmt.Sprintf("Showing 20 of %d blocked marketplaces.", len(merged.BlockedMarketplaces)))
		}
	}
	return strings.Join(lines, "\n")
}

func (r Runner) formatPluginConfig(args []string) string {
	merged := r.mergedSettings()
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		names := pluginConfigNames(merged)
		if len(names) == 0 {
			return "No plugin configs configured."
		}
		lines := []string{"Plugin configs:"}
		for _, name := range firstStrings(names, 20) {
			lines = append(lines, "- "+name)
		}
		if len(names) > 20 {
			lines = append(lines, fmt.Sprintf("Showing 20 of %d plugin configs.", len(names)))
		}
		return strings.Join(lines, "\n")
	}
	name := strings.TrimSpace(args[1])
	canonical, configValue, hasConfig := findPluginConfig(merged.PluginConfigs, name)
	legacyName, legacyValue, hasLegacy := findLegacyPluginSettings(merged.Plugins, name)
	if !hasConfig && !hasLegacy {
		return "Plugin config " + name + " was not found."
	}
	if canonical == "" {
		canonical = legacyName
	}
	lines := []string{"Plugin config " + canonical}
	if state, ok := pluginEnabledState(merged.EnabledPlugins, canonical); ok {
		lines = append(lines, "State: "+state)
	}
	if hasConfig {
		lines = append(lines, fmt.Sprintf("Option keys: %d", len(configValue.Options)))
		if len(configValue.Options) > 0 {
			lines = append(lines, "Options: "+strings.Join(sortedAnyMapKeys(configValue.Options), ", "))
		}
		lines = append(lines, fmt.Sprintf("MCP server configs: %d", len(configValue.MCPServers)))
		if len(configValue.MCPServers) > 0 {
			lines = append(lines, "MCP server config names: "+strings.Join(sortedNestedAnyMapKeys(configValue.MCPServers), ", "))
		}
	}
	if hasLegacy {
		keys := legacyPluginSettingKeys(legacyValue)
		lines = append(lines, fmt.Sprintf("Legacy settings keys: %d", len(keys)))
		if len(keys) > 0 {
			lines = append(lines, "Legacy settings: "+strings.Join(keys, ", "))
		}
	}
	return strings.Join(lines, "\n")
}

func (r Runner) setPluginEnabledSummary(args []string) string {
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		return "Usage: /plugin " + args[0] + " <plugin-name>"
	}
	name := strings.TrimSpace(args[1])
	enabled := args[0] == "enable"
	if err := setUserPluginEnabled(name, enabled); err != nil {
		return fmt.Sprintf("Failed to %s plugin %s: %v", args[0], name, err)
	}
	if r.MCP != nil {
		if r.MCP.UserSettings.EnabledPlugins == nil {
			r.MCP.UserSettings.EnabledPlugins = map[string]any{}
		}
		r.MCP.UserSettings.EnabledPlugins[name] = enabled
	}
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	return fmt.Sprintf("Plugin %s %s.", name, state)
}

func setUserPluginEnabled(name string, enabled bool) error {
	document, err := readUserSettingsDocument()
	if err != nil {
		return err
	}
	enabledPlugins, _ := document["enabledPlugins"].(map[string]any)
	if enabledPlugins == nil {
		enabledPlugins = map[string]any{}
	}
	enabledPlugins[name] = enabled
	document["enabledPlugins"] = enabledPlugins
	return writeUserSettingsDocument(document)
}

func setUserMCPServerEnabled(name string, enabled bool, allowlistActive bool) error {
	document, err := readUserSettingsDocument()
	if err != nil {
		return err
	}
	if enabled {
		if denied, ok := removeMCPPolicyEntryValue(document["deniedMcpServers"], name, false); ok {
			document["deniedMcpServers"] = denied
		} else {
			delete(document, "deniedMcpServers")
		}
		if allowed, exists := document["allowedMcpServers"]; exists || allowlistActive {
			document["allowedMcpServers"] = appendMCPPolicyEntryValue(allowed, name)
		}
	} else {
		if allowed, exists := document["allowedMcpServers"]; exists {
			if next, ok := removeMCPPolicyEntryValue(allowed, name, true); ok {
				document["allowedMcpServers"] = next
			} else {
				document["allowedMcpServers"] = []any{}
			}
		}
		document["deniedMcpServers"] = appendMCPPolicyEntryValue(document["deniedMcpServers"], name)
	}
	return writeUserSettingsDocument(document)
}

func setMCPServerEnabledInSettings(settings *contracts.Settings, name string, enabled bool, allowlistActive bool) {
	if settings == nil {
		return
	}
	if enabled {
		settings.DeniedMCPServers = removeMCPPolicyEntries(settings.DeniedMCPServers, name)
		if settings.AllowedMCPServers != nil || allowlistActive {
			settings.AllowedMCPServers = appendMCPPolicyEntry(settings.AllowedMCPServers, name)
		}
		return
	}
	if settings.AllowedMCPServers != nil {
		settings.AllowedMCPServers = removeMCPPolicyEntries(settings.AllowedMCPServers, name)
	}
	settings.DeniedMCPServers = appendMCPPolicyEntry(settings.DeniedMCPServers, name)
}

func removeMCPPolicyEntries(entries []contracts.MCPServerPolicyEntry, name string) []contracts.MCPServerPolicyEntry {
	var out []contracts.MCPServerPolicyEntry
	for _, entry := range entries {
		if mcpPolicyEntryNameMatches(entry, name) {
			continue
		}
		out = append(out, entry)
	}
	if out == nil && entries != nil {
		return []contracts.MCPServerPolicyEntry{}
	}
	return out
}

func appendMCPPolicyEntry(entries []contracts.MCPServerPolicyEntry, name string) []contracts.MCPServerPolicyEntry {
	for _, entry := range entries {
		if mcpPolicyEntryNameMatches(entry, name) {
			return entries
		}
	}
	return append(entries, contracts.MCPServerPolicyEntry{ServerName: name})
}

func mcpPolicyEntryNameMatches(entry contracts.MCPServerPolicyEntry, name string) bool {
	name = strings.TrimSpace(name)
	return strings.TrimSpace(entry.ServerName) == name || strings.TrimSpace(entry.Name) == name
}

func appendMCPPolicyEntryValue(value any, name string) []any {
	entries, _ := value.([]any)
	out := append([]any(nil), entries...)
	for _, entry := range out {
		if mcpPolicyEntryValueMatches(entry, name) {
			return out
		}
	}
	return append(out, map[string]any{"serverName": name})
}

func removeMCPPolicyEntryValue(value any, name string, keepEmpty bool) ([]any, bool) {
	entries, _ := value.([]any)
	out := make([]any, 0, len(entries))
	for _, entry := range entries {
		if mcpPolicyEntryValueMatches(entry, name) {
			continue
		}
		out = append(out, entry)
	}
	if len(out) == 0 && !keepEmpty {
		return nil, false
	}
	return out, true
}

func mcpPolicyEntryValueMatches(value any, name string) bool {
	name = strings.TrimSpace(name)
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == name
	}
	entry, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"serverName", "server_name", "name"} {
		if text, ok := entry[key].(string); ok && strings.TrimSpace(text) == name {
			return true
		}
	}
	return false
}

func setUserOutputStyle(name string) error {
	document, err := readUserSettingsDocument()
	if err != nil {
		return err
	}
	document["outputStyle"] = name
	return writeUserSettingsDocument(document)
}

func setUserFastMode(enabled bool) error {
	document, err := readUserSettingsDocument()
	if err != nil {
		return err
	}
	document["fastMode"] = enabled
	return writeUserSettingsDocument(document)
}

func setUserModel(name string) error {
	document, err := readUserSettingsDocument()
	if err != nil {
		return err
	}
	document["model"] = name
	return writeUserSettingsDocument(document)
}

func setUserPermissionMode(mode contracts.PermissionMode) error {
	document, err := readUserSettingsDocument()
	if err != nil {
		return err
	}
	permissions, _ := document["permissions"].(map[string]any)
	if permissions == nil {
		permissions = map[string]any{}
	}
	permissions["defaultMode"] = string(mode)
	document["permissions"] = permissions
	return writeUserSettingsDocument(document)
}

func readUserSettingsDocument() (map[string]any, error) {
	path := config.UserSettingsPath()
	document := map[string]any{}
	data, err := os.ReadFile(path)
	if err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &document); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return document, nil
}

func writeUserSettingsDocument(document map[string]any) error {
	path := config.UserSettingsPath()
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (r Runner) formatMemorySummary(raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) > 0 {
		switch args[0] {
		case "list", "status":
		case "show", "view", "cat":
			query := subcommandRemainder(raw, args[0])
			if strings.TrimSpace(query) != "" {
				return r.formatMemoryFileShow(query)
			}
			return r.formatMemoryShow()
		case "search", "find", "grep":
			query := subcommandRemainder(raw, args[0])
			if strings.TrimSpace(query) == "" {
				return "Usage: /memory " + args[0] + " <query>"
			}
			return r.formatMemorySearch(query)
		default:
			return "Memory subcommand is not implemented in the Go runtime yet: " + strings.Join(args, " ")
		}
	}
	sessionRoot := r.SessionMemoryRoot
	if sessionRoot == "" {
		sessionRoot = memory.DefaultSessionMemoryRoot(r.SessionPath)
	}
	sessionRootText := sessionRoot
	if sessionRootText == "" {
		sessionRootText = "(not configured)"
	}
	relevantRoot := strings.TrimSpace(r.RelevantMemoryDir)
	relevantRootText := relevantRoot
	if relevantRootText == "" {
		relevantRootText = "(not configured)"
	}
	return strings.Join([]string{
		"Memory",
		"Session memory root: " + sessionRootText,
		fmt.Sprintf("Session summaries: %d", countSessionSummaries(sessionRoot)),
		"Relevant memory directory: " + relevantRootText,
		fmt.Sprintf("Relevant memory files: %d", countMarkdownFiles(relevantRoot)),
		"Session memory recall: " + boolEnabledText(r.EnableSessionMemoryRecall),
		"Turn-end memory extraction: " + boolEnabledText(r.EnableMemoryExtraction),
	}, "\n")
}

func (r Runner) formatMemoryShow() string {
	sessionRoot := r.SessionMemoryRoot
	if sessionRoot == "" {
		sessionRoot = memory.DefaultSessionMemoryRoot(r.SessionPath)
	}
	relevantRoot := strings.TrimSpace(r.RelevantMemoryDir)
	lines := []string{"Memory files"}
	appendMemoryFileSection := func(title string, root string) {
		rootText := root
		if strings.TrimSpace(rootText) == "" {
			rootText = "(not configured)"
		}
		lines = append(lines, title+": "+rootText)
		files := collectMarkdownFilePreviews(root, 10)
		if len(files) == 0 {
			lines = append(lines, "- none")
			return
		}
		for _, file := range files {
			lines = append(lines, "- "+file.RelPath+": "+file.Preview)
		}
	}
	appendMemoryFileSection("Session memory root", sessionRoot)
	appendMemoryFileSection("Relevant memory directory", relevantRoot)
	return strings.Join(lines, "\n")
}

func (r Runner) formatMemorySearch(query string) string {
	results, total := searchMemoryMarkdownFiles(r.memoryRoots(), query, 20)
	query = strings.TrimSpace(query)
	if total == 0 {
		return "No memory files matched " + query + "."
	}
	lines := []string{
		"Memory search: " + query,
		fmt.Sprintf("Matches: %d", total),
	}
	for _, result := range results {
		lines = append(lines, fmt.Sprintf("- %s/%s: %s", result.RootLabel, result.RelPath, result.Preview))
	}
	if total > len(results) {
		lines = append(lines, fmt.Sprintf("Showing %d of %d memory matches.", len(results), total))
	}
	return strings.Join(lines, "\n")
}

func (r Runner) formatMemoryFileShow(query string) string {
	roots := r.memoryRoots()
	file, ok := findMemoryMarkdownFile(roots, query)
	if !ok {
		return "Memory file " + strings.TrimSpace(query) + " was not found."
	}
	preview, truncated := readMarkdownBodyPreview(file.Path, 2000)
	lines := []string{
		"Memory file " + file.RelPath,
		"Root: " + file.RootLabel,
		"Path: " + file.RelPath,
		"Absolute path: " + file.Path,
		fmt.Sprintf("Size: %d bytes", file.Size),
		"Modified: " + file.ModTime.Format(time.RFC3339),
		fmt.Sprintf("Truncated: %t", truncated),
		"Content:",
		preview,
	}
	return strings.Join(lines, "\n")
}

func (r Runner) memoryRoots() []memoryRoot {
	sessionRoot := r.SessionMemoryRoot
	if sessionRoot == "" {
		sessionRoot = memory.DefaultSessionMemoryRoot(r.SessionPath)
	}
	return []memoryRoot{
		{Label: "Session memory root", Path: sessionRoot},
		{Label: "Relevant memory directory", Path: strings.TrimSpace(r.RelevantMemoryDir)},
	}
}

func subcommandRemainder(raw string, subcommand string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	subcommand = strings.TrimSpace(subcommand)
	if subcommand == "" {
		return raw
	}
	if len(raw) < len(subcommand) || !strings.EqualFold(raw[:len(subcommand)], subcommand) {
		return raw
	}
	return strings.TrimSpace(raw[len(subcommand):])
}

func (r Runner) mergedSettings() contracts.Settings {
	if r.MCP == nil {
		return contracts.Settings{}
	}
	return config.MergeSettings(r.MCP.UserSettings, r.MCP.ProjectSettings, r.MCP.LocalSettings)
}

func (r Runner) systemPromptWithOutputStyle() string {
	base := strings.TrimSpace(r.SystemPrompt)
	style, ok := r.outputStyleConfig()
	if !ok {
		return base
	}
	section := outputstyles.Section(style)
	if base == "" {
		return section
	}
	return base + "\n\n" + section
}

func (r Runner) outputStyleConfig() (outputstyles.Config, bool) {
	return outputstyles.Resolve(r.WorkingDirectory, r.mergedSettings(), r.outputStylePlugins())
}

func (r Runner) AvailableOutputStyleNames() []string {
	return outputstyles.Names(r.WorkingDirectory, r.outputStylePlugins())
}

func (r Runner) EffectiveOutputStyleName() string {
	return r.effectiveOutputStyleName()
}

func (r Runner) effectiveOutputStyleName() string {
	return outputstyles.EffectiveName(r.WorkingDirectory, r.mergedSettings(), r.outputStylePlugins())
}

func (r Runner) outputStylePlugins() []pluginpkg.LoadedPlugin {
	if strings.TrimSpace(r.WorkingDirectory) == "" {
		return nil
	}
	return pluginpkg.LoadPluginDirsWithSettings(pluginpkg.ProjectPluginDirs(r.WorkingDirectory), r.mergedSettings())
}

func settingsPermissionsSummary(setting *contracts.PermissionsSetting) string {
	if setting == nil {
		return "none"
	}
	parts := []string{
		fmt.Sprintf("allow %d", len(setting.Allow)),
		fmt.Sprintf("deny %d", len(setting.Deny)),
		fmt.Sprintf("ask %d", len(setting.Ask)),
	}
	if setting.DefaultMode != "" {
		parts = append(parts, "default "+string(setting.DefaultMode))
	}
	return strings.Join(parts, ", ")
}

func fileStatusText(path string) string {
	if path == "" {
		return "missing"
	}
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return "directory"
		}
		return "present"
	}
	if os.IsNotExist(err) {
		return "missing"
	}
	return "unreadable"
}

func pluginCommandNames(commandsList []contracts.Command, pluginSkills []string) []string {
	skillNames := map[string]struct{}{}
	for _, name := range pluginSkills {
		skillNames[name] = struct{}{}
	}
	var names []string
	for _, cmd := range commandsList {
		if cmd.Source != contracts.CommandSourcePlugin && cmd.LoadedFrom != "plugin" {
			continue
		}
		name := commands.UserFacingName(cmd)
		if _, ok := skillNames[name]; ok {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func findLoadedPlugin(plugins []pluginpkg.LoadedPlugin, name string) (pluginpkg.LoadedPlugin, bool) {
	name = strings.TrimSpace(name)
	for _, plugin := range plugins {
		for _, candidate := range []string{plugin.Name, filepath.Base(plugin.Root), plugin.Root} {
			if strings.TrimSpace(candidate) == name {
				return plugin, true
			}
		}
	}
	return pluginpkg.LoadedPlugin{}, false
}

func loadedPluginCommandNames(plugin pluginpkg.LoadedPlugin) []string {
	skillNames := map[string]struct{}{}
	for _, command := range plugin.SkillCommands {
		for _, key := range []string{command.Name, commands.UserFacingName(command)} {
			key = strings.TrimSpace(key)
			if key != "" {
				skillNames[key] = struct{}{}
			}
		}
	}
	var names []string
	for _, command := range plugin.Commands {
		name := strings.TrimSpace(commands.UserFacingName(command))
		if name != "" {
			names = append(names, "/"+name)
		}
	}
	for _, prompt := range plugin.PromptTemplates {
		name := strings.TrimSpace(commands.UserFacingName(prompt.Command))
		if name == "" {
			continue
		}
		if _, ok := skillNames[name]; ok {
			continue
		}
		names = append(names, "/"+name)
	}
	sort.Strings(names)
	return names
}

func loadedPluginSkillNames(plugin pluginpkg.LoadedPlugin) []string {
	var names []string
	for _, command := range plugin.SkillCommands {
		name := strings.TrimSpace(commands.UserFacingName(command))
		if name != "" {
			names = append(names, "/"+name)
		}
	}
	sort.Strings(names)
	return names
}

func loadedPluginAgentNames(plugin pluginpkg.LoadedPlugin) []string {
	var names []string
	for _, agent := range plugin.Agents {
		if strings.TrimSpace(agent.Name) != "" {
			names = append(names, agent.Name)
		}
	}
	sort.Strings(names)
	return names
}

func loadedPluginMCPServerNames(plugin pluginpkg.LoadedPlugin) []string {
	var names []string
	for name := range plugin.MCPServers {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func loadedPluginOutputStyleNames(plugin pluginpkg.LoadedPlugin) []string {
	var names []string
	for _, style := range plugin.OutputStyles {
		if strings.TrimSpace(style.Name) != "" {
			names = append(names, style.Name)
		}
	}
	sort.Strings(names)
	return names
}

func loadedPluginHookEventLines(plugin pluginpkg.LoadedPlugin) []string {
	var lines []string
	for _, event := range plugin.HookEvents {
		if strings.TrimSpace(event.Event) != "" && event.Count > 0 {
			lines = append(lines, fmt.Sprintf("%s (%d)", event.Event, event.Count))
		}
	}
	sort.Strings(lines)
	return lines
}

func countEnabledPlugins(values map[string]any) int {
	count := 0
	for _, value := range values {
		if enabled, ok := value.(bool); ok && !enabled {
			continue
		}
		count++
	}
	return count
}

func pluginEnabledStateLines(values map[string]any) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		state := "configured"
		if enabled, ok := values[name].(bool); ok {
			if enabled {
				state = "enabled"
			} else {
				state = "disabled"
			}
		}
		lines = append(lines, name+": "+state)
	}
	return lines
}

func pluginEnabledState(values map[string]any, name string) (string, bool) {
	name = strings.TrimSpace(name)
	for key, value := range values {
		if strings.TrimSpace(key) != name && !strings.EqualFold(strings.TrimSpace(key), name) {
			continue
		}
		if enabled, ok := pluginEnabledValueText(value); ok {
			return enabled, true
		}
		return "configured", true
	}
	return "", false
}

func pluginEnabledValueText(value any) (string, bool) {
	switch typed := value.(type) {
	case bool:
		if typed {
			return "enabled", true
		}
		return "disabled", true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "enabled", "enable", "on", "1":
			return "enabled", true
		case "false", "disabled", "disable", "off", "0":
			return "disabled", true
		}
	case float64:
		if typed == 0 {
			return "disabled", true
		}
		if typed == 1 {
			return "enabled", true
		}
	case int:
		if typed == 0 {
			return "disabled", true
		}
		if typed == 1 {
			return "enabled", true
		}
	}
	return "", false
}

type pluginSearchResult struct {
	Plugin  string
	Version string
	State   string
	Match   string
}

func pluginSearchResults(plugins []pluginpkg.LoadedPlugin, enabledPlugins map[string]any, query string) []pluginSearchResult {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	var results []pluginSearchResult
	for _, plugin := range plugins {
		state := "enabled"
		if !pluginpkg.PluginEnabled(plugin, enabledPlugins) {
			state = "disabled"
		}
		for _, match := range pluginSearchMatches(plugin, query) {
			results = append(results, pluginSearchResult{
				Plugin:  plugin.Name,
				Version: plugin.Version,
				State:   state,
				Match:   match,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Plugin != results[j].Plugin {
			return results[i].Plugin < results[j].Plugin
		}
		if results[i].State != results[j].State {
			return results[i].State < results[j].State
		}
		return results[i].Match < results[j].Match
	})
	return results
}

func pluginSearchMatches(plugin pluginpkg.LoadedPlugin, query string) []string {
	seen := map[string]struct{}{}
	var matches []string
	add := func(label string, values ...string) {
		label = strings.TrimSpace(label)
		if label == "" {
			return
		}
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), query) {
				if _, ok := seen[label]; !ok {
					seen[label] = struct{}{}
					matches = append(matches, label)
				}
				return
			}
		}
	}
	add("plugin metadata", plugin.Name, plugin.Version, plugin.Description, plugin.Root)
	for _, command := range plugin.Commands {
		add("command /"+commands.UserFacingName(command), command.Name, command.DisplayName, command.Description, command.WhenToUse, command.ArgumentHint)
	}
	for _, prompt := range plugin.PromptTemplates {
		command := prompt.Command
		add("command /"+commands.UserFacingName(command), command.Name, command.DisplayName, command.Description, command.WhenToUse, command.ArgumentHint, prompt.Content)
	}
	for _, command := range plugin.SkillCommands {
		add("skill /"+commands.UserFacingName(command), command.Name, command.DisplayName, command.Description, command.WhenToUse, command.ArgumentHint)
	}
	for _, agent := range plugin.Agents {
		add("agent "+agent.Name, agent.Name, agent.Description, agent.Path)
	}
	for name, server := range plugin.MCPServers {
		add("MCP server "+name, name, server.Name, server.ID, server.Command, server.URL, server.PluginSource)
	}
	for _, style := range plugin.OutputStyles {
		add("output style "+style.Name, style.Name, style.Description, style.Path)
	}
	for _, event := range plugin.HookEvents {
		add("hook "+event.Event, event.Event)
	}
	sort.Strings(matches)
	return matches
}

func pluginConfigNames(settings contracts.Settings) []string {
	seen := map[string]struct{}{}
	var names []string
	for name := range settings.PluginConfigs {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for name := range settings.Plugins {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func findPluginConfig(values map[string]contracts.PluginConfig, name string) (string, contracts.PluginConfig, bool) {
	name = strings.TrimSpace(name)
	if value, ok := values[name]; ok {
		return name, value, true
	}
	for key, value := range values {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return key, value, true
		}
	}
	return "", contracts.PluginConfig{}, false
}

func findLegacyPluginSettings(values map[string]any, name string) (string, any, bool) {
	name = strings.TrimSpace(name)
	if value, ok := values[name]; ok {
		return name, value, true
	}
	for key, value := range values {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return key, value, true
		}
	}
	return "", nil, false
}

func legacyPluginSettingKeys(value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		return sortedAnyMapKeys(typed)
	case map[string]string:
		return sortedStringMapKeys(typed)
	default:
		return nil
	}
}

func sortedAnyMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func sortedNestedAnyMapKeys(values map[string]map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func pluginAnyListLabels(values []any) []string {
	labels := make([]string, 0, len(values))
	for _, value := range values {
		label := pluginAnyLabel(value)
		if label != "" {
			labels = append(labels, label)
		}
	}
	sort.Strings(labels)
	return labels
}

func pluginAnyLabel(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"name", "id", "url", "source", "marketplace"} {
			if text, ok := typed[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	case map[string]string:
		for _, key := range []string{"name", "id", "url", "source", "marketplace"} {
			if text := strings.TrimSpace(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func pluginSkillNames(plugins []pluginpkg.LoadedPlugin) []string {
	var names []string
	for _, plugin := range plugins {
		for _, skill := range plugin.SkillCommands {
			name := commands.UserFacingName(skill)
			if strings.TrimSpace(name) != "" {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func pluginAgentNames(plugins []pluginpkg.LoadedPlugin) []string {
	var names []string
	for _, plugin := range plugins {
		for _, agent := range plugin.Agents {
			if strings.TrimSpace(agent.Name) != "" {
				names = append(names, agent.Name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func pluginMCPServerNames(plugins []pluginpkg.LoadedPlugin) []string {
	var names []string
	for _, plugin := range plugins {
		for name := range plugin.MCPServers {
			if strings.TrimSpace(name) != "" {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func pluginOutputStyleNames(plugins []pluginpkg.LoadedPlugin) []string {
	var names []string
	for _, plugin := range plugins {
		for _, style := range plugin.OutputStyles {
			if strings.TrimSpace(style.Name) != "" {
				names = append(names, style.Name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func pluginHookCount(plugins []pluginpkg.LoadedPlugin) int {
	count := 0
	for _, plugin := range plugins {
		for _, event := range plugin.HookEvents {
			count += event.Count
		}
	}
	return count
}

func pluginHookEventLines(plugins []pluginpkg.LoadedPlugin) []string {
	counts := map[string]int{}
	for _, plugin := range plugins {
		for _, event := range plugin.HookEvents {
			if strings.TrimSpace(event.Event) != "" && event.Count > 0 {
				counts[event.Event] += event.Count
			}
		}
	}
	events := make([]string, 0, len(counts))
	for event := range counts {
		events = append(events, event)
	}
	sort.Strings(events)
	lines := make([]string, 0, len(events))
	for _, event := range events {
		lines = append(lines, fmt.Sprintf("%s (%d)", event, counts[event]))
	}
	return lines
}

func firstStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func firstMCPSummaries(values []mcpServerSummary, limit int) []mcpServerSummary {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func firstLoadedPlugins(values []pluginpkg.LoadedPlugin, limit int) []pluginpkg.LoadedPlugin {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func firstPluginSearchResults(values []pluginSearchResult, limit int) []pluginSearchResult {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func countSessionSummaries(root string) int {
	if root == "" {
		return 0
	}
	var count int
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != memory.SessionSummaryFilename {
			return nil
		}
		count++
		return nil
	})
	return count
}

func countMarkdownFiles(root string) int {
	if root == "" {
		return 0
	}
	var count int
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			count++
		}
		return nil
	})
	return count
}

type markdownFilePreview struct {
	Path    string
	RelPath string
	Preview string
	ModTime time.Time
}

type memoryRoot struct {
	Label string
	Path  string
}

type memoryMarkdownFile struct {
	RootLabel string
	Path      string
	RelPath   string
	Size      int64
	ModTime   time.Time
}

type memorySearchResult struct {
	RootLabel string
	RelPath   string
	Preview   string
}

func collectMarkdownFilePreviews(root string, limit int) []markdownFilePreview {
	if strings.TrimSpace(root) == "" || limit <= 0 {
		return nil
	}
	var files []markdownFilePreview
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		files = append(files, markdownFilePreview{
			Path:    path,
			RelPath: filepath.ToSlash(rel),
			Preview: markdownPreview(path),
			ModTime: info.ModTime(),
		})
		return nil
	})
	sort.Slice(files, func(i, j int) bool {
		if !files[i].ModTime.Equal(files[j].ModTime) {
			return files[i].ModTime.After(files[j].ModTime)
		}
		return files[i].RelPath < files[j].RelPath
	})
	if len(files) > limit {
		files = files[:limit]
	}
	return files
}

func searchMemoryMarkdownFiles(roots []memoryRoot, query string, limit int) ([]memorySearchResult, int) {
	query = strings.TrimSpace(query)
	if query == "" || limit <= 0 {
		return nil, 0
	}
	queryLower := strings.ToLower(query)
	var results []memorySearchResult
	var total int
	for _, root := range roots {
		rootPath := strings.TrimSpace(root.Path)
		if rootPath == "" {
			continue
		}
		rootAbs, err := filepath.Abs(rootPath)
		if err != nil {
			continue
		}
		_ = filepath.WalkDir(rootAbs, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				name := entry.Name()
				if name == ".git" || name == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(path), ".md") {
				return nil
			}
			file, ok := memoryMarkdownFileFromPath(root, rootAbs, path)
			if !ok {
				return nil
			}
			data, err := os.ReadFile(file.Path)
			if err != nil {
				return nil
			}
			_, body := memory.ParseFrontmatter(string(data))
			searchText := strings.ToLower(file.RelPath + "\n" + body)
			if !strings.Contains(searchText, queryLower) {
				return nil
			}
			total++
			if len(results) < limit {
				results = append(results, memorySearchResult{
					RootLabel: root.Label,
					RelPath:   file.RelPath,
					Preview:   markdownSearchPreview(body, queryLower),
				})
			}
			return nil
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].RootLabel != results[j].RootLabel {
			return results[i].RootLabel < results[j].RootLabel
		}
		return results[i].RelPath < results[j].RelPath
	})
	return results, total
}

func findMemoryMarkdownFile(roots []memoryRoot, query string) (memoryMarkdownFile, bool) {
	query = strings.TrimSpace(strings.TrimPrefix(query, "/"))
	if query == "" {
		return memoryMarkdownFile{}, false
	}
	for _, root := range roots {
		if file, ok := findMemoryMarkdownFileInRoot(root, query); ok {
			return file, true
		}
	}
	return memoryMarkdownFile{}, false
}

func findMemoryMarkdownFileInRoot(root memoryRoot, query string) (memoryMarkdownFile, bool) {
	rootPath := strings.TrimSpace(root.Path)
	if rootPath == "" {
		return memoryMarkdownFile{}, false
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		return memoryMarkdownFile{}, false
	}
	var candidates []string
	if filepath.IsAbs(query) {
		candidates = append(candidates, query)
	} else {
		candidates = append(candidates, filepath.Join(rootAbs, filepath.FromSlash(query)))
		if !strings.EqualFold(filepath.Ext(query), ".md") {
			candidates = append(candidates, filepath.Join(rootAbs, filepath.FromSlash(query+".md")))
		}
	}
	for _, candidate := range candidates {
		if file, ok := memoryMarkdownFileFromPath(root, rootAbs, candidate); ok {
			return file, true
		}
	}
	var found memoryMarkdownFile
	matched := false
	querySlash := filepath.ToSlash(strings.TrimSuffix(query, ".md"))
	_ = filepath.WalkDir(rootAbs, func(path string, entry os.DirEntry, err error) error {
		if matched || err != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		relNoExt := strings.TrimSuffix(relSlash, filepath.Ext(relSlash))
		if strings.EqualFold(relSlash, filepath.ToSlash(query)) || strings.EqualFold(relNoExt, querySlash) || strings.EqualFold(filepath.Base(relNoExt), querySlash) {
			if file, ok := memoryMarkdownFileFromPath(root, rootAbs, path); ok {
				found = file
				matched = true
			}
		}
		return nil
	})
	return found, matched
}

func memoryMarkdownFileFromPath(root memoryRoot, rootAbs string, path string) (memoryMarkdownFile, bool) {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return memoryMarkdownFile{}, false
	}
	rootCheck := rootAbs
	if resolvedRoot, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootCheck = resolvedRoot
	}
	pathCheck := pathAbs
	if resolvedPath, err := filepath.EvalSymlinks(pathAbs); err == nil {
		pathCheck = resolvedPath
	}
	if !pathWithinRoot(pathCheck, rootCheck) || !strings.EqualFold(filepath.Ext(pathAbs), ".md") {
		return memoryMarkdownFile{}, false
	}
	info, err := os.Stat(pathAbs)
	if err != nil || info.IsDir() {
		return memoryMarkdownFile{}, false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		rel = filepath.Base(pathAbs)
	}
	return memoryMarkdownFile{
		RootLabel: root.Label,
		Path:      pathAbs,
		RelPath:   filepath.ToSlash(rel),
		Size:      info.Size(),
		ModTime:   info.ModTime(),
	}, true
}

func pathWithinRoot(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != "" && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func markdownPreview(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "(unreadable)"
	}
	lines := strings.Split(string(data), "\n")
	inFrontmatter := false
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		inFrontmatter = true
		lines = lines[1:]
	}
	for _, line := range lines {
		text := strings.TrimSpace(line)
		if inFrontmatter {
			if text == "---" {
				inFrontmatter = false
			}
			continue
		}
		if text == "" {
			continue
		}
		return truncatePreviewLine(strings.TrimLeft(text, "# "))
	}
	return "(empty)"
}

func markdownSearchPreview(body string, queryLower string) string {
	for _, line := range strings.Split(body, "\n") {
		text := strings.TrimSpace(line)
		if text == "" || strings.TrimSpace(queryLower) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(text), queryLower) {
			return truncatePreviewLine(strings.TrimLeft(text, "# "))
		}
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "(empty)"
	}
	for _, line := range strings.Split(body, "\n") {
		text := strings.TrimSpace(line)
		if text != "" {
			return truncatePreviewLine(strings.TrimLeft(text, "# "))
		}
	}
	return "(empty)"
}

func readMarkdownBodyPreview(path string, limit int) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "(unreadable)", false
	}
	_, body := memory.ParseFrontmatter(string(data))
	body = strings.TrimSpace(body)
	if body == "" {
		return "(empty)", false
	}
	if limit <= 0 || len(body) <= limit {
		return body, false
	}
	if limit <= 3 {
		return body[:limit], true
	}
	return body[:limit-3] + "...", true
}

func truncatePreviewLine(text string) string {
	const limit = 96
	if len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}

func boolEnabledText(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}

func (r Runner) toolNames() []string {
	if r.Tools.Registry == nil {
		return nil
	}
	names := append([]string(nil), r.Tools.Registry.Names()...)
	sort.Strings(names)
	return names
}

func (r Runner) mcpServerNames() []string {
	servers := r.mcpServers()
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}
	return names
}

func (r *Runner) formatModelSummary(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "Current model: " + r.model()
	}
	switch strings.ToLower(raw) {
	case "list", "status", "available", "models":
		return formatModelList(r.model())
	}
	name, display := resolveModelSelection(raw)
	r.Model = name
	if display != "" && display != name {
		return fmt.Sprintf("Selected model: %s\nDisplay name: %s", name, display)
	}
	return "Selected model: " + name
}

func formatModelList(current string) string {
	registry := modelpkg.DefaultRegistry()
	names := make([]string, 0, len(registry.Models))
	for name := range registry.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	aliases := make([]string, 0, len(registry.Aliases))
	for alias := range registry.Aliases {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	lines := []string{
		"Available models",
		"Current model: " + current,
		fmt.Sprintf("Models: %d", len(names)),
		fmt.Sprintf("Aliases: %d", len(aliases)),
	}
	for _, name := range names {
		capability := registry.Models[name]
		display := strings.TrimSpace(capability.DisplayName)
		if display == "" {
			display = capability.Name
		}
		flags := modelCapabilityFlags(capability)
		if flags != "" {
			flags = ", " + flags
		}
		lines = append(lines, fmt.Sprintf(
			"- %s: %s (context %d, max output %d%s)",
			display,
			capability.Name,
			capability.ContextWindowTokens,
			capability.MaxOutputTokens,
			flags,
		))
	}
	if len(aliases) > 0 {
		lines = append(lines, "Alias names: "+strings.Join(aliases, ", "))
	}
	return strings.Join(lines, "\n")
}

func modelCapabilityFlags(capability modelpkg.Capability) string {
	var flags []string
	if capability.SupportsThinking {
		flags = append(flags, "thinking")
	}
	if capability.SupportsEffort {
		flags = append(flags, "effort")
	}
	if capability.Supports1MContext {
		flags = append(flags, "1m")
	}
	if capability.AlwaysOnThinking {
		flags = append(flags, "always-on-thinking")
	}
	return strings.Join(flags, ", ")
}

func resolveModelSelection(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if capability, ok := modelpkg.DefaultRegistry().Resolve(raw); ok {
		display := strings.TrimSpace(capability.DisplayName)
		if display == "" {
			display = capability.Name
		}
		return capability.Name, display
	}
	return raw, raw
}

func (r Runner) formatMCPCommandSummary(raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) > 0 {
		switch args[0] {
		case "list", "status":
		case "show", "info":
			return r.formatMCPServerShow(args)
		case "enable", "disable":
			return r.setMCPServerEnabledSummary(args)
		default:
			return "MCP subcommand is not implemented in the Go runtime yet: " + strings.Join(args, " ")
		}
	}
	servers := r.mcpServers()
	if len(servers) == 0 {
		return "No MCP servers configured."
	}
	var lines []string
	lines = append(lines, "MCP servers:")
	for _, server := range servers {
		status := ""
		if !server.Policy.Allowed {
			status = fmt.Sprintf(" [blocked: %s]", server.Policy.Reason)
		}
		lines = append(lines, fmt.Sprintf("- %s (%s): %s%s", server.Name, mcpServerTransport(server.Config), mcpServerTarget(server.Config), status))
	}
	return strings.Join(lines, "\n")
}

func (r Runner) formatMCPServerShow(args []string) string {
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		return "Usage: /mcp " + args[0] + " <server-name>"
	}
	name := strings.TrimSpace(args[1])
	server, ok := findMCPServerSummary(r.mcpServers(), name)
	if !ok {
		return "MCP server " + name + " was not found."
	}
	config := server.Config
	status := "configured"
	if !server.Policy.Allowed {
		status = "blocked"
	}
	lines := []string{
		"MCP server " + server.Name,
		"Status: " + status,
		"Policy: " + server.Policy.Reason,
		"Transport: " + mcpServerTransport(config),
		"Target: " + mcpServerTarget(config),
		"Source: " + mcpServerSource(config),
	}
	if scope := strings.TrimSpace(config.Scope); scope != "" {
		lines = append(lines, "Scope: "+scope)
	}
	if pluginSource := strings.TrimSpace(config.PluginSource); pluginSource != "" {
		lines = append(lines, "Plugin source: "+pluginSource)
	}
	if configuredName := strings.TrimSpace(config.Name); configuredName != "" && configuredName != server.Name {
		lines = append(lines, "Configured name: "+configuredName)
	}
	if id := strings.TrimSpace(config.ID); id != "" {
		lines = append(lines, "ID: "+id)
	}
	if ideName := strings.TrimSpace(config.IDEName); ideName != "" {
		lines = append(lines, "IDE name: "+ideName)
	}
	if command := strings.TrimSpace(config.Command); command != "" {
		lines = append(lines, "Command: "+command)
	}
	if len(config.Args) > 0 {
		lines = append(lines, "Args: "+strings.Join(config.Args, " "))
	}
	if url := strings.TrimSpace(config.URL); url != "" {
		lines = append(lines, "URL: "+url)
	}
	if len(config.Env) > 0 {
		lines = append(lines, fmt.Sprintf("Env vars: %d", len(config.Env)))
		lines = append(lines, "Env names: "+strings.Join(sortedStringMapKeys(config.Env), ", "))
	}
	if len(config.Headers) > 0 {
		lines = append(lines, fmt.Sprintf("Headers: %d", len(config.Headers)))
		lines = append(lines, "Header names: "+strings.Join(sortedStringMapKeys(config.Headers), ", "))
	}
	if strings.TrimSpace(config.HeadersHelper) != "" {
		lines = append(lines, "Headers helper: configured")
	}
	if strings.TrimSpace(config.AuthToken) != "" {
		lines = append(lines, "Auth token: configured")
	}
	if config.OAuth != nil {
		lines = append(lines, "OAuth: configured")
		if clientID := strings.TrimSpace(config.OAuth.ClientID); clientID != "" {
			lines = append(lines, "OAuth client ID: "+clientID)
		}
		if config.OAuth.CallbackPort != nil {
			lines = append(lines, fmt.Sprintf("OAuth callback port: %d", *config.OAuth.CallbackPort))
		}
		if metadataURL := strings.TrimSpace(config.OAuth.AuthServerMetadataURL); metadataURL != "" {
			lines = append(lines, "OAuth metadata URL: "+metadataURL)
		}
	}
	return strings.Join(lines, "\n")
}

func findMCPServerSummary(servers []mcpServerSummary, name string) (mcpServerSummary, bool) {
	name = strings.TrimSpace(name)
	for _, server := range servers {
		if server.Name == name {
			return server, true
		}
	}
	for _, server := range servers {
		if strings.EqualFold(server.Name, name) {
			return server, true
		}
	}
	return mcpServerSummary{}, false
}

func (r Runner) setMCPServerEnabledSummary(args []string) string {
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		return "Usage: /mcp " + args[0] + " <server-name>"
	}
	name := strings.TrimSpace(args[1])
	enabled := args[0] == "enable"
	allowlistActive := r.mergedSettings().AllowedMCPServers != nil
	if err := setUserMCPServerEnabled(name, enabled, allowlistActive); err != nil {
		return fmt.Sprintf("Failed to %s MCP server %s: %v", args[0], name, err)
	}
	if r.MCP != nil {
		setMCPServerEnabledInSettings(&r.MCP.UserSettings, name, enabled, allowlistActive)
	}
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	return fmt.Sprintf("MCP server %s %s.", name, state)
}

type mcpServerSummary struct {
	Name   string
	Config contracts.MCPServer
	Policy mcp.PolicyDecision
}

func (r Runner) mcpServers() []mcpServerSummary {
	if r.MCP == nil {
		return nil
	}
	merged := config.MergeSettings(r.MCP.UserSettings, r.MCP.ProjectSettings, r.MCP.LocalSettings)
	mcpServers := mcp.MergeServers(merged.MCPServers, r.MCP.PluginServers)
	policy := mcp.PolicyFromSettings(merged)
	servers := make([]mcpServerSummary, 0, len(mcpServers))
	for name, server := range mcpServers {
		servers = append(servers, mcpServerSummary{Name: name, Config: server, Policy: mcp.EvaluatePolicy(name, server, policy)})
	}
	sort.Slice(servers, func(i, j int) bool {
		return servers[i].Name < servers[j].Name
	})
	return servers
}

func mcpServerTransport(server contracts.MCPServer) string {
	if server.Type != "" {
		return server.Type
	}
	if strings.TrimSpace(server.URL) != "" {
		return "http"
	}
	return "stdio"
}

func mcpServerTarget(server contracts.MCPServer) string {
	if url := strings.TrimSpace(server.URL); url != "" {
		return url
	}
	if command := strings.TrimSpace(server.Command); command != "" {
		if len(server.Args) == 0 {
			return command
		}
		return command + " " + strings.Join(server.Args, " ")
	}
	return "(no target)"
}

func mcpServerSource(server contracts.MCPServer) string {
	if strings.TrimSpace(server.PluginSource) != "" {
		return "plugin"
	}
	if scope := strings.TrimSpace(server.Scope); scope != "" {
		return scope
	}
	return "settings"
}

func sortedStringMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func (r Runner) formatResumeSummary(raw string) (string, error) {
	query := strings.TrimSpace(raw)
	cwd := r.WorkingDirectory
	if cwd == "" {
		cwd = "."
	}
	args := strings.Fields(query)
	if len(args) > 0 {
		switch args[0] {
		case "list", "status":
			query = ""
		case "show", "info":
			target := subcommandRemainder(query, args[0])
			if strings.TrimSpace(target) == "" {
				return "Usage: /resume " + args[0] + " <session-id>", nil
			}
			return formatResumeSessionDetail(cwd, target)
		}
	}
	if query != "" {
		results, err := session.SearchProjectSessions(cwd, query, 10)
		if err != nil {
			return "", err
		}
		if len(results) == 0 {
			return fmt.Sprintf("No sessions found for %q.", query), nil
		}
		var lines []string
		lines = append(lines, fmt.Sprintf("Matching sessions for %q:", query))
		for _, result := range results {
			lines = append(lines, formatSessionInfoLine(result.SessionInfo))
		}
		return strings.Join(lines, "\n"), nil
	}
	page, err := session.ListProjectSessionsPage(cwd, 0, 10)
	if err != nil {
		return "", err
	}
	if len(page.Sessions) == 0 {
		return "No sessions found for " + cwd + ".", nil
	}
	var lines []string
	lines = append(lines, "Recent sessions:")
	for _, info := range page.Sessions {
		lines = append(lines, formatSessionInfoLine(info))
	}
	if page.HasMore {
		lines = append(lines, fmt.Sprintf("Showing %d of %d sessions.", len(page.Sessions), page.Total))
	}
	return strings.Join(lines, "\n"), nil
}

func formatResumeSessionDetail(cwd string, target string) (string, error) {
	info, ok, err := findProjectSession(cwd, target)
	if err != nil {
		return "", err
	}
	if !ok {
		return "Session " + strings.TrimSpace(target) + " was not found.", nil
	}
	index, err := session.LoadTranscriptIndex(info.Path, info.ID)
	if err != nil {
		return "", err
	}
	title := strings.TrimSpace(index.Title)
	if title == "" {
		title = strings.TrimSpace(info.Title)
	}
	if title == "" {
		title = "(untitled)"
	}
	modified := "unknown time"
	if !info.Modified.IsZero() {
		modified = info.Modified.Format(time.RFC3339)
	}
	lines := []string{
		"Session " + string(info.ID),
		"Title: " + title,
		"Path: " + info.Path,
		"Modified: " + modified,
		fmt.Sprintf("Size: %d bytes", info.Size),
		fmt.Sprintf("Messages: %d", index.MessageCount),
		fmt.Sprintf("User messages: %d", index.UserMessageCount),
		fmt.Sprintf("Assistant messages: %d", index.AssistantMessageCount),
		fmt.Sprintf("System messages: %d", index.SystemMessageCount),
	}
	if index.FirstUUID != "" {
		lines = append(lines, "First message UUID: "+string(index.FirstUUID))
	}
	if index.LastUUID != "" {
		lines = append(lines, "Last message UUID: "+string(index.LastUUID))
	}
	if index.FirstTimestamp != "" {
		lines = append(lines, "First timestamp: "+index.FirstTimestamp)
	}
	if index.LastTimestamp != "" {
		lines = append(lines, "Last timestamp: "+index.LastTimestamp)
	}
	if text := strings.TrimSpace(index.FirstUserText); text != "" {
		lines = append(lines, "First user: "+truncatePreviewLine(text))
	}
	if text := strings.TrimSpace(index.LastUserText); text != "" {
		lines = append(lines, "Last user: "+truncatePreviewLine(text))
	}
	if text := strings.TrimSpace(index.LastAssistantText); text != "" {
		lines = append(lines, "Last assistant: "+truncatePreviewLine(text))
	}
	if projectPath := strings.TrimSpace(index.ProjectPath); projectPath != "" {
		lines = append(lines, "Project path: "+projectPath)
	} else if projectPath := strings.TrimSpace(info.ProjectPath); projectPath != "" {
		lines = append(lines, "Project path: "+projectPath)
	}
	if branch := strings.TrimSpace(index.GitBranch); branch != "" {
		lines = append(lines, "Git branch: "+branch)
	} else if branch := strings.TrimSpace(info.GitBranch); branch != "" {
		lines = append(lines, "Git branch: "+branch)
	}
	if index.AITitle != "" {
		lines = append(lines, "AI title: "+index.AITitle)
	}
	if index.LastPrompt != "" {
		lines = append(lines, "Last prompt: "+truncatePreviewLine(index.LastPrompt))
	}
	if index.TaskSummary != "" {
		lines = append(lines, "Task summary: "+truncatePreviewLine(index.TaskSummary))
	}
	if index.Tag != "" {
		lines = append(lines, "Tag: "+index.Tag)
	}
	if index.AgentName != "" {
		lines = append(lines, "Agent: "+index.AgentName)
	}
	if index.AgentSetting != "" {
		lines = append(lines, "Agent setting: "+index.AgentSetting)
	}
	if index.Mode != "" {
		lines = append(lines, "Mode: "+index.Mode)
	}
	if index.PRNumber != 0 || index.PRURL != "" || index.PRRepository != "" {
		lines = append(lines, "Pull request: "+formatSessionPR(index))
	}
	lines = append(lines, fmt.Sprintf("Summaries: %d", index.SummaryCount))
	lines = append(lines, fmt.Sprintf("Content replacements: %d", index.ContentReplacementCount))
	lines = append(lines, fmt.Sprintf("Worktree state: %t", index.HasWorktreeState))
	return strings.Join(lines, "\n"), nil
}

func findProjectSession(cwd string, target string) (session.SessionInfo, bool, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return session.SessionInfo{}, false, nil
	}
	sessions, err := session.ListProjectSessions(cwd)
	if err != nil {
		return session.SessionInfo{}, false, err
	}
	for _, info := range sessions {
		if string(info.ID) == target || info.Path == target {
			return info, true, nil
		}
	}
	for _, info := range sessions {
		if strings.EqualFold(string(info.ID), target) {
			return info, true, nil
		}
	}
	return session.SessionInfo{}, false, nil
}

func formatSessionPR(index session.TranscriptIndex) string {
	var parts []string
	if index.PRNumber != 0 {
		parts = append(parts, fmt.Sprintf("#%d", index.PRNumber))
	}
	if index.PRRepository != "" {
		parts = append(parts, index.PRRepository)
	}
	if index.PRURL != "" {
		parts = append(parts, index.PRURL)
	}
	if len(parts) == 0 {
		return "(unknown)"
	}
	return strings.Join(parts, " ")
}

func formatSessionInfoLine(info session.SessionInfo) string {
	title := strings.TrimSpace(info.Title)
	if title == "" {
		title = "(untitled)"
	}
	modified := "unknown time"
	if !info.Modified.IsZero() {
		modified = info.Modified.Format(time.RFC3339)
	}
	return fmt.Sprintf("- %s - %s - %s", info.ID, title, modified)
}

func (r Runner) appendCompactTranscript(plan compactpkg.Plan) error {
	if r.SessionPath != "" {
		if err := session.AppendTranscriptMessage(r.SessionPath, compactpkg.BoundaryTranscriptMessage(plan.Boundary, plan.Metadata)); err != nil {
			return err
		}
		if err := session.Append(r.SessionPath, session.EntryFromMessage(r.SessionID, plan.Summary)); err != nil {
			return err
		}
	}
	root := r.SessionMemoryRoot
	if root == "" {
		root = memory.DefaultSessionMemoryRoot(r.SessionPath)
	}
	if root == "" || r.SessionID == "" {
		return nil
	}
	_, err := memory.WriteSessionSummary(memory.SessionSummaryOptions{
		Root:            root,
		SessionID:       r.SessionID,
		Summary:         msgs.TextContent(plan.Summary),
		LastMessageUUID: plan.Summary.UUID,
		Metadata:        plan.Metadata,
	})
	return err
}

func (r Runner) maybeExtractSessionMemory(ctx context.Context, messages []contracts.Message) error {
	if !r.EnableMemoryExtraction || r.SessionID == "" || len(messages) == 0 {
		return nil
	}
	root := r.SessionMemoryRoot
	if root == "" {
		root = memory.DefaultSessionMemoryRoot(r.SessionPath)
	}
	if root == "" {
		return nil
	}
	result, err := (memory.Agent{
		Client:    r.MemoryAgentClient,
		Model:     r.model(),
		MaxTokens: r.CompactMaxTokens,
	}).Extract(ctx, messages, memory.ExtractOptions{Limit: r.MemoryExtractLimit})
	if err != nil {
		return err
	}
	summary := memory.BuildFactsSummary(result.Facts)
	if summary == "" {
		return nil
	}
	_, err = memory.WriteSessionSummary(memory.SessionSummaryOptions{
		Root:            root,
		SessionID:       r.SessionID,
		Summary:         summary,
		LastMessageUUID: messages[len(messages)-1].UUID,
	})
	return err
}

func (r Runner) send(ctx context.Context, history []contracts.Message, relevantMemoryPrefetch *relevantMemoryPrefetchTask) (anthropic.Request, []string, *anthropic.Response, time.Duration, error) {
	models := append([]string{r.model()}, r.FallbackModels...)
	var attempts []string
	var lastRequest anthropic.Request
	var lastErr error
	var apiDuration time.Duration
	relevantMemory, err := relevantMemoryPrefetch.requestContext(ctx)
	if err != nil {
		return anthropic.Request{}, attempts, nil, apiDuration, err
	}
	for i, model := range models {
		historyForRequest, err := r.applyToolResultBudget(history)
		if err != nil {
			return anthropic.Request{}, attempts, nil, apiDuration, err
		}
		request, err := r.buildRequest(historyForRequest, model, relevantMemory)
		if err != nil {
			return anthropic.Request{}, attempts, nil, apiDuration, err
		}
		lastRequest = request
		attempts = append(attempts, model)
		startedAt := time.Now()
		response, err := r.createMessage(ctx, request)
		apiDuration += time.Since(startedAt)
		if err == nil {
			return request, attempts, response, apiDuration, nil
		}
		lastErr = err
		if i == len(models)-1 || !isFallbackEligible(err) {
			return request, attempts, nil, apiDuration, err
		}
		r.emit(Event{Type: EventRetry, Model: model, Error: err})
	}
	return lastRequest, attempts, nil, apiDuration, lastErr
}

func (r Runner) applyToolResultBudget(history []contracts.Message) ([]contracts.Message, error) {
	if r.ContentBudget == nil {
		return history, nil
	}
	storeDir := r.ContentBudgetDir
	if storeDir == "" && r.SessionPath != "" && r.SessionID != "" {
		storeDir = filepath.Join(filepath.Dir(r.SessionPath), string(r.SessionID), "tool-results")
	}
	updated, records, err := session.ApplyToolResultBudget(history, r.ContentBudget, session.ToolResultBudgetOptions{
		LimitChars:    r.ContentBudgetMax,
		StoreDir:      storeDir,
		SkipToolNames: r.SkipBudgetTools,
	})
	if err != nil {
		return history, err
	}
	if len(records) > 0 && r.SessionPath != "" {
		if err := session.AppendContentReplacements(r.SessionPath, r.SessionID, records); err != nil {
			return history, err
		}
	}
	return updated, nil
}

func (r Runner) createMessage(ctx context.Context, request anthropic.Request) (*anthropic.Response, error) {
	if r.UseStreaming {
		if streamer, ok := r.Client.(StreamingMessageClient); ok {
			request.Stream = true
			acc := anthropic.NewStreamAccumulator()
			if err := streamer.StreamMessages(ctx, request, func(event anthropic.StreamEvent) error {
				eventCopy := event
				r.emit(Event{Type: EventStreamEvent, StreamEvent: &eventCopy, Model: request.Model})
				return acc.Add(event)
			}); err != nil {
				return nil, err
			}
			return acc.Finish(), nil
		}
	}
	return r.Client.CreateMessage(ctx, request)
}

func (r Runner) executeToolUses(ctx context.Context, uses []contracts.ToolUse, metadata map[string]any, turnMessages []contracts.Message) ([]contracts.Message, []contracts.ToolResult) {
	toolMessages := make([]contracts.Message, 0, len(uses))
	toolResults := make([]contracts.ToolResult, 0, len(uses))
	for _, use := range uses {
		use := use
		r.emit(Event{Type: EventToolUse, ToolUse: &use})
	}
	toolCtx := tool.Context{
		Context:          ctx,
		WorkingDirectory: r.WorkingDirectory,
		SessionID:        r.SessionID,
		Permissions:      r.permissionsForTurn(turnMessages),
		Metadata:         metadata,
	}
	for update := range tool.RunTools(toolCtx, r.Tools, uses, nil, tool.RunOptions{}) {
		use := update.ToolUse
		result := update.Result
		err := update.Err
		if err != nil && result.Content == nil {
			result = tool.ErrorResult(use, err)
		}
		message := msgs.ToolResult(use.ID, result.Content, result.IsError)
		if r.SessionID != "" {
			message.SessionID = r.SessionID
		}
		toolMessages = append(toolMessages, message)
		r.emit(Event{Type: EventToolResult, Message: &message, ToolResult: &result})
		if !result.IsError {
			for _, newMessage := range result.NewMessages {
				if newMessage.Type == "" {
					newMessage.Type = contracts.MessageUser
				}
				if newMessage.UUID == "" {
					newMessage.UUID = contracts.NewID()
				}
				if newMessage.SessionID == "" && r.SessionID != "" {
					newMessage.SessionID = r.SessionID
				}
				toolMessages = append(toolMessages, newMessage)
				r.emit(Event{Type: EventUserMessage, Message: &newMessage})
			}
		}
		toolResults = append(toolResults, result)
	}
	return toolMessages, toolResults
}

func (r Runner) permissionsForTurn(messages []contracts.Message) tool.PermissionDecider {
	commandPermissions := commands.CommandPermissionsFromMessages(messages)
	if len(commandPermissions.AllowedTools) == 0 {
		return r.Permissions
	}
	rules := commandPermissionRules(commandPermissions.AllowedTools)
	if len(rules) == 0 {
		return r.Permissions
	}
	switch decider := r.Permissions.(type) {
	case tool.EnginePermissionDecider:
		baseRules := decider.Engine.Rules()
		baseRules = append(baseRules, rules...)
		return tool.NewEnginePermissionDecider(permissions.NewEngine(decider.Engine.Context(), baseRules...))
	case *tool.EnginePermissionDecider:
		if decider == nil {
			return r.Permissions
		}
		baseRules := decider.Engine.Rules()
		baseRules = append(baseRules, rules...)
		return tool.NewEnginePermissionDecider(permissions.NewEngine(decider.Engine.Context(), baseRules...))
	default:
		return r.Permissions
	}
}

func commandPermissionRules(allowedTools []string) []permissions.Rule {
	var rules []permissions.Rule
	for _, raw := range allowedTools {
		rule, err := permissions.ParseRule(contracts.PermissionSourceCommand, contracts.PermissionAllow, raw)
		if err != nil {
			continue
		}
		rules = append(rules, rule)
	}
	return rules
}

func ToolUses(message contracts.Message) []contracts.ToolUse {
	var out []contracts.ToolUse
	for _, block := range message.Content {
		if block.Type != contracts.ContentToolUse {
			continue
		}
		id := contracts.ID(block.ID)
		if id == "" {
			id = contracts.NewID()
		}
		out = append(out, contracts.ToolUse{
			ID:    id,
			Name:  block.Name,
			Input: normalizeInput(block.Input),
		})
	}
	return out
}

func messageFromResponse(sessionID contracts.ID, response *anthropic.Response) contracts.Message {
	message := contracts.Message{
		ID:        response.ID,
		Type:      contracts.MessageAssistant,
		UUID:      contracts.NewID(),
		SessionID: sessionID,
		Model:     response.Model,
		Content:   response.Content,
		Usage:     &response.Usage,
		Raw: map[string]any{
			"id":            response.ID,
			"type":          response.Type,
			"stop_reason":   response.StopReason,
			"stop_sequence": response.StopSequence,
		},
	}
	return message
}

func appendMessage(history []contracts.Message, message contracts.Message) ([]contracts.Message, contracts.Message) {
	next := append([]contracts.Message(nil), history...)
	if message.UUID == "" {
		message.UUID = contracts.NewID()
	}
	if len(history) == 0 {
		return []contracts.Message{message}, message
	}
	last := next[len(next)-1]
	if last.UUID == "" {
		next[len(next)-1].UUID = contracts.NewID()
		last = next[len(next)-1]
	}
	parent := last.UUID
	message.ParentUUID = &parent
	next = append(next, message)
	return next, message
}

func (r Runner) appendTranscript(message contracts.Message) error {
	if r.SessionPath == "" {
		return nil
	}
	return session.Append(r.SessionPath, session.EntryFromMessage(r.SessionID, message))
}

func normalizeInput(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func isFallbackEligible(err error) bool {
	var apiErr anthropic.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}
	return false
}
