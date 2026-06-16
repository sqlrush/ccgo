package tasktools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
	"ccgo/internal/tool"
)

type taskWorktree struct {
	Path               string
	Owned              bool
	SparsePaths        []string
	SymlinkDirectories []string
}

type taskWorktreeCleanup struct {
	Attempted bool
	Status    string
	Reason    string
}

func taskInputRequestsWorktree(raw []byte) bool {
	input, err := decodeTaskInput(raw)
	return err == nil && input.Worktree
}

func taskSidechainID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		id = string(contracts.NewID())
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\x00", "")
	return replacer.Replace(id)
}

func ensureTaskCanStart(sessionPath string, sessionID contracts.ID, taskID string) error {
	state, err := session.FindSidechainState(sessionPath, sessionID, taskID)
	if err != nil {
		return err
	}
	if state.MessageCount > 0 && state.Status == session.SidechainStatusRunning {
		return fmt.Errorf("sidechain %s is already running", state.ID)
	}
	return nil
}

func prepareTaskWorktree(ctx tool.Context, taskID string, requested bool) (taskWorktree, error) {
	cwd := strings.TrimSpace(ctx.WorkingDirectory)
	if !requested {
		return taskWorktree{Path: cwd}, nil
	}
	if cwd == "" {
		return taskWorktree{}, fmt.Errorf("working directory is required for worktree isolation")
	}
	root, err := taskGitRoot(ctx, cwd)
	if err != nil {
		return taskWorktree{}, fmt.Errorf("cannot create isolated worktree: %w", err)
	}
	base := taskManagedWorktreeBase(root)
	path := filepath.Join(base, taskWorktreeName(root, ctx.SessionID, taskID))
	if _, err := os.Stat(path); err == nil {
		return taskWorktree{}, fmt.Errorf("cannot create isolated worktree: %s already exists", path)
	} else if err != nil && !os.IsNotExist(err) {
		return taskWorktree{}, err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return taskWorktree{}, err
	}
	head, err := taskGitOutput(ctx, cwd, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return taskWorktree{}, fmt.Errorf("cannot create isolated worktree: repository has no HEAD: %w", err)
	}
	if _, err := taskGitOutput(ctx, cwd, "worktree", "add", "--detach", path, strings.TrimSpace(head)); err != nil {
		return taskWorktree{}, fmt.Errorf("cannot create isolated worktree: %w", err)
	}
	applied, err := applyTaskWorktreeSettings(ctx, root, path)
	if err != nil {
		_ = removePreparedTaskWorktree(ctx, path)
		return taskWorktree{}, fmt.Errorf("cannot configure isolated worktree: %w", err)
	}
	return taskWorktree{
		Path:               path,
		Owned:              true,
		SparsePaths:        append([]string(nil), applied.SparsePaths...),
		SymlinkDirectories: append([]string(nil), applied.SymlinkDirectories...),
	}, nil
}

func removePreparedTaskWorktree(ctx tool.Context, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := taskGitOutput(ctx, ctx.WorkingDirectory, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	return nil
}

func cleanupOwnedTaskWorktree(ctx tool.Context, manager session.SidechainManager, state session.SidechainState, reason string) (taskWorktreeCleanup, error) {
	if !taskShouldCleanupOwnedWorktree(state) {
		return taskWorktreeCleanup{}, nil
	}
	cleanup := taskWorktreeCleanup{Attempted: true}
	path := strings.TrimSpace(state.Metadata.WorktreePath)
	safe, safeErr := taskIsManagedWorktreePath(ctx, path)
	switch {
	case safeErr != nil:
		cleanup.Status = "failed"
		cleanup.Reason = safeErr.Error()
	case !safe:
		cleanup.Status = "skipped"
		cleanup.Reason = "worktree path is outside ccgo managed worktree directory"
	default:
		if _, err := os.Stat(path); os.IsNotExist(err) {
			cleanup.Status = "missing"
			cleanup.Reason = "worktree path no longer exists"
		} else if err != nil {
			cleanup.Status = "failed"
			cleanup.Reason = err.Error()
		} else if err := removePreparedTaskWorktree(ctx, path); err != nil {
			cleanup.Status = "failed"
			cleanup.Reason = err.Error()
		} else {
			cleanup.Status = "removed"
			cleanup.Reason = strings.TrimSpace(reason)
			if cleanup.Reason == "" {
				cleanup.Reason = "owned worktree removed"
			}
		}
	}
	if _, err := manager.MarkWorktreeCleanup(state.ID, cleanup.Status, cleanup.Reason, time.Now().UTC()); err != nil {
		return cleanup, err
	}
	return cleanup, nil
}

func taskShouldCleanupOwnedWorktree(state session.SidechainState) bool {
	if !state.Metadata.WorktreeOwned || strings.TrimSpace(state.Metadata.WorktreePath) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(state.Metadata.WorktreeCleanupStatus)) {
	case "removed", "missing", "skipped":
		return false
	default:
		return true
	}
}

func taskGitRoot(ctx tool.Context, cwd string) (string, error) {
	out, err := taskGitOutput(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(out)
	if root == "" {
		return "", fmt.Errorf("git root is empty")
	}
	return filepath.Abs(root)
}

func taskManagedWorktreeBase(root string) string {
	return filepath.Join(filepath.Dir(root), ".ccgo-worktrees")
}

func applyTaskWorktreeSettings(ctx tool.Context, root string, worktreePath string) (contracts.WorktreeSetting, error) {
	settings := taskSettingsFromMetadata(ctx.Metadata)
	if settings.Worktree == nil {
		return contracts.WorktreeSetting{}, nil
	}
	sparsePaths, err := cleanTaskWorktreeRelPaths(settings.Worktree.SparsePaths)
	if err != nil {
		return contracts.WorktreeSetting{}, err
	}
	symlinkDirs, err := cleanTaskWorktreeRelPaths(settings.Worktree.SymlinkDirectories)
	if err != nil {
		return contracts.WorktreeSetting{}, err
	}
	if len(sparsePaths) > 0 {
		args := append([]string{"sparse-checkout", "set", "--no-cone", "--"}, sparsePaths...)
		if _, err := taskGitOutput(ctx, worktreePath, args...); err != nil {
			return contracts.WorktreeSetting{}, err
		}
	}
	appliedSymlinks := make([]string, 0, len(symlinkDirs))
	for _, rel := range symlinkDirs {
		applied, err := symlinkTaskWorktreeDirectory(root, worktreePath, rel)
		if err != nil {
			return contracts.WorktreeSetting{}, err
		}
		if applied {
			appliedSymlinks = append(appliedSymlinks, rel)
		}
	}
	return contracts.WorktreeSetting{
		SparsePaths:        sparsePaths,
		SymlinkDirectories: appliedSymlinks,
	}, nil
}

func taskSettingsFromMetadata(metadata map[string]any) contracts.Settings {
	if metadata == nil {
		return contracts.Settings{}
	}
	switch settings := metadata[tool.MetadataSettingsKey].(type) {
	case contracts.Settings:
		return settings
	case *contracts.Settings:
		if settings != nil {
			return *settings
		}
	}
	return contracts.Settings{}
}

func cleanTaskWorktreeRelPaths(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		raw := strings.TrimSpace(value)
		if raw == "" {
			continue
		}
		if filepath.IsAbs(raw) {
			return nil, fmt.Errorf("worktree path %q must be relative", value)
		}
		cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(raw)))
		if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return nil, fmt.Errorf("worktree path %q escapes repository root", value)
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out, nil
}

func symlinkTaskWorktreeDirectory(root string, worktreePath string, rel string) (bool, error) {
	source := filepath.Join(root, filepath.FromSlash(rel))
	sourceInfo, err := os.Stat(source)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !sourceInfo.IsDir() {
		return false, fmt.Errorf("worktree symlink source %s is not a directory", rel)
	}
	target := filepath.Join(worktreePath, filepath.FromSlash(rel))
	if linkTarget, err := os.Readlink(target); err == nil {
		if filepath.Clean(linkTarget) == filepath.Clean(source) {
			return true, nil
		}
		return false, fmt.Errorf("worktree symlink target %s already points to %s", rel, linkTarget)
	}
	if _, err := os.Lstat(target); err == nil {
		return false, fmt.Errorf("worktree symlink target %s already exists", rel)
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, err
	}
	if err := os.Symlink(source, target); err != nil {
		return false, err
	}
	return true, nil
}

func taskWorktreeName(root string, sessionID contracts.ID, taskID string) string {
	parts := []string{filepath.Base(root), string(sessionID), taskID}
	for i, part := range parts {
		parts[i] = sanitizeTaskWorktreePathPart(part)
	}
	return strings.Join(parts, "-")
}

func sanitizeTaskWorktreePathPart(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "task"
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	cleaned := strings.Trim(builder.String(), "-.")
	if cleaned == "" {
		return "task"
	}
	return cleaned
}

func taskIsManagedWorktreePath(ctx tool.Context, path string) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, fmt.Errorf("worktree path is empty")
	}
	root, err := taskGitRoot(ctx, ctx.WorkingDirectory)
	if err != nil {
		return false, err
	}
	base, err := filepath.Abs(taskManagedWorktreeBase(root))
	if err != nil {
		return false, err
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false, err
	}
	return rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel), nil
}

func taskGitOutput(ctx tool.Context, cwd string, args ...string) (string, error) {
	runCtx := ctx.Context
	if runCtx == nil {
		runCtx = context.Background()
	}
	cmd := exec.CommandContext(runCtx, "git", args...)
	if strings.TrimSpace(cwd) != "" {
		cmd.Dir = cwd
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text != "" {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, text)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(output), nil
}
