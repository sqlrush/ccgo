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
	"ccgo/internal/orchestration"
	"ccgo/internal/session"
	"ccgo/internal/tool"
)

func taskExecutor(t *testing.T) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(NewTaskTool(), NewTaskOutputTool(), NewKillTaskTool(), NewSendMessageTool(), NewTeamCreateTool(), NewTeamDeleteTool(), NewTeamOutputTool(), NewTeamSendMessageTool(), NewTeamDispatchTool(), NewTeamScheduleTool(), NewTeamAutoScheduleTool(), NewTeamCoordinateTool(), NewResumeTaskTool(), NewSleepTool(), NewBriefTool(), NewScheduleCronTool(), NewRemoteTriggerTool())
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
	dispatched, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_team_dispatch",
		Name: "TeamDispatch",
		Input: json.RawMessage(`{
			"team_id":"review/team",
			"assignments":[
				{"task_id":"agent/team-one","message":"Review the API changes."},
				{"task_id":"agent/team-two","message":"Update the release notes."}
			]
		}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dispatched.StructuredContent["type"] != "team_dispatch" || dispatched.StructuredContent["assignment_count"] != 2 {
		t.Fatalf("team dispatch structured content = %#v", dispatched.StructuredContent)
	}
	for taskID, want := range map[string]string{
		"agent/team-one": "Review the API changes.",
		"agent/team-two": "Update the release notes.",
	} {
		resume, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_team_dispatch_resume_" + strings.ReplaceAll(taskID, "/", "_")),
			Name:  "ResumeTask",
			Input: json.RawMessage(`{"task_id":"` + taskID + `","limit":4}`),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		messages, ok := resume.StructuredContent["resume_messages"].([]map[string]any)
		if !ok || len(messages) != 4 {
			t.Fatalf("team dispatch resume messages for %s = %#v", taskID, resume.StructuredContent["resume_messages"])
		}
		text, _ := messages[3]["text"].(string)
		if !strings.Contains(text, "Team dispatch assignment for review_team.") || !strings.Contains(text, want) {
			t.Fatalf("team dispatch message for %s = %q", taskID, text)
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

func TestTeamScheduleAssignsObjectiveToMembers(t *testing.T) {
	ctx, _ := taskContext(t)
	executor := taskExecutor(t)
	for _, id := range []string{"agent/schedule-one", "agent/schedule-two"} {
		if _, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_team_schedule_" + strings.ReplaceAll(id, "/", "_")),
			Name:  "Task",
			Input: json.RawMessage(`{"id":"` + id + `","description":"Team task","prompt":"Work with the scheduled team","subagent_type":"general-purpose"}`),
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_schedule_create",
		Name:  "TeamCreate",
		Input: json.RawMessage(`{"name":"schedule/team","description":"Scheduled team","members":["agent/schedule-one","agent/schedule-two"]}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	const objective = "Prepare the release checklist and verify docs."
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_schedule_request",
		Name:  "TeamSchedule",
		Input: json.RawMessage(`{"team_id":"schedule/team","goal":"` + objective + `"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["type"] != "team_schedule" || result.StructuredContent["assignment_count"] != 2 || result.StructuredContent["objective"] != objective {
		t.Fatalf("team schedule structured content = %#v", result.StructuredContent)
	}
	assignments, ok := result.StructuredContent["assignments"].([]map[string]any)
	if !ok || len(assignments) != 2 || assignments[0]["task_id"] != "agent_schedule-one" || assignments[1]["task_id"] != "agent_schedule-two" {
		t.Fatalf("team schedule assignments = %#v", result.StructuredContent["assignments"])
	}
	for _, taskID := range []string{"agent/schedule-one", "agent/schedule-two"} {
		resume, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_team_schedule_resume_" + strings.ReplaceAll(taskID, "/", "_")),
			Name:  "ResumeTask",
			Input: json.RawMessage(`{"task_id":"` + taskID + `","limit":3}`),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		messages, ok := resume.StructuredContent["resume_messages"].([]map[string]any)
		if !ok || len(messages) != 3 {
			t.Fatalf("team schedule resume messages for %s = %#v", taskID, resume.StructuredContent["resume_messages"])
		}
		text, _ := messages[2]["text"].(string)
		if !strings.Contains(text, "Team scheduled assignment for schedule_team.") || !strings.Contains(text, "Objective:\n"+objective) || !strings.Contains(text, "Assigned member: "+strings.ReplaceAll(taskID, "/", "_")) {
			t.Fatalf("team schedule message for %s = %q", taskID, text)
		}
	}
}

func TestTeamAutoScheduleBriefsCoordinatorAndAssignsMembers(t *testing.T) {
	ctx, _ := taskContext(t)
	executor := taskExecutor(t)
	for _, id := range []string{"agent/auto-lead", "agent/auto-one", "agent/auto-two"} {
		if _, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_team_auto_task_" + strings.ReplaceAll(id, "/", "_")),
			Name:  "Task",
			Input: json.RawMessage(`{"id":"` + id + `","description":"Auto team task","prompt":"Work with the auto team","subagent_type":"general-purpose"}`),
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_team_auto_create",
		Name: "TeamCreate",
		Input: json.RawMessage(`{
			"team_id":"auto/team",
			"description":"Auto scheduled team",
			"coordinator_task_id":"agent/auto-lead",
			"task_ids":["agent/auto-one","agent/auto-two"]
		}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	const objective = "Plan implementation ownership and start verification."
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_auto_schedule_request",
		Name:  "TeamAutoSchedule",
		Input: json.RawMessage(`{"team_id":"auto/team","instruction":"` + objective + `"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["type"] != "team_auto_schedule" || result.StructuredContent["assignment_count"] != 2 || result.StructuredContent["objective"] != objective {
		t.Fatalf("team auto schedule structured content = %#v", result.StructuredContent)
	}
	coordinator, ok := result.StructuredContent["coordinator"].(map[string]any)
	if !ok || coordinator["task_id"] != "agent_auto-lead" || coordinator["status"] != session.SidechainStatusRunning {
		t.Fatalf("team auto schedule coordinator = %#v", result.StructuredContent)
	}
	coordinatorMessage, ok := result.StructuredContent["coordinator_message"].(map[string]any)
	if !ok || coordinatorMessage["task_id"] != "agent_auto-lead" {
		t.Fatalf("team auto schedule coordinator message = %#v", result.StructuredContent["coordinator_message"])
	}
	assignments, ok := result.StructuredContent["assignments"].([]map[string]any)
	if !ok || len(assignments) != 2 || assignments[0]["task_id"] != "agent_auto-one" || assignments[1]["task_id"] != "agent_auto-two" {
		t.Fatalf("team auto schedule assignments = %#v", result.StructuredContent["assignments"])
	}
	coordinatorResume, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_auto_coordinator_resume",
		Name:  "ResumeTask",
		Input: json.RawMessage(`{"task_id":"agent/auto-lead","limit":3}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinatorMessages, ok := coordinatorResume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(coordinatorMessages) != 3 {
		t.Fatalf("team auto coordinator resume messages = %#v", coordinatorResume.StructuredContent["resume_messages"])
	}
	coordinatorText, _ := coordinatorMessages[2]["text"].(string)
	if !strings.Contains(coordinatorText, "Team coordination request for auto_team.") || !strings.Contains(coordinatorText, "- agent_auto-one: running") || !strings.Contains(coordinatorText, "Objective:\n"+objective) {
		t.Fatalf("team auto coordinator briefing = %q", coordinatorText)
	}
	for _, taskID := range []string{"agent/auto-one", "agent/auto-two"} {
		resume, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_team_auto_resume_" + strings.ReplaceAll(taskID, "/", "_")),
			Name:  "ResumeTask",
			Input: json.RawMessage(`{"task_id":"` + taskID + `","limit":3}`),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		messages, ok := resume.StructuredContent["resume_messages"].([]map[string]any)
		if !ok || len(messages) != 3 {
			t.Fatalf("team auto member resume messages for %s = %#v", taskID, resume.StructuredContent["resume_messages"])
		}
		text, _ := messages[2]["text"].(string)
		if !strings.Contains(text, "Team scheduled assignment for auto_team.") || !strings.Contains(text, "Objective:\n"+objective) || !strings.Contains(text, "Assigned member: "+strings.ReplaceAll(taskID, "/", "_")) {
			t.Fatalf("team auto member message for %s = %q", taskID, text)
		}
	}
}

func TestTeamAutoScheduleAppliesCoordinatorPlanAssignments(t *testing.T) {
	ctx, _ := taskContext(t)
	executor := taskExecutor(t)
	for _, id := range []string{"agent/auto-plan-lead", "agent/auto-plan-one", "agent/auto-plan-two"} {
		if _, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_team_auto_plan_task_" + strings.ReplaceAll(id, "/", "_")),
			Name:  "Task",
			Input: json.RawMessage(`{"id":"` + id + `","description":"Auto plan team task","prompt":"Work with the planned team","subagent_type":"general-purpose"}`),
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_team_auto_plan_create",
		Name: "TeamCreate",
		Input: json.RawMessage(`{
			"team_id":"auto/plan",
			"description":"Auto planned team",
			"coordinator_task_id":"agent/auto-plan-lead",
			"task_ids":["agent/auto-plan-one","agent/auto-plan-two"]
		}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	const objective = "Split implementation and verification by ownership."
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_team_auto_plan_schedule_request",
		Name: "TeamAutoSchedule",
		Input: json.RawMessage(`{
			"team_id":"auto/plan",
			"goal":"` + objective + `",
			"plan":[
				{"task_id":"agent/auto-plan-one","message":"Implement the scheduler plan path."},
				{"task_id":"agent/auto-plan-two","message":"Verify transcript and structured output coverage."}
			]
		}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["type"] != "team_auto_schedule" || result.StructuredContent["assignment_count"] != 2 || result.StructuredContent["schedule_source"] != teamAutoScheduleSourceCoordinatorPlan {
		t.Fatalf("team auto plan structured content = %#v", result.StructuredContent)
	}
	assignments, ok := result.StructuredContent["assignments"].([]map[string]any)
	if !ok || len(assignments) != 2 || assignments[0]["task_id"] != "agent_auto-plan-one" || assignments[1]["task_id"] != "agent_auto-plan-two" {
		t.Fatalf("team auto plan assignments = %#v", result.StructuredContent["assignments"])
	}
	if assignments[0]["schedule_source"] != teamAutoScheduleSourceCoordinatorPlan || assignments[1]["schedule_source"] != teamAutoScheduleSourceCoordinatorPlan {
		t.Fatalf("team auto plan assignment source = %#v", assignments)
	}
	coordinatorResume, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_auto_plan_coordinator_resume",
		Name:  "ResumeTask",
		Input: json.RawMessage(`{"task_id":"agent/auto-plan-lead","limit":3}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinatorMessages, ok := coordinatorResume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(coordinatorMessages) != 3 {
		t.Fatalf("team auto plan coordinator resume messages = %#v", coordinatorResume.StructuredContent["resume_messages"])
	}
	coordinatorText, _ := coordinatorMessages[2]["text"].(string)
	if !strings.Contains(coordinatorText, "Team coordination request for auto_plan.") || !strings.Contains(coordinatorText, "Planned assignments:") || !strings.Contains(coordinatorText, "agent_auto-plan-one: Implement the scheduler plan path.") {
		t.Fatalf("team auto plan coordinator briefing = %q", coordinatorText)
	}
	memberChecks := map[string]string{
		"agent/auto-plan-one": "Implement the scheduler plan path.",
		"agent/auto-plan-two": "Verify transcript and structured output coverage.",
	}
	for taskID, wantAssignment := range memberChecks {
		resume, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_team_auto_plan_resume_" + strings.ReplaceAll(taskID, "/", "_")),
			Name:  "ResumeTask",
			Input: json.RawMessage(`{"task_id":"` + taskID + `","limit":3}`),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		messages, ok := resume.StructuredContent["resume_messages"].([]map[string]any)
		if !ok || len(messages) != 3 {
			t.Fatalf("team auto plan member resume messages for %s = %#v", taskID, resume.StructuredContent["resume_messages"])
		}
		text, _ := messages[2]["text"].(string)
		if !strings.Contains(text, "Team planned assignment for auto_plan.") || !strings.Contains(text, "Objective:\n"+objective) || !strings.Contains(text, "Assignment:\n"+wantAssignment) {
			t.Fatalf("team auto plan member message for %s = %q", taskID, text)
		}
		if strings.Contains(text, "Team scheduled assignment") {
			t.Fatalf("team auto plan member message used deterministic schedule for %s = %q", taskID, text)
		}
	}
}

func TestTeamAutoScheduleAcceptsWrappedCoordinatorPlan(t *testing.T) {
	ctx, _ := taskContext(t)
	executor := taskExecutor(t)
	for _, id := range []string{"agent/auto-wrap-lead", "agent/auto-wrap-one", "agent/auto-wrap-two"} {
		if _, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_team_auto_wrap_task_" + strings.ReplaceAll(id, "/", "_")),
			Name:  "Task",
			Input: json.RawMessage(`{"id":"` + id + `","description":"Wrapped auto plan task","prompt":"Work with the wrapped planned team","subagent_type":"general-purpose"}`),
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_team_auto_wrap_create",
		Name: "TeamCreate",
		Input: json.RawMessage(`{
			"team_id":"auto/wrapped",
			"description":"Wrapped auto planned team",
			"coordinator_task_id":"agent/auto-wrap-lead",
			"task_ids":["agent/auto-wrap-one","agent/auto-wrap-two"]
		}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	const objective = "Coordinate a wrapped model plan."
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_team_auto_wrap_schedule_request",
		Name: "TeamAutoSchedule",
		Input: json.RawMessage(`{
			"team_id":"auto/wrapped",
			"coordinator_plan":{
				"objective":"` + objective + `",
				"assignments":[
					{"taskId":"agent/auto-wrap-one","assignment":"Own wrapped input parser."},
					{"member":"agent/auto-wrap-two","content":"Verify wrapped input aliases."}
				]
			}
		}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["type"] != "team_auto_schedule" || result.StructuredContent["objective"] != objective || result.StructuredContent["schedule_source"] != teamAutoScheduleSourceCoordinatorPlan {
		t.Fatalf("team auto wrapped plan structured content = %#v", result.StructuredContent)
	}
	assignments, ok := result.StructuredContent["assignments"].([]map[string]any)
	if !ok || len(assignments) != 2 || assignments[0]["task_id"] != "agent_auto-wrap-one" || assignments[1]["task_id"] != "agent_auto-wrap-two" {
		t.Fatalf("team auto wrapped plan assignments = %#v", result.StructuredContent["assignments"])
	}
	coordinatorResume, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_team_auto_wrap_coordinator_resume",
		Name:  "ResumeTask",
		Input: json.RawMessage(`{"task_id":"agent/auto-wrap-lead","limit":3}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinatorMessages, ok := coordinatorResume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(coordinatorMessages) != 3 {
		t.Fatalf("team auto wrapped coordinator resume messages = %#v", coordinatorResume.StructuredContent["resume_messages"])
	}
	coordinatorText, _ := coordinatorMessages[2]["text"].(string)
	if !strings.Contains(coordinatorText, "Objective:\n"+objective) || !strings.Contains(coordinatorText, "agent_auto-wrap-two: Verify wrapped input aliases.") {
		t.Fatalf("team auto wrapped coordinator briefing = %q", coordinatorText)
	}
	memberChecks := map[string]string{
		"agent/auto-wrap-one": "Own wrapped input parser.",
		"agent/auto-wrap-two": "Verify wrapped input aliases.",
	}
	for taskID, wantAssignment := range memberChecks {
		resume, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_team_auto_wrap_resume_" + strings.ReplaceAll(taskID, "/", "_")),
			Name:  "ResumeTask",
			Input: json.RawMessage(`{"task_id":"` + taskID + `","limit":3}`),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		messages, ok := resume.StructuredContent["resume_messages"].([]map[string]any)
		if !ok || len(messages) != 3 {
			t.Fatalf("team auto wrapped member resume messages for %s = %#v", taskID, resume.StructuredContent["resume_messages"])
		}
		text, _ := messages[2]["text"].(string)
		if !strings.Contains(text, "Team planned assignment for auto_wrapped.") || !strings.Contains(text, "Assignment:\n"+wantAssignment) {
			t.Fatalf("team auto wrapped member message for %s = %q", taskID, text)
		}
	}
}

func TestSleepToolWaitsForBoundedDuration(t *testing.T) {
	ctx, _ := taskContext(t)
	executor := taskExecutor(t)
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_sleep",
		Name:  "Sleep",
		Input: json.RawMessage(`{"ms":"1"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["type"] != "sleep" || result.StructuredContent["duration_ms"] != int64(1) || result.StructuredContent["cancelled"] != false {
		t.Fatalf("sleep structured content = %#v", result.StructuredContent)
	}
	if !strings.Contains(result.Content.(string), "Slept for 1ms.") {
		t.Fatalf("sleep content = %#v", result.Content)
	}
}

func TestBriefToolCreatesStructuredHandoff(t *testing.T) {
	ctx, _ := taskContext(t)
	executor := taskExecutor(t)
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_brief",
		Name: "Brief",
		Input: json.RawMessage(`{
			"topic":"Release handoff",
			"state":"ready",
			"body":"CI is green and the branch is pushed.",
			"detail":"Latest commit passed Go CI.",
			"actions":["Watch deployment","Notify team"],
			"risk":"Deployment window is short."
		}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["type"] != "brief" || result.StructuredContent["title"] != "Release handoff" || result.StructuredContent["status"] != "ready" || result.StructuredContent["summary"] != "CI is green and the branch is pushed." {
		t.Fatalf("brief structured content = %#v", result.StructuredContent)
	}
	details, ok := result.StructuredContent["details"].([]string)
	if !ok || len(details) != 1 || details[0] != "Latest commit passed Go CI." {
		t.Fatalf("brief details = %#v", result.StructuredContent["details"])
	}
	nextSteps, ok := result.StructuredContent["next_steps"].([]string)
	if !ok || len(nextSteps) != 2 || nextSteps[1] != "Notify team" {
		t.Fatalf("brief next steps = %#v", result.StructuredContent["next_steps"])
	}
	risks, ok := result.StructuredContent["risks"].([]string)
	if !ok || len(risks) != 1 || risks[0] != "Deployment window is short." {
		t.Fatalf("brief risks = %#v", result.StructuredContent["risks"])
	}
	content, _ := result.Content.(string)
	for _, want := range []string{"Brief: Release handoff", "Status: ready", "Summary: CI is green", "Details:\n- Latest commit passed Go CI.", "Next steps:\n- Watch deployment\n- Notify team", "Risks:\n- Deployment window is short."} {
		if !strings.Contains(content, want) {
			t.Fatalf("brief content missing %q: %q", want, content)
		}
	}
}

func TestScheduleCronPersistsManifest(t *testing.T) {
	ctx, transcriptPath := taskContext(t)
	executor := taskExecutor(t)
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_schedule_member",
		Name:  "Task",
		Input: json.RawMessage(`{"id":"agent/schedule-member","description":"Scheduled task","prompt":"Handle scheduled work","subagent_type":"general-purpose"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_schedule_team",
		Name:  "TeamCreate",
		Input: json.RawMessage(`{"name":"ops/team","description":"Ops team","members":["agent/schedule-member"]}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	created, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_schedule_create",
		Name: "ScheduleCron",
		Input: json.RawMessage(`{
			"name":"daily/check",
			"description":"Daily ops check",
			"cron":"0 9 * * MON-FRI",
			"message":"Check the deployment status.",
			"team":"ops/team",
			"target":"all"
		}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.StructuredContent["schedule_id"] != "daily_check" || created.StructuredContent["cron"] != "0 9 * * MON-FRI" || created.StructuredContent["team_id"] != "ops_team" || created.StructuredContent["target"] != "all" || created.StructuredContent["enabled"] != true || created.StructuredContent["schedule_count"] != 1 {
		t.Fatalf("schedule create structured content = %#v", created.StructuredContent)
	}
	manifest, err := session.LoadScheduleManifest(transcriptPath, ctx.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Schedules) != 1 || manifest.Schedules[0].ID != "daily_check" || manifest.Schedules[0].Message != "Check the deployment status." {
		t.Fatalf("schedule manifest = %#v", manifest)
	}
	listed, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_schedule_list",
		Name:  "ScheduleCron",
		Input: json.RawMessage(`{"action":"list"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	schedules, ok := listed.StructuredContent["schedules"].([]map[string]any)
	if !ok || len(schedules) != 1 || schedules[0]["schedule_id"] != "daily_check" {
		t.Fatalf("schedule list = %#v", listed.StructuredContent)
	}
	triggered, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_schedule_trigger",
		Name:  "ScheduleCron",
		Input: json.RawMessage(`{"action":"trigger","schedule_id":"daily/check"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if triggered.StructuredContent["action"] != "trigger" || triggered.StructuredContent["sent_count"] != 1 {
		t.Fatalf("schedule trigger structured content = %#v", triggered.StructuredContent)
	}
	if triggered.StructuredContent["last_run_status"] != "success" || triggered.StructuredContent["run_count"] != 1 {
		t.Fatalf("schedule trigger run state = %#v", triggered.StructuredContent)
	}
	resume, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_schedule_resume",
		Name:  "ResumeTask",
		Input: json.RawMessage(`{"task_id":"agent/schedule-member","limit":3}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	messages, ok := resume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("schedule trigger resume messages = %#v", resume.StructuredContent["resume_messages"])
	}
	text, _ := messages[2]["text"].(string)
	if !strings.Contains(text, "Scheduled cron trigger received.") || !strings.Contains(text, "Schedule: daily_check") || !strings.Contains(text, "Cron: 0 9 * * MON-FRI") || !strings.Contains(text, "Check the deployment status.") {
		t.Fatalf("schedule trigger message = %q", text)
	}
	due, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_schedule_due",
		Name:  "ScheduleCron",
		Input: json.RawMessage(`{"action":"run_due","now":"2026-06-22T09:00:00Z"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if due.StructuredContent["action"] != "run_due" || due.StructuredContent["due_count"] != 1 || due.StructuredContent["triggered_count"] != 1 || due.StructuredContent["error_count"] != 0 {
		t.Fatalf("schedule run due structured content = %#v", due.StructuredContent)
	}
	duplicateDue, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_schedule_due_duplicate",
		Name:  "ScheduleCron",
		Input: json.RawMessage(`{"action":"run_due","now":"2026-06-22T09:00:30Z"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if duplicateDue.StructuredContent["due_count"] != 0 || duplicateDue.StructuredContent["triggered_count"] != 0 {
		t.Fatalf("schedule duplicate run due structured content = %#v", duplicateDue.StructuredContent)
	}
	deleted, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_schedule_delete",
		Name:  "ScheduleCron",
		Input: json.RawMessage(`{"action":"delete","id":"daily/check"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.StructuredContent["deleted"] != true || deleted.StructuredContent["schedule_id"] != "daily_check" || deleted.StructuredContent["schedule_count"] != 0 {
		t.Fatalf("schedule delete structured content = %#v", deleted.StructuredContent)
	}
	manifest, err = session.LoadScheduleManifest(transcriptPath, ctx.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Schedules) != 0 {
		t.Fatalf("schedule manifest after delete = %#v", manifest)
	}
}

func TestRemoteTriggerSendsEventToCoordinatorByDefault(t *testing.T) {
	ctx, transcriptPath := taskContext(t)
	executor := taskExecutor(t)
	for _, id := range []string{"agent/remote-lead", "agent/remote-member"} {
		if _, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_remote_" + strings.ReplaceAll(id, "/", "_")),
			Name:  "Task",
			Input: json.RawMessage(`{"id":"` + id + `","description":"Remote team","prompt":"Handle remote triggers","subagent_type":"general-purpose"}`),
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_remote_team",
		Name:  "TeamCreate",
		Input: json.RawMessage(`{"name":"remote/team","coordinator":"agent/remote-lead","members":["agent/remote-member"]}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	triggered, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_remote_trigger",
		Name: "RemoteTrigger",
		Input: json.RawMessage(`{
			"team":"remote/team",
			"event_id":"delivery-123",
			"source":"github",
			"event_type":"workflow_failed",
			"payload":"Investigate the failed CI run."
		}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if triggered.StructuredContent["type"] != "remote_trigger" || triggered.StructuredContent["target"] != "coordinator" || triggered.StructuredContent["event_id"] != "delivery-123" || triggered.StructuredContent["sent_count"] != 1 || triggered.StructuredContent["source"] != "github" || triggered.StructuredContent["event"] != "workflow_failed" || triggered.StructuredContent["duplicate"] != false {
		t.Fatalf("remote trigger structured content = %#v", triggered.StructuredContent)
	}
	manifest, err := session.LoadRemoteTriggerManifest(transcriptPath, ctx.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Receipts) != 1 || manifest.Receipts[0].EventID != "delivery-123" || manifest.Receipts[0].SentCount != 1 {
		t.Fatalf("remote trigger manifest = %#v", manifest)
	}
	resume, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_remote_resume_lead",
		Name:  "ResumeTask",
		Input: json.RawMessage(`{"task_id":"agent/remote-lead","limit":3}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	messages, ok := resume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("remote trigger resume messages = %#v", resume.StructuredContent["resume_messages"])
	}
	text, _ := messages[2]["text"].(string)
	if !strings.Contains(text, "Remote trigger received.") || !strings.Contains(text, "Source: github") || !strings.Contains(text, "Event: workflow_failed") || !strings.Contains(text, "Event ID: delivery-123") || !strings.Contains(text, "Investigate the failed CI run.") {
		t.Fatalf("remote trigger message = %q", text)
	}
	duplicate, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_remote_trigger_duplicate",
		Name: "RemoteTrigger",
		Input: json.RawMessage(`{
			"team":"remote/team",
			"event_id":"delivery-123",
			"source":"github",
			"event_type":"workflow_failed",
			"payload":"Investigate the failed CI run."
		}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.StructuredContent["duplicate"] != true || duplicate.StructuredContent["sent_count"] != 0 || duplicate.StructuredContent["duplicate_count"] != 1 {
		t.Fatalf("remote trigger duplicate structured content = %#v", duplicate.StructuredContent)
	}
	resume, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_remote_resume_lead_duplicate",
		Name:  "ResumeTask",
		Input: json.RawMessage(`{"task_id":"agent/remote-lead","limit":4}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	messages, ok = resume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("remote trigger duplicate resume messages = %#v", resume.StructuredContent["resume_messages"])
	}
	memberResume, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_remote_resume_member",
		Name:  "ResumeTask",
		Input: json.RawMessage(`{"task_id":"agent/remote-member","limit":3}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	memberMessages, ok := memberResume.StructuredContent["resume_messages"].([]map[string]any)
	if !ok || len(memberMessages) != 2 {
		t.Fatalf("remote trigger member messages = %#v", memberResume.StructuredContent["resume_messages"])
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
	if err == nil || !strings.Contains(err.Error(), "input.subagent_type must be one of general-purpose, demo:reviewer") {
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
		{name: "missing team dispatch id", tool: "TeamDispatch", input: `{"assignments":[{"task_id":"agent/member","message":"hello"}]}`, want: "team_id is required"},
		{name: "missing team dispatch assignments", tool: "TeamDispatch", input: `{"team_id":"missing"}`, want: "input.assignments is required"},
		{name: "unknown team dispatch field", tool: "TeamDispatch", input: `{"team_id":"missing","assignments":[],"extra":true}`, want: "input.extra is not allowed"},
		{name: "missing team dispatch team", tool: "TeamDispatch", input: `{"team_id":"missing","assignments":[{"task_id":"agent/member","message":"hello"}]}`, want: "team not found: missing"},
		{name: "missing team schedule id", tool: "TeamSchedule", input: `{"objective":"ship"}`, want: "team_id is required"},
		{name: "missing team schedule objective", tool: "TeamSchedule", input: `{"team_id":"missing"}`, want: "input.objective is required"},
		{name: "unknown team schedule field", tool: "TeamSchedule", input: `{"team_id":"missing","objective":"ship","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing team schedule team", tool: "TeamSchedule", input: `{"team_id":"missing","objective":"ship"}`, want: "team not found: missing"},
		{name: "missing team auto schedule id", tool: "TeamAutoSchedule", input: `{"objective":"ship"}`, want: "team_id is required"},
		{name: "missing team auto schedule objective", tool: "TeamAutoSchedule", input: `{"team_id":"missing"}`, want: "input.objective is required"},
		{name: "unknown team auto schedule field", tool: "TeamAutoSchedule", input: `{"team_id":"missing","objective":"ship","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing team auto schedule team", tool: "TeamAutoSchedule", input: `{"team_id":"missing","objective":"ship"}`, want: "team not found: missing"},
		{name: "missing team coordinate id", tool: "TeamCoordinate", input: `{"message":"hello"}`, want: "team_id is required"},
		{name: "missing team coordinate message", tool: "TeamCoordinate", input: `{"team_id":"missing"}`, want: "message is required"},
		{name: "unknown team coordinate field", tool: "TeamCoordinate", input: `{"team_id":"missing","message":"hello","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing team coordinate team", tool: "TeamCoordinate", input: `{"team_id":"missing","message":"hello"}`, want: "team not found: missing"},
		{name: "bad resume limit", tool: "ResumeTask", input: `{"task_id":"missing","limit":0}`, want: "limit must be positive"},
		{name: "missing resume task", tool: "ResumeTask", input: `{"id":"missing"}`, want: "task not found: missing"},
		{name: "missing sleep duration", tool: "Sleep", input: `{}`, want: "duration is required"},
		{name: "unknown sleep field", tool: "Sleep", input: `{"until":"later"}`, want: "input.until is not allowed"},
		{name: "bad sleep duration", tool: "Sleep", input: `{"duration":"soon"}`, want: "duration must be a valid Go duration"},
		{name: "conflicting sleep duration", tool: "Sleep", input: `{"duration_ms":1,"seconds":1}`, want: "provide exactly one of duration_ms, seconds, or duration"},
		{name: "long sleep duration", tool: "Sleep", input: `{"duration_ms":60001}`, want: "duration must be <= 60000ms"},
		{name: "missing brief summary", tool: "Brief", input: `{}`, want: "summary is required"},
		{name: "unknown brief field", tool: "Brief", input: `{"summary":"hello","extra":true}`, want: "input.extra is not allowed"},
		{name: "bad brief list", tool: "Brief", input: `{"summary":"hello","details":12}`, want: "details must be a string or string array"},
		{name: "bad schedule action", tool: "ScheduleCron", input: `{"action":"invalid"}`, want: "action must be one of create, list, delete, trigger, run_due"},
		{name: "bad schedule now", tool: "ScheduleCron", input: `{"action":"run_due","now":"soon"}`, want: "now must be RFC3339"},
		{name: "missing schedule cron", tool: "ScheduleCron", input: `{"message":"hello"}`, want: "cron is required"},
		{name: "bad schedule cron", tool: "ScheduleCron", input: `{"cron":"bad","message":"hello"}`, want: "cron must be a supported 5-field expression"},
		{name: "missing schedule message", tool: "ScheduleCron", input: `{"cron":"@daily"}`, want: "message is required"},
		{name: "bad schedule target", tool: "ScheduleCron", input: `{"cron":"@daily","message":"hello","target":"leaders"}`, want: "target must be one of members, coordinator, all"},
		{name: "missing schedule team", tool: "ScheduleCron", input: `{"cron":"@daily","message":"hello","team_id":"missing"}`, want: "team not found: missing"},
		{name: "missing schedule delete id", tool: "ScheduleCron", input: `{"action":"delete"}`, want: "schedule_id is required"},
		{name: "missing schedule delete schedule", tool: "ScheduleCron", input: `{"action":"delete","schedule_id":"missing"}`, want: "schedule not found: missing"},
		{name: "missing remote trigger team", tool: "RemoteTrigger", input: `{"message":"hello"}`, want: "team_id is required"},
		{name: "missing remote trigger message", tool: "RemoteTrigger", input: `{"team_id":"missing"}`, want: "message is required"},
		{name: "unknown remote trigger field", tool: "RemoteTrigger", input: `{"team_id":"missing","message":"hello","extra":true}`, want: "input.extra is not allowed"},
		{name: "bad remote trigger target", tool: "RemoteTrigger", input: `{"team_id":"missing","message":"hello","target":"leaders"}`, want: "target must be one of members, coordinator, all"},
		{name: "missing remote trigger team state", tool: "RemoteTrigger", input: `{"team_id":"missing","message":"hello"}`, want: "team not found: missing"},
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

// TestTaskToolModelOverrideWritesToSidechainMetadata verifies TOOL-TASK-04 / ORCH-05:
// when input.Model is set, the effective model stored in sidechain metadata
// is the input model, not the agentfile model.
func TestTaskToolModelOverrideWritesToSidechainMetadata(t *testing.T) {
	// Provide an agentfile with model=sonnet; input overrides with haiku.
	ctx, transcriptPath := taskContextWithAgents(t, []tool.AgentInfo{{
		Name:  "general-purpose",
		Model: "sonnet",
	}})
	executor := taskExecutor(t)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_model_override",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"test","prompt":"do stuff","subagent_type":"general-purpose","model":"haiku"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %#v", result)
	}
	// Structured content must reflect the override.
	if result.StructuredContent["agent_model"] != "haiku" {
		t.Fatalf("agent_model = %v, want haiku", result.StructuredContent["agent_model"])
	}
	if result.StructuredContent["model_override"] != "haiku" {
		t.Fatalf("model_override = %v, want haiku", result.StructuredContent["model_override"])
	}
	// Sidechain metadata must carry the override model so the runner uses it.
	states, err := session.ListSidechainStates(transcriptPath, ctx.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 sidechain, got %d", len(states))
	}
	if states[0].Metadata.AgentModel != "haiku" {
		t.Fatalf("sidechain AgentModel = %q, want haiku", states[0].Metadata.AgentModel)
	}
}

// TestTaskToolRunBackgroundSetsStructuredFlag verifies TOOL-TASK-02 / ORCH-03:
// when run_in_background=true the structured result carries run_in_background=true
// so the conversation layer can route it to the AgentRegistry.
func TestTaskToolRunBackgroundSetsStructuredFlag(t *testing.T) {
	ctx, _ := taskContext(t)
	executor := taskExecutor(t)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_bg_task",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"bg test","prompt":"run in bg","subagent_type":"general-purpose","run_in_background":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	if result.StructuredContent["run_in_background"] != true {
		t.Fatalf("run_in_background not set in structured content: %#v", result.StructuredContent)
	}
	if result.StructuredContent["type"] != "task" {
		t.Fatalf("type = %v, want task", result.StructuredContent["type"])
	}
}

// TestBackgroundTaskRunInBackgroundFlag verifies that the structured result from
// callTask carries run_in_background=true when the input requests it, so the
// conversation layer (maybeRunTaskSubagent) can detect and route to AgentRegistry.
func TestBackgroundTaskRunInBackgroundFlag(t *testing.T) {
	ctx, _ := taskContext(t)
	executor := taskExecutor(t)

	// run_in_background without run — should still record the flag.
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_bg_flag",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"flag test","prompt":"check flag","subagent_type":"general-purpose","run_in_background":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	if v, _ := result.StructuredContent["run_in_background"].(bool); !v {
		t.Fatalf("expected run_in_background=true in structured content, got %#v", result.StructuredContent["run_in_background"])
	}
}

// TestAgentRegistryBackgroundDispatch verifies the AgentRegistry dispatch
// path used by background task launch (ORCH-03): the registry goroutine
// runs, stores the outcome, and Harvest returns it.
func TestAgentRegistryBackgroundDispatch(t *testing.T) {
	reg := orchestration.NewAgentRegistry()
	done := make(chan struct{})
	reg.StartBackground("bg-1", func(_ context.Context) orchestration.Outcome {
		defer close(done)
		return orchestration.Outcome{Summary: "background done"}
	})
	// Should be running immediately.
	snap := reg.Snapshot()
	if len(snap) != 1 || snap[0].ID != "bg-1" {
		t.Fatalf("snapshot after start = %#v", snap)
	}
	// Wait for the goroutine.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background goroutine did not complete")
	}
	out, ok := reg.Harvest("bg-1")
	if !ok {
		t.Fatal("Harvest returned false, expected done agent")
	}
	if out.Summary != "background done" {
		t.Fatalf("summary = %q, want %q", out.Summary, "background done")
	}
}

// TestAgentfileIsolationWorktreeRequestsWorktree verifies ORCH-12: when an
// AgentInfo has Isolation=="worktree", taskInputRequestsWorktree returns true
// even when the caller did not explicitly set the worktree input field.
func TestAgentfileIsolationWorktreeRequestsWorktree(t *testing.T) {
	ctx, _ := taskContext(t)
	ctx.Metadata[tool.MetadataAvailableAgentsKey] = []tool.AgentInfo{
		{Name: "isolated-agent", Isolation: "worktree"},
	}
	input := taskInput{SubagentType: "isolated-agent"} // WorktreeSet=false
	if !taskInputRequestsWorktree(ctx, input) {
		t.Error("taskInputRequestsWorktree should return true when agent has Isolation=worktree")
	}
}

// TestAgentfileIsolationEmptyDoesNotForceWorktree verifies that an agent with
// no isolation field does not trigger automatic worktree creation.
func TestAgentfileIsolationEmptyDoesNotForceWorktree(t *testing.T) {
	ctx, _ := taskContext(t)
	ctx.Metadata[tool.MetadataAvailableAgentsKey] = []tool.AgentInfo{
		{Name: "plain-agent", Isolation: ""},
	}
	input := taskInput{SubagentType: "plain-agent"}
	// With no worktree settings in metadata, should be false.
	if taskInputRequestsWorktree(ctx, input) {
		t.Error("taskInputRequestsWorktree should return false when agent has no Isolation")
	}
}

// TestAgentfileBackgroundForcesSidechainRunBackground verifies ORCH-13:
// when an AgentInfo has Background==true the Task tool records the sidechain
// with run_in_background=true in the structured result regardless of input.
func TestAgentfileBackgroundForcesSidechainRunBackground(t *testing.T) {
	ctx, _ := taskContext(t)
	ctx.Metadata[tool.MetadataAvailableAgentsKey] = []tool.AgentInfo{
		{Name: "bg-agent", Background: true},
	}
	executor := taskExecutor(t)
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_bg_agent",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"bg","prompt":"run","subagent_type":"bg-agent"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	if v, _ := result.StructuredContent["run_in_background"].(bool); !v {
		t.Errorf("expected run_in_background=true in structured content (forced by agentfile background:true), got %#v", result.StructuredContent["run_in_background"])
	}
}

// TestAgentfileOmitClaudeMdPropagatedToSidechain verifies ORCH-35: when an
// AgentInfo has OmitClaudeMd==true the sidechain metadata records
// agentOmitClaudeMd=true so the sub-agent runner can strip CLAUDE.md.
func TestAgentfileOmitClaudeMdPropagatedToSidechain(t *testing.T) {
	ctx, transcriptPath := taskContext(t)
	ctx.Metadata[tool.MetadataAvailableAgentsKey] = []tool.AgentInfo{
		{Name: "lean-agent", OmitClaudeMd: true},
	}
	executor := taskExecutor(t)
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_lean",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"lean","prompt":"go","subagent_type":"lean-agent"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	taskID, _ := result.StructuredContent["sidechain_id"].(string)
	if taskID == "" {
		t.Fatal("sidechain_id missing from structured content")
	}
	// The sidechain metadata must carry agentOmitClaudeMd=true.
	// FindSidechainState takes the transcript path (MetadataSessionPathKey value).
	state, err := session.FindSidechainState(transcriptPath, ctx.SessionID, taskID)
	if err != nil {
		t.Fatalf("FindSidechainState: %v", err)
	}
	if !state.Metadata.AgentOmitClaudeMd {
		t.Error("sidechain metadata AgentOmitClaudeMd should be true when agentfile has omitClaudeMd:true")
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
