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
	case "osascript":
		return buildOsaScriptCommand(base, action)
	case "powershell.exe", "powershell", "pwsh":
		return buildPowerShellInputCommand(base, action)
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

func buildOsaScriptCommand(base []string, action ComputerUseInputAction) ([]string, error) {
	switch normalizeComputerUseActionType(action.Type) {
	case "move":
		return base, fmt.Errorf("macOS osascript adapter does not support move without a click")
	case "click":
		button := action.Button
		if button <= 0 {
			button = 1
		}
		if button != 1 {
			return base, fmt.Errorf("macOS osascript adapter only supports primary-button click")
		}
		if !action.HasPosition {
			return base, fmt.Errorf("click action requires a position for macOS osascript")
		}
		return append(base, "-e", fmt.Sprintf(`tell application "System Events" to click at {%d, %d}`, action.X, action.Y)), nil
	case "type":
		if action.Text == "" {
			return base, fmt.Errorf("type action requires text")
		}
		return append(base, "-e", `tell application "System Events" to keystroke `+appleScriptString(action.Text)), nil
	case "key":
		key := strings.TrimSpace(action.Key)
		if key == "" {
			return base, fmt.Errorf("key action requires a key")
		}
		if code, ok := macOSKeyCode(key); ok {
			return append(base, "-e", fmt.Sprintf(`tell application "System Events" to key code %d`, code)), nil
		}
		return append(base, "-e", `tell application "System Events" to keystroke `+appleScriptString(key)), nil
	default:
		return base, fmt.Errorf("unsupported computer-use input action %q", action.Type)
	}
}

func buildPowerShellInputCommand(base []string, action ComputerUseInputAction) ([]string, error) {
	switch normalizeComputerUseActionType(action.Type) {
	case "move":
		if !action.HasPosition {
			return base, fmt.Errorf("move action requires a position")
		}
		return append(base, powerShellInputPrelude()+fmt.Sprintf("[NativeInput]::SetCursorPos(%d,%d) | Out-Null", action.X, action.Y)), nil
	case "click":
		button := action.Button
		if button <= 0 {
			button = 1
		}
		down, up, err := windowsMouseButtonFlags(button)
		if err != nil {
			return base, err
		}
		script := powerShellInputPrelude()
		if action.HasPosition {
			script += fmt.Sprintf("[NativeInput]::SetCursorPos(%d,%d) | Out-Null;", action.X, action.Y)
		}
		script += fmt.Sprintf("[NativeInput]::mouse_event(%d,0,0,0,[UIntPtr]::Zero);[NativeInput]::mouse_event(%d,0,0,0,[UIntPtr]::Zero)", down, up)
		return append(base, script), nil
	case "type":
		if action.Text == "" {
			return base, fmt.Errorf("type action requires text")
		}
		return append(base, "Add-Type -AssemblyName System.Windows.Forms;[System.Windows.Forms.SendKeys]::SendWait("+powerShellSingleQuoted(sendKeysEscape(action.Text))+")"), nil
	case "key":
		key := strings.TrimSpace(action.Key)
		if key == "" {
			return base, fmt.Errorf("key action requires a key")
		}
		return append(base, "Add-Type -AssemblyName System.Windows.Forms;[System.Windows.Forms.SendKeys]::SendWait("+powerShellSingleQuoted(windowsSendKey(key))+")"), nil
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

func appleScriptString(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func macOSKeyCode(key string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "escape", "esc":
		return 53, true
	case "enter", "return":
		return 36, true
	case "tab":
		return 48, true
	case "space":
		return 49, true
	case "backspace", "delete":
		return 51, true
	case "forward-delete", "delete-forward":
		return 117, true
	case "left", "arrow-left":
		return 123, true
	case "right", "arrow-right":
		return 124, true
	case "down", "arrow-down":
		return 125, true
	case "up", "arrow-up":
		return 126, true
	default:
		return 0, false
	}
}

func powerShellInputPrelude() string {
	return `Add-Type -TypeDefinition 'using System; using System.Runtime.InteropServices; public static class NativeInput { [DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y); [DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, UIntPtr dwExtraInfo); }';`
}

func windowsMouseButtonFlags(button int) (int, int, error) {
	switch button {
	case 1:
		return 0x0002, 0x0004, nil
	case 2:
		return 0x0020, 0x0040, nil
	case 3:
		return 0x0008, 0x0010, nil
	default:
		return 0, 0, fmt.Errorf("unsupported Windows mouse button %d", button)
	}
}

func powerShellSingleQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func sendKeysEscape(value string) string {
	replacer := strings.NewReplacer(
		"+", "{+}",
		"^", "{^}",
		"%", "{%}",
		"~", "{~}",
		"(", "{(}",
		")", "{)}",
		"{", "{{}",
		"}", "{}}",
		"[", "{[}",
		"]", "{]}",
	)
	return replacer.Replace(value)
}

func windowsSendKey(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "escape", "esc":
		return "{ESC}"
	case "enter", "return":
		return "{ENTER}"
	case "tab":
		return "{TAB}"
	case "space":
		return " "
	case "backspace":
		return "{BACKSPACE}"
	case "delete":
		return "{DELETE}"
	case "left", "arrow-left":
		return "{LEFT}"
	case "right", "arrow-right":
		return "{RIGHT}"
	case "down", "arrow-down":
		return "{DOWN}"
	case "up", "arrow-up":
		return "{UP}"
	default:
		return sendKeysEscape(key)
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
