package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// ReadWriter combines io.Reader, io.Writer, and io.Closer. It is the transport
// for ConcreteNavigationClient — satisfied by an io.ReadWriteCloser or the
// readWriteCloser helper below.
type ReadWriter interface {
	io.Reader
	io.Writer
	io.Closer
}

// readWriteCloser combines a separate ReadCloser + WriteCloser into a single
// ReadWriter. Useful in tests where the two directions come from different pipes.
type readWriteCloser struct {
	io.ReadCloser
	io.WriteCloser
}

// Close closes both the reader and writer halves.
func (rwc readWriteCloser) Close() error {
	rerr := rwc.ReadCloser.Close()
	werr := rwc.WriteCloser.Close()
	if rerr != nil {
		return rerr
	}
	return werr
}

// ConcreteNavigationClientOptions configures a ConcreteNavigationClient.
type ConcreteNavigationClientOptions struct {
	// ReadWriter is the bidirectional transport to the language server
	// (e.g. the stdin/stdout pipes of a spawned process, or a test pipe pair).
	// Required.
	ReadWriter ReadWriter

	// WorkspaceRoot is the root URI / path used in the initialize request.
	// Optional; when empty the client sends a nil rootUri.
	WorkspaceRoot string

	// ClientName overrides the LSP clientInfo.name. Defaults to "ccgo".
	ClientName string

	// DiagnosticHandler receives every non-response LSP message (notifications
	// and server-initiated requests) as a raw framed payload. Use to wire
	// publishDiagnostics into the diagnostics snapshot machinery.
	// When nil, notifications are silently discarded.
	DiagnosticHandler func(payload []byte)

	// InitializeID is the JSON-RPC request id used for the initialize request.
	// Defaults to defaultInitializeID.
	InitializeID int
}

// ConcreteNavigationClient is a live implementation of NavigationClient that
// speaks the LSP JSON-RPC wire protocol over a ReadWriter.
//
// It performs the initialize handshake on construction, multiplexes concurrent
// requests through RPCMux, and forwards server notifications to DiagnosticHandler.
//
// The client is safe for concurrent use.
type ConcreteNavigationClient struct {
	transport ReadWriter
	mux       *RPCMux
	cancel    context.CancelFunc
	done      chan struct{}

	writeMu sync.Mutex // serialises writes to transport
}

// NewConcreteNavigationClient creates a ConcreteNavigationClient, performs the
// LSP initialize handshake, and starts the background RPCMux read loop.
// The caller must call Close to release resources.
func NewConcreteNavigationClient(ctx context.Context, opts ConcreteNavigationClientOptions) (*ConcreteNavigationClient, error) {
	if opts.ReadWriter == nil {
		return nil, fmt.Errorf("lsp: ConcreteNavigationClientOptions.ReadWriter is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	mux := NewRPCMux()
	mux.NotificationHandler = opts.DiagnosticHandler

	loopCtx, cancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})

	c := &ConcreteNavigationClient{
		transport: opts.ReadWriter,
		mux:       mux,
		cancel:    cancel,
		done:      doneCh,
	}

	// Start the background read loop before sending the initialize request so
	// that the initialize response is routed correctly.
	go func() {
		defer close(doneCh)
		_ = mux.Run(loopCtx, opts.ReadWriter, defaultFrameLimit)
	}()

	// Send the initialize handshake.
	initID := opts.InitializeID
	if initID == 0 {
		initID = defaultInitializeID
	}
	idStr := json.Number(fmt.Sprintf("%d", initID))
	ch := mux.Register(idStr)

	handshakeOpts := ServerHandshakeOptions{
		InitializeID: initID,
		ClientName:   opts.ClientName,
	}
	if opts.WorkspaceRoot != "" {
		handshakeOpts.RootURI = FileURIFromPath(opts.WorkspaceRoot)
		handshakeOpts.RootPath = opts.WorkspaceRoot
	}

	c.writeMu.Lock()
	err := WriteInitializeAndOpen(ctx, opts.ReadWriter, handshakeOpts)
	c.writeMu.Unlock()
	if err != nil {
		cancel()
		_ = opts.ReadWriter.Close()
		return nil, fmt.Errorf("lsp: initialize handshake: %w", err)
	}

	// Wait for the initialize response.
	select {
	case <-ctx.Done():
		cancel()
		mux.Unregister(idStr)
		_ = opts.ReadWriter.Close()
		return nil, fmt.Errorf("lsp: initialize timed out: %w", ctx.Err())
	case resp, ok := <-ch:
		if !ok {
			cancel()
			return nil, fmt.Errorf("lsp: initialize channel closed without response")
		}
		if resp.Error != nil {
			cancel()
			return nil, fmt.Errorf("lsp: initialize error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		// Capabilities are available in resp.Result; we ignore them for now
		// (a future session manager can inspect them).
	}

	return c, nil
}

// SendRequest sends a JSON-RPC request to the language server and waits for
// the response. It returns the raw JSON result or an error.
//
// filePath is informational only (used by callers to route to the right server
// in multi-language setups; the single-client implementation ignores it).
//
// Returns (nil, nil) only if ctx is cancelled and the channel receives no response.
func (c *ConcreteNavigationClient) SendRequest(ctx context.Context, _ string, method string, params any) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := NextRequestID()
	ch := c.mux.Register(id)

	msg := jsonRPCMessage{
		JSONRPC: "2.0",
		ID:      int(mustParseRequestID(id)),
		Method:  method,
		Params:  params,
	}

	c.writeMu.Lock()
	err := writeFramedJSON(c.transport, msg)
	c.writeMu.Unlock()
	if err != nil {
		c.mux.Unregister(id)
		return nil, fmt.Errorf("lsp: write request %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		c.mux.Unregister(id)
		return nil, fmt.Errorf("lsp: request %s: %w", method, ctx.Err())
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("lsp: response channel closed for %s", method)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("lsp: server error for %s (code %d): %s", method, resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// Close shuts down the client's background read loop and closes the transport.
// After Close, further calls to SendRequest will fail.
func (c *ConcreteNavigationClient) Close() error {
	c.cancel()
	err := c.transport.Close()
	<-c.done
	return err
}

// mustParseRequestID parses a JSON number request id to int64 for the message
// struct. It falls back to 0 on parse failure (should never happen since ids
// are generated by NextRequestID).
func mustParseRequestID(id json.Number) int64 {
	v, err := id.Int64()
	if err != nil {
		return 0
	}
	return v
}

// newBufioReader wraps r in a *bufio.Reader if it isn't one already.
func newBufioReader(r io.Reader) *bufio.Reader {
	if br, ok := r.(*bufio.Reader); ok {
		return br
	}
	return bufio.NewReader(r)
}

// LSPDiagnosticsFromSnapshot loads the current diagnostics snapshot from disk
// and returns it, optionally filtered.
//
// This is the wire-up point for TOOL-LSP-05 (LSPDiagnostics): the diagnostic
// tool reads from the snapshot path that the RPCMux notification handler
// writes publishDiagnostics payloads into (via ApplyPublishDiagnosticsSnapshot).
func LSPDiagnosticsFromSnapshot(snapshotPath string, filter Filter) ([]Diagnostic, bool, error) {
	if snapshotPath == "" {
		return nil, false, os.ErrInvalid
	}
	all, err := LoadSnapshot(snapshotPath)
	if err != nil {
		return nil, false, fmt.Errorf("lsp: load diagnostics snapshot: %w", err)
	}
	filtered, truncated := FilterDiagnostics(all, filter)
	return filtered, truncated, nil
}

// NewDiagnosticNotificationHandler returns a notification handler suitable for
// ConcreteNavigationClientOptions.DiagnosticHandler that writes
// publishDiagnostics payloads into the given snapshot file.
//
// Other notification types (e.g. window/logMessage) are silently discarded.
func NewDiagnosticNotificationHandler(snapshotPath string) func([]byte) {
	return func(payload []byte) {
		if snapshotPath == "" {
			return
		}
		// Only process publishDiagnostics.
		var envelope struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			return
		}
		if envelope.Method != "textDocument/publishDiagnostics" {
			return
		}
		_, _ = ApplyPublishDiagnosticsSnapshot(snapshotPath, payload)
	}
}
