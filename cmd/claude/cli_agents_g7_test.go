package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentsListShowsModel verifies SUBCMD-AGENTS-04: the model field is shown
// next to the agent name in the list output.
func TestAgentsListShowsModel(t *testing.T) {
	cwd := t.TempDir()
	agentDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	content := "---\nname: myagent\ndescription: A test agent\nmodel: claude-opus-4-5\n---\n# myagent\n"
	if err := os.WriteFile(filepath.Join(agentDir, "myagent.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runAgentsCLI(cwd, []string{}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "claude-opus-4-5") {
		t.Fatalf("expected model 'claude-opus-4-5' in list output; got %q", out.String())
	}
}

// TestAgentsListShadowedAgent verifies SUBCMD-AGENTS-05: when a project agent
// has the same name as a user agent, the lower-priority agent is marked "(shadowed)".
func TestAgentsListShadowedAgent(t *testing.T) {
	cwd := t.TempDir()

	// Project agent: higher priority (overrides user agent).
	projectAgentDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(projectAgentDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	projectContent := "---\nname: shared\ndescription: Project version\n---\n# shared\n"
	if err := os.WriteFile(filepath.Join(projectAgentDir, "shared.md"), []byte(projectContent), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// User agent: lower priority — should be listed as shadowed.
	userDir := t.TempDir()
	userAgentDir := filepath.Join(userDir, ".claude", "agents")
	if err := os.MkdirAll(userAgentDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	userContent := "---\nname: shared\ndescription: User version\n---\n# shared\n"
	if err := os.WriteFile(filepath.Join(userAgentDir, "shared.md"), []byte(userContent), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runAgentsCLIWithUserDir(cwd, userDir, []string{}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code %d, stderr=%s", code, errOut.String())
	}
	// One of the "shared" entries should be marked as shadowed.
	if !strings.Contains(out.String(), "shadowed") {
		t.Fatalf("expected '(shadowed)' in output for overridden agent; got %q", out.String())
	}
}

// TestAgentsListNoShadowWhenNoDuplicate verifies that agents with unique names
// are not marked shadowed.
func TestAgentsListNoShadowWhenNoDuplicate(t *testing.T) {
	cwd := t.TempDir()
	agentDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	content := "---\nname: uniqueagent\ndescription: Only one\n---\n# uniqueagent\n"
	if err := os.WriteFile(filepath.Join(agentDir, "uniqueagent.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runAgentsCLI(cwd, []string{}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code %d, stderr=%s", code, errOut.String())
	}
	if strings.Contains(out.String(), "shadowed") {
		t.Fatalf("unique agent should not be shadowed; got %q", out.String())
	}
}
