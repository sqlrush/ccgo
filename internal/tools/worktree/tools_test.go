package worktreetools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func makeGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %s", args, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")
	run("git", "commit", "--allow-empty", "-m", "init")
	return dir
}

func toolCtx(dir string) tool.Context {
	return tool.Context{
		Context:          context.Background(),
		WorkingDirectory: dir,
		SessionID:        "sess_wt_test",
		Metadata:         map[string]any{},
	}
}

func TestEnterWorktreeValidation(t *testing.T) {
	t.Parallel()
	tl := NewEnterWorktreeTool()
	dir := t.TempDir()
	ctx := toolCtx(dir)
	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "empty name ok", input: `{}`, wantErr: ""},
		{name: "valid name", input: `{"name":"my-feature"}`, wantErr: ""},
		{name: "name too long", input: `{"name":"` + strings.Repeat("a", 65) + `"}`, wantErr: "at most 64"},
		{name: "empty segment", input: `{"name":"foo//bar"}`, wantErr: "empty path segments"},
		{name: "invalid chars", input: `{"name":"foo bar"}`, wantErr: "invalid characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tl.Validate(ctx, json.RawMessage(tc.input))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
			}
		})
	}
}

func TestEnterWorktreeNotInGitRepo(t *testing.T) {
	t.Parallel()
	tl := NewEnterWorktreeTool()
	dir := t.TempDir()
	ctx := toolCtx(dir)
	_, err := tl.Call(ctx, json.RawMessage(`{}`), nil)
	if err == nil || !strings.Contains(err.Error(), "git repository") {
		t.Fatalf("expected git repository error, got %v", err)
	}
}

func TestEnterWorktreeAlreadyInWorktree(t *testing.T) {
	t.Parallel()
	tl := NewEnterWorktreeTool()
	dir := makeGitRepo(t)
	ctx := toolCtx(dir)
	ctx.Metadata[MetadataWorktreePath] = "/some/existing/worktree"
	_, err := tl.Call(ctx, json.RawMessage(`{}`), nil)
	if err == nil || !strings.Contains(err.Error(), "already in a worktree") {
		t.Fatalf("expected already-in-worktree error, got %v", err)
	}
}

func TestEnterWorktreeSuccess(t *testing.T) {
	t.Parallel()
	tl := NewEnterWorktreeTool()
	dir := makeGitRepo(t)
	ctx := toolCtx(dir)
	result, err := tl.Call(ctx, json.RawMessage(`{"name":"test-wt"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wtPath, _ := result.StructuredContent["worktree_path"].(string)
	if wtPath == "" {
		t.Fatalf("expected worktree_path in structured content, got %v", result.StructuredContent)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree path %s does not exist: %v", wtPath, err)
	}
	// cleanup
	_ = exec.Command("git", "-C", dir, "worktree", "remove", "--force", wtPath).Run()
}

func TestExitWorktreeValidation(t *testing.T) {
	t.Parallel()
	tl := NewExitWorktreeTool()
	dir := t.TempDir()
	cases := []struct {
		name    string
		meta    map[string]any
		input   string
		wantErr string
	}{
		{
			name:    "missing action",
			meta:    map[string]any{MetadataWorktreePath: "/some/path"},
			input:   `{}`,
			wantErr: "action is required",
		},
		{
			name:    "invalid action",
			meta:    map[string]any{MetadataWorktreePath: "/some/path"},
			input:   `{"action":"delete"}`,
			wantErr: "keep",
		},
		{
			name:    "not in worktree",
			meta:    map[string]any{},
			input:   `{"action":"keep"}`,
			wantErr: "not in a worktree",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := toolCtx(dir)
			ctx.Metadata = tc.meta
			err := tl.Validate(ctx, json.RawMessage(tc.input))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
			}
		})
	}
}

func TestExitWorktreeNotInWorktree(t *testing.T) {
	t.Parallel()
	tl := NewExitWorktreeTool()
	dir := t.TempDir()
	ctx := toolCtx(dir)
	err := tl.Validate(ctx, json.RawMessage(`{"action":"keep"}`))
	if err == nil || !strings.Contains(err.Error(), "not in a worktree") {
		t.Fatalf("expected not-in-worktree error, got %v", err)
	}
}

func TestExitWorktreeKeep(t *testing.T) {
	t.Parallel()
	// Create repo, create worktree, then exit with keep
	dir := makeGitRepo(t)
	ctx := toolCtx(dir)
	// Manually create a worktree
	wtDir := filepath.Join(t.TempDir(), "wt-keep")
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "--detach", wtDir, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("creating worktree: %s", out)
	}
	defer func() { _ = exec.Command("git", "-C", dir, "worktree", "remove", "--force", wtDir).Run() }()

	ctx.Metadata[MetadataWorktreePath] = wtDir
	ctx.Metadata[MetadataWorktreeOriginalCWD] = dir

	tl := NewExitWorktreeTool()
	result, err := tl.Call(ctx, json.RawMessage(`{"action":"keep"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Worktree directory must still exist
	if _, err := os.Stat(wtDir); err != nil {
		t.Fatalf("worktree should still exist after keep, got: %v", err)
	}
	if result.StructuredContent["action"] != "keep" {
		t.Fatalf("unexpected structured content: %v", result.StructuredContent)
	}
}

func TestExitWorktreeRemoveClean(t *testing.T) {
	t.Parallel()
	dir := makeGitRepo(t)
	ctx := toolCtx(dir)
	wtDir := filepath.Join(t.TempDir(), "wt-remove")
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "--detach", wtDir, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("creating worktree: %s", out)
	}
	ctx.Metadata[MetadataWorktreePath] = wtDir
	ctx.Metadata[MetadataWorktreeOriginalCWD] = dir

	tl := NewExitWorktreeTool()
	_, err := tl.Call(ctx, json.RawMessage(`{"action":"remove"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Worktree directory should be gone
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Fatalf("worktree should be removed, but still exists (stat: %v)", err)
	}
}

func TestExitWorktreeRemoveDirtyRefused(t *testing.T) {
	t.Parallel()
	dir := makeGitRepo(t)
	wtDir := filepath.Join(t.TempDir(), "wt-dirty")
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "--detach", wtDir, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("creating worktree: %s", out)
	}
	defer func() { _ = exec.Command("git", "-C", dir, "worktree", "remove", "--force", wtDir).Run() }()

	// Create an uncommitted file in the worktree
	if err := os.WriteFile(filepath.Join(wtDir, "dirty.txt"), []byte("change"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := toolCtx(dir)
	ctx.Metadata[MetadataWorktreePath] = wtDir
	ctx.Metadata[MetadataWorktreeOriginalCWD] = dir

	tl := NewExitWorktreeTool()
	_, err := tl.Call(ctx, json.RawMessage(`{"action":"remove"}`), nil)
	if err == nil || !strings.Contains(err.Error(), "discard_changes=true") {
		t.Fatalf("expected dirty-worktree refusal error, got %v", err)
	}
	// With discard_changes=true it should succeed
	_, err = tl.Call(ctx, json.RawMessage(`{"action":"remove","discard_changes":true}`), nil)
	if err != nil {
		t.Fatalf("with discard_changes=true, expected success, got: %v", err)
	}
}

func TestWorktreeToolsNames(t *testing.T) {
	t.Parallel()
	if name := NewEnterWorktreeTool().Name(); name != "EnterWorktree" {
		t.Fatalf("expected EnterWorktree, got %q", name)
	}
	if name := NewExitWorktreeTool().Name(); name != "ExitWorktree" {
		t.Fatalf("expected ExitWorktree, got %q", name)
	}
}

func TestWorktreeToolsPermissionAllow(t *testing.T) {
	t.Parallel()
	ctx := tool.Context{Context: context.Background()}
	for _, tl := range []tool.Tool{NewEnterWorktreeTool(), NewExitWorktreeTool()} {
		d, err := tl.CheckPermissions(ctx, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s CheckPermissions error: %v", tl.Name(), err)
		}
		if d.Behavior != contracts.PermissionAllow {
			t.Fatalf("%s expected allow, got %v", tl.Name(), d.Behavior)
		}
	}
}
