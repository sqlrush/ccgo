package main

import (
	"testing"

	"ccgo/internal/contracts"
)

// TestAutoMemoryFieldsExist is a seam test verifying that contracts.Settings
// has AutoMemoryEnabled and AutoMemoryDirectory fields.
// CFG-42: autoMemoryEnabled and autoMemoryDirectory control automatic memory writes.
// ⚠️  Full wiring to conversation runner deferred (requires conversation result hook).
// CC ref: utils/settings/types.ts autoMemoryEnabled / autoMemoryDirectory.
func TestAutoMemoryFieldsExist(t *testing.T) {
	enabled := true
	s := contracts.Settings{
		AutoMemoryEnabled:   &enabled,
		AutoMemoryDirectory: "/tmp/mem",
	}
	if s.AutoMemoryEnabled == nil {
		t.Fatal("expected AutoMemoryEnabled to be non-nil")
	}
	if !*s.AutoMemoryEnabled {
		t.Errorf("expected AutoMemoryEnabled=true, got false")
	}
	if s.AutoMemoryDirectory != "/tmp/mem" {
		t.Errorf("expected AutoMemoryDirectory=/tmp/mem, got %q", s.AutoMemoryDirectory)
	}
}
