package outputstyles

import (
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
	pluginpkg "ccgo/internal/plugins"
)

func TestResolveUsesForcedPluginStyleBeforeSettings(t *testing.T) {
	forced := true
	style, ok := Resolve("", contracts.Settings{OutputStyle: "Explanatory"}, []pluginpkg.LoadedPlugin{{
		OutputStyles: []pluginpkg.PluginOutputStyle{{
			Name:           "demo:forced",
			Description:    "Forced",
			Prompt:         "Always use plugin style.",
			ForceForPlugin: &forced,
		}},
	}})
	if !ok || style.Name != "demo:forced" || style.Source != SourcePlugin {
		t.Fatalf("style = %#v ok=%v", style, ok)
	}
}

func TestResolveLoadsUserAndProjectOutputStyles(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".claude", "output-styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configHome, "output-styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "output-styles", "brief.md"), []byte("---\ndescription: User brief\n---\nUser prompt."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".claude", "output-styles", "brief.md"), []byte("---\ndescription: Project brief\n---\nProject prompt."), 0o644); err != nil {
		t.Fatal(err)
	}
	style, ok := Resolve(cwd, contracts.Settings{OutputStyle: "brief"}, nil)
	if !ok || style.Source != SourceProject || style.Description != "Project brief" || style.Prompt != "Project prompt." {
		t.Fatalf("style = %#v ok=%v", style, ok)
	}
}

func TestResolveDefaultHasNoStyleSection(t *testing.T) {
	style, ok := Resolve("", contracts.Settings{}, nil)
	if ok || style.Name != "" {
		t.Fatalf("default style = %#v ok=%v", style, ok)
	}
}
