package native

import (
	"strings"
	"testing"
)

func TestBuildColorDiff(t *testing.T) {
	diff := BuildColorDiff("one\ntwo\nthree\n", "one\ntwo changed\nthree\n", ColorDiffOptions{
		Path:         "main.go",
		ContextLines: 1,
		Color:        true,
	})
	if !diff.Changed || diff.Path != "main.go" {
		t.Fatalf("diff metadata = %#v", diff)
	}
	for _, want := range []string{"--- a/main.go", "+++ b/main.go", "-two", "+two changed"} {
		if !strings.Contains(diff.Unified, want) {
			t.Fatalf("unified missing %q: %q", want, diff.Unified)
		}
	}
	if !strings.Contains(diff.Colored, diffColorRed+"-two"+diffColorReset) ||
		!strings.Contains(diff.Colored, diffColorGreen+"+two changed"+diffColorReset) {
		t.Fatalf("colored diff = %q", diff.Colored)
	}
}

func TestBuildColorDiffUnchanged(t *testing.T) {
	diff := BuildColorDiff("same\n", "same\n", ColorDiffOptions{Path: "same.txt", Color: true})
	if diff.Changed || diff.Unified != "" || diff.Colored != "" || diff.Path != "same.txt" {
		t.Fatalf("diff = %#v", diff)
	}
}
