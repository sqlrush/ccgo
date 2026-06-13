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
			return r.appendLocalTextResult(result, history, formatCostSummary(originalHistory))
		}
		if localResult != nil && localResult.Type == commands.LocalCommandResultStatus {
			return r.appendLocalTextResult(result, history, r.formatStatusSummary())
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
	registry := commands.Load(commands.Options{CWD: r.WorkingDirectory})
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
	metadata := map[string]any{}
	skillDirs := append([]string(nil), r.SkillDirs...)
	if r.WorkingDirectory != "" {
		skillDirs = appendUniqueStrings(skillDirs, skills.ProjectSkillDirs(r.WorkingDirectory)...)
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

func formatCostSummary(history []contracts.Message) string {
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

func (r Runner) formatStatusSummary() string {
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
		"Status\nSession ID: %s\nWorking directory: %s\nModel: %s\nTools: %d\nMCP servers: %s",
		sessionID,
		cwd,
		model,
		toolCount,
		mcpText,
	)
}

func (r Runner) formatConfigSummary(raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) > 0 && args[0] != "show" && args[0] != "list" {
		return "Config subcommand is not implemented in the Go runtime yet: " + strings.Join(args, " ")
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
		"- permission rules: " + permissionsText,
		fmt.Sprintf("- hooks: %d", len(merged.Hooks)),
		fmt.Sprintf("- enabled plugins: %d", len(merged.EnabledPlugins)),
	}
	return strings.Join(lines, "\n")
}

func (r Runner) formatPluginSummary(raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) > 0 && args[0] != "list" && args[0] != "status" {
		return "Plugin subcommand is not implemented in the Go runtime yet: " + strings.Join(args, " ")
	}
	merged := r.mergedSettings()
	registry := commands.Load(commands.Options{CWD: r.WorkingDirectory})
	localPlugins := pluginpkg.LoadPluginDirs(pluginpkg.ProjectPluginDirs(r.WorkingDirectory))
	pluginSkills := pluginSkillNames(localPlugins)
	pluginCommands := pluginCommandNames(registry.Visible(), pluginSkills)
	pluginAgents := pluginAgentNames(localPlugins)
	pluginMCPServers := pluginMCPServerNames(localPlugins)
	pluginOutputStyles := pluginOutputStyleNames(localPlugins)
	pluginHookEvents := pluginHookEventLines(localPlugins)
	totalPluginHooks := pluginHookCount(localPlugins)
	lines := []string{
		"Plugins",
		fmt.Sprintf("Enabled plugins: %d", len(merged.EnabledPlugins)),
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

func (r Runner) formatMemorySummary(raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) > 0 && args[0] != "list" && args[0] != "status" {
		return "Memory subcommand is not implemented in the Go runtime yet: " + strings.Join(args, " ")
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

func (r Runner) mergedSettings() contracts.Settings {
	if r.MCP == nil {
		return contracts.Settings{}
	}
	return config.MergeSettings(r.MCP.UserSettings, r.MCP.ProjectSettings, r.MCP.LocalSettings)
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

func firstLoadedPlugins(values []pluginpkg.LoadedPlugin, limit int) []pluginpkg.LoadedPlugin {
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

func boolEnabledText(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
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
	if capability, ok := modelpkg.DefaultRegistry().Resolve(raw); ok {
		r.Model = capability.Name
		display := strings.TrimSpace(capability.DisplayName)
		if display == "" {
			display = capability.Name
		}
		return fmt.Sprintf("Selected model: %s\nDisplay name: %s", capability.Name, display)
	}
	r.Model = raw
	return "Selected model: " + raw
}

func (r Runner) formatMCPCommandSummary(raw string) string {
	args := strings.Fields(strings.TrimSpace(raw))
	if len(args) > 0 && args[0] != "list" {
		return "MCP subcommand is not implemented in the Go runtime yet: " + strings.Join(args, " ")
	}
	servers := r.mcpServers()
	if len(servers) == 0 {
		return "No MCP servers configured."
	}
	var lines []string
	lines = append(lines, "MCP servers:")
	for _, server := range servers {
		lines = append(lines, fmt.Sprintf("- %s (%s): %s", server.Name, mcpServerTransport(server.Config), mcpServerTarget(server.Config)))
	}
	return strings.Join(lines, "\n")
}

type mcpServerSummary struct {
	Name   string
	Config contracts.MCPServer
}

func (r Runner) mcpServers() []mcpServerSummary {
	if r.MCP == nil {
		return nil
	}
	merged := config.MergeSettings(r.MCP.UserSettings, r.MCP.ProjectSettings, r.MCP.LocalSettings)
	mcpServers := mcp.MergeServers(merged.MCPServers, r.MCP.PluginServers)
	servers := make([]mcpServerSummary, 0, len(mcpServers))
	for name, server := range mcpServers {
		servers = append(servers, mcpServerSummary{Name: name, Config: server})
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

func (r Runner) formatResumeSummary(raw string) (string, error) {
	query := strings.TrimSpace(raw)
	cwd := r.WorkingDirectory
	if cwd == "" {
		cwd = "."
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
