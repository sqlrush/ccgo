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

	// Controller: handles interrupt (cancel the turn) and set_model.
	controller := NewController(
		func() { cancelTurn(errInterrupted) },
		func(m string) error { runner.Model = m; return nil },
	)

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

	// Wire events: emit each conversation event as an sdk_event line on Out
	// so the SDK client can observe turn progress.
	runner.OnEvent = func(ev conversation.Event) {
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
