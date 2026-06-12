package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectConfigChainMergesFromRootToCWD(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "project")
	child := filepath.Join(parent, "sub")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".mcp.json"), []byte(`{
		"mcpServers": {
			"shared": {"command": "parent"},
			"parent-only": {"command": "parent-only"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".mcp.json"), []byte(`{
		"mcpServers": {
			"shared": {"command": "child"},
			"child-only": {"type": "http", "url": "https://child.example/mcp"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadProjectConfigChain(child, ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if got := result.Servers["shared"]; got.Command != "child" || got.Scope != ScopeProject {
		t.Fatalf("shared = %#v", got)
	}
	if got := result.Servers["parent-only"]; got.Command != "parent-only" || got.Scope != ScopeProject {
		t.Fatalf("parent-only = %#v", got)
	}
	if got := result.Servers["child-only"]; got.URL != "https://child.example/mcp" || got.Scope != ScopeProject {
		t.Fatalf("child-only = %#v", got)
	}
}

func TestLoadProjectConfigChainSkipsMissingFilesButKeepsMalformedErrors(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "project")
	child := filepath.Join(parent, "sub")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".mcp.json"), []byte(`{
		"mcpServers": {
			"bad": {"type": "http"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".mcp.json"), []byte(`{
		"mcpServers": {
			"good": {"command": "node"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadProjectConfigChain(child, ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Servers["good"]; got.Command != "node" {
		t.Fatalf("good = %#v", got)
	}
	if _, ok := result.Servers["bad"]; ok {
		t.Fatalf("bad server should not be loaded: %#v", result.Servers)
	}
	if len(result.Errors) != 1 || result.Errors[0].ServerName != "bad" || result.Errors[0].Severity != "fatal" {
		t.Fatalf("errors = %#v", result.Errors)
	}
}

func TestLoadProjectConfigChainExpandsEnvWarnings(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
		"mcpServers": {
			"env": {"command": "${NODE}", "args": ["${MISSING}"]}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadProjectConfigChain(root, ParseOptions{
		ExpandVars: true,
		UseEnvMap:  true,
		Env:        map[string]string{"NODE": "node"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Servers["env"]; got.Command != "node" {
		t.Fatalf("env server = %#v", got)
	}
	if len(result.Errors) != 1 || result.Errors[0].Severity != "warning" || result.Errors[0].ServerName != "env" {
		t.Fatalf("errors = %#v", result.Errors)
	}
}
