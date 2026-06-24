package main

import (
	"os"
	"testing"

	"ccgo/internal/contracts"
)

// TestAutoMemoryFieldsExist is a seam test verifying that contracts.Settings
// has AutoMemoryEnabled and AutoMemoryDirectory fields.
// CFG-42: autoMemoryEnabled and autoMemoryDirectory control automatic memory writes.
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

// TestResolveAutoMemoryDir_ExplicitlyDisabled verifies that when autoMemoryEnabled=false
// the directory is not returned (even if autoMemoryDirectory is set).
// CFG-42.
func TestResolveAutoMemoryDir_ExplicitlyDisabled(t *testing.T) {
	f := false
	merged := contracts.Settings{
		AutoMemoryEnabled:   &f,
		AutoMemoryDirectory: "/tmp/mem",
	}
	got := resolveAutoMemoryDir(merged, "/wd")
	if got != "" {
		t.Errorf("expected empty dir when disabled, got %q", got)
	}
}

// TestResolveAutoMemoryDir_NoDirectory verifies that when autoMemoryDirectory is empty
// the function returns "".
// CFG-42.
func TestResolveAutoMemoryDir_NoDirectory(t *testing.T) {
	got := resolveAutoMemoryDir(contracts.Settings{}, "/wd")
	if got != "" {
		t.Errorf("expected empty dir when no directory configured, got %q", got)
	}
}

// TestResolveAutoMemoryDir_AbsolutePath verifies that an absolute autoMemoryDirectory
// is returned as-is (cleaned).
// CFG-42.
func TestResolveAutoMemoryDir_AbsolutePath(t *testing.T) {
	merged := contracts.Settings{
		AutoMemoryDirectory: "/tmp/auto-mem/",
	}
	got := resolveAutoMemoryDir(merged, "/wd")
	if got != "/tmp/auto-mem" {
		t.Errorf("expected /tmp/auto-mem, got %q", got)
	}
}

// TestResolveAutoMemoryDir_TildeExpansion verifies that a ~ prefix is expanded.
// CFG-42.
func TestResolveAutoMemoryDir_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	merged := contracts.Settings{
		AutoMemoryDirectory: "~/my-memories",
	}
	got := resolveAutoMemoryDir(merged, "/wd")
	want := home + "/my-memories"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestResolveAutoMemoryDir_EnabledExplicitly verifies that when autoMemoryEnabled=true
// and a directory is set, the directory is returned.
// CFG-42: wired to runner.RelevantMemoryDir in headlessRunner.
func TestResolveAutoMemoryDir_EnabledExplicitly(t *testing.T) {
	tr := true
	merged := contracts.Settings{
		AutoMemoryEnabled:   &tr,
		AutoMemoryDirectory: "/workspace/memory",
	}
	got := resolveAutoMemoryDir(merged, "/wd")
	if got != "/workspace/memory" {
		t.Errorf("expected /workspace/memory, got %q", got)
	}
}
