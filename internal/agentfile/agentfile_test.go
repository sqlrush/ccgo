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
