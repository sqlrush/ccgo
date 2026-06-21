package bashtools

import (
	"strings"
	"testing"

	"ccgo/internal/tool"
)

func TestBashPromptHasCoreSections(t *testing.T) {
	got, err := BashPrompt(tool.PromptContext{WorkingDirectory: "/repo"})
	if err != nil {
		t.Fatalf("BashPrompt err: %v", err)
	}
	for _, want := range []string{
		"Executes a given bash command",
		"# Committing changes with git",
		"# Creating pull requests",
		"# Instructions",
		"Glob",  // tool preference
		"Grep",
		"Read",
		"quote file paths that contain spaces",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("BashPrompt missing %q", want)
		}
	}
	// Banned-command guidance must name the dedicated-tool fallbacks.
	for _, banned := range []string{"`find`", "`grep`", "`cat`", "`head`", "`tail`", "`sed`", "`awk`", "`echo`"} {
		if !strings.Contains(got, banned) {
			t.Fatalf("BashPrompt missing banned-command mention %q", banned)
		}
	}
	if len(got) < 1500 {
		t.Fatalf("BashPrompt too short (%d chars); expected the full prompt", len(got))
	}
}
