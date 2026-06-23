package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunAgentsCLISettingSourcesProject verifies --setting-sources project filters to project scope.
func TestRunAgentsCLISettingSourcesProject(t *testing.T) {
	cwd := t.TempDir()
	projectAgentDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(projectAgentDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	agentContent := "---\nname: project-agent\ndescription: project scope agent\n---\n# project-agent\n"
	if err := os.WriteFile(filepath.Join(projectAgentDir, "project-agent.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runAgentsCLI(cwd, []string{"--setting-sources", "project"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "project-agent") {
		t.Fatalf("expected project-agent in output; got %q", out.String())
	}
}

// TestRunAgentsCLICreate verifies 'claude agents create <name>' creates the agent file.
func TestRunAgentsCLICreate(t *testing.T) {
	cwd := t.TempDir()
	var out, errOut bytes.Buffer
	code := runAgentsCLI(cwd, []string{"create", "newagent"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code %d, stderr=%s", code, errOut.String())
	}
	path := filepath.Join(cwd, ".claude", "agents", "newagent.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected agent file at %s; got error: %v", path, err)
	}
	if !strings.Contains(out.String(), "newagent") {
		t.Fatalf("expected 'newagent' in output; got %q", out.String())
	}
}

// TestRunAgentsCLICreateNoName verifies 'claude agents create' with no name fails.
func TestRunAgentsCLICreateNoName(t *testing.T) {
	cwd := t.TempDir()
	var out, errOut bytes.Buffer
	code := runAgentsCLI(cwd, []string{"create"}, &out, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero exit for create with no name")
	}
}

// TestRunAgentsCLIShowAgent verifies 'claude agents show <name>' shows agent details.
func TestRunAgentsCLIShowAgent(t *testing.T) {
	cwd := t.TempDir()
	agentDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	agentContent := "---\nname: myagent\ndescription: A test agent\nmodel: claude-opus-4-5\n---\n# myagent\n"
	if err := os.WriteFile(filepath.Join(agentDir, "myagent.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runAgentsCLI(cwd, []string{"show", "myagent"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "myagent") || !strings.Contains(out.String(), "A test agent") {
		t.Fatalf("expected agent details in output; got %q", out.String())
	}
	if !strings.Contains(out.String(), "claude-opus-4-5") {
		t.Fatalf("expected model in output; got %q", out.String())
	}
}

// TestRunAgentsCLIDeleteAgent verifies 'claude agents delete <name>' removes the file.
func TestRunAgentsCLIDeleteAgent(t *testing.T) {
	cwd := t.TempDir()
	agentDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	agentContent := "---\nname: tobedeleted\n---\n# tobedeleted\n"
	if err := os.WriteFile(filepath.Join(agentDir, "tobedeleted.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runAgentsCLI(cwd, []string{"delete", "tobedeleted"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code %d, stderr=%s", code, errOut.String())
	}
	path := filepath.Join(agentDir, "tobedeleted.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected agent file to be deleted at %s", path)
	}
}
