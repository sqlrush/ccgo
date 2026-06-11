package bashtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
	"ccgo/internal/tool"
)

func bashExecutor(t *testing.T) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(NewBashTool(), NewBashOutputTool(), NewKillBashTool())
	if err != nil {
		t.Fatal(err)
	}
	return tool.NewExecutor(registry)
}

func TestBashRunsCommandAndReturnsStructuredOutput(t *testing.T) {
	dir := t.TempDir()
	executor := bashExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context:          context.Background(),
		WorkingDirectory: dir,
		Metadata:         map[string]any{},
	}, contracts.ToolUse{
		ID:    "toolu_bash",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"printf hello","description":"print greeting"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result should not be an error: %#v", result)
	}
	if result.Content != "hello" {
		t.Fatalf("content = %#v", result.Content)
	}
	if result.StructuredContent["stdout"] != "hello" || result.StructuredContent["stderr"] != "" || result.StructuredContent["exit_code"] != 0 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if result.StructuredContent["description"] != "print greeting" {
		t.Fatalf("description = %#v", result.StructuredContent["description"])
	}
}

func TestBashCapturesStderrAndExitCode(t *testing.T) {
	executor := bashExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, contracts.ToolUse{
		ID:    "toolu_bash_fail",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"printf problem >&2; exit 3"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("result should be an error: %#v", result)
	}
	content := result.Content.(string)
	if !strings.Contains(content, "problem") || !strings.Contains(content, "Command exited with code 3.") {
		t.Fatalf("content = %#v", content)
	}
	if result.StructuredContent["stderr"] != "problem" || result.StructuredContent["exit_code"] != 3 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
}

func TestBashTimeout(t *testing.T) {
	executor := bashExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, contracts.ToolUse{
		ID:    "toolu_bash_timeout",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"sleep 1","timeout":50}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("timeout should be an error: %#v", result)
	}
	if !strings.Contains(result.Content.(string), "Command timed out after 50ms.") {
		t.Fatalf("content = %#v", result.Content)
	}
	if result.StructuredContent["timed_out"] != true || result.StructuredContent["exit_code"] != -1 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
}

func TestBashValidation(t *testing.T) {
	executor := bashExecutor(t)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty command", input: `{"command":"  "}`, want: "command is required"},
		{name: "invalid timeout", input: `{"command":"pwd","timeout":0}`, want: "timeout must be positive"},
		{name: "unknown field", input: `{"command":"pwd","extra":true}`, want: "input.extra is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
				ID:    "toolu_invalid",
				Name:  "Bash",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBashCommandClassification(t *testing.T) {
	readOnly := []string{
		"pwd",
		"ls -la",
		"git status --short",
		"git diff --stat -S needle -- file.go",
		"git log --oneline --max-count 5 -- file.go",
		"git show --name-only HEAD",
		"git status --untracked-files=all -- file.go",
		"git ls-files --exclude-standard -z -- *.go",
		"git grep -n -e TODO -- *.go",
		"git branch --show-current",
		"git remote -v",
		"git remote show origin",
		"git remote show -n origin",
		"git remote get-url --push origin",
		"git rev-parse --show-toplevel",
		"git rev-parse --short=8 HEAD",
		"git reflog",
		"git reflog show HEAD",
		"git reflog --date=iso HEAD",
		"git reflog --date iso HEAD",
		"git reflog -n 5 HEAD",
		"git stash list",
		"git stash show -p stash@{0}",
		"git worktree list --porcelain",
		"git worktree list --expire 2.weeks.ago",
		"git merge-base --is-ancestor HEAD~1 HEAD",
		"git describe --tags --abbrev=8 HEAD",
		"git cat-file -p HEAD",
		"git for-each-ref --format=%(refname) refs/heads",
		"git rev-list --count --max-count 5 HEAD",
		"git blame -L 1,3 --date=iso file.go",
		"git shortlog -s -n -e --since 2.weeks.ago HEAD",
		"git config --get --global user.name",
		"rg TODO internal",
		"find . -name '*.go' -print",
		"printf hello",
	}
	for _, command := range readOnly {
		if !IsReadOnlyCommand(command) {
			t.Fatalf("%q should be read-only", command)
		}
		if IsDestructiveCommand(command) {
			t.Fatalf("%q should not be destructive", command)
		}
	}

	notReadOnly := []string{
		"echo hi > out.txt",
		`echo "$(date)"`,
		"make build",
		"git commit -m test",
		"git branch new-feature",
		"git tag v1.2.3",
		"git remote set-url origin git@example.com:repo/project.git",
		"git diff --output=/tmp/diff.patch",
		"git log --output=/tmp/log.txt",
		"git show --output=/tmp/show.txt HEAD",
		"git status --output=/tmp/status.txt",
		"git ls-files --format=%(path)",
		"git grep --open-files-in-pager TODO",
		"git remote show origin extra",
		"git remote show ../origin",
		"git remote get-url origin extra",
		"git rev-parse --output=/tmp/rev HEAD",
		"git reflog expire --all",
		"git reflog --all expire",
		"git reflog delete HEAD@{0}",
		"git reflog exists HEAD",
		"git stash",
		"git stash apply stash@{0}",
		"git stash show stash@{0} extra",
		"git stash show --output=/tmp/stash.diff",
		"git stash list --output=/tmp/stashes.txt",
		"git worktree add ../other",
		"git worktree list ../other",
		"git worktree list --output=/tmp/worktrees.txt",
		"git describe --output=/tmp/describe.txt",
		"git cat-file --batch HEAD",
		"git cat-file -p HEAD extra",
		"git for-each-ref --shell",
		"git rev-list --output=/tmp/revs HEAD",
		"git blame --contents=/tmp/file.go file.go",
		"git shortlog --output=/tmp/shortlog HEAD",
		"git config --set user.name bot",
		"git config --get --blob HEAD:.gitconfig user.name",
		"ls && echo hi > out.txt",
	}
	for _, command := range notReadOnly {
		if IsReadOnlyCommand(command) {
			t.Fatalf("%q should not be read-only", command)
		}
	}

	destructive := []string{
		"rm -rf build",
		"git reset --hard",
		"git clean -fd",
		"git branch -D old-feature",
		"git tag --delete v1.0.0",
		"git remote remove origin",
		"git push --force origin main",
		"git reflog expire --all",
		"git reflog --all expire",
		"git reflog delete HEAD@{0}",
		"git stash drop stash@{0}",
		"git stash pop",
		"git stash clear",
		"git worktree remove ../old",
		"git worktree prune",
		"find . -name '*.tmp' -delete",
		"find . -type f -exec rm {} ;",
		"find . -type f -execdir rmdir {} ;",
		"printf '%s\n' build | xargs rm -rf",
		"sudo make install",
		"chmod -R 777 .",
	}
	for _, command := range destructive {
		if !IsDestructiveCommand(command) {
			t.Fatalf("%q should be destructive", command)
		}
	}
}

func TestBashToolDynamicSafetyFlags(t *testing.T) {
	bash := NewBashTool()
	if !bash.IsReadOnly(json.RawMessage(`{"command":"git status --short"}`)) {
		t.Fatalf("git status should be read-only")
	}
	if !bash.IsConcurrencySafe(json.RawMessage(`{"command":"git status --short"}`)) {
		t.Fatalf("read-only bash command should be concurrency safe")
	}
	if bash.IsReadOnly(json.RawMessage(`{"command":"git commit -m test"}`)) {
		t.Fatalf("git commit should not be read-only")
	}
	if !bash.IsDestructive(json.RawMessage(`{"command":"rm -rf build"}`)) {
		t.Fatalf("rm should be destructive")
	}
}

func TestBashDynamicFlagsFeedPermissionDecision(t *testing.T) {
	bash := NewBashTool()
	ctx := tool.Context{
		Context: context.Background(),
		Permissions: tool.NewEnginePermissionDecider(permissions.NewEngine(contracts.PermissionContext{
			Mode: contracts.PermissionDefault,
		})),
	}
	readDecision, err := bash.CheckPermissions(ctx, json.RawMessage(`{"command":"git status --short"}`))
	if err != nil {
		t.Fatal(err)
	}
	if readDecision.Behavior != contracts.PermissionAllow {
		t.Fatalf("read decision = %#v", readDecision)
	}
	mutateDecision, err := bash.CheckPermissions(ctx, json.RawMessage(`{"command":"echo hi > out.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if mutateDecision.Behavior != contracts.PermissionAsk {
		t.Fatalf("mutate decision = %#v", mutateDecision)
	}

	allowed := NewBashTool()
	allowCtx := tool.Context{
		Context: context.Background(),
		Permissions: tool.NewEnginePermissionDecider(permissions.NewEngine(
			contracts.PermissionContext{Mode: contracts.PermissionDefault},
			permissions.MustParseRule(contracts.PermissionSourceSession, contracts.PermissionAllow, "Bash(make build*)"),
		)),
	}
	ruleDecision, err := allowed.CheckPermissions(allowCtx, json.RawMessage(`{"command":"make build-fast"}`))
	if err != nil {
		t.Fatal(err)
	}
	if ruleDecision.Behavior != contracts.PermissionAllow {
		t.Fatalf("rule decision = %#v", ruleDecision)
	}
}

func TestBashRunInBackgroundAndReadOutput(t *testing.T) {
	executor := bashExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	start, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_background",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"printf 'one\ntwo\nthree\n'","run_in_background":true,"description":"background print"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(start.Content.(string), "Command started in background with ID: bash_") {
		t.Fatalf("start content = %#v", start.Content)
	}
	bashID := start.StructuredContent["bash_id"].(string)
	if bashID == "" || start.StructuredContent["running"] != true {
		t.Fatalf("start structured content = %#v", start.StructuredContent)
	}

	output := waitForBashOutput(t, executor, ctx, bashID)
	if output.IsError {
		t.Fatalf("output should not be error: %#v", output)
	}
	if output.StructuredContent["running"] != false || output.StructuredContent["exit_code"] != 0 {
		t.Fatalf("output structured content = %#v", output.StructuredContent)
	}
	if output.StructuredContent["stdout"] != "one\ntwo\nthree\n" {
		t.Fatalf("stdout = %#v", output.StructuredContent["stdout"])
	}

	tail, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_background_tail",
		Name:  "BashOutput",
		Input: json.RawMessage(`{"bash_id":` + strconvQuote(bashID) + `,"tail_lines":2}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tail.StructuredContent["stdout"] != "two\nthree" {
		t.Fatalf("tail stdout = %#v", tail.StructuredContent["stdout"])
	}
}

func TestBashBackgroundTimeout(t *testing.T) {
	executor := bashExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	start, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_background_timeout",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"sleep 1","runInBackground":true,"timeout":50}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bashID := start.StructuredContent["bash_id"].(string)
	output := waitForBashOutput(t, executor, ctx, bashID)
	if !output.IsError {
		t.Fatalf("timeout output should be error: %#v", output)
	}
	if output.StructuredContent["timed_out"] != true || output.StructuredContent["exit_code"] != -1 {
		t.Fatalf("timeout structured content = %#v", output.StructuredContent)
	}
	if !strings.Contains(output.Content.(string), "timed out after 50ms") {
		t.Fatalf("timeout content = %#v", output.Content)
	}
}

func TestKillBashCancelsBackgroundCommand(t *testing.T) {
	executor := bashExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	start, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_background_kill_start",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"sleep 5","run_in_background":true,"timeout":5000}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bashID := start.StructuredContent["bash_id"].(string)

	killed, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_background_kill",
		Name:  "KillBash",
		Input: json.RawMessage(`{"bash_id":` + strconvQuote(bashID) + `}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if killed.StructuredContent["killed"] != true || killed.StructuredContent["cancelled"] != true {
		t.Fatalf("kill structured content = %#v", killed.StructuredContent)
	}

	output := waitForBashOutput(t, executor, ctx, bashID)
	if !output.IsError {
		t.Fatalf("cancelled output should be error: %#v", output)
	}
	if output.StructuredContent["cancelled"] != true || output.StructuredContent["timed_out"] != false {
		t.Fatalf("cancelled structured content = %#v", output.StructuredContent)
	}
	if !strings.Contains(output.Content.(string), "was cancelled") {
		t.Fatalf("cancelled content = %#v", output.Content)
	}
}

func TestBashOutputValidationAndMissingTask(t *testing.T) {
	executor := bashExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing id", input: `{}`, want: "bash_id is required"},
		{name: "bad tail", input: `{"bash_id":"bash_missing","tail_lines":0}`, want: "tail_lines must be positive"},
		{name: "unknown field", input: `{"bash_id":"bash_missing","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing task", input: `{"id":"bash_missing"}`, want: "background bash command not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(ctx, contracts.ToolUse{
				ID:    "toolu_output_invalid",
				Name:  "BashOutput",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestKillBashValidationAndMissingTask(t *testing.T) {
	executor := bashExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing id", input: `{}`, want: "bash_id is required"},
		{name: "unknown field", input: `{"bash_id":"bash_missing","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing task", input: `{"id":"bash_missing"}`, want: "background bash command not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(ctx, contracts.ToolUse{
				ID:    "toolu_kill_invalid",
				Name:  "KillBash",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func waitForBashOutput(t *testing.T, executor tool.Executor, ctx tool.Context, bashID string) contracts.ToolResult {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		output, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    "toolu_background_output",
			Name:  "BashOutput",
			Input: json.RawMessage(`{"bash_id":` + strconvQuote(bashID) + `}`),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if output.StructuredContent["running"] == false {
			return output
		}
		if time.Now().After(deadline) {
			t.Fatalf("background command %s did not finish; last output = %#v", bashID, output.StructuredContent)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func strconvQuote(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
