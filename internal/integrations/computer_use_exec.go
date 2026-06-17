package integrations

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	defaultComputerUseCommandTimeout = 5 * time.Second
	defaultComputerUseMaxBytes       = 10 * 1024 * 1024
)

type ComputerUseCommandRunner func(ctx context.Context, command []string, stdin string, maxBytes int64) ([]byte, bool, error)

type ComputerUseExecutionOptions struct {
	Timeout  time.Duration
	MaxBytes int64
	Runner   ComputerUseCommandRunner
}

type ComputerUseScreenshotResult struct {
	AdapterName string   `json:"adapter_name,omitempty"`
	AdapterKind string   `json:"adapter_kind,omitempty"`
	Command     []string `json:"command,omitempty"`
	Format      string   `json:"format"`
	Bytes       int      `json:"bytes"`
	Truncated   bool     `json:"truncated,omitempty"`
	Skipped     bool     `json:"skipped,omitempty"`
	Detail      string   `json:"detail,omitempty"`
	Image       []byte   `json:"-"`
}

type ComputerUseInputAction struct {
	Type        string `json:"type"`
	X           int    `json:"x,omitempty"`
	Y           int    `json:"y,omitempty"`
	HasPosition bool   `json:"has_position,omitempty"`
	Button      int    `json:"button,omitempty"`
	Text        string `json:"text,omitempty"`
	Key         string `json:"key,omitempty"`
}

type ComputerUseInputResult struct {
	AdapterName string   `json:"adapter_name,omitempty"`
	AdapterKind string   `json:"adapter_kind,omitempty"`
	Command     []string `json:"command,omitempty"`
	ActionType  string   `json:"action_type,omitempty"`
	Skipped     bool     `json:"skipped,omitempty"`
	Detail      string   `json:"detail,omitempty"`
}

func CaptureComputerUseScreenshot(ctx context.Context, plan ComputerUseDriverPlan, options ComputerUseExecutionOptions) (ComputerUseScreenshotResult, error) {
	result := ComputerUseScreenshotResult{
		AdapterName: plan.ScreenCaptureAdapter.Name,
		AdapterKind: plan.ScreenCaptureAdapter.Kind,
		Command:     append([]string(nil), plan.ScreenCaptureAdapter.Command...),
		Format:      plan.ScreenshotFormat,
	}
	if result.Format == "" {
		result.Format = "png"
	}
	if !plan.ScreenCaptureAdapter.Available || len(plan.ScreenCaptureAdapter.Command) == 0 {
		result.Skipped = true
		result.Detail = "no executable screen capture adapter is available"
		return result, nil
	}
	runner := options.Runner
	if runner == nil {
		runner = DefaultComputerUseCommandRunner
	}
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultComputerUseMaxBytes
	}
	ctx, cancel := computerUseCommandContext(ctx, options.Timeout)
	defer cancel()
	image, truncated, err := runner(ctx, plan.ScreenCaptureAdapter.Command, "", maxBytes)
	result.Image = image
	result.Bytes = len(image)
	result.Truncated = truncated
	if err != nil {
		result.Detail = err.Error()
		return result, err
	}
	return result, nil
}

func ExecuteComputerUseInput(ctx context.Context, plan ComputerUseDriverPlan, action ComputerUseInputAction, options ComputerUseExecutionOptions) (ComputerUseInputResult, error) {
	result := ComputerUseInputResult{
		AdapterName: plan.InputControlAdapter.Name,
		AdapterKind: plan.InputControlAdapter.Kind,
		ActionType:  normalizeComputerUseActionType(action.Type),
	}
	if !plan.InputControlAdapter.Available || len(plan.InputControlAdapter.Command) == 0 {
		result.Skipped = true
		result.Detail = "no executable input control adapter is available"
		return result, nil
	}
	command, err := BuildComputerUseInputCommand(plan.InputControlAdapter, action)
	result.Command = command
	if err != nil {
		result.Skipped = true
		result.Detail = err.Error()
		return result, err
	}
	runner := options.Runner
	if runner == nil {
		runner = DefaultComputerUseCommandRunner
	}
	ctx, cancel := computerUseCommandContext(ctx, options.Timeout)
	defer cancel()
	if _, _, err := runner(ctx, command, "", 64*1024); err != nil {
		result.Detail = err.Error()
		return result, err
	}
	return result, nil
}

func BuildComputerUseInputCommand(adapter Adapter, action ComputerUseInputAction) ([]string, error) {
	if len(adapter.Command) == 0 {
		return nil, os.ErrInvalid
	}
	base := append([]string(nil), adapter.Command...)
	switch strings.ToLower(strings.TrimSpace(adapter.Name)) {
	case "xdotool":
		return buildXdotoolCommand(base, action)
	default:
		return base, fmt.Errorf("computer-use input adapter %q does not support semantic actions yet", adapter.Name)
	}
}

func DefaultComputerUseCommandRunner(ctx context.Context, command []string, stdin string, maxBytes int64) ([]byte, bool, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return nil, false, os.ErrInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if maxBytes <= 0 {
		maxBytes = defaultComputerUseMaxBytes
	}
	stdout := &limitedBuffer{max: int(maxBytes)}
	stderr := &limitedBuffer{max: 64 * 1024}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return append([]byte(nil), stdout.Bytes()...), stdout.truncated, fmt.Errorf("%w: %s", err, detail)
		}
		return append([]byte(nil), stdout.Bytes()...), stdout.truncated, err
	}
	return append([]byte(nil), stdout.Bytes()...), stdout.truncated, nil
}

func buildXdotoolCommand(base []string, action ComputerUseInputAction) ([]string, error) {
	switch normalizeComputerUseActionType(action.Type) {
	case "move":
		if !action.HasPosition {
			return base, fmt.Errorf("move action requires a position")
		}
		return append(base, "mousemove", strconv.Itoa(action.X), strconv.Itoa(action.Y)), nil
	case "click":
		button := action.Button
		if button <= 0 {
			button = 1
		}
		if action.HasPosition {
			base = append(base, "mousemove", strconv.Itoa(action.X), strconv.Itoa(action.Y))
		}
		return append(base, "click", strconv.Itoa(button)), nil
	case "type":
		if action.Text == "" {
			return base, fmt.Errorf("type action requires text")
		}
		return append(base, "type", "--", action.Text), nil
	case "key":
		key := strings.TrimSpace(action.Key)
		if key == "" {
			return base, fmt.Errorf("key action requires a key")
		}
		return append(base, "key", key), nil
	default:
		return base, fmt.Errorf("unsupported computer-use input action %q", action.Type)
	}
}

func normalizeComputerUseActionType(actionType string) string {
	actionType = strings.TrimSpace(strings.ToLower(actionType))
	actionType = strings.ReplaceAll(actionType, "_", "-")
	switch actionType {
	case "mousemove", "mouse-move", "pointer-move":
		return "move"
	case "mouse-click", "left-click", "right-click":
		return "click"
	case "text", "type-text":
		return "type"
	case "keypress", "key-press":
		return "key"
	default:
		return actionType
	}
}

func computerUseCommandContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	if timeout <= 0 {
		timeout = defaultComputerUseCommandTimeout
	}
	return context.WithTimeout(ctx, timeout)
}
