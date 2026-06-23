package fuzzy_test

import (
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/fuzzy"
)

// buildFixture creates a temporary directory with the given relative file paths
// and returns the root directory path. The caller is responsible for cleanup.
func buildFixture(t *testing.T, files []string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("writefile: %v", err)
		}
	}
	return root
}

func TestWalkFilesBasic(t *testing.T) {
	root := buildFixture(t, []string{
		"main.go",
		"internal/foo.go",
		"internal/bar.go",
	})
	got := fuzzy.WalkFiles(root, fuzzy.WalkOptions{})
	if len(got) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(got), got)
	}
}

func TestWalkFilesIgnoresGitDir(t *testing.T) {
	root := buildFixture(t, []string{
		"main.go",
		".git/HEAD",
		".git/objects/abc",
	})
	got := fuzzy.WalkFiles(root, fuzzy.WalkOptions{})
	// Only main.go should be returned; .git/* is skipped.
	for _, f := range got {
		if len(f) >= 4 && f[:4] == ".git" {
			t.Errorf("expected .git to be skipped, got %q", f)
		}
	}
	if len(got) != 1 {
		t.Errorf("expected 1 file, got %d: %v", len(got), got)
	}
}

func TestWalkFilesIgnoresNodeModules(t *testing.T) {
	root := buildFixture(t, []string{
		"index.js",
		"node_modules/react/index.js",
		"node_modules/lodash/index.js",
	})
	got := fuzzy.WalkFiles(root, fuzzy.WalkOptions{})
	for _, f := range got {
		if len(f) >= 12 && f[:12] == "node_modules" {
			t.Errorf("expected node_modules to be skipped, got %q", f)
		}
	}
	if len(got) != 1 {
		t.Errorf("expected 1 file, got %d: %v", len(got), got)
	}
}

func TestWalkFilesIgnoresHiddenDirs(t *testing.T) {
	root := buildFixture(t, []string{
		"main.go",
		".hidden/secret.go",
	})
	got := fuzzy.WalkFiles(root, fuzzy.WalkOptions{})
	for _, f := range got {
		if len(f) > 0 && f[0] == '.' {
			t.Errorf("expected hidden dir to be skipped, got %q", f)
		}
	}
	if len(got) != 1 {
		t.Errorf("expected 1 file, got %d: %v", len(got), got)
	}
}

func TestWalkFilesMaxFiles(t *testing.T) {
	root := buildFixture(t, []string{
		"a.go", "b.go", "c.go", "d.go", "e.go",
	})
	got := fuzzy.WalkFiles(root, fuzzy.WalkOptions{MaxFiles: 3})
	if len(got) != 3 {
		t.Errorf("expected 3 files due to MaxFiles limit, got %d", len(got))
	}
}

func TestWalkFilesExtraIgnoreDirs(t *testing.T) {
	root := buildFixture(t, []string{
		"main.go",
		"generated/code.go",
	})
	got := fuzzy.WalkFiles(root, fuzzy.WalkOptions{
		ExtraIgnoreDirs: map[string]bool{"generated": true},
	})
	for _, f := range got {
		if len(f) >= 9 && f[:9] == "generated" {
			t.Errorf("expected generated dir to be skipped, got %q", f)
		}
	}
	if len(got) != 1 {
		t.Errorf("expected 1 file, got %d: %v", len(got), got)
	}
}

func TestWalkFilesForwardSlashes(t *testing.T) {
	root := buildFixture(t, []string{
		"internal/repl/loop.go",
	})
	got := fuzzy.WalkFiles(root, fuzzy.WalkOptions{})
	for _, f := range got {
		for _, ch := range f {
			if ch == '\\' {
				t.Errorf("expected forward slashes only, got %q", f)
			}
		}
	}
}

func TestFilterFiles(t *testing.T) {
	root := buildFixture(t, []string{
		"cmd/main.go",
		"internal/repl/loop.go",
		"internal/fuzzy/fuzzy.go",
	})
	got := fuzzy.FilterFiles(root, "loop", fuzzy.WalkOptions{})
	if len(got) == 0 {
		t.Fatal("expected at least one match for 'loop'")
	}
	if got[0] != "internal/repl/loop.go" {
		t.Errorf("expected internal/repl/loop.go first, got %q", got[0])
	}
}

func TestWalkFilesNonExistentRoot(t *testing.T) {
	got := fuzzy.WalkFiles("/nonexistent/path/xyz123", fuzzy.WalkOptions{})
	if got != nil && len(got) != 0 {
		t.Errorf("expected empty result for non-existent root, got %v", got)
	}
}
