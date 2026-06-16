package tasktools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/session"
	"ccgo/internal/tool"
)

func taskExecutor(t *testing.T) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(NewTaskTool(), NewTaskOutputTool(), NewKillTaskTool(), NewSendMessageTool(), NewTeamCreateTool(), NewTeamDeleteTool(), NewTeamOutputTool(), NewTeamSendMessageTool(), NewTeamCoordinateTool(), NewResumeTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	return tool.NewExecutor(registry)
}

func taskContext(t *testing.T) (tool.Context, string) {
	t.Helper()
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	return tool.Context{
		Context:          context.Background(),
		WorkingDirectory: filepath.Join(dir, "worktree"),
		SessionID:        "sess_task",
		Metadata: map[string]any{
			tool.MetadataSessionPathKey: transcriptPath,
		},
	}, transcriptPath
}

func taskContextWithAgents(t *testing.T, agents []tool.AgentInfo) (tool.Context, string) {
	t.Helper()
	ctx, transcriptPath := taskContext(t)
	ctx.Metadata[tool.MetadataAvailableAgentsKey] = agents
	return ctx, transcriptPath
}

func TestTaskToolStartsSidechainAndStoresPrompt(t *testing.T) {
	ctx, transcriptPath := taskContext(t)
	executor := taskExecutor(t)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task",
		Name:  "Task",
		Input: json.RawMessage(`{"taskId":"agent/one","description":"Review API","prompt":"Inspect API changes","subagentType":"general-purpose"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content.(string), "Task started: Review API") {
		t.Fatalf("content = %#v", result.Content)
	}
	if result.StructuredContent["status"] != session.SidechainStatusRunning ||
		result.StructuredContent["sidechain_id"] != "agent_one" ||
		result.StructuredContent["subagent_type"] != "general-purpose" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}

	states, err := session.ListSidechainStates(transcriptPath, ctx.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %#v", states)
	}
	state := states[0]
	if state.ID != "agent_one" || state.Status != session.SidechainStatusRunning || state.MessageCount != 2 {
		t.Fatalf("state = %#v", state)
	}
	if state.Metadata.AgentType != "general-purpose" || state.Metadata.Description != "Review API" || state.Metadata.WorktreePath != ctx.WorkingDirectory {
		t.Fatalf("metadata = %#v", state.Metadata)
	}

	transcript, err := session.LoadTranscript(state.Path)
	if err != nil {
		t.Fatal(err)
	}
	var foundPrompt bool
	for _, id := range transcript.Order {
		entry := transcript.Messages[id]
		if entry == nil || entry.Message == nil {
			continue
		}
		if msgs.TextContent(*entry.Message) == "Inspect API changes" && entry.IsSidechain && entry.AgentID == "agent_one" {
			foundPrompt = true
			break
		}
	}
	if !foundPrompt {
		t.Fatalf("sidechain transcript missing prompt: %#v", transcript.Order)
	}
}

func TestTaskOutputListsAndReadsSidechainOutput(t *testing.T) {
	ctx, transcriptPath := taskContext(t)
	executor := taskExecutor(t)

	_, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_output_start",
		Name:  "Task",
		Input: json.RawMessage(`{"id":"agent/output","description":"Review API","prompt":"Inspect API changes","subagent_type":"general-purpose"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := session.NewSidechainManager(transcriptPath, ctx.SessionID)
	assistant := msgs.AssistantText("Investigated files\nFound issue", "sonnet", nil)
	assistant.SessionID = ctx.SessionID
	if err := manager.Append("agent_output", session.TranscriptMessage{
		Type:      string(contracts.MessageAssistant),
		UUID:      assistant.UUID,
		SessionID: ctx.SessionID,
		Timestamp: time.Unix(100, 0).UTC().Format(time.RFC3339Nano),
		Message:   &assistant,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.MarkWorktreeCleanup("agent_output", "requested", "cleanup queued", time.Unix(101, 0).UTC()); err != nil {
		t.Fatal(err)
	}

	list, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_output_list",
		Name:  "TaskOutput",
		Input: json.RawMessage(`{}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list.Content.(string), "agent_output [running] general-purpose: Review API") {
		t.Fatalf("list content = %#v", list.Content)
	}
	if list.StructuredContent["count"] != 1 {
		t.Fatalf("list structured content = %#v", list.StructuredContent)
	}

	output, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_output_read",
		Name:  "AgentOutputTool",
		Input: json.RawMessage(`{"sidechainId":"agent/output"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if output.StructuredContent["status"] != session.SidechainStatusRunning || output.StructuredContent["subagent_type"] != "general-purpose" {
		t.Fatalf("output structured content = %#v", output.StructuredContent)
	}
	if output.StructuredContent["worktree_path"] != ctx.WorkingDirectory ||
		output.StructuredContent["worktree_cleanup_status"] != "requested" ||
		output.StructuredContent["worktree_cleanup_reason"] != "cleanup queued" ||
		output.StructuredContent["worktree_cleanup_at"] != time.Unix(101, 0).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("output worktree structured content = %#v", output.StructuredContent)
	}
	text, ok := output.StructuredContent["output"].(string)
	if !ok || !strings.Contains(text, "[user] Inspect API changes") || !strings.Contains(text, "[assistant] Investigated files\nFound issue") {
		t.Fatalf("output text = %#v", output.StructuredContent["output"])
	}

	tail, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_output_tail",
		Name:  "TaskOutput",
		Input: json.RawMessage(`{"taskId":"agent/output","tailLines":"1"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tail.StructuredContent["tail_lines"] != 1 || strings.TrimSpace(tail.StructuredContent["output"].(string)) != "Found issue" {
		t.Fatalf("tail structured content = %#v", tail.StructuredContent)
	}
}

func TestTaskToolCreatesAndKillCleansOwnedWorktree(t *testing.T) {
	repo := initTaskGitRepo(t)
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	ctx := tool.Context{
		Context:          context.Background(),
		WorkingDirectory: repo,
		SessionID:        "sess_task",
		Metadata: map[string]any{
			tool.MetadataSessionPathKey: transcriptPath,
			tool.MetadataSettingsKey: contracts.Settings{
				Worktree: &contracts.WorktreeSetting{
					SparsePaths:        []string{"README.md"},
					SymlinkDirectories: []string{"cache"},
				},
			},
		},
	}
	if err := os.MkdirAll(filepath.Join(repo, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "cache", "data.txt"), []byte("cached\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := taskExecutor(t)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_worktree",
		Name:  "Task",
		Input: json.RawMessage(`{"id":"agent/worktree","description":"Isolated task","prompt":"Work separately","subagent_type":"general-purpose","worktree":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	worktreePath, ok := result.StructuredContent["worktree_path"].(string)
	if !ok || worktreePath == "" || worktreePath == repo {
		t.Fatalf("worktree result = %#v", result.StructuredContent)
	}
	if result.StructuredContent["worktree_owned"] != true {
		t.Fatalf("worktree ownership result = %#v", result.StructuredContent)
	}
	if _, err := os.Stat(filepath.Join(worktreePath, "README.md")); err != nil {
		t.Fatalf("created worktree missing checkout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(worktreePath, "other.txt")); !os.IsNotExist(err) {
		t.Fatalf("sparse checkout kept excluded file: %v", err)
	}
	expectedCachePath, err := filepath.EvalSymlinks(filepath.Join(repo, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(worktreePath, "cache")); err != nil || filepath.Clean(target) != filepath.Clean(expectedCachePath) {
		t.Fatalf("worktree cache symlink = %q err=%v", target, err)
	}
	if sparsePaths, ok := result.StructuredContent["worktree_sparse_paths"].([]string); !ok || len(sparsePaths) != 1 || sparsePaths[0] != "README.md" {
		t.Fatalf("worktree sparse result = %#v", result.StructuredContent)
	}
	if symlinkDirs, ok := result.StructuredContent["worktree_symlink_directories"].([]string); !ok || len(symlinkDirs) != 1 || symlinkDirs[0] != "cache" {
		t.Fatalf("worktree symlink result = %#v", result.StructuredContent)
	}

	state, err := session.FindSidechainState(transcriptPath, ctx.SessionID, "agent/worktree")
	if err != nil {
		t.Fatal(err)
	}
	if state.Metadata.WorktreePath != worktreePath || !state.Metadata.WorktreeOwned {
		t.Fatalf("worktree metadata = %#v", state.Metadata)
	}
	if len(state.Metadata.WorktreeSparsePaths) != 1 || state.Metadata.WorktreeSparsePaths[0] != "README.md" || len(state.Metadata.WorktreeSymlinkDirs) != 1 || state.Metadata.WorktreeSymlinkDirs[0] != "cache" {
		t.Fatalf("worktree settings metadata = %#v", state.Metadata)
	}

	killed, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_worktree_kill",
		Name:  "KillTask",
		Input: json.RawMessage(`{"task_id":"agent/worktree","reason":"done testing cleanup"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if killed.StructuredContent["worktree_cleanup_attempted"] != true ||
		killed.StructuredContent["worktree_cleanup_status"] != "removed" ||
		killed.StructuredContent["worktree_cleanup_reason"] != "done testing cleanup" {
		t.Fatalf("kill cleanup structured content = %#v", killed.StructuredContent)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists after cleanup: %v", err)
	}

	output, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_worktree_output",
		Name:  "TaskOutput",
		Input: json.RawMessage(`{"task_id":"agent/worktree"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if output.StructuredContent["worktree_cleanup_status"] != "removed" ||
		output.StructuredContent["worktree_cleanup_reason"] != "done testing cleanup" {
		t.Fatalf("output cleanup structured content = %#v", output.StructuredContent)
	}
	if sparsePaths, ok := output.StructuredContent["worktree_sparse_paths"].([]string); !ok || len(sparsePaths) != 1 || sparsePaths[0] != "README.md" {
		t.Fatalf("output sparse structured content = %#v", output.StructuredContent)
	}
	if symlinkDirs, ok := output.StructuredContent["worktree_symlink_directories"].([]string); !ok || len(symlinkDirs) != 1 || symlinkDirs[0] != "cache" {
		t.Fatalf("output symlink structured content = %#v", output.StructuredContent)
	}
}

func TestTaskToolUsesSettingsDefaultWorktree(t *testing.T) {
	repo := initTaskGitRepo(t)
	defaultWorktree := true
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	ctx := tool.Context{
		Context:          context.Background(),
		WorkingDirectory: repo,
		SessionID:        "sess_task_default_worktree",
		Metadata: map[string]any{
			tool.MetadataSessionPathKey: transcriptPath,
			tool.MetadataSettingsKey: contracts.Settings{
				Worktree: &contracts.WorktreeSetting{Enabled: &defaultWorktree},
			},
		},
	}
	executor := taskExecutor(t)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_default_worktree",
		Name:  "Task",
		Input: json.RawMessage(`{"id":"agent/default-worktree","description":"Default isolated task","prompt":"Work separately","subagent_type":"general-purpose"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	worktreePath, ok := result.StructuredContent["worktree_path"].(string)
	if !ok || worktreePath == "" || worktreePath == repo {
		t.Fatalf("default worktree result = %#v", result.StructuredContent)
	}
	if result.StructuredContent["worktree_owned"] != true || result.StructuredContent["worktree_defaulted"] != true {
		t.Fatalf("default worktree ownership result = %#v", result.StructuredContent)
	}
	if _, err := os.Stat(filepath.Join(worktreePath, "README.md")); err != nil {
		t.Fatalf("created default worktree missing checkout: %v", err)
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_default_worktree_kill",
		Name:  "KillTask",
		Input: json.RawMessage(`{"task_id":"agent/default-worktree","reason":"default worktree cleanup"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("default worktree still exists after cleanup: %v", err)
	}

	disabled, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_default_worktree_disabled",
		Name:  "Task",
		Input: json.RawMessage(`{"id":"agent/no-default-worktree","description":"Stay in repo","prompt":"Use original cwd","subagent_type":"general-purpose","worktree":false}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.StructuredContent["worktree_path"] != repo || disabled.StructuredContent["worktree_owned"] == true {
		t.Fatalf("explicit worktree false result = %#v", disabled.StructuredContent)
	}
	if _, ok := disabled.StructuredContent["worktree_defaulted"]; ok {
		t.Fatalf("explicit worktree false defaulted = %#v", disabled.StructuredContent)
	}
}

func TestKillTaskCancelsRunningSidechain(t *testing.T) {
	ctx, transcriptPath := taskContext(t)
	executor := taskExecutor(t)

	_, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_kill_start",
		Name:  "Task",
		Input: json.RawMessage(`{"id":"agent/kill","description":"Long task","prompt":"Keep working","subagent_type":"general-purpose"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	killed, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_kill",
		Name:  "TaskStop",
		Input: json.RawMessage(`{"sidechain_id":"agent/kill","message":"user stopped it"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if killed.StructuredContent["killed"] != true || killed.StructuredContent["cancelled"] != true || killed.StructuredContent["status"] != session.SidechainStatusCancelled {
		t.Fatalf("kill structured content = %#v", killed.StructuredContent)
	}

	state, err := session.FindSidechainState(transcriptPath, ctx.SessionID, "agent/kill")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != session.SidechainStatusCancelled || state.Summary != "user stopped it" {
		t.Fatalf("cancelled state = %#v", state)
	}
	output, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_kill_output",
		Name:  "TaskOutput",
		Input: json.RawMessage(`{"id":"agent/kill"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if output.StructuredContent["status"] != session.SidechainStatusCancelled || output.StructuredContent["summary"] != "user stopped it" {
		t.Fatalf("cancelled output = %#v", output.StructuredContent)
	}

	again, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_kill_again",
		Name:  "KillTask",
		Input: json.RawMessage(`{"task_id":"agent/kill"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.StructuredContent["killed"] != false || !strings.Contains(again.Content.(string), "is not running") {
		t.Fatalf("second kill = %#v", again)
	}
}

func TestSendMessageAppendsToRunningSidechain(t *testing.T) {
	ctx, transcriptPath := taskContext(t)
	executor := taskExecutor(t)
	_, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_send_start",
		Name:  "Task",
		Input: json.RawMessage(`{"id":"agent/send","description":"Interactive task","prompt":"Initial prompt","subagent_type":"general-purpose"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sent, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_send",
		Name:  "SendMessage",
		Input: json.RawMessage(`{"sidechain_id":"agent/send","text":"Please continue with more detail."}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sent.StructuredContent["type"] != "send_message" ||
		sent.StructuredContent["status"] != session.SidechainStatusRunning ||
		sent.StructuredContent["message_chars"] != len("Please continue with more detail.") {
		t.Fatalf("send structured content = %#v", sent.StructuredContent)
	}
	state, err := session.FindSidechainState(transcriptPath, ctx.SessionID, "agent/send")
	if err != nil {
		t.Fatal(err)
	}
	if state.MessageCount != 3 {
		t.Fatalf("message count = %d", state.MessageCount)
	}
	resume, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_send_resume",
		Name:  "ResumeTask",
		Input: json.RawMessage(`{"task_id":"agent/send","limit":3}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	messages, ok := resume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(messages) != 3 || messages[2]["type"] != contracts.MessageUser || messages[2]["text"] != "Please continue with more detail." {
		t.Fatalf("resume messages = %#v", resume.StructuredContent["resume_messages"])
	}
}

func TestTeamCreateAndDeletePersistManifest(t *testing.T) {
	ctx, transcriptPath := taskContext(t)
	executor := taskExecutor(t)
	for _, id := range []string{"agent/team-one", "agent/team-two", "agent/coordinator"} {
		_, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_" + strings.ReplaceAll(id, "/", "_")),
			Name:  "Task",
			Input: json.RawMessage(`{"id":"` + id + `","description":"Team member","prompt":"Work as a team member","subagent_type":"general-purpose"}`),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_create_missing_coordinator",
		Name:  "TeamCreate",
		Input: json.RawMessage(`{"name":"missing/coordinator","coordinator":"missing"}`),
	}, nil); err == nil || !strings.Contains(err.Error(), "task not found: missing") {
		t.Fatalf("team create missing coordinator err = %v", err)
	}

	created, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_create",
		Name:  "TeamCreate",
		Input: json.RawMessage(`{"name":"review/team","description":"Review team","coordinator":"agent/coordinator","members":["agent/team-one","agent/team-two","agent/team-one"]}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	taskIDs, ok := created.StructuredContent["task_ids"].([]string)
	if !ok || len(taskIDs) != 2 || taskIDs[0] != "agent_team-one" || taskIDs[1] != "agent_team-two" {
		t.Fatalf("team task ids = %#v", created.StructuredContent)
	}
	if created.StructuredContent["team_id"] != "review_team" || created.StructuredContent["task_count"] != 2 || created.StructuredContent["team_count"] != 1 {
		t.Fatalf("team create structured content = %#v", created.StructuredContent)
	}
	if created.StructuredContent["coordinator_task_id"] != "agent_coordinator" {
		t.Fatalf("team create coordinator = %#v", created.StructuredContent)
	}
	manifest, err := session.LoadTeamManifest(transcriptPath, ctx.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Teams) != 1 || manifest.Teams[0].ID != "review_team" || len(manifest.Teams[0].TaskIDs) != 2 || manifest.Teams[0].CoordinatorTaskID != "agent_coordinator" {
		t.Fatalf("team manifest = %#v", manifest)
	}
	listed, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_output_list",
		Name:  "TeamOutput",
		Input: json.RawMessage(`{}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	teams, ok := listed.StructuredContent["teams"].([]map[string]any)
	if !ok || len(teams) != 1 || teams[0]["team_id"] != "review_team" || teams[0]["coordinator_task_id"] != "agent_coordinator" {
		t.Fatalf("team list = %#v", listed.StructuredContent)
	}
	read, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_output_read",
		Name:  "TeamOutput",
		Input: json.RawMessage(`{"id":"review/team"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, ok := read.StructuredContent["coordinator"].(map[string]any)
	if !ok || coordinator["task_id"] != "agent_coordinator" || coordinator["status"] != session.SidechainStatusRunning {
		t.Fatalf("team coordinator = %#v", read.StructuredContent)
	}
	tasks, ok := read.StructuredContent["tasks"].([]map[string]any)
	if !ok || len(tasks) != 2 || tasks[0]["status"] != session.SidechainStatusRunning || tasks[1]["task_id"] != "agent_team-two" {
		t.Fatalf("team read = %#v", read.StructuredContent)
	}
	if !strings.Contains(read.Content.(string), "Coordinator: agent_coordinator: running") {
		t.Fatalf("team read content = %#v", read.Content)
	}
	coordinatorSent, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_send_coordinator",
		Name:  "TeamSendMessage",
		Input: json.RawMessage(`{"team_id":"review/team","recipient":"coordinator","message":"Please coordinate the review plan."}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinatorRecipients, ok := coordinatorSent.StructuredContent["sent"].([]map[string]any)
	if !ok || len(coordinatorRecipients) != 1 || coordinatorRecipients[0]["task_id"] != "agent_coordinator" || coordinatorSent.StructuredContent["target"] != "coordinator" {
		t.Fatalf("team coordinator send structured content = %#v", coordinatorSent.StructuredContent)
	}
	coordinatorResume, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_resume_coordinator",
		Name:  "ResumeTask",
		Input: json.RawMessage(`{"task_id":"agent/coordinator","limit":3}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinatorMessages, ok := coordinatorResume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(coordinatorMessages) != 3 || coordinatorMessages[2]["text"] != "Please coordinate the review plan." {
		t.Fatalf("team coordinator resume messages = %#v", coordinatorResume.StructuredContent["resume_messages"])
	}
	sent, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_send",
		Name:  "TeamSendMessage",
		Input: json.RawMessage(`{"team_id":"review/team","message":"Please coordinate the review."}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sent.StructuredContent["target"] != "members" || sent.StructuredContent["sent_count"] != 2 || sent.StructuredContent["message_chars"] != len("Please coordinate the review.") {
		t.Fatalf("team send structured content = %#v", sent.StructuredContent)
	}
	for _, taskID := range []string{"agent/team-one", "agent/team-two"} {
		resume, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_team_resume_" + strings.ReplaceAll(taskID, "/", "_")),
			Name:  "ResumeTask",
			Input: json.RawMessage(`{"task_id":"` + taskID + `","limit":3}`),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		messages, ok := resume.StructuredContent["resume_messages"].([]map[string]any)
		if !ok || len(messages) != 3 || messages[2]["text"] != "Please coordinate the review." {
			t.Fatalf("team broadcast resume messages for %s = %#v", taskID, resume.StructuredContent["resume_messages"])
		}
	}
	coordinatorResume, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_resume_coordinator_after_members",
		Name:  "ResumeTask",
		Input: json.RawMessage(`{"task_id":"agent/coordinator","limit":4}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinatorMessages, ok = coordinatorResume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(coordinatorMessages) != 3 || coordinatorMessages[2]["text"] != "Please coordinate the review plan." {
		t.Fatalf("team coordinator messages after member broadcast = %#v", coordinatorResume.StructuredContent["resume_messages"])
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_cancel_member",
		Name:  "KillTask",
		Input: json.RawMessage(`{"task_id":"agent/team-one","reason":"member done"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_send_cancelled",
		Name:  "TeamSendMessage",
		Input: json.RawMessage(`{"team_id":"review/team","message":"Should not partially send."}`),
	}, nil); err == nil || !strings.Contains(err.Error(), "task agent_team-one is not running") {
		t.Fatalf("team send after cancellation err = %v", err)
	}

	deleted, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_delete",
		Name:  "TeamDelete",
		Input: json.RawMessage(`{"team_id":"review/team"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.StructuredContent["deleted"] != true || deleted.StructuredContent["team_id"] != "review_team" || deleted.StructuredContent["team_count"] != 0 {
		t.Fatalf("team delete structured content = %#v", deleted.StructuredContent)
	}
	manifest, err = session.LoadTeamManifest(transcriptPath, ctx.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Teams) != 0 {
		t.Fatalf("team manifest after delete = %#v", manifest)
	}
}

func TestTeamSendMessageSupportsCoordinatorOnlyTeam(t *testing.T) {
	ctx, _ := taskContext(t)
	executor := taskExecutor(t)
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_coordinator_only_task",
		Name:  "Task",
		Input: json.RawMessage(`{"id":"agent/coordinator-only","description":"Coordinator","prompt":"Coordinate work","subagent_type":"general-purpose"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_coordinator_only_team",
		Name:  "TeamCreate",
		Input: json.RawMessage(`{"name":"lead/team","coordinator":"agent/coordinator-only"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	sent, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_coordinator_only_send",
		Name:  "TeamSendMessage",
		Input: json.RawMessage(`{"team_id":"lead/team","target":"coordinator","message":"Lead the next step."}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sent.StructuredContent["target"] != "coordinator" || sent.StructuredContent["sent_count"] != 1 {
		t.Fatalf("coordinator-only send structured content = %#v", sent.StructuredContent)
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_coordinator_only_members",
		Name:  "TeamSendMessage",
		Input: json.RawMessage(`{"team_id":"lead/team","message":"Members only."}`),
	}, nil); err == nil || !strings.Contains(err.Error(), "team lead_team has no tasks") {
		t.Fatalf("coordinator-only members err = %v", err)
	}
}

func TestTeamCoordinateSendsBriefingToCoordinator(t *testing.T) {
	ctx, _ := taskContext(t)
	executor := taskExecutor(t)
	for _, id := range []string{"agent/lead", "agent/member"} {
		if _, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_coordinate_" + strings.ReplaceAll(id, "/", "_")),
			Name:  "Task",
			Input: json.RawMessage(`{"id":"` + id + `","description":"Team task","prompt":"Work with the team","subagent_type":"general-purpose"}`),
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_coordinate_team",
		Name:  "TeamCreate",
		Input: json.RawMessage(`{"name":"coordinate/team","description":"Coordinate team","coordinator":"agent/lead","members":["agent/member"]}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_coordinate_request",
		Name:  "TeamCoordinate",
		Input: json.RawMessage(`{"team_id":"coordinate/team","objective":"Plan the next review step."}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["type"] != "team_coordinate" || result.StructuredContent["coordinator_task_id"] != "agent_lead" || result.StructuredContent["message_chars"] != len("Plan the next review step.") {
		t.Fatalf("team coordinate structured content = %#v", result.StructuredContent)
	}
	coordinator, ok := result.StructuredContent["coordinator"].(map[string]any)
	if !ok || coordinator["task_id"] != "agent_lead" || coordinator["status"] != session.SidechainStatusRunning {
		t.Fatalf("team coordinate coordinator = %#v", result.StructuredContent)
	}
	resume, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_coordinate_resume",
		Name:  "ResumeTask",
		Input: json.RawMessage(`{"task_id":"agent/lead","limit":3}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	messages, ok := resume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("coordinate resume messages = %#v", resume.StructuredContent["resume_messages"])
	}
	text, _ := messages[2]["text"].(string)
	if !strings.Contains(text, "Team coordination request for coordinate_team.") || !strings.Contains(text, "- agent_member: running") || !strings.Contains(text, "Objective:\nPlan the next review step.") {
		t.Fatalf("coordinate briefing = %q", text)
	}
}

func TestResumeTaskBuildsTruncatedContextWithAgentPrompt(t *testing.T) {
	ctx, transcriptPath := taskContextWithAgents(t, []tool.AgentInfo{{
		Name:        "demo:reviewer",
		Description: "Review changes",
		Prompt:      "Review carefully.",
	}})
	executor := taskExecutor(t)
	_, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_resume_start",
		Name:  "Task",
		Input: json.RawMessage(`{"id":"agent/resume","description":"Review API","prompt":"Inspect API changes","subagent_type":"demo:reviewer"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := session.NewSidechainManager(transcriptPath, ctx.SessionID)
	assistant := msgs.AssistantText("Partial answer", "sonnet", nil)
	assistant.SessionID = ctx.SessionID
	if err := manager.Append("agent_resume", session.TranscriptMessage{
		Type:      string(contracts.MessageAssistant),
		UUID:      assistant.UUID,
		SessionID: ctx.SessionID,
		Timestamp: time.Unix(200, 0).UTC().Format(time.RFC3339Nano),
		Message:   &assistant,
	}); err != nil {
		t.Fatal(err)
	}

	resume, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_resume",
		Name:  "TaskResume",
		Input: json.RawMessage(`{"sidechainId":"agent/resume","messageLimit":"1"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resume.StructuredContent["can_resume"] != true || resume.StructuredContent["truncated"] != true || resume.StructuredContent["message_limit"] != 1 {
		t.Fatalf("resume structured content = %#v", resume.StructuredContent)
	}
	messages, ok := resume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("resume messages = %#v", resume.StructuredContent["resume_messages"])
	}
	if messages[0]["type"] != contracts.MessageSystem || messages[0]["subtype"] != "agent_prompt" || messages[0]["is_meta"] != true || messages[0]["text"] != "Review carefully." {
		t.Fatalf("agent prompt resume message = %#v", messages[0])
	}
	if messages[1]["type"] != contracts.MessageAssistant || messages[1]["text"] != "Partial answer" {
		t.Fatalf("tail resume message = %#v", messages[1])
	}
	if !strings.Contains(resume.Content.(string), "Task agent_resume can be resumed") || !strings.Contains(resume.Content.(string), "Resume context truncated to 1 messages") {
		t.Fatalf("resume content = %#v", resume.Content)
	}
}

func TestTaskToolUsesAvailableAgentsInPromptSchemaAndValidation(t *testing.T) {
	ctx, transcriptPath := taskContextWithAgents(t, []tool.AgentInfo{{
		Name:           "demo:reviewer",
		Description:    "Review changes",
		Path:           "/tmp/reviewer.md",
		Prompt:         "Review with plugin instructions.",
		Model:          "opus",
		PermissionMode: contracts.PermissionBypassPermissions,
		AllowedTools:   []string{"Read", "Edit"},
	}})
	task := NewTaskTool()

	prompt, err := task.Prompt(tool.PromptContext{Metadata: ctx.Metadata})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "general-purpose") || !strings.Contains(prompt, "demo:reviewer: Review changes") {
		t.Fatalf("prompt = %q", prompt)
	}
	schema := task.InputSchema(tool.PromptContext{Metadata: ctx.Metadata})
	properties := schema["properties"].(map[string]any)
	subagent := properties["subagent_type"].(map[string]any)
	enumValues, ok := subagent["enum"].([]any)
	if !ok || !containsEnum(enumValues, "general-purpose") || !containsEnum(enumValues, "demo:reviewer") {
		t.Fatalf("schema enum = %#v", subagent["enum"])
	}

	executor := taskExecutor(t)
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_plugin_agent",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"Review API","prompt":"Inspect API changes","subagent_type":"demo:reviewer"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["subagent_type"] != "demo:reviewer" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if result.StructuredContent["agent_path"] != "/tmp/reviewer.md" || result.StructuredContent["agent_prompt_chars"] != len("Review with plugin instructions.") {
		t.Fatalf("structured agent metadata = %#v", result.StructuredContent)
	}
	if result.StructuredContent["agent_model"] != "opus" || result.StructuredContent["agent_permission_mode"] != string(contracts.PermissionBypassPermissions) {
		t.Fatalf("structured agent runtime metadata = %#v", result.StructuredContent)
	}
	allowedTools, ok := result.StructuredContent["agent_allowed_tools"].([]string)
	if !ok || len(allowedTools) != 2 || allowedTools[0] != "Read" || allowedTools[1] != "Edit" {
		t.Fatalf("structured allowed tools = %#v", result.StructuredContent["agent_allowed_tools"])
	}
	states, err := session.ListSidechainStates(transcriptPath, ctx.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].MessageCount != 3 {
		t.Fatalf("states = %#v", states)
	}
	if states[0].Metadata.AgentPath != "/tmp/reviewer.md" || states[0].Metadata.AgentPrompt != "Review with plugin instructions." || states[0].Metadata.AgentModel != "opus" || states[0].Metadata.AgentPermissionMode != string(contracts.PermissionBypassPermissions) || len(states[0].Metadata.AgentAllowedTools) != 2 {
		t.Fatalf("metadata = %#v", states[0].Metadata)
	}
	transcript, err := session.LoadTranscript(states[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	var foundAgentPrompt bool
	for _, id := range transcript.Order {
		entry := transcript.Messages[id]
		if entry == nil || entry.Subtype != "agent_prompt" || entry.Message == nil {
			continue
		}
		if msgs.TextContent(*entry.Message) == "Review with plugin instructions." {
			foundAgentPrompt = true
			break
		}
	}
	if !foundAgentPrompt {
		t.Fatalf("sidechain transcript missing agent prompt: %#v", transcript.Order)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_unknown_agent",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"Review API","prompt":"Inspect API changes","subagent_type":"missing:agent"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), `subagent_type "missing:agent" is not available`) {
		t.Fatalf("err = %v", err)
	}
}

func TestTaskToolValidatesRuntimeContext(t *testing.T) {
	executor := taskExecutor(t)
	ctx := tool.Context{Context: context.Background(), SessionID: "sess_task"}
	_, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_invalid",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"Review API","prompt":"Inspect API changes","subagent_type":"general-purpose"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "session path is required") {
		t.Fatalf("err = %v", err)
	}

	ctx, _ = taskContext(t)
	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_missing_prompt",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"Review API","subagent_type":"general-purpose"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestTaskOutputAndKillValidation(t *testing.T) {
	executor := taskExecutor(t)
	ctx, _ := taskContext(t)
	tests := []struct {
		name  string
		tool  string
		input string
		want  string
	}{
		{name: "bad output tail", tool: "TaskOutput", input: `{"tail_lines":0}`, want: "tail_lines must be positive"},
		{name: "unknown output field", tool: "TaskOutput", input: `{"extra":true}`, want: "input.extra is not allowed"},
		{name: "missing task", tool: "TaskOutput", input: `{"task_id":"missing"}`, want: "task not found: missing"},
		{name: "missing kill id", tool: "KillTask", input: `{}`, want: "task_id is required"},
		{name: "unknown kill field", tool: "KillTask", input: `{"task_id":"missing","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing kill task", tool: "KillTask", input: `{"id":"missing"}`, want: "task not found: missing"},
		{name: "missing send id", tool: "SendMessage", input: `{"message":"hello"}`, want: "task_id is required"},
		{name: "missing send message", tool: "SendMessage", input: `{"task_id":"missing"}`, want: "message is required"},
		{name: "unknown send field", tool: "SendMessage", input: `{"task_id":"missing","message":"hello","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing send task", tool: "SendMessage", input: `{"task_id":"missing","message":"hello"}`, want: "task not found: missing"},
		{name: "missing team id", tool: "TeamCreate", input: `{}`, want: "team_id is required"},
		{name: "unknown team field", tool: "TeamCreate", input: `{"team_id":"team","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing team task", tool: "TeamCreate", input: `{"team_id":"team","task_ids":["missing"]}`, want: "task not found: missing"},
		{name: "missing delete team id", tool: "TeamDelete", input: `{}`, want: "team_id is required"},
		{name: "missing delete team", tool: "TeamDelete", input: `{"team_id":"missing"}`, want: "team not found: missing"},
		{name: "unknown team output field", tool: "TeamOutput", input: `{"extra":true}`, want: "input.extra is not allowed"},
		{name: "missing team output team", tool: "TeamOutput", input: `{"team_id":"missing"}`, want: "team not found: missing"},
		{name: "missing team send id", tool: "TeamSendMessage", input: `{"message":"hello"}`, want: "team_id is required"},
		{name: "missing team send message", tool: "TeamSendMessage", input: `{"team_id":"missing"}`, want: "message is required"},
		{name: "unknown team send field", tool: "TeamSendMessage", input: `{"team_id":"missing","message":"hello","extra":true}`, want: "input.extra is not allowed"},
		{name: "bad team send target", tool: "TeamSendMessage", input: `{"team_id":"missing","message":"hello","target":"leaders"}`, want: "target must be one of members, coordinator, all"},
		{name: "missing team send team", tool: "TeamSendMessage", input: `{"team_id":"missing","message":"hello"}`, want: "team not found: missing"},
		{name: "missing team coordinate id", tool: "TeamCoordinate", input: `{"message":"hello"}`, want: "team_id is required"},
		{name: "missing team coordinate message", tool: "TeamCoordinate", input: `{"team_id":"missing"}`, want: "message is required"},
		{name: "unknown team coordinate field", tool: "TeamCoordinate", input: `{"team_id":"missing","message":"hello","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing team coordinate team", tool: "TeamCoordinate", input: `{"team_id":"missing","message":"hello"}`, want: "team not found: missing"},
		{name: "bad resume limit", tool: "ResumeTask", input: `{"task_id":"missing","limit":0}`, want: "limit must be positive"},
		{name: "missing resume task", tool: "ResumeTask", input: `{"id":"missing"}`, want: "task not found: missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(ctx, contracts.ToolUse{
				ID:    "toolu_task_invalid",
				Name:  tt.tool,
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestTaskToolDefinitionIsPermissionSafeButOrdered(t *testing.T) {
	task := NewTaskTool()
	if task.IsReadOnly(nil) {
		t.Fatalf("Task should not be read-only without an explicit worktree opt-out")
	}
	if !task.IsReadOnly(json.RawMessage(`{"description":"Review API","prompt":"Inspect API changes","subagent_type":"general-purpose","worktree":false}`)) {
		t.Fatalf("Task with explicit worktree false should be read-only for permission decisions")
	}
	if task.IsReadOnly(json.RawMessage(`{"description":"Review API","prompt":"Inspect API changes","subagent_type":"general-purpose","worktree":true}`)) {
		t.Fatalf("Task with worktree isolation should not be read-only")
	}
	if task.IsConcurrencySafe(nil) {
		t.Fatalf("Task should preserve ordered sidechain lifecycle updates")
	}
	if task.IsDestructive(nil) {
		t.Fatalf("Task should not be destructive")
	}
}

func containsEnum(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func initTaskGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for worktree tests")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runTaskGitTest(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "other.txt"), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTaskGitTest(t, repo, "add", "README.md", "other.txt")
	runTaskGitTest(t, repo, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	return repo
}

func runTaskGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}
