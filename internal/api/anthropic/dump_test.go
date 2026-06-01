package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestPromptDumperWritesInitNewUserMessagesAndResponses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dump.jsonl")
	dumper := NewPromptDumper(path)
	dumper.Now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"sonnet","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithPromptDumper(dumper))
	req := Request{
		Model:     "sonnet",
		MaxTokens: 32,
		System:    "system prompt",
		Tools:     []ToolDefinition{{Name: "Read", InputSchema: contracts.JSONSchema{"type": "object"}}},
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	}
	if _, err := client.CreateMessage(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.Messages = append(req.Messages,
		contracts.APIMessage{Role: "assistant", Content: []contracts.ContentBlock{contracts.NewTextBlock("ok")}},
		contracts.APIMessage{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("next")}},
	)
	if _, err := client.CreateMessage(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	entries := readDumpEntries(t, path)
	if got := dumpTypes(entries); got != "init,message,response,message,response" {
		t.Fatalf("entry types = %s", got)
	}
	if _, ok := entries[0]["data"].(map[string]any)["messages"]; ok {
		t.Fatalf("init data should not include messages: %#v", entries[0])
	}
	if entries[1]["data"].(map[string]any)["role"] != "user" || entries[3]["data"].(map[string]any)["role"] != "user" {
		t.Fatalf("message entries = %#v %#v", entries[1], entries[3])
	}
	if len(dumper.CachedRequests()) != 2 {
		t.Fatalf("cached requests = %#v", dumper.CachedRequests())
	}
}

func TestPromptDumperWritesStreamingResponseChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dump-stream.jsonl")
	dumper := NewPromptDumper(path)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"sonnet\",\"content\":[]}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n"))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithPromptDumper(dumper))
	var seen int
	err := client.StreamMessages(context.Background(), Request{
		Model:     "sonnet",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	}, func(event StreamEvent) error {
		seen++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("seen events = %d", seen)
	}
	entries := readDumpEntries(t, path)
	if got := dumpTypes(entries); got != "init,message,response" {
		t.Fatalf("entry types = %s", got)
	}
	response := entries[2]["data"].(map[string]any)
	if response["stream"] != true || len(response["chunks"].([]any)) != 2 {
		t.Fatalf("stream response = %#v", response)
	}
}

func readDumpEntries(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var entries []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return entries
}

func dumpTypes(entries []map[string]any) string {
	out := ""
	for i, entry := range entries {
		if i > 0 {
			out += ","
		}
		out += entry["type"].(string)
	}
	return out
}
