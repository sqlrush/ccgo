package bashtools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ccgo/internal/tool"
)

func TestCWDStateGetSet(t *testing.T) {
	s := NewCWDState("/initial")
	if got := s.Get(); got != "/initial" {
		t.Fatalf("Get() = %q, want /initial", got)
	}
	s.Set("/updated")
	if got := s.Get(); got != "/updated" {
		t.Fatalf("Get() after Set() = %q, want /updated", got)
	}
}

func TestCWDStateConcurrentAccess(t *testing.T) {
	s := NewCWDState("/start")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.Set("/concurrent")
		}()
		go func() {
			defer wg.Done()
			_ = s.Get()
		}()
	}
	wg.Wait()
}

func TestBashEffectiveCWDFallsBackToContext(t *testing.T) {
	ctx := tool.Context{WorkingDirectory: "/repo"}
	if got := bashEffectiveCWD(ctx); got != "/repo" {
		t.Fatalf("bashEffectiveCWD = %q want /repo", got)
	}
}

func TestBashEffectiveCWDUsesState(t *testing.T) {
	state := NewCWDState("/from-state")
	ctx := tool.Context{
		WorkingDirectory: "/from-ctx",
		Metadata:         map[string]any{MetadataBashCWDKey: state},
	}
	if got := bashEffectiveCWD(ctx); got != "/from-state" {
		t.Fatalf("bashEffectiveCWD = %q want /from-state", got)
	}
}

func TestBashCWDPersistsAcrossCalls(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	state := NewCWDState(root)
	ctx := tool.Context{
		Context:          context.Background(),
		WorkingDirectory: root,
		Metadata:         map[string]any{MetadataBashCWDKey: state},
	}
	// First call: cd into sub.
	raw1, _ := json.Marshal(map[string]any{"command": "cd sub"})
	if _, err := NewBashTool().Call(ctx, raw1, tool.NopProgressSink()); err != nil {
		t.Fatalf("call 1 err: %v", err)
	}
	if got := state.Get(); got != sub {
		t.Fatalf("cwd after cd = %q want %q", got, sub)
	}
	// Second call: pwd should report sub, proving persistence.
	raw2, _ := json.Marshal(map[string]any{"command": "pwd"})
	res, err := NewBashTool().Call(ctx, raw2, tool.NopProgressSink())
	if err != nil {
		t.Fatalf("call 2 err: %v", err)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "sub") {
		t.Fatalf("pwd output = %q want it to contain sub", content)
	}
}

func TestBashCWDRelativeResolves(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(a, "b")
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	state := NewCWDState(root)
	ctx := tool.Context{
		Context:          context.Background(),
		WorkingDirectory: root,
		Metadata:         map[string]any{MetadataBashCWDKey: state},
	}
	// cd a/b (relative, multi-segment)
	raw, _ := json.Marshal(map[string]any{"command": "cd a/b"})
	if _, err := NewBashTool().Call(ctx, raw, tool.NopProgressSink()); err != nil {
		t.Fatalf("call err: %v", err)
	}
	if got := state.Get(); got != b {
		t.Fatalf("cwd after cd a/b = %q want %q", got, b)
	}
}

func TestBashCWDNonExistentDoesNotChange(t *testing.T) {
	root := t.TempDir()
	state := NewCWDState(root)
	ctx := tool.Context{
		Context:          context.Background(),
		WorkingDirectory: root,
		Metadata:         map[string]any{MetadataBashCWDKey: state},
	}
	raw, _ := json.Marshal(map[string]any{"command": "cd /does/not/exist/at/all"})
	// The bash command itself will fail, but we care that the cwd state did not change.
	NewBashTool().Call(ctx, raw, tool.NopProgressSink()) //nolint:errcheck
	if got := state.Get(); got != root {
		t.Fatalf("cwd after failed cd = %q want unchanged %q", got, root)
	}
}

func TestBashCWDNoStateFallsBackUnchanged(t *testing.T) {
	root := t.TempDir()
	// No MetadataBashCWDKey injected — behavior must be unchanged (ctx.WorkingDirectory).
	ctx := tool.Context{
		Context:          context.Background(),
		WorkingDirectory: root,
	}
	raw, _ := json.Marshal(map[string]any{"command": "pwd"})
	res, err := NewBashTool().Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatalf("call err: %v", err)
	}
	content, _ := res.Content.(string)
	// pwd output should reflect root (ctx.WorkingDirectory).
	if !strings.Contains(content, filepath.Base(root)) {
		t.Fatalf("pwd output = %q does not contain root %q", content, root)
	}
}
