package memory

import (
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
