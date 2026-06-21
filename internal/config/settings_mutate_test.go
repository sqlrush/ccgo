package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSetSettingsValueInDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"sonnet"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetSettingsValue(path, "theme", "dark"); err != nil {
		t.Fatalf("SetSettingsValue err: %v", err)
	}
	raw, _ := os.ReadFile(path)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["theme"] != "dark" || got["model"] != "sonnet" {
		t.Fatalf("merged doc = %v; want theme=dark, model preserved", got)
	}
}

func TestSetSettingsValueCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	if err := SetSettingsValue(path, "editorMode", "vim"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestSetSettingsValueNoClobber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	initial := `{"model":"claude-3-5-sonnet","theme":"light","permissions":{"allow":["Bash"]}}`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write only editorMode — must preserve all other keys
	if err := SetSettingsValue(path, "editorMode", "vim"); err != nil {
		t.Fatalf("SetSettingsValue err: %v", err)
	}
	raw, _ := os.ReadFile(path)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["editorMode"] != "vim" {
		t.Fatalf("editorMode not written: %v", got)
	}
	if got["model"] != "claude-3-5-sonnet" {
		t.Fatalf("model clobbered: got %v", got["model"])
	}
	if got["theme"] != "light" {
		t.Fatalf("theme clobbered: got %v", got["theme"])
	}
	perms, ok := got["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions missing or wrong type: %v", got["permissions"])
	}
	allow, ok := perms["allow"].([]any)
	if !ok || len(allow) != 1 || allow[0] != "Bash" {
		t.Fatalf("permissions.allow clobbered: %v", perms["allow"])
	}
}

func TestSetSettingsValueDeleteKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"sonnet","theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// nil value should delete the key
	if err := SetSettingsValue(path, "theme", nil); err != nil {
		t.Fatalf("SetSettingsValue err: %v", err)
	}
	raw, _ := os.ReadFile(path)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if _, exists := got["theme"]; exists {
		t.Fatalf("theme key should have been deleted: %v", got)
	}
	if got["model"] != "sonnet" {
		t.Fatalf("model clobbered: got %v", got["model"])
	}
}

func TestSetSettingsValueEmptyKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := SetSettingsValue(path, "", "value"); err == nil {
		t.Fatal("expected error for empty key")
	}
}
