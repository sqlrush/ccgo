package repl

// G24: PERM-TOOL-02 - tool-specific permission dialog content tests.
// Verifies that the permission dialog shows enriched tool-specific content:
//   - Bash: full command text
//   - WebFetch: the URL domain
//   - Edit/Write: the path (diff in future enhancement)
//
// CC ref: src/components/permissions/BashPermissionRequest/BashPermissionRequest.tsx
//         src/components/permissions/WebFetchPermissionRequest/WebFetchPermissionRequest.tsx

import (
	"testing"

	"ccgo/internal/tool"
)

func TestToolSpecificDialogContentBash(t *testing.T) {
	req := tool.PermissionAskRequest{
		ToolName:    "Bash",
		Description: "git status",
	}
	content := toolSpecificDialogContent(req)
	if !contains(content, "git status") {
		t.Fatalf("Bash content missing command text: %q", content)
	}
}

func TestToolSpecificDialogContentBashShowsCommand(t *testing.T) {
	// The full command should be prominently shown, not just a generic description.
	req := tool.PermissionAskRequest{
		ToolName:    "Bash",
		Description: "rm -rf /tmp/test",
	}
	content := toolSpecificDialogContent(req)
	if !contains(content, "rm -rf /tmp/test") {
		t.Fatalf("Bash content must include full command: %q", content)
	}
}

func TestToolSpecificDialogContentWebFetch(t *testing.T) {
	req := tool.PermissionAskRequest{
		ToolName:    "WebFetch",
		Description: "https://api.github.com/repos/foo/bar",
	}
	content := toolSpecificDialogContent(req)
	if !contains(content, "api.github.com") {
		t.Fatalf("WebFetch content must include domain: %q", content)
	}
}

func TestToolSpecificDialogContentWebFetchInvalidURL(t *testing.T) {
	// Non-URL description: fall back to raw description.
	req := tool.PermissionAskRequest{
		ToolName:    "WebFetch",
		Description: "fetch some resource",
	}
	content := toolSpecificDialogContent(req)
	if content == "" {
		t.Fatal("content must not be empty")
	}
	if !contains(content, "fetch some resource") {
		t.Fatalf("WebFetch with non-URL should show description: %q", content)
	}
}

func TestToolSpecificDialogContentEdit(t *testing.T) {
	req := tool.PermissionAskRequest{
		ToolName:    "Edit",
		Path:        "/src/main.go",
		Description: "Replace line 42",
	}
	content := toolSpecificDialogContent(req)
	if !contains(content, "/src/main.go") {
		t.Fatalf("Edit content must include path: %q", content)
	}
}

// TestToolSpecificDialogContentEditShowsDiff verifies PERM-TOOL-02:
// when the Edit tool's Input map provides old_string/new_string, the
// permission dialog shows a unified diff so the user sees the change
// they are approving.
func TestToolSpecificDialogContentEditShowsDiff(t *testing.T) {
	req := tool.PermissionAskRequest{
		ToolName: "Edit",
		Path:     "/src/main.go",
		Input: map[string]any{
			"file_path":  "/src/main.go",
			"old_string": "return nil",
			"new_string": "return err",
		},
	}
	content := toolSpecificDialogContent(req)
	// The diff must include both the removed and added lines.
	if !contains(content, "return nil") {
		t.Fatalf("Edit diff must show old_string (removed): %q", content)
	}
	if !contains(content, "return err") {
		t.Fatalf("Edit diff must show new_string (added): %q", content)
	}
	// Must also include the file path.
	if !contains(content, "/src/main.go") {
		t.Fatalf("Edit diff must include file path: %q", content)
	}
}

// TestToolSpecificDialogContentWriteShowsDiff verifies that Write tool
// permission dialogs show the new content (no old_string).
func TestToolSpecificDialogContentWriteShowsDiff(t *testing.T) {
	req := tool.PermissionAskRequest{
		ToolName: "Write",
		Path:     "/out.go",
		Input: map[string]any{
			"file_path": "/out.go",
			"content":   "package main\n\nfunc main() {}\n",
		},
	}
	content := toolSpecificDialogContent(req)
	if !contains(content, "package main") {
		t.Fatalf("Write diff must show new content: %q", content)
	}
}

func TestToolSpecificDialogContentRead(t *testing.T) {
	req := tool.PermissionAskRequest{
		ToolName:    "Read",
		Path:        "/etc/passwd",
		Description: "Read file",
	}
	content := toolSpecificDialogContent(req)
	if content == "" {
		t.Fatal("content must not be empty")
	}
}

func TestToolSpecificDialogContentPowerShell(t *testing.T) {
	req := tool.PermissionAskRequest{
		ToolName:    "PowerShell",
		Description: "Get-Process -Name explorer",
	}
	content := toolSpecificDialogContent(req)
	if !contains(content, "Get-Process") {
		t.Fatalf("PowerShell content must include command: %q", content)
	}
}

func TestToolSpecificDialogContentUnknownTool(t *testing.T) {
	req := tool.PermissionAskRequest{
		ToolName:    "CustomTool",
		Description: "do something",
	}
	content := toolSpecificDialogContent(req)
	if content == "" {
		t.Fatal("unknown tool content must not be empty")
	}
}
