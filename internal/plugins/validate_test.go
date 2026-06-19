package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateManifestPathLoadsRootPlugin(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "commands", "deploy.md"), []byte("---\ndescription: Deploy app\n---\nDeploy."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestFileName), []byte(`{
		"name": "demo-plugin",
		"version": "1.0.0",
		"description": "Demo plugin",
		"author": {"name": "Team"}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := ValidateManifestPath(".", root)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.FileType != "plugin" || result.FilePath != filepath.Join(root, ManifestFileName) {
		t.Fatalf("result = %#v", result)
	}
	if result.Plugin.Name != "demo-plugin" || len(result.Plugin.PromptTemplates) != 1 {
		t.Fatalf("plugin = %#v", result.Plugin)
	}
	if len(result.Errors) != 0 || len(result.Warnings) != 0 {
		t.Fatalf("unexpected diagnostics: errors=%#v warnings=%#v", result.Errors, result.Warnings)
	}
}

func TestValidateManifestPathDirectoryPrefersOfficialMarketplace(t *testing.T) {
	root := t.TempDir()
	officialDir := filepath.Join(root, ".claude-plugin")
	if err := os.MkdirAll(officialDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(officialDir, ManifestFileName), []byte(`{"name":"demo-plugin","author":{"name":"Team"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(officialDir, "marketplace.json"), []byte(`{
		"metadata": {"description": "Team marketplace"},
		"plugins": [{"name": "market-demo", "source": "./plugins/demo"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := ValidateManifestPath(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.FileType != "marketplace" || result.FilePath != filepath.Join(officialDir, "marketplace.json") {
		t.Fatalf("result = %#v", result)
	}
	if result.PluginCount != 1 || len(result.MarketplaceIDs) != 1 || result.MarketplaceIDs[0] != "market-demo" {
		t.Fatalf("marketplace summary = %#v", result)
	}
}

func TestValidateManifestPathDirectoryReportsManifestDirectory(t *testing.T) {
	root := t.TempDir()
	officialDir := filepath.Join(root, ".claude-plugin")
	if err := os.MkdirAll(filepath.Join(officialDir, "marketplace.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(officialDir, ManifestFileName), []byte(`{"name":"demo-plugin","author":{"name":"Team"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := ValidateManifestPath(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || result.FileType != "marketplace" || len(result.Errors) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Errors[0].Code != "EISDIR" || !strings.Contains(result.Errors[0].Message, "Path is not a file") {
		t.Fatalf("errors = %#v", result.Errors)
	}
}

func TestValidateManifestPathReportsInvalidJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ManifestFileName)
	if err := os.WriteFile(path, []byte(`{"name":`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := ValidateManifestPath(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || result.FileType != "plugin" || len(result.Errors) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Errors[0].Path != "json" || !strings.Contains(result.Errors[0].Message, "Invalid JSON syntax") {
		t.Fatalf("errors = %#v", result.Errors)
	}
}

func TestValidateManifestPathReportsWarningsAndTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ManifestFileName), []byte(`{
		"name": "Bad Name",
		"version": "1.0.0",
		"description": "Bad plugin",
		"author": {"name": "Team"},
		"source": {"name": "market"},
		"commands": ["../escape.md"]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := ValidateManifestPath(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || len(result.Errors) == 0 || len(result.Warnings) == 0 {
		t.Fatalf("result = %#v", result)
	}
	if !validationContains(result.Errors, "commands[0]", "Path contains") {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if !validationContains(result.Warnings, "source", "belongs in the marketplace entry") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func validationContains(messages []ManifestValidationMessage, path string, text string) bool {
	for _, message := range messages {
		if message.Path == path && strings.Contains(message.Message, text) {
			return true
		}
	}
	return false
}
