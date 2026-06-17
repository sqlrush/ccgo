package integrations

import (
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
)

func TestSessionManifestPath(t *testing.T) {
	got := SessionManifestPath(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_1")
	want := filepath.Join("tmp", "sessions", "sess_1", manifestFileName)
	if got != want {
		t.Fatalf("SessionManifestPath() = %q, want %q", got, want)
	}
	if got := SessionManifestPath("", "sess_1"); got != "" {
		t.Fatalf("empty transcript path = %q, want empty", got)
	}
	if got := SessionManifestPath("session.jsonl", ""); got != "" {
		t.Fatalf("empty session id = %q, want empty", got)
	}
}

func TestBuildManifestMarksEnabledRuntimesAsNotWired(t *testing.T) {
	enabledValue := true
	disabledValue := false
	manifest := BuildManifest("sess_integrations", "/work", &contracts.AdvancedSetting{
		Chrome:      &enabledValue,
		ComputerUse: &enabledValue,
		Voice:       &disabledValue,
	})
	if manifest.SessionID != "sess_integrations" || manifest.WorkingDirectory != "/work" || manifest.GeneratedAt == "" {
		t.Fatalf("manifest metadata = %#v", manifest)
	}
	if len(manifest.Integrations) != 3 {
		t.Fatalf("integrations = %#v", manifest.Integrations)
	}
	if CountEnabled(manifest.Integrations) != 2 {
		t.Fatalf("enabled count = %d, integrations = %#v", CountEnabled(manifest.Integrations), manifest.Integrations)
	}
	if !hasIntegration(manifest.Integrations, "chrome", true, RuntimeStateNotWired) {
		t.Fatalf("chrome integration missing/not wired: %#v", manifest.Integrations)
	}
	if !hasIntegration(manifest.Integrations, "computer_use", true, RuntimeStateNotWired) {
		t.Fatalf("computer_use integration missing/not wired: %#v", manifest.Integrations)
	}
	if !hasIntegration(manifest.Integrations, "voice", false, RuntimeStateDisabled) {
		t.Fatalf("voice integration missing/disabled: %#v", manifest.Integrations)
	}
	counts := CountByRuntimeState(manifest.Integrations)
	if counts[RuntimeStateNotWired] != 2 || counts[RuntimeStateDisabled] != 1 {
		t.Fatalf("state counts = %#v", counts)
	}
}

func TestAnyEnabled(t *testing.T) {
	enabledValue := true
	disabledValue := false
	if AnyEnabled(nil) {
		t.Fatal("nil advanced setting enabled integrations")
	}
	if AnyEnabled(&contracts.AdvancedSetting{Chrome: &disabledValue, Voice: &disabledValue, ComputerUse: &disabledValue}) {
		t.Fatal("disabled advanced setting enabled integrations")
	}
	if !AnyEnabled(&contracts.AdvancedSetting{Voice: &enabledValue}) {
		t.Fatal("enabled voice not detected")
	}
}

func TestWriteAndLoadManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_integrations", manifestFileName)
	input := Manifest{
		SessionID:    "sess_integrations",
		Integrations: []Integration{{Name: "chrome", Enabled: true, RuntimeState: RuntimeStateNotWired}},
	}
	if err := WriteManifest(path, input); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_integrations" || loaded.GeneratedAt == "" || len(loaded.Integrations) != 1 {
		t.Fatalf("loaded manifest = %#v", loaded)
	}
}

func hasIntegration(integrations []Integration, name string, enabled bool, state string) bool {
	for _, integration := range integrations {
		if integration.Name == name && integration.Enabled == enabled && integration.RuntimeState == state {
			return true
		}
	}
	return false
}
