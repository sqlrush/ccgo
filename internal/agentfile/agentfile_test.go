package agentfile

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `---
name: reviewer
description: Reviews Go code for idioms
tools: Read, Grep, Bash
model: sonnet
color: blue
---
You are a meticulous Go reviewer. Focus on idiomatic patterns.
`

func TestParseRoundTrip(t *testing.T) {
	a, err := Parse("reviewer", []byte(sample))
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	if a.Name != "reviewer" || a.Description != "Reviews Go code for idioms" {
		t.Fatalf("bad metadata: %+v", a)
	}
	if len(a.Tools) != 3 || a.Tools[0] != "Read" || a.Tools[2] != "Bash" {
		t.Fatalf("bad tools: %v", a.Tools)
	}
	if a.Model != "sonnet" || a.Color != "blue" {
		t.Fatalf("bad model/color: %+v", a)
	}
	if a.Prompt == "" || a.Prompt[:7] != "You are" {
		t.Fatalf("bad prompt: %q", a.Prompt)
	}
	// Format must reproduce a parseable file.
	again, err := Parse("reviewer", []byte(Format(a)))
	if err != nil {
		t.Fatalf("re-parse err: %v", err)
	}
	if again.Description != a.Description || len(again.Tools) != len(a.Tools) {
		t.Fatalf("round-trip mismatch: %+v vs %+v", again, a)
	}
}

func TestSaveListDelete(t *testing.T) {
	dir := t.TempDir()
	a := AgentFile{Name: "helper", Description: "d", Prompt: "p"}
	if err := Save(dir, a); err != nil {
		t.Fatalf("Save err: %v", err)
	}
	if err := Save(dir, a); err == nil {
		t.Fatal("second Save must fail (no overwrite)")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "helper.md")); statErr != nil {
		t.Fatalf("file not written: %v", statErr)
	}
	list, err := List(dir)
	if err != nil || len(list) != 1 || list[0].Name != "helper" {
		t.Fatalf("List = %v, %v", list, err)
	}
	if err := Delete(dir, "helper"); err != nil {
		t.Fatalf("Delete err: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "helper.md")); !os.IsNotExist(statErr) {
		t.Fatal("file not deleted")
	}
}

func TestParseRejectsEmptyName(t *testing.T) {
	if _, err := Parse("", []byte("---\n---\nbody")); err == nil {
		t.Fatal("empty name must error")
	}
}

// TestParseIsolationWorktree verifies ORCH-12: agentfile isolation:worktree is
// parsed into the Isolation field so the task runner can apply worktree isolation
// automatically even when the caller did not explicitly request it.
func TestParseIsolationWorktree(t *testing.T) {
	content := []byte("---\nname: isolated-agent\nisolation: worktree\n---\nDo isolated work.\n")
	a, err := Parse("isolated-agent", content)
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	if a.Isolation != "worktree" {
		t.Errorf("Isolation = %q, want %q", a.Isolation, "worktree")
	}
	// Round-trip through Format.
	again, err := Parse("isolated-agent", []byte(Format(a)))
	if err != nil {
		t.Fatalf("re-parse err: %v", err)
	}
	if again.Isolation != "worktree" {
		t.Errorf("round-trip Isolation = %q, want %q", again.Isolation, "worktree")
	}
}

// TestParseIsolationUnknownIsIgnored verifies that an unrecognised isolation
// value is silently normalised to the empty string (only "worktree" is valid).
func TestParseIsolationUnknownIsIgnored(t *testing.T) {
	content := []byte("---\nname: foo\nisolation: remote\n---\nbody\n")
	a, err := Parse("foo", content)
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	if a.Isolation != "" {
		t.Errorf("unknown isolation should be normalised to empty string, got %q", a.Isolation)
	}
}

// TestParseBackgroundTrue verifies ORCH-13: agentfile background:true is parsed
// into the Background field so callTask can force run_in_background=true.
func TestParseBackgroundTrue(t *testing.T) {
	content := []byte("---\nname: bg-agent\nbackground: true\n---\nRun in background.\n")
	a, err := Parse("bg-agent", content)
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	if !a.Background {
		t.Error("Background should be true when frontmatter background:true")
	}
	// Round-trip.
	again, err := Parse("bg-agent", []byte(Format(a)))
	if err != nil {
		t.Fatalf("re-parse err: %v", err)
	}
	if !again.Background {
		t.Error("round-trip Background should still be true")
	}
}

// TestParseBackgroundFalse verifies that background:false (and absence) leaves Background=false.
func TestParseBackgroundFalse(t *testing.T) {
	for _, raw := range []string{
		"---\nname: x\nbackground: false\n---\nbody\n",
		"---\nname: x\n---\nbody\n",
	} {
		a, err := Parse("x", []byte(raw))
		if err != nil {
			t.Fatalf("Parse err: %v", err)
		}
		if a.Background {
			t.Errorf("Background should be false for input %q", raw)
		}
	}
}

// TestParseOmitClaudeMd verifies ORCH-35: agentfile omitClaudeMd:true is parsed
// and preserved through a round-trip so the sub-agent runner can strip the
// CLAUDE.md hierarchy from the system prompt.
func TestParseOmitClaudeMd(t *testing.T) {
	content := []byte("---\nname: lean-agent\nomitClaudeMd: true\n---\nDo lean work.\n")
	a, err := Parse("lean-agent", content)
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	if !a.OmitClaudeMd {
		t.Error("OmitClaudeMd should be true when frontmatter omitClaudeMd:true")
	}
	// Round-trip.
	again, err := Parse("lean-agent", []byte(Format(a)))
	if err != nil {
		t.Fatalf("re-parse err: %v", err)
	}
	if !again.OmitClaudeMd {
		t.Error("round-trip OmitClaudeMd should still be true")
	}
}
