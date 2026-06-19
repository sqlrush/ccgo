package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallInstalledPluginInScopeRemovesVisiblePlugin(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	cwd := filepath.Join(t.TempDir(), "repo")
	projectPlugin := filepath.Join(cwd, ".claude", "plugins", "demo")
	userPlugin := filepath.Join(configHome, "plugins", "user-demo")
	writeTestPlugin(t, projectPlugin, "demo")
	writeTestPlugin(t, userPlugin, "user-demo")

	result, err := UninstallInstalledPluginInScope("demo", cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plugin.Name != "demo" || result.TargetPath != projectPlugin || result.Scope != "project" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(projectPlugin); !os.IsNotExist(err) {
		t.Fatalf("project plugin still exists or unexpected stat err: %v", err)
	}
	if _, err := os.Stat(userPlugin); err != nil {
		t.Fatalf("user plugin should remain: %v", err)
	}
}

func TestUninstallInstalledPluginInScopeHonorsScope(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	cwd := filepath.Join(t.TempDir(), "repo")
	projectPlugin := filepath.Join(cwd, ".claude", "plugins", "demo")
	userPlugin := filepath.Join(configHome, "plugins", "demo")
	writeTestPlugin(t, projectPlugin, "demo")
	writeTestPlugin(t, userPlugin, "demo")

	result, err := UninstallInstalledPluginInScope("demo", cwd, "user")
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetPath != userPlugin || result.Scope != "user" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(userPlugin); !os.IsNotExist(err) {
		t.Fatalf("user plugin still exists or unexpected stat err: %v", err)
	}
	if _, err := os.Stat(projectPlugin); err != nil {
		t.Fatalf("project plugin should remain: %v", err)
	}
}

func TestUninstallInstalledPluginInScopeRejectsUnsupportedScope(t *testing.T) {
	_, err := UninstallInstalledPluginInScope("demo", t.TempDir(), "system")
	if err == nil || !strings.Contains(err.Error(), `scope "system" is not supported`) {
		t.Fatalf("err = %v", err)
	}
}

func writeTestPlugin(t *testing.T, root string, name string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"name": "` + name + `"}` + "\n")
	if err := os.WriteFile(filepath.Join(root, ManifestFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
