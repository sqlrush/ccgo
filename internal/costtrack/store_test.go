package costtrack

import (
	"path/filepath"
	"testing"
)

func TestSaveRestoreSameSession(t *testing.T) {
	dir := t.TempDir()
	opts := Options{ProjectsDir: dir, CWD: "/home/u/proj"}
	want := ProjectCost{LastCost: 0.42, LastSessionID: "s1", LastTotalInputTokens: 10, LastTotalOutputTokens: 5}
	if err := Save(opts, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Restore(opts, "s1")
	if err != nil || !ok {
		t.Fatalf("Restore ok=%v err=%v", ok, err)
	}
	if got.LastCost != 0.42 || got.LastSessionID != "s1" {
		t.Fatalf("got %+v", got)
	}
}

func TestRestoreDifferentSessionMisses(t *testing.T) {
	dir := t.TempDir()
	opts := Options{ProjectsDir: dir, CWD: "/home/u/proj"}
	if err := Save(opts, ProjectCost{LastCost: 1.0, LastSessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	_, ok, err := Restore(opts, "s2")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("must not restore cost across a different session id")
	}
}

func TestRestoreNoFile(t *testing.T) {
	opts := Options{ProjectsDir: filepath.Join(t.TempDir(), "empty"), CWD: "/x"}
	_, ok, err := Restore(opts, "s1")
	if err != nil || ok {
		t.Fatalf("missing file should be (false,nil); ok=%v err=%v", ok, err)
	}
}
