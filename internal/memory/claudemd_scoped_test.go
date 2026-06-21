package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadScopedClaudeContextExpandsImports(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "user")
	proj := filepath.Join(root, "proj")
	writeFile(t, filepath.Join(user, "CLAUDE.md"), "user-global\n")
	writeFile(t, filepath.Join(proj, "shared.md"), "SHARED-CONTENT\n")
	writeFile(t, filepath.Join(proj, "CLAUDE.md"), "proj-root\n@./shared.md\n")

	opts := LoadOptions{
		Scope:  ScopeOptions{CWD: proj, UserDir: user},
		Import: ImportOptions{AllowedRoot: root, MaxDepth: 5},
	}
	docs, err := LoadScopedClaudeContext(opts)
	if err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, d := range docs {
		seq = append(seq, strings.TrimSpace(d.Content))
	}
	joined := strings.Join(seq, "|")
	// user before project; imported shared.md appears immediately before its host.
	if !strings.Contains(joined, "user-global") {
		t.Fatalf("missing user scope: %v", seq)
	}
	si, pi := indexOf(seq, "SHARED-CONTENT"), indexOf(seq, "proj-root")
	ui := indexOf(seq, "user-global")
	if !(ui < pi && si >= 0 && si < pi) {
		t.Fatalf("ordering wrong (user<proj, import<host): %v", seq)
	}
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}
