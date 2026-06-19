package conversation

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	"ccgo/internal/memory"
	msgs "ccgo/internal/messages"
	"ccgo/internal/model"
	"ccgo/internal/session"
	"ccgo/internal/tool"
)

const (
	defaultAutoToolSearchPercentage = 10
	toolSearchCharsPerToken         = 2.5
	modelContextWindowDefault       = 200000
	toolTokenCountOverhead          = 500
)

func (r Runner) BuildRequest(history []contracts.Message, model string) (anthropic.Request, error) {
	return r.buildRequest(context.Background(), history, model, relevantMemoryRequestContext{})
}

type relevantMemoryRequestContext struct {
	Prefetch *memory.RelevantMemoryPrefetchResult
	SkipSync bool
}

func (r Runner) buildRequest(ctx context.Context, history []contracts.Message, model string, relevantMemory relevantMemoryRequestContext) (anthropic.Request, error) {
	history, err := r.applySessionMemoryRecall(history)
	if err != nil {
		return anthropic.Request{}, err
	}
	if relevantMemory.Prefetch != nil {
		history = appendRelevantMemoryPrefetch(history, *relevantMemory.Prefetch)
	} else if !relevantMemory.SkipSync {
		history, err = r.applyRelevantMemoryAttachments(history)
		if err != nil {
			return anthropic.Request{}, err
		}
	}
	history = memory.ExpandRelevantMemoryAttachments(history, time.Time{})
	var definitions []contracts.ToolDefinition
	var deferredToolNames []string
	toolSearchActive := false
	if r.Tools.Registry != nil {
		defs, err := r.Tools.Registry.Definitions(toolPromptContext(r))
		if err != nil {
			return anthropic.Request{}, err
		}
		decision := toolSearchDecisionForRequest(model, defs, r.deferredToolTokenCounter(ctx, model))
		r.emitToolSearchModeDecision(decision)
		definitions, deferredToolNames, toolSearchActive = filterToolSearchDefinitionsWithDecision(defs, history, decision.Enabled)
	}
	apiMessages := msgs.NormalizeForAPI(history)
	if !toolSearchActive {
		apiMessages = stripToolReferenceBlocksFromAPIMessages(apiMessages)
	}
	if len(deferredToolNames) > 0 && !r.deferredToolsDeltaEnabled() {
		apiMessages = prependAvailableDeferredToolsMessage(apiMessages, deferredToolNames)
	}
	request := anthropic.Request{
		Model:     model,
		MaxTokens: r.maxTokens(),
		Messages:  apiMessages,
	}
	if system := r.systemPromptWithOutputStyle(); system != "" {
		request.System = system
	}
	if len(definitions) > 0 {
		request.Tools = anthropic.ToolsFromContracts(definitions)
	}
	return request, nil
}

type deferredToolTokenCounter func([]contracts.ToolDefinition) (int, bool)

type deferredToolTokenCountCacheEntry struct {
	Tokens int
	OK     bool
}

var deferredToolTokenCountCache sync.Map

func filterToolSearchDefinitions(definitions []contracts.ToolDefinition, history []contracts.Message, model string, tokenCounter deferredToolTokenCounter) ([]contracts.ToolDefinition, []string, bool) {
	return filterToolSearchDefinitionsWithDecision(definitions, history, toolSearchDecisionForRequest(model, definitions, tokenCounter).Enabled)
}

func filterToolSearchDefinitionsWithDecision(definitions []contracts.ToolDefinition, history []contracts.Message, toolSearchEnabled bool) ([]contracts.ToolDefinition, []string, bool) {
	if len(definitions) == 0 {
		return definitions, nil, false
	}
	if !hasToolSearchDefinition(definitions) {
		return applyDiscoveredToolReferences(definitions, history), nil, false
	}
	if !toolSearchEnabled {
		return loadAllDeferredTools(withoutToolSearchDefinition(definitions)), nil, false
	}
	deferredNames := deferredToolNames(definitions)
	if len(deferredNames) == 0 {
		return withoutToolSearchDefinition(definitions), nil, false
	}
	discovered := discoveredToolReferenceNames(history)
	out := make([]contracts.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if isToolSearchDefinition(definition) || !toolDefinitionDeferred(definition) {
			out = append(out, definition)
			continue
		}
		if toolDefinitionDiscovered(definition, discovered) {
			definition.AlwaysLoad = true
			definition.ShouldDefer = false
			out = append(out, definition)
		}
	}
	return out, deferredNames, true
}

type toolSearchMode string

const (
	toolSearchModeTST      toolSearchMode = "tst"
	toolSearchModeTSTAuto  toolSearchMode = "tst-auto"
	toolSearchModeStandard toolSearchMode = "standard"
)

type toolSearchDecision struct {
	Enabled                      bool
	Mode                         toolSearchMode
	Reason                       string
	CheckedModel                 string
	MCPToolCount                 int
	UserType                     string
	DeferredToolTokens           int
	Threshold                    int
	DeferredToolDescriptionChars int
	CharThreshold                int
}

func toolSearchEnabledForRequest(model string, definitions []contracts.ToolDefinition, tokenCounter deferredToolTokenCounter) bool {
	return toolSearchDecisionForRequest(model, definitions, tokenCounter).Enabled
}

func toolSearchDecisionForRequest(model string, definitions []contracts.ToolDefinition, tokenCounter deferredToolTokenCounter) toolSearchDecision {
	mode := toolSearchModeFromEnv()
	decision := toolSearchDecision{
		Mode:         mode,
		CheckedModel: model,
		MCPToolCount: countMCPToolDefinitions(definitions),
		UserType:     toolSearchUserType(),
	}
	if !modelSupportsToolReference(model) {
		decision.Mode = toolSearchModeStandard
		decision.Reason = "model_unsupported"
		return decision
	}
	if !hasToolSearchDefinition(definitions) {
		decision.Mode = toolSearchModeStandard
		decision.Reason = "mcp_search_unavailable"
		return decision
	}
	if mode == toolSearchModeStandard {
		decision.Reason = "standard_mode"
		return decision
	}
	if os.Getenv("ENABLE_TOOL_SEARCH") == "" && !isFirstPartyAnthropicBaseURL(os.Getenv("ANTHROPIC_BASE_URL")) {
		decision.Reason = "non_first_party_base_url"
		return decision
	}
	if mode == toolSearchModeTSTAuto {
		threshold := autoToolSearchTokenThreshold(model)
		if tokenCounter != nil {
			if deferredToolTokens, ok := tokenCounter(definitions); ok {
				decision.DeferredToolTokens = deferredToolTokens
				decision.Threshold = threshold
				decision.Enabled = deferredToolTokens >= threshold
				if decision.Enabled {
					decision.Reason = "auto_above_threshold"
				} else {
					decision.Reason = "auto_below_threshold"
				}
				return decision
			}
		}
		chars := deferredToolDescriptionChars(definitions)
		charThreshold := autoToolSearchCharThreshold(model)
		decision.DeferredToolDescriptionChars = chars
		decision.CharThreshold = charThreshold
		decision.Enabled = chars >= charThreshold
		if decision.Enabled {
			decision.Reason = "auto_above_threshold"
		} else {
			decision.Reason = "auto_below_threshold"
		}
		return decision
	}
	decision.Enabled = true
	decision.Reason = "tst_enabled"
	return decision
}

func countMCPToolDefinitions(definitions []contracts.ToolDefinition) int {
	count := 0
	for _, definition := range definitions {
		if definition.MCP != nil {
			count++
		}
	}
	return count
}

func toolSearchUserType() string {
	if userType := strings.TrimSpace(os.Getenv("USER_TYPE")); userType != "" {
		return userType
	}
	return "external"
}

func (r Runner) emitToolSearchModeDecision(decision toolSearchDecision) {
	r.emit(Event{Type: EventToolSearchDecision, ToolSearchModeDecision: &ToolSearchModeDecision{
		Enabled:                      decision.Enabled,
		Mode:                         string(decision.Mode),
		Reason:                       decision.Reason,
		CheckedModel:                 decision.CheckedModel,
		MCPToolCount:                 decision.MCPToolCount,
		UserType:                     decision.UserType,
		DeferredToolTokens:           decision.DeferredToolTokens,
		Threshold:                    decision.Threshold,
		DeferredToolDescriptionChars: decision.DeferredToolDescriptionChars,
		CharThreshold:                decision.CharThreshold,
	}})
}

func (r Runner) deferredToolTokenCounter(ctx context.Context, modelName string) deferredToolTokenCounter {
	if r.Client == nil {
		return nil
	}
	return func(definitions []contracts.ToolDefinition) (int, bool) {
		tools := countTokenToolDefinitions(definitions)
		if len(tools) == 0 {
			return 0, true
		}
		cacheKey := deferredToolTokenCountCacheKey(definitions)
		if cacheKey != "" {
			if cached, ok := deferredToolTokenCountCache.Load(cacheKey); ok {
				entry := cached.(deferredToolTokenCountCacheEntry)
				return entry.Tokens, entry.OK
			}
		}
		tokens, ok := r.countDeferredToolTokens(ctx, modelName, tools)
		if cacheKey != "" {
			deferredToolTokenCountCache.Store(cacheKey, deferredToolTokenCountCacheEntry{Tokens: tokens, OK: ok})
		}
		return tokens, ok
	}
}

func (r Runner) countDeferredToolTokens(ctx context.Context, modelName string, tools []anthropic.ToolDefinition) (int, bool) {
	if len(tools) == 0 {
		return 0, true
	}
	if counter, ok := r.Client.(TokenCountingMessageClient); ok && counter != nil {
		response, err := counter.CountTokens(ctx, anthropic.CountTokensRequest{
			Model:    modelName,
			Messages: []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("foo")}}},
			Tools:    tools,
		})
		if err == nil && response != nil && response.InputTokens > 0 {
			return normalizeToolTokenCount(response.InputTokens), true
		}
	}
	if tokens, ok := r.countToolTokensViaHaikuFallback(ctx, tools); ok {
		return tokens, true
	}
	return 0, false
}

func deferredToolTokenCountCacheKey(definitions []contracts.ToolDefinition) string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if toolDefinitionDeferred(definition) {
			names = append(names, definition.Name)
		}
	}
	return strings.Join(names, ",")
}

func resetDeferredToolTokenCountCache() {
	deferredToolTokenCountCache = sync.Map{}
}

func (r Runner) countToolTokensViaHaikuFallback(ctx context.Context, tools []anthropic.ToolDefinition) (int, bool) {
	response, err := r.Client.CreateMessage(ctx, anthropic.Request{
		Model:     model.Claude45Haiku,
		MaxTokens: 1,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("count")}}},
		Tools:     tools,
	})
	if err != nil || response == nil {
		return 0, false
	}
	total := response.Usage.InputTokens + response.Usage.CacheCreationInputTokens + response.Usage.CacheReadInputTokens
	if total == 0 {
		return 0, false
	}
	return normalizeToolTokenCount(total), true
}

func normalizeToolTokenCount(inputTokens int) int {
	tokens := inputTokens - toolTokenCountOverhead
	if tokens < 0 {
		return 0
	}
	return tokens
}

func countTokenToolDefinitions(definitions []contracts.ToolDefinition) []anthropic.ToolDefinition {
	deferred := make([]contracts.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if !toolDefinitionDeferred(definition) {
			continue
		}
		definition.AlwaysLoad = true
		definition.ShouldDefer = false
		deferred = append(deferred, definition)
	}
	return anthropic.ToolsFromContracts(deferred)
}

func modelSupportsToolReference(model string) bool {
	return !strings.Contains(strings.ToLower(strings.TrimSpace(model)), "haiku")
}

func toolSearchModeFromEnv() toolSearchMode {
	if session.IsEnvTruthy(os.Getenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS")) {
		return toolSearchModeStandard
	}
	value := os.Getenv("ENABLE_TOOL_SEARCH")
	if percent, ok := autoToolSearchPercent(value); ok {
		if percent == 0 {
			return toolSearchModeTST
		}
		if percent == 100 {
			return toolSearchModeStandard
		}
	}
	if isAutoToolSearchMode(value) {
		return toolSearchModeTSTAuto
	}
	if session.IsEnvTruthy(value) {
		return toolSearchModeTST
	}
	if isEnvDefinedFalsy(value) {
		return toolSearchModeStandard
	}
	return toolSearchModeTST
}

func autoToolSearchPercent(value string) (int, bool) {
	if !strings.HasPrefix(value, "auto:") {
		return 0, false
	}
	percent, ok := parseLeadingInt(value[len("auto:"):])
	if !ok {
		return 0, false
	}
	if percent < 0 {
		return 0, true
	}
	if percent > 100 {
		return 100, true
	}
	return percent, true
}

func parseLeadingInt(value string) (int, bool) {
	value = strings.TrimLeft(value, " \t\r\n")
	if value == "" {
		return 0, false
	}
	sign := 1
	if value[0] == '-' || value[0] == '+' {
		if value[0] == '-' {
			sign = -1
		}
		value = value[1:]
	}
	i := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, false
	}
	parsed := 0
	for j := 0; j < i; j++ {
		if parsed > 100 {
			break
		}
		parsed = parsed*10 + int(value[j]-'0')
	}
	return sign * parsed, true
}

func isAutoToolSearchMode(value string) bool {
	return value == "auto" || strings.HasPrefix(value, "auto:")
}

func autoToolSearchPercentage() int {
	value := os.Getenv("ENABLE_TOOL_SEARCH")
	if value == "auto" {
		return defaultAutoToolSearchPercentage
	}
	if percent, ok := autoToolSearchPercent(value); ok {
		return percent
	}
	return defaultAutoToolSearchPercentage
}

func autoToolSearchCharThreshold(modelName string) int {
	return int(float64(autoToolSearchTokenThreshold(modelName)) * toolSearchCharsPerToken)
}

func autoToolSearchTokenThreshold(modelName string) int {
	return toolSearchContextWindowTokens(modelName) * autoToolSearchPercentage() / 100
}

func toolSearchContextWindowTokens(modelName string) int {
	if os.Getenv("USER_TYPE") == "ant" {
		if override, err := strconv.Atoi(os.Getenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS")); err == nil && override > 0 {
			return override
		}
	}
	lookupName := modelName
	if session.IsEnvTruthy(os.Getenv("CLAUDE_CODE_DISABLE_1M_CONTEXT")) {
		lookupName = trimOneMillionContextSuffix(lookupName)
	}
	if capability, ok := model.DefaultRegistry().Resolve(lookupName); ok && capability.ContextWindowTokens > 0 {
		return capability.ContextWindowTokens
	}
	return modelContextWindowDefault
}

func trimOneMillionContextSuffix(modelName string) string {
	trimmed := strings.TrimSpace(modelName)
	if strings.HasSuffix(strings.ToLower(trimmed), "[1m]") {
		return strings.TrimSpace(trimmed[:len(trimmed)-len("[1m]")])
	}
	return modelName
}

func deferredToolDescriptionChars(definitions []contracts.ToolDefinition) int {
	total := 0
	for _, definition := range definitions {
		if !toolDefinitionDeferred(definition) {
			continue
		}
		total += len(definition.Name)
		total += len(toolSearchDescriptionText(definition))
		if len(definition.InputSchema) > 0 {
			if encoded, err := json.Marshal(definition.InputSchema); err == nil {
				total += len(encoded)
			}
		}
	}
	return total
}

func toolSearchDescriptionText(definition contracts.ToolDefinition) string {
	for _, value := range []string{definition.Description, definition.Prompt, definition.SearchHint} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isEnvDefinedFalsy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

func isFirstPartyAnthropicBaseURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := parsed.Host
	if host == "" {
		return false
	}
	if host == "api.anthropic.com" {
		return true
	}
	return os.Getenv("USER_TYPE") == "ant" && host == "api-staging.anthropic.com"
}

func loadAllDeferredTools(definitions []contracts.ToolDefinition) []contracts.ToolDefinition {
	out := make([]contracts.ToolDefinition, len(definitions))
	copy(out, definitions)
	for i := range out {
		if toolDefinitionDeferred(out[i]) {
			out[i].AlwaysLoad = true
			out[i].ShouldDefer = false
		}
	}
	return out
}

func applyDiscoveredToolReferences(definitions []contracts.ToolDefinition, history []contracts.Message) []contracts.ToolDefinition {
	discovered := discoveredToolReferenceNames(history)
	if len(discovered) == 0 || len(definitions) == 0 {
		return definitions
	}
	out := make([]contracts.ToolDefinition, len(definitions))
	copy(out, definitions)
	for i := range out {
		if toolDefinitionDiscovered(out[i], discovered) {
			out[i].AlwaysLoad = true
			out[i].ShouldDefer = false
		}
	}
	return out
}

func prependAvailableDeferredToolsMessage(messages []contracts.APIMessage, toolNames []string) []contracts.APIMessage {
	if len(toolNames) == 0 {
		return messages
	}
	content := "<available-deferred-tools>\n" + strings.Join(toolNames, "\n") + "\n</available-deferred-tools>"
	out := make([]contracts.APIMessage, 0, len(messages)+1)
	out = append(out, contracts.APIMessage{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock(content)}})
	out = append(out, messages...)
	return out
}

func stripToolReferenceBlocksFromAPIMessages(messages []contracts.APIMessage) []contracts.APIMessage {
	out := make([]contracts.APIMessage, len(messages))
	for i, message := range messages {
		out[i] = message
		if message.Role != "user" {
			continue
		}
		out[i].Content = stripToolReferenceBlocksFromContent(message.Content)
	}
	return out
}

func stripToolReferenceBlocksFromContent(content []contracts.ContentBlock) []contracts.ContentBlock {
	out := make([]contracts.ContentBlock, len(content))
	for i, block := range content {
		out[i] = block
		if block.Type != contracts.ContentToolResult {
			continue
		}
		if stripped, ok := stripToolReferenceItems(block.Content); ok {
			out[i].Content = stripped
		}
	}
	return out
}

func stripToolReferenceItems(content any) (any, bool) {
	items, ok := toolResultContentItems(content)
	if !ok {
		return content, false
	}
	filtered := make([]any, 0, len(items))
	removed := false
	for _, item := range items {
		if toolReferenceItem(item) {
			removed = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !removed {
		return content, false
	}
	if len(filtered) == 0 {
		return []contracts.ContentBlock{contracts.NewTextBlock("[Tool references removed - tool search not enabled]")}, true
	}
	return filtered, true
}

func toolResultContentItems(content any) ([]any, bool) {
	switch typed := content.(type) {
	case []any:
		return typed, true
	case []contracts.ToolReference:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out, true
	case []contracts.ContentBlock:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out, true
	default:
		return nil, false
	}
}

func toolReferenceItem(item any) bool {
	switch typed := item.(type) {
	case contracts.ToolReference:
		return typed.Type == "tool_reference"
	case map[string]any:
		return toolReferenceType(typed)
	case contracts.ContentBlock:
		return typed.Type == "tool_reference"
	default:
		return false
	}
}

func deferredToolNames(definitions []contracts.ToolDefinition) []string {
	var names []string
	for _, definition := range definitions {
		if toolDefinitionDeferred(definition) {
			names = append(names, definition.Name)
		}
	}
	sort.Strings(names)
	return names
}

func toolDefinitionDeferred(definition contracts.ToolDefinition) bool {
	if definition.AlwaysLoad || isToolSearchDefinition(definition) {
		return false
	}
	return definition.MCP != nil || definition.ShouldDefer
}

func (r Runner) deferredToolsDeltaEnabled() bool {
	if os.Getenv("USER_TYPE") == "ant" {
		return true
	}
	settings := r.mergedSettings()
	return settings.Advanced != nil && advancedBoolEnabled(settings.Advanced.TenguGlacier2XR)
}

func hasToolSearchDefinition(definitions []contracts.ToolDefinition) bool {
	for _, definition := range definitions {
		if isToolSearchDefinition(definition) {
			return true
		}
	}
	return false
}

func withoutToolSearchDefinition(definitions []contracts.ToolDefinition) []contracts.ToolDefinition {
	out := make([]contracts.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if !isToolSearchDefinition(definition) {
			out = append(out, definition)
		}
	}
	return out
}

func isToolSearchDefinition(definition contracts.ToolDefinition) bool {
	if strings.EqualFold(strings.TrimSpace(definition.Name), "ToolSearch") {
		return true
	}
	for _, alias := range definition.Aliases {
		if strings.EqualFold(strings.TrimSpace(alias), "ToolSearch") {
			return true
		}
	}
	return false
}

func toolDefinitionDiscovered(definition contracts.ToolDefinition, discovered map[string]struct{}) bool {
	if _, ok := discovered[strings.ToLower(definition.Name)]; ok {
		return true
	}
	for _, alias := range definition.Aliases {
		if _, ok := discovered[strings.ToLower(alias)]; ok {
			return true
		}
	}
	return false
}

func discoveredToolReferenceNames(history []contracts.Message) map[string]struct{} {
	discovered := map[string]struct{}{}
	for _, message := range history {
		collectCompactBoundaryToolReferences(message, discovered)
		for _, block := range message.Content {
			if block.Type != contracts.ContentToolResult {
				continue
			}
			collectToolReferenceNames(block.Content, discovered)
		}
	}
	return discovered
}

func collectCompactBoundaryToolReferences(message contracts.Message, discovered map[string]struct{}) {
	if message.Type != contracts.MessageSystem || message.Subtype != "compact_boundary" {
		return
	}
	if len(message.Raw) == 0 {
		return
	}
	for _, key := range []string{"compactMetadata", "compact_metadata"} {
		if metadata, ok := message.Raw[key]; ok {
			collectCompactMetadataToolReferences(metadata, discovered)
		}
	}
}

func collectCompactMetadataToolReferences(metadata any, discovered map[string]struct{}) {
	switch typed := metadata.(type) {
	case session.CompactMetadata:
		for _, toolName := range typed.PreCompactDiscoveredTools {
			addDiscoveredToolReference(toolName, discovered)
		}
	case *session.CompactMetadata:
		if typed != nil {
			collectCompactMetadataToolReferences(*typed, discovered)
		}
	case map[string]any:
		for _, key := range []string{"preCompactDiscoveredTools", "pre_compact_discovered_tools"} {
			collectStringSliceToolReferences(typed[key], discovered)
		}
	}
}

func collectStringSliceToolReferences(value any, discovered map[string]struct{}) {
	switch typed := value.(type) {
	case []string:
		for _, toolName := range typed {
			addDiscoveredToolReference(toolName, discovered)
		}
	case []any:
		for _, item := range typed {
			if toolName, ok := item.(string); ok {
				addDiscoveredToolReference(toolName, discovered)
			}
		}
	}
}

func collectToolReferenceNames(content any, discovered map[string]struct{}) {
	switch typed := content.(type) {
	case contracts.ToolReference:
		addDiscoveredToolReference(typed.ToolName, discovered)
	case []contracts.ToolReference:
		for _, reference := range typed {
			addDiscoveredToolReference(reference.ToolName, discovered)
		}
	case map[string]any:
		if toolName, ok := stringMapField(typed, "tool_name", "toolName", "name"); ok && toolReferenceType(typed) {
			addDiscoveredToolReference(toolName, discovered)
		}
	case []map[string]any:
		for _, item := range typed {
			collectToolReferenceNames(item, discovered)
		}
	case []any:
		for _, item := range typed {
			collectToolReferenceNames(item, discovered)
		}
	}
}

func toolReferenceType(item map[string]any) bool {
	value, ok := stringMapField(item, "type")
	return ok && value == "tool_reference"
}

func stringMapField(item map[string]any, names ...string) (string, bool) {
	for _, name := range names {
		if value, ok := item[name].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

func addDiscoveredToolReference(toolName string, discovered map[string]struct{}) {
	if trimmed := strings.TrimSpace(toolName); trimmed != "" {
		discovered[strings.ToLower(trimmed)] = struct{}{}
	}
}

func appendRelevantMemoryPrefetch(history []contracts.Message, result memory.RelevantMemoryPrefetchResult) []contracts.Message {
	if len(result.Memories) == 0 {
		return history
	}
	out := make([]contracts.Message, 0, len(history)+1)
	out = append(out, history...)
	out = append(out, memory.RelevantMemoriesAttachmentMessage(result.Memories))
	return out
}

func (r Runner) applyRelevantMemoryAttachments(history []contracts.Message) ([]contracts.Message, error) {
	if r.RelevantMemoryDir == "" {
		return history, nil
	}
	plan, ok := memory.RelevantMemoryPrefetchPlanForMessages(history, 0)
	if !ok {
		return history, nil
	}
	selected, err := memory.FindRelevantMemorySelections(
		r.RelevantMemoryDir,
		plan.Input,
		memory.CollectRecentSuccessfulTools(history),
		plan.Surfaced.Paths,
		r.relevantMemoryLimit(),
	)
	if err != nil {
		return nil, err
	}
	memories := memory.ReadMemoriesForSurfacing(selected, memory.RelevantMemorySurfaceOptions{})
	if len(memories) == 0 {
		return history, nil
	}
	out := make([]contracts.Message, 0, len(history)+1)
	out = append(out, history...)
	out = append(out, memory.RelevantMemoriesAttachmentMessage(memories))
	return out, nil
}

func (r Runner) applySessionMemoryRecall(history []contracts.Message) ([]contracts.Message, error) {
	if !r.EnableSessionMemoryRecall {
		return history, nil
	}
	root := r.SessionMemoryRecallRoot
	if root == "" {
		root = r.SessionMemoryRoot
	}
	if root == "" {
		root = memory.DefaultSessionMemoryRoot(r.SessionPath)
	}
	if root == "" {
		return history, nil
	}
	query := lastUserText(history)
	if query == "" {
		return history, nil
	}
	matches, err := memory.RecallSessionSummaries(root, query, memory.RecallOptions{
		Limit:            r.sessionMemoryRecallLimit(),
		ExcludeSessionID: r.SessionID,
	})
	if err != nil {
		return nil, err
	}
	message := memory.RecallContextMessage(matches)
	if message.Type == "" {
		return history, nil
	}
	out := make([]contracts.Message, 0, len(history)+1)
	out = append(out, message)
	out = append(out, history...)
	return out, nil
}

func (r Runner) sessionMemoryRecallLimit() int {
	if r.SessionMemoryRecallLimit > 0 {
		return r.SessionMemoryRecallLimit
	}
	return 3
}

func (r Runner) relevantMemoryLimit() int {
	if r.RelevantMemoryLimit > 0 {
		return r.RelevantMemoryLimit
	}
	return memory.MaxRelevantMemoryAttachments
}

func lastUserText(history []contracts.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Type != contracts.MessageUser {
			continue
		}
		if history[i].Subtype == memory.RecallContextSubtype {
			continue
		}
		if text := msgs.TextContent(history[i]); text != "" {
			return text
		}
	}
	return ""
}

func toolPromptContext(r Runner) tool.PromptContext {
	return tool.PromptContext{
		Model:            r.model(),
		WorkingDirectory: r.WorkingDirectory,
		Metadata:         r.toolMetadata(),
	}
}
