package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"ccgo/internal/contracts"
)

type StdioTransport struct {
	reader              *bufio.Reader
	readerCloser        io.Closer
	writer              io.Writer
	mu                  sync.Mutex
	notificationMu      sync.RWMutex
	notificationHandler RPCNotificationHandler
	requestMu           sync.RWMutex
	requestHandler      RPCRequestHandler
}

func NewStdioTransport(reader io.Reader, writer io.Writer) *StdioTransport {
	closer, _ := reader.(io.Closer)
	return &StdioTransport{
		reader:       bufio.NewReader(reader),
		readerCloser: closer,
		writer:       writer,
	}
}

func (t *StdioTransport) RoundTrip(ctx context.Context, request RPCRequest) (RPCResponse, error) {
	if t == nil || t.reader == nil || t.writer == nil {
		return RPCResponse{}, fmt.Errorf("mcp stdio transport is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return RPCResponse{}, err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return RPCResponse{}, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, err := t.writer.Write(append(data, '\n')); err != nil {
		return RPCResponse{}, err
	}
	stopReadWatch := t.watchReadContext(ctx)
	defer stopReadWatch()
	for {
		if err := ctx.Err(); err != nil {
			return RPCResponse{}, err
		}
		line, err := t.reader.ReadBytes('\n')
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return RPCResponse{}, ctxErr
			}
			return RPCResponse{}, err
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		var response RPCResponse
		if err := json.Unmarshal(trimmed, &response); err != nil {
			return RPCResponse{}, fmt.Errorf("decode mcp stdio response: %w", err)
		}
		if err := t.handleInboundRequest(ctx, response); err != nil {
			return RPCResponse{}, err
		}
		if _, ok := InboundRequestFromRPCResponse(response); ok {
			continue
		}
		if t.dispatchNotification(response) {
			continue
		}
		if response.ID == "" {
			continue
		}
		if response.ID != request.ID {
			continue
		}
		return response, nil
	}
}

func (t *StdioTransport) watchReadContext(ctx context.Context) func() {
	if t == nil || t.readerCloser == nil || ctx == nil || ctx.Done() == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = t.readerCloser.Close()
		case <-done:
		}
	}()
	return func() {
		close(done)
	}
}

func (t *StdioTransport) SendNotification(ctx context.Context, notification RPCNotification) error {
	if t == nil || t.writer == nil {
		return fmt.Errorf("mcp stdio transport is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if notification.JSONRPC == "" {
		notification.JSONRPC = JSONRPCVersion
	}
	data, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err = t.writer.Write(append(data, '\n'))
	return err
}

func (t *StdioTransport) SetRequestHandler(handler RPCRequestHandler) {
	if t == nil {
		return
	}
	t.requestMu.Lock()
	t.requestHandler = handler
	t.requestMu.Unlock()
}

func (t *StdioTransport) handleInboundRequest(ctx context.Context, response RPCResponse) error {
	request, ok := InboundRequestFromRPCResponse(response)
	if !ok {
		return nil
	}
	t.requestMu.RLock()
	handler := t.requestHandler
	t.requestMu.RUnlock()
	data, err := json.Marshal(ResponseForInboundRequest(ctx, request, handler))
	if err != nil {
		return err
	}
	_, err = t.writer.Write(append(data, '\n'))
	return err
}

func (t *StdioTransport) SetNotificationHandler(handler RPCNotificationHandler) {
	if t == nil {
		return
	}
	t.notificationMu.Lock()
	t.notificationHandler = handler
	t.notificationMu.Unlock()
}

func (t *StdioTransport) dispatchNotification(response RPCResponse) bool {
	notification, ok := NotificationFromRPCResponse(response)
	if !ok {
		return false
	}
	t.notificationMu.RLock()
	handler := t.notificationHandler
	t.notificationMu.RUnlock()
	if handler != nil {
		handler(notification)
	}
	return true
}

type StdioProcessTransport struct {
	*StdioTransport
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *bytes.Buffer
}

func StartStdioTransport(ctx context.Context, server contracts.MCPServer) (*StdioProcessTransport, error) {
	command, ok := ServerCommandArray(server)
	if !ok {
		return nil, fmt.Errorf("mcp stdio server command is required")
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Env = stdioEnv(server.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	return &StdioProcessTransport{
		StdioTransport: NewStdioTransport(stdout, stdin),
		cmd:            cmd,
		stdin:          stdin,
		stderr:         &stderr,
	}, nil
}

func (t *StdioProcessTransport) Close() error {
	if t == nil {
		return nil
	}
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	if t.cmd.ProcessState == nil || !t.cmd.ProcessState.Exited() {
		_ = t.cmd.Process.Kill()
	}
	return t.cmd.Wait()
}

func (t *StdioProcessTransport) StderrString() string {
	if t == nil || t.stderr == nil {
		return ""
	}
	return t.stderr.String()
}

func stdioEnv(env map[string]string) []string {
	out := os.Environ()
	if len(env) == 0 {
		return out
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}
