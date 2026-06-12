package main

import (
	"bytes"
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
