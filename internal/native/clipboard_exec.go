package native

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"ccgo/internal/contracts"
)

const defaultClipboardCommandTimeout = 5 * time.Second

type ClipboardCommandRunner func(ctx context.Context, command []string, stdin string) (string, error)

type ClipboardCommandResult struct {
	AdapterName string   `json:"adapter_name,omitempty"`
	AdapterKind string   `json:"adapter_kind,omitempty"`
	Command     []string `json:"command,omitempty"`
	External    bool     `json:"external"`
	Skipped     bool     `json:"skipped,omitempty"`
	Detail      string   `json:"detail,omitempty"`
}

func WriteClipboardTextWithAdapters(ctx context.Context, path string, sessionID contracts.ID, selection string, text string, adapters []ClipboardAdapter, runner ClipboardCommandRunner) (ClipboardState, ClipboardCommandResult, error) {
	state, err := WriteClipboardText(path, sessionID, selection, text)
	if err != nil {
		return ClipboardState{}, ClipboardCommandResult{}, err
	}
	adapter, ok := selectClipboardAdapter(adapters, true)
	if !ok {
		return state, ClipboardCommandResult{
			Skipped: true,
			Detail:  "no writable system or multiplexer clipboard adapter is available",
		}, nil
	}
	if runner == nil {
		runner = DefaultClipboardCommandRunner
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := ClipboardCommandResult{
		AdapterName: adapter.Name,
		AdapterKind: adapter.Kind,
		Command:     append([]string(nil), adapter.WriteCommand...),
		External:    true,
	}
	if _, err := runner(ctx, adapter.WriteCommand, text); err != nil {
		result.Detail = err.Error()
		return state, result, err
	}
	return state, result, nil
}

func ReadClipboardTextWithAdapters(ctx context.Context, path string, selection string, adapters []ClipboardAdapter, runner ClipboardCommandRunner) (string, bool, ClipboardCommandResult, error) {
	adapter, ok := selectClipboardAdapter(adapters, false)
	if !ok {
		text, found, err := ReadClipboardText(path, selection)
		return text, found, ClipboardCommandResult{
			Skipped: true,
			Detail:  "no readable system or multiplexer clipboard adapter is available; returned session clipboard state",
		}, err
	}
	if runner == nil {
		runner = DefaultClipboardCommandRunner
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := ClipboardCommandResult{
		AdapterName: adapter.Name,
		AdapterKind: adapter.Kind,
		Command:     append([]string(nil), adapter.ReadCommand...),
		External:    true,
	}
	text, err := runner(ctx, adapter.ReadCommand, "")
	if err != nil {
		result.Detail = err.Error()
		return "", false, result, err
	}
	return text, true, result, nil
}

func DefaultClipboardCommandRunner(ctx context.Context, command []string, stdin string) (string, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return "", os.ErrInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultClipboardCommandTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("%w: %s", err, detail)
		}
		return "", err
	}
	return stdout.String(), nil
}

func selectClipboardAdapter(adapters []ClipboardAdapter, write bool) (ClipboardAdapter, bool) {
	for _, kind := range []string{ClipboardAdapterKindSystem, ClipboardAdapterKindMultiplexer} {
		for _, adapter := range adapters {
			if !adapter.Available || adapter.Kind != kind {
				continue
			}
			if write && len(adapter.WriteCommand) > 0 {
				return adapter, true
			}
			if !write && len(adapter.ReadCommand) > 0 {
				return adapter, true
			}
		}
	}
	return ClipboardAdapter{}, false
}
