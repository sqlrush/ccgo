package parity

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

type Golden struct {
	Dir string
}

func (g Golden) Assert(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join(g.Dir, name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("golden mismatch for %s\nwant:\n%s\n\ngot:\n%s", name, want, got)
	}
}
