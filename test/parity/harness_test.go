package parity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoldenAssert(t *testing.T) {
	dir := t.TempDir()
	g := Golden{Dir: dir}
	name := "sample.txt"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.Assert(t, name, []byte("ok\n"))
}
