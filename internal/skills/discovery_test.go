package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectSkillDirsWalksToGitRootMostSpecificFirst(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "nested", "pkg")
	rootSkill := filepath.Join(repo, ".claude", "skills", "root")
	nestedSkill := filepath.Join(repo, "nested", ".claude", "skills", "nested")
	writeSkill(t, rootSkill)
	writeSkill(t, nestedSkill)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".claude", "skills", "README.md"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ProjectSkillDirs(cwd)
	want := []string{nestedSkill, rootSkill}
	if !sameStringSlice(got, want) {
		t.Fatalf("skill dirs = %#v, want %#v", got, want)
	}
}

func TestDiscoverSkillDirsForPathsExcludesCwdLevelAndSortsDeepestFirst(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	rootSkill := filepath.Join(repo, ".claude", "skills", "root")
	pkgSkill := filepath.Join(repo, "pkg", ".claude", "skills", "pkg")
	subSkill := filepath.Join(repo, "pkg", "sub", ".claude", "skills", "sub")
	writeSkill(t, rootSkill)
	writeSkill(t, pkgSkill)
	writeSkill(t, subSkill)

	got := DiscoverSkillDirsForPaths([]string{filepath.Join(repo, "pkg", "sub", "file.go")}, repo)
	want := []string{subSkill, pkgSkill}
	if !sameStringSlice(got, want) {
		t.Fatalf("skill dirs = %#v, want %#v", got, want)
	}
}

func writeSkill(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\ndescription: test\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
