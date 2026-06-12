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
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex
}

func NewStdioTransport(reader io.Reader, writer io.Writer) *StdioTransport {
	return &StdioTransport{
		reader: bufio.NewReader(reader),
		writer: writer,
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
	for {
		if err := ctx.Err(); err != nil {
			return RPCResponse{}, err
		}
		line, err := t.reader.ReadBytes('\n')
		if err != nil {
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
		if response.ID == "" {
			continue
		}
		if response.ID != request.ID {
			continue
		}
		return response, nil
	}
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
