package filetools

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/memory"
	"ccgo/internal/permissions"
	"ccgo/internal/tool"
	bashtools "ccgo/internal/tools/bash"
	todotools "ccgo/internal/tools/todo"
	webtools "ccgo/internal/tools/web"
)

const maxReadImageBytes = 10 * 1024 * 1024

type readInput struct {
	FilePath string `json:"file_path"`
	Offset   *int   `json:"offset,omitempty"`
	Limit    *int   `json:"limit,omitempty"`
	Pages    string `json:"pages,omitempty"`
}

type writeInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type editInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func NewReadTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "Read",
			Description:        "Read a file from the local filesystem.",
			SearchHint:         "read files",
			ReadOnly:           true,
			ConcurrencySafe:    true,
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"file_path"},
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string"},
					"offset":    map[string]any{"type": "integer"},
					"limit":     map[string]any{"type": "integer"},
					"pages":     map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Reads a text or image file from the local filesystem. Text results include line numbers starting at 1; supported images are returned as image content blocks.", nil
		},
		ValidateFunc:    validateRead,
		CallFunc:        callRead,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func NewWriteTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "Write",
			Description:        "Write a file to the local filesystem.",
			SearchHint:         "create or overwrite files",
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"file_path", "content"},
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string"},
					"content":   map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Writes a file to the local filesystem. Existing files must be read first; prefer Edit for modifying existing files.", nil
		},
		ValidateFunc: validateWrite,
		CallFunc:     callWrite,
	}
}

func NewEditTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "Edit",
			Description:        "Perform exact string replacements in files.",
			SearchHint:         "modify file contents in place",
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"file_path", "old_string", "new_string"},
				"properties": map[string]any{
					"file_path":   map[string]any{"type": "string"},
					"old_string":  map[string]any{"type": "string"},
					"new_string":  map[string]any{"type": "string"},
					"replace_all": map[string]any{"type": "boolean"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Performs exact string replacements in files. The file must be read first, and old_string must uniquely identify the target unless replace_all is true.", nil
		},
		ValidateFunc: validateEdit,
		CallFunc:     callEdit,
	}
}

func BuiltinTools() []tool.Tool {
	return []tool.Tool{NewReadTool(), NewEditTool(), NewWriteTool(), bashtools.NewBashTool(), bashtools.NewBashOutputTool(), bashtools.NewKillBashTool(), NewGlobTool(), NewGrepTool(), todotools.NewTodoWriteTool(), webtools.NewWebFetchTool(), webtools.NewWebSearchTool()}
}

func validateRead(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeRead(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.FilePath) == "" {
		return fmt.Errorf("file_path is required")
	}
	if input.Offset != nil && *input.Offset < 0 {
		return fmt.Errorf("offset must be nonnegative")
	}
	if input.Limit != nil && *input.Limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}
	if input.Pages != "" {
		return fmt.Errorf("PDF page ranges are not implemented yet")
	}
	path := resolvePath(ctx.WorkingDirectory, input.FilePath)
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return nil
	}
	if isBlockedDevicePath(path) {
		return fmt.Errorf("cannot read %q: this device file would block or produce infinite output", input.FilePath)
	}
	if hasBinaryExtension(path) {
		if imageMediaTypeForPath(path) != "" {
			if input.Offset != nil || input.Limit != nil {
				return fmt.Errorf("offset and limit are only supported for text files")
			}
			return nil
		}
		return fmt.Errorf("this tool cannot read binary files: %s", filepath.Ext(path))
	}
	return nil
}

func memoryFreshnessPrefix(ctx tool.Context, path string, info os.FileInfo) string {
	internal := tool.InternalPathContextFromMetadata(ctx.Metadata)
	if !permissions.CheckReadableInternalPath(path, permissions.InternalPathContext{AutoMemoryDir: internal.AutoMemoryDir}).Allowed {
		return ""
	}
	return memory.MemoryFreshnessNote(info.ModTime(), time.Time{})
}

func callRead(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeRead(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	path := resolvePath(ctx.WorkingDirectory, input.FilePath)
	offset := 1
	if input.Offset != nil {
		offset = *input.Offset
	}
	state := EnsureReadState(ctx)
	if state != nil {
		if existing, ok := state.Get(path); ok && existing.Offset != nil && *existing.Offset == offset && sameLimit(existing.Limit, input.Limit) {
			if mtime, err := mtimeMillis(path); err == nil && mtime == existing.Timestamp {
				return contracts.ToolResult{
					Content: fileUnchangedStub,
					StructuredContent: map[string]any{
						"type": "file_unchanged",
						"file": map[string]any{"filePath": input.FilePath},
					},
				}, nil
			}
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return contracts.ToolResult{}, fmt.Errorf("file does not exist: %s", input.FilePath)
		}
		return contracts.ToolResult{}, err
	}
	if info.IsDir() {
		return contracts.ToolResult{}, fmt.Errorf("cannot read directory: %s", input.FilePath)
	}
	if mediaType := imageMediaTypeForPath(path); mediaType != "" {
		return readImageResult(input.FilePath, path, mediaType, info)
	}
	content, err := readText(path)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	selected, lineCount, totalLines := selectedLines(content, offset, input.Limit)
	mtime := info.ModTime().UnixMilli()
	if state != nil {
		offsetCopy := offset
		var limitCopy *int
		if input.Limit != nil {
			value := *input.Limit
			limitCopy = &value
		}
		state.Set(path, ReadFileState{
			Content:     strings.ReplaceAll(content, "\r\n", "\n"),
			Timestamp:   mtime,
			Offset:      &offsetCopy,
			Limit:       limitCopy,
			PartialView: offset != 1 || input.Limit != nil,
		})
	}
	if selected == "" {
		if totalLines == 0 {
			return contracts.ToolResult{Content: "<system-reminder>Warning: the file exists but the contents are empty.</system-reminder>"}, nil
		}
		return contracts.ToolResult{Content: fmt.Sprintf("<system-reminder>Warning: the file exists but is shorter than the provided offset (%d). The file has %d lines.</system-reminder>", offset, totalLines)}, nil
	}
	return contracts.ToolResult{
		Content: memoryFreshnessPrefix(ctx, path, info) + addLineNumbers(selected, offset),
		StructuredContent: map[string]any{
			"type": "text",
			"file": map[string]any{
				"filePath":   input.FilePath,
				"content":    selected,
				"numLines":   lineCount,
				"startLine":  offset,
				"totalLines": totalLines,
			},
		},
	}, nil
}

func readImageResult(displayPath string, path string, mediaType string, info os.FileInfo) (contracts.ToolResult, error) {
	if info.Size() > maxReadImageBytes {
		return contracts.ToolResult{}, fmt.Errorf("image file is too large to read: %d bytes exceeds %d bytes", info.Size(), maxReadImageBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	content := []contracts.ContentBlock{
		contracts.NewTextBlock(fmt.Sprintf("Read image file %s (%s, %d bytes).", displayPath, mediaType, len(data))),
		{
			Type: contracts.ContentImage,
			Source: contracts.ImageSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      encoded,
			},
		},
	}
	return contracts.ToolResult{
		Content: content,
		StructuredContent: map[string]any{
			"type": "image",
			"file": map[string]any{
				"filePath":  displayPath,
				"mediaType": mediaType,
				"bytes":     len(data),
			},
		},
	}, nil
}

func imageMediaTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

func validateWrite(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeWrite(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.FilePath) == "" {
		return fmt.Errorf("file_path is required")
	}
	path := resolvePath(ctx.WorkingDirectory, input.FilePath)
	if err := memory.GuardTeamMemoryWrite(path, input.Content); err != nil {
		return err
	}
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("cannot write directory: %s", input.FilePath)
		}
		return validateFreshFullRead(ctx, path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func callWrite(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeWrite(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	path := resolvePath(ctx.WorkingDirectory, input.FilePath)
	original, existed, _, mode, err := readTextForEdit(path)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if existed {
		if err := validateFreshFullReadWithContent(ctx, path, original); err != nil {
			return contracts.ToolResult{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return contracts.ToolResult{}, err
	}
	if err := writeText(path, input.Content, mode); err != nil {
		return contracts.ToolResult{}, err
	}
	if state := EnsureReadState(ctx); state != nil {
		if mtime, err := mtimeMillis(path); err == nil {
			state.Set(path, ReadFileState{Content: input.Content, Timestamp: mtime})
		}
	}
	writeType := "create"
	message := fmt.Sprintf("File created successfully at: %s", input.FilePath)
	var originalValue any
	if existed {
		writeType = "update"
		message = fmt.Sprintf("The file %s has been updated successfully.", input.FilePath)
		originalValue = original
	}
	return contracts.ToolResult{
		Content: message,
		StructuredContent: map[string]any{
			"type":         writeType,
			"filePath":     input.FilePath,
			"content":      input.Content,
			"originalFile": originalValue,
		},
	}, nil
}

func validateEdit(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeEdit(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.FilePath) == "" {
		return fmt.Errorf("file_path is required")
	}
	if input.OldString == input.NewString {
		return fmt.Errorf("No changes to make: old_string and new_string are exactly the same.")
	}
	path := resolvePath(ctx.WorkingDirectory, input.FilePath)
	if err := memory.GuardTeamMemoryWrite(path, input.NewString); err != nil {
		return err
	}
	if strings.HasSuffix(strings.ToLower(path), ".ipynb") {
		return fmt.Errorf("File is a Jupyter Notebook. Use the NotebookEdit tool to edit this file.")
	}
	content, existed, _, _, err := readTextForEdit(path)
	if err != nil {
		return err
	}
	if !existed {
		if input.OldString == "" {
			return nil
		}
		return fmt.Errorf("File does not exist.")
	}
	if input.OldString == "" {
		if strings.TrimSpace(content) != "" {
			return fmt.Errorf("Cannot create new file - file already exists.")
		}
		return nil
	}
	if err := validateFreshFullReadWithContent(ctx, path, content); err != nil {
		return err
	}
	actualOld, ok := findActualString(content, input.OldString)
	if !ok {
		return fmt.Errorf("String to replace not found in file.\nString: %s", input.OldString)
	}
	matches := strings.Count(content, actualOld)
	if matches > 1 && !input.ReplaceAll {
		return fmt.Errorf("Found %d matches of the string to replace, but replace_all is false. To replace all occurrences, set replace_all to true. To replace only one occurrence, please provide more context to uniquely identify the instance.\nString: %s", matches, input.OldString)
	}
	return nil
}

func callEdit(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeEdit(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	path := resolvePath(ctx.WorkingDirectory, input.FilePath)
	content, existed, crlf, mode, err := readTextForEdit(path)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if existed && input.OldString != "" {
		if err := validateFreshFullReadWithContent(ctx, path, content); err != nil {
			return contracts.ToolResult{}, err
		}
	}
	actualOld := input.OldString
	if input.OldString != "" {
		found, ok := findActualString(content, input.OldString)
		if !ok {
			return contracts.ToolResult{}, fmt.Errorf("String to replace not found in file.\nString: %s", input.OldString)
		}
		actualOld = found
	}
	actualNew := preserveQuoteStyle(input.OldString, actualOld, input.NewString)
	updated := applyEdit(content, actualOld, actualNew, input.ReplaceAll)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return contracts.ToolResult{}, err
	}
	if err := writeNormalizedText(path, updated, crlf, mode); err != nil {
		return contracts.ToolResult{}, err
	}
	if state := EnsureReadState(ctx); state != nil {
		if mtime, err := mtimeMillis(path); err == nil {
			state.Set(path, ReadFileState{Content: updated, Timestamp: mtime})
		}
	}
	message := fmt.Sprintf("The file %s has been updated successfully.", input.FilePath)
	if input.ReplaceAll {
		message = fmt.Sprintf("The file %s has been updated. All occurrences were successfully replaced.", input.FilePath)
	}
	return contracts.ToolResult{
		Content: message,
		StructuredContent: map[string]any{
			"filePath":     input.FilePath,
			"oldString":    actualOld,
			"newString":    input.NewString,
			"originalFile": content,
			"replaceAll":   input.ReplaceAll,
		},
	}, nil
}

func validateFreshFullRead(ctx tool.Context, path string) error {
	content, existed, _, _, err := readTextForEdit(path)
	if err != nil {
		return err
	}
	if !existed {
		return nil
	}
	return validateFreshFullReadWithContent(ctx, path, content)
}

func validateFreshFullReadWithContent(ctx tool.Context, path string, currentContent string) error {
	state := EnsureReadState(ctx)
	if state == nil {
		return fmt.Errorf("File has not been read yet. Read it first before writing to it.")
	}
	readState, ok := state.Get(path)
	if !ok || !fullRead(readState) {
		return fmt.Errorf("File has not been read yet. Read it first before writing to it.")
	}
	lastWrite, err := mtimeMillis(path)
	if err != nil {
		return err
	}
	if lastWrite > readState.Timestamp && currentContent != readState.Content {
		return fmt.Errorf(staleWriteError)
	}
	return nil
}

func decodeRead(raw json.RawMessage) (readInput, error) {
	var input readInput
	if err := decodeStrict(raw, map[string]struct{}{"file_path": {}, "offset": {}, "limit": {}, "pages": {}}, &input); err != nil {
		return readInput{}, err
	}
	return input, nil
}

func decodeWrite(raw json.RawMessage) (writeInput, error) {
	var input writeInput
	if err := decodeStrict(raw, map[string]struct{}{"file_path": {}, "content": {}}, &input); err != nil {
		return writeInput{}, err
	}
	return input, nil
}

func decodeEdit(raw json.RawMessage) (editInput, error) {
	var input editInput
	if err := decodeStrict(raw, map[string]struct{}{"file_path": {}, "old_string": {}, "new_string": {}, "replace_all": {}}, &input); err != nil {
		return editInput{}, err
	}
	return input, nil
}

func decodeStrict(raw json.RawMessage, allowed map[string]struct{}, out any) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	for key := range obj {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("input.%s is not allowed", key)
		}
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func sameLimit(a *int, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
