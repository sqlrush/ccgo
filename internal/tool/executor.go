package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

var ErrUnknownTool = errors.New("unknown tool")
var ErrPermissionDenied = errors.New("permission denied")

type PermissionError struct {
	Decision contracts.PermissionDecision
}

func (e PermissionError) Error() string {
	if e.Decision.Message != "" {
		return e.Decision.Message
	}
	return string(ErrPermissionDenied.Error())
}

type Executor struct {
	Registry       *Registry
	ResultStoreDir string
	Hooks          []Hook
	Asker          PermissionAsker
}

func NewExecutor(registry *Registry) Executor {
	return Executor{Registry: registry}
}

func (e Executor) Execute(ctx Context, use contracts.ToolUse, sink ProgressSink) (contracts.ToolResult, error) {
	if sink == nil {
		sink = NopProgressSink()
	}
	if err := contextError(ctx); err != nil {
		return ErrorResult(use, err), err
	}
	if e.Registry == nil {
		err := fmt.Errorf("%w: registry is nil", ErrUnknownTool)
		return ErrorResult(use, err), err
	}
	ctx = e.withRegistryMetadata(ctx)
	t, ok := e.Registry.Lookup(use.Name)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrUnknownTool, use.Name)
		return ErrorResult(use, err), err
	}
	raw := normalizeRawInput(use.Input)
	if err := t.Validate(ctx, raw); err != nil {
		err = e.validationErrorWithSchemaHint(ctx, t, err)
		return ErrorResult(use, err), err
	}
	raw, err := e.runPreHooks(ctx, use, t, raw, sink)
	if err != nil {
		return ErrorResult(use, err), err
	}
	if err := t.Validate(ctx, raw); err != nil {
		err = e.validationErrorWithSchemaHint(ctx, t, err)
		return ErrorResult(use, err), err
	}
	if err := contextError(ctx); err != nil {
		return ErrorResult(use, err), err
	}
	_ = SendProgress(sink, use.ID, "started", map[string]any{"tool": t.Name()})
	decision, err := t.CheckPermissions(ctx, raw)
	if err != nil {
		_ = SendProgress(sink, use.ID, "failed", map[string]any{"tool": t.Name(), "error": err.Error()})
		return ErrorResult(use, err), err
	}
	if decision.Behavior == contracts.PermissionDeny {
		permissionErr := PermissionError{Decision: decision}
		result := contracts.ToolResult{
			ToolUseID: use.ID,
			IsError:   true,
			Content:   decision.Message,
			Meta: map[string]any{
				"permission": decision,
			},
		}
		result = e.runPermissionDeniedHooks(ctx, use, t, raw, decision, result, permissionErr, sink)
		_ = SendProgress(sink, use.ID, "permission_denied", map[string]any{"tool": t.Name(), "behavior": string(decision.Behavior)})
		return result, permissionErr
	}
	if decision.Behavior == contracts.PermissionAsk {
		permissionErr := PermissionError{Decision: decision}
		result := contracts.ToolResult{
			ToolUseID: use.ID,
			IsError:   true,
			Content:   decision.Message,
			Meta: map[string]any{
				"permission": decision,
			},
		}
		var hookDecision *contracts.PermissionDecision
		result, hookDecision, raw = e.runPermissionRequestHooks(ctx, use, t, raw, decision, result, permissionErr, sink)
		resolved := hookDecision
		if resolved == nil || resolved.Behavior == contracts.PermissionAsk {
			if e.Asker == nil {
				_ = SendProgress(sink, use.ID, "permission_requested", map[string]any{"tool": t.Name(), "behavior": string(decision.Behavior)})
				return result, permissionErr
			}
			asked, askErr := e.Asker.Ask(ctx.Context, PermissionAskRequest{
				ToolUseID:   use.ID,
				ToolName:    t.Name(),
				Path:        decision.BlockedPath,
				Description: decision.Message,
				Decision:    decision,
			})
			if askErr != nil {
				return ErrorResult(use, askErr), askErr
			}
			resolved = &asked
		}
		// resolved is now non-nil and not Ask.
		if resolved.Behavior == contracts.PermissionDeny {
			if resolved.Message != "" {
				result.Content = resolved.Message
			}
			result.Meta["permission"] = *resolved
			_ = SendProgress(sink, use.ID, "permission_denied", map[string]any{"tool": t.Name(), "behavior": string(resolved.Behavior)})
			return result, PermissionError{Decision: *resolved}
		}
		if resolved.Behavior != contracts.PermissionAllow {
			// Fail safe: any non-Allow resolution (Ask/Passthrough/unknown) blocks the tool.
			_ = SendProgress(sink, use.ID, "permission_requested", map[string]any{"tool": t.Name(), "behavior": string(resolved.Behavior)})
			return result, permissionErr
		}
		if err := t.Validate(ctx, raw); err != nil {
			err = e.validationErrorWithSchemaHint(ctx, t, err)
			return ErrorResult(use, err), err
		}
		_ = SendProgress(sink, use.ID, "permission_allowed", map[string]any{"tool": t.Name(), "behavior": string(resolved.Behavior)})
	}
	if err := contextError(ctx); err != nil {
		_ = SendProgress(sink, use.ID, "cancelled", map[string]any{"tool": t.Name(), "error": err.Error()})
		return ErrorResult(use, err), err
	}
	result, err := t.Call(ctx, raw, defaultToolUseProgressSink{sink: sink, toolUseID: use.ID})
	if result.ToolUseID == "" {
		result.ToolUseID = use.ID
	}
	if err == nil {
		result = e.limitResult(t, use, result)
	}
	result, hookErr := e.runPostHooks(ctx, use, t, raw, result, err, sink)
	if hookErr != nil && err == nil {
		err = hookErr
	}
	if err != nil {
		if result.Content == nil {
			result.Content = err.Error()
		}
		result.IsError = true
		_ = SendProgress(sink, use.ID, "failed", map[string]any{"tool": t.Name(), "error": err.Error()})
		return result, err
	}
	_ = SendProgress(sink, use.ID, "completed", map[string]any{"tool": t.Name()})
	return result, nil
}

func (e Executor) validationErrorWithSchemaHint(ctx Context, t Tool, err error) error {
	if err == nil {
		return nil
	}
	hint := e.schemaNotSentHint(ctx, t)
	if hint == "" || strings.Contains(err.Error(), "This tool's schema was not sent to the API") {
		return err
	}
	return fmt.Errorf("%v%s", err, hint)
}

func (e Executor) schemaNotSentHint(ctx Context, t Tool) string {
	registry := e.Registry
	if registry == nil {
		registry = registryFromMetadata(ctx.Metadata)
	}
	if registry == nil || !toolSearchAvailable(registry) {
		return ""
	}
	messages, ok := messagesFromMetadata(ctx.Metadata)
	if !ok {
		return ""
	}
	definition, err := Definition(PromptContext{
		WorkingDirectory: ctx.WorkingDirectory,
		Metadata:         ctx.Metadata,
	}, t)
	if err != nil || !definition.ShouldDefer || definition.AlwaysLoad {
		return ""
	}
	discovered := discoveredToolNamesFromMessages(messages)
	if toolDefinitionNameDiscovered(definition, discovered) {
		return ""
	}
	return fmt.Sprintf(
		"\n\nThis tool's schema was not sent to the API - it was not in the discovered-tool set derived from message history. "+
			"Without the schema in your prompt, typed parameters can be emitted as strings and the client-side parser rejects them. "+
			"Load the tool first: call ToolSearch with query \"select:%s\", then retry this call.",
		definition.Name,
	)
}

func registryFromMetadata(metadata map[string]any) *Registry {
	if metadata == nil {
		return nil
	}
	switch typed := metadata[MetadataToolRegistryKey].(type) {
	case *Registry:
		return typed
	default:
		return nil
	}
}

func toolSearchAvailable(registry *Registry) bool {
	if registry == nil {
		return false
	}
	_, ok := registry.Lookup("ToolSearch")
	return ok
}

func messagesFromMetadata(metadata map[string]any) ([]contracts.Message, bool) {
	if metadata == nil {
		return nil, false
	}
	switch typed := metadata[MetadataMessagesKey].(type) {
	case []contracts.Message:
		return typed, true
	case *[]contracts.Message:
		if typed != nil {
			return *typed, true
		}
	}
	return nil, false
}

func discoveredToolNamesFromMessages(messages []contracts.Message) map[string]struct{} {
	discovered := map[string]struct{}{}
	for _, message := range messages {
		collectCompactMetadataToolNames(message, discovered)
		for _, block := range message.Content {
			if block.Type != contracts.ContentToolResult {
				continue
			}
			collectToolReferenceNames(block.Content, discovered)
		}
	}
	return discovered
}

func collectCompactMetadataToolNames(message contracts.Message, discovered map[string]struct{}) {
	if message.Type != contracts.MessageSystem || message.Subtype != "compact_boundary" || len(message.Raw) == 0 {
		return
	}
	for _, key := range []string{"compactMetadata", "compact_metadata"} {
		if metadata, ok := message.Raw[key]; ok {
			collectPreCompactDiscoveredTools(metadata, discovered)
		}
	}
}

func collectPreCompactDiscoveredTools(metadata any, discovered map[string]struct{}) {
	switch typed := metadata.(type) {
	case map[string]any:
		for _, key := range []string{"preCompactDiscoveredTools", "pre_compact_discovered_tools"} {
			collectStringSliceToolReferences(typed[key], discovered)
		}
	case map[string][]string:
		for _, key := range []string{"preCompactDiscoveredTools", "pre_compact_discovered_tools"} {
			collectStringSliceToolReferences(typed[key], discovered)
		}
	default:
		collectPreCompactDiscoveredToolsFromStruct(metadata, discovered)
	}
}

func collectPreCompactDiscoveredToolsFromStruct(metadata any, discovered map[string]struct{}) {
	value := reflect.ValueOf(metadata)
	if !value.IsValid() {
		return
	}
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return
	}
	field := value.FieldByName("PreCompactDiscoveredTools")
	if !field.IsValid() || field.Kind() != reflect.Slice {
		return
	}
	for i := 0; i < field.Len(); i++ {
		item := field.Index(i)
		if item.Kind() == reflect.String {
			addDiscoveredToolName(item.String(), discovered)
		}
	}
}

func collectStringSliceToolReferences(value any, discovered map[string]struct{}) {
	switch typed := value.(type) {
	case []string:
		for _, toolName := range typed {
			addDiscoveredToolName(toolName, discovered)
		}
	case []any:
		for _, item := range typed {
			if toolName, ok := item.(string); ok {
				addDiscoveredToolName(toolName, discovered)
			}
		}
	}
}

func collectToolReferenceNames(content any, discovered map[string]struct{}) {
	switch typed := content.(type) {
	case contracts.ToolReference:
		addDiscoveredToolName(typed.ToolName, discovered)
	case []contracts.ToolReference:
		for _, reference := range typed {
			addDiscoveredToolName(reference.ToolName, discovered)
		}
	case map[string]any:
		if toolName, ok := stringMapField(typed, "tool_name", "toolName", "name"); ok && toolReferenceType(typed) {
			addDiscoveredToolName(toolName, discovered)
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

func toolDefinitionNameDiscovered(definition contracts.ToolDefinition, discovered map[string]struct{}) bool {
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

func addDiscoveredToolName(toolName string, discovered map[string]struct{}) {
	if trimmed := strings.TrimSpace(toolName); trimmed != "" {
		discovered[strings.ToLower(trimmed)] = struct{}{}
	}
}

func (e Executor) withRegistryMetadata(ctx Context) Context {
	if ctx.Metadata == nil {
		ctx.Metadata = map[string]any{}
	}
	if _, ok := ctx.Metadata[MetadataToolRegistryKey]; !ok && e.Registry != nil {
		ctx.Metadata[MetadataToolRegistryKey] = e.Registry
	}
	return ctx
}

type defaultToolUseProgressSink struct {
	sink      ProgressSink
	toolUseID contracts.ID
}

func (s defaultToolUseProgressSink) Send(progress contracts.ToolProgress) error {
	if s.sink == nil {
		return nil
	}
	if progress.ToolUseID == "" {
		progress.ToolUseID = s.toolUseID
	}
	return s.sink.Send(progress)
}

func (e Executor) runPreHooks(ctx Context, use contracts.ToolUse, t Tool, raw json.RawMessage, sink ProgressSink) (json.RawMessage, error) {
	current := normalizeRawInput(raw)
	for idx, hook := range e.hooksForPhase(HookPreToolUse) {
		_ = e.sendHookProgress(sink, use.ID, t, HookPreToolUse, idx, "hook_started", nil)
		result, err := hook.RunToolHook(ctx, HookEvent{Phase: HookPreToolUse, ToolUse: use, ToolName: t.Name(), Input: current})
		if err != nil {
			_ = e.sendHookProgress(sink, use.ID, t, HookPreToolUse, idx, "hook_failed", map[string]any{"error": err.Error()})
			return current, err
		}
		if len(result.UpdatedInput) > 0 {
			current = normalizeRawInput(result.UpdatedInput)
		}
		if result.Block {
			if result.Message == "" {
				result.Message = "blocked by PreToolUse hook"
			}
			_ = e.sendHookProgress(sink, use.ID, t, HookPreToolUse, idx, "hook_blocked", map[string]any{"message": result.Message})
			return current, HookBlockedError{Phase: HookPreToolUse, Message: result.Message, Metadata: result.Metadata}
		}
		data := map[string]any{}
		if len(result.UpdatedInput) > 0 {
			data["updated_input"] = true
		}
		if result.Message != "" {
			data["message"] = result.Message
		}
		_ = e.sendHookProgress(sink, use.ID, t, HookPreToolUse, idx, "hook_completed", data)
	}
	return current, nil
}

func (e Executor) runPermissionDeniedHooks(ctx Context, use contracts.ToolUse, t Tool, raw json.RawMessage, decision contracts.PermissionDecision, result contracts.ToolResult, originalErr error, sink ProgressSink) contracts.ToolResult {
	result, _, _ = e.runPermissionHooks(ctx, use, t, raw, decision, result, originalErr, sink, HookPermissionDenied, "permission_denied_hook")
	return result
}

func (e Executor) runPermissionRequestHooks(ctx Context, use contracts.ToolUse, t Tool, raw json.RawMessage, decision contracts.PermissionDecision, result contracts.ToolResult, originalErr error, sink ProgressSink) (contracts.ToolResult, *contracts.PermissionDecision, json.RawMessage) {
	return e.runPermissionHooks(ctx, use, t, raw, decision, result, originalErr, sink, HookPermissionRequest, "permission_request_hook")
}

func (e Executor) runPermissionHooks(ctx Context, use contracts.ToolUse, t Tool, raw json.RawMessage, decision contracts.PermissionDecision, result contracts.ToolResult, originalErr error, sink ProgressSink, phase string, metaKey string) (contracts.ToolResult, *contracts.PermissionDecision, json.RawMessage) {
	current := raw
	// accumBehavior tracks the folded decision with deny > ask > allow precedence.
	// The empty string means no hook has returned a decision yet.
	var accumBehavior contracts.PermissionBehavior
	var accumMessage string
	for idx, hook := range e.hooksForPhase(phase) {
		_ = e.sendHookProgress(sink, use.ID, t, phase, idx, "hook_started", map[string]any{"behavior": string(decision.Behavior)})
		hookResult, err := hook.RunToolHook(ctx, HookEvent{Phase: phase, ToolUse: use, ToolName: t.Name(), Input: current, Decision: &decision, Result: &result, Error: originalErr.Error()})
		if result.Meta == nil {
			result.Meta = map[string]any{}
		}
		if err != nil {
			result.Meta[metaKey+"_error"] = err.Error()
			_ = e.sendHookProgress(sink, use.ID, t, phase, idx, "hook_failed", map[string]any{"behavior": string(decision.Behavior), "error": err.Error()})
			continue
		}
		if hookResult.Message != "" {
			result.Meta[metaKey+"_message"] = hookResult.Message
		}
		if len(hookResult.Metadata) > 0 {
			result.Meta[metaKey] = hookResult.Metadata
		}
		if len(hookResult.UpdatedInput) > 0 {
			current = normalizeRawInput(hookResult.UpdatedInput)
		}
		if hookResult.PermissionDecision != nil {
			folded := foldPermissionBehavior(accumBehavior, hookResult.PermissionDecision.Behavior)
			if folded != accumBehavior {
				accumBehavior = folded
				if hookResult.PermissionDecision.Behavior == contracts.PermissionDeny {
					accumMessage = firstNonEmptyExec(hookResult.PermissionDecision.Message, hookResult.Message, accumMessage)
				}
			}
		} else if hookResult.Block {
			accumBehavior = contracts.PermissionDeny
			accumMessage = firstNonEmptyExec(hookResult.Message, accumMessage, "blocked by "+phase+" hook")
		}
		data := map[string]any{"behavior": string(decision.Behavior)}
		if hookResult.Message != "" {
			data["message"] = hookResult.Message
		}
		if len(hookResult.UpdatedInput) > 0 {
			data["updated_input"] = true
		}
		if hookResult.PermissionDecision != nil {
			data["permission_behavior"] = string(hookResult.PermissionDecision.Behavior)
		}
		_ = e.sendHookProgress(sink, use.ID, t, phase, idx, "hook_completed", data)
	}
	var hookDecision *contracts.PermissionDecision
	if accumBehavior != "" {
		hookDecision = &contracts.PermissionDecision{Behavior: accumBehavior, Message: accumMessage}
	}
	return result, hookDecision, current
}

func (e Executor) runPostHooks(ctx Context, use contracts.ToolUse, t Tool, raw json.RawMessage, result contracts.ToolResult, callErr error, sink ProgressSink) (contracts.ToolResult, error) {
	var errText string
	if callErr != nil {
		errText = callErr.Error()
	}
	for idx, hook := range e.hooksForPhase(HookPostToolUse) {
		_ = e.sendHookProgress(sink, use.ID, t, HookPostToolUse, idx, "hook_started", nil)
		hookResult, err := hook.RunToolHook(ctx, HookEvent{Phase: HookPostToolUse, ToolUse: use, ToolName: t.Name(), Input: raw, Result: &result, Error: errText})
		if err != nil {
			_ = e.sendHookProgress(sink, use.ID, t, HookPostToolUse, idx, "hook_failed", map[string]any{"error": err.Error()})
			return result, err
		}
		if hookResult.Block {
			if hookResult.Message == "" {
				hookResult.Message = "blocked by PostToolUse hook"
			}
			_ = e.sendHookProgress(sink, use.ID, t, HookPostToolUse, idx, "hook_blocked", map[string]any{"message": hookResult.Message})
			return result, HookBlockedError{Phase: HookPostToolUse, Message: hookResult.Message, Metadata: hookResult.Metadata}
		}
		if len(hookResult.Metadata) > 0 {
			if result.Meta == nil {
				result.Meta = map[string]any{}
			}
			result.Meta["post_tool_use_hook"] = hookResult.Metadata
		}
		data := map[string]any{}
		if hookResult.Message != "" {
			data["message"] = hookResult.Message
		}
		_ = e.sendHookProgress(sink, use.ID, t, HookPostToolUse, idx, "hook_completed", data)
	}
	return result, nil
}

// foldPermissionBehavior applies deny > ask > allow precedence across hook decisions.
// Passthrough and unknown values are no-ops. Mirrors internal/hooks.foldBehavior
// (deliberate duplication: internal/hooks imports internal/tool, so the reverse import
// would create a cycle).
func foldPermissionBehavior(current, next contracts.PermissionBehavior) contracts.PermissionBehavior {
	switch next {
	case contracts.PermissionDeny:
		return contracts.PermissionDeny
	case contracts.PermissionAsk:
		if current != contracts.PermissionDeny {
			return contracts.PermissionAsk
		}
		return current
	case contracts.PermissionAllow:
		if current == "" {
			return contracts.PermissionAllow
		}
		return current
	default:
		return current
	}
}

// firstNonEmptyExec returns the first non-empty string among its arguments.
func firstNonEmptyExec(a, b, c string) string {
	if a != "" {
		return a
	}
	if b != "" {
		return b
	}
	return c
}

func (e Executor) hooksForPhase(phase string) []Hook {
	if len(e.Hooks) == 0 {
		return nil
	}
	out := make([]Hook, 0, len(e.Hooks))
	for _, hook := range e.Hooks {
		if hook == nil || !hookMatchesPhase(hook, phase) {
			continue
		}
		out = append(out, hook)
	}
	return out
}

func hookMatchesPhase(hook Hook, phase string) bool {
	phaseHook, ok := hook.(PhaseHook)
	if !ok {
		return true
	}
	for _, candidate := range phaseHook.HookPhases() {
		if candidate == phase {
			return true
		}
	}
	return false
}

func (e Executor) sendHookProgress(sink ProgressSink, toolUseID contracts.ID, t Tool, phase string, index int, progressType string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	data["tool"] = t.Name()
	data["phase"] = phase
	data["hook_index"] = index
	return SendProgress(sink, toolUseID, progressType, data)
}

type HookBlockedError struct {
	Phase    string
	Message  string
	Metadata map[string]any
}

func (e HookBlockedError) Error() string {
	return e.Message
}

func contextError(ctx Context) error {
	if ctx.Context == nil {
		return nil
	}
	if err := ctx.Context.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		return err
	}
	return nil
}

func (e Executor) limitResult(t Tool, use contracts.ToolUse, result contracts.ToolResult) contracts.ToolResult {
	limit := t.MaxResultSizeChars()
	if limit <= 0 {
		return result
	}
	content, ok := result.Content.(string)
	if !ok || len(content) <= limit {
		return result
	}
	dir := e.ResultStoreDir
	if dir == "" {
		dir = filepath.Join(platform.ClaudeHomeDir(), "tool-results")
	}
	name := string(use.ID)
	if name == "" {
		name = string(contracts.NewID())
	}
	path := filepath.Join(dir, sanitizeResultFileName(name)+".txt")
	_ = platform.AtomicWriteFile(path, []byte(content), 0o600)
	preview := content[:limit]
	result.Content = fmt.Sprintf("%s\n\n[Tool output truncated; full output saved to %s]", strings.TrimRight(preview, "\n"), path)
	if result.Meta == nil {
		result.Meta = map[string]any{}
	}
	result.Meta["truncated"] = true
	result.Meta["full_output_path"] = path
	result.Meta["full_output_bytes"] = len(content)
	return result
}

func sanitizeResultFileName(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', 0:
			return '-'
		default:
			return r
		}
	}, name)
	if name == "." || name == string(os.PathSeparator) || name == "" {
		return "tool-result"
	}
	return name
}
