package mcp

import (
	"strings"
	"testing"
)

func TestTruncateMCPTextShortPassthrough(t *testing.T) {
	text := "short description"
	if got := TruncateMCPText(text); got != text {
		t.Fatalf("got %q want %q", got, text)
	}
}

func TestTruncateMCPTextEmptyPassthrough(t *testing.T) {
	if got := TruncateMCPText(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestTruncateMCPTextExactLimitPassthrough(t *testing.T) {
	text := strings.Repeat("a", MaxMCPDescriptionLength)
	if got := TruncateMCPText(text); got != text {
		t.Fatalf("exact-limit text should pass through unchanged")
	}
}

func TestTruncateMCPTextOverLimitTruncates(t *testing.T) {
	text := strings.Repeat("x", MaxMCPDescriptionLength+100)
	got := TruncateMCPText(text)
	if !strings.HasSuffix(got, "… [truncated]") {
		t.Fatalf("truncated text should end with marker, got %q…", got[:50])
	}
	if len(got) > MaxMCPDescriptionLength+len("… [truncated]") {
		t.Fatalf("truncated text is too long: len=%d", len(got))
	}
}

func TestTruncateMCPTextPreservesRuneBoundary(t *testing.T) {
	// Build a string where MaxMCPDescriptionLength falls in the middle of a 3-byte rune.
	prefix := strings.Repeat("a", MaxMCPDescriptionLength-1)
	// "€" is 3 UTF-8 bytes (0xE2 0x82 0xAC).
	text := prefix + "€" + strings.Repeat("b", 10)
	got := TruncateMCPText(text)
	if !strings.HasSuffix(got, "… [truncated]") {
		t.Fatalf("should be truncated, got %q", got[:50])
	}
	// Result must be valid UTF-8 and not contain a broken rune.
	for i, r := range got {
		_ = i
		_ = r // range decodes runes; invalid bytes produce RuneError but the loop still runs
		if r == '�' {
			t.Fatalf("invalid rune at position %d in truncated text", i)
		}
	}
}
