package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if len(lines) != 3 {
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
	if events[0]["type"] != "user_message" || events[1]["type"] != "assistant_message" || events[2]["type"] != "result" {
		t.Fatalf("events = %#v", events)
	}
	if events[2]["result"] != "stream ok" {
		t.Fatalf("result event = %#v", events[2])
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
