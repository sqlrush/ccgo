package sandbox

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ── SBX-54: ViolationStore ──────────────────────────────────────────────────

func TestViolationStoreRecordAndAll(t *testing.T) {
	s := &ViolationStore{}
	v1 := Violation{Rule: "file-write*", Path: "/etc/hosts", Operation: "write"}
	v2 := Violation{Rule: "network*", Operation: "network"}
	s.Record(v1)
	s.Record(v2)
	all := s.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(all))
	}
	if all[0].Rule != "file-write*" {
		t.Fatalf("first violation rule: got %q want file-write*", all[0].Rule)
	}
	// Timestamps should be populated by Record.
	if all[0].At.IsZero() {
		t.Fatal("Violation.At should be populated by Record")
	}
}

func TestViolationStoreReset(t *testing.T) {
	s := &ViolationStore{}
	s.Record(Violation{Rule: "network*"})
	s.Reset()
	if len(s.All()) != 0 {
		t.Fatal("Reset must clear all violations")
	}
}

func TestViolationStoreAllReturnsSnapshot(t *testing.T) {
	// Mutating the returned slice must not affect the store.
	s := &ViolationStore{}
	s.Record(Violation{Rule: "file-write*"})
	all := s.All()
	all[0].Rule = "mutated"
	if s.All()[0].Rule == "mutated" {
		t.Fatal("All() must return a copy, not a reference to internal slice")
	}
}

func TestViolationStoreConcurrentSafe(t *testing.T) {
	// -race will catch data races here.
	s := &ViolationStore{}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Record(Violation{Rule: "file-write*", At: time.Now()})
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.All()
		}()
	}
	wg.Wait()
}

// ── SBX-57: IgnoreViolationsConfig.ShouldIgnore ────────────────────────────

func TestIgnoreViolationsNil(t *testing.T) {
	var cfg IgnoreViolationsConfig
	v := Violation{Rule: "network*", Path: "/"}
	if cfg.ShouldIgnore(v) {
		t.Fatal("nil config must not ignore any violation")
	}
}

func TestIgnoreViolationsUnknownRule(t *testing.T) {
	cfg := IgnoreViolationsConfig{"file-write*": {"api.internal"}}
	v := Violation{Rule: "network*", Path: "api.internal"}
	if cfg.ShouldIgnore(v) {
		t.Fatal("rule not in config must not be ignored")
	}
}

func TestIgnoreViolationsEmptySliceIgnoresAll(t *testing.T) {
	cfg := IgnoreViolationsConfig{"network*": nil}
	v := Violation{Rule: "network*", Path: "evil.com"}
	if !cfg.ShouldIgnore(v) {
		t.Fatal("empty pattern slice must ignore all violations of the rule")
	}
}

func TestIgnoreViolationsPathPrefix(t *testing.T) {
	cfg := IgnoreViolationsConfig{"network*": {"api.internal"}}
	v := Violation{Rule: "network*", Path: "api.internal/endpoint"}
	if !cfg.ShouldIgnore(v) {
		t.Fatal("path prefix match must cause violation to be ignored")
	}
}

func TestIgnoreViolationsPathNoMatch(t *testing.T) {
	cfg := IgnoreViolationsConfig{"network*": {"api.internal"}}
	v := Violation{Rule: "network*", Path: "evil.com"}
	if cfg.ShouldIgnore(v) {
		t.Fatal("non-matching path must not be ignored")
	}
}

// ── SBX-56: RemoveSandboxViolationTags ─────────────────────────────────────

func TestRemoveSandboxViolationTagsEmpty(t *testing.T) {
	if got := RemoveSandboxViolationTags("no tags here"); got != "no tags here" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestRemoveSandboxViolationTagsStrips(t *testing.T) {
	input := "before\n<sandbox_violations>Sandbox: foo deny file-write* /etc</sandbox_violations>\nafter"
	got := RemoveSandboxViolationTags(input)
	if strings.Contains(got, "<sandbox_violations>") {
		t.Fatalf("tags not stripped: %q", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("surrounding text must be preserved: %q", got)
	}
}

func TestRemoveSandboxViolationTagsMultiple(t *testing.T) {
	input := "<sandbox_violations>a</sandbox_violations> mid <sandbox_violations>b</sandbox_violations>"
	got := RemoveSandboxViolationTags(input)
	if strings.Contains(got, "<sandbox_violations>") {
		t.Fatalf("not all tags removed: %q", got)
	}
	if !strings.Contains(got, "mid") {
		t.Fatalf("content between tags must be preserved: %q", got)
	}
}

// ── SBX-55: AnnotateStderrWithSandboxFailures ──────────────────────────────

func TestAnnotateStderrNoDenials(t *testing.T) {
	stderr := "Permission denied (not a sandbox tag)"
	got := AnnotateStderrWithSandboxFailures(stderr, nil, nil)
	if got != stderr {
		t.Fatalf("stderr without sandbox violations must be unchanged; got %q", got)
	}
}

func TestAnnotateStderrRecordsViolations(t *testing.T) {
	stderr := "error\n<sandbox_violations>Sandbox: foo(1) deny(1) file-write-data /etc/passwd</sandbox_violations>\n"
	store := &ViolationStore{}
	got := AnnotateStderrWithSandboxFailures(stderr, store, nil)
	// Tags must be stripped.
	if strings.Contains(got, "<sandbox_violations>") {
		t.Fatalf("tags must be removed from annotated output: %q", got)
	}
	// Human-readable annotation must appear.
	if !strings.Contains(got, "sandbox") {
		t.Fatalf("annotated stderr must contain sandbox summary: %q", got)
	}
	// Violation must be recorded in store.
	all := store.All()
	if len(all) == 0 {
		t.Fatal("violation must be recorded in store")
	}
	if all[0].Rule != "file-write-data" {
		t.Fatalf("violation rule: got %q want file-write-data", all[0].Rule)
	}
}

func TestAnnotateStderrIgnoresViolations(t *testing.T) {
	stderr := "<sandbox_violations>Sandbox: foo(1) deny(1) network-outbound api.internal</sandbox_violations>"
	store := &ViolationStore{}
	cfg := IgnoreViolationsConfig{"network-outbound": {"api.internal"}}
	got := AnnotateStderrWithSandboxFailures(stderr, store, cfg)
	// Violation is suppressed — no annotation.
	if strings.Contains(got, "blocked operations") {
		t.Fatalf("ignored violation must not produce annotation: %q", got)
	}
	// Store must also be empty.
	if len(store.All()) != 0 {
		t.Fatal("ignored violation must not be recorded in store")
	}
}

// ── ViolationString ─────────────────────────────────────────────────────────

func TestViolationStringWithPath(t *testing.T) {
	v := Violation{Rule: "file-write*", Path: "/etc/hosts", Operation: "write"}
	s := v.String()
	if !strings.Contains(s, "/etc/hosts") || !strings.Contains(s, "write") {
		t.Fatalf("String() missing path or operation: %q", s)
	}
}

func TestViolationStringNoPath(t *testing.T) {
	v := Violation{Rule: "network*", Operation: "network"}
	s := v.String()
	if !strings.Contains(s, "network") {
		t.Fatalf("String() missing operation: %q", s)
	}
}
