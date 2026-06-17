package integrations

import (
	"context"
	"errors"
	"testing"
)

func TestCaptureComputerUseScreenshotRunsCaptureCommand(t *testing.T) {
	plan := BuildComputerUseDriverPlan("sess_computer", "/work", []Adapter{
		{Name: "grim", Kind: AdapterKindScreenCapture, Available: true, Command: []string{"/usr/bin/grim", "-"}},
		{Name: "ydotool", Kind: AdapterKindInputControl, Available: false},
	})
	var gotCommand []string
	result, err := CaptureComputerUseScreenshot(context.Background(), plan, ComputerUseExecutionOptions{
		MaxBytes: 8,
		Runner: func(ctx context.Context, command []string, stdin string, maxBytes int64) ([]byte, bool, error) {
			gotCommand = append([]string(nil), command...)
			if stdin != "" || maxBytes != 8 {
				t.Fatalf("stdin=%q max=%d", stdin, maxBytes)
			}
			return []byte{0x89, 'P', 'N', 'G'}, false, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AdapterName != "grim" || result.Format != "png" || result.Bytes != 4 || result.Truncated {
		t.Fatalf("result = %#v", result)
	}
	if len(gotCommand) != 2 || gotCommand[0] != "/usr/bin/grim" || gotCommand[1] != "-" {
		t.Fatalf("command = %#v", gotCommand)
	}
}

func TestCaptureComputerUseScreenshotSkipsUnavailableAdapter(t *testing.T) {
	result, err := CaptureComputerUseScreenshot(context.Background(), BuildComputerUseDriverPlan("sess_computer", "/work", nil), ComputerUseExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped || result.Bytes != 0 || result.Detail == "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteComputerUseInputBuildsXdotoolClick(t *testing.T) {
	plan := BuildComputerUseDriverPlan("sess_computer", "/work", []Adapter{
		{Name: "screen-capture", Kind: AdapterKindScreenCapture, Available: false},
		{Name: "xdotool", Kind: AdapterKindInputControl, Available: true, Command: []string{"/usr/bin/xdotool"}},
	})
	var gotCommand []string
	result, err := ExecuteComputerUseInput(context.Background(), plan, ComputerUseInputAction{
		Type:        "click",
		X:           10,
		Y:           20,
		HasPosition: true,
		Button:      1,
	}, ComputerUseExecutionOptions{
		Runner: func(ctx context.Context, command []string, stdin string, maxBytes int64) ([]byte, bool, error) {
			gotCommand = append([]string(nil), command...)
			return nil, false, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/bin/xdotool", "mousemove", "10", "20", "click", "1"}
	if !sameStrings(gotCommand, want) {
		t.Fatalf("command = %#v, want %#v", gotCommand, want)
	}
	if result.ActionType != "click" || result.Skipped {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteComputerUseInputReturnsRunnerErrors(t *testing.T) {
	wantErr := errors.New("input failed")
	plan := BuildComputerUseDriverPlan("sess_computer", "/work", []Adapter{
		{Name: "screen-capture", Kind: AdapterKindScreenCapture, Available: false},
		{Name: "xdotool", Kind: AdapterKindInputControl, Available: true, Command: []string{"/usr/bin/xdotool"}},
	})
	result, err := ExecuteComputerUseInput(context.Background(), plan, ComputerUseInputAction{Type: "key", Key: "Escape"}, ComputerUseExecutionOptions{
		Runner: func(context.Context, []string, string, int64) ([]byte, bool, error) {
			return nil, false, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if result.Detail == "" || result.Skipped {
		t.Fatalf("result = %#v", result)
	}
}

func TestBuildComputerUseInputCommandRejectsUnsupportedAdapter(t *testing.T) {
	_, err := BuildComputerUseInputCommand(Adapter{Name: "osascript", Command: []string{"/usr/bin/osascript"}}, ComputerUseInputAction{Type: "click"})
	if err == nil {
		t.Fatal("expected unsupported adapter error")
	}
}

func sameStrings(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
