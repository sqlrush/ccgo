package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ccgo/internal/api/anthropic"
	bridgepkg "ccgo/internal/bridge"
	"ccgo/internal/commands"
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/config"
	"ccgo/internal/contracts"
	daemonpkg "ccgo/internal/daemon"
	integrationspkg "ccgo/internal/integrations"
	lsppkg "ccgo/internal/lsp"
	"ccgo/internal/mcp"
	"ccgo/internal/memory"
	msgs "ccgo/internal/messages"
	modelpkg "ccgo/internal/model"
	nativepkg "ccgo/internal/native"
	"ccgo/internal/outputstyles"
	"ccgo/internal/permissions"
	pluginpkg "ccgo/internal/plugins"
	"ccgo/internal/session"
	"ccgo/internal/skills"
	telemetrypkg "ccgo/internal/telemetry"
	"ccgo/internal/tool"
	tasktools "ccgo/internal/tools/task"
)

func (r *Runner) RunTurn(ctx context.Context, history []contracts.Message, user contracts.Message) (Result, error) {
	if r == nil {
		return Result{}, fmt.Errorf("conversation runner is nil")
	}
	if r.Client == nil {
		return Result{}, fmt.Errorf("conversation runner missing client")
	}
	r.maybeWriteBridgeManifest()
	r.maybeWriteNativeManifest()
	r.maybeWriteIntegrationsManifest()
	r.maybeWriteLSPManagerStatus()
	r.maybeStartLSPServers(ctx)
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
		if localResult != nil && localResult.Type == commands.LocalCommandResultNative {
			return r.appendLocalTextResult(result, history, r.formatNativeCommandSummary(ctx, localResult.Value))
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
	runner, err = runner.withAdvancedTools()
	if err != nil {
		return result, err
	}
	runner.maybeRunDueSchedules(ctx)
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
	settings := r.mergedSettings()
	metadata := map[string]any{
		tool.MetadataSettingsKey: settings,
	}
	if r.SessionPath != "" {
		metadata[tool.MetadataSessionPathKey] = r.SessionPath
	}
	if agents := r.toolAvailableAgents(settings); len(agents) > 0 {
		metadata[tool.MetadataAvailableAgentsKey] = agents
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

func (r Runner) toolAvailableAgents(settings contracts.Settings) []tool.AgentInfo {
	if r.WorkingDirectory == "" {
		return nil
	}
	plugins := pluginpkg.LoadPluginDirsWithSettings(pluginpkg.ProjectPluginDirs(r.WorkingDirectory), settings)
	var agents []tool.AgentInfo
	for _, plugin := range plugins {
		for _, agent := range plugin.Agents {
			name := strings.TrimSpace(agent.Name)
			if name == "" {
				continue
			}
			agents = append(agents, tool.AgentInfo{
				Name:           name,
				Description:    strings.TrimSpace(agent.Description),
				Path:           agent.Path,
				Prompt:         strings.TrimSpace(agent.Prompt),
				Model:          strings.TrimSpace(agent.Model),
				PermissionMode: agent.PermissionMode,
				AllowedTools:   append([]string(nil), agent.AllowedTools...),
			})
		}
	}
	sort.SliceStable(agents, func(i, j int) bool {
		if agents[i].Name == agents[j].Name {
			return agents[i].Path < agents[j].Path
		}
		return agents[i].Name < agents[j].Name
	})
	return agents
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

func (r Runner) maybeRunDueSchedules(ctx context.Context) {
	if r.SessionID == "" || strings.TrimSpace(r.SessionPath) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	const toolUseID contracts.ID = "schedule_due_tick"
	progressSink := tool.ProgressFunc(func(progress contracts.ToolProgress) error {
		if isNoopScheduleDueProgress(progress) {
			return nil
		}
		progressCopy := progress
		if progressCopy.ToolUseID == "" {
			progressCopy.ToolUseID = toolUseID
		}
		r.emit(Event{Type: EventToolProgress, ToolProgress: &progressCopy})
		return nil
	})
	_, err := tasktools.RunDueSchedules(tool.Context{
		Context:          ctx,
		WorkingDirectory: r.WorkingDirectory,
		SessionID:        r.SessionID,
		Metadata: map[string]any{
			tool.MetadataSessionPathKey: r.SessionPath,
		},
	}, "", time.Now().UTC(), progressSink)
	if err == nil {
		return
	}
	r.emit(Event{Type: EventToolProgress, ToolProgress: &contracts.ToolProgress{
		ToolUseID: toolUseID,
		Type:      "schedule_due_error",
		Data: map[string]any{
			"error": err.Error(),
		},
	}})
}

func isNoopScheduleDueProgress(progress contracts.ToolProgress) bool {
	if progress.Type != "schedule_due_run" {
		return false
	}
	return progressDataInt(progress.Data, "due_count") == 0 &&
		progressDataInt(progress.Data, "triggered_count") == 0 &&
		progressDataInt(progress.Data, "error_count") == 0
}

func progressDataInt(data map[string]any, key string) int {
	if data == nil {
		return 0
	}
	switch value := data[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
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
				return "Usage: /status " + args[0] + " <session|model|auth|tools|mcp|plugins|telemetry|bridge|lsp|native|integrations>"
			}
			return r.formatStatusShow(args[1])
		case "session", "model", "auth", "tools", "mcp", "plugins", "telemetry", "bridge", "lsp", "native":
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
	case "telemetry":
		return r.formatStatusTelemetry()
	case "bridge":
		return r.formatStatusBridge()
	case "daemon":
		return r.formatStatusDaemon()
	case "lsp":
		return r.formatStatusLSP()
	case "native":
		return r.formatStatusNative()
	case "integrations":
		return r.formatStatusIntegrations()
	default:
		return "Unknown status section " + strings.TrimSpace(raw) + ". Available sections: session, model, auth, tools, mcp, plugins, telemetry, bridge, daemon, lsp, native, integrations"
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
	case "telemetry", "telemetry-events", "trace", "tracing":
		return "telemetry"
	case "bridge", "repl-bridge", "remote-control", "control":
		return "bridge"
	case "lsp", "language-server", "language-servers", "diagnostic", "diagnostics":
		return "lsp"
	case "native", "native-integration", "native-integrations", "platform":
		return "native"
	case "integration", "integrations", "advanced-integration", "advanced-integrations", "chrome", "voice", "computer-use", "computeruse":
		return "integrations"
	default:
		return compact
	}
}

func (r Runner) maybeWriteIntegrationsManifest() {
	settings := r.mergedSettings()
	if settings.Advanced == nil || !integrationspkg.AnyEnabled(settings.Advanced) {
		return
	}
	path := integrationspkg.SessionManifestPath(r.SessionPath, r.SessionID)
	if path == "" {
		return
	}
	manifest := integrationspkg.BuildManifest(r.SessionID, r.WorkingDirectory, settings.Advanced)
	_ = integrationspkg.WriteManifest(path, manifest)
	for _, integration := range manifest.Integrations {
		statePath := integrationspkg.SessionRuntimeStatePath(r.SessionPath, r.SessionID, integration.Name)
		if statePath == "" {
			continue
		}
		runtimeState := integrationspkg.BuildRuntimeState(r.SessionPath, r.SessionID, r.WorkingDirectory, integration)
		if integration.Enabled && strings.TrimSpace(integration.Name) == "chrome" {
			chromeManifestPath, chromeWrapperPath, err := r.writeChromeNativeHostArtifacts()
			if err == nil && chromeManifestPath != "" {
				if runtimeState.Artifacts == nil {
					runtimeState.Artifacts = map[string]string{}
				}
				runtimeState.Artifacts["chrome_native_host_manifest"] = chromeManifestPath
				if strings.TrimSpace(chromeWrapperPath) != "" {
					runtimeState.Artifacts["chrome_native_host_wrapper"] = chromeWrapperPath
				}
			}
		}
		if integration.Enabled && strings.TrimSpace(integration.Name) == "voice" {
			voicePlanPath := integrationspkg.VoiceCapturePlanPath(r.SessionPath, r.SessionID)
			if voicePlanPath != "" {
				voicePlan := integrationspkg.BuildVoiceCapturePlan(r.SessionID, r.WorkingDirectory, integration.Adapters)
				_ = integrationspkg.WriteVoiceCapturePlan(voicePlanPath, voicePlan)
				if runtimeState.Artifacts == nil {
					runtimeState.Artifacts = map[string]string{}
				}
				runtimeState.Artifacts["voice_capture_plan"] = voicePlanPath
			}
		}
		if integration.Enabled && strings.TrimSpace(integration.Name) == "computer_use" {
			computerUsePlanPath := integrationspkg.ComputerUseDriverPlanPath(r.SessionPath, r.SessionID)
			if computerUsePlanPath != "" {
				computerUsePlan := integrationspkg.BuildComputerUseDriverPlan(r.SessionID, r.WorkingDirectory, integration.Adapters)
				_ = integrationspkg.WriteComputerUseDriverPlan(computerUsePlanPath, computerUsePlan)
				if runtimeState.Artifacts == nil {
					runtimeState.Artifacts = map[string]string{}
				}
				runtimeState.Artifacts["computer_use_driver_plan"] = computerUsePlanPath
			}
		}
		_ = integrationspkg.WriteRuntimeState(statePath, runtimeState)
	}
}

func (r Runner) writeChromeNativeHostArtifacts() (string, string, error) {
	chromeManifestPath := integrationspkg.ChromeNativeHostManifestPath(r.SessionPath, r.SessionID)
	if chromeManifestPath == "" {
		return "", "", os.ErrInvalid
	}
	hostPath, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	chromeWrapperPath := integrationspkg.ChromeNativeHostWrapperPath(r.SessionPath, r.SessionID)
	chromeManifestHostPath := hostPath
	if chromeWrapperPath != "" {
		if err := integrationspkg.WriteChromeNativeHostWrapper(chromeWrapperPath, hostPath); err != nil {
			return "", "", err
		}
		chromeManifestHostPath = chromeWrapperPath
	}
	chromeManifest := integrationspkg.BuildChromeNativeHostManifest(chromeManifestHostPath, integrationspkg.ChromeAllowedOriginsFromEnv(os.Getenv))
	if err := integrationspkg.WriteChromeNativeHostManifest(chromeManifestPath, chromeManifest); err != nil {
		return "", "", err
	}
	return chromeManifestPath, chromeWrapperPath, nil
}

func (r Runner) maybeWriteNativeManifest() {
	settings := r.mergedSettings()
	if settings.Advanced == nil || !advancedBoolEnabled(settings.Advanced.NativeIntegrations) {
		return
	}
	path := nativepkg.SessionManifestPath(r.SessionPath, r.SessionID)
	if path == "" {
		return
	}
	_ = nativepkg.WriteManifest(path, nativepkg.BuildManifest(r.SessionID, r.WorkingDirectory))
	clipboardPath := nativepkg.SessionClipboardPath(r.SessionPath, r.SessionID)
	if clipboardPath != "" {
		_ = nativepkg.EnsureClipboardState(clipboardPath, r.SessionID)
	}
	indexPath := nativepkg.SessionFileIndexPath(r.SessionPath, r.SessionID)
	if indexPath == "" || strings.TrimSpace(r.WorkingDirectory) == "" {
		return
	}
	if index, err := nativepkg.BuildFileIndex(r.SessionID, r.WorkingDirectory, nativepkg.FileIndexOptions{}); err == nil {
		_ = nativepkg.WriteFileIndex(indexPath, index)
	}
}

func (r Runner) maybeWriteLSPManagerStatus() {
	settings := r.mergedSettings()
	if settings.Advanced == nil || !advancedBoolEnabled(settings.Advanced.LSP) {
		return
	}
	path := lsppkg.SessionManagerStatusPath(r.SessionPath, r.SessionID)
	if path == "" {
		return
	}
	_ = lsppkg.WriteManagerStatus(path, lsppkg.BuildManagerStatus(r.SessionID, r.WorkingDirectory, r.lspServerDefinitions(), nil))
}

func (r *Runner) maybeStartLSPServers(ctx context.Context) {
	settings := r.mergedSettings()
	if settings.Advanced == nil || !advancedBoolEnabled(settings.Advanced.LSP) {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	definitions := r.lspServerDefinitions()
	if len(definitions) == 0 {
		return
	}
	diagnosticsPath := lsppkg.SessionDiagnosticsPath(r.SessionPath, r.SessionID)
	managerPath := lsppkg.SessionManagerStatusPath(r.SessionPath, r.SessionID)
	if diagnosticsPath == "" || managerPath == "" {
		return
	}
	status := lsppkg.BuildManagerStatus(r.SessionID, r.WorkingDirectory, definitions, nil)
	for _, server := range status.Servers {
		if server.RuntimeState != lsppkg.ServerRuntimeNotStarted || r.lspProcessRunning(server.Name) {
			continue
		}
		definition, ok := r.lspDefinitionByName(server.Name)
		if !ok {
			continue
		}
		if _, err := exec.LookPath(definition.Command); err != nil {
			server.RuntimeState = lsppkg.ServerRuntimeNotStarted
			server.Reason = "language server command not found in PATH: " + definition.Command
			status = lsppkg.UpsertServerStatus(status, server)
			_ = lsppkg.WriteManagerStatus(managerPath, status)
			continue
		}
		process, err := lsppkg.StartServerProcess(ctx, lsppkg.ServerProcessOptions{
			SessionID:         r.SessionID,
			Definition:        definition,
			WorkingDirectory:  r.WorkingDirectory,
			SnapshotPath:      diagnosticsPath,
			ManagerStatusPath: managerPath,
		})
		if err != nil {
			server.RuntimeState = lsppkg.ServerRuntimeFailed
			server.Reason = err.Error()
			status = lsppkg.UpsertServerStatus(status, server)
			_ = lsppkg.WriteManagerStatus(managerPath, status)
			continue
		}
		if r.LSPProcesses == nil {
			r.LSPProcesses = map[string]*lsppkg.ServerProcess{}
		}
		r.LSPProcesses[server.Name] = process
		if err := process.InitializeAndOpen(ctx, lsppkg.ServerHandshakeOptions{
			RootURI:       lsppkg.FileURIFromPath(r.WorkingDirectory),
			RootPath:      r.WorkingDirectory,
			ClientName:    "ccgo",
			ClientVersion: "go-rewrite",
			Documents:     r.LSPStartupDocuments,
		}); err != nil {
			server.RuntimeState = lsppkg.ServerRuntimeFailed
			server.Reason = err.Error()
			status = lsppkg.UpsertServerStatus(status, server)
			_ = lsppkg.WriteManagerStatus(managerPath, status)
		}
	}
}

func (r Runner) lspServerDefinitions() []lsppkg.ServerDefinition {
	if len(r.LSPServerDefinitions) > 0 {
		return r.LSPServerDefinitions
	}
	return lsppkg.DefaultServerDefinitions()
}

func (r Runner) lspDefinitionByName(name string) (lsppkg.ServerDefinition, bool) {
	name = strings.TrimSpace(name)
	for _, definition := range r.lspServerDefinitions() {
		if strings.TrimSpace(definition.Name) == name {
			return definition, true
		}
	}
	return lsppkg.ServerDefinition{}, false
}

func (r *Runner) lspProcessRunning(name string) bool {
	if r == nil || r.LSPProcesses == nil {
		return false
	}
	process := r.LSPProcesses[name]
	if process == nil {
		delete(r.LSPProcesses, name)
		return false
	}
	select {
	case <-process.Done():
		delete(r.LSPProcesses, name)
		return false
	default:
		return true
	}
}

func (r Runner) formatStatusNative() string {
	settings := r.mergedSettings()
	enabled := settings.Advanced != nil && advancedBoolEnabled(settings.Advanced.NativeIntegrations)
	path := nativepkg.SessionManifestPath(r.SessionPath, r.SessionID)
	lines := []string{
		"Status native integrations",
		"Enabled: " + boolEnabledText(enabled),
	}
	if path == "" {
		return strings.Join(append(lines, "Manifest path: (not configured)", "Capabilities: 0"), "\n")
	}
	manifest, err := nativepkg.LoadManifest(path)
	if err != nil {
		return strings.Join(append(lines, "Manifest path: "+path, "Native integrations error: "+err.Error()), "\n")
	}
	lines = append(lines,
		"Manifest path: "+path,
		"Platform: "+manifest.GOOS+"/"+manifest.GOARCH,
		fmt.Sprintf("Capabilities: %d", len(manifest.Capabilities)),
		fmt.Sprintf("Available capabilities: %d", nativepkg.CountAvailable(manifest.Capabilities)),
		fmt.Sprintf("Clipboard adapters: %d", nativepkg.CountAvailableClipboardAdapters(manifest.ClipboardAdapters)),
	)
	if len(manifest.ClipboardAdapters) > 0 {
		lines = append(lines, "Clipboard adapter states:")
		for _, adapter := range manifest.ClipboardAdapters {
			state := "unavailable"
			if adapter.Available {
				state = "available"
			}
			line := "- " + adapter.Name + ": " + state
			if adapter.Kind != "" {
				line += " kind=" + adapter.Kind
			}
			if len(adapter.WriteCommand) > 0 {
				line += " write=" + strings.Join(adapter.WriteCommand, " ")
			}
			if len(adapter.ReadCommand) > 0 {
				line += " read=" + strings.Join(adapter.ReadCommand, " ")
			}
			lines = append(lines, line)
		}
	}
	clipboardPath := nativepkg.SessionClipboardPath(r.SessionPath, r.SessionID)
	if clipboardPath != "" {
		clipboard, err := nativepkg.LoadClipboard(clipboardPath)
		if err == nil && clipboard.UpdatedAt != "" {
			lines = append(lines,
				"Clipboard path: "+clipboardPath,
				fmt.Sprintf("Clipboard items: %d", len(clipboard.Items)),
			)
		}
	}
	indexPath := nativepkg.SessionFileIndexPath(r.SessionPath, r.SessionID)
	if indexPath != "" {
		index, err := nativepkg.LoadFileIndex(indexPath)
		if err == nil && index.GeneratedAt != "" {
			lines = append(lines,
				"File index path: "+indexPath,
				fmt.Sprintf("Indexed files: %d", len(index.Files)),
			)
			if index.Truncated {
				lines = append(lines, "File index truncated: yes")
			}
		}
	}
	if manifest.Terminal != "" {
		lines = append(lines, "Terminal: "+manifest.Terminal)
	}
	if manifest.ColorTerminal != "" {
		lines = append(lines, "Color terminal: "+manifest.ColorTerminal)
	}
	if len(manifest.Capabilities) > 0 {
		lines = append(lines, "Capability states:")
		for _, capability := range manifest.Capabilities {
			state := "unavailable"
			if capability.Available {
				state = "available"
			}
			line := "- " + capability.Name + ": " + state
			if capability.Detail != "" {
				line += " (" + capability.Detail + ")"
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func (r Runner) formatNativeCommandSummary(ctx context.Context, raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) == 0 {
		return nativeCommandUsage()
	}
	switch strings.ToLower(args[0]) {
	case "clipboard":
		return r.formatNativeClipboardCommand(ctx, strings.TrimSpace(dropLeadingFields(raw, 1)))
	case "chrome":
		return r.formatNativeChromeCommand(ctx, strings.TrimSpace(dropLeadingFields(raw, 1)))
	case "voice":
		return r.formatNativeVoiceCommand(ctx, strings.TrimSpace(dropLeadingFields(raw, 1)))
	case "computer", "computer-use", "computer_use":
		return r.formatNativeComputerCommand(ctx, strings.TrimSpace(dropLeadingFields(raw, 1)))
	default:
		return "Native command is not implemented in the Go runtime yet: " + strings.Join(args, " ")
	}
}

func (r Runner) formatNativeChromeCommand(ctx context.Context, raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) == 0 {
		return "Usage: /native chrome <status|install [chrome|chromium|edge]>"
	}
	manifestPath, wrapperPath, err := r.writeChromeNativeHostArtifacts()
	if err != nil {
		return "Native Chrome native host\nArtifact error: " + err.Error()
	}
	switch strings.ToLower(args[0]) {
	case "status", "show":
		browser := "chrome"
		if len(args) > 1 {
			browser = args[1]
		}
		targetPath, targetErr := integrationspkg.ChromeNativeHostInstallPath(integrationspkg.ChromeNativeHostName, integrationspkg.ChromeNativeHostInstallOptions{
			Browser:    browser,
			InstallDir: strings.TrimSpace(os.Getenv("CLAUDE_CHROME_NATIVE_HOST_INSTALL_DIR")),
		})
		lines := []string{
			"Native Chrome native host",
			"Manifest path: " + manifestPath,
			"Wrapper path: " + wrapperPath,
		}
		if targetErr != nil {
			lines = append(lines, "Install target error: "+targetErr.Error())
		} else {
			lines = append(lines, "Install target: "+targetPath)
		}
		return strings.Join(lines, "\n")
	case "install":
		browser := "chrome"
		if len(args) > 1 {
			browser = args[1]
		}
		install, err := integrationspkg.InstallChromeNativeHostManifest(ctx, manifestPath, integrationspkg.ChromeNativeHostInstallOptions{
			Browser:           browser,
			InstallDir:        strings.TrimSpace(os.Getenv("CLAUDE_CHROME_NATIVE_HOST_INSTALL_DIR")),
			WrapperSourcePath: wrapperPath,
		})
		lines := []string{
			"Native Chrome native host install",
			"Browser: " + install.Browser,
			"Manifest path: " + manifestPath,
			"Wrapper path: " + wrapperPath,
		}
		if install.TargetPath != "" {
			lines = append(lines, "Installed manifest: "+install.TargetPath)
		}
		if install.WrapperPath != "" {
			lines = append(lines, "Installed wrapper: "+install.WrapperPath)
		}
		if install.Skipped {
			lines = append(lines, "Install: skipped")
		}
		if install.Detail != "" {
			lines = append(lines, "Detail: "+install.Detail)
		}
		if err != nil {
			lines = append(lines, "Install error: "+err.Error())
		}
		return strings.Join(lines, "\n")
	default:
		return "Usage: /native chrome <status|install [chrome|chromium|edge]>"
	}
}

func (r Runner) formatNativeClipboardCommand(ctx context.Context, raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) == 0 {
		return "Usage: /native clipboard <read|write <text>>"
	}
	clipboardPath := nativepkg.SessionClipboardPath(r.SessionPath, r.SessionID)
	if clipboardPath == "" {
		return "Native clipboard\nClipboard path: (not configured)"
	}
	adapters := nativepkg.DetectClipboardAdapters(nativepkg.ClipboardAdapterOptions{})
	switch strings.ToLower(args[0]) {
	case "write", "set", "copy":
		text := strings.TrimSpace(dropLeadingFields(raw, 1))
		if text == "" {
			return "Usage: /native clipboard write <text>"
		}
		state, external, err := nativepkg.WriteClipboardTextWithAdapters(ctx, clipboardPath, r.SessionID, "clipboard", text, adapters, r.NativeClipboardRunner)
		lines := []string{
			"Native clipboard write",
			"Clipboard path: " + clipboardPath,
			fmt.Sprintf("Session clipboard items: %d", len(state.Items)),
			formatNativeClipboardExternalResult(external),
		}
		if err != nil {
			lines = append(lines, "External clipboard error: "+err.Error())
		}
		return strings.Join(lines, "\n")
	case "read", "get", "paste":
		text, found, external, err := nativepkg.ReadClipboardTextWithAdapters(ctx, clipboardPath, "clipboard", adapters, r.NativeClipboardRunner)
		lines := []string{
			"Native clipboard read",
			"Clipboard path: " + clipboardPath,
			formatNativeClipboardExternalResult(external),
		}
		if err != nil {
			lines = append(lines, "External clipboard error: "+err.Error())
			return strings.Join(lines, "\n")
		}
		if !found {
			lines = append(lines, "Text: (empty)")
			return strings.Join(lines, "\n")
		}
		lines = append(lines, "Text: "+text)
		return strings.Join(lines, "\n")
	default:
		return "Usage: /native clipboard <read|write <text>>"
	}
}

func (r Runner) formatNativeVoiceCommand(ctx context.Context, raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) == 0 {
		return nativeVoiceUsage()
	}
	plan := integrationspkg.BuildVoiceCapturePlan(r.SessionID, r.WorkingDirectory, integrationspkg.DetectAdapters("voice", integrationspkg.AdapterOptions{}))
	switch strings.ToLower(args[0]) {
	case "capture":
		capture, err := integrationspkg.CaptureVoiceAudio(ctx, plan, integrationspkg.VoiceCaptureOptions{Runner: r.NativeVoiceRunner})
		lines := formatNativeVoiceCaptureLines("Native voice capture", capture)
		if err != nil {
			lines = append(lines, "Capture error: "+err.Error())
		}
		return strings.Join(lines, "\n")
	case "transcribe", "transcription", "stt":
		capture, captureErr := integrationspkg.CaptureVoiceAudio(ctx, plan, integrationspkg.VoiceCaptureOptions{Runner: r.NativeVoiceRunner})
		lines := formatNativeVoiceCaptureLines("Native voice transcribe", capture)
		if captureErr != nil {
			lines = append(lines, "Capture error: "+captureErr.Error())
			return strings.Join(lines, "\n")
		}
		if capture.Skipped {
			return strings.Join(lines, "\n")
		}
		transcription, err := integrationspkg.TranscribeVoiceAudio(ctx, capture.Audio, integrationspkg.VoiceTranscriptionOptions{
			Command: integrationspkg.VoiceTranscriptionCommandFromEnv(os.Getenv),
			Runner:  r.NativeVoiceTranscribeRunner,
		})
		if transcription.Skipped {
			lines = append(lines, "Transcription: skipped")
		}
		if transcription.Truncated {
			lines = append(lines, "Transcript truncated: yes")
		}
		if transcription.Detail != "" {
			lines = append(lines, "Transcription detail: "+transcription.Detail)
		}
		if transcription.Transcript != "" {
			lines = append(lines, "Transcript: "+transcription.Transcript)
		}
		if err != nil {
			lines = append(lines, "Transcription error: "+err.Error())
		}
		return strings.Join(lines, "\n")
	default:
		return nativeVoiceUsage()
	}
}

func nativeVoiceUsage() string {
	return "Usage: /native voice <capture|transcribe>"
}

func formatNativeVoiceCaptureLines(title string, capture integrationspkg.VoiceCaptureResult) []string {
	lines := []string{
		title,
		"Adapter: " + capture.AdapterName,
		fmt.Sprintf("Audio bytes: %d", capture.Bytes),
		fmt.Sprintf("Sample rate: %d", capture.SampleRateHz),
		fmt.Sprintf("Channels: %d", capture.Channels),
		"Encoding: " + capture.Encoding,
	}
	if capture.Truncated {
		lines = append(lines, "Truncated: yes")
	}
	if capture.Skipped {
		lines = append(lines, "Capture: skipped")
	}
	if capture.Detail != "" {
		lines = append(lines, "Detail: "+capture.Detail)
	}
	return lines
}

func (r Runner) formatNativeComputerCommand(ctx context.Context, raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) == 0 {
		return nativeComputerUsage()
	}
	plan := integrationspkg.BuildComputerUseDriverPlan(r.SessionID, r.WorkingDirectory, integrationspkg.DetectAdapters("computer_use", integrationspkg.AdapterOptions{}))
	switch strings.ToLower(args[0]) {
	case "screenshot", "screen", "capture":
		screenshot, err := integrationspkg.CaptureComputerUseScreenshot(ctx, plan, integrationspkg.ComputerUseExecutionOptions{Runner: r.NativeComputerUseRunner})
		lines := []string{
			"Native computer screenshot",
			"Adapter: " + screenshot.AdapterName,
			"Format: " + screenshot.Format,
			fmt.Sprintf("Image bytes: %d", screenshot.Bytes),
		}
		if screenshot.Truncated {
			lines = append(lines, "Truncated: yes")
		}
		if screenshot.Skipped {
			lines = append(lines, "Capture: skipped")
		}
		if screenshot.Detail != "" {
			lines = append(lines, "Detail: "+screenshot.Detail)
		}
		if err != nil {
			lines = append(lines, "Capture error: "+err.Error())
		}
		return strings.Join(lines, "\n")
	case "move", "mousemove", "mouse-move", "pointer-move", "click", "mouse-click", "left-click", "right-click", "leftclick", "rightclick", "type", "text", "type-text", "key", "keypress", "key-press":
		action, err := parseNativeComputerInputAction(raw)
		if err != nil {
			return err.Error() + "\n" + nativeComputerUsage()
		}
		input, err := integrationspkg.ExecuteComputerUseInput(ctx, plan, action, integrationspkg.ComputerUseExecutionOptions{Runner: r.NativeComputerUseRunner})
		lines := []string{
			"Native computer input",
			"Action: " + input.ActionType,
			"Adapter: " + input.AdapterName,
		}
		if input.Skipped {
			lines = append(lines, "Input: skipped")
		}
		if input.Detail != "" {
			lines = append(lines, "Detail: "+input.Detail)
		}
		if err != nil {
			lines = append(lines, "Input error: "+err.Error())
		}
		return strings.Join(lines, "\n")
	default:
		return nativeComputerUsage()
	}
}

func nativeCommandUsage() string {
	return "Usage: /native <clipboard|chrome|voice|computer>"
}

func nativeComputerUsage() string {
	return "Usage: /native computer <screenshot|move <x> <y>|click [x y] [button]|type <text>|key <key>>"
}

func parseNativeComputerInputAction(raw string) (integrationspkg.ComputerUseInputAction, error) {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) == 0 {
		return integrationspkg.ComputerUseInputAction{}, fmt.Errorf("computer input action is required")
	}
	verb := strings.ToLower(args[0])
	switch verb {
	case "move", "mousemove", "mouse-move", "pointer-move":
		if len(args) != 3 {
			return integrationspkg.ComputerUseInputAction{}, fmt.Errorf("move action requires x and y coordinates")
		}
		x, y, err := parseNativeComputerPosition(args[1], args[2])
		if err != nil {
			return integrationspkg.ComputerUseInputAction{}, err
		}
		return integrationspkg.ComputerUseInputAction{Type: "move", X: x, Y: y, HasPosition: true}, nil
	case "click", "mouse-click", "left-click", "right-click", "leftclick", "rightclick":
		action := integrationspkg.ComputerUseInputAction{Type: "click"}
		if verb == "right-click" || verb == "rightclick" {
			action.Button = 3
		}
		if verb == "left-click" || verb == "leftclick" {
			action.Button = 1
		}
		switch len(args) {
		case 1:
			return action, nil
		case 2:
			button, err := parseNativeComputerButton(args[1])
			if err != nil {
				return integrationspkg.ComputerUseInputAction{}, err
			}
			action.Button = button
			return action, nil
		case 3, 4:
			x, y, err := parseNativeComputerPosition(args[1], args[2])
			if err != nil {
				return integrationspkg.ComputerUseInputAction{}, err
			}
			action.X = x
			action.Y = y
			action.HasPosition = true
			if len(args) == 4 {
				button, err := parseNativeComputerButton(args[3])
				if err != nil {
					return integrationspkg.ComputerUseInputAction{}, err
				}
				action.Button = button
			}
			return action, nil
		default:
			return integrationspkg.ComputerUseInputAction{}, fmt.Errorf("click action accepts optional x y coordinates and button")
		}
	case "type", "text", "type-text":
		text := strings.TrimSpace(dropLeadingFields(raw, 1))
		if text == "" {
			return integrationspkg.ComputerUseInputAction{}, fmt.Errorf("type action requires text")
		}
		return integrationspkg.ComputerUseInputAction{Type: "type", Text: text}, nil
	case "key", "keypress", "key-press":
		key := strings.TrimSpace(dropLeadingFields(raw, 1))
		if key == "" {
			return integrationspkg.ComputerUseInputAction{}, fmt.Errorf("key action requires a key")
		}
		return integrationspkg.ComputerUseInputAction{Type: "key", Key: key}, nil
	default:
		return integrationspkg.ComputerUseInputAction{}, fmt.Errorf("unsupported computer input action %q", args[0])
	}
}

func parseNativeComputerPosition(rawX string, rawY string) (int, int, error) {
	x, err := strconv.Atoi(rawX)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid x coordinate %q", rawX)
	}
	y, err := strconv.Atoi(rawY)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid y coordinate %q", rawY)
	}
	return x, y, nil
}

func parseNativeComputerButton(raw string) (int, error) {
	button, err := strconv.Atoi(raw)
	if err != nil || button <= 0 {
		return 0, fmt.Errorf("invalid mouse button %q", raw)
	}
	return button, nil
}

func formatNativeClipboardExternalResult(result nativepkg.ClipboardCommandResult) string {
	if result.Skipped {
		if result.Detail != "" {
			return "External clipboard: skipped (" + result.Detail + ")"
		}
		return "External clipboard: skipped"
	}
	if !result.External {
		return "External clipboard: none"
	}
	line := "External clipboard: " + result.AdapterName
	if result.AdapterKind != "" {
		line += " kind=" + result.AdapterKind
	}
	if len(result.Command) > 0 {
		line += " command=" + strings.Join(result.Command, " ")
	}
	return line
}

func dropLeadingFields(raw string, count int) string {
	remaining := strings.TrimSpace(raw)
	for i := 0; i < count; i++ {
		fields := strings.Fields(remaining)
		if len(fields) == 0 {
			return ""
		}
		remaining = strings.TrimSpace(strings.TrimPrefix(remaining, fields[0]))
	}
	return remaining
}

func (r Runner) formatStatusIntegrations() string {
	settings := r.mergedSettings()
	enabled := settings.Advanced != nil && integrationspkg.AnyEnabled(settings.Advanced)
	path := integrationspkg.SessionManifestPath(r.SessionPath, r.SessionID)
	lines := []string{
		"Status advanced integrations",
		"Enabled: " + boolEnabledText(enabled),
	}
	if path == "" {
		return strings.Join(append(lines, "Manifest path: (not configured)", "Integrations: 0"), "\n")
	}
	manifest, err := integrationspkg.LoadManifest(path)
	if err != nil {
		return strings.Join(append(lines, "Manifest path: "+path, "Integrations error: "+err.Error()), "\n")
	}
	lines = append(lines,
		"Manifest path: "+path,
		fmt.Sprintf("Integrations: %d", len(manifest.Integrations)),
		fmt.Sprintf("Enabled integrations: %d", integrationspkg.CountEnabled(manifest.Integrations)),
	)
	if manifest.GeneratedAt != "" {
		lines = append(lines, "Generated at: "+manifest.GeneratedAt)
	}
	stateCounts := integrationspkg.CountByRuntimeState(manifest.Integrations)
	if len(stateCounts) > 0 {
		lines = append(lines, "Runtime states:")
		for _, key := range sortedIntMapKeys(stateCounts) {
			lines = append(lines, fmt.Sprintf("- %s: %d", key, stateCounts[key]))
		}
	}
	if len(manifest.Integrations) > 0 {
		lines = append(lines, "Integration states:")
		for _, integration := range manifest.Integrations {
			state := integration.RuntimeState
			if state == "" {
				state = integrationspkg.RuntimeStateDisabled
			}
			line := fmt.Sprintf("- %s: enabled=%s runtime=%s", integration.Name, boolEnabledText(integration.Enabled), state)
			statePath := integrationspkg.SessionRuntimeStatePath(r.SessionPath, r.SessionID, integration.Name)
			if runtimeState, err := integrationspkg.LoadRuntimeState(statePath); err == nil && runtimeState.GeneratedAt != "" {
				line += " state=" + statePath
				if len(runtimeState.Adapters) > 0 {
					line += fmt.Sprintf(" adapters=%d", integrationspkg.CountAvailableAdapters(runtimeState.Adapters))
				}
				if chromeManifestPath := runtimeState.Artifacts["chrome_native_host_manifest"]; chromeManifestPath != "" {
					line += " chrome_manifest=" + chromeManifestPath
				}
				if chromeWrapperPath := runtimeState.Artifacts["chrome_native_host_wrapper"]; chromeWrapperPath != "" {
					line += " chrome_wrapper=" + chromeWrapperPath
				}
				if voicePlanPath := runtimeState.Artifacts["voice_capture_plan"]; voicePlanPath != "" {
					line += " voice_plan=" + voicePlanPath
				}
				if computerUsePlanPath := runtimeState.Artifacts["computer_use_driver_plan"]; computerUsePlanPath != "" {
					line += " computer_use_plan=" + computerUsePlanPath
				}
			} else if len(integration.Adapters) > 0 {
				line += fmt.Sprintf(" adapters=%d", integrationspkg.CountAvailableAdapters(integration.Adapters))
			}
			lines = append(lines, line)
			adapters := integration.Adapters
			if runtimeState, err := integrationspkg.LoadRuntimeState(statePath); err == nil && len(runtimeState.Adapters) > 0 {
				adapters = runtimeState.Adapters
			}
			for _, adapter := range adapters {
				adapterState := "unavailable"
				if adapter.Available {
					adapterState = "available"
				}
				adapterLine := fmt.Sprintf("  - %s/%s: %s", integration.Name, adapter.Name, adapterState)
				if adapter.Kind != "" {
					adapterLine += " kind=" + adapter.Kind
				}
				if len(adapter.Command) > 0 {
					adapterLine += " command=" + strings.Join(adapter.Command, " ")
				}
				lines = append(lines, adapterLine)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (r Runner) formatStatusLSP() string {
	settings := r.mergedSettings()
	enabled := settings.Advanced != nil && advancedBoolEnabled(settings.Advanced.LSP)
	path := lsppkg.SessionDiagnosticsPath(r.SessionPath, r.SessionID)
	managerPath := lsppkg.SessionManagerStatusPath(r.SessionPath, r.SessionID)
	lines := []string{
		"Status LSP",
		"Enabled: " + boolEnabledText(enabled),
	}
	if path == "" {
		return strings.Join(append(lines, "Diagnostics path: (not configured)", "Diagnostics: 0"), "\n")
	}
	diagnostics, err := lsppkg.LoadSnapshot(path)
	if err != nil {
		return strings.Join(append(lines, "Diagnostics path: "+path, "Diagnostics error: "+err.Error()), "\n")
	}
	summary := lsppkg.Summarize(diagnostics)
	lines = append(lines,
		"Diagnostics path: "+path,
		fmt.Sprintf("Diagnostics: %d", summary.Total),
		fmt.Sprintf("Files: %d", summary.Files),
		fmt.Sprintf("Errors: %d", summary.ErrorCount),
		fmt.Sprintf("Warnings: %d", summary.WarningCount),
		fmt.Sprintf("Info: %d", summary.InfoCount),
		fmt.Sprintf("Hints: %d", summary.HintCount),
	)
	if len(summary.BySeverity) > 0 {
		lines = append(lines, "Severities:")
		for _, key := range sortedIntMapKeys(summary.BySeverity) {
			lines = append(lines, fmt.Sprintf("- %s: %d", key, summary.BySeverity[key]))
		}
	}
	if len(summary.BySource) > 0 {
		lines = append(lines, "Sources:")
		for _, key := range sortedIntMapKeys(summary.BySource) {
			lines = append(lines, fmt.Sprintf("- %s: %d", key, summary.BySource[key]))
		}
	}
	if managerPath != "" {
		manager, err := lsppkg.LoadManagerStatus(managerPath)
		if err != nil {
			lines = append(lines, "Manager path: "+managerPath, "Manager error: "+err.Error())
		} else {
			lines = append(lines,
				"Manager path: "+managerPath,
				fmt.Sprintf("Configured LSP servers: %d", len(manager.Servers)),
				fmt.Sprintf("Matched LSP servers: %d", lsppkg.CountMatchedServers(manager.Servers)),
			)
			stateCounts := lsppkg.CountServerRuntimeStates(manager.Servers)
			if len(stateCounts) > 0 {
				lines = append(lines, "Server runtime states:")
				for _, key := range sortedIntMapKeys(stateCounts) {
					lines = append(lines, fmt.Sprintf("- %s: %d", key, stateCounts[key]))
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

func (r Runner) formatStatusBridge() string {
	settings := r.mergedSettings()
	enabled := settings.Advanced != nil && advancedBoolEnabled(settings.Advanced.Bridge)
	path := bridgepkg.SessionManifestPath(r.SessionPath, r.SessionID)
	lines := []string{
		"Status bridge",
		"Enabled: " + boolEnabledText(enabled),
	}
	if path == "" {
		return strings.Join(append(lines, "Manifest path: (not configured)", "Bridge-safe commands: 0"), "\n")
	}
	manifest, err := bridgepkg.LoadManifest(path)
	if err != nil {
		return strings.Join(append(lines, "Manifest path: "+path, "Bridge error: "+err.Error()), "\n")
	}
	lines = append(lines,
		"Manifest path: "+path,
		fmt.Sprintf("Bridge-safe commands: %d", len(manifest.Commands)),
		fmt.Sprintf("Bridge capabilities: %d", len(manifest.Capabilities)),
	)
	if len(manifest.Capabilities) > 0 {
		for _, capability := range manifest.Capabilities {
			lines = append(lines, formatBridgeCapability(capability))
		}
	}
	directPath := bridgepkg.SessionDirectStatePath(r.SessionPath, r.SessionID)
	if directPath != "" {
		state, err := bridgepkg.LoadDirectState(directPath)
		if err == nil && state.GeneratedAt != "" {
			lines = append(lines,
				"Direct connect state: "+state.RuntimeState,
				"Direct connect path: "+directPath,
			)
			if state.URL != "" {
				lines = append(lines, "Direct connect url: "+state.URL)
			}
			if state.WebSocketURL != "" {
				lines = append(lines, "Direct websocket url: "+state.WebSocketURL)
			}
			lines = append(lines, "Direct token required: "+boolEnabledText(state.TokenRequired))
			if state.Error != "" {
				lines = append(lines, "Direct connect error: "+state.Error)
			}
		}
	}
	if manifest.GeneratedAt != "" {
		lines = append(lines, "Generated at: "+manifest.GeneratedAt)
	}
	if len(manifest.Commands) > 0 {
		names := make([]string, 0, len(manifest.Commands))
		for _, command := range manifest.Commands {
			names = append(names, command.Name)
		}
		sort.Strings(names)
		lines = append(lines, "Command names: "+strings.Join(firstStrings(names, 40), ", "))
		if len(names) > 40 {
			lines = append(lines, fmt.Sprintf("Showing 40 of %d bridge-safe commands.", len(names)))
		}
	}
	return strings.Join(lines, "\n")
}

func formatBridgeCapability(capability bridgepkg.Capability) string {
	parts := []string{capability.Name}
	if capability.HTTPPath != "" {
		parts = append(parts, "http "+capability.HTTPPath)
	}
	if capability.WebSocketAction != "" {
		parts = append(parts, "websocket "+capability.WebSocketAction)
	}
	return "- " + strings.Join(parts, ": ")
}

func (r Runner) formatStatusDaemon() string {
	path := daemonpkg.SessionStatePath(r.SessionPath, r.SessionID)
	lines := []string{
		"Status daemon",
	}
	if path == "" {
		return strings.Join(append(lines, "Daemon state path: (not configured)", "Daemon state: disabled"), "\n")
	}
	state, err := daemonpkg.LoadState(path)
	if err != nil {
		return strings.Join(append(lines, "Daemon state path: "+path, "Daemon error: "+err.Error()), "\n")
	}
	lines = append(lines, "Daemon state path: "+path)
	if state.GeneratedAt == "" {
		return strings.Join(append(lines, "Daemon state: disabled"), "\n")
	}
	runtimeState := daemonpkg.RuntimeStateAt(state, time.Now().UTC(), 2*time.Minute)
	lines = append(lines, "Daemon state: "+runtimeState)
	if state.PID > 0 {
		lines = append(lines, fmt.Sprintf("Daemon pid: %d", state.PID))
	}
	if state.Endpoint != "" {
		lines = append(lines, "Daemon endpoint: "+state.Endpoint)
	}
	if state.HeartbeatAt != "" {
		lines = append(lines, "Daemon heartbeat: "+state.HeartbeatAt)
	}
	if state.StartedAt != "" {
		lines = append(lines, "Daemon started: "+state.StartedAt)
	}
	if state.GeneratedAt != "" {
		lines = append(lines, "Generated at: "+state.GeneratedAt)
	}
	if state.Error != "" {
		lines = append(lines, "Daemon error: "+state.Error)
	}
	return strings.Join(lines, "\n")
}

func (r Runner) formatStatusTelemetry() string {
	path := telemetrypkg.SessionPath(r.SessionPath, r.SessionID)
	lines := []string{
		"Status telemetry",
		"Enabled: " + boolEnabledText(r.telemetryEnabled()),
	}
	target := r.telemetryExportTarget()
	if path == "" {
		lines = append(lines, "Telemetry path: (not configured)")
		lines = append(lines, telemetryExporterStatusLines(target)...)
		return strings.Join(append(lines, "Events: 0"), "\n")
	}
	events, err := telemetrypkg.Load(path)
	if err != nil {
		lines = append(lines, "Telemetry path: "+path)
		lines = append(lines, telemetryExporterStatusLines(target)...)
		return strings.Join(append(lines, "Telemetry error: "+err.Error()), "\n")
	}
	summary := telemetrypkg.Summarize(events)
	lines = append(lines,
		"Telemetry path: "+path,
	)
	lines = append(lines, telemetryExporterStatusLines(target)...)
	lines = append(lines,
		fmt.Sprintf("Events: %d", summary.Total),
		fmt.Sprintf("Traces: %d", summary.Traces),
		fmt.Sprintf("Spans: %d", summary.Spans),
		fmt.Sprintf("Tool events: %d", summary.ToolEvents),
		fmt.Sprintf("Tool errors: %d", summary.ToolErrors),
		fmt.Sprintf("Error events: %d", summary.ErrorEvents),
		fmt.Sprintf("Compactions: %d", summary.Compactions),
		fmt.Sprintf("Token warnings: %d", summary.TokenWarnings),
	)
	if len(summary.ByType) > 0 {
		lines = append(lines, "Event types:")
		for _, key := range sortedIntMapKeys(summary.ByType) {
			lines = append(lines, fmt.Sprintf("- %s: %d", key, summary.ByType[key]))
		}
	}
	if len(summary.ByModel) > 0 {
		lines = append(lines, "Models:")
		for _, key := range sortedIntMapKeys(summary.ByModel) {
			lines = append(lines, fmt.Sprintf("- %s: %d", key, summary.ByModel[key]))
		}
	}
	return strings.Join(lines, "\n")
}

func telemetryExporterStatusLines(target telemetrypkg.ExportTarget) []string {
	var lines []string
	if strings.TrimSpace(target.Path) != "" {
		lines = append(lines, "Exporter path: "+strings.TrimSpace(target.Path))
	}
	if strings.TrimSpace(target.URL) != "" {
		lines = append(lines, "Exporter url: "+telemetrypkg.RedactEndpoint(target.URL))
	}
	if len(lines) == 0 {
		lines = append(lines, "Exporter: disabled")
	}
	return lines
}

func (r *Runner) formatConfigSummary(raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) > 0 {
		switch args[0] {
		case "show", "list":
			if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
				return r.formatConfigShow(args[1])
			}
		case "search", "find":
			query := subcommandRemainder(raw, args[0])
			if strings.TrimSpace(query) == "" {
				return "Usage: /config " + args[0] + " <query>"
			}
			return r.formatConfigSearch(query)
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
	case "advanced":
		return formatAdvancedSettings(merged.Advanced)
	default:
		return "Unknown config section " + strings.TrimSpace(raw) + ". Available sections: settings, model, output-style, auth, fast-mode, betas, env, permissions, mcp, hooks, plugins, marketplaces, sandbox, advanced"
	}
}

func (r Runner) formatConfigSearch(query string) string {
	query = strings.TrimSpace(query)
	results := configSearchResults(r, query)
	if len(results) == 0 {
		return "No config matched " + query + "."
	}
	lines := []string{
		"Config search: " + query,
		fmt.Sprintf("Matches: %d", len(results)),
	}
	for _, result := range firstConfigSearchResults(results, 30) {
		lines = append(lines, fmt.Sprintf("- %s: %s", result.Section, result.Match))
	}
	if len(results) > 30 {
		lines = append(lines, fmt.Sprintf("Showing 30 of %d config matches.", len(results)))
	}
	return strings.Join(lines, "\n")
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
	case "advanced", "advance", "adv", "gated", "gates", "feature", "features", "integration", "integrations":
		return "advanced"
	default:
		return compact
	}
}

type configSearchResult struct {
	Section string
	Match   string
}

func configSearchResults(r Runner, query string) []configSearchResult {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	merged := r.mergedSettings()
	seen := map[string]struct{}{}
	var results []configSearchResult
	add := func(section string, label string, values ...string) {
		section = strings.TrimSpace(section)
		label = strings.TrimSpace(label)
		if section == "" || label == "" {
			return
		}
		haystacks := append([]string{section, label}, values...)
		for _, value := range haystacks {
			if !strings.Contains(strings.ToLower(value), query) {
				continue
			}
			key := section + "\x00" + label
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
			results = append(results, configSearchResult{Section: section, Match: label})
			return
		}
	}

	add("settings", "user settings file", config.UserSettingsPath())
	cwd := strings.TrimSpace(r.WorkingDirectory)
	if cwd == "" {
		cwd = "."
	}
	add("settings", "project settings file", config.ProjectSettingsPath(cwd))
	add("settings", "local settings file", config.LocalSettingsPath(cwd))
	add("runtime", "working directory", r.WorkingDirectory)
	add("model", "current model "+r.model(), r.model())
	add("model", "configured model", merged.Model)
	for _, name := range merged.AvailableModels {
		add("model", "available model "+name, name)
	}
	for _, name := range sortedStringMapKeys(merged.ModelOverrides) {
		add("model", "model override "+name, name, merged.ModelOverrides[name])
	}
	add("output-style", "effective output style "+r.effectiveOutputStyleName(), r.effectiveOutputStyleName())
	add("output-style", "configured output style", merged.OutputStyle)
	add("auth", "auth source "+r.authSourceText(), r.authSourceText())
	add("auth", "force login method", merged.ForceLoginMethod)
	add("fast-mode", "runtime fast mode "+boolEnabledText(r.FastMode), boolEnabledText(r.FastMode))
	add("fast-mode", "configured fast mode "+boolPtrEnabledText(merged.FastMode), boolPtrEnabledText(merged.FastMode))
	for _, beta := range r.BetaHeaders {
		add("betas", "beta "+beta, beta)
	}
	for _, name := range sortedStringMapKeys(merged.Env) {
		add("env", "env name "+name, name)
	}
	addConfigPermissionsSearchResults(add, merged.Permissions)
	for _, server := range r.mcpServers() {
		addConfigMCPServerSearchResults(add, server)
	}
	for _, event := range sortedAnyMapKeys(merged.Hooks) {
		add("hooks", "hook event "+event, event)
	}
	add("hooks", "disable all hooks "+boolPtrEnabledText(merged.DisableAllHooks), boolPtrEnabledText(merged.DisableAllHooks))
	add("hooks", "allow managed hooks only "+boolPtrEnabledText(merged.AllowManagedHooksOnly), boolPtrEnabledText(merged.AllowManagedHooksOnly))
	for _, name := range merged.HTTPHookAllowedEnvVars {
		add("hooks", "HTTP hook env name "+name, name)
	}
	for _, name := range pluginEnabledStateLines(merged.EnabledPlugins) {
		add("plugins", "plugin "+name, name)
	}
	for _, name := range pluginConfigNames(merged) {
		add("plugins", "plugin config "+name, name)
	}
	for name, pluginConfig := range merged.PluginConfigs {
		for _, key := range sortedAnyMapKeys(pluginConfig.Options) {
			add("plugins", fmt.Sprintf("%s option key %s", name, key), name, key)
		}
		for _, serverName := range sortedNestedAnyMapKeys(pluginConfig.MCPServers) {
			add("plugins", fmt.Sprintf("%s MCP server config %s", name, serverName), name, serverName)
		}
	}
	for name, legacy := range merged.Plugins {
		for _, key := range legacyPluginSettingKeys(legacy) {
			add("plugins", fmt.Sprintf("%s legacy setting %s", name, key), name, key)
		}
	}
	for _, name := range sortedAnyMapKeys(merged.ExtraKnownMarketplaces) {
		add("marketplaces", "extra marketplace "+name, name)
	}
	for _, name := range pluginAnyListLabels(merged.StrictKnownMarketplaces) {
		add("marketplaces", "strict marketplace "+name, name)
	}
	for _, name := range pluginAnyListLabels(merged.BlockedMarketplaces) {
		add("marketplaces", "blocked marketplace "+name, name)
	}
	for _, key := range sortedAnyMapKeys(merged.Sandbox) {
		add("sandbox", "sandbox key "+key, key)
	}
	addConfigAdvancedSearchResults(add, merged.Advanced)

	sort.Slice(results, func(i, j int) bool {
		if results[i].Section != results[j].Section {
			return results[i].Section < results[j].Section
		}
		return results[i].Match < results[j].Match
	})
	return results
}

func formatAdvancedSettings(setting *contracts.AdvancedSetting) string {
	return strings.Join([]string{
		"Config advanced integrations",
		"Bridge: " + boolPtrEnabledText(advancedBool(setting, "bridge")),
		"LSP: " + boolPtrEnabledText(advancedBool(setting, "lsp")),
		"Telemetry: " + boolPtrEnabledText(advancedBool(setting, "telemetry")),
		"Chrome: " + boolPtrEnabledText(advancedBool(setting, "chrome")),
		"Voice: " + boolPtrEnabledText(advancedBool(setting, "voice")),
		"Computer use: " + boolPtrEnabledText(advancedBool(setting, "computerUse")),
		"Native integrations: " + boolPtrEnabledText(advancedBool(setting, "nativeIntegrations")),
	}, "\n")
}

func addConfigAdvancedSearchResults(add func(string, string, ...string), setting *contracts.AdvancedSetting) {
	for _, item := range []struct {
		Name  string
		Value *bool
	}{
		{Name: "bridge", Value: advancedBool(setting, "bridge")},
		{Name: "lsp", Value: advancedBool(setting, "lsp")},
		{Name: "telemetry", Value: advancedBool(setting, "telemetry")},
		{Name: "chrome", Value: advancedBool(setting, "chrome")},
		{Name: "voice", Value: advancedBool(setting, "voice")},
		{Name: "computer use", Value: advancedBool(setting, "computerUse")},
		{Name: "native integrations", Value: advancedBool(setting, "nativeIntegrations")},
	} {
		state := boolPtrEnabledText(item.Value)
		add("advanced", item.Name+" "+state, item.Name, state)
	}
}

func advancedBool(setting *contracts.AdvancedSetting, name string) *bool {
	if setting == nil {
		return nil
	}
	switch name {
	case "bridge":
		return setting.Bridge
	case "lsp":
		return setting.LSP
	case "telemetry":
		return setting.Telemetry
	case "chrome":
		return setting.Chrome
	case "voice":
		return setting.Voice
	case "computerUse":
		return setting.ComputerUse
	case "nativeIntegrations":
		return setting.NativeIntegrations
	default:
		return nil
	}
}

func addConfigPermissionsSearchResults(add func(string, string, ...string), permissions *contracts.PermissionsSetting) {
	if permissions == nil {
		return
	}
	if permissions.DefaultMode != "" {
		add("permissions", "default mode "+string(permissions.DefaultMode), string(permissions.DefaultMode))
	}
	for _, rule := range permissions.Allow {
		add("permissions", "allow rule "+rule, rule)
	}
	for _, rule := range permissions.Deny {
		add("permissions", "deny rule "+rule, rule)
	}
	for _, rule := range permissions.Ask {
		add("permissions", "ask rule "+rule, rule)
	}
	for _, dir := range permissions.AdditionalDirectories {
		add("permissions", "additional directory", dir)
	}
	add("permissions", "disable bypass mode "+configuredValueText(permissions.DisableBypassMode), configuredValueText(permissions.DisableBypassMode))
	add("permissions", "disable auto mode "+configuredValueText(permissions.DisableAutoMode), configuredValueText(permissions.DisableAutoMode))
}

func addConfigMCPServerSearchResults(add func(string, string, ...string), server mcpServerSummary) {
	config := server.Config
	name := server.Name
	add("mcp", "MCP server "+name, name, config.Name, config.ID, config.IDEName)
	add("mcp", fmt.Sprintf("%s transport %s", name, mcpServerTransport(config)), mcpServerTransport(config))
	add("mcp", fmt.Sprintf("%s source %s", name, mcpServerSource(config)), mcpServerSource(config), config.Scope, config.PluginSource)
	if mcpServerTarget(config) != "(no target)" {
		add("mcp", name+" target configured", name, "target")
	}
	for _, key := range sortedStringMapKeys(config.Env) {
		add("mcp", fmt.Sprintf("%s env name %s", name, key), name, key)
	}
	for _, key := range sortedStringMapKeys(config.Headers) {
		add("mcp", fmt.Sprintf("%s header name %s", name, key), name, key)
	}
	if strings.TrimSpace(config.HeadersHelper) != "" {
		add("mcp", name+" headers helper configured", name, "headers helper")
	}
	if strings.TrimSpace(config.AuthToken) != "" {
		add("mcp", name+" auth token configured", name, "auth token")
	}
	if config.OAuth != nil {
		add("mcp", name+" OAuth configured", name, "oauth")
	}
	if !server.Policy.Allowed {
		add("mcp", fmt.Sprintf("%s policy %s", name, server.Policy.Reason), name, "blocked", server.Policy.Reason)
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

func sortedIntMapKeys(values map[string]int) []string {
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

func firstMCPServerSearchResults(values []mcpServerSearchResult, limit int) []mcpServerSearchResult {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func firstConfigSearchResults(values []configSearchResult, limit int) []configSearchResult {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func firstModelSearchResults(values []modelSearchResult, limit int) []modelSearchResult {
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
	args := strings.Fields(raw)
	switch strings.ToLower(args[0]) {
	case "list", "status", "available", "models":
		return formatModelList(r.model())
	case "search", "find":
		query := subcommandRemainder(raw, args[0])
		if strings.TrimSpace(query) == "" {
			return "Usage: /model " + args[0] + " <query>"
		}
		return formatModelSearch(r.model(), query)
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

type modelSearchResult struct {
	Capability modelpkg.Capability
	Aliases    []string
}

func formatModelSearch(current string, query string) string {
	query = strings.TrimSpace(query)
	results := modelSearchResults(query)
	if len(results) == 0 {
		return "No models matched " + query + "."
	}
	lines := []string{
		"Model search: " + query,
		fmt.Sprintf("Matches: %d", len(results)),
		"Current model: " + current,
	}
	for _, result := range firstModelSearchResults(results, 20) {
		capability := result.Capability
		display := strings.TrimSpace(capability.DisplayName)
		if display == "" {
			display = capability.Name
		}
		parts := []string{
			fmt.Sprintf("context %d", capability.ContextWindowTokens),
			fmt.Sprintf("max output %d", capability.MaxOutputTokens),
		}
		if len(result.Aliases) > 0 {
			parts = append(parts, "aliases: "+strings.Join(result.Aliases, ", "))
		}
		if flags := modelCapabilityFlags(capability); flags != "" {
			parts = append(parts, flags)
		}
		if capability.Name == current {
			parts = append(parts, "current")
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (%s)", display, capability.Name, strings.Join(parts, "; ")))
	}
	if len(results) > 20 {
		lines = append(lines, fmt.Sprintf("Showing 20 of %d model matches.", len(results)))
	}
	return strings.Join(lines, "\n")
}

func modelSearchResults(query string) []modelSearchResult {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	registry := modelpkg.DefaultRegistry()
	aliasesByModel := map[string][]string{}
	for alias, target := range registry.Aliases {
		aliasesByModel[target] = append(aliasesByModel[target], alias)
	}
	for target := range aliasesByModel {
		sort.Strings(aliasesByModel[target])
	}
	names := make([]string, 0, len(registry.Models))
	for name := range registry.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	var results []modelSearchResult
	for _, name := range names {
		capability := registry.Models[name]
		aliases := aliasesByModel[name]
		values := []string{
			capability.Name,
			capability.CanonicalName,
			capability.DisplayName,
			modelCapabilityFlags(capability),
			strings.Join(aliases, " "),
		}
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), query) {
				results = append(results, modelSearchResult{Capability: capability, Aliases: aliases})
				break
			}
		}
	}
	return results
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
		case "search", "find":
			query := subcommandRemainder(raw, args[0])
			if strings.TrimSpace(query) == "" {
				return "Usage: /mcp " + args[0] + " <query>"
			}
			return r.formatMCPServerSearch(query)
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

func (r Runner) formatMCPServerSearch(query string) string {
	query = strings.TrimSpace(query)
	results := mcpServerSearchResults(r.mcpServers(), query)
	if len(results) == 0 {
		return "No MCP servers matched " + query + "."
	}
	lines := []string{
		"MCP search: " + query,
		fmt.Sprintf("Matches: %d", len(results)),
	}
	for _, result := range firstMCPServerSearchResults(results, 20) {
		status := "configured"
		if !result.Server.Policy.Allowed {
			status = "blocked: " + result.Server.Policy.Reason
		}
		lines = append(lines, fmt.Sprintf(
			"- %s (%s, %s, %s): %s",
			result.Server.Name,
			mcpServerTransport(result.Server.Config),
			status,
			mcpServerSource(result.Server.Config),
			result.Match,
		))
	}
	if len(results) > 20 {
		lines = append(lines, fmt.Sprintf("Showing 20 of %d MCP server matches.", len(results)))
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

type mcpServerSearchResult struct {
	Server mcpServerSummary
	Match  string
}

func mcpServerSearchResults(servers []mcpServerSummary, query string) []mcpServerSearchResult {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	var results []mcpServerSearchResult
	for _, server := range servers {
		for _, match := range mcpServerSearchMatches(server, query) {
			results = append(results, mcpServerSearchResult{Server: server, Match: match})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Server.Name != results[j].Server.Name {
			return results[i].Server.Name < results[j].Server.Name
		}
		return results[i].Match < results[j].Match
	})
	return results
}

func mcpServerSearchMatches(server mcpServerSummary, query string) []string {
	config := server.Config
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
	add("server metadata", server.Name, config.Name, config.ID, config.IDEName, config.Scope, config.PluginSource)
	add("transport "+mcpServerTransport(config), mcpServerTransport(config))
	add("source "+mcpServerSource(config), mcpServerSource(config))
	add("target "+mcpServerTarget(config), mcpServerTarget(config), config.Command, strings.Join(config.Args, " "), config.URL)
	for _, key := range sortedStringMapKeys(config.Env) {
		add("env "+key, key)
	}
	for _, key := range sortedStringMapKeys(config.Headers) {
		add("header "+key, key)
	}
	if strings.TrimSpace(config.HeadersHelper) != "" {
		add("headers helper configured", "headers helper", config.HeadersHelper)
	}
	if strings.TrimSpace(config.AuthToken) != "" {
		add("auth token configured", "auth token")
	}
	if config.OAuth != nil {
		add("OAuth configured", "oauth", config.OAuth.ClientID, config.OAuth.AuthServerMetadataURL)
	}
	if !server.Policy.Allowed {
		add("policy "+server.Policy.Reason, "blocked", server.Policy.Reason)
	}
	sort.Strings(matches)
	return matches
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
		case "search", "find":
			query = subcommandRemainder(query, args[0])
			if strings.TrimSpace(query) == "" {
				return "Usage: /resume " + args[0] + " <query>", nil
			}
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
	progressSink := tool.ProgressFunc(func(progress contracts.ToolProgress) error {
		progressCopy := progress
		r.emit(Event{Type: EventToolProgress, ToolProgress: &progressCopy})
		return nil
	})
	for update := range tool.RunTools(toolCtx, r.Tools, uses, progressSink, tool.RunOptions{}) {
		use := update.ToolUse
		result := update.Result
		err := update.Err
		if err != nil && result.Content == nil {
			result = tool.ErrorResult(use, err)
		}
		r.maybeRunTaskSubagent(ctx, use, &result)
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
