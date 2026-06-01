package platform

import "testing"

func TestSanitizeProjectPath(t *testing.T) {
	got := SanitizeProjectPath("/Users/example/project")
	if got == "" || got == "root" {
		t.Fatalf("unexpected sanitized path %q", got)
	}
}
