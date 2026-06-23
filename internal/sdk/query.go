package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/messages"
)

// Options configures a programmatic SDK query (CC agentSdkTypes.ts:112-122).
// All fields are optional except Prompt, Out, and RunnerFactory.
type Options struct {
	// Prompt is the user message to send in this turn.
	Prompt string
	// Model overrides the runner's model when non-empty.
	Model string
	// PermissionMode is a hint for the runner's permission policy.
	PermissionMode string
	// In is the optional NDJSON stream of inbound control messages
	// (interrupt, set_model, can_use_tool responses). EOF or nil stops the read-loop.
	In io.Reader
	// Out receives NDJSON control_request/control_response/sdk_event lines.
	Out io.Writer
	// RunnerFactory builds (or fetches) the conversation runner.
	// cmd/claude supplies a default via bootstrap.State.ConversationRunner().
	RunnerFactory func() (*conversation.Runner, error)

	// ── G1 live-backend callbacks ─────────────────────────────────────────────
	// When nil, Query injects a default from the runner (if feasible) or the
	// Controller falls back to its ⚠️ "not supported" response.

	// RewindFiles, when non-nil, is called by the rewind_files subtype handler
	// instead of the default (which returns canRewind:false). Callers that have
	// a live rewind store wire this via rewind.Rewind.
	// CC ref: controlSchemas.ts:308-328 (SDK-34).
	RewindFiles func(userMessageID string, dryRun bool) (*RewindFilesResult, error)
}

// Query runs a single conversation turn under the SDK control protocol.
// It composes:
//   - T7 Encoder/Decoder for NDJSON framing on Out/In.
//   - T8 controlAsker (tool permission → can_use_tool round-trip).
//   - T8 Controller (interrupt → cancel turn; set_model → next turn model).
//   - conversation.Runner.RunTurn (the actual LLM turn).
//
// Concurrent model: RunTurn runs synchronously; the read-loop runs in a
// separate goroutine reading from In and dispatching to Controller/asker.
// Both are bound to turnCtx; cancelling it (via interrupt or parent ctx)
// terminates both. The read-loop exits on EOF or ctx cancellation.
// No goroutine is leaked: the read-loop exits when turnCtx is done or In is
// exhausted, whichever comes first.
func Query(ctx context.Context, opts Options) error {
	if opts.Prompt == "" {
		return fmt.Errorf("sdk: Options.Prompt is required")
	}
	if opts.RunnerFactory == nil {
		return fmt.Errorf("sdk: Options.RunnerFactory is required")
	}
	if opts.Out == nil {
		return fmt.Errorf("sdk: Options.Out is required")
	}

	runner, err := opts.RunnerFactory()
	if err != nil {
		return fmt.Errorf("sdk: build runner: %w", err)
	}
	if opts.Model != "" {
		runner.Model = opts.Model
	}

	enc := NewEncoder(opts.Out)

	// turnCtx is cancelled by interrupt or parent ctx.
	turnCtx, cancelTurn := context.WithCancelCause(ctx)
	defer cancelTurn(nil)

	// Request ID generator (atomic counter, goroutine-safe).
	var reqCounter int64
	nextID := func() string {
		n := atomic.AddInt64(&reqCounter, 1)
		return "ctl-" + strconv.FormatInt(n, 10)
	}

	// controlAsker: sends can_use_tool requests over Out and blocks until the
	// read-loop calls asker.Resolve with the matching control_response.
	asker := newControlAsker(enc.WriteRequest, nextID)
	runner.Tools.Asker = asker

	// ── G1: wire real callbacks from runner subsystems ─────────────────────────

	// set_permission_mode → update runner.PermissionMode.
	// CC ref: bridgeMessaging.ts:328-358; controlSchemas.ts:124-135 (SDK-30).
	setPermissionMode := func(mode contracts.PermissionMode) error {
		runner.PermissionMode = mode
		return nil
	}

	// get_context_usage → return accumulated usage from the runner's history.
	// We track usage via the OnEvent hook: each assistant message carries Usage.
	// Accumulate into a mutex-protected total so the callback is goroutine-safe.
	usageMu := &usageMutex{}
	getContextUsage := func() (*ContextUsage, error) {
		u := usageMu.get()
		return &ContextUsage{
			Categories:  []ContextCategory{},
			TotalTokens: u.InputTokens + u.OutputTokens,
			MaxTokens:   runner.MaxTokens,
			Percentage:  contextUsagePercentage(u.InputTokens+u.OutputTokens, runner.MaxTokens),
			GridRows:    [][]any{},
			Model:       runner.Model,
			MemoryFiles: []any{},
			MCPTools:    []any{},
			Agents:      []any{},
		}, nil
	}

	// mcp_status → enumerate servers from runner.MCP static config.
	// No live connection manager in a single-shot SDK session — return "configured".
	// CC ref: controlSchemas.ts:157-173 (SDK-32).
	mcpStatus := func() ([]MCPServerStatus, error) {
		return mcpStatusFromRunner(runner), nil
	}

	// get_settings → merge runner.MCP settings layers.
	// CC ref: controlSchemas.ts:475-519 (SDK-45).
	getSettings := func() (*SettingsResult, error) {
		return settingsFromRunner(runner), nil
	}

	// rewind_files → use caller-provided callback (e.g. rewind.Rewind) if set,
	// otherwise fall back to canRewind:false (no store available).
	// CC ref: controlSchemas.ts:308-328 (SDK-34).
	var rewindFiles func(string, bool) (*RewindFilesResult, error)
	if opts.RewindFiles != nil {
		rewindFiles = opts.RewindFiles
	}

	// Controller: handles all control subtypes with live backend callbacks.
	controller := &Controller{
		interrupt:         func() { cancelTurn(errInterrupted) },
		setModel:          func(m string) error { runner.Model = m; return nil },
		setPermissionMode: setPermissionMode,
		getContextUsage:   getContextUsage,
		mcpStatus:         mcpStatus,
		getSettings:       getSettings,
		rewindFiles:       rewindFiles,
		// Subtypes without a live backend in a single-shot SDK session:
		// cancelAsyncMessage, seedReadState, hookCallback, mcpMessage,
		// mcpSetServers, mcpReconnect, mcpToggle, reloadPlugins,
		// applyFlagSettings, stopTask, elicitation remain nil — Controller
		// returns its ⚠️ "not supported" responses (matching CC bridgeMessaging.ts:339).
	}

	// Read-loop goroutine: decodes inbound NDJSON from In, routes
	// control_request to Controller (interrupt/set_model) and
	// control_response to asker.Resolve (can_use_tool replies).
	loopDone := make(chan struct{})
	if opts.In != nil {
		go func() {
			defer close(loopDone)
			readControlLoop(turnCtx, opts.In, controller, asker, enc)
		}()
	} else {
		close(loopDone)
	}

	// Wire events: emit each conversation event as an sdk_event line on Out.
	// Also track accumulated usage so get_context_usage reflects real tokens.
	runner.OnEvent = func(ev conversation.Event) {
		// Accumulate usage from assistant messages (carries LLM billing data).
		if ev.Message != nil && ev.Message.Usage != nil {
			usageMu.accumulate(*ev.Message.Usage)
		}
		payload := map[string]any{"type": string(ev.Type)}
		if ev.Model != "" {
			payload["model"] = ev.Model
		}
		_ = enc.WriteRequest(ControlRequest{
			Type:    "sdk_event",
			Request: payload,
		})
	}

	user := messages.UserText(opts.Prompt)
	_, turnErr := runner.RunTurn(turnCtx, nil, user)
	// Cancel the turn ctx so the read-loop exits promptly.
	cancelTurn(turnErr)

	// Wait for the read-loop to exit (no goroutine leak).
	<-loopDone

	// If the turn was interrupted intentionally via a control_request,
	// return context.Canceled to the caller.
	if errors.Is(context.Cause(turnCtx), errInterrupted) {
		return context.Canceled
	}
	if turnErr != nil && !errors.Is(turnErr, context.Canceled) && !errors.Is(turnErr, context.DeadlineExceeded) {
		_ = enc.WriteResponse(ErrorResponse("", turnErr.Error()))
		return fmt.Errorf("sdk: run turn: %w", turnErr)
	}
	return nil
}

// errInterrupted is the cause stored in turnCtx when an interrupt
// control_request fires, distinguishing it from a parent ctx cancellation.
var errInterrupted = errors.New("sdk: interrupted by control request")

// readControlLoop reads raw NDJSON from r line-by-line. For each line it:
//   - If type == "control_request" → Controller.Handle, writes response to enc.
//   - If type == "control_response" → resolves a pending can_use_tool via asker.Resolve.
//
// The read goroutine exits when r returns EOF or ctx is done.
// To support ctx cancellation while blocked on a slow/pipe reader, the loop
// runs blocking reads in a separate goroutine and selects on ctx.Done().
func readControlLoop(ctx context.Context, r io.Reader, c *Controller, asker *controlAsker, enc *Encoder) {
	type lineResult struct {
		line string
		err  error
	}

	lineCh := make(chan lineResult, 1)
	br := bufio.NewReader(r)

	// lineReader goroutine: reads one line at a time, sends to lineCh.
	// It must exit when the loop exits. Since we can't cancel blocking reads,
	// we accept that this goroutine may linger until r is closed/EOF'd by the
	// caller (e.g. when the turn ends and the pipe is closed). This is
	// intentional — it's the caller's responsibility to close opts.In when done.
	go func() {
		for {
			line, err := br.ReadString('\n')
			lineCh <- lineResult{line: line, err: err}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case res := <-lineCh:
			trimmed := strings.TrimSpace(res.line)
			if trimmed != "" {
				dispatchLine(trimmed, c, asker, enc)
			}
			if res.err != nil {
				return
			}
		}
	}
}

// dispatchLine decodes one NDJSON line and routes it to the appropriate handler.
// Handled message types:
//   - "control_request"        → Controller.Handle, write response.
//   - "control_response"       → asker.Resolve (can_use_tool reply).
//   - "control_cancel_request" → cancel a pending asker request by request_id.
//   - "keep_alive"             → silently ignored (no response required).
//   - "update_environment_variables" → silently accepted (no env mutation in-process).
//
// CC ref: controlSchemas.ts:612-636 (control_cancel_request, keep_alive,
// update_environment_variables).
func dispatchLine(line string, c *Controller, asker *controlAsker, enc *Encoder) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return // silently drop malformed lines
	}

	var msgType string
	if t, ok := raw["type"]; ok {
		_ = json.Unmarshal(t, &msgType)
	}

	switch msgType {
	case "control_request":
		// Decode as ControlRequest and dispatch to controller.
		var req ControlRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			return
		}
		resp := c.Handle(req)
		_ = enc.WriteResponse(resp)

	case "control_response":
		// Decode as ControlResponse (can_use_tool reply) and resolve the asker.
		resolveFromRaw(raw, asker)

	case "control_cancel_request":
		// Cancel a pending asker request (e.g. a can_use_tool that was sent but
		// the SDK consumer no longer needs an answer).
		// CC ref: controlSchemas.ts:612-619.
		var reqID string
		if r, ok := raw["request_id"]; ok {
			_ = json.Unmarshal(r, &reqID)
		}
		if reqID != "" {
			asker.Cancel(reqID)
		}

	case "keep_alive", "update_environment_variables":
		// keep_alive: both ends may send this to maintain a long-lived connection.
		// update_environment_variables: accepted but not applied in-process.
		// CC ref: controlSchemas.ts:621-636.
		// No response sent.
	}
}

// resolveFromRaw extracts requestID and PermissionDecision from a raw
// control_response envelope and delivers them to asker.Resolve.
//
// CC wire shape: {"type":"control_response","response":{"subtype":"success",
// "request_id":"...","response":{"behavior":"allow",...}}}
func resolveFromRaw(raw map[string]json.RawMessage, asker *controlAsker) {
	// Decode the outer "response" field → ControlResponseBody.
	var body ControlResponseBody
	if r, ok := raw["response"]; ok {
		if err := json.Unmarshal(r, &body); err != nil {
			return
		}
	}
	requestID := body.RequestID
	if requestID == "" {
		return
	}

	// Extract PermissionDecision from body.Response.
	var decision contracts.PermissionDecision
	if body.Response != nil {
		if b, ok := body.Response["behavior"].(string); ok {
			decision.Behavior = contracts.PermissionBehavior(b)
		}
		if msg, ok := body.Response["message"].(string); ok {
			decision.Message = msg
		}
		if ui, ok := body.Response["updatedInput"].(map[string]any); ok {
			decision.UpdatedInput = ui
		}
	}

	asker.Resolve(requestID, decision)
}

// ── G1 helpers ────────────────────────────────────────────────────────────────

// usageMutex safely accumulates LLM usage across concurrent OnEvent callbacks.
type usageMutex struct {
	mu    sync.Mutex
	total contracts.Usage
}

func (u *usageMutex) accumulate(usage contracts.Usage) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.total.InputTokens += usage.InputTokens
	u.total.OutputTokens += usage.OutputTokens
	u.total.CacheCreationInputTokens += usage.CacheCreationInputTokens
	u.total.CacheReadInputTokens += usage.CacheReadInputTokens
}

func (u *usageMutex) get() contracts.Usage {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.total
}

// contextUsagePercentage returns the fraction [0,1] of total/max tokens.
// Returns 0 when maxTokens is zero (unknown capacity).
func contextUsagePercentage(total, maxTokens int) float64 {
	if maxTokens <= 0 {
		return 0
	}
	p := float64(total) / float64(maxTokens)
	if p > 1 {
		return 1
	}
	return p
}

// mcpStatusFromRunner builds an MCPServerStatus list from the runner's static
// MCP configuration. No live connection manager exists in a single-shot SDK
// session — all servers are reported as "configured".
// CC ref: controlSchemas.ts:157-173; coreSchemas.ts:167-220 (SDK-32).
func mcpStatusFromRunner(runner *conversation.Runner) []MCPServerStatus {
	if runner.MCP == nil {
		return []MCPServerStatus{}
	}
	merged := runner.MCP.MergedSettings()
	if len(merged.MCPServers) == 0 {
		return []MCPServerStatus{}
	}
	out := make([]MCPServerStatus, 0, len(merged.MCPServers))
	for name, srv := range merged.MCPServers {
		out = append(out, MCPServerStatus{
			Name:   name,
			Status: "configured",
			Scope:  srv.Scope,
		})
	}
	return out
}

// settingsFromRunner builds a SettingsResult from the runner's MCP settings
// layers.  The "effective" field is the merged view; sources lists each layer.
// CC ref: controlSchemas.ts:475-519 (SDK-45).
func settingsFromRunner(runner *conversation.Runner) *SettingsResult {
	if runner.MCP == nil {
		return &SettingsResult{
			Effective: map[string]any{},
			Sources:   []SettingsSource{},
		}
	}
	merged := runner.MCP.MergedSettings()
	effective := settingsToMap(merged)
	sources := []SettingsSource{
		{Source: "user", Settings: settingsToMap(runner.MCP.UserSettings)},
		{Source: "project", Settings: settingsToMap(runner.MCP.ProjectSettings)},
		{Source: "local", Settings: settingsToMap(runner.MCP.LocalSettings)},
		{Source: "policy", Settings: settingsToMap(runner.MCP.PolicySettings)},
	}
	return &SettingsResult{Effective: effective, Sources: sources}
}

// settingsToMap serialises a contracts.Settings value to a map[string]any via
// JSON round-trip. This is simple and correct for the get_settings wire format.
func settingsToMap(s any) map[string]any {
	data, err := json.Marshal(s)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}
