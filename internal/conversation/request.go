package conversation

import (
	"strings"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	"ccgo/internal/memory"
	msgs "ccgo/internal/messages"
	"ccgo/internal/session"
	"ccgo/internal/tool"
)

func (r Runner) BuildRequest(history []contracts.Message, model string) (anthropic.Request, error) {
	return r.buildRequest(history, model, relevantMemoryRequestContext{})
}

type relevantMemoryRequestContext struct {
	Prefetch *memory.RelevantMemoryPrefetchResult
	SkipSync bool
}

func (r Runner) buildRequest(history []contracts.Message, model string, relevantMemory relevantMemoryRequestContext) (anthropic.Request, error) {
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
	request := anthropic.Request{
		Model:     model,
		MaxTokens: r.maxTokens(),
		Messages:  msgs.NormalizeForAPI(history),
	}
	if system := r.systemPromptWithOutputStyle(); system != "" {
		request.System = system
	}
	if r.Tools.Registry != nil {
		definitions, err := r.Tools.Registry.Definitions(toolPromptContext(r))
		if err != nil {
			return anthropic.Request{}, err
		}
		definitions = applyDiscoveredToolReferences(definitions, history)
		if len(definitions) > 0 {
			request.Tools = anthropic.ToolsFromContracts(definitions)
		}
	}
	return request, nil
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
