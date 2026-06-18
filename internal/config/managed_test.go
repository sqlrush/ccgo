package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadPolicySettingsMergesManagedFileDropIns(t *testing.T) {
	root := t.TempDir()
	writeTestSettingsFile(t, filepath.Join(root, "managed-settings.json"), `{
		"model": "sonnet",
		"env": {"A": "1"},
		"permissions": {"allow": ["Read"]}
	}`)
	dropInDir := filepath.Join(root, "managed-settings.d")
	writeTestSettingsFile(t, filepath.Join(dropInDir, "20-security.json"), `{
		"permissions": {"deny": ["Bash(rm *)"]}
	}`)
	writeTestSettingsFile(t, filepath.Join(dropInDir, "10-model.json"), `{
		"model": "opus",
		"env": {"B": "2"}
	}`)
	writeTestSettingsFile(t, filepath.Join(dropInDir, ".hidden.json"), `{"model":"hidden"}`)
	writeTestSettingsFile(t, filepath.Join(dropInDir, "ignored.txt"), `{"model":"ignored"}`)

	settings, err := LoadPolicySettingsWithOptions(ManagedPolicyOptions{
		GOOS:       "linux",
		ManagedDir: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "opus" {
		t.Fatalf("model = %q", settings.Model)
	}
	if settings.Env["A"] != "1" || settings.Env["B"] != "2" {
		t.Fatalf("env = %#v", settings.Env)
	}
	if settings.Permissions == nil || !slices.Equal(settings.Permissions.Allow, []string{"Read"}) || !slices.Equal(settings.Permissions.Deny, []string{"Bash(rm *)"}) {
		t.Fatalf("permissions = %#v", settings.Permissions)
	}
}

func TestLoadPolicySettingsAdminSourceWinsOverFile(t *testing.T) {
	root := t.TempDir()
	writeTestSettingsFile(t, filepath.Join(root, "managed-settings.json"), `{"model":"file"}`)
	var commands []string
	settings, err := LoadPolicySettingsWithOptions(ManagedPolicyOptions{
		GOOS:       "darwin",
		Username:   "alice",
		ManagedDir: root,
		RunCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return []byte(`{"model":"mdm","env":{"SOURCE":"plist"}}`), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "mdm" || settings.Env["SOURCE"] != "plist" {
		t.Fatalf("settings = %#v", settings)
	}
	if len(commands) != 1 || !strings.Contains(commands[0], "/Library/Managed Preferences/alice/com.anthropic.claudecode.plist") {
		t.Fatalf("commands = %#v", commands)
	}
}

func TestLoadPolicySettingsFileWinsOverHKCU(t *testing.T) {
	root := t.TempDir()
	writeTestSettingsFile(t, filepath.Join(root, "managed-settings.json"), `{"model":"file"}`)
	settings, err := LoadPolicySettingsWithOptions(ManagedPolicyOptions{
		GOOS:       "windows",
		ManagedDir: root,
		RunCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if strings.Contains(strings.Join(args, " "), windowsRegistryKeyPathHKCU) {
				t.Fatal("HKCU should not be read when file policy exists")
			}
			return nil, errors.New("missing registry value")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "file" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestLoadPolicySettingsFallsBackToHKCU(t *testing.T) {
	root := t.TempDir()
	settings, err := LoadPolicySettingsWithOptions(ManagedPolicyOptions{
		GOOS:       "windows",
		ManagedDir: root,
		RunCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, windowsRegistryKeyPathHKLM) {
				return nil, errors.New("missing hklm")
			}
			if strings.Contains(joined, windowsRegistryKeyPathHKCU) {
				return []byte("\n    Settings    REG_SZ    {\"model\":\"hkcu\"}\n"), nil
			}
			return nil, errors.New("unexpected command")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "hkcu" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestManagedSettingsDirUsesAntOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("USER_TYPE", "ant")
	t.Setenv("CLAUDE_CODE_MANAGED_SETTINGS_PATH", root)
	if ManagedSettingsDir() != root {
		t.Fatalf("managed dir = %q", ManagedSettingsDir())
	}
	if ManagedSettingsPath() != filepath.Join(root, "managed-settings.json") {
		t.Fatalf("managed path = %q", ManagedSettingsPath())
	}
	if ManagedSettingsDropInDir() != filepath.Join(root, "managed-settings.d") {
		t.Fatalf("drop-in path = %q", ManagedSettingsDropInDir())
	}
}

func TestParseRegQuerySettingsValue(t *testing.T) {
	stdout := "\r\nHKEY_CURRENT_USER\\SOFTWARE\\Policies\\ClaudeCode\r\n    Settings    REG_EXPAND_SZ    {\"model\":\"opus 4\"}\r\n"
	got := parseRegQuerySettingsValue(stdout, "Settings")
	if got != `{"model":"opus 4"}` {
		t.Fatalf("value = %q", got)
	}
}

func writeTestSettingsFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
