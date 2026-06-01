package permissions

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestPathInWorkingPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if !PathInWorkingPath(filepath.Join(root, "src", "main.go"), root) {
		t.Fatalf("expected path inside root")
	}
	if PathInWorkingPath(root+"-other/file", root) {
		t.Fatalf("prefix-only path should not match")
	}
}

func TestValidatePathUsesAdditionalDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	extra := filepath.Join(t.TempDir(), "extra")
	got := ValidatePath(filepath.Join(extra, "a.txt"), root, FileOperationRead, map[string]contracts.PermissionRuleSource{
		extra: contracts.PermissionSourceProjectSettings,
	})
	if !got.Allowed || got.Source != contracts.PermissionSourceProjectSettings {
		t.Fatalf("got = %#v", got)
	}
}

func TestDangerousRemovalPath(t *testing.T) {
	if !IsDangerousRemovalPath("/") {
		t.Fatalf("root should be dangerous")
	}
}

func TestValidatePathBlocksSensitiveWriteTargets(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	allowed := ValidatePath(filepath.Join(root, "src", "main.go"), root, FileOperationWrite, nil)
	if !allowed.Allowed {
		t.Fatalf("ordinary file should be allowed: %#v", allowed)
	}
	for _, path := range []string{
		filepath.Join(root, ".git", "config"),
		filepath.Join(root, ".claude", "settings.json"),
		filepath.Join(root, ".vscode", "settings.json"),
		filepath.Join(root, ".bashrc"),
	} {
		got := ValidatePath(path, root, FileOperationWrite, nil)
		if got.Allowed || !strings.Contains(got.Reason, "sensitive") && !strings.Contains(got.Reason, "Claude configuration") {
			t.Fatalf("%s should be blocked as sensitive: %#v", path, got)
		}
	}
}

func TestValidatePathChecksSymlinkResolvedTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on some Windows systems")
	}
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "safe-looking")
	if err := os.Symlink(filepath.Join(root, ".git"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	got := ValidatePath(filepath.Join(link, "config"), root, FileOperationWrite, nil)
	if got.Allowed {
		t.Fatalf("symlink into .git should be blocked: %#v", got)
	}
}

func TestValidatePathDeniesSymlinkEscapeForNewChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on some Windows systems")
	}
	root := filepath.Join(t.TempDir(), "repo")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	got := ValidatePath(filepath.Join(link, "new.txt"), root, FileOperationWrite, nil)
	if got.Allowed || !strings.Contains(got.Reason, "outside") {
		t.Fatalf("symlink escape should be denied: %#v", got)
	}
}

func TestValidatePathBlocksSuspiciousWindowsPatternsOnAllPlatforms(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	got := ValidatePath(filepath.Join(root, "SETTIN~1.JSON"), root, FileOperationWrite, nil)
	if got.Allowed || !strings.Contains(got.Reason, "suspicious Windows") {
		t.Fatalf("short-name pattern should be blocked: %#v", got)
	}
}

func TestValidatePathAllowsReadableInternalPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	projectDir := filepath.Join(t.TempDir(), "claude-project")
	internal := InternalPathContext{
		ProjectDir:       projectDir,
		SessionMemoryDir: filepath.Join(projectDir, "sess_1", "session-memory"),
		ToolResultsDir:   filepath.Join(projectDir, "sess_1", "tool-results"),
		TasksDir:         filepath.Join(t.TempDir(), "tasks"),
	}
	tests := []string{
		filepath.Join(internal.SessionMemoryDir, "summary.md"),
		filepath.Join(internal.ToolResultsDir, "toolu_1.txt"),
		filepath.Join(internal.TasksDir, "task.json"),
	}
	for _, path := range tests {
		got := ValidatePathWithInternalContext(path, root, FileOperationRead, nil, internal)
		if !got.Allowed {
			t.Fatalf("%s should be internally readable: %#v", path, got)
		}
	}
}

func TestValidatePathAllowsEditableScratchpadPlanAndJobPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	claudeHome := filepath.Join(t.TempDir(), "claude")
	jobDir := filepath.Join(claudeHome, "jobs", "job_1")
	internal := InternalPathContext{
		ScratchpadEnabled: true,
		ScratchpadDir:     filepath.Join(t.TempDir(), "scratchpad"),
		PlansDir:          filepath.Join(claudeHome, "plans"),
		PlanSlug:          "plan-1",
		JobDir:            jobDir,
		JobsRoot:          filepath.Join(claudeHome, "jobs"),
	}
	tests := []string{
		filepath.Join(internal.ScratchpadDir, "note.txt"),
		filepath.Join(internal.PlansDir, "plan-1-agent-a.md"),
		filepath.Join(jobDir, "output.txt"),
	}
	for _, path := range tests {
		got := ValidatePathWithInternalContext(path, root, FileOperationWrite, nil, internal)
		if !got.Allowed {
			t.Fatalf("%s should be internally editable: %#v", path, got)
		}
	}
}

func TestValidatePathRejectsJobDirSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on some Windows systems")
	}
	root := filepath.Join(t.TempDir(), "repo")
	claudeHome := filepath.Join(t.TempDir(), "claude")
	jobsRoot := filepath.Join(claudeHome, "jobs")
	jobDir := filepath.Join(jobsRoot, "job_1")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(jobDir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	got := ValidatePathWithInternalContext(filepath.Join(link, "out.txt"), root, FileOperationWrite, nil, InternalPathContext{
		JobDir:   jobDir,
		JobsRoot: jobsRoot,
	})
	if got.Allowed {
		t.Fatalf("job symlink escape should not be internally allowed: %#v", got)
	}
}
