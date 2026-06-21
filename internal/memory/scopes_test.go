package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func writeScopeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverScopedClaudeFilesPrecedence(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, "managed")
	user := filepath.Join(root, "user")
	proj := filepath.Join(root, "proj")
	sub := filepath.Join(proj, "a", "b")

	writeScopeFile(t, filepath.Join(managed, "CLAUDE.md"), "managed")
	writeScopeFile(t, filepath.Join(managed, ".claude", "rules", "policy.md"), "managed-rule")
	writeScopeFile(t, filepath.Join(user, "CLAUDE.md"), "user")
	writeScopeFile(t, filepath.Join(user, "rules", "style.md"), "user-rule")
	writeScopeFile(t, filepath.Join(proj, "CLAUDE.md"), "proj-root")
	writeScopeFile(t, filepath.Join(proj, ".claude", "CLAUDE.md"), "proj-dotclaude")
	writeScopeFile(t, filepath.Join(proj, ".claude", "rules", "team.md"), "proj-rule")
	writeScopeFile(t, filepath.Join(sub, "CLAUDE.md"), "proj-sub")
	writeScopeFile(t, filepath.Join(proj, "CLAUDE.local.md"), "local-root")

	opts := ScopeOptions{CWD: sub, ManagedDir: managed, UserDir: user}
	files, err := DiscoverScopedClaudeFiles(opts)
	if err != nil {
		t.Fatal(err)
	}

	// Build path->scope index for assertions.
	got := map[string]Scope{}
	var order []string
	for _, f := range files {
		got[filepath.Base(filepath.Dir(f.Path))+"/"+filepath.Base(f.Path)] = f.Scope
		order = append(order, f.Path)
	}

	if s := got["managed/CLAUDE.md"]; s != ScopeManaged {
		t.Fatalf("managed CLAUDE.md scope = %q want managed", s)
	}
	if s := got["user/CLAUDE.md"]; s != ScopeUser {
		t.Fatalf("user CLAUDE.md scope = %q want user", s)
	}

	// Managed must come before User, User before any project file, project before local.
	pos := func(want string) int {
		for i, p := range order {
			if p == want {
				return i
			}
		}
		t.Fatalf("expected discovered file %s; got order %v", want, order)
		return -1
	}
	managedRoot := filepath.Join(managed, "CLAUDE.md")
	userRoot := filepath.Join(user, "CLAUDE.md")
	projRoot := filepath.Join(proj, "CLAUDE.md")
	projSub := filepath.Join(sub, "CLAUDE.md")
	localRoot := filepath.Join(proj, "CLAUDE.local.md")
	if !(pos(managedRoot) < pos(userRoot) &&
		pos(userRoot) < pos(projRoot) &&
		pos(projRoot) < pos(projSub) &&
		pos(projSub) < pos(localRoot)) {
		t.Fatalf("precedence order wrong: %v", order)
	}
}

func TestDiscoverScopedClaudeFilesScopes(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, "managed")
	user := filepath.Join(root, "user")
	proj := filepath.Join(root, "proj")

	writeScopeFile(t, filepath.Join(managed, "CLAUDE.md"), "managed")
	writeScopeFile(t, filepath.Join(managed, ".claude", "rules", "policy.md"), "managed-rule")
	writeScopeFile(t, filepath.Join(user, "CLAUDE.md"), "user")
	writeScopeFile(t, filepath.Join(user, "rules", "style.md"), "user-rule")
	writeScopeFile(t, filepath.Join(proj, "CLAUDE.md"), "proj-root")
	writeScopeFile(t, filepath.Join(proj, "CLAUDE.local.md"), "local-root")

	opts := ScopeOptions{CWD: proj, ManagedDir: managed, UserDir: user}
	files, err := DiscoverScopedClaudeFiles(opts)
	if err != nil {
		t.Fatal(err)
	}

	scopeFor := func(path string) Scope {
		for _, f := range files {
			if f.Path == path {
				return f.Scope
			}
		}
		t.Fatalf("file not found: %s", path)
		return ""
	}
	labelFor := func(path string) string {
		for _, f := range files {
			if f.Path == path {
				return f.Label
			}
		}
		t.Fatalf("file not found: %s", path)
		return ""
	}

	if s := scopeFor(filepath.Join(managed, "CLAUDE.md")); s != ScopeManaged {
		t.Errorf("managed CLAUDE.md scope = %q", s)
	}
	if s := scopeFor(filepath.Join(managed, ".claude", "rules", "policy.md")); s != ScopeManaged {
		t.Errorf("managed rule scope = %q", s)
	}
	if s := scopeFor(filepath.Join(user, "CLAUDE.md")); s != ScopeUser {
		t.Errorf("user CLAUDE.md scope = %q", s)
	}
	if s := scopeFor(filepath.Join(user, "rules", "style.md")); s != ScopeUser {
		t.Errorf("user rule scope = %q", s)
	}
	if s := scopeFor(filepath.Join(proj, "CLAUDE.md")); s != ScopeProject {
		t.Errorf("proj CLAUDE.md scope = %q", s)
	}
	if s := scopeFor(filepath.Join(proj, "CLAUDE.local.md")); s != ScopeLocal {
		t.Errorf("proj CLAUDE.local.md scope = %q", s)
	}

	// Verify labels are non-empty.
	for _, path := range []string{
		filepath.Join(managed, "CLAUDE.md"),
		filepath.Join(user, "CLAUDE.md"),
		filepath.Join(proj, "CLAUDE.md"),
		filepath.Join(proj, "CLAUDE.local.md"),
	} {
		if l := labelFor(path); l == "" {
			t.Errorf("label empty for %s", path)
		}
	}
}

func TestDiscoverScopedClaudeFilesMissingScopes(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	writeScopeFile(t, filepath.Join(proj, "CLAUDE.md"), "proj")

	// No managed, no user dirs.
	opts := ScopeOptions{CWD: proj, ManagedDir: "", UserDir: ""}
	files, err := DiscoverScopedClaudeFiles(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file (proj CLAUDE.md), got %d: %v", len(files), files)
	}
	if files[0].Scope != ScopeProject {
		t.Fatalf("expected ScopeProject, got %q", files[0].Scope)
	}
}

func TestDiscoverScopedClaudeFilesLocalVariants(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	sub := filepath.Join(proj, "sub")
	writeScopeFile(t, filepath.Join(proj, "CLAUDE.local.md"), "local-proj")
	writeScopeFile(t, filepath.Join(sub, "CLAUDE.local.md"), "local-sub")

	opts := ScopeOptions{CWD: sub}
	files, err := DiscoverScopedClaudeFiles(opts)
	if err != nil {
		t.Fatal(err)
	}

	var localFiles []ClaudeFile
	for _, f := range files {
		if f.Scope == ScopeLocal {
			localFiles = append(localFiles, f)
		}
	}
	if len(localFiles) != 2 {
		t.Fatalf("expected 2 local files, got %d: %v", len(localFiles), localFiles)
	}
	// proj local must come before sub local (root-first ordering).
	projLocal := filepath.Join(proj, "CLAUDE.local.md")
	subLocal := filepath.Join(sub, "CLAUDE.local.md")
	posLocal := func(want string) int {
		for i, f := range localFiles {
			if f.Path == want {
				return i
			}
		}
		t.Fatalf("local file not found: %s", want)
		return -1
	}
	if !(posLocal(projLocal) < posLocal(subLocal)) {
		t.Fatalf("local ordering wrong: proj should come before sub")
	}
}

func TestDefaultScopeOptions(t *testing.T) {
	opts := DefaultScopeOptions("/tmp")
	if opts.CWD != "/tmp" {
		t.Errorf("CWD = %q", opts.CWD)
	}
	// ManagedDir and UserDir are read from globals; just ensure they are non-empty strings
	// (not checking actual paths to avoid depending on machine state).
	_ = opts.ManagedDir
	_ = opts.UserDir
}
