package bridge

import (
	"path/filepath"
	"testing"

	"ccgo/internal/commands"
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

func TestBuildManifestIncludesOnlyBridgeSafeCommands(t *testing.T) {
	registry := commands.FromSources(commands.Sources{Builtins: []contracts.Command{
		{Name: "ask", Type: contracts.CommandPrompt, Source: contracts.CommandSourceSkills, LoadedFrom: "skills", Aliases: []string{"question"}},
		{Name: "compact", Type: contracts.CommandLocal, Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
		{Name: "status", Type: contracts.CommandLocalJSX, Source: contracts.CommandSourceBuiltin},
		{Name: "model", Type: contracts.CommandLocal, Source: contracts.CommandSourceBuiltin},
	}})
	manifest := BuildManifest("sess_bridge", "/work", registry)
	if manifest.SessionID != "sess_bridge" || manifest.WorkingDirectory != "/work" || manifest.GeneratedAt == "" {
		t.Fatalf("manifest metadata = %#v", manifest)
	}
	if len(manifest.Commands) != 2 {
		t.Fatalf("commands = %#v", manifest.Commands)
	}
	if manifest.Commands[0].Name != "ask" || manifest.Commands[1].Name != "compact" {
		t.Fatalf("commands = %#v", manifest.Commands)
	}
	if len(manifest.Commands[0].Aliases) != 1 || manifest.Commands[0].Aliases[0] != "question" {
		t.Fatalf("aliases = %#v", manifest.Commands[0].Aliases)
	}
}

func TestWriteAndLoadManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_bridge", manifestFileName)
	input := Manifest{
		SessionID:        "sess_bridge",
		WorkingDirectory: "/work",
		Commands:         []Command{{Name: "compact", Type: contracts.CommandLocal}},
	}
	if err := WriteManifest(path, input); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_bridge" || loaded.GeneratedAt == "" || len(loaded.Commands) != 1 || loaded.Commands[0].Name != "compact" {
		t.Fatalf("loaded manifest = %#v", loaded)
	}
}
