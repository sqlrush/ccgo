package integrations

import (
	"path/filepath"
	"testing"
)

func TestComputerUseDriverPlanPath(t *testing.T) {
	got := ComputerUseDriverPlanPath(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_computer")
	want := filepath.Join("tmp", "sessions", "sess_computer", computerUseDriverPlanFileName)
	if got != want {
		t.Fatalf("ComputerUseDriverPlanPath() = %q, want %q", got, want)
	}
	if got := ComputerUseDriverPlanPath("", "sess_computer"); got != "" {
		t.Fatalf("empty transcript path = %q, want empty", got)
	}
}

func TestComputerUseDriverPlanWriteLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_computer", computerUseDriverPlanFileName)
	plan := BuildComputerUseDriverPlan("sess_computer", "/work", []Adapter{
		{Name: "grim", Kind: AdapterKindScreenCapture, Available: true, Command: []string{"/usr/bin/grim", "-"}},
		{Name: "ydotool", Kind: AdapterKindInputControl, Available: true, Command: []string{"/usr/bin/ydotool"}},
	})
	if err := WriteComputerUseDriverPlan(path, plan); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadComputerUseDriverPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_computer" || loaded.WorkingDirectory != "/work" || loaded.GeneratedAt == "" {
		t.Fatalf("loaded metadata = %#v", loaded)
	}
	if loaded.ScreenCaptureAdapter.Name != "grim" || loaded.InputControlAdapter.Name != "ydotool" {
		t.Fatalf("loaded adapters = %#v %#v", loaded.ScreenCaptureAdapter, loaded.InputControlAdapter)
	}
	if loaded.ScreenshotFormat != "png" || loaded.CoordinateSystem != "screen_pixels" || loaded.ExecutionMode != "planned" {
		t.Fatalf("loaded plan = %#v", loaded)
	}
}

func TestComputerUseDriverPlanFallsBackToUnavailableAdapters(t *testing.T) {
	plan := BuildComputerUseDriverPlan("sess_computer", "/work", nil)
	if plan.ScreenCaptureAdapter.Available || plan.ScreenCaptureAdapter.Kind != AdapterKindScreenCapture {
		t.Fatalf("screen fallback = %#v", plan.ScreenCaptureAdapter)
	}
	if plan.InputControlAdapter.Available || plan.InputControlAdapter.Kind != AdapterKindInputControl {
		t.Fatalf("input fallback = %#v", plan.InputControlAdapter)
	}
}
