package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/bootstrap"
	"ccgo/internal/contracts"
	"ccgo/internal/messages"
	"ccgo/internal/session"
	"ccgo/internal/tool"
	bashtools "ccgo/internal/tools/bash"
	filetools "ccgo/internal/tools/file"
)

func TestRunPrintSendsPromptAndPrintsAssistantText(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q", got)
		}
		if got := r.Header.Get("user-agent"); got != "ccgo/"+version {
			t.Fatalf("user-agent = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_1",
			"type":"message",
			"role":"assistant",
			"model":"claude-haiku-4-5-20251001",
			"content":[{"type":"text","text":"hello from api"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":2}
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--model", "haiku", "--max-tokens", "17", "say hello"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "hello from api\n" {
		t.Fatalf("stdout = %q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if requestBody["model"] != "claude-haiku-4-5-20251001" {
		t.Fatalf("model = %#v", requestBody["model"])
	}
	if requestBody["max_tokens"] != float64(17) {
		t.Fatalf("max_tokens = %#v", requestBody["max_tokens"])
	}
	messages, ok := requestBody["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v", requestBody["messages"])
	}
	message, ok := messages[0].(map[string]any)
	if !ok || message["role"] != "user" {
		t.Fatalf("message = %#v", messages[0])
	}
	content, ok := message["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %#v", message["content"])
	}
	block, ok := content[0].(map[string]any)
	if !ok || block["type"] != "text" || block["text"] != "say hello" {
		t.Fatalf("block = %#v", content[0])
	}
	tools, ok := requestBody["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("missing builtin tools: %#v", requestBody["tools"])
	}
}

func TestRunHelpExitsSuccessfully(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage of claude:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunCWDFlagSetsScaffoldWorkingDirectory(t *testing.T) {
	project := t.TempDir()
	resolvedProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "cwd="+resolvedProject) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintReadsPromptFromStdinAndSettingsModel(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_2",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"stdin ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	claudeHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(claudeHome, "settings.json"), []byte(`{"model":"sonnet"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-p"}, strings.NewReader("from stdin\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "stdin ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	if requestBody["model"] != "claude-sonnet-4-6" {
		t.Fatalf("model = %#v", requestBody["model"])
	}
	messages := requestBody["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	if got := content[0].(map[string]any)["text"]; got != "from stdin" {
		t.Fatalf("prompt = %#v", got)
	}
}

func TestRunPrintCWDFlagLoadsProjectSettings(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_cwd",
			"type":"message",
			"role":"assistant",
			"model":"claude-haiku-4-5-20251001",
			"content":[{"type":"text","text":"cwd ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".claude", "settings.json"), []byte(`{"model":"haiku"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "--print", "cwd prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "cwd ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	if requestBody["model"] != "claude-haiku-4-5-20251001" {
		t.Fatalf("model = %#v", requestBody["model"])
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "cwd prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestHeadlessRunnerLoadsCLIProvidedMCPConfig(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "mcp.json"), []byte(`{
		"mcpServers": {
			"cli": {"command": "cli-server", "args": ["--stdio"]}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	state, err := bootstrap.New()
	if err != nil {
		t.Fatal(err)
	}
	state.SetCWD(project)
	runner, err := headlessRunner(context.Background(), state, cliOptions{MCPConfig: "mcp.json"})
	if err != nil {
		t.Fatal(err)
	}
	server := runner.MCP.LocalSettings.MCPServers["cli"]
	if server.Command != "cli-server" || len(server.Args) != 1 || server.Args[0] != "--stdio" {
		t.Fatalf("server = %#v", server)
	}
}

func TestRunPrintReadsJSONInputFormatPrompt(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_json_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"json input ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "json"}, strings.NewReader(`{"prompt":"json prompt"}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "json input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "json prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintReadsStreamJSONInputFormatUserEvent(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_stream_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"stream input ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	input := strings.Join([]string{
		`{"type":"system","status":"ready"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"first prompt"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"latest prompt"}]}}`,
	}, "\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "stream-json"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "stream input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "latest prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintAcceptsCamelCaseFlagAliases(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_camel",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"camel ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--print",
		"--inputFormat", "json",
		"--outputFormat", "json",
		"--maxTokens", "23",
		"--systemPrompt", "Base",
		"--appendSystemPrompt", "Extra",
		`{"prompt":"camel prompt"}`,
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["result"] != "camel ok" {
		t.Fatalf("payload = %#v", payload)
	}
	if requestBody["max_tokens"] != float64(23) {
		t.Fatalf("max_tokens = %#v", requestBody["max_tokens"])
	}
	if requestBody["system"] != "Base\n\nExtra" {
		t.Fatalf("system = %#v", requestBody["system"])
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "camel prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintSystemPromptFlags(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_system",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"system ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--system-prompt", "Base system", "--append-system-prompt", "Extra system", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if requestBody["system"] != "Base system\n\nExtra system" {
		t.Fatalf("system = %#v", requestBody["system"])
	}
}

func TestRunPrintMaxTurnsLimitsToolLoop(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_tool_round",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"tool_use","id":"toolu_read","name":"Read","input":{"file_path":"cmd/claude/main.go"}}],
			"stop_reason":"tool_use"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--max-turns", "1", "read once"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "maximum tool rounds exceeded: 1") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsNegativeMaxTurns(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--max-turns", "-1", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "invalid --max-turns -1") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsNegativeMaxTokens(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--max-tokens", "-1", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "invalid --max-tokens -1") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestPermissionDeciderFromCLIAllowDenyRules(t *testing.T) {
	allowed := parseToolRules(`Write, Bash(git status *)`)
	denied := parseToolRules(`Bash(rm *)`)
	decider, err := permissionDeciderFromSettings(nil, "dontAsk", allowed, denied, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.Context{Permissions: decider, WorkingDirectory: t.TempDir()}
	writeDecision, err := filetools.NewWriteTool().CheckPermissions(ctx, json.RawMessage(`{"file_path":"allowed.txt","content":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if writeDecision.Behavior != contracts.PermissionAllow {
		t.Fatalf("write decision = %#v", writeDecision)
	}
	bashDecision, err := bashtools.NewBashTool().CheckPermissions(ctx, json.RawMessage(`{"command":"rm -rf tmp"}`))
	if err != nil {
		t.Fatal(err)
	}
	if bashDecision.Behavior != contracts.PermissionDeny {
		t.Fatalf("bash decision = %#v", bashDecision)
	}
}

func TestPermissionDeciderFromCLIAdditionalDirectories(t *testing.T) {
	base := t.TempDir()
	extra1 := filepath.Join(base, "extra 1")
	extra2 := filepath.Join(base, "extra2")
	decider, err := permissionDeciderFromSettings(nil, "", nil, nil, parsePathList([]string{extra1 + "," + extra2, extra1}))
	if err != nil {
		t.Fatal(err)
	}
	engineDecider, ok := decider.(tool.EnginePermissionDecider)
	if !ok {
		t.Fatalf("decider = %T", decider)
	}
	dirs := engineDecider.Engine.Context().AdditionalWorkingDirectories
	if dirs[extra1] != contracts.PermissionSourceCLIArg {
		t.Fatalf("extra1 source = %q dirs=%#v", dirs[extra1], dirs)
	}
	if dirs[extra2] != contracts.PermissionSourceCLIArg {
		t.Fatalf("extra2 source = %q dirs=%#v", dirs[extra2], dirs)
	}
	if len(dirs) != 2 {
		t.Fatalf("dirs = %#v", dirs)
	}
}

func TestRunPrintJSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_json",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"json ok"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":3,"output_tokens":4}
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "json prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "success" || payload["result"] != "json ok" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["session_id"] == "" {
		t.Fatalf("missing session_id: %#v", payload)
	}
	if payload["stop_reason"] != "end_turn" || payload["model"] != "claude-sonnet-4-6" {
		t.Fatalf("metadata = %#v", payload)
	}
	usage, ok := payload["usage"].(map[string]any)
	if !ok || usage["input_tokens"] != float64(3) || usage["output_tokens"] != float64(4) {
		t.Fatalf("usage = %#v", payload["usage"])
	}
	message, ok := payload["message"].(map[string]any)
	if !ok || message["type"] != "assistant" {
		t.Fatalf("message = %#v", payload["message"])
	}
}

func TestRunPrintStreamJSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_stream",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"stream ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "stream-json", "stream prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %#v", lines)
	}
	var events []map[string]any
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid json line %q: %v", line, err)
		}
		events = append(events, event)
	}
	if events[0]["type"] != "system" || events[0]["subtype"] != "init" {
		t.Fatalf("init event = %#v", events[0])
	}
	if events[0]["session_id"] == "" || events[0]["cwd"] == "" {
		t.Fatalf("init metadata = %#v", events[0])
	}
	tools, ok := events[0]["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("init tools = %#v", events[0]["tools"])
	}
	if events[1]["type"] != "user_message" || events[2]["type"] != "assistant_message" || events[3]["type"] != "result" {
		t.Fatalf("events = %#v", events)
	}
	if events[3]["result"] != "stream ok" {
		t.Fatalf("result event = %#v", events[3])
	}
}

func TestRunPrintStreamJSONIncludesRawStreamingEvents(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream_delta\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"content\":[]}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"delta ok\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--stream", "--output-format", "stream-json", "stream prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if requestBody["stream"] != true {
		t.Fatalf("stream flag = %#v", requestBody["stream"])
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var sawDelta bool
	var final map[string]any
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid json line %q: %v", line, err)
		}
		if event["type"] == "stream_event" {
			streamEvent, ok := event["stream_event"].(map[string]any)
			if !ok {
				t.Fatalf("stream event = %#v", event)
			}
			if streamEvent["type"] == "content_block_delta" {
				delta := streamEvent["delta"].(map[string]any)
				if delta["text"] == "delta ok" {
					sawDelta = true
				}
			}
		}
		if event["type"] == "result" {
			final = event
		}
	}
	if !sawDelta {
		t.Fatalf("missing content_block_delta in %q", stdout.String())
	}
	if final == nil || final["result"] != "delta ok" {
		t.Fatalf("final = %#v stdout=%q", final, stdout.String())
	}
}

func TestRunPrintResumeLoadsTranscriptHistory(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_resume",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"resume ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	claudeHome := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sessionID := contracts.ID("resume-session")
	transcriptPath := session.TranscriptPath(cwd, sessionID)
	writeTestTranscript(t, transcriptPath, sessionID, "old question", "old answer")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--resume", string(sessionID), "new question"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if stdout.String() != "resume ok\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "old question" {
		t.Fatalf("old user = %q", got)
	}
	if got := messageTextAt(t, requestMessages, 1); got != "old answer" {
		t.Fatalf("old assistant = %q", got)
	}
	if got := messageTextAt(t, requestMessages, 2); got != "new question" {
		t.Fatalf("new user = %q", got)
	}
	entries, err := session.Load(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestRunPrintContinueUsesMostRecentSession(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_continue",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"continue ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	claudeHome := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sessionID := contracts.ID("continue-session")
	writeTestTranscript(t, session.TranscriptPath(cwd, sessionID), sessionID, "continue old", "continue answer")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--continue", "continue new"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if stdout.String() != "continue ok\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "continue old" {
		t.Fatalf("continued old user = %q", got)
	}
	if got := messageTextAt(t, requestMessages, 2); got != "continue new" {
		t.Fatalf("continued new user = %q", got)
	}
}

func TestRunPrintRequiresCredentials(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "missing Anthropic credentials") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsUnsupportedOutputFormat(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "xml", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unsupported output format") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsUnsupportedInputFormat(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "xml", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unsupported input format") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunRejectsInvalidCWD(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", filepath.Join(t.TempDir(), "missing")}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "invalid --cwd") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsMissingMCPConfig(t *testing.T) {
	project := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "--print", "--mcp-config", "missing.json", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "load --mcp-config") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsEmptyPrompt(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "   "}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--print requires a prompt") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func writeTestTranscript(t *testing.T, path string, sessionID contracts.ID, userText string, assistantText string) {
	t.Helper()
	user := messages.UserText(userText)
	user.SessionID = sessionID
	assistant := messages.AssistantText(assistantText, "sonnet", nil)
	assistant.SessionID = sessionID
	parent := user.UUID
	assistant.ParentUUID = &parent
	if err := session.Append(path, session.EntryFromMessage(sessionID, user)); err != nil {
		t.Fatal(err)
	}
	if err := session.Append(path, session.EntryFromMessage(sessionID, assistant)); err != nil {
		t.Fatal(err)
	}
}

func messageTextAt(t *testing.T, requestMessages []any, index int) string {
	t.Helper()
	if index >= len(requestMessages) {
		t.Fatalf("messages = %#v", requestMessages)
	}
	message := requestMessages[index].(map[string]any)
	content := message["content"].([]any)
	block := content[0].(map[string]any)
	return block["text"].(string)
}
