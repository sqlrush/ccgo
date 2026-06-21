package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionBash(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCompletionCLI([]string{"bash"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "complete") || !strings.Contains(s, "claude") {
		t.Fatalf("bash completion malformed: %q", s)
	}
}

func TestCompletionZsh(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCompletionCLI([]string{"zsh"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "#compdef") || !strings.Contains(s, "claude") {
		t.Fatalf("zsh completion malformed: %q", s)
	}
}

func TestCompletionFish(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCompletionCLI([]string{"fish"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "complete") || !strings.Contains(s, "claude") {
		t.Fatalf("fish completion malformed: %q", s)
	}
}

func TestCompletionBashContainsSubcommands(t *testing.T) {
	var out, errOut bytes.Buffer
	runCompletionCLI([]string{"bash"}, &out, &errOut)
	s := out.String()
	for _, sub := range []string{"mcp", "auth", "plugin", "agents", "doctor", "update", "completion"} {
		if !strings.Contains(s, sub) {
			t.Errorf("bash completion missing subcommand %q", sub)
		}
	}
}

func TestCompletionUnknownShell(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCompletionCLI([]string{"powershell-xyz"}, &out, &errOut); code == 0 {
		t.Fatal("unknown shell should be a non-zero exit")
	}
}

func TestCompletionRequiresShellArg(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCompletionCLI(nil, &out, &errOut); code == 0 {
		t.Fatal("missing shell arg should error")
	}
}
