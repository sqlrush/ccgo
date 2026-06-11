package filetools

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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

func mustToolInput(t *testing.T, input any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return data
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

func TestReadPDFExtractsTextAndPageSelection(t *testing.T) {
	dir := t.TempDir()
	pdf := `%PDF-1.4
1 0 obj
<< /Type /Page >>
stream
BT
(First page) Tj
ET
endstream
endobj
2 0 obj
<< /Type /Page >>
stream
BT
(Second page) Tj
ET
endstream
endobj
%%EOF`
	if err := os.WriteFile(filepath.Join(dir, "doc.pdf"), []byte(pdf), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	full, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_pdf_full",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"doc.pdf"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full.Content.(string), "Page 1:\nFirst page") || !strings.Contains(full.Content.(string), "Page 2:\nSecond page") {
		t.Fatalf("full PDF content = %#v", full.Content)
	}
	if full.StructuredContent["type"] != "pdf" || full.StructuredContent["pageCount"] != 2 {
		t.Fatalf("full PDF structured = %#v", full.StructuredContent)
	}

	page, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_pdf_page",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"doc.pdf","pages":"2"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Content != "PDF: doc.pdf\nPages: 2\n\nPage 2:\nSecond page" {
		t.Fatalf("page PDF content = %#v", page.Content)
	}
	selected := page.StructuredContent["selected_pages"].([]int)
	if len(selected) != 1 || selected[0] != 2 || page.StructuredContent["text"] != "Second page" {
		t.Fatalf("page PDF structured = %#v", page.StructuredContent)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_pdf_bad_page",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"doc.pdf","pages":"3"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds PDF page count") {
		t.Fatalf("bad PDF page err = %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_non_pdf_pages",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"plain.txt","pages":"1"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "pages are only supported for PDF files") {
		t.Fatalf("non-PDF pages err = %v", err)
	}
}

func TestReadPDFExtractsReferencedCompressedPageContent(t *testing.T) {
	dir := t.TempDir()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write([]byte("BT\n(Compressed second) Tj\nET")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	pdf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	pdf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>\nendobj\n")
	pdf.WriteString("4 0 obj\n<< /Type /Page /Contents 6 0 R >>\nendobj\n")
	pdf.WriteString("3 0 obj\n<< /Type /Page /Contents 5 0 R >>\nendobj\n")
	pdf.WriteString("5 0 obj\n<< /Length 24 >>\nstream\nBT\n(Referenced first) Tj\nET\nendstream\nendobj\n")
	pdf.WriteString("6 0 obj\n<< /Length ")
	pdf.WriteString(strconv.Itoa(compressed.Len()))
	pdf.WriteString(" /Filter /FlateDecode >>\nstream\n")
	pdf.Write(compressed.Bytes())
	pdf.WriteString("\nendstream\nendobj\n%%EOF")
	if err := os.WriteFile(filepath.Join(dir, "referenced.pdf"), pdf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := fileExecutor(t).Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_pdf_referenced",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"referenced.pdf","pages":"1-2"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := result.Content.(string)
	if !strings.Contains(content, "Page 1:\nReferenced first") || !strings.Contains(content, "Page 2:\nCompressed second") {
		t.Fatalf("referenced PDF content = %#v", content)
	}
	if result.StructuredContent["pageCount"] != 2 || result.StructuredContent["text"] != "Referenced first\n\nCompressed second" {
		t.Fatalf("referenced PDF structured = %#v", result.StructuredContent)
	}
}

func TestReadPDFDecodesUTF16HexStrings(t *testing.T) {
	dir := t.TempDir()
	pdf := `%PDF-1.4
1 0 obj
<< /Type /Page >>
stream
BT
<FEFF00480065006C006C006F00204E16754C> Tj
ET
endstream
endobj
%%EOF`
	if err := os.WriteFile(filepath.Join(dir, "utf16.pdf"), []byte(pdf), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := fileExecutor(t).Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_pdf_utf16",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"utf16.pdf"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "Hello \u4e16\u754c"
	if !strings.Contains(result.Content.(string), want) || result.StructuredContent["text"] != want {
		t.Fatalf("UTF-16 PDF result = %#v structured=%#v", result.Content, result.StructuredContent)
	}
}

func TestReadToolReturnsImageContentBlock(t *testing.T) {
	dir := t.TempDir()
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n'}
	if err := os.WriteFile(filepath.Join(dir, "chart.png"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := fileExecutor(t).Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_read_image",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"chart.png"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	blocks, ok := result.Content.([]contracts.ContentBlock)
	if !ok || len(blocks) != 2 {
		t.Fatalf("image content = %#v", result.Content)
	}
	if blocks[0].Type != contracts.ContentText || !strings.Contains(blocks[0].Text, "Read image file chart.png") {
		t.Fatalf("image summary block = %#v", blocks[0])
	}
	source, ok := blocks[1].Source.(contracts.ImageSource)
	if blocks[1].Type != contracts.ContentImage || !ok {
		t.Fatalf("image block = %#v", blocks[1])
	}
	if source.Type != "base64" || source.MediaType != "image/png" || source.Data != base64.StdEncoding.EncodeToString(data) {
		t.Fatalf("image source = %#v", source)
	}
	file := result.StructuredContent["file"].(map[string]any)
	if result.StructuredContent["type"] != "image" || file["mediaType"] != "image/png" || file["bytes"] != len(data) {
		t.Fatalf("structured image content = %#v", result.StructuredContent)
	}
}

func TestReadToolRejectsImageOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := fileExecutor(t).Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_read_image_offset",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"photo.jpg","offset":1}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "offset and limit are only supported for text files") {
		t.Fatalf("image offset err = %v", err)
	}
}

func TestReadToolLargeTextUsesResultBudget(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("0123456789\n", 12_000)
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	executor.ResultStoreDir = filepath.Join(dir, "tool-results")
	result, err := executor.Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_read_large",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"large.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	text, ok := result.Content.(string)
	if !ok || !strings.Contains(text, "[Tool output truncated; full output saved to ") {
		t.Fatalf("large read content = %#v", result.Content)
	}
	if result.Meta["truncated"] != true {
		t.Fatalf("large read meta = %#v", result.Meta)
	}
	fullPath, ok := result.Meta["full_output_path"].(string)
	if !ok || fullPath == "" {
		t.Fatalf("full output path meta = %#v", result.Meta)
	}
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("full output file missing: %v", err)
	}
}

func TestReadToolRendersNotebookCells(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "cells": [
    {"cell_type": "markdown", "source": ["# Title\n", "body"]},
    {"cell_type": "code", "execution_count": 1, "source": "print('hi')\n", "outputs": [{"output_type": "stream", "name": "stdout", "text": ["hi\n"]}]}
  ],
  "metadata": {},
  "nbformat": 4,
  "nbformat_minor": 5
}`
	path := filepath.Join(dir, "analysis.ipynb")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := fileToolContext(dir)
	result, err := fileExecutor(t).Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read_notebook",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"analysis.ipynb"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := result.Content.(string)
	for _, want := range []string{"Notebook: analysis.ipynb", "Cell 1 [markdown]:\n# Title\nbody", "Cell 2 [code] execution_count=1:\nprint('hi')", "Outputs:\nhi"} {
		if !strings.Contains(content, want) {
			t.Fatalf("notebook content missing %q:\n%s", want, content)
		}
	}
	file := result.StructuredContent["file"].(map[string]any)
	cells := file["cells"].([]map[string]any)
	if len(cells) != 2 || cells[0]["cell_type"] != "markdown" || cells[1]["cell_type"] != "code" {
		t.Fatalf("structured notebook cells = %#v", cells)
	}
	record, ok := EnsureReadState(ctx).Get(path)
	if !ok || !strings.Contains(record.Content, `"nbformat": 4`) || record.PartialView {
		t.Fatalf("notebook read state = %#v ok=%v", record, ok)
	}
}

func TestReadToolRejectsNotebookOffsetLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "analysis.ipynb"), []byte(`{"cells":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := fileExecutor(t).Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_read_notebook_offset",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"analysis.ipynb","limit":1}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "offset and limit are only supported for text files") {
		t.Fatalf("notebook offset err = %v", err)
	}
}

func TestNotebookEditReplacesCodeCellAndClearsOutputs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "analysis.ipynb")
	raw := `{
  "cells": [
    {"cell_type": "markdown", "id": "intro", "metadata": {}, "source": "# Title\n"},
    {"cell_type": "code", "id": "code-a", "metadata": {}, "execution_count": 7, "source": "print('old')\n", "outputs": [{"output_type": "stream", "name": "stdout", "text": "old\n"}]}
  ],
  "metadata": {"language_info": {"name": "python"}},
  "nbformat": 4,
  "nbformat_minor": 5
}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := fileToolContext(dir)
	executor := fileExecutor(t)
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read_notebook_before_edit",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"analysis.ipynb"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_notebook_replace",
		Name:  "NotebookEdit",
		Input: json.RawMessage(`{"notebook_path":"analysis.ipynb","cell_id":"code-a","new_source":"print('new')\n"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["edit_mode"] != "replace" || result.StructuredContent["cell_id"] != "code-a" || result.StructuredContent["cell_type"] != "code" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if diff := result.StructuredContent["diff"].(string); !strings.Contains(diff, "-print('old')") || !strings.Contains(diff, "+print('new')") {
		t.Fatalf("notebook edit diff = %#v", diff)
	}
	if hunks := result.StructuredContent["hunks"].([]map[string]any); len(hunks) != 1 {
		t.Fatalf("notebook edit hunks = %#v", hunks)
	}
	var updated map[string]any
	if err := readJSONFile(path, &updated); err != nil {
		t.Fatal(err)
	}
	cells := updated["cells"].([]any)
	code := cells[1].(map[string]any)
	if code["source"] != "print('new')\n" || code["execution_count"] != nil || len(code["outputs"].([]any)) != 0 {
		t.Fatalf("updated code cell = %#v", code)
	}
	record, ok := EnsureReadState(ctx).Get(path)
	if !ok || !strings.Contains(record.Content, "print('new')") || record.PartialView {
		t.Fatalf("notebook edit read state = %#v ok=%v", record, ok)
	}
}

func TestNotebookEditInsertsAndDeletesCells(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "analysis.ipynb")
	raw := `{
  "cells": [
    {"cell_type": "code", "id": "a", "metadata": {}, "execution_count": null, "source": "a = 1", "outputs": []},
    {"cell_type": "markdown", "id": "b", "metadata": {}, "source": "old"}
  ],
  "metadata": {"language_info": {"name": "python"}},
  "nbformat": 4,
  "nbformat_minor": 5
}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := fileToolContext(dir)
	executor := fileExecutor(t)
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read_notebook_before_insert",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"analysis.ipynb"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	inserted, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_notebook_insert",
		Name:  "NotebookEdit",
		Input: json.RawMessage(`{"notebook_path":"analysis.ipynb","cell_id":"a","new_source":"inserted","cell_type":"markdown","edit_mode":"insert"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if inserted.StructuredContent["edit_mode"] != "insert" || inserted.StructuredContent["cell_type"] != "markdown" {
		t.Fatalf("insert structured content = %#v", inserted.StructuredContent)
	}
	var afterInsert map[string]any
	if err := readJSONFile(path, &afterInsert); err != nil {
		t.Fatal(err)
	}
	cells := afterInsert["cells"].([]any)
	if len(cells) != 3 || cells[1].(map[string]any)["source"] != "inserted" || cells[1].(map[string]any)["cell_type"] != "markdown" {
		t.Fatalf("inserted cells = %#v", cells)
	}

	deleted, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_notebook_delete",
		Name:  "NotebookEdit",
		Input: json.RawMessage(`{"notebook_path":"analysis.ipynb","cell_id":"b","new_source":"","edit_mode":"delete"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.StructuredContent["edit_mode"] != "delete" || deleted.StructuredContent["cell_id"] != "b" {
		t.Fatalf("delete structured content = %#v", deleted.StructuredContent)
	}
	var afterDelete map[string]any
	if err := readJSONFile(path, &afterDelete); err != nil {
		t.Fatal(err)
	}
	cells = afterDelete["cells"].([]any)
	if len(cells) != 2 || cells[1].(map[string]any)["source"] == "old" {
		t.Fatalf("deleted cells = %#v", cells)
	}
}

func TestNotebookEditRequiresReadFirst(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "analysis.ipynb"), []byte(`{"cells":[],"nbformat":4,"nbformat_minor":5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := fileExecutor(t).Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_notebook_without_read",
		Name:  "NotebookEdit",
		Input: json.RawMessage(`{"notebook_path":"analysis.ipynb","cell_id":"cell-0","new_source":"x"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "Read it first") {
		t.Fatalf("notebook edit read-first err = %v", err)
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
	if diff := result.StructuredContent["diff"].(string); !strings.Contains(diff, "-old") || !strings.Contains(diff, "+new") {
		t.Fatalf("write diff = %#v", diff)
	}
	hunks := result.StructuredContent["hunks"].([]map[string]any)
	if len(hunks) != 1 {
		t.Fatalf("write hunks = %#v", hunks)
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
	if diff := result.StructuredContent["diff"].(string); !strings.Contains(diff, "--- a/nested/new.txt") || !strings.Contains(diff, "+created") {
		t.Fatalf("create diff = %#v", diff)
	}
	hunks := result.StructuredContent["hunks"].([]map[string]any)
	if len(hunks) != 1 || hunks[0]["old_lines"] != 0 || hunks[0]["new_lines"] != 1 {
		t.Fatalf("create hunks = %#v", hunks)
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

func TestWriteRejectsInvalidSettingsJSON(t *testing.T) {
	dir := t.TempDir()
	ctx := fileToolContext(dir)
	executor := fileExecutor(t)

	for _, tc := range []struct {
		name    string
		path    string
		content string
		wantErr string
	}{
		{
			name:    "malformed",
			path:    ".claude/settings.json",
			content: `{"model":`,
			wantErr: "invalid settings file",
		},
		{
			name:    "validation warning",
			path:    ".claude/settings.local.json",
			content: `{"permissions":{"defaultMode":"bad-mode"}}`,
			wantErr: "permissions.defaultMode",
		},
	} {
		_, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    contracts.ID("toolu_settings_" + strings.ReplaceAll(tc.name, " ", "_")),
			Name:  "Write",
			Input: mustToolInput(t, writeInput{FilePath: tc.path, Content: tc.content}),
		}, nil)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("%s err = %v", tc.name, err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(tc.path))); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("%s file should not be written, stat err = %v", tc.name, statErr)
		}
	}
}

func TestEditRejectsInvalidSettingsJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"model":"opus"}` + "\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := fileToolContext(dir)
	executor := fileExecutor(t)
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read_settings",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":".claude/settings.json"}`),
	}, nil); err != nil {
		t.Fatal(err)
	}

	_, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_edit_settings",
		Name: "Edit",
		Input: mustToolInput(t, editInput{
			FilePath:  ".claude/settings.json",
			OldString: "opus",
			NewString: `bad"json`,
		}),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid settings file") {
		t.Fatalf("edit err = %v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != original {
		t.Fatalf("settings file changed after rejected edit: %q", data)
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
	if diff := result.StructuredContent["diff"].(string); !strings.Contains(diff, "-foo") || !strings.Contains(diff, "+bar") {
		t.Fatalf("edit diff = %#v", diff)
	}
	hunks := result.StructuredContent["hunks"].([]map[string]any)
	if len(hunks) != 1 {
		t.Fatalf("edit hunks = %#v", hunks)
	}
	lines := hunks[0]["lines"].([]map[string]any)
	if len(lines) != 4 || lines[0]["op"] != "delete" || lines[2]["op"] != "insert" {
		t.Fatalf("edit hunk lines = %#v", lines)
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

func TestGlobToolMatchesRecursiveFilesSortedByModifiedTime(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "src", "old.go")
	newPath := filepath.Join(dir, "src", "nested", "new.go")
	hiddenPath := filepath.Join(dir, ".git", "hidden.go")
	if err := os.MkdirAll(filepath.Dir(hiddenPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		oldPath:    "package old\n",
		newPath:    "package nested\n",
		hiddenPath: "package hidden\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-1 * time.Hour)
	hiddenTime := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(hiddenPath, hiddenTime, hiddenTime); err != nil {
		t.Fatal(err)
	}

	result, err := fileExecutor(t).Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_glob",
		Name:  "Glob",
		Input: json.RawMessage(`{"pattern":"**/*.go"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != ".git/hidden.go\nsrc/old.go\nsrc/nested/new.go" {
		t.Fatalf("glob content = %#v", result.Content)
	}
	files := result.StructuredContent["files"].([]string)
	if len(files) != 3 || files[0] != ".git/hidden.go" || files[1] != "src/old.go" || files[2] != "src/nested/new.go" {
		t.Fatalf("structured files = %#v", files)
	}
}

func TestGlobToolDefaultNoIgnoreAndHidden(t *testing.T) {
	t.Setenv("CLAUDE_CODE_GLOB_NO_IGNORE", "")
	t.Setenv("CLAUDE_CODE_GLOB_HIDDEN", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{".hidden.log", "debug.log", "keep.log"}
	mtime := time.Now().Add(-time.Hour)
	for _, rel := range paths {
		path := filepath.Join(dir, rel)
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	result, err := fileExecutor(t).Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_glob_no_ignore_hidden",
		Name:  "Glob",
		Input: json.RawMessage(`{"pattern":"**/*.log"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != ".hidden.log\ndebug.log\nkeep.log" {
		t.Fatalf("glob no-ignore hidden content = %#v", result.Content)
	}
}

func TestGlobAndGrepReturnWorkingDirectoryRelativePaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"src/a.go":  "Needle go\n",
		"src/b.txt": "Needle text\n",
	}
	mtime := time.Now().Add(-time.Hour)
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	pathResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_glob_path_relative_output",
		Name:  "Glob",
		Input: json.RawMessage(`{"pattern":"*.go","path":"src"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pathResult.Content != "src/a.go" {
		t.Fatalf("glob path output = %#v", pathResult.Content)
	}

	absolutePattern := filepath.ToSlash(filepath.Join(dir, "src", "*.txt"))
	absInput, err := json.Marshal(map[string]any{"pattern": absolutePattern})
	if err != nil {
		t.Fatal(err)
	}
	absResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_glob_absolute_pattern",
		Name:  "Glob",
		Input: absInput,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if absResult.Content != "src/b.txt" {
		t.Fatalf("glob absolute output = %#v", absResult.Content)
	}

	grepResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_path_relative_output",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","path":"src"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if grepResult.Content != "src/a.go\nsrc/b.txt" {
		t.Fatalf("grep path output = %#v", grepResult.Content)
	}
}

func TestGrepToolOutputModesAndGlobFilter(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"src/a.go":  "package main\nfunc Alpha() {}\n",
		"src/b.txt": "Alpha text\n",
		"src/c.go":  "func Beta() {}\nfunc AlphaBeta() {}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	filesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_files",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.go"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filesResult.Content != "src/a.go\nsrc/c.go" {
		t.Fatalf("files result = %#v", filesResult.Content)
	}

	contentResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_content",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.go","output_mode":"content"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if contentResult.Content != "src/a.go:2:func Alpha() {}\nsrc/c.go:2:func AlphaBeta() {}" {
		t.Fatalf("content result = %#v", contentResult.Content)
	}

	countResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"func","glob":"**/*.go","output_mode":"count"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if countResult.Content != "src/a.go:1\nsrc/c.go:2" {
		t.Fatalf("count result = %#v", countResult.Content)
	}

	camelModeResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_camel_output_mode",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"func","glob":"**/*.go","outputMode":"count"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if camelModeResult.Content != "src/a.go:1\nsrc/c.go:2" || camelModeResult.StructuredContent["output_mode"] != "count" {
		t.Fatalf("camel outputMode result = %#v", camelModeResult)
	}

	multiGlobResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_multi_glob",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.go,**/*.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if multiGlobResult.Content != "src/a.go\nsrc/b.txt\nsrc/c.go" {
		t.Fatalf("multi glob result = %#v", multiGlobResult.Content)
	}

	braceGlobResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_brace_glob",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.{go,txt}"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if braceGlobResult.Content != "src/a.go\nsrc/b.txt\nsrc/c.go" {
		t.Fatalf("brace glob result = %#v", braceGlobResult.Content)
	}
}

func TestGrepToolContentContextAndPagination(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"one",
		"Needle first",
		"three",
		"four",
		"Needle second",
		"six",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	contextResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_context",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","before_context":1,"after_context":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantContext := "a.txt-1-one\na.txt:2:Needle first\na.txt-3-three\na.txt-4-four\na.txt:5:Needle second\na.txt-6-six"
	if contextResult.Content != wantContext {
		t.Fatalf("context content = %#v", contextResult.Content)
	}
	matches := contextResult.StructuredContent["matches"].([]map[string]any)
	if len(matches) != 6 || matches[0]["matched"] != false || matches[1]["matched"] != true {
		t.Fatalf("structured context matches = %#v", matches)
	}

	noLineNumberResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_line_numbers",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","-n":false,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if noLineNumberResult.Content != "a.txt:Needle first" || noLineNumberResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("no-line-number content = %#v", noLineNumberResult)
	}

	snakeLineNumberResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_line_numbers_alias",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","line_numbers":false,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snakeLineNumberResult.Content != "a.txt:Needle first" || snakeLineNumberResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("line_numbers alias result = %#v", snakeLineNumberResult)
	}

	camelLineNumberResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_line_numbers_camel_alias",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","lineNumbers":false,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if camelLineNumberResult.Content != "a.txt:Needle first" || camelLineNumberResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("lineNumbers alias result = %#v", camelLineNumberResult)
	}

	shortContextResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_short_context",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","-B":1,"-A":0}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantShortContext := "a.txt-1-one\na.txt:2:Needle first\na.txt-4-four\na.txt:5:Needle second"
	if shortContextResult.Content != wantShortContext {
		t.Fatalf("short context content = %#v", shortContextResult.Content)
	}
	if shortContextResult.StructuredContent["before_context"] != 1 || shortContextResult.StructuredContent["after_context"] != 0 {
		t.Fatalf("short context structured content = %#v", shortContextResult.StructuredContent)
	}

	pagedResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_paged",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","offset":1,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pagedResult.Content != "a.txt:5:Needle second" {
		t.Fatalf("paged content = %#v", pagedResult.Content)
	}
	if pagedResult.StructuredContent["total_matches"] != 2 || pagedResult.StructuredContent["offset"] != 1 || pagedResult.StructuredContent["limit"] != 1 || pagedResult.StructuredContent["truncated"] != false {
		t.Fatalf("paged structured content = %#v", pagedResult.StructuredContent)
	}

	unlimitedResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_unlimited_head_limit",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","head_limit":0}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantUnlimited := "a.txt:2:Needle first\na.txt:5:Needle second"
	if unlimitedResult.Content != wantUnlimited || unlimitedResult.StructuredContent["limit"] != 0 || unlimitedResult.StructuredContent["truncated"] != false {
		t.Fatalf("unlimited head_limit result = %#v", unlimitedResult)
	}

	maxCountResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_max_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","max_count":1,"context":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantMaxCount := "a.txt-1-one\na.txt:2:Needle first\na.txt-3-three"
	if maxCountResult.Content != wantMaxCount || maxCountResult.StructuredContent["max_count"] != 1 {
		t.Fatalf("max_count result = %#v", maxCountResult)
	}

	shortMaxCountResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_short_max_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"count","-m":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortMaxCountResult.Content != "a.txt:1" || shortMaxCountResult.StructuredContent["max_count"] != 1 {
		t.Fatalf("short max_count result = %#v", shortMaxCountResult)
	}
}

func TestGrepToolCaseInsensitiveAndValidation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mixed.txt"), []byte("Alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_ignore_case",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha","case_insensitive":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "mixed.txt" || result.StructuredContent["case_insensitive"] != true {
		t.Fatalf("case-insensitive result = %#v", result)
	}

	ignoreCaseResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_ignore_case_alias",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha","ignore_case":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ignoreCaseResult.Content != "mixed.txt" || ignoreCaseResult.StructuredContent["case_insensitive"] != true {
		t.Fatalf("ignore_case result = %#v", ignoreCaseResult)
	}

	shortResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_short_ignore_case",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha","-i":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortResult.Content != "mixed.txt" || shortResult.StructuredContent["case_insensitive"] != true {
		t.Fatalf("short case-insensitive result = %#v", shortResult)
	}

	ignoredContextResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_ignored_context",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","context":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ignoredContextResult.Content != "mixed.txt" || ignoredContextResult.StructuredContent["before_context"] != 0 || ignoredContextResult.StructuredContent["after_context"] != 0 {
		t.Fatalf("ignored context result = %#v", ignoredContextResult)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_bad_max_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","max_count":-1}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "max_count must be non-negative") {
		t.Fatalf("max_count validation err = %v", err)
	}
}

func TestGrepToolFixedStrings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "literal.txt"), []byte("a+b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "regex.txt"), []byte("aaab\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	regexResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_regex_meta",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"a+b","output_mode":"content"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if regexResult.Content != "regex.txt:1:aaab" {
		t.Fatalf("regex result = %#v", regexResult.Content)
	}

	fixedResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_fixed",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"a+b","output_mode":"content","-F":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fixedResult.Content != "literal.txt:1:a+b" || fixedResult.StructuredContent["fixed_strings"] != true {
		t.Fatalf("fixed result = %#v", fixedResult)
	}
}

func TestGrepToolMultiline(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"alpha start",
		"middle",
		"gamma end",
		"tail",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "multi.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	defaultResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_default_single_line",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha.*gamma","output_mode":"content"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if defaultResult.Content != "No matches found" || defaultResult.StructuredContent["multiline"] != false {
		t.Fatalf("default single-line result = %#v", defaultResult)
	}

	multilineResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_multiline",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha.*gamma","output_mode":"content","multiline":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "multi.txt:1:alpha start\nmulti.txt:2:middle\nmulti.txt:3:gamma end"
	if multilineResult.Content != want || multilineResult.StructuredContent["multiline"] != true {
		t.Fatalf("multiline result = %#v", multilineResult)
	}
	matches := multilineResult.StructuredContent["matches"].([]map[string]any)
	if len(matches) != 3 || matches[0]["matched"] != true || matches[2]["line"] != 3 {
		t.Fatalf("multiline structured matches = %#v", matches)
	}
}

func TestGrepToolTypeFilter(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"src/a.go":  "Needle go\n",
		"src/b.txt": "Needle text\n",
		"src/c.jsx": "Needle jsx\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	goResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_type_go",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","type":"go"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if goResult.Content != "src/a.go" || goResult.StructuredContent["type_filter"] != "go" {
		t.Fatalf("go type result = %#v", goResult)
	}

	jsResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_type_js",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","type":"javascript"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if jsResult.Content != "src/c.jsx" {
		t.Fatalf("javascript type result = %#v", jsResult.Content)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_bad_type",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","type":"*.go"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "type must be a file type or extension") {
		t.Fatalf("type validation err = %v", err)
	}
}

func TestGlobAndGrepRespectIgnoreFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub", "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, vcsDir := range []string{".bzr", ".jj", ".sl"} {
		if err := os.MkdirAll(filepath.Join(dir, vcsDir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n!important.log\nignored/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ignore"), []byte("scratch.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", ".gitignore"), []byte("local.txt\n!visible.txt\nlogs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"debug.log":       "Needle hidden by gitignore\n",
		"important.log":   "Needle visible by negation\n",
		"keep.txt":        "Needle visible\n",
		"ignored/hit.txt": "Needle hidden by ignored directory\n",
		"scratch.txt":     "Needle hidden by ignore file\n",
		"sub/local.txt":   "Needle hidden by nested gitignore\n",
		"sub/visible.txt": "Needle visible by nested negation\n",
		"sub/logs/hit.md": "Needle hidden by nested ignored directory\n",
		".bzr/hit.log":    "Needle hidden by bzr metadata\n",
		".jj/hit.log":     "Needle hidden by jj metadata\n",
		".sl/hit.log":     "Needle hidden by sapling metadata\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	t.Setenv("CLAUDE_CODE_GLOB_NO_IGNORE", "false")
	t.Setenv("CLAUDE_CODE_GLOB_HIDDEN", "false")
	globResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_glob_ignore",
		Name:  "Glob",
		Input: json.RawMessage(`{"pattern":"**/*.log"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if globResult.Content != "important.log" {
		t.Fatalf("glob content = %#v", globResult.Content)
	}

	grepResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_ignore",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if grepResult.Content != "important.log\nkeep.txt\nsub/visible.txt" {
		t.Fatalf("grep content = %#v", grepResult.Content)
	}
}

func readJSONFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
