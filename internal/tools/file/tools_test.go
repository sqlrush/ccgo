package filetools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
	"ccgo/internal/tool"
)

func fileToolContext(dir string) tool.Context {
	return WithReadState(tool.Context{
		Context:          context.Background(),
		WorkingDirectory: dir,
		Metadata:         map[string]any{},
	}, NewReadState())
}

func fileExecutor(t *testing.T) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(BuiltinTools()...)
	if err != nil {
		t.Fatal(err)
	}
	return tool.NewExecutor(registry)
}

func TestReadToolLineNumbersAndDedup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := fileToolContext(dir)
	executor := fileExecutor(t)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"sample.txt","offset":2,"limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "2\tbeta" {
		t.Fatalf("content = %#v", result.Content)
	}
	state := EnsureReadState(ctx)
	record, ok := state.Get(path)
	if !ok || !record.PartialView {
		t.Fatalf("read state = %#v ok=%v", record, ok)
	}

	result, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_full",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"sample.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content.(string), "1\talpha") || !strings.Contains(result.Content.(string), "3\tgamma") {
		t.Fatalf("full read content = %#v", result.Content)
	}

	result, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_dedup",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"sample.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != fileUnchangedStub {
		t.Fatalf("dedup content = %#v", result.Content)
	}
}

func TestReadToolPrefixesAutoMemoryFreshnessNote(t *testing.T) {
	dir := t.TempDir()
	autoMemoryDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(autoMemoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(autoMemoryDir, "old.md")
	if err := os.WriteFile(path, []byte("memory fact\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Now().Add(-3 * 24 * time.Hour)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	ctx := fileToolContext(dir)
	ctx.Metadata[tool.MetadataInternalPathContextKey] = permissions.InternalPathContext{AutoMemoryDir: autoMemoryDir}
	executor := fileExecutor(t)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read_memory",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"memory/old.md"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := result.Content.(string)
	if !strings.HasPrefix(content, "<system-reminder>This memory is 3 days old.") || !strings.Contains(content, "1\tmemory fact") {
		t.Fatalf("content = %#v", content)
	}
	if file := result.StructuredContent["file"].(map[string]any); file["content"] != "memory fact\n" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}

	regularPath := filepath.Join(dir, "regular.md")
	if err := os.WriteFile(regularPath, []byte("regular\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(regularPath, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	regular, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read_regular",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"regular.md"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(regular.Content.(string), "This memory is") {
		t.Fatalf("regular content = %#v", regular.Content)
	}
}

func TestWriteRequiresReadForExistingFileAndRejectsStaleWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := fileToolContext(dir)
	executor := fileExecutor(t)

	_, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_write",
		Name:  "Write",
		Input: json.RawMessage(`{"file_path":"existing.txt","content":"new\n"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "Read it first") {
		t.Fatalf("write err = %v", err)
	}

	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"existing.txt"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_write2",
		Name:  "Write",
		Input: json.RawMessage(`{"file_path":"existing.txt","content":"new\n"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "The file existing.txt has been updated successfully." {
		t.Fatalf("write content = %#v", result.Content)
	}

	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read_again",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"existing.txt"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_stale",
		Name:  "Write",
		Input: json.RawMessage(`{"file_path":"existing.txt","content":"agent\n"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), staleWriteError) {
		t.Fatalf("stale err = %v", err)
	}
}

func TestWriteCreatesNewFileWithoutPriorRead(t *testing.T) {
	dir := t.TempDir()
	ctx := fileToolContext(dir)
	executor := fileExecutor(t)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_create",
		Name:  "Write",
		Input: json.RawMessage(`{"file_path":"nested/new.txt","content":"created\n"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "File created successfully at: nested/new.txt" {
		t.Fatalf("create content = %#v", result.Content)
	}
	data, err := os.ReadFile(filepath.Join(dir, "nested", "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "created\n" {
		t.Fatalf("file content = %q", data)
	}
}

func TestWriteRejectsTeamMemorySecrets(t *testing.T) {
	dir := t.TempDir()
	ctx := fileToolContext(dir)
	executor := fileExecutor(t)

	_, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_secret",
		Name:  "Write",
		Input: json.RawMessage(`{"file_path":".claude/team-memory/auth.md","content":"token = ghp_123456789012345678901234567890123456"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "possible secret") {
		t.Fatalf("secret err = %v", err)
	}
}

func TestEditRequiresUniqueMatchUnlessReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.txt")
	if err := os.WriteFile(path, []byte("foo\nfoo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := fileToolContext(dir)
	executor := fileExecutor(t)
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"dup.txt"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}

	_, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_edit",
		Name:  "Edit",
		Input: json.RawMessage(`{"file_path":"dup.txt","old_string":"foo","new_string":"bar"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "Found 2 matches") {
		t.Fatalf("duplicate err = %v", err)
	}

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_replace_all",
		Name:  "Edit",
		Input: json.RawMessage(`{"file_path":"dup.txt","old_string":"foo","new_string":"bar","replace_all":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content.(string), "All occurrences") {
		t.Fatalf("replace_all content = %#v", result.Content)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "bar\nbar\n" {
		t.Fatalf("edited content = %q", data)
	}

	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_second_edit",
		Name:  "Edit",
		Input: json.RawMessage(`{"file_path":"dup.txt","old_string":"bar\nbar\n","new_string":"baz\nbaz\n"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "baz\nbaz\n" {
		t.Fatalf("second edited content = %q", data)
	}
}

func TestEditPreservesCurlyQuoteStyle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quotes.txt")
	if err := os.WriteFile(path, []byte("const s = “hello”\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := fileToolContext(dir)
	executor := fileExecutor(t)
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"quotes.txt"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_edit",
		Name:  "Edit",
		Input: json.RawMessage(`{"file_path":"quotes.txt","old_string":"const s = \"hello\"","new_string":"const s = \"bye\""}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "const s = “bye”\n" {
		t.Fatalf("edited content = %q", data)
	}
}
