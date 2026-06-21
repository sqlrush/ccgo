package memory

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func docFor(t *testing.T, path, content string) Document {
	t.Helper()
	writeFile(t, path, content)
	return Document{Header: Header{Path: path, Filename: filepath.Base(path)}, Content: content}
}

func TestExtractImports(t *testing.T) {
	content := "intro\n@./a.md and @~/b.md plus @/abs/c.md\n```\n@./inside-code.md\n```\nemail@example.com not an import\n"
	got := extractImports(content)
	want := []string{"./a.md", "~/b.md", "/abs/c.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("extractImports = %v want %v", got, want)
	}
}

func TestResolveImportsRecursiveAndCycle(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "CLAUDE.md")
	a := filepath.Join(root, "a.md")
	b := filepath.Join(root, "b.md")
	writeFile(t, a, "A body\n@./b.md\n")
	writeFile(t, b, "B body\n@./a.md\n") // cycle back to a
	doc := docFor(t, main, "Main body\n@./a.md\n")

	opts := ImportOptions{BaseDir: root, AllowedRoot: root, MaxDepth: 5}
	imported, err := ResolveImports(doc, opts)
	if err != nil {
		t.Fatalf("ResolveImports err: %v", err)
	}
	// a and b each appear exactly once; cycle did not loop forever.
	var paths []string
	for _, d := range imported {
		paths = append(paths, filepath.Base(d.Path))
	}
	joined := strings.Join(paths, ",")
	if strings.Count(joined, "a.md") != 1 || strings.Count(joined, "b.md") != 1 {
		t.Fatalf("expected a.md and b.md once each; got %v", paths)
	}
}

func TestResolveImportsBlocksTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.md")
	writeFile(t, outside, "secret")
	doc := docFor(t, filepath.Join(root, "CLAUDE.md"), "@"+outside+"\n")

	opts := ImportOptions{BaseDir: root, AllowedRoot: root, MaxDepth: 5, AllowExternal: false}
	imported, err := ResolveImports(doc, opts)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(imported) != 0 {
		t.Fatalf("expected traversal import to be skipped; got %v", imported)
	}
}

// TestResolveImportsDefaultAllowedRootToBaseDir ensures that when AllowExternal
// is false and AllowedRoot is empty, the default root is BaseDir, so traversal
// protection is on by default even if the caller forgets AllowedRoot.
func TestResolveImportsDefaultAllowedRootToBaseDir(t *testing.T) {
	root := t.TempDir()
	sibling := t.TempDir()
	doc := docFor(t, filepath.Join(root, "CLAUDE.md"), "")

	// Attempt relative traversal outside BaseDir (to sibling dir).
	escapeAttempt := filepath.Join(sibling, "secret.md")
	writeFile(t, escapeAttempt, "secret data")

	// Try to import it using a path like @../sibling/secret.md from root.
	relativePath := filepath.Join("..", filepath.Base(sibling), "secret.md")
	doc.Content = "@" + relativePath + "\n"

	// AllowExternal=false, AllowedRoot="" (empty) — should default to BaseDir.
	opts := ImportOptions{BaseDir: root, AllowedRoot: "", MaxDepth: 5, AllowExternal: false}
	imported, err := ResolveImports(doc, opts)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(imported) != 0 {
		t.Fatalf("expected traversal to be blocked (default AllowedRoot=BaseDir); got %v imported docs", len(imported))
	}
}

func TestResolveImportsDepthCap(t *testing.T) {
	root := t.TempDir()
	// chain c0 -> c1 -> ... -> c10
	for i := 0; i < 11; i++ {
		next := ""
		if i < 10 {
			next = "@./c" + strconv.Itoa(i+1) + ".md\n"
		}
		writeFile(t, filepath.Join(root, "c"+strconv.Itoa(i)+".md"), "level "+strconv.Itoa(i)+"\n"+next)
	}
	doc := Document{Header: Header{Path: filepath.Join(root, "c0.md")}, Content: "@./c1.md\n"}
	opts := ImportOptions{BaseDir: root, AllowedRoot: root, MaxDepth: 5}
	imported, err := ResolveImports(doc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) > 5 {
		t.Fatalf("depth cap not honored: %d imported docs", len(imported))
	}
	_ = time.Now
}

// TestResolveImportsRejectsSymlinkEscapingRoot proves that a symlink placed
// INSIDE AllowedRoot whose target is OUTSIDE AllowedRoot is rejected by the
// containment check AFTER symlink resolution — and is therefore not read.
func TestResolveImportsRejectsSymlinkEscapingRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Create the secret file outside root.
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("exfiltrated"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside root pointing to the outside secret.
	symlink := filepath.Join(root, "link.txt")
	if err := os.Symlink(secret, symlink); err != nil {
		t.Skipf("os.Symlink not supported: %v", err)
	}

	doc := Document{
		Header:  Header{Path: filepath.Join(root, "CLAUDE.md")},
		Content: "@./link.txt\n",
	}
	opts := ImportOptions{BaseDir: root, AllowedRoot: root, MaxDepth: 5, AllowExternal: false}
	imported, err := ResolveImports(doc, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, d := range imported {
		if strings.Contains(d.Content, "exfiltrated") {
			t.Fatal("symlink-escaped content was read into model context — SECURITY BUG")
		}
	}
	if len(imported) != 0 {
		t.Fatalf("expected symlink import to be rejected; got %d docs", len(imported))
	}
}

// TestResolveImportsAllowsNormalInRootFile verifies that EvalSymlinks does not
// break ordinary (non-symlink) file imports that reside inside AllowedRoot.
func TestResolveImportsAllowsNormalInRootFile(t *testing.T) {
	root := t.TempDir()
	normal := filepath.Join(root, "normal.md")
	writeFile(t, normal, "normal content\n")

	doc := Document{
		Header:  Header{Path: filepath.Join(root, "CLAUDE.md")},
		Content: "@./normal.md\n",
	}
	opts := ImportOptions{BaseDir: root, AllowedRoot: root, MaxDepth: 5, AllowExternal: false}
	imported, err := ResolveImports(doc, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imported) != 1 || imported[0].Content != "normal content\n" {
		t.Fatalf("expected normal in-root file to be imported; got %v", imported)
	}
}

// TestResolveImportsAllowExternalFollowsSymlink verifies that when
// AllowExternal is true, symlinks are followed without the containment check.
func TestResolveImportsAllowExternalFollowsSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	external := filepath.Join(outside, "external.md")
	if err := os.WriteFile(external, []byte("external content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "ext-link.md")
	if err := os.Symlink(external, symlink); err != nil {
		t.Skipf("os.Symlink not supported: %v", err)
	}

	doc := Document{
		Header:  Header{Path: filepath.Join(root, "CLAUDE.md")},
		Content: "@./ext-link.md\n",
	}
	opts := ImportOptions{BaseDir: root, AllowedRoot: root, MaxDepth: 5, AllowExternal: true}
	imported, err := ResolveImports(doc, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imported) != 1 || imported[0].Content != "external content\n" {
		t.Fatalf("AllowExternal=true should follow symlink; got %v", imported)
	}
}
