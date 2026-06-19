package config

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestLoadPolicySettingsLoadsRemoteManagedSettings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Fatalf("accept = %q", r.Header.Get("Accept"))
		}
		if r.Header.Get("Authorization") != "Bearer remote-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"settings":{"model":"remote","env":{"REMOTE":2},"permissions":{"allow":["Read"]}}}`))
	}))
	defer server.Close()

	settings, err := LoadPolicySettingsWithOptions(ManagedPolicyOptions{
		GOOS:            "linux",
		ManagedDir:      t.TempDir(),
		RemoteURL:       server.URL + "/policy?cache=1",
		RemoteAuthToken: "remote-token",
		HTTPClient:      server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "remote" || settings.Env["REMOTE"] != "2" || settings.Permissions == nil || !slices.Equal(settings.Permissions.Allow, []string{"Read"}) {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestLoadPolicySettingsFileWinsOverRemote(t *testing.T) {
	root := t.TempDir()
	writeTestSettingsFile(t, filepath.Join(root, "managed-settings.json"), `{"model":"file"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("remote managed settings should not be fetched when file policy exists")
	}))
	defer server.Close()

	settings, err := LoadPolicySettingsWithOptions(ManagedPolicyOptions{
		GOOS:       "linux",
		ManagedDir: root,
		RemoteURL:  server.URL + "/policy",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "file" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestLoadPolicySettingsRemoteWinsOverHKCUFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"policy":{"model":"remote"}}`))
	}))
	defer server.Close()

	settings, err := LoadPolicySettingsWithOptions(ManagedPolicyOptions{
		GOOS:       "windows",
		ManagedDir: t.TempDir(),
		RemoteURL:  server.URL + "/policy",
		HTTPClient: server.Client(),
		RunCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, windowsRegistryKeyPathHKCU) {
				t.Fatal("HKCU should not be read when remote policy exists")
			}
			return nil, errors.New("missing hklm")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "remote" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestLoadPolicySettingsRemoteStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not authorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := LoadPolicySettingsWithOptions(ManagedPolicyOptions{
		GOOS:       "linux",
		ManagedDir: t.TempDir(),
		RemoteURL:  server.URL + "/policy",
		HTTPClient: server.Client(),
	})
	if err == nil || !strings.Contains(err.Error(), "remote managed settings status 401") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoteManagedSettingsSourceLabelRedactsURLSensitiveParts(t *testing.T) {
	parsed, err := url.Parse("https://user:pass@example.com/policy?token=secret#fragment")
	if err != nil {
		t.Fatal(err)
	}
	got := remoteManagedSettingsSourceLabel(parsed)
	if got != "Remote managed settings: https://example.com/policy" {
		t.Fatalf("label = %q", got)
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
