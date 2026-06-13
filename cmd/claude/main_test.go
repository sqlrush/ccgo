package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/bootstrap"
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
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

func TestRunPrintReadsJSONInputFormatTextAlias(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_json_text_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"json text input ok"}],
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
	code := run([]string{"--print", "--input-format", "json"}, strings.NewReader(`{"text":"text alias prompt"}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "json text input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "text alias prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintReadsJSONInputFormatMessageWrapper(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_json_wrapped_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"json wrapped input ok"}],
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

	input := `{"message":{"role":"user","content":[{"type":"text","text":"wrapped message prompt"}]}}`
	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "json"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "json wrapped input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "wrapped message prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintReadsJSONInputFormatMessagesArray(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_json_messages_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"json messages input ok"}],
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
		`{"messages":[`,
		`{"role":"user","content":[{"type":"text","text":"older prompt"}]},`,
		`{"role":"assistant","content":[{"type":"text","text":"old answer"}]},`,
		`{"role":"user","content":[{"type":"text","text":"latest array prompt"}]}`,
		`]}`,
	}, "")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "json"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "json messages input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "latest array prompt" {
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

func TestRunPrintReadsStreamJSONInputFormatUserMessageEventAlias(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_stream_alias_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"stream alias input ok"}],
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
		`{"type":"status","status":"ready"}`,
		`{"type":"user_message","message":{"content":[{"type":"text","text":"alias latest prompt"}]}}`,
	}, "\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "stream-json"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "stream alias input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "alias latest prompt" {
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

func TestEffectivePermissionModeDangerouslySkipsPermissions(t *testing.T) {
	mode, err := effectivePermissionMode("", true)
	if err != nil {
		t.Fatal(err)
	}
	if mode != string(contracts.PermissionBypassPermissions) {
		t.Fatalf("mode = %q", mode)
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

func TestParseToolRulesAcceptsRepeatedFlagValues(t *testing.T) {
	got := parseToolRules("Write", "Bash(git status *)", "Read, Edit")
	want := []string{"Write", "Bash(git status *)", "Read", "Edit"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rules = %#v, want %#v", got, want)
	}
}

func TestRunPrintAccumulatesRepeatedAllowedToolFlags(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("content-type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{
				"id":"msg_write_request",
				"type":"message",
				"role":"assistant",
				"model":"claude-sonnet-4-6",
				"content":[{"type":"tool_use","id":"toolu_write","name":"Write","input":{"file_path":"flag-write.txt","content":"written by repeated allow"}}],
				"stop_reason":"tool_use"
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"id":"msg_write_done",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"rules ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	project := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--cwd", project,
		"--print",
		"--permission-mode", "dontAsk",
		"--allowed-tools", "Write",
		"--allowedTools", "Bash(git status *)",
		"--disallowed-tools", "Bash(rm *)",
		"--disallowedTools", "Edit",
		"write with repeated flags",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if stdout.String() != "rules ok\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(project, "flag-write.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "written by repeated allow" {
		t.Fatalf("written file = %q", string(data))
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
	if payload["type"] != "result" || payload["subtype"] != "success" || payload["is_error"] != false || payload["num_turns"] != float64(1) || payload["result"] != "json ok" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["session_id"] == "" {
		t.Fatalf("missing session_id: %#v", payload)
	}
	if payload["stop_reason"] != "end_turn" || payload["model"] != "claude-sonnet-4-6" {
		t.Fatalf("metadata = %#v", payload)
	}
	if _, ok := payload["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms = %#v", payload["duration_ms"])
	}
	if _, ok := payload["duration_api_ms"].(float64); !ok {
		t.Fatalf("duration_api_ms = %#v", payload["duration_api_ms"])
	}
	if cost, ok := payload["total_cost_usd"].(float64); !ok || cost <= 0 {
		t.Fatalf("total_cost_usd = %#v", payload["total_cost_usd"])
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

func TestRunPrintJSONClearIncludesCleared(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "/clear"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "success" || payload["is_error"] != false || payload["cleared"] != true {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["result"] != "" || payload["num_turns"] != nil {
		t.Fatalf("clear result metadata = %#v", payload)
	}
}

func TestRunPrintJSONLocalTextResult(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "/status"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	result, ok := payload["result"].(string)
	if !ok || !strings.Contains(result, "Status") || !strings.Contains(result, "Session ID:") {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["num_turns"] != nil || payload["cleared"] != nil {
		t.Fatalf("local result metadata = %#v", payload)
	}
}

func TestRunPrintTextLocalTextResult(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "/status"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if text := stdout.String(); !strings.Contains(text, "Status") || !strings.Contains(text, "Session ID:") {
		t.Fatalf("stdout = %q", text)
	}
}

func TestRunPrintJSONModelCommandIncludesSelectedModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "/model opus"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["model"] != "claude-opus-4-6" || !strings.Contains(fmt.Sprint(payload["result"]), "Selected model: claude-opus-4-6") {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRunPrintJSONOutputIncludesErrorResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "fail prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "error" || payload["is_error"] != true || payload["error"] == "" {
		t.Fatalf("payload = %#v", payload)
	}
	if _, ok := payload["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms = %#v", payload["duration_ms"])
	}
	if _, ok := payload["duration_api_ms"].(float64); !ok {
		t.Fatalf("duration_api_ms = %#v", payload["duration_api_ms"])
	}
	if !strings.Contains(stderr.String(), "ccgo:") {
		t.Fatalf("stderr = %q", stderr.String())
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
	t.Setenv("ANTHROPIC_BETA", "beta-one beta-two,beta-one")
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(`{"outputStyle":"Explanatory","fastMode":true,"permissions":{"defaultMode":"plan"},"mcpServers":{"docs":{"type":"http","url":"https://docs.example/mcp"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	pluginRoot := filepath.Join(project, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "plugin.json"), []byte(`{"name":"demo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "skills", "review", "SKILL.md"), []byte("---\ndescription: Review code\n---\nReview ${CLAUDE_SKILL_DIR}."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "agents", "reviewer.md"), []byte("---\nname: reviewer\ndescription: Review changes\n---\nReview."), 0o644); err != nil {
		t.Fatal(err)
	}
	expectedPluginRoot, err := filepath.EvalSymlinks(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "--print", "--output-format", "stream-json", "stream prompt"}, strings.NewReader(""), &stdout, &stderr)
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
	if events[0]["output_style"] != "Explanatory" {
		t.Fatalf("init output style = %#v", events[0])
	}
	if events[0]["permission_mode"] != "plan" || events[0]["api_key_source"] != "api_key" || events[0]["fast_mode"] != true {
		t.Fatalf("init runtime metadata = %#v", events[0])
	}
	betas, ok := events[0]["betas"].([]any)
	if !ok || len(betas) != 2 || betas[0] != "beta-one" || betas[1] != "beta-two" {
		t.Fatalf("init betas = %#v", events[0]["betas"])
	}
	outputStyles, ok := events[0]["available_output_styles"].([]any)
	if !ok || len(outputStyles) < 3 || outputStyles[0] != "default" {
		t.Fatalf("init available output styles = %#v", events[0]["available_output_styles"])
	}
	slashCommands, ok := events[0]["slash_commands"].([]any)
	if !ok || !containsAnyString(slashCommands, "help") || !containsAnyString(slashCommands, "output-style") {
		t.Fatalf("init slash commands = %#v", events[0]["slash_commands"])
	}
	skills, ok := events[0]["skills"].([]any)
	if !ok || !containsAnyString(skills, "demo:review") {
		t.Fatalf("init skills = %#v", events[0]["skills"])
	}
	agents, ok := events[0]["agents"].([]any)
	if !ok || !containsAnyString(agents, "demo:reviewer") {
		t.Fatalf("init agents = %#v", events[0]["agents"])
	}
	plugins, ok := events[0]["plugins"].([]any)
	if !ok || !containsPluginSummary(plugins, "demo", expectedPluginRoot, "local") {
		t.Fatalf("init plugins = %#v", events[0]["plugins"])
	}
	mcpServers, ok := events[0]["mcp_servers"].([]any)
	if !ok || !containsMCPServerSummary(mcpServers, "docs", "configured", "http", "user", "user", "") {
		t.Fatalf("init mcp servers = %#v", events[0]["mcp_servers"])
	}
	tools, ok := events[0]["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("init tools = %#v", events[0]["tools"])
	}
	if events[1]["type"] != "user_message" || events[2]["type"] != "assistant_message" || events[3]["type"] != "result" {
		t.Fatalf("events = %#v", events)
	}
	if events[3]["result"] != "stream ok" || events[3]["is_error"] != false || events[3]["num_turns"] != float64(1) {
		t.Fatalf("result event = %#v", events[3])
	}
	if _, ok := events[3]["duration_ms"].(float64); !ok {
		t.Fatalf("result duration_ms = %#v", events[3]["duration_ms"])
	}
	if _, ok := events[3]["duration_api_ms"].(float64); !ok {
		t.Fatalf("result duration_api_ms = %#v", events[3]["duration_api_ms"])
	}
}

func TestRunPrintStreamJSONClearIncludesCleared(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "stream-json", "/clear"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %#v stdout=%q", lines, stdout.String())
	}
	var final map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &final); err != nil {
		t.Fatalf("invalid final line %q: %v", lines[2], err)
	}
	if final["type"] != "result" || final["subtype"] != "success" || final["cleared"] != true || final["result"] != "" {
		t.Fatalf("final = %#v", final)
	}
}

func TestResultNumTurnsCountsAssistantMessages(t *testing.T) {
	result := conversation.Result{
		Messages: []contracts.Message{
			messages.UserText("one"),
			messages.AssistantText("two", "sonnet", nil),
			messages.UserText("three"),
			messages.AssistantText("four", "sonnet", nil),
		},
	}
	if turns := resultNumTurns(result); turns != 2 {
		t.Fatalf("turns = %d", turns)
	}
}

func TestWritePrintJSONResultIncludesCompactMetadata(t *testing.T) {
	plan := compactpkg.BuildPlan(
		[]contracts.Message{messages.UserText("old")},
		compactpkg.PlanOptions{
			Trigger:     compactpkg.TriggerManual,
			PreTokens:   42,
			UserContext: "keep API details",
			Summary:     "summary",
		},
	)
	result := conversation.Result{
		Messages:  []contracts.Message{plan.Summary},
		Compacted: true,
		Compact:   &compactpkg.Result{Plan: plan},
	}
	var stdout bytes.Buffer
	if err := writePrintJSONResult(&stdout, result, messages.TextContent(plan.Summary), 10, "sonnet"); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	compactPayload, ok := payload["compact"].(map[string]any)
	if payload["compacted"] != true || !ok {
		t.Fatalf("payload = %#v", payload)
	}
	if compactPayload["trigger"] != "manual" || compactPayload["preTokens"] != float64(42) || compactPayload["userContext"] != "keep API details" {
		t.Fatalf("compact payload = %#v", compactPayload)
	}
}

func TestRunPrintStreamJSONOutputIncludesErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "stream-json", "fail prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %#v", lines)
	}
	var final map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &final); err != nil {
		t.Fatalf("invalid final line %q: %v", lines[2], err)
	}
	if final["type"] != "error" || final["is_error"] != true || final["error"] == "" {
		t.Fatalf("final = %#v stdout=%q", final, stdout.String())
	}
	if _, ok := final["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms = %#v", final["duration_ms"])
	}
	if _, ok := final["duration_api_ms"].(float64); !ok {
		t.Fatalf("duration_api_ms = %#v", final["duration_api_ms"])
	}
	if !strings.Contains(stderr.String(), "ccgo:") {
		t.Fatalf("stderr = %q", stderr.String())
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

func TestRunnerMCPServerSummariesMergesSettingsAndPluginServers(t *testing.T) {
	runner := conversation.Runner{MCP: &conversation.MCPConfig{
		UserSettings: contracts.Settings{MCPServers: map[string]contracts.MCPServer{
			"zeta": {Command: "user"},
		}},
		ProjectSettings: contracts.Settings{MCPServers: map[string]contracts.MCPServer{
			"alpha": {Command: "project"},
		}},
		LocalSettings: contracts.Settings{MCPServers: map[string]contracts.MCPServer{
			"beta": {Command: "local"},
		}},
		PluginServers: map[string]contracts.MCPServer{
			"plugin:docs": {Type: "http", URL: "https://example.com/mcp", PluginSource: "demo"},
		},
	}}
	got := runnerMCPServerSummaries(runner)
	if len(got) != 4 {
		t.Fatalf("summaries = %#v", got)
	}
	if got[0].Name != "alpha" || got[0].Status != "configured" || got[0].Type != "stdio" || got[0].Scope != "project" || got[0].Source != "project" {
		t.Fatalf("alpha = %#v", got[0])
	}
	if got[1].Name != "beta" || got[1].Scope != "local" || got[1].Source != "local" {
		t.Fatalf("beta = %#v", got[1])
	}
	if got[2].Name != "plugin:docs" || got[2].Type != "http" || got[2].Source != "plugin" || got[2].PluginSource != "demo" {
		t.Fatalf("plugin = %#v", got[2])
	}
	if got[3].Name != "zeta" || got[3].Scope != "user" || got[3].Source != "user" {
		t.Fatalf("zeta = %#v", got[3])
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

func TestRunPrintJSONOutputsSetupError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "error" || !strings.Contains(fmt.Sprint(payload["error"]), "missing Anthropic credentials") {
		t.Fatalf("payload = %#v", payload)
	}
	if !strings.Contains(stderr.String(), "missing Anthropic credentials") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintJSONOutputsInputFormatError(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "--input-format", "xml", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "error" || !strings.Contains(fmt.Sprint(payload["error"]), "unsupported input format") {
		t.Fatalf("payload = %#v", payload)
	}
	if !strings.Contains(stderr.String(), "unsupported input format") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintJSONOutputsPromptError(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "error" || !strings.Contains(fmt.Sprint(payload["error"]), "--print requires a prompt") {
		t.Fatalf("payload = %#v", payload)
	}
	if !strings.Contains(stderr.String(), "--print requires a prompt") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsPermissionModeSkipConflict(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--print",
		"--output-format", "json",
		"--permission-mode", "plan",
		"--dangerously-skip-permissions",
		"hello",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["subtype"] != "error" || !strings.Contains(fmt.Sprint(payload["error"]), "dangerously-skip-permissions") {
		t.Fatalf("payload = %#v", payload)
	}
	if !strings.Contains(stderr.String(), "dangerously-skip-permissions") {
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

func TestNormalizeInputFormatAliases(t *testing.T) {
	tests := map[string]string{
		"":            "text",
		" text ":      "text",
		"json":        "json",
		"stream-json": "stream-json",
		"stream_json": "stream-json",
		"streamJson":  "stream-json",
		"stream JSON": "stream-json",
		"STREAM_JSON": "stream-json",
	}
	for raw, want := range tests {
		got, err := normalizeInputFormat(raw)
		if err != nil {
			t.Fatalf("normalizeInputFormat(%q) error: %v", raw, err)
		}
		if got != want {
			t.Fatalf("normalizeInputFormat(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestNormalizeOutputFormatAliases(t *testing.T) {
	tests := map[string]string{
		"":            "text",
		" text ":      "text",
		"json":        "json",
		"stream-json": "stream-json",
		"stream_json": "stream-json",
		"streamJson":  "stream-json",
		"stream JSON": "stream-json",
		"STREAM_JSON": "stream-json",
	}
	for raw, want := range tests {
		got, err := normalizeOutputFormat(raw)
		if err != nil {
			t.Fatalf("normalizeOutputFormat(%q) error: %v", raw, err)
		}
		if got != want {
			t.Fatalf("normalizeOutputFormat(%q) = %q, want %q", raw, got, want)
		}
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

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && text == want {
			return true
		}
	}
	return false
}

func containsPluginSummary(values []any, name string, path string, source string) bool {
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if object["name"] == name && object["path"] == path && object["source"] == source {
			return true
		}
	}
	return false
}

func containsMCPServerSummary(values []any, name string, status string, typ string, scope string, source string, pluginSource string) bool {
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if object["name"] != name || object["status"] != status || object["type"] != typ || object["scope"] != scope || object["source"] != source {
			continue
		}
		if pluginSource != "" && object["plugin_source"] != pluginSource {
			continue
		}
		return true
	}
	return false
}
