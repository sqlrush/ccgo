package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPrintsVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "(ccgo mcp)") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunHelpExitsSuccessfully(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage of claude-mcp:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunServesInitializeRequest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	input := `{"jsonrpc":"2.0","id":"1","method":"initialize","params":{}}` + "\n"
	code := run([]string{"--cwd", "."}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"protocolVersion":"2025-06-18"`) || !strings.Contains(stdout.String(), `"serverInfo"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestRunRejectsInvalidCWD(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", filepath.Join(t.TempDir(), "missing")}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("code=%d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "invalid --cwd") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunAllowMutatingToolsCamelAlias(t *testing.T) {
	project := t.TempDir()
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"init","method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"write","method":"tools/call","params":{"name":"Write","arguments":{"file_path":"mcp-write.txt","content":"written through mcp alias"}}}`,
		"",
	}, "\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "--allowMutatingTools"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), `"isError":true`) || !strings.Contains(stdout.String(), `"id":"write"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(project, "mcp-write.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "written through mcp alias" {
		t.Fatalf("written file = %q", string(data))
	}
}
