package tui

import (
	"strings"
	"testing"
)

func TestPromptStateEditsAndSubmits(t *testing.T) {
	prompt := NewPromptState([]string{"old command"})
	for _, seq := range []string{"h", "i", "\x1b[D", "!"} {
		prompt.Apply(ParseKey(seq))
	}
	if prompt.Text != "h!i" || prompt.Cursor != 2 {
		t.Fatalf("prompt = %#v", prompt)
	}
	result := prompt.Apply(ParseKey("\n"))
	if result.Submitted != "h!i" {
		t.Fatalf("result = %#v", result)
	}
	if prompt.Text != "" || prompt.Cursor != 0 {
		t.Fatalf("prompt after submit = %#v", prompt)
	}
}

func TestPromptHistoryNavigationKeepsDraft(t *testing.T) {
	prompt := NewPromptState([]string{"one", "two"})
	prompt.Apply(ParseKey("d"))
	prompt.Apply(ParseKey("\x1b[A"))
	if prompt.Text != "two" {
		t.Fatalf("history prev = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1b[A"))
	if prompt.Text != "one" {
		t.Fatalf("history prev again = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1b[B"))
	prompt.Apply(ParseKey("\x1b[B"))
	if prompt.Text != "d" {
		t.Fatalf("draft = %#v", prompt)
	}
}

func TestRendererIncludesStatusPromptAndDialog(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.Text = "hello"
	prompt.Cursor = 5
	output := RenderOnce(32, 8, Frame{
		Messages: []Message{
			{Role: RoleUser, Text: "Please edit the file"},
			{Role: RoleAssistant, Text: "I will inspect it first"},
		},
		Status: "sonnet | 12%",
		Prompt: prompt,
		Dialog: &Dialog{
			Title:   "Permission",
			Body:    "Allow Edit on /tmp/a.txt?",
			Actions: []string{"Allow", "Deny"},
			Focused: 0,
		},
		ShowCursor: true,
	})
	if !strings.Contains(output, "\x1b[2J") || !strings.Contains(output, "Permission") || !strings.Contains(output, "[Allow]") {
		t.Fatalf("output = %q", output)
	}
	if !strings.Contains(output, "\x1b[8;8H") {
		t.Fatalf("cursor position missing: %q", output)
	}
}

func TestRenderMessagesWrapsWithRolePrefix(t *testing.T) {
	lines := RenderMessages([]Message{{Role: RoleAssistant, Text: "alpha beta gamma"}}, 18)
	if len(lines) < 2 {
		t.Fatalf("lines = %#v", lines)
	}
	if !strings.HasPrefix(lines[0], "assistant:") || strings.HasPrefix(strings.TrimLeft(lines[1], " "), "assistant:") {
		t.Fatalf("wrapped lines = %#v", lines)
	}
}

func TestViewportScrollsAndClamps(t *testing.T) {
	v := NewViewport([]string{"1", "2", "3", "4", "5"}, 3)
	if got := strings.Join(v.Visible(), ","); got != "3,4,5" {
		t.Fatalf("bottom visible = %s", got)
	}
	v.Scroll(-2)
	if got := strings.Join(v.Visible(), ","); got != "1,2,3" {
		t.Fatalf("scrolled visible = %s", got)
	}
	v.Page(10)
	if got := strings.Join(v.Visible(), ","); got != "3,4,5" {
		t.Fatalf("paged visible = %s", got)
	}
}

func TestSelectionMovesAndRendersFocus(t *testing.T) {
	s := NewSelection([]string{"one", "two", "three", "four"})
	s.Move(2)
	current, ok := s.Current()
	if !ok || current != "three" {
		t.Fatalf("current = %q ok=%v", current, ok)
	}
	lines := s.Render(12, 3)
	if len(lines) != 3 || !strings.HasPrefix(lines[1], "> three") {
		t.Fatalf("lines = %#v", lines)
	}
	s.Move(100)
	current, _ = s.Current()
	if current != "four" {
		t.Fatalf("clamped current = %q", current)
	}
}

func TestKeymapResolvesDefaultActions(t *testing.T) {
	keymap := DefaultKeymap()
	if action := keymap.Resolve(ParseKey("\x12")); action != ActionReverseSearch {
		t.Fatalf("ctrl-r action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("x")); action != ActionInsertRune {
		t.Fatalf("rune action = %q", action)
	}
}

func TestKeymapFromSpecsOverridesAndRemovesBindings(t *testing.T) {
	keymap, err := KeymapFromSpecs(DefaultKeymap(), []BindingSpec{
		{Key: "ctrl-r", Action: ActionPageUp},
		{Key: "esc", Action: ActionNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\x12")); action != ActionPageUp {
		t.Fatalf("ctrl-r action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b")); action != ActionNone {
		t.Fatalf("esc action = %q", action)
	}
	if _, err := KeymapFromSpecs(DefaultKeymap(), []BindingSpec{{Key: "wat", Action: ActionCancel}}); err == nil {
		t.Fatal("expected unknown key error")
	}
}

func TestPermissionAndTaskDialogs(t *testing.T) {
	permission := PermissionDialog(PermissionRequest{
		ToolName:    "Edit",
		Path:        "/tmp/a.txt",
		Description: "Modify file contents.",
	})
	if permission.Title != "Permission" || !strings.Contains(permission.Body, "Tool: Edit") || len(permission.Actions) != 3 {
		t.Fatalf("permission = %#v", permission)
	}
	tasks := TaskDialog([]TaskStatus{{ID: "task_1", Title: "Search", State: "running", Detail: "grep", Progress: 42}})
	if tasks.Title != "Tasks" || !strings.Contains(tasks.Body, "Search [running] 42% - grep") {
		t.Fatalf("tasks = %#v", tasks)
	}
}

func TestREPLScreenSubmitsPromptAndRendersMessages(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.Status = "ready"
	screen.AppendMessage(Message{Role: RoleAssistant, Text: "hello from assistant"})
	for _, seq := range []string{"r", "u", "n"} {
		event := screen.ApplyKey(ParseKey(seq))
		if event.Type != ScreenEventNone {
			t.Fatalf("unexpected event = %#v", event)
		}
	}
	event := screen.ApplyKey(ParseKey("\n"))
	if event.Type != ScreenEventPromptSubmitted || event.Value != "run" {
		t.Fatalf("submit event = %#v", event)
	}
	output := screen.Render()
	if !strings.Contains(output, "assistant: hello from assistant") || !strings.Contains(output, "ready") {
		t.Fatalf("output = %q", output)
	}
}

func TestREPLScreenDialogFocusAndConfirm(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.Dialog = &Dialog{Title: "Permission", Body: "Allow?", Actions: []string{"Allow", "Deny"}}
	screen.ApplyKey(ParseKey("\t"))
	if screen.Dialog.Focused != 1 {
		t.Fatalf("focused = %d", screen.Dialog.Focused)
	}
	event := screen.ApplyKey(ParseKey("\n"))
	if event.Type != ScreenEventDialogAction || event.Value != "Deny" {
		t.Fatalf("dialog event = %#v", event)
	}
	if screen.Dialog != nil {
		t.Fatalf("dialog should close")
	}
}

func TestREPLScreenViewportScrolls(t *testing.T) {
	screen := NewREPLScreen(20, 6, nil)
	screen.SetMessages([]Message{
		{Role: RoleSystem, Text: "one"},
		{Role: RoleSystem, Text: "two"},
		{Role: RoleSystem, Text: "three"},
		{Role: RoleSystem, Text: "four"},
		{Role: RoleSystem, Text: "five"},
	})
	before := strings.Join(screen.Viewport.Visible(), "\n")
	screen.ApplyKey(ParseKey("\x1b[5~"))
	after := strings.Join(screen.Viewport.Visible(), "\n")
	if before == after || !strings.Contains(after, "one") {
		t.Fatalf("before=%q after=%q", before, after)
	}
}

func TestREPLScreenVimNormalModeEditsPrompt(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"a", "b", "c"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	if screen.VimMode != VimNormal || screen.Prompt.Text != "abc" {
		t.Fatalf("screen = %#v", screen)
	}
	screen.ApplyKey(ParseKey("h"))
	screen.ApplyKey(ParseKey("x"))
	screen.ApplyKey(ParseKey("i"))
	screen.ApplyKey(ParseKey("Z"))
	if screen.VimMode != VimInsert || screen.Prompt.Text != "abZ" {
		t.Fatalf("screen = %#v", screen)
	}
}
