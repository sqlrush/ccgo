package bashtools

// CFG-14/15: Git attribution tests.
// Verifies that BashPrompt injects / omits the Co-Authored-By trailer based on
// settings.IncludeCoAuthoredBy and settings.Attribution.Commit.

import (
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func promptCtxWithSettings(s contracts.Settings) tool.PromptContext {
	return tool.PromptContext{
		Metadata: map[string]any{
			tool.MetadataSettingsKey: s,
		},
	}
}

func TestBashPromptDefaultIncludesCoAuthoredBy(t *testing.T) {
	// Default settings → Co-Authored-By trailer must appear.
	got, err := BashPrompt(promptCtxWithSettings(contracts.Settings{}))
	if err != nil {
		t.Fatalf("BashPrompt: %v", err)
	}
	if !strings.Contains(got, "Co-Authored-By:") {
		t.Fatalf("expected default prompt to contain 'Co-Authored-By:', got:\n%s", got)
	}
}

func TestBashPromptIncludeCoAuthoredByFalseOmitsTrailer(t *testing.T) {
	// includeCoAuthoredBy: false → commit section must NOT mention Co-Authored-By.
	f := false
	got, err := BashPrompt(promptCtxWithSettings(contracts.Settings{IncludeCoAuthoredBy: &f}))
	if err != nil {
		t.Fatalf("BashPrompt: %v", err)
	}
	if strings.Contains(got, "Co-Authored-By:") {
		t.Fatalf("expected Co-Authored-By omitted when IncludeCoAuthoredBy=false, got:\n%s", got)
	}
	// Commit section must still be present.
	if !strings.Contains(got, "# Committing changes with git") {
		t.Fatalf("commit section must still be present even when attribution disabled")
	}
}

func TestBashPromptCustomAttributionCommit(t *testing.T) {
	// attribution.commit: "Custom Bot" → that text appears instead of default.
	custom := "Custom Bot <bot@example.com>"
	got, err := BashPrompt(promptCtxWithSettings(contracts.Settings{
		Attribution: &contracts.AttributionSetting{
			Commit: &custom,
		},
	}))
	if err != nil {
		t.Fatalf("BashPrompt: %v", err)
	}
	if !strings.Contains(got, custom) {
		t.Fatalf("expected custom attribution %q in prompt, got:\n%s", custom, got)
	}
}

func TestBashPromptEmptyAttributionCommitOmitsTrailer(t *testing.T) {
	// attribution.commit: "" → no Co-Authored-By section (empty string hides attribution).
	empty := ""
	got, err := BashPrompt(promptCtxWithSettings(contracts.Settings{
		Attribution: &contracts.AttributionSetting{
			Commit: &empty,
		},
	}))
	if err != nil {
		t.Fatalf("BashPrompt: %v", err)
	}
	if strings.Contains(got, "Co-Authored-By:") {
		t.Fatalf("expected Co-Authored-By omitted for empty attribution.commit, got:\n%s", got)
	}
}
