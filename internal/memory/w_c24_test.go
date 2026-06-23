package memory

// W-C24 CFG-44: claudeMdExcludes — ExcludePatterns in LoadOptions.

import "testing"

func TestClaudeMdExcludedMatchesGlob(t *testing.T) {
	// claudeMdExcluded matches against both the full path and the basename.
	// Pattern "*.secret" matches the basename "api.secret"; "private/*" matches
	// the segment "private/internal.md" only when it appears literally in the
	// path basename — filepath.Match does not perform substring matching.
	cases := []struct {
		path     string
		patterns []string
		wantOut  bool
	}{
		// Basename match: "api.secret" matches "*.secret"
		{"/home/user/.claude/api.secret", []string{"*.secret"}, true},
		// Full path match with trailing wildcard: exact prefix
		{"/home/user/CLAUDE.md", []string{"*.secret"}, false},
		// No match when pattern doesn't apply
		{"/home/user/.claude/CLAUDE.md", []string{"*.secret", "*.tmp"}, false},
		// Basename match for CLAUDE.md explicitly excluded
		{"CLAUDE.md", []string{"CLAUDE.md"}, true},
	}
	for _, tc := range cases {
		got := claudeMdExcluded(tc.path, tc.patterns)
		if got != tc.wantOut {
			t.Errorf("claudeMdExcluded(%q, %v) = %v, want %v", tc.path, tc.patterns, got, tc.wantOut)
		}
	}
}

func TestClaudeMdExcludedNoPatterns(t *testing.T) {
	if claudeMdExcluded("/any/path/CLAUDE.md", nil) {
		t.Error("expected claudeMdExcluded to return false when patterns is nil")
	}
	if claudeMdExcluded("/any/path/CLAUDE.md", []string{}) {
		t.Error("expected claudeMdExcluded to return false when patterns is empty")
	}
}
