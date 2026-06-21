package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAgentsCLIListsEmpty(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runAgentsCLI(t.TempDir(), nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "agents") && out.Len() == 0 {
		t.Fatalf("expected some listing output, got %q", out.String())
	}
}

func TestRunAgentsCLIListsAgents(t *testing.T) {
	cwd := t.TempDir()
	agentDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "myreviewer.md"), []byte("---\nname: myreviewer\n---\n# myreviewer\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runAgentsCLI(cwd, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "myreviewer") {
		t.Fatalf("expected myreviewer in output, got %q", out.String())
	}
}
