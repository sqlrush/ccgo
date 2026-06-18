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
		Input: json.RawMessage(`{"file_path":"sample.txt","offset":"02.0","limit":"1"}`),
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

func TestReadToolDiscoversNestedSkillDirs(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "pkg", "sub", "target.txt")
	pkgSkill := filepath.Join(dir, "pkg", ".claude", "skills", "pkg-skill")
	subSkill := filepath.Join(dir, "pkg", "sub", ".claude", "skills", "sub-skill")
	for _, skillDir := range []string{pkgSkill, subSkill} {
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: test\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filePath, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := fileToolContext(dir)
	_, err := fileExecutor(t).Execute(ctx, contracts.ToolUse{
		ID:    "toolu_read_nested_skill",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"pkg/sub/target.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	internal := tool.InternalPathContextFromMetadata(ctx.Metadata)
	want := []string{subSkill, pkgSkill}
	if len(internal.SkillDirs) != len(want) {
		t.Fatalf("skill dirs = %#v, want %#v", internal.SkillDirs, want)
	}
	for i := range want {
		if internal.SkillDirs[i] != want[i] {
			t.Fatalf("skill dirs = %#v, want %#v", internal.SkillDirs, want)
		}
	}
	if got := permissions.CheckReadableInternalPath(filepath.Join(subSkill, "SKILL.md"), internal); !got.Allowed {
		t.Fatalf("discovered skill should be readable: %#v", got)
	}
}

func TestReadRejectsFractionalSemanticNumber(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	_, err := executor.Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_read_fractional_offset",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"sample.txt","offset":"1.5"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "input.offset must be integer") {
		t.Fatalf("err = %v, want integer schema error", err)
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

func TestWriteToolDiscoversNestedSkillDirs(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "feature", "note.txt")
	skillDir := filepath.Join(dir, "feature", ".claude", "skills", "feature-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: feature\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := fileToolContext(dir)
	_, err := fileExecutor(t).Execute(ctx, contracts.ToolUse{
		ID:    "toolu_write_nested_skill",
		Name:  "Write",
		Input: mustToolInput(t, writeInput{FilePath: "feature/note.txt", Content: "new\n"}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatal(err)
	}
	internal := tool.InternalPathContextFromMetadata(ctx.Metadata)
	if len(internal.SkillDirs) != 1 || internal.SkillDirs[0] != skillDir {
		t.Fatalf("skill dirs = %#v, want %q", internal.SkillDirs, skillDir)
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
		Input: json.RawMessage(`{"file_path":"dup.txt","old_string":"foo","new_string":"bar","replace_all":"true"}`),
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

func TestGlobToolTruncationMessage(t *testing.T) {
	dir := t.TempDir()
	mtime := time.Now().Add(-time.Hour)
	for i := 0; i <= defaultSearchLimit; i++ {
		rel := "f" + strconv.Itoa(1000 + i)[1:] + ".go"
		path := filepath.Join(dir, rel)
		if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	result, err := fileExecutor(t).Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_glob_truncated",
		Name:  "Glob",
		Input: json.RawMessage(`{"pattern":"*.go"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := make([]string, 0, defaultSearchLimit)
	for i := 0; i < defaultSearchLimit; i++ {
		wantFiles = append(wantFiles, "f"+strconv.Itoa(1000 + i)[1:]+".go")
	}
	wantLines := append(append([]string{}, wantFiles...), globTruncatedMessage)
	want := strings.Join(wantLines, "\n")
	if result.Content != want {
		t.Fatalf("glob truncated content = %#v", result.Content)
	}
	if result.StructuredContent["truncated"] != true {
		t.Fatalf("glob truncated structured content = %#v", result.StructuredContent)
	}
	files := result.StructuredContent["files"].([]string)
	if len(files) != defaultSearchLimit || files[0] != "f000.go" || files[len(files)-1] != "f099.go" {
		t.Fatalf("glob truncated files = %#v", files)
	}

	_, err = fileExecutor(t).Execute(fileToolContext(dir), contracts.ToolUse{
		ID:    "toolu_glob_limit_rejected",
		Name:  "Glob",
		Input: json.RawMessage(`{"pattern":"*.go","limit":2}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "input.limit is not allowed") {
		t.Fatalf("glob limit err = %v", err)
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
	if grepResult.Content != "Found 2 files\nsrc/a.go\nsrc/b.txt" {
		t.Fatalf("grep path output = %#v", grepResult.Content)
	}

	grepFileResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_file_path",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","path":"src/a.go"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if grepFileResult.Content != "Found 1 file\nsrc/a.go" {
		t.Fatalf("grep file path output = %#v", grepFileResult.Content)
	}
}

func TestGlobAndGrepValidatePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("Needle go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	_, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_glob_missing_path",
		Name:  "Glob",
		Input: json.RawMessage(`{"pattern":"*.go","path":"missing"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "Directory does not exist: missing") || !strings.Contains(err.Error(), "Note: your current working directory is") {
		t.Fatalf("glob missing path err = %v", err)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_glob_file_path",
		Name:  "Glob",
		Input: json.RawMessage(`{"pattern":"*.go","path":"src/a.go"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "Path is not a directory: src/a.go") {
		t.Fatalf("glob file path err = %v", err)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_missing_path",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","path":"missing"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "Path does not exist: missing") || !strings.Contains(err.Error(), "Note: your current working directory is") {
		t.Fatalf("grep missing path err = %v", err)
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
	mtime := time.Now().Add(-time.Hour)
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
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
	if filesResult.Content != "Found 2 files\nsrc/a.go\nsrc/c.go" {
		t.Fatalf("files result = %#v", filesResult.Content)
	}
	if filesResult.StructuredContent["files_with_matches"] != true {
		t.Fatalf("files result structured content = %#v", filesResult.StructuredContent)
	}

	filesWithFlagResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_files_with_matches_flag",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.go","--files-with-matches":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filesWithFlagResult.Content != "Found 2 files\nsrc/a.go\nsrc/c.go" ||
		filesWithFlagResult.StructuredContent["output_mode"] != "files_with_matches" ||
		filesWithFlagResult.StructuredContent["files_with_matches"] != true {
		t.Fatalf("files-with-matches flag result = %#v", filesWithFlagResult)
	}

	shortFilesWithResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_files_with_matches_short",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.go","-l":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortFilesWithResult.Content != "Found 2 files\nsrc/a.go\nsrc/c.go" ||
		shortFilesWithResult.StructuredContent["files_with_matches"] != true {
		t.Fatalf("short files-with-matches result = %#v", shortFilesWithResult)
	}

	singularFilesWithResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_files_with_match_mode",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.go","output_mode":"files_with_match"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if singularFilesWithResult.Content != "Found 2 files\nsrc/a.go\nsrc/c.go" ||
		singularFilesWithResult.StructuredContent["output_mode"] != "files_with_matches" {
		t.Fatalf("singular files-with-match result = %#v", singularFilesWithResult)
	}

	filesPagedResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_files_paged",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","head_limit":2}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filesPagedResult.Content != "Found 2 files limit: 2\nsrc/a.go\nsrc/b.txt" || filesPagedResult.StructuredContent["truncated"] != true {
		t.Fatalf("files paged result = %#v", filesPagedResult)
	}

	filesOffsetResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_files_offset",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","offset":1,"head_limit":2}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filesOffsetResult.Content != "Found 2 files offset: 1\nsrc/b.txt\nsrc/c.go" || filesOffsetResult.StructuredContent["offset"] != 1 {
		t.Fatalf("files offset result = %#v", filesOffsetResult)
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

	regexpAliasResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_regexp_alias",
		Name:  "Grep",
		Input: json.RawMessage(`{"--regexp":"Alpha","glob":"**/*.go","output_mode":"content"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if regexpAliasResult.Content != "src/a.go:2:func Alpha() {}\nsrc/c.go:2:func AlphaBeta() {}" ||
		regexpAliasResult.StructuredContent["pattern"] != "Alpha" {
		t.Fatalf("regexp alias result = %#v", regexpAliasResult)
	}

	regexAliasResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_regex_alias",
		Name:  "Grep",
		Input: json.RawMessage(`{"regex":"Beta","glob":"**/*.go"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if regexAliasResult.Content != "Found 1 file\nsrc/c.go" ||
		regexAliasResult.StructuredContent["pattern"] != "Beta" {
		t.Fatalf("regex alias result = %#v", regexAliasResult)
	}

	shortRegexpAliasResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_short_regexp_alias",
		Name:  "Grep",
		Input: json.RawMessage(`{"-e":"Alpha","glob":"**/*.go","output_mode":"count"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantRegexpAliasCount := "src/a.go:1\nsrc/c.go:1\n\nFound 2 total occurrences across 2 files."
	if shortRegexpAliasResult.Content != wantRegexpAliasCount ||
		shortRegexpAliasResult.StructuredContent["pattern"] != "Alpha" {
		t.Fatalf("short regexp alias result = %#v", shortRegexpAliasResult)
	}

	countResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"func","glob":"**/*.go","output_mode":"count"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCount := "src/a.go:1\nsrc/c.go:2\n\nFound 3 total occurrences across 2 files."
	if countResult.Content != wantCount {
		t.Fatalf("count result = %#v", countResult.Content)
	}

	longCountResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_count_long_flag",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"func","glob":"**/*.go","--count":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longCountResult.Content != wantCount ||
		longCountResult.StructuredContent["output_mode"] != "count" ||
		longCountResult.StructuredContent["count"] != true ||
		longCountResult.StructuredContent["count_matches"] != false {
		t.Fatalf("long count flag result = %#v", longCountResult)
	}

	shortCountResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_count_short_flag",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"func","glob":"**/*.go","-c":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortCountResult.Content != wantCount ||
		shortCountResult.StructuredContent["output_mode"] != "count" ||
		shortCountResult.StructuredContent["count"] != true {
		t.Fatalf("short count flag result = %#v", shortCountResult)
	}

	if err := os.WriteFile(filepath.Join(dir, "src", "multi.txt"), []byte("func func\nfunc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	countMatchesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_count_matches",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"func","glob":"**/multi.txt","output_mode":"count","--count-matches":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCountMatches := "src/multi.txt:3\n\nFound 3 total occurrences across 1 file."
	if countMatchesResult.Content != wantCountMatches || countMatchesResult.StructuredContent["count_matches"] != true {
		t.Fatalf("count-matches result = %#v", countMatchesResult)
	}

	quotedCountMatchesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_count_matches_quoted",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"func","glob":"**/multi.txt","outputMode":"count","countMatches":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if quotedCountMatchesResult.Content != wantCountMatches || quotedCountMatchesResult.StructuredContent["count_matches"] != true {
		t.Fatalf("quoted countMatches result = %#v", quotedCountMatchesResult)
	}

	camelModeResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_camel_output_mode",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"func","glob":"**/*.go","outputMode":"count"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if camelModeResult.Content != wantCount || camelModeResult.StructuredContent["output_mode"] != "count" {
		t.Fatalf("camel outputMode result = %#v", camelModeResult)
	}

	if err := os.WriteFile(filepath.Join(dir, "src", "noalpha.go"), []byte("package main\nfunc Gamma() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	filesWithoutResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_files_without_match",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.go","output_mode":"files_without_match"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filesWithoutResult.Content != "Found 1 file\nsrc/noalpha.go" ||
		filesWithoutResult.StructuredContent["output_mode"] != "files_without_matches" ||
		filesWithoutResult.StructuredContent["files_without_match"] != true {
		t.Fatalf("files-without-match result = %#v", filesWithoutResult)
	}

	shortFilesWithoutResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_files_without_match_short",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.go","-L":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortFilesWithoutResult.Content != "Found 1 file\nsrc/noalpha.go" ||
		shortFilesWithoutResult.StructuredContent["files_without_match"] != true {
		t.Fatalf("short files-without-match result = %#v", shortFilesWithoutResult)
	}

	multiGlobResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_multi_glob",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.go,**/*.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if multiGlobResult.Content != "Found 3 files\nsrc/a.go\nsrc/b.txt\nsrc/c.go" {
		t.Fatalf("multi glob result = %#v", multiGlobResult.Content)
	}

	longGlobResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_long_glob",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","--glob":"**/*.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longGlobResult.Content != "Found 1 file\nsrc/b.txt" ||
		longGlobResult.StructuredContent["glob"] != "**/*.txt" {
		t.Fatalf("long glob result = %#v", longGlobResult)
	}

	shortGlobResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_short_glob",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","-g":"**/*.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortGlobResult.Content != "Found 1 file\nsrc/b.txt" ||
		shortGlobResult.StructuredContent["glob"] != "**/*.txt" {
		t.Fatalf("short glob result = %#v", shortGlobResult)
	}

	braceGlobResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_brace_glob",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.{go,txt}"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if braceGlobResult.Content != "Found 3 files\nsrc/a.go\nsrc/b.txt\nsrc/c.go" {
		t.Fatalf("brace glob result = %#v", braceGlobResult.Content)
	}

	negatedGlobResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_negated_glob",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"**/*.{go,txt},!**/c.go"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if negatedGlobResult.Content != "Found 2 files\nsrc/a.go\nsrc/b.txt" {
		t.Fatalf("negated glob result = %#v", negatedGlobResult.Content)
	}

	onlyNegatedGlobResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_only_negated_glob",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","glob":"!**/*.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if onlyNegatedGlobResult.Content != "Found 2 files\nsrc/a.go\nsrc/c.go" {
		t.Fatalf("only negated glob result = %#v", onlyNegatedGlobResult.Content)
	}
}

func TestGrepToolFilesMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"src/a.go":      []byte("Alpha go\n"),
		"src/b.TXT":     []byte("Alpha text\n"),
		"src/image.png": []byte{0, 1, 2, 3},
		".hidden.txt":   []byte("Alpha hidden\n"),
		"ignored.txt":   []byte("Alpha ignored\n"),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	filesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_files_mode",
		Name:  "Grep",
		Input: json.RawMessage(`{"--files":true,"--no-hidden":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filesResult.Content != "Found 2 files\nsrc/a.go\nsrc/b.TXT" ||
		filesResult.StructuredContent["output_mode"] != "files" ||
		filesResult.StructuredContent["files"] != true ||
		filesResult.StructuredContent["pattern"] != "" {
		t.Fatalf("files mode result = %#v", filesResult)
	}

	outputModeResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_output_mode_files",
		Name:  "Grep",
		Input: json.RawMessage(`{"output_mode":"files","type":"txt","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if outputModeResult.Content != "Found 2 files\n.hidden.txt\nsrc/b.TXT" ||
		outputModeResult.StructuredContent["files"] != true ||
		outputModeResult.StructuredContent["type_filter"] != "txt" {
		t.Fatalf("output_mode files result = %#v", outputModeResult)
	}

	textBinaryResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_files_text_binary",
		Name:  "Grep",
		Input: json.RawMessage(`{"files":true,"--text":true,"iglob":"*.png","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if textBinaryResult.Content != "Found 1 file\nsrc/image.png" ||
		textBinaryResult.StructuredContent["text"] != true ||
		textBinaryResult.StructuredContent["iglob"] != "*.png" {
		t.Fatalf("files text binary result = %#v", textBinaryResult)
	}
}

func TestGrepToolMaxDepth(t *testing.T) {
	dir := t.TempDir()
	for _, subdir := range []string{
		filepath.Join(dir, "one", "two"),
		filepath.Join(dir, "skip"),
	} {
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"root.txt":          "Needle root\n",
		"one/one.txt":       "Needle one\n",
		"one/two/two.txt":   "Needle two\n",
		"skip/ignored.txt":  "no match\n",
		"skip/matched.txt":  "Needle skip\n",
		"skip/nested.txt":   "Needle nested\n",
		"skip/another.json": "Needle json\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	filesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_max_depth_files",
		Name:  "Grep",
		Input: json.RawMessage(`{"--files":true,"--max-depth":"2","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := "Found 6 files\none/one.txt\nroot.txt\nskip/another.json\nskip/ignored.txt\nskip/matched.txt\nskip/nested.txt"
	if filesResult.Content != wantFiles ||
		filesResult.StructuredContent["max_depth"] != 2 {
		t.Fatalf("max-depth files result = %#v", filesResult)
	}

	matchResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_short_max_depth",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","-d":1,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if matchResult.Content != "Found 1 file\nroot.txt" ||
		matchResult.StructuredContent["max_depth"] != 1 {
		t.Fatalf("short max-depth result = %#v", matchResult)
	}

	zeroResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_zero_max_depth",
		Name:  "Grep",
		Input: json.RawMessage(`{"--files":true,"max_depth":0}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if zeroResult.Content != "No files found" ||
		zeroResult.StructuredContent["max_depth"] != 0 ||
		zeroResult.StructuredContent["total_matches"] != 0 {
		t.Fatalf("zero max-depth result = %#v", zeroResult)
	}
}

func TestGrepToolFilesWithMatchesSortsByModifiedTime(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-4 * time.Hour)
	files := map[string]time.Time{
		"old.txt":   base,
		"new.txt":   base.Add(3 * time.Hour),
		"tie-a.txt": base.Add(2 * time.Hour),
		"tie-b.txt": base.Add(2 * time.Hour),
	}
	for name, mtime := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("Needle\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_files_mtime_sort",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "Found 4 files\nnew.txt\ntie-a.txt\ntie-b.txt\nold.txt"
	if result.Content != want {
		t.Fatalf("mtime-sorted files result = %#v", result.Content)
	}
}

func TestGrepToolSortAliases(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-4 * time.Hour)
	files := map[string]time.Time{
		"a.txt": base,
		"m.txt": base.Add(2 * time.Hour),
		"z.txt": base.Add(3 * time.Hour),
	}
	for name, mtime := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("Needle\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	pathResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_sort_path",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pathResult.Content != "Found 3 files\na.txt\nm.txt\nz.txt" ||
		pathResult.StructuredContent["sort"] != "path" ||
		pathResult.StructuredContent["sort_reverse"] != false ||
		pathResult.StructuredContent["sort_explicit"] != true {
		t.Fatalf("path sort result = %#v", pathResult)
	}

	sortFilesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_sort_files",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--sort-files":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sortFilesResult.Content != "Found 3 files\na.txt\nm.txt\nz.txt" ||
		sortFilesResult.StructuredContent["sort"] != "path" ||
		sortFilesResult.StructuredContent["sort_files"] != true ||
		sortFilesResult.StructuredContent["sort_explicit"] != true {
		t.Fatalf("sort-files result = %#v", sortFilesResult)
	}

	reversePathResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_sort_reverse_path",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--sortr":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reversePathResult.Content != "Found 3 files\nz.txt\nm.txt\na.txt" ||
		reversePathResult.StructuredContent["sort"] != "path" ||
		reversePathResult.StructuredContent["sort_reverse"] != true {
		t.Fatalf("reverse path sort result = %#v", reversePathResult)
	}

	modifiedCountResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_sort_modified_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"count","sort":"modified"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantModifiedCount := "a.txt:1\nm.txt:1\nz.txt:1\n\nFound 3 total occurrences across 3 files."
	if modifiedCountResult.Content != wantModifiedCount ||
		modifiedCountResult.StructuredContent["sort"] != "modified" ||
		modifiedCountResult.StructuredContent["sort_reverse"] != false {
		t.Fatalf("modified count sort result = %#v", modifiedCountResult)
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

	contextPrecedenceResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_context_precedence",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","context":1,"before_context":0,"after_context":0}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if contextPrecedenceResult.Content != wantContext {
		t.Fatalf("context precedence content = %#v", contextPrecedenceResult.Content)
	}

	shortContextPrecedenceResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_short_context_precedence",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","-C":1,"-B":0,"-A":0}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortContextPrecedenceResult.Content != wantContext {
		t.Fatalf("short context precedence content = %#v", shortContextPrecedenceResult.Content)
	}

	noLineNumberResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_line_numbers",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","-n":false,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoLineNumber := "a.txt:Needle first\n\n[Showing results with pagination = limit: 1]"
	if noLineNumberResult.Content != wantNoLineNumber || noLineNumberResult.StructuredContent["line_numbers"] != false {
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
	if snakeLineNumberResult.Content != wantNoLineNumber || snakeLineNumberResult.StructuredContent["line_numbers"] != false {
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
	if camelLineNumberResult.Content != wantNoLineNumber || camelLineNumberResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("lineNumbers alias result = %#v", camelLineNumberResult)
	}

	longLineNumberResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_line_number_long_alias",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--line-number":"false","head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longLineNumberResult.Content != wantNoLineNumber || longLineNumberResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("long line-number alias result = %#v", longLineNumberResult)
	}

	longNoLineNumberResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_line_number_long_alias",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--no-line-number":true,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longNoLineNumberResult.Content != wantNoLineNumber || longNoLineNumberResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("long no-line-number alias result = %#v", longNoLineNumberResult)
	}

	shortNoLineNumberResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_line_number_short_alias",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","-N":"true","head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortNoLineNumberResult.Content != wantNoLineNumber || shortNoLineNumberResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("short no-line-number alias result = %#v", shortNoLineNumberResult)
	}

	noLineNumberOverrideResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_line_number_override",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","line_numbers":true,"no_line_numbers":true,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if noLineNumberOverrideResult.Content != wantNoLineNumber || noLineNumberOverrideResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("no-line-number override result = %#v", noLineNumberOverrideResult)
	}

	shortContextResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_short_context",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","-B":1,"-A":0}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantShortContext := "a.txt-1-one\na.txt:2:Needle first\n--\na.txt-4-four\na.txt:5:Needle second"
	if shortContextResult.Content != wantShortContext {
		t.Fatalf("short context content = %#v", shortContextResult.Content)
	}
	if shortContextResult.StructuredContent["before_context"] != 1 || shortContextResult.StructuredContent["after_context"] != 0 {
		t.Fatalf("short context structured content = %#v", shortContextResult.StructuredContent)
	}

	passthruResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_passthru",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--passthru":true,"context":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if passthruResult.Content != wantContext ||
		passthruResult.StructuredContent["passthru"] != true ||
		passthruResult.StructuredContent["before_context"] != 0 ||
		passthruResult.StructuredContent["after_context"] != 0 {
		t.Fatalf("passthru result = %#v", passthruResult)
	}
	passthruMatches := passthruResult.StructuredContent["matches"].([]map[string]any)
	if len(passthruMatches) != 6 || passthruMatches[0]["matched"] != false || passthruMatches[1]["matched"] != true {
		t.Fatalf("passthru structured matches = %#v", passthruMatches)
	}

	passthroughResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_passthrough_alias",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","passthrough":"true","head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantPassthrough := "a.txt-1-one\n\n[Showing results with pagination = limit: 1]"
	if passthroughResult.Content != wantPassthrough || passthroughResult.StructuredContent["passthru"] != true {
		t.Fatalf("passthrough alias result = %#v", passthroughResult)
	}

	pagedResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_paged",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","offset":1,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantPaged := "a.txt:5:Needle second\n\n[Showing results with pagination = offset: 1]"
	if pagedResult.Content != wantPaged {
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
	wantShortMaxCount := "a.txt:1\n\nFound 1 total occurrence across 1 file."
	if shortMaxCountResult.Content != wantShortMaxCount || shortMaxCountResult.StructuredContent["max_count"] != 1 {
		t.Fatalf("short max_count result = %#v", shortMaxCountResult)
	}

	semanticStringResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_semantic_string_inputs",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","max_count":"1.0","context":"1.0","-n":"false"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantSemanticString := "a.txt-one\na.txt:Needle first\na.txt-three"
	if semanticStringResult.Content != wantSemanticString ||
		semanticStringResult.StructuredContent["max_count"] != 1 ||
		semanticStringResult.StructuredContent["before_context"] != 1 ||
		semanticStringResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("semantic string input result = %#v", semanticStringResult)
	}
}

func TestGrepToolColumnNumbers(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"before",
		"xx Needle here",
		"after",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_column",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--column":true,"before_context":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "a.txt-1-before\na.txt:2:4:xx Needle here"
	if result.Content != want ||
		result.StructuredContent["column_numbers"] != true {
		t.Fatalf("column result = %#v", result)
	}
	matches := result.StructuredContent["matches"].([]map[string]any)
	if len(matches) != 2 || matches[0]["column"] != nil || matches[1]["column"] != 4 {
		t.Fatalf("column structured matches = %#v", matches)
	}

	semanticResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_column_semantic",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","column":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if semanticResult.Content != "a.txt:2:4:xx Needle here" ||
		semanticResult.StructuredContent["column_numbers"] != true {
		t.Fatalf("semantic column result = %#v", semanticResult)
	}
}

func TestGrepToolVimgrep(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"before",
		"Needle Needle",
		"after",
		"Needle",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "hits.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_vimgrep",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--vimgrep":"true","context":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "hits.txt-1-before\nhits.txt:2:1:Needle Needle\nhits.txt:2:8:Needle Needle\nhits.txt-3-after\nhits.txt:4:1:Needle"
	if result.Content != want || result.StructuredContent["vimgrep"] != true {
		t.Fatalf("vimgrep result = %#v", result)
	}
	matches := result.StructuredContent["matches"].([]map[string]any)
	if len(matches) != 5 || matches[1]["column"] != 1 || matches[2]["column"] != 8 || matches[3]["matched"] != false {
		t.Fatalf("vimgrep structured matches = %#v", matches)
	}

	noLineResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_vimgrep_no_line",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","vimgrep":true,"-N":true,"head_limit":2}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoLine := "hits.txt:1:Needle Needle\nhits.txt:8:Needle Needle\n\n[Showing results with pagination = limit: 2]"
	if noLineResult.Content != wantNoLine || noLineResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("vimgrep no-line result = %#v", noLineResult)
	}

	onlyResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_vimgrep_only_matching",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--vimgrep":true,"only_matching":true,"head_limit":2}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantOnly := "hits.txt:2:1:Needle\nhits.txt:2:8:Needle\n\n[Showing results with pagination = limit: 2]"
	if onlyResult.Content != wantOnly || onlyResult.StructuredContent["vimgrep"] != true || onlyResult.StructuredContent["only_matching"] != true {
		t.Fatalf("vimgrep only-matching result = %#v", onlyResult)
	}
}

func TestGrepToolFilenameControls(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("before\nNeedle Needle\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("Needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	contentResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_filename_content",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","glob":"a.txt","output_mode":"content","--no-filename":"true","context":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantContent := "1-before\n2:Needle Needle\n3-after"
	if contentResult.Content != wantContent ||
		contentResult.StructuredContent["with_filename"] != false ||
		contentResult.StructuredContent["no_filename"] != true {
		t.Fatalf("no-filename content result = %#v", contentResult)
	}

	countResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_filename_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"count","no_filename":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCount := "1\n1\n\nFound 2 total occurrences across 2 files."
	if countResult.Content != wantCount || countResult.StructuredContent["with_filename"] != false {
		t.Fatalf("no-filename count result = %#v", countResult)
	}

	vimgrepResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_filename_vimgrep",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","glob":"a.txt","output_mode":"content","-I":true,"-N":true,"--vimgrep":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantVimgrep := "1:Needle Needle\n8:Needle Needle"
	if vimgrepResult.Content != wantVimgrep ||
		vimgrepResult.StructuredContent["with_filename"] != false ||
		vimgrepResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("no-filename vimgrep result = %#v", vimgrepResult)
	}

	filesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_filename_files",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--files-with-matches":true,"--no-filename":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filesResult.Content != "Found 2 files\na.txt\nb.txt" || filesResult.StructuredContent["with_filename"] != true {
		t.Fatalf("no-filename files result = %#v", filesResult)
	}

	withResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_with_filename",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","glob":"a.txt","output_mode":"content","-H":true,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantWith := "a.txt:2:Needle Needle"
	if withResult.Content != wantWith || withResult.StructuredContent["with_filename"] != true {
		t.Fatalf("with-filename result = %#v", withResult)
	}
}

func TestGrepToolHeading(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("Needle one\nskip\nNeedle two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("before\nNeedle beta\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	headingResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_heading",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--heading":"true","context":1,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantHeading := "a.txt\n1:Needle one\n2-skip\n3:Needle two\n\nb.txt\n1-before\n2:Needle beta\n3-after"
	if headingResult.Content != wantHeading || headingResult.StructuredContent["heading"] != true {
		t.Fatalf("heading result = %#v", headingResult)
	}

	noFilenameResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_heading_no_filename",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","heading":true,"--no-filename":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoFilename := "1:Needle one\n3:Needle two\n\n2:Needle beta"
	if noFilenameResult.Content != wantNoFilename ||
		noFilenameResult.StructuredContent["heading"] != true ||
		noFilenameResult.StructuredContent["with_filename"] != false {
		t.Fatalf("heading no-filename result = %#v", noFilenameResult)
	}

	noHeadingResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_heading",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","heading":true,"no-heading":"true","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoHeading := "a.txt:1:Needle one\na.txt:3:Needle two\nb.txt:2:Needle beta"
	if noHeadingResult.Content != wantNoHeading || noHeadingResult.StructuredContent["heading"] != false {
		t.Fatalf("no-heading result = %#v", noHeadingResult)
	}

	vimgrepResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_heading_vimgrep_ignored",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","heading":true,"--vimgrep":true,"sort":"path","head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantVimgrep := "a.txt:1:1:Needle one\n\n[Showing results with pagination = limit: 1]"
	if vimgrepResult.Content != wantVimgrep ||
		vimgrepResult.StructuredContent["heading"] != false ||
		vimgrepResult.StructuredContent["vimgrep"] != true {
		t.Fatalf("heading vimgrep result = %#v", vimgrepResult)
	}
}

func TestGrepToolPathSeparator(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src", "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "pkg", "a.txt"), []byte("Needle alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "other", "b.txt"), []byte("Needle beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	contentResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_path_separator_content",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--path-separator":"\\","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantContent := "src\\other\\b.txt:1:Needle beta\nsrc\\pkg\\a.txt:1:Needle alpha"
	if contentResult.Content != wantContent || contentResult.StructuredContent["path_separator"] != `\` {
		t.Fatalf("path-separator content result = %#v", contentResult)
	}

	headingResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_path_separator_heading",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","heading":true,"pathSeparator":"\\","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantHeading := "src\\other\\b.txt\n1:Needle beta\n\nsrc\\pkg\\a.txt\n1:Needle alpha"
	if headingResult.Content != wantHeading {
		t.Fatalf("path-separator heading result = %#v", headingResult)
	}

	countResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_path_separator_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"count","path-separator":"\\","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCount := "src\\other\\b.txt:1\nsrc\\pkg\\a.txt:1\n\nFound 2 total occurrences across 2 files."
	if countResult.Content != wantCount {
		t.Fatalf("path-separator count result = %#v", countResult)
	}

	filesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_path_separator_files",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--files-with-matches":true,"path_separator":"\\","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := "Found 2 files\nsrc\\other\\b.txt\nsrc\\pkg\\a.txt"
	if filesResult.Content != wantFiles {
		t.Fatalf("path-separator files result = %#v", filesResult)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_bad_path_separator",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","path-separator":"::"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "path_separator must be exactly one byte") {
		t.Fatalf("path_separator validation err = %v", err)
	}
}

func TestGrepToolNullPathSeparator(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "pkg", "a.txt"), []byte("Needle alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "b.txt"), []byte("before\nNeedle beta\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	contentResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_null_content",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--null":"true","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantContent := "src/b.txt\x002:Needle beta\nsrc/pkg/a.txt\x001:Needle alpha"
	if contentResult.Content != wantContent || contentResult.StructuredContent["null"] != true {
		t.Fatalf("null content result = %#v", contentResult)
	}

	contextResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_null_context",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","null":true,"context":1,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantContext := "src/b.txt\x001-before\nsrc/b.txt\x002:Needle beta\nsrc/b.txt\x003-after\n--\nsrc/pkg/a.txt\x001:Needle alpha"
	if contextResult.Content != wantContext {
		t.Fatalf("null context result = %#v", contextResult)
	}

	headingResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_null_heading",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","heading":true,"-0":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantHeading := "src/b.txt\x002:Needle beta\n\nsrc/pkg/a.txt\x001:Needle alpha"
	if headingResult.Content != wantHeading || headingResult.StructuredContent["null"] != true {
		t.Fatalf("null heading result = %#v", headingResult)
	}

	countResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_null_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"count","--null":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCount := "src/b.txt\x001\nsrc/pkg/a.txt\x001\n\nFound 2 total occurrences across 2 files."
	if countResult.Content != wantCount {
		t.Fatalf("null count result = %#v", countResult)
	}

	filesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_null_files",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--files-with-matches":true,"--null":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := "Found 2 files\nsrc/b.txt\x00src/pkg/a.txt\x00"
	if filesResult.Content != wantFiles {
		t.Fatalf("null files result = %#v", filesResult)
	}

	noFilenameResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_null_no_filename",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--null":true,"--no-filename":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if noFilenameResult.Content != "2:Needle beta\n1:Needle alpha" {
		t.Fatalf("null no-filename result = %#v", noFilenameResult)
	}
}

func TestGrepToolFieldSeparators(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("before\nNeedle here\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("Needle other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	contextResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_field_separators_context",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","field-match-separator":"::","field_context_separator":"~~","context":1,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantContext := "a.txt~~1~~before\na.txt::2::Needle here\na.txt~~3~~after\n--\nb.txt::1::Needle other"
	if contextResult.Content != wantContext ||
		contextResult.StructuredContent["field_match_separator"] != "::" ||
		contextResult.StructuredContent["field_context_separator"] != "~~" {
		t.Fatalf("field separator context result = %#v", contextResult)
	}

	columnResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_field_separators_column",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--field-match-separator":"::","--field-context-separator":"~~","column":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantColumn := "a.txt::2::1::Needle here\nb.txt::1::1::Needle other"
	if columnResult.Content != wantColumn {
		t.Fatalf("field separator column result = %#v", columnResult)
	}

	nullResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_field_separators_null",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--null":true,"fieldMatchSeparator":"::","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNull := "a.txt\x002::Needle here\nb.txt\x001::Needle other"
	if nullResult.Content != wantNull {
		t.Fatalf("field separator null result = %#v", nullResult)
	}

	emptyResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_empty_field_separator",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","field_match_separator":"","sort":"path","glob":"a.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if emptyResult.Content != "a.txt2Needle here" ||
		emptyResult.StructuredContent["field_match_separator"] != "" {
		t.Fatalf("empty field separator result = %#v", emptyResult)
	}
}

func TestGrepToolContextSeparators(t *testing.T) {
	dir := t.TempDir()
	aContent := strings.Join([]string{
		"before1",
		"Needle one",
		"after1",
		"gap",
		"before2",
		"Needle two",
		"after2",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(aContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("pre\nNeedle b\npost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	defaultResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_default_context_separator",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","context":1,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantDefault := "a.txt-1-before1\na.txt:2:Needle one\na.txt-3-after1\n--\na.txt-5-before2\na.txt:6:Needle two\na.txt-7-after2\n--\nb.txt-1-pre\nb.txt:2:Needle b\nb.txt-3-post"
	if defaultResult.Content != wantDefault ||
		defaultResult.StructuredContent["context_separator"] != "--" ||
		defaultResult.StructuredContent["no_context_separator"] != false {
		t.Fatalf("default context separator result = %#v", defaultResult)
	}

	customResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_custom_context_separator",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","context":1,"context-separator":"==","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCustom := strings.ReplaceAll(wantDefault, "\n--\n", "\n==\n")
	if customResult.Content != wantCustom ||
		customResult.StructuredContent["context_separator"] != "==" {
		t.Fatalf("custom context separator result = %#v", customResult)
	}

	noSeparatorResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_context_separator",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","context":1,"--no-context-separator":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoSeparator := strings.ReplaceAll(wantDefault, "\n--\n", "\n")
	if noSeparatorResult.Content != wantNoSeparator ||
		noSeparatorResult.StructuredContent["no_context_separator"] != true {
		t.Fatalf("no context separator result = %#v", noSeparatorResult)
	}

	headingResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_heading_context_separator",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","heading":true,"context":1,"context_separator":"==","glob":"a.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantHeading := "a.txt\n1-before1\n2:Needle one\n3-after1\n==\n5-before2\n6:Needle two\n7-after2"
	if headingResult.Content != wantHeading {
		t.Fatalf("heading context separator result = %#v", headingResult)
	}

	noContextResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_context_separator_without_context",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--context-separator":"==","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoContext := "a.txt:2:Needle one\na.txt:6:Needle two\nb.txt:2:Needle b"
	if noContextResult.Content != wantNoContext ||
		noContextResult.StructuredContent["context_separator"] != "==" {
		t.Fatalf("context separator without context result = %#v", noContextResult)
	}
}

func TestGrepToolByteOffset(t *testing.T) {
	dir := t.TempDir()
	aContent := strings.Join([]string{
		"alpha",
		"xx Needle here",
		"plain",
		"Needle again",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(aContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("pre\nNeedle b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_byte_offset",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","byte-offset":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "a.txt:2:6:xx Needle here\na.txt:4:27:Needle again\nb.txt:2:4:Needle b"
	if result.Content != want ||
		result.StructuredContent["byte_offset"] != true {
		t.Fatalf("byte offset result = %#v", result)
	}
	matches := result.StructuredContent["matches"].([]map[string]any)
	if len(matches) != 3 || matches[0]["byte_offset"] != 6 || matches[1]["byte_offset"] != 27 || matches[2]["byte_offset"] != 4 {
		t.Fatalf("byte offset structured matches = %#v", matches)
	}

	contextResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_byte_offset_context",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","-b":"true","context":1,"field-match-separator":"::","field-context-separator":"~~","glob":"a.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantContext := "a.txt~~1~~0~~alpha\na.txt::2::6::xx Needle here\na.txt~~3~~21~~plain\na.txt::4::27::Needle again"
	if contextResult.Content != wantContext ||
		contextResult.StructuredContent["byte_offset"] != true {
		t.Fatalf("byte offset context result = %#v", contextResult)
	}

	onlyMatchingResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_byte_offset_only_matching",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","only_matching":true,"column":true,"byte_offset":true,"glob":"a.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantOnlyMatching := "a.txt:2:4:9:Needle\na.txt:4:1:27:Needle"
	if onlyMatchingResult.Content != wantOnlyMatching {
		t.Fatalf("byte offset only-matching result = %#v", onlyMatchingResult)
	}

	vimgrepResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_byte_offset_vimgrep",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--vimgrep":true,"--byte-offset":true,"glob":"a.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantVimgrep := "a.txt:2:4:9:xx Needle here\na.txt:4:1:27:Needle again"
	if vimgrepResult.Content != wantVimgrep {
		t.Fatalf("byte offset vimgrep result = %#v", vimgrepResult)
	}

	nullResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_byte_offset_null",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--byte-offset":true,"--null":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNull := "a.txt\x002:6:xx Needle here\na.txt\x004:27:Needle again\nb.txt\x002:4:Needle b"
	if nullResult.Content != wantNull {
		t.Fatalf("byte offset null result = %#v", nullResult)
	}
}

func TestGrepToolIncludeZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("Needle Needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	countResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_include_zero_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"count","include_zero":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCount := "a.txt:1\nb.txt:0\n\nFound 1 total occurrence across 2 files."
	if countResult.Content != wantCount || countResult.StructuredContent["include_zero"] != true {
		t.Fatalf("include-zero count result = %#v", countResult)
	}

	countMatchesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_include_zero_count_matches",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"count","count_matches":true,"--include-zero":"true","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCountMatches := "a.txt:2\nb.txt:0\n\nFound 2 total occurrences across 2 files."
	if countMatchesResult.Content != wantCountMatches ||
		countMatchesResult.StructuredContent["count_matches"] != true ||
		countMatchesResult.StructuredContent["include_zero"] != true {
		t.Fatalf("include-zero count-matches result = %#v", countMatchesResult)
	}

	noFilenameResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_include_zero_no_filename",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"count","include-zero":true,"--no-filename":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoFilename := "1\n0\n\nFound 1 total occurrence across 2 files."
	if noFilenameResult.Content != wantNoFilename ||
		noFilenameResult.StructuredContent["with_filename"] != false ||
		noFilenameResult.StructuredContent["include_zero"] != true {
		t.Fatalf("include-zero no-filename result = %#v", noFilenameResult)
	}

	contentResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_include_zero_content_ignored",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--include-zero":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if contentResult.Content != "a.txt:1:Needle Needle" || contentResult.StructuredContent["include_zero"] != false {
		t.Fatalf("include-zero content result = %#v", contentResult)
	}
}

func TestGrepToolTrim(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"  Needle first",
		"\tcontext line",
		"  xNeedle second",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "trim.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokens.txt"), []byte("  ID-1\nxx ID-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_trim",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","glob":"trim.txt","output_mode":"content","--trim":true,"before_context":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "trim.txt:1:Needle first\ntrim.txt-2-context line\ntrim.txt:3:xNeedle second"
	if result.Content != want || result.StructuredContent["trim"] != true {
		t.Fatalf("trim result = %#v", result)
	}
	matches := result.StructuredContent["matches"].([]map[string]any)
	if len(matches) != 3 || matches[0]["text"] != "Needle first" || matches[1]["text"] != "context line" {
		t.Fatalf("trim structured matches = %#v", matches)
	}

	noTrimResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_trim",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","glob":"trim.txt","output_mode":"content","trim":true,"--no-trim":"true","head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoTrim := "trim.txt:1:  Needle first\n\n[Showing results with pagination = limit: 1]"
	if noTrimResult.Content != wantNoTrim || noTrimResult.StructuredContent["trim"] != false {
		t.Fatalf("no-trim result = %#v", noTrimResult)
	}

	columnResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_trim_column",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","glob":"trim.txt","output_mode":"content","--trim":"true","--column":true,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantColumn := "trim.txt:1:3:Needle first\n\n[Showing results with pagination = limit: 1]"
	if columnResult.Content != wantColumn || columnResult.StructuredContent["trim"] != true {
		t.Fatalf("trim column result = %#v", columnResult)
	}

	onlyResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_trim_only_matching",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":" ID-[0-9]+","glob":"tokens.txt","output_mode":"content","only_matching":true,"--trim":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantOnly := "tokens.txt:1:ID-1\ntokens.txt:2:ID-2"
	if onlyResult.Content != wantOnly || onlyResult.StructuredContent["trim"] != true {
		t.Fatalf("trim only-matching result = %#v", onlyResult)
	}
}

func TestGrepToolTextSearchesBinaryExtensionFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "payload.bin"), []byte("Needle\x00inside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	defaultResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_binary_default",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if defaultResult.Content != "No files found" || defaultResult.StructuredContent["text"] != false {
		t.Fatalf("default binary result = %#v", defaultResult)
	}

	textResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_binary_text",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--text":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if textResult.Content != "Found 1 file\npayload.bin" || textResult.StructuredContent["text"] != true {
		t.Fatalf("text binary result = %#v", textResult)
	}

	shortTextResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_binary_short_text",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"count","-a":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantShortText := "payload.bin:1\n\nFound 1 total occurrence across 1 file."
	if shortTextResult.Content != wantShortText || shortTextResult.StructuredContent["text"] != true {
		t.Fatalf("short text binary result = %#v", shortTextResult)
	}
}

func TestGrepToolMaxColumnsOmission(t *testing.T) {
	dir := t.TempDir()
	longMatch := strings.Repeat("x", defaultGrepMaxColumns-len("Needle")) + "Needle"
	longContext := strings.Repeat("c", defaultGrepMaxColumns)
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(longMatch+"\n"+longContext+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	contentResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_max_columns_content",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","after_context":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantContent := "long.txt:1:[Omitted long matching line]\nlong.txt-2-[Omitted long context line]"
	if contentResult.Content != wantContent {
		t.Fatalf("max-columns content = %#v", contentResult.Content)
	}

	filesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_max_columns_files",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filesResult.Content != "Found 1 file\nlong.txt" {
		t.Fatalf("max-columns files = %#v", filesResult.Content)
	}

	countResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_max_columns_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"count"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if countResult.Content != "long.txt:1\n\nFound 1 total occurrence across 1 file." {
		t.Fatalf("max-columns count = %#v", countResult.Content)
	}

	customResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_custom_max_columns",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","after_context":1,"--max-columns":"0"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCustom := "long.txt:1:" + longMatch + "\nlong.txt-2-" + longContext
	if customResult.Content != wantCustom || customResult.StructuredContent["max_columns"] != 0 {
		t.Fatalf("custom max-columns result = %#v", customResult)
	}

	shortResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_short_max_columns",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","max_columns":6}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortResult.Content != "long.txt:1:[Omitted long matching line]" || shortResult.StructuredContent["max_columns"] != 6 {
		t.Fatalf("short max-columns result = %#v", shortResult)
	}
}

func TestGrepToolMaxColumnsPreview(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"1234567890NeedleXYZ",
		"  1234567890NeedleXYZ",
		"short Needle",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "preview.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_max_columns_preview",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","--max-columns":10,"--max-columns-preview":true,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "preview.txt:1:1234567890 [... omitted end of long line]\n\n[Showing results with pagination = limit: 1]"
	if result.Content != want || result.StructuredContent["max_columns_preview"] != true {
		t.Fatalf("max-columns-preview result = %#v", result)
	}

	trimResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_max_columns_preview_trim",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","max_columns":10,"max_columns_preview":"true","--trim":true,"offset":1,"head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantTrim := "preview.txt:2:1234567890 [... omitted end of long line]\n\n[Showing results with pagination = limit: 1, offset: 1]"
	if trimResult.Content != wantTrim ||
		trimResult.StructuredContent["max_columns_preview"] != true ||
		trimResult.StructuredContent["trim"] != true {
		t.Fatalf("trim max-columns-preview result = %#v", trimResult)
	}

	noPreviewResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_max_columns_preview",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","max_columns":10,"maxColumnsPreview":true,"--no-max-columns-preview":"true","head_limit":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoPreview := "preview.txt:1:[Omitted long matching line]\n\n[Showing results with pagination = limit: 1]"
	if noPreviewResult.Content != wantNoPreview || noPreviewResult.StructuredContent["max_columns_preview"] != false {
		t.Fatalf("no max-columns-preview result = %#v", noPreviewResult)
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
	if result.Content != "Found 1 file\nmixed.txt" || result.StructuredContent["case_insensitive"] != true {
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
	if ignoreCaseResult.Content != "Found 1 file\nmixed.txt" || ignoreCaseResult.StructuredContent["case_insensitive"] != true {
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
	if shortResult.Content != "Found 1 file\nmixed.txt" || shortResult.StructuredContent["case_insensitive"] != true {
		t.Fatalf("short case-insensitive result = %#v", shortResult)
	}

	semanticBoolResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_semantic_bool_case_insensitive",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha","-i":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if semanticBoolResult.Content != "Found 1 file\nmixed.txt" || semanticBoolResult.StructuredContent["case_insensitive"] != true {
		t.Fatalf("semantic bool case-insensitive result = %#v", semanticBoolResult)
	}

	longIgnoreCaseResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_long_ignore_case",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha","--ignore-case":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longIgnoreCaseResult.Content != "Found 1 file\nmixed.txt" || longIgnoreCaseResult.StructuredContent["case_insensitive"] != true {
		t.Fatalf("long ignore-case result = %#v", longIgnoreCaseResult)
	}

	smartCaseLowerResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_smart_case_lower",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha","--smart-case":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if smartCaseLowerResult.Content != "Found 1 file\nmixed.txt" ||
		smartCaseLowerResult.StructuredContent["case_insensitive"] != true ||
		smartCaseLowerResult.StructuredContent["smart_case"] != true {
		t.Fatalf("smart-case lowercase result = %#v", smartCaseLowerResult)
	}

	smartCaseUpperResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_smart_case_upper",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha","-S":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if smartCaseUpperResult.Content != "Found 1 file\nmixed.txt" ||
		smartCaseUpperResult.StructuredContent["case_insensitive"] != true ||
		smartCaseUpperResult.StructuredContent["smart_case"] != true {
		t.Fatalf("smart-case short lowercase result = %#v", smartCaseUpperResult)
	}

	smartCaseSensitiveResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_smart_case_sensitive",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"ALPHA","smart_case":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if smartCaseSensitiveResult.Content != "No files found" ||
		smartCaseSensitiveResult.StructuredContent["case_insensitive"] != false ||
		smartCaseSensitiveResult.StructuredContent["smart_case"] != true {
		t.Fatalf("smart-case sensitive result = %#v", smartCaseSensitiveResult)
	}

	caseSensitiveOverrideResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_case_sensitive_override",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha","ignore_case":true,"--case-sensitive":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if caseSensitiveOverrideResult.Content != "No files found" ||
		caseSensitiveOverrideResult.StructuredContent["case_insensitive"] != false ||
		caseSensitiveOverrideResult.StructuredContent["case_sensitive"] != true {
		t.Fatalf("case-sensitive override result = %#v", caseSensitiveOverrideResult)
	}

	shortCaseSensitiveResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_case_sensitive_short",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha","-i":true,"-s":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortCaseSensitiveResult.Content != "No files found" ||
		shortCaseSensitiveResult.StructuredContent["case_sensitive"] != true {
		t.Fatalf("short case-sensitive result = %#v", shortCaseSensitiveResult)
	}

	ignoredContextResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_ignored_context",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","context":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ignoredContextResult.Content != "Found 1 file\nmixed.txt" || ignoredContextResult.StructuredContent["before_context"] != 0 || ignoredContextResult.StructuredContent["after_context"] != 0 {
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

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_bad_max_columns",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","--max-columns":"-1"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "max_columns must be non-negative") {
		t.Fatalf("max_columns validation err = %v", err)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_bad_max_depth",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Alpha","--max-depth":"-1"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "max_depth must be non-negative") {
		t.Fatalf("max_depth validation err = %v", err)
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

	longFixedResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_long_fixed",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"a+b","output_mode":"content","--fixed-strings":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longFixedResult.Content != "literal.txt:1:a+b" || longFixedResult.StructuredContent["fixed_strings"] != true {
		t.Fatalf("long fixed result = %#v", longFixedResult)
	}
}

func TestGrepToolOnlyMatching(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"ID-123 ID-456",
		"none",
		"prefix ID-789 tail",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "tokens.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_only_matching",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"ID-[0-9]+","output_mode":"content","only_matching":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "tokens.txt:1:ID-123\ntokens.txt:1:ID-456\ntokens.txt:3:ID-789"
	if result.Content != want || result.StructuredContent["only_matching"] != true {
		t.Fatalf("only_matching result = %#v", result)
	}
	matches := result.StructuredContent["matches"].([]map[string]any)
	if len(matches) != 3 || matches[1]["text"] != "ID-456" || matches[1]["line"] != 1 || matches[1]["column"] != 8 {
		t.Fatalf("only_matching structured matches = %#v", matches)
	}

	shortResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_only_matching_short",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"ID-[0-9]+","outputMode":"content","-o":"true","lineNumbers":false}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantShort := "tokens.txt:ID-123\ntokens.txt:ID-456\ntokens.txt:ID-789"
	if shortResult.Content != wantShort || shortResult.StructuredContent["only_matching"] != true || shortResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("short only-matching result = %#v", shortResult)
	}

	longResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_only_matching_long",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"ID-[0-9]+","outputMode":"content","--only-matching":"true","--line-number":false}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longResult.Content != wantShort || longResult.StructuredContent["only_matching"] != true || longResult.StructuredContent["line_numbers"] != false {
		t.Fatalf("long only-matching result = %#v", longResult)
	}
}

func TestGrepToolReplace(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"before",
		"ID-123 ID-456",
		"after",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "tokens.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_replace",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"ID-([0-9]+)","output_mode":"content","replace":"[$1]","context":1}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "tokens.txt-1-before\ntokens.txt:2:[123] [456]\ntokens.txt-3-after"
	if result.Content != want || result.StructuredContent["replace"] != "[$1]" || result.StructuredContent["has_replace"] != true {
		t.Fatalf("replace result = %#v", result)
	}

	onlyResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_replace_only_matching",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"ID-([0-9]+)","output_mode":"content","only_matching":true,"--replace":"<$1>"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantOnly := "tokens.txt:2:<123>\ntokens.txt:2:<456>"
	if onlyResult.Content != wantOnly || onlyResult.StructuredContent["replace"] != "<$1>" {
		t.Fatalf("replace only-matching result = %#v", onlyResult)
	}

	vimgrepResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_replace_vimgrep",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"ID-([0-9]+)","output_mode":"content","--vimgrep":true,"-r":"[$1]"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantVimgrep := "tokens.txt:2:1:[123] [456]\ntokens.txt:2:8:[123] [456]"
	if vimgrepResult.Content != wantVimgrep || vimgrepResult.StructuredContent["vimgrep"] != true {
		t.Fatalf("replace vimgrep result = %#v", vimgrepResult)
	}

	countResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_replace_count_ignored",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"ID-([0-9]+)","output_mode":"count","replace":"[$1]"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if countResult.Content != "tokens.txt:1\n\nFound 1 total occurrence across 1 file." ||
		countResult.StructuredContent["has_replace"] != false {
		t.Fatalf("replace count result = %#v", countResult)
	}
}

func TestGrepToolWordRegexp(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"cat",
		"concatenate",
		"bobcat",
		"cat.",
		"cat_thing",
		"dog",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "words.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	contentResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_word_regexp",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"cat","output_mode":"content","word_regexp":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantContent := "words.txt:1:cat\nwords.txt:4:cat."
	if contentResult.Content != wantContent || contentResult.StructuredContent["word_regexp"] != true {
		t.Fatalf("word_regexp content result = %#v", contentResult)
	}

	camelResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_word_regexp_camel",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"cat","outputMode":"count","wordRegexp":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCount := "words.txt:2\n\nFound 2 total occurrences across 1 file."
	if camelResult.Content != wantCount || camelResult.StructuredContent["word_regexp"] != true {
		t.Fatalf("wordRegexp count result = %#v", camelResult)
	}

	dashResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_word_regexp_dash",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"cat","output_mode":"count","word-regexp":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dashResult.Content != wantCount || dashResult.StructuredContent["word_regexp"] != true {
		t.Fatalf("word-regexp count result = %#v", dashResult)
	}

	shortSemanticResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_word_regexp_short_semantic",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"cat","output_mode":"count","-w":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortSemanticResult.Content != wantCount || shortSemanticResult.StructuredContent["word_regexp"] != true {
		t.Fatalf("short semantic word regexp result = %#v", shortSemanticResult)
	}

	longResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_word_regexp_long",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"cat","output_mode":"count","--word-regexp":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longResult.Content != wantCount || longResult.StructuredContent["word_regexp"] != true {
		t.Fatalf("long word-regexp result = %#v", longResult)
	}
}

func TestGrepToolLineRegexp(t *testing.T) {
	dir := t.TempDir()
	content := strings.Join([]string{
		"cat",
		"cat.",
		"concatenate",
		"a+b",
		"xx a+b",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "lines.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	contentResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_line_regexp",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"cat","output_mode":"content","line_regexp":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if contentResult.Content != "lines.txt:1:cat" || contentResult.StructuredContent["line_regexp"] != true {
		t.Fatalf("line_regexp content result = %#v", contentResult)
	}

	shortResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_line_regexp_short",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"cat","output_mode":"count","-x":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortResult.Content != "lines.txt:1\n\nFound 1 total occurrence across 1 file." ||
		shortResult.StructuredContent["line_regexp"] != true {
		t.Fatalf("short line-regexp result = %#v", shortResult)
	}

	longResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_line_regexp_long",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"cat","output_mode":"content","--line-regexp":true,"word_regexp":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longResult.Content != "lines.txt:1:cat" ||
		longResult.StructuredContent["line_regexp"] != true ||
		longResult.StructuredContent["word_regexp"] != true {
		t.Fatalf("long line-regexp result = %#v", longResult)
	}

	fixedResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_line_regexp_fixed",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"a+b","output_mode":"content","fixed_strings":true,"line-regexp":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fixedResult.Content != "lines.txt:4:a+b" ||
		fixedResult.StructuredContent["fixed_strings"] != true ||
		fixedResult.StructuredContent["line_regexp"] != true {
		t.Fatalf("fixed line-regexp result = %#v", fixedResult)
	}
}

func TestGrepToolInvertMatch(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"one.txt": "keep\nNeedle\nplain\nNeedle again\n",
		"all.txt": "Needle\nNeedle again\n",
	}
	mtime := time.Now().Add(-time.Hour)
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	contentResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_invert_match_content",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"content","invert_match":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantContent := "one.txt:1:keep\none.txt:3:plain"
	if contentResult.Content != wantContent || contentResult.StructuredContent["invert_match"] != true {
		t.Fatalf("invert_match content result = %#v", contentResult)
	}

	camelResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_invert_match_camel_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","outputMode":"count","invertMatch":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCount := "one.txt:2\n\nFound 2 total occurrences across 1 file."
	if camelResult.Content != wantCount || camelResult.StructuredContent["invert_match"] != true {
		t.Fatalf("invertMatch count result = %#v", camelResult)
	}

	dashResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_invert_match_dash_count",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","output_mode":"count","invert-match":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dashResult.Content != wantCount || dashResult.StructuredContent["invert_match"] != true {
		t.Fatalf("invert-match count result = %#v", dashResult)
	}

	shortResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_invert_match_short_files",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","-v":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortResult.Content != "Found 1 file\none.txt" || shortResult.StructuredContent["invert_match"] != true {
		t.Fatalf("short invert-match result = %#v", shortResult)
	}

	longResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_invert_match_long_files",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--invert-match":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longResult.Content != "Found 1 file\none.txt" || longResult.StructuredContent["invert_match"] != true {
		t.Fatalf("long invert-match result = %#v", longResult)
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

	shortMultilineResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_multiline_short",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha.*gamma","output_mode":"content","-U":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortMultilineResult.Content != want || shortMultilineResult.StructuredContent["multiline"] != true {
		t.Fatalf("short multiline result = %#v", shortMultilineResult)
	}

	dotallResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_multiline_dotall",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha.*gamma","output_mode":"content","--multiline-dotall":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dotallResult.Content != want || dotallResult.StructuredContent["multiline"] != true {
		t.Fatalf("multiline-dotall result = %#v", dotallResult)
	}

	invertResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_multiline_invert",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"alpha.*gamma","output_mode":"content","multiline":true,"invert_match":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if invertResult.Content != "multi.txt:4:tail" || invertResult.StructuredContent["invert_match"] != true {
		t.Fatalf("multiline invert result = %#v", invertResult)
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
	if goResult.Content != "Found 1 file\nsrc/a.go" || goResult.StructuredContent["type_filter"] != "go" {
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
	if jsResult.Content != "Found 1 file\nsrc/c.jsx" {
		t.Fatalf("javascript type result = %#v", jsResult.Content)
	}

	longTypeResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_long_type",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--type":"javascript"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longTypeResult.Content != "Found 1 file\nsrc/c.jsx" || longTypeResult.StructuredContent["type_filter"] != "javascript" {
		t.Fatalf("long type result = %#v", longTypeResult)
	}

	shortTypeResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_short_type",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","-t":"go"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortTypeResult.Content != "Found 1 file\nsrc/a.go" || shortTypeResult.StructuredContent["type_filter"] != "go" {
		t.Fatalf("short type result = %#v", shortTypeResult)
	}

	typeNotResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_type_not_go",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","type_not":"go","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if typeNotResult.Content != "Found 2 files\nsrc/b.txt\nsrc/c.jsx" || typeNotResult.StructuredContent["type_not_filter"] != "go" {
		t.Fatalf("type_not result = %#v", typeNotResult)
	}

	longTypeNotResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_long_type_not",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--type-not":"javascript","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if longTypeNotResult.Content != "Found 2 files\nsrc/a.go\nsrc/b.txt" || longTypeNotResult.StructuredContent["type_not_filter"] != "javascript" {
		t.Fatalf("long type_not result = %#v", longTypeNotResult)
	}

	shortTypeNotResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_short_type_not",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","-T":"txt","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if shortTypeNotResult.Content != "Found 2 files\nsrc/a.go\nsrc/c.jsx" || shortTypeNotResult.StructuredContent["type_not_filter"] != "txt" {
		t.Fatalf("short type_not result = %#v", shortTypeNotResult)
	}

	combinedTypeResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_type_and_type_not",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","type":"javascript","--type-not":"js","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if combinedTypeResult.Content != "No files found" || combinedTypeResult.StructuredContent["type_filter"] != "javascript" || combinedTypeResult.StructuredContent["type_not_filter"] != "js" {
		t.Fatalf("combined type/type_not result = %#v", combinedTypeResult)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_bad_type",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","type":"*.go"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "type must be a file type or extension") {
		t.Fatalf("type validation err = %v", err)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_bad_type_not",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","type_not":"*.go"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "type must be a file type or extension") {
		t.Fatalf("type_not validation err = %v", err)
	}
}

func TestGrepToolCaseInsensitiveGlobFilter(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"src/Alpha.GO": "Needle go\n",
		"src/Beta.TXT": "Needle text\n",
		"src/gamma.md": "Needle markdown\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	caseSensitiveResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_glob_case_sensitive",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","glob":"*.go","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if caseSensitiveResult.Content != "No files found" {
		t.Fatalf("case-sensitive glob result = %#v", caseSensitiveResult)
	}

	iglobResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_iglob",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--iglob":"*.go","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if iglobResult.Content != "Found 1 file\nsrc/Alpha.GO" || iglobResult.StructuredContent["iglob"] != "*.go" {
		t.Fatalf("iglob result = %#v", iglobResult)
	}

	globCaseInsensitiveResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_glob_case_insensitive_enabled",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","glob":"*.go","--glob-case-insensitive":"true","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if globCaseInsensitiveResult.Content != "Found 1 file\nsrc/Alpha.GO" ||
		globCaseInsensitiveResult.StructuredContent["glob_case_insensitive"] != true {
		t.Fatalf("glob-case-insensitive result = %#v", globCaseInsensitiveResult)
	}

	noGlobCaseInsensitiveResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_glob_case_insensitive",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","glob":"*.go","glob_case_insensitive":true,"--no-glob-case-insensitive":"true","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if noGlobCaseInsensitiveResult.Content != "No files found" ||
		noGlobCaseInsensitiveResult.StructuredContent["glob_case_insensitive"] != false {
		t.Fatalf("no glob-case-insensitive result = %#v", noGlobCaseInsensitiveResult)
	}

	unionResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_glob_iglob_union",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","glob":"*.md","iglob":"*.go","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unionResult.Content != "Found 2 files\nsrc/Alpha.GO\nsrc/gamma.md" {
		t.Fatalf("glob/iglob union result = %#v", unionResult)
	}

	negativeResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_negative_iglob",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","iglob":"!*.txt","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if negativeResult.Content != "Found 2 files\nsrc/Alpha.GO\nsrc/gamma.md" {
		t.Fatalf("negative iglob result = %#v", negativeResult)
	}
}

func TestGrepToolHiddenControls(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{".hdir", ".git"} {
		if err := os.MkdirAll(filepath.Join(dir, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".ignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"visible.txt":     "Needle visible\n",
		".hidden.txt":     "Needle hidden file\n",
		".hdir/hit.txt":   "Needle hidden dir\n",
		"ignored.log":     "Needle ignored log\n",
		".hidden.log":     "Needle hidden ignored log\n",
		".git/hidden.txt": "Needle vcs metadata\n",
	}
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)

	defaultResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_hidden_default",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--files-with-matches":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantDefault := "Found 3 files\n.hdir/hit.txt\n.hidden.txt\nvisible.txt"
	if defaultResult.Content != wantDefault ||
		defaultResult.StructuredContent["hidden"] != true ||
		defaultResult.StructuredContent["no_hidden"] != false {
		t.Fatalf("default hidden result = %#v", defaultResult)
	}

	hiddenFalseResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_hidden_false",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--files-with-matches":true,"hidden":false,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoHidden := "Found 1 file\nvisible.txt"
	if hiddenFalseResult.Content != wantNoHidden ||
		hiddenFalseResult.StructuredContent["hidden"] != false ||
		hiddenFalseResult.StructuredContent["no_hidden"] != true {
		t.Fatalf("hidden false result = %#v", hiddenFalseResult)
	}

	noHiddenResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_hidden",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--files-with-matches":true,"--no-hidden":"true","sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if noHiddenResult.Content != wantNoHidden {
		t.Fatalf("no-hidden result = %#v", noHiddenResult)
	}

	noIgnoreHiddenResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_hidden_no_ignore",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--files-with-matches":true,"--hidden":true,"--no-ignore":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoIgnoreHidden := "Found 5 files\n.hdir/hit.txt\n.hidden.log\n.hidden.txt\nignored.log\nvisible.txt"
	if noIgnoreHiddenResult.Content != wantNoIgnoreHidden {
		t.Fatalf("hidden no-ignore result = %#v", noIgnoreHiddenResult)
	}

	noIgnoreNoHiddenResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_ignore_no_hidden",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--files-with-matches":true,"--no-ignore":true,"--no-hidden":true,"sort":"path"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantNoIgnoreNoHidden := "Found 2 files\nignored.log\nvisible.txt"
	if noIgnoreNoHiddenResult.Content != wantNoIgnoreNoHidden {
		t.Fatalf("no-ignore no-hidden result = %#v", noIgnoreNoHiddenResult)
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
	if err := os.WriteFile(filepath.Join(dir, ".rgignore"), []byte("rgonly.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", ".gitignore"), []byte("local.txt\n!visible.txt\nlogs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", ".rgignore"), []byte("rgonly.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"debug.log":       "Needle hidden by gitignore\n",
		"important.log":   "Needle visible by negation\n",
		"keep.txt":        "Needle visible\n",
		"ignored/hit.txt": "Needle hidden by ignored directory\n",
		"rgonly.txt":      "Needle hidden by rgignore\n",
		"scratch.txt":     "Needle hidden by ignore file\n",
		"sub/local.txt":   "Needle hidden by nested gitignore\n",
		"sub/rgonly.md":   "Needle hidden by nested rgignore\n",
		"sub/visible.txt": "Needle visible by nested negation\n",
		"sub/logs/hit.md": "Needle hidden by nested ignored directory\n",
		".bzr/hit.log":    "Needle hidden by bzr metadata\n",
		".jj/hit.log":     "Needle hidden by jj metadata\n",
		".sl/hit.log":     "Needle hidden by sapling metadata\n",
	}
	mtime := time.Now().Add(-time.Hour)
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
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
	if grepResult.Content != "Found 3 files\nimportant.log\nkeep.txt\nsub/visible.txt" {
		t.Fatalf("grep content = %#v", grepResult.Content)
	}

	noIgnoreFilesResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_ignore_files",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--no-ignore-files":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectedNoIgnoreFiles := "Found 6 files\n" +
		"important.log\n" +
		"keep.txt\n" +
		"rgonly.txt\n" +
		"scratch.txt\n" +
		"sub/rgonly.md\n" +
		"sub/visible.txt"
	if noIgnoreFilesResult.Content != expectedNoIgnoreFiles {
		t.Fatalf("grep no-ignore-files content = %#v", noIgnoreFilesResult.Content)
	}
	if noIgnoreFilesResult.StructuredContent["no_ignore"] != false ||
		noIgnoreFilesResult.StructuredContent["ignore_files"] != false ||
		noIgnoreFilesResult.StructuredContent["no_ignore_files"] != true {
		t.Fatalf("grep no-ignore-files structured content = %#v", noIgnoreFilesResult.StructuredContent)
	}

	noIgnoreResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_no_ignore",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle","--no-ignore":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectedNoIgnore := "Found 10 files\n" +
		"debug.log\n" +
		"ignored/hit.txt\n" +
		"important.log\n" +
		"keep.txt\n" +
		"rgonly.txt\n" +
		"scratch.txt\n" +
		"sub/local.txt\n" +
		"sub/logs/hit.md\n" +
		"sub/rgonly.md\n" +
		"sub/visible.txt"
	if noIgnoreResult.Content != expectedNoIgnore {
		t.Fatalf("grep no-ignore content = %#v", noIgnoreResult.Content)
	}
	if noIgnoreResult.StructuredContent["no_ignore"] != true {
		t.Fatalf("grep no-ignore structured content = %#v", noIgnoreResult.StructuredContent)
	}
}

func TestGlobAndGrepRespectReadDenyPermissionRules(t *testing.T) {
	dir := t.TempDir()
	for _, subdir := range []string{"sub", "blocked"} {
		if err := os.MkdirAll(filepath.Join(dir, subdir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"public.txt":      "Needle visible\n",
		"secret.txt":      "Needle hidden by basename deny\n",
		"runtime.txt":     "Needle hidden by runtime deny\n",
		"sub/secret.txt":  "Needle hidden by basename deny in subdir\n",
		"blocked/hit.txt": "Needle hidden by directory deny\n",
	}
	mtime := time.Now().Add(-time.Hour)
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	engine, err := permissions.NewEngineFromSettings(contracts.PermissionSourceProjectSettings, &contracts.PermissionsSetting{
		Deny: []string{
			"Read(secret.txt)",
			"Read(blocked/)",
			"Bash(rm *)",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.AddRule(permissions.MustParseRule(contracts.PermissionSourceSession, contracts.PermissionDeny, "Read(runtime.txt)"))
	executor := fileExecutor(t)
	ctx := fileToolContext(dir)
	ctx.Permissions = tool.NewEnginePermissionDecider(engine)
	extraIgnores := readDenySearchIgnoreRules(ctx, dir)
	if len(extraIgnores) != 3 {
		t.Fatalf("read deny extra ignores = %#v", extraIgnores)
	}

	globResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_glob_read_deny",
		Name:  "Glob",
		Input: json.RawMessage(`{"pattern":"**/*.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if globResult.Content != "public.txt" {
		t.Fatalf("glob read-deny content = %#v", globResult.Content)
	}

	grepResult, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_grep_read_deny",
		Name:  "Grep",
		Input: json.RawMessage(`{"pattern":"Needle"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if grepResult.Content != "Found 1 file\npublic.txt" {
		t.Fatalf("grep read-deny content = %#v", grepResult.Content)
	}
}

func readJSONFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
