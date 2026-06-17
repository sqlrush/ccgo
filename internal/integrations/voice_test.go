package integrations

import (
	"path/filepath"
	"testing"
)

func TestVoiceCapturePlanPath(t *testing.T) {
	got := VoiceCapturePlanPath(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_voice")
	want := filepath.Join("tmp", "sessions", "sess_voice", voiceCapturePlanFileName)
	if got != want {
		t.Fatalf("VoiceCapturePlanPath() = %q, want %q", got, want)
	}
	if got := VoiceCapturePlanPath("", "sess_voice"); got != "" {
		t.Fatalf("empty transcript path = %q, want empty", got)
	}
}

func TestVoiceCapturePlanWriteLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_voice", voiceCapturePlanFileName)
	plan := BuildVoiceCapturePlan("sess_voice", "/work", []Adapter{{
		Name:      "pw-record",
		Kind:      AdapterKindAudioCapture,
		Available: true,
		Command:   []string{"/usr/bin/pw-record", "--target", "default", "-"},
	}})
	if err := WriteVoiceCapturePlan(path, plan); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadVoiceCapturePlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_voice" || loaded.WorkingDirectory != "/work" || loaded.GeneratedAt == "" {
		t.Fatalf("loaded metadata = %#v", loaded)
	}
	if loaded.Adapter.Name != "pw-record" || !loaded.Adapter.Available {
		t.Fatalf("loaded adapter = %#v", loaded.Adapter)
	}
	if loaded.SampleRateHz != 16000 || loaded.Channels != 1 || loaded.Encoding != "pcm_s16le" || !loaded.Streaming {
		t.Fatalf("loaded plan = %#v", loaded)
	}
}

func TestVoiceCapturePlanFallsBackToUnavailableAdapter(t *testing.T) {
	plan := BuildVoiceCapturePlan("sess_voice", "/work", nil)
	if plan.Adapter.Name != "audio-capture" || plan.Adapter.Available {
		t.Fatalf("fallback adapter = %#v", plan.Adapter)
	}
}
