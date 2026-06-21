package powershelltools

import (
	"strings"
	"testing"

	"ccgo/internal/tool"
)

func TestPowerShellPromptHasCoreSections(t *testing.T) {
	got, err := PowerShellPrompt(tool.PromptContext{})
	if err != nil {
		t.Fatalf("PowerShellPrompt err: %v", err)
	}
	for _, want := range []string{
		"PowerShell",
		"Verb-Noun",
		"backtick", // escape rule
		"$env:",    // env var syntax
		"-NonInteractive",
		"Read-Host", // forbidden interactive cmdlet
		"Glob",      // cmdlet preference
		"Select-String",
		"-Confirm:$false",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("PowerShellPrompt missing %q", want)
		}
	}
	if strings.Contains(got, "# Creating pull requests") {
		t.Fatal("PowerShell prompt must NOT include the Bash git/PR ceremony")
	}
}
