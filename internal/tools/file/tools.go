package filetools

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/memory"
	"ccgo/internal/permissions"
	"ccgo/internal/skills"
	"ccgo/internal/tool"
	bashtools "ccgo/internal/tools/bash"
	powershelltools "ccgo/internal/tools/powershell"
	skilltools "ccgo/internal/tools/skill"
	tasktools "ccgo/internal/tools/task"
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

var allowedReadInputKeys = map[string]struct{}{"file_path": {}, "offset": {}, "limit": {}, "pages": {}}

var readSemanticNumberKeys = map[string]struct{}{
	"offset": {},
	"limit":  {},
}

var allowedEditInputKeys = map[string]struct{}{"file_path": {}, "old_string": {}, "new_string": {}, "replace_all": {}}

var editSemanticBooleanKeys = map[string]struct{}{
	"replace_all": {},
}

type notebookEditInput struct {
	NotebookPath string `json:"notebook_path"`
	CellID       string `json:"cell_id,omitempty"`
	NewSource    string `json:"new_source"`
	CellType     string `json:"cell_type,omitempty"`
	EditMode     string `json:"edit_mode,omitempty"`
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
		NormalizeFunc:   normalizeReadRawInput,
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
		NormalizeFunc: normalizeEditRawInput,
		ValidateFunc:  validateEdit,
		CallFunc:      callEdit,
	}
}

func NewNotebookEditTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "NotebookEdit",
			Description:        "Edit a Jupyter notebook cell.",
			SearchHint:         "edit Jupyter notebook cells ipynb",
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"notebook_path", "new_source"},
				"properties": map[string]any{
					"notebook_path": map[string]any{"type": "string"},
					"cell_id":       map[string]any{"type": "string"},
					"new_source":    map[string]any{"type": "string"},
					"cell_type":     map[string]any{"type": "string", "enum": []any{"code", "markdown"}},
					"edit_mode":     map[string]any{"type": "string", "enum": []any{"replace", "insert", "delete"}},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Edits a Jupyter notebook cell. notebook_path points to the .ipynb file, cell_id selects a cell ID or cell-N index, new_source is the replacement or inserted source, cell_type is code or markdown, and edit_mode is replace, insert, or delete.", nil
		},
		ValidateFunc: validateNotebookEdit,
		CallFunc:     callNotebookEdit,
	}
}

func BuiltinTools() []tool.Tool {
	return []tool.Tool{
		NewReadTool(),
		NewEditTool(),
		NewWriteTool(),
		NewNotebookEditTool(),
		bashtools.NewBashTool(),
		bashtools.NewBashOutputTool(),
		bashtools.NewKillBashTool(),
		powershelltools.NewPowerShellTool(),
		powershelltools.NewPowerShellOutputTool(),
		powershelltools.NewKillPowerShellTool(),
		NewGlobTool(),
		NewGrepTool(),
		todotools.NewTodoWriteTool(),
		tasktools.NewTaskTool(),
		tasktools.NewTaskOutputTool(),
		tasktools.NewKillTaskTool(),
		tasktools.NewSendMessageTool(),
		tasktools.NewTeamCreateTool(),
		tasktools.NewTeamDeleteTool(),
		tasktools.NewTeamOutputTool(),
		tasktools.NewTeamSendMessageTool(),
		tasktools.NewTeamCoordinateTool(),
		tasktools.NewResumeTaskTool(),
		tasktools.NewSleepTool(),
		skilltools.NewSkillTool(),
		webtools.NewWebFetchTool(),
		webtools.NewWebSearchTool(),
	}
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
	path := resolvePath(ctx.WorkingDirectory, input.FilePath)
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return nil
	}
	if isBlockedDevicePath(path) {
		return fmt.Errorf("cannot read %q: this device file would block or produce infinite output", input.FilePath)
	}
	if isPDFPath(path) {
		if input.Offset != nil || input.Limit != nil {
			return fmt.Errorf("offset and limit are only supported for text files")
		}
		if _, err := parsePDFPageSelection(input.Pages, 0); err != nil {
			return err
		}
		return nil
	}
	if input.Pages != "" {
		return fmt.Errorf("pages are only supported for PDF files")
	}
	if isNotebookPath(path) && (input.Offset != nil || input.Limit != nil) {
		return fmt.Errorf("offset and limit are only supported for text files")
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

func discoverSkillDirsForFile(ctx tool.Context, path string) {
	if ctx.Metadata == nil || strings.TrimSpace(ctx.WorkingDirectory) == "" {
		return
	}
	discovered := skills.DiscoverSkillDirsForPaths([]string{path}, ctx.WorkingDirectory)
	if len(discovered) == 0 {
		return
	}
	internal := tool.InternalPathContextFromMetadata(ctx.Metadata)
	internal.SkillDirs = appendUniquePaths(internal.SkillDirs, discovered...)
	ctx.Metadata[tool.MetadataInternalPathContextKey] = internal
}

func appendUniquePaths(base []string, items ...string) []string {
	seen := map[string]struct{}{}
	for _, item := range base {
		seen[filepath.Clean(item)] = struct{}{}
	}
	for _, item := range items {
		if item == "" {
			continue
		}
		clean := filepath.Clean(item)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		base = append(base, clean)
	}
	return base
}

func callRead(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeRead(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	path := resolvePath(ctx.WorkingDirectory, input.FilePath)
	discoverSkillDirsForFile(ctx, path)
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
	if isPDFPath(path) {
		return readPDFResult(input.FilePath, path, input.Pages)
	}
	if isNotebookPath(path) {
		return readNotebookResult(input.FilePath, path, info, state)
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

type notebookDocument struct {
	Cells []notebookCell `json:"cells"`
}

type notebookCell struct {
	CellType       string           `json:"cell_type"`
	Source         any              `json:"source"`
	Outputs        []notebookOutput `json:"outputs"`
	ExecutionCount any              `json:"execution_count"`
}

type notebookOutput struct {
	OutputType string         `json:"output_type"`
	Name       string         `json:"name"`
	Text       any            `json:"text"`
	Data       map[string]any `json:"data"`
	EName      string         `json:"ename"`
	EValue     string         `json:"evalue"`
	Traceback  []string       `json:"traceback"`
}

func readNotebookResult(displayPath string, path string, info os.FileInfo, state *ReadState) (contracts.ToolResult, error) {
	raw, err := readText(path)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	var doc notebookDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return contracts.ToolResult{}, fmt.Errorf("invalid notebook JSON: %w", err)
	}
	mtime := info.ModTime().UnixMilli()
	if state != nil {
		offset := 1
		state.Set(path, ReadFileState{
			Content:     strings.ReplaceAll(raw, "\r\n", "\n"),
			Timestamp:   mtime,
			Offset:      &offset,
			PartialView: false,
		})
	}
	rendered, cells := renderNotebook(displayPath, doc)
	return contracts.ToolResult{
		Content: rendered,
		StructuredContent: map[string]any{
			"type": "notebook",
			"file": map[string]any{
				"filePath": displayPath,
				"cells":    cells,
			},
		},
	}, nil
}

func renderNotebook(displayPath string, doc notebookDocument) (string, []map[string]any) {
	var b strings.Builder
	fmt.Fprintf(&b, "Notebook: %s\nCells: %d", displayPath, len(doc.Cells))
	cells := make([]map[string]any, 0, len(doc.Cells))
	for i, cell := range doc.Cells {
		cellType := strings.TrimSpace(cell.CellType)
		if cellType == "" {
			cellType = "unknown"
		}
		source := strings.TrimRight(notebookText(cell.Source), "\n")
		outputs := notebookOutputTexts(cell.Outputs)
		fmt.Fprintf(&b, "\n\nCell %d [%s]", i+1, cellType)
		if cell.ExecutionCount != nil && cellType == "code" {
			fmt.Fprintf(&b, " execution_count=%v", cell.ExecutionCount)
		}
		if source == "" {
			b.WriteString(":\n<empty>")
		} else {
			fmt.Fprintf(&b, ":\n%s", source)
		}
		if len(outputs) > 0 {
			b.WriteString("\nOutputs:")
			for _, output := range outputs {
				fmt.Fprintf(&b, "\n%s", output)
			}
		}
		cells = append(cells, map[string]any{
			"index":           i + 1,
			"cell_type":       cellType,
			"source":          source,
			"outputs":         outputs,
			"execution_count": cell.ExecutionCount,
		})
	}
	return b.String(), cells
}

func notebookOutputTexts(outputs []notebookOutput) []string {
	out := make([]string, 0, len(outputs))
	for _, output := range outputs {
		text := strings.TrimRight(notebookText(output.Text), "\n")
		if text == "" && output.Data != nil {
			text = strings.TrimRight(notebookText(output.Data["text/plain"]), "\n")
		}
		if text == "" && (output.EName != "" || output.EValue != "") {
			text = strings.TrimSpace(output.EName + ": " + output.EValue)
		}
		if text == "" && len(output.Traceback) > 0 {
			text = strings.Join(output.Traceback, "\n")
		}
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func notebookText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []string:
		return strings.Join(typed, "")
	case []any:
		var b strings.Builder
		for _, item := range typed {
			b.WriteString(notebookText(item))
		}
		return b.String()
	default:
		return ""
	}
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

func isNotebookPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".ipynb")
}

func isPDFPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
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
	if err := validateSettingsFileContent(path, input.Content); err != nil {
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

func validateSettingsFileContent(path string, content string) error {
	if !isClaudeSettingsPath(path) {
		return nil
	}
	warnings, err := config.ValidateSettingsJSON([]byte(content), path)
	if err != nil {
		return fmt.Errorf("invalid settings file: %w", err)
	}
	if len(warnings) > 0 {
		return fmt.Errorf("invalid settings file: %s", formatSettingsValidationWarning(warnings[0], len(warnings)))
	}
	return nil
}

func isClaudeSettingsPath(path string) bool {
	clean := filepath.Clean(path)
	name := filepath.Base(clean)
	if name != "settings.json" && name != "settings.local.json" {
		return false
	}
	return filepath.Base(filepath.Dir(clean)) == ".claude"
}

func formatSettingsValidationWarning(warning config.ValidationError, total int) string {
	path := warning.Path
	if path == "" {
		path = filepath.Base(warning.File)
	}
	message := warning.Message
	if message == "" {
		message = "settings validation failed"
	}
	if total > 1 {
		return fmt.Sprintf("%s: %s (and %d more)", path, message, total-1)
	}
	return fmt.Sprintf("%s: %s", path, message)
}

func callWrite(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeWrite(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	path := resolvePath(ctx.WorkingDirectory, input.FilePath)
	discoverSkillDirsForFile(ctx, path)
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
	diff := buildTextDiff(input.FilePath, original, input.Content)
	return contracts.ToolResult{
		Content: message,
		StructuredContent: map[string]any{
			"type":         writeType,
			"filePath":     input.FilePath,
			"content":      input.Content,
			"originalFile": originalValue,
			"diff":         diff.Unified,
			"hunks":        diff.Hunks,
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
			return validateSettingsFileContent(path, input.NewString)
		}
		return fmt.Errorf("File does not exist.")
	}
	if input.OldString == "" {
		if strings.TrimSpace(content) != "" {
			return fmt.Errorf("Cannot create new file - file already exists.")
		}
		return validateSettingsFileContent(path, input.NewString)
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
	actualNew := preserveQuoteStyle(input.OldString, actualOld, input.NewString)
	updated := applyEdit(content, actualOld, actualNew, input.ReplaceAll)
	return validateSettingsFileContent(path, updated)
}

func callEdit(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeEdit(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	path := resolvePath(ctx.WorkingDirectory, input.FilePath)
	discoverSkillDirsForFile(ctx, path)
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
	diff := buildTextDiff(input.FilePath, content, updated)
	return contracts.ToolResult{
		Content: message,
		StructuredContent: map[string]any{
			"filePath":     input.FilePath,
			"oldString":    actualOld,
			"newString":    input.NewString,
			"originalFile": content,
			"replaceAll":   input.ReplaceAll,
			"diff":         diff.Unified,
			"hunks":        diff.Hunks,
		},
	}, nil
}

func validateNotebookEdit(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeNotebookEdit(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.NotebookPath) == "" {
		return fmt.Errorf("notebook_path is required")
	}
	path := resolvePath(ctx.WorkingDirectory, input.NotebookPath)
	if !isNotebookPath(path) {
		return fmt.Errorf("File must be a Jupyter notebook (.ipynb file). For editing other file types, use the FileEdit tool.")
	}
	mode := notebookEditMode(input)
	if mode != "replace" && mode != "insert" && mode != "delete" {
		return fmt.Errorf("Edit mode must be replace, insert, or delete.")
	}
	if input.CellType != "" && input.CellType != "code" && input.CellType != "markdown" {
		return fmt.Errorf("cell_type must be code or markdown")
	}
	if mode == "insert" && input.CellType == "" {
		return fmt.Errorf("Cell type is required when using edit_mode=insert.")
	}
	content, existed, _, _, err := readTextForEdit(path)
	if err != nil {
		return err
	}
	if !existed {
		return fmt.Errorf("Notebook file does not exist.")
	}
	if err := validateFreshFullReadWithContent(ctx, path, content); err != nil {
		return err
	}
	_, cells, err := parseNotebookJSON(content)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.CellID) == "" {
		if mode != "insert" {
			return fmt.Errorf("Cell ID must be specified when not inserting a new cell.")
		}
		return nil
	}
	_, _, err = findNotebookCellIndex(cells, input.CellID)
	return err
}

func callNotebookEdit(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeNotebookEdit(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	path := resolvePath(ctx.WorkingDirectory, input.NotebookPath)
	discoverSkillDirsForFile(ctx, path)
	content, existed, crlf, modeBits, err := readTextForEdit(path)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if !existed {
		return contracts.ToolResult{}, fmt.Errorf("Notebook file does not exist.")
	}
	if err := validateFreshFullReadWithContent(ctx, path, content); err != nil {
		return contracts.ToolResult{}, err
	}
	notebook, cells, err := parseNotebookJSON(content)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	mode := notebookEditMode(input)
	cellIndex := 0
	targetCellID := strings.TrimSpace(input.CellID)
	if targetCellID != "" {
		foundIndex, foundID, err := findNotebookCellIndex(cells, targetCellID)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		cellIndex = foundIndex
		targetCellID = foundID
		if mode == "insert" {
			cellIndex++
		}
	}
	cellType := input.CellType
	oldSource := ""
	if mode == "delete" {
		if cellIndex < 0 || cellIndex >= len(cells) {
			return contracts.ToolResult{}, fmt.Errorf("Cell with index %d does not exist in notebook.", cellIndex)
		}
		oldSource = notebookCellSource(cells[cellIndex])
		if cell, ok := cells[cellIndex].(map[string]any); ok && cellType == "" {
			cellType, _ = cell["cell_type"].(string)
		}
		cells = append(cells[:cellIndex], cells[cellIndex+1:]...)
	} else if mode == "insert" {
		if cellType == "" {
			cellType = "code"
		}
		newCellID := ""
		if notebookSupportsCellID(notebook) {
			newCellID = "cell-" + string(contracts.NewID())[:12]
		}
		cells = append(cells, nil)
		copy(cells[cellIndex+1:], cells[cellIndex:])
		cells[cellIndex] = newNotebookCell(cellType, input.NewSource, newCellID)
		targetCellID = newCellID
	} else {
		if cellIndex < 0 || cellIndex >= len(cells) {
			return contracts.ToolResult{}, fmt.Errorf("Cell with index %d does not exist in notebook.", cellIndex)
		}
		cell, ok := cells[cellIndex].(map[string]any)
		if !ok {
			return contracts.ToolResult{}, fmt.Errorf("Cell with index %d is not a notebook cell object.", cellIndex)
		}
		oldSource = notebookCellSource(cell)
		if cellType == "" {
			cellType, _ = cell["cell_type"].(string)
			if cellType == "" {
				cellType = "code"
			}
		}
		if existingType, _ := cell["cell_type"].(string); existingType == "code" {
			cell["execution_count"] = nil
			cell["outputs"] = []any{}
		}
		cell["source"] = input.NewSource
		if input.CellType != "" {
			cell["cell_type"] = input.CellType
		}
		cells[cellIndex] = cell
	}
	notebook["cells"] = cells
	data, err := json.MarshalIndent(notebook, "", " ")
	if err != nil {
		return contracts.ToolResult{}, err
	}
	updated := string(data)
	if err := writeNormalizedText(path, updated, crlf, modeBits); err != nil {
		return contracts.ToolResult{}, err
	}
	if state := EnsureReadState(ctx); state != nil {
		if mtime, err := mtimeMillis(path); err == nil {
			state.Set(path, ReadFileState{Content: updated, Timestamp: mtime})
		}
	}
	language := notebookLanguage(notebook)
	diffTarget := targetCellID
	if diffTarget == "" {
		diffTarget = fmt.Sprintf("cell-%d", cellIndex)
	}
	newDiffSource := input.NewSource
	if mode == "delete" {
		newDiffSource = ""
	}
	diff := buildTextDiff(fmt.Sprintf("%s:%s", input.NotebookPath, diffTarget), oldSource, newDiffSource)
	message := fmt.Sprintf("Updated notebook %s cell %s.", input.NotebookPath, targetCellID)
	if mode == "insert" {
		message = fmt.Sprintf("Inserted notebook cell %s in %s.", targetCellID, input.NotebookPath)
	} else if mode == "delete" {
		message = fmt.Sprintf("Deleted notebook cell %s from %s.", targetCellID, input.NotebookPath)
	}
	return contracts.ToolResult{
		Content: message,
		StructuredContent: map[string]any{
			"new_source":    input.NewSource,
			"cell_id":       targetCellID,
			"cell_type":     cellType,
			"language":      language,
			"edit_mode":     mode,
			"error":         "",
			"notebook_path": path,
			"original_file": content,
			"updated_file":  updated,
			"diff":          diff.Unified,
			"hunks":         diff.Hunks,
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
	normalized, err := normalizeReadRawInput(raw)
	if err != nil {
		return readInput{}, err
	}
	if err := json.Unmarshal(normalized, &input); err != nil {
		return readInput{}, err
	}
	return input, nil
}

func normalizeReadRawInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeStrictObject(raw, allowedReadInputKeys)
	if err != nil {
		return nil, err
	}
	coerceSemanticJSONStrings(obj, readSemanticNumberKeys, nil)
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return data, nil
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
	normalized, err := normalizeEditRawInput(raw)
	if err != nil {
		return editInput{}, err
	}
	if err := json.Unmarshal(normalized, &input); err != nil {
		return editInput{}, err
	}
	return input, nil
}

func normalizeEditRawInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeStrictObject(raw, allowedEditInputKeys)
	if err != nil {
		return nil, err
	}
	coerceSemanticJSONStrings(obj, nil, editSemanticBooleanKeys)
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func decodeNotebookEdit(raw json.RawMessage) (notebookEditInput, error) {
	var input notebookEditInput
	if err := decodeStrict(raw, map[string]struct{}{"notebook_path": {}, "cell_id": {}, "new_source": {}, "cell_type": {}, "edit_mode": {}}, &input); err != nil {
		return notebookEditInput{}, err
	}
	return input, nil
}

func notebookEditMode(input notebookEditInput) string {
	mode := strings.TrimSpace(input.EditMode)
	if mode == "" {
		return "replace"
	}
	return mode
}

func parseNotebookJSON(content string) (map[string]any, []any, error) {
	var notebook map[string]any
	if err := json.Unmarshal([]byte(content), &notebook); err != nil {
		return nil, nil, fmt.Errorf("Notebook is not valid JSON.")
	}
	cells, ok := notebook["cells"].([]any)
	if !ok {
		return nil, nil, fmt.Errorf("Notebook JSON must contain a cells array.")
	}
	return notebook, cells, nil
}

func findNotebookCellIndex(cells []any, cellID string) (int, string, error) {
	cellID = strings.TrimSpace(cellID)
	for i, cellValue := range cells {
		cell, ok := cellValue.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := cell["id"].(string); id == cellID {
			return i, id, nil
		}
	}
	if index, ok := parseNotebookCellID(cellID); ok {
		if index < 0 || index >= len(cells) {
			return 0, "", fmt.Errorf("Cell with index %d does not exist in notebook.", index)
		}
		return index, cellID, nil
	}
	return 0, "", fmt.Errorf("Cell with ID %q not found in notebook.", cellID)
}

func parseNotebookCellID(cellID string) (int, bool) {
	if !strings.HasPrefix(cellID, "cell-") {
		return 0, false
	}
	index, err := strconv.Atoi(strings.TrimPrefix(cellID, "cell-"))
	if err != nil {
		return 0, false
	}
	return index, true
}

func newNotebookCell(cellType string, source string, id string) map[string]any {
	cell := map[string]any{
		"cell_type": cellType,
		"metadata":  map[string]any{},
		"source":    source,
	}
	if id != "" {
		cell["id"] = id
	}
	if cellType == "code" {
		cell["execution_count"] = nil
		cell["outputs"] = []any{}
	}
	return cell
}

func notebookCellSource(cellValue any) string {
	cell, ok := cellValue.(map[string]any)
	if !ok {
		return ""
	}
	return notebookText(cell["source"])
}

func notebookSupportsCellID(notebook map[string]any) bool {
	major := jsonNumberAsInt(notebook["nbformat"])
	minor := jsonNumberAsInt(notebook["nbformat_minor"])
	return major > 4 || (major == 4 && minor >= 5)
}

func notebookLanguage(notebook map[string]any) string {
	metadata, ok := notebook["metadata"].(map[string]any)
	if !ok {
		return "python"
	}
	languageInfo, ok := metadata["language_info"].(map[string]any)
	if !ok {
		return "python"
	}
	name, _ := languageInfo["name"].(string)
	if strings.TrimSpace(name) == "" {
		return "python"
	}
	return name
}

func jsonNumberAsInt(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	default:
		return 0
	}
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
