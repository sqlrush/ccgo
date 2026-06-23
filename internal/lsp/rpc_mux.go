package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// RPCMux multiplexes an LSP stdout stream: it routes JSON-RPC responses (by id)
// to waiting callers and forwards notifications/other messages to a handler.
// It is safe for concurrent use.
type RPCMux struct {
	mu      sync.Mutex
	pending map[json.Number]chan rpcResponse

	// NotificationHandler receives every non-response message (notifications,
	// server-initiated requests). When nil the messages are silently discarded.
	NotificationHandler func(payload []byte)
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewRPCMux creates an idle RPCMux. Call Run to start processing messages.
func NewRPCMux() *RPCMux {
	return &RPCMux{pending: make(map[json.Number]chan rpcResponse)}
}

// Run reads framed LSP messages from r until EOF or ctx is cancelled.
// It must be called in a dedicated goroutine. Errors are returned when the
// loop exits (typically io.EOF or context.Canceled).
func (m *RPCMux) Run(ctx context.Context, r io.Reader, frameLimit int64) error {
	if frameLimit <= 0 {
		frameLimit = defaultFrameLimit
	}
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		payload, err := ReadFramedMessage(br, frameLimit)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		m.dispatch(payload)
	}
}

// dispatch routes a single LSP payload to the right consumer.
func (m *RPCMux) dispatch(payload []byte) {
	var envelope struct {
		ID     json.Number     `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return
	}

	// If there's an id and no method, this is a response.
	if envelope.ID != "" && envelope.Method == "" {
		m.mu.Lock()
		ch, ok := m.pending[envelope.ID]
		if ok {
			delete(m.pending, envelope.ID)
		}
		m.mu.Unlock()
		if ok {
			ch <- rpcResponse{Result: envelope.Result, Error: envelope.Error}
			close(ch)
		}
		return
	}

	// Otherwise it is a notification or a server-initiated request.
	if m.NotificationHandler != nil {
		m.NotificationHandler(payload)
	}
}

// Register creates a response channel for the given request id.
// It must be called before writing the corresponding request to stdin.
func (m *RPCMux) Register(id json.Number) chan rpcResponse {
	ch := make(chan rpcResponse, 1)
	m.mu.Lock()
	m.pending[id] = ch
	m.mu.Unlock()
	return ch
}

// Unregister removes a pending registration without receiving (for cleanup).
func (m *RPCMux) Unregister(id json.Number) {
	m.mu.Lock()
	delete(m.pending, id)
	m.mu.Unlock()
}

// rpcRequestID is a monotonic counter for JSON-RPC request ids, starting at 100
// to avoid collisions with the server's initialize id (which is typically 1).
var rpcRequestID atomic.Int64

func init() {
	rpcRequestID.Store(100)
}

// NextRequestID returns the next unique JSON-RPC request id as a string.
func NextRequestID() json.Number {
	return json.Number(fmt.Sprintf("%d", rpcRequestID.Add(1)))
}
