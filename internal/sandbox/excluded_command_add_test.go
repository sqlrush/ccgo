package sandbox

// AddExcludedCommand and LocalExcludedCommandsStore tests (SBX-59).
// CC ref: src/utils/sandbox/sandbox-adapter.ts:addToExcludedCommands.

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeExcludedCommandsStore is a test-only in-memory ExcludedCommandsStore.
type fakeExcludedCommandsStore struct {
	commands []string
}

func (f *fakeExcludedCommandsStore) ReadExcludedCommands() ([]string, error) {
	return append([]string(nil), f.commands...), nil
}

func (f *fakeExcludedCommandsStore) WriteExcludedCommands(commands []string) error {
	f.commands = append([]string(nil), commands...)
	return nil
}

// TestAddExcludedCommandAppends verifies AddExcludedCommand appends a new command (SBX-59).
func TestAddExcludedCommandAppends(t *testing.T) {
	store := &fakeExcludedCommandsStore{}
	got, err := AddExcludedCommand(store, "npm run test")
	if err != nil {
		t.Fatalf("AddExcludedCommand error: %v", err)
	}
	if len(got) != 1 || got[0] != "npm run test" {
		t.Errorf("got %v want [npm run test]", got)
	}
	if len(store.commands) != 1 || store.commands[0] != "npm run test" {
		t.Errorf("store = %v want [npm run test]", store.commands)
	}
}

// TestAddExcludedCommandIdempotent verifies AddExcludedCommand does not duplicate (SBX-59).
func TestAddExcludedCommandIdempotent(t *testing.T) {
	store := &fakeExcludedCommandsStore{commands: []string{"make build"}}
	got, err := AddExcludedCommand(store, "make build")
	if err != nil {
		t.Fatalf("AddExcludedCommand error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("idempotent: expected 1 entry, got %v", got)
	}
	// WriteExcludedCommands should not have been called.
	if len(store.commands) != 1 {
		t.Errorf("store mutated on duplicate add: %v", store.commands)
	}
}

// TestAddExcludedCommandPreservesExisting verifies AddExcludedCommand preserves
// existing entries when appending a new one.
func TestAddExcludedCommandPreservesExisting(t *testing.T) {
	store := &fakeExcludedCommandsStore{commands: []string{"git", "make"}}
	got, err := AddExcludedCommand(store, "npm")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 entries, got %v", got)
	}
	if got[0] != "git" || got[1] != "make" || got[2] != "npm" {
		t.Errorf("order/values wrong: %v", got)
	}
}

// TestAddExcludedCommandEmptyReturnsError verifies AddExcludedCommand rejects empty command.
func TestAddExcludedCommandEmptyReturnsError(t *testing.T) {
	store := &fakeExcludedCommandsStore{}
	_, err := AddExcludedCommand(store, "")
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

// TestLocalExcludedCommandsStoreRoundTrip verifies LocalExcludedCommandsStore reads
// and writes sandbox.excludedCommands in a local settings file (SBX-59).
func TestLocalExcludedCommandsStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	store := LocalExcludedCommandsStore{SettingsPath: settingsPath}

	// Initially empty.
	initial, err := store.ReadExcludedCommands()
	if err != nil {
		t.Fatalf("ReadExcludedCommands: %v", err)
	}
	if len(initial) != 0 {
		t.Errorf("initial list should be empty, got %v", initial)
	}

	// Add via AddExcludedCommand.
	got, err := AddExcludedCommand(store, "make build")
	if err != nil {
		t.Fatalf("AddExcludedCommand: %v", err)
	}
	if len(got) != 1 || got[0] != "make build" {
		t.Errorf("AddExcludedCommand returned %v", got)
	}

	// Read back.
	readBack, err := store.ReadExcludedCommands()
	if err != nil {
		t.Fatalf("ReadExcludedCommands after write: %v", err)
	}
	if len(readBack) != 1 || readBack[0] != "make build" {
		t.Errorf("ReadExcludedCommands returned %v", readBack)
	}

	// Idempotent second add.
	got2, err := AddExcludedCommand(store, "make build")
	if err != nil {
		t.Fatalf("AddExcludedCommand idempotent: %v", err)
	}
	if len(got2) != 1 {
		t.Errorf("idempotent add should return 1 entry, got %v", got2)
	}
}

// TestLocalExcludedCommandsStorePreservesOtherSettings verifies that
// WriteExcludedCommands does not clobber other settings keys (SBX-59).
func TestLocalExcludedCommandsStorePreservesOtherSettings(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write initial document with other keys.
	initial := `{"theme":"dark","sandbox":{"enabled":true}}`
	if err := os.WriteFile(settingsPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store := LocalExcludedCommandsStore{SettingsPath: settingsPath}
	if _, err := AddExcludedCommand(store, "cargo build"); err != nil {
		t.Fatalf("AddExcludedCommand: %v", err)
	}

	// The "theme" key and sandbox.enabled must still be present.
	doc, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(doc)
	if !contains(s, "dark") {
		t.Errorf("theme key lost after WriteExcludedCommands; doc=%s", s)
	}
	if !contains(s, "enabled") {
		t.Errorf("sandbox.enabled lost after WriteExcludedCommands; doc=%s", s)
	}
	if !contains(s, "cargo build") {
		t.Errorf("cargo build not present after AddExcludedCommand; doc=%s", s)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
