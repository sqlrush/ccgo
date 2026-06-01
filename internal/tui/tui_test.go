package tui

import (
	"os"
	"path/filepath"
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

func TestPromptHandlesBracketedPasteAndImageHints(t *testing.T) {
	prompt := NewPromptState(nil)
	paste := ParseKey("\x1b[200~hello\nworld\x1b[201~")
	if paste.Type != KeyPaste || paste.Text != "hello\nworld" {
		t.Fatalf("paste key = %#v", paste)
	}
	prompt.Apply(paste)
	if prompt.Text != "hello\nworld" || prompt.Cursor != len([]rune("hello\nworld")) {
		t.Fatalf("prompt after paste = %#v", prompt)
	}
	image := ParseKey("\x1b]1337;File=name=chart.png;inline=1:AAAA\a")
	if image.Type != KeyImageHint || image.Text != "[Image: chart.png]" {
		t.Fatalf("image key = %#v", image)
	}
	prompt.Apply(image)
	if !strings.Contains(prompt.Text, "[Image: chart.png]") {
		t.Fatalf("prompt after image = %#v", prompt)
	}
}

func TestParseImageHintUsesGenericPlaceholder(t *testing.T) {
	key := ParseKey("\x1b]1337;File=inline=1:AAAA\a")
	if key.Type != KeyImageHint || key.Text != ImageHintPlaceholder {
		t.Fatalf("key = %#v", key)
	}
}

func TestParseSGRMouse(t *testing.T) {
	press := ParseKey("\x1b[<64;10;4M")
	if press.Type != KeyMouse || press.MouseButton != 64 || press.MouseX != 10 || press.MouseY != 4 || press.MouseRelease {
		t.Fatalf("press = %#v", press)
	}
	release := ParseKey("\x1b[<0;1;2m")
	if release.Type != KeyMouse || release.MouseButton != 0 || release.MouseX != 1 || release.MouseY != 2 || !release.MouseRelease {
		t.Fatalf("release = %#v", release)
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
		ID:          "perm_1",
		ToolName:    "Edit",
		Path:        "/tmp/a.txt",
		Description: "Modify file contents.",
	})
	if permission.Title != "Permission" || permission.ID != "perm_1" || permission.Kind != DialogPermission || !strings.Contains(permission.Body, "Tool: Edit") || len(permission.Actions) != 3 {
		t.Fatalf("permission = %#v", permission)
	}
	tasks := TaskDialog([]TaskStatus{{ID: "task_1", Title: "Search", State: "running", Detail: "grep", Progress: 42}})
	if tasks.Title != "Tasks" || tasks.Kind != DialogTask || !strings.Contains(tasks.Body, "Search [running] 42% - grep") {
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
	screen.Dialog = &Dialog{Title: "Permission", Body: "Allow?", Actions: []string{"Allow", "Deny"}, ID: "perm_1", Kind: DialogPermission}
	screen.ApplyKey(ParseKey("x"))
	if screen.Prompt.Text != "" {
		t.Fatalf("dialog input should not edit prompt: %#v", screen.Prompt)
	}
	screen.ApplyKey(ParseKey("\t"))
	if screen.Dialog.Focused != 1 {
		t.Fatalf("focused = %d", screen.Dialog.Focused)
	}
	event := screen.ApplyKey(ParseKey("\n"))
	if event.Type != ScreenEventDialogAction || event.Value != "Deny" || event.DialogID != "perm_1" || event.DialogKind != DialogPermission {
		t.Fatalf("dialog event = %#v", event)
	}
	if screen.Dialog != nil {
		t.Fatalf("dialog should close")
	}
}

func TestDialogRuntimeResolvesPermissionAndTasks(t *testing.T) {
	runtime := NewDialogRuntime()
	dialog := runtime.RequestPermission(PermissionRequest{ID: "perm_1", ToolName: "Write"})
	if runtime.Active == nil || dialog.ID != "perm_1" || len(runtime.Permissions) != 1 {
		t.Fatalf("runtime = %#v dialog = %#v", runtime, dialog)
	}
	result := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow Session", DialogID: "perm_1", DialogKind: DialogPermission})
	if !result.Found || result.Status != DialogResultAllowed || result.Action != "Allow Session" {
		t.Fatalf("result = %#v", result)
	}
	if len(runtime.Permissions) != 0 || runtime.Active != nil {
		t.Fatalf("runtime after resolve = %#v", runtime)
	}
	missing := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow", DialogID: "perm_1", DialogKind: DialogPermission})
	if missing.Found {
		t.Fatalf("stale permission event should be ignored: %#v", missing)
	}

	runtime.UpsertTask(TaskStatus{ID: "b", Title: "Done", State: "completed"})
	runtime.UpsertTask(TaskStatus{ID: "a", Title: "Run", State: "running"})
	tasks := runtime.SortedTasks()
	if len(tasks) != 2 || tasks[0].ID != "a" {
		t.Fatalf("tasks = %#v", tasks)
	}
	taskDialog := runtime.OpenTasksDialog()
	if taskDialog.Kind != DialogTask || !strings.Contains(taskDialog.Body, "Run [running]") {
		t.Fatalf("task dialog = %#v", taskDialog)
	}
	closed := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Close", DialogID: "tasks", DialogKind: DialogTask})
	if !closed.Found || closed.Status != DialogResultClosed {
		t.Fatalf("closed = %#v", closed)
	}
}

func TestDialogRuntimeTaskLifecycle(t *testing.T) {
	runtime := NewDialogRuntime()
	running := runtime.StartTask("task_1", "Search", "starting")
	if running.State != TaskRunning || running.Progress != 0 {
		t.Fatalf("running = %#v", running)
	}
	progress := runtime.UpdateTaskProgress("task_1", "halfway", 50)
	if progress.State != TaskRunning || progress.Detail != "halfway" || progress.Progress != 50 {
		t.Fatalf("progress = %#v", progress)
	}
	done := runtime.CompleteTask("task_1", "done")
	if done.State != TaskCompleted || done.Progress != 100 {
		t.Fatalf("done = %#v", done)
	}
	failed := runtime.FailTask("task_2", "boom")
	if failed.State != TaskFailed || failed.Title != "task_2" {
		t.Fatalf("failed = %#v", failed)
	}
	cancelled := runtime.CancelTask("task_3", "stopped")
	if cancelled.State != TaskCancelled || cancelled.Detail != "stopped" {
		t.Fatalf("cancelled = %#v", cancelled)
	}
}

func TestDialogRuntimeIgnoresStalePermissionEventsAndCancelsActive(t *testing.T) {
	runtime := NewDialogRuntime()
	runtime.RequestPermission(PermissionRequest{ID: "old", ToolName: "Write"})
	runtime.RequestPermission(PermissionRequest{ID: "new", ToolName: "Edit"})
	stale := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow", DialogID: "old", DialogKind: DialogPermission})
	if !stale.Stale || stale.Found {
		t.Fatalf("stale = %#v", stale)
	}
	if _, ok := runtime.Permissions["old"]; !ok {
		t.Fatalf("old permission should remain pending until explicitly replaced or cancelled: %#v", runtime.Permissions)
	}
	cancelled := runtime.CancelActive()
	if !cancelled.Found || cancelled.Status != DialogResultCancelled || cancelled.ID != "new" {
		t.Fatalf("cancelled = %#v", cancelled)
	}
	if _, ok := runtime.Permissions["new"]; ok || runtime.Active != nil {
		t.Fatalf("runtime after cancel = %#v", runtime)
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
	screen.ApplyKey(ParseKey("\x1b[<65;4;4M"))
	scrolledDown := strings.Join(screen.Viewport.Visible(), "\n")
	if scrolledDown == after {
		t.Fatalf("mouse wheel down did not scroll: before=%q after=%q", after, scrolledDown)
	}
	screen.ApplyKey(ParseKey("\x1b[<64;4;4M"))
	scrolledUp := strings.Join(screen.Viewport.Visible(), "\n")
	if scrolledUp != after {
		t.Fatalf("mouse wheel up mismatch: after=%q scrolledUp=%q", after, scrolledUp)
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

func TestScreenLifecycleAlternateScreenSequences(t *testing.T) {
	var lifecycle ScreenLifecycle
	enter := lifecycle.EnterAlternate()
	if !lifecycle.AlternateScreen || !lifecycle.CursorHidden {
		t.Fatalf("lifecycle after enter = %#v", lifecycle)
	}
	if !strings.Contains(enter, EnterAlternateScreen) || !strings.Contains(enter, HideCursor) {
		t.Fatalf("enter = %q", enter)
	}
	exit := lifecycle.ExitAlternate()
	if lifecycle.AlternateScreen || lifecycle.CursorHidden {
		t.Fatalf("lifecycle after exit = %#v", lifecycle)
	}
	if !strings.Contains(exit, ShowCursor) || !strings.Contains(exit, ExitAlternateScreen) {
		t.Fatalf("exit = %q", exit)
	}
}

func TestCaptureANSISnapshotPreservesOutputAndVisibleText(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.Text = "run"
	prompt.Cursor = 3
	snapshot := CaptureANSISnapshot("main", 32, 6, Frame{
		Messages:   []Message{{Role: RoleAssistant, Text: "hello"}},
		Status:     "ready",
		Prompt:     prompt,
		ShowCursor: true,
	})
	if snapshot.Name != "main" || snapshot.Width != 32 || snapshot.Height != 6 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if !strings.Contains(snapshot.Output, HomeCursor) || !strings.Contains(snapshot.Output, ClearScreen) {
		t.Fatalf("output = %q", snapshot.Output)
	}
	if strings.Contains(snapshot.Text, "\x1b[") || !strings.Contains(snapshot.Text, "assistant: hello") || !strings.Contains(snapshot.Text, "> run") {
		t.Fatalf("text = %q", snapshot.Text)
	}
}

func TestSnapshotCorpusWritesAndComparesVisibleText(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.Text = "run"
	snapshot := CaptureANSISnapshot("main:view", 32, 6, Frame{
		Messages:   []Message{{Role: RoleAssistant, Text: "hello"}},
		Status:     "ready",
		Prompt:     prompt,
		ShowCursor: true,
	})
	corpus := SnapshotCorpus{Dir: t.TempDir()}
	if err := corpus.Write(snapshot); err != nil {
		t.Fatal(err)
	}
	text, err := corpus.LoadText("main:view")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "assistant: hello") {
		t.Fatalf("text = %q", text)
	}
	comparison, err := corpus.Compare(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.Match || comparison.ExpectedText != comparison.ActualText {
		t.Fatalf("comparison = %#v", comparison)
	}
	changed := snapshot
	changed.Text = strings.ReplaceAll(changed.Text, "hello", "bye")
	comparison, err = corpus.Compare(changed)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Match || !strings.Contains(comparison.ExpectedText, "hello") || !strings.Contains(comparison.ActualText, "bye") {
		t.Fatalf("changed comparison = %#v", comparison)
	}
	if _, err := os.Stat(filepath.Join(corpus.Dir, "main_view.ansi")); err != nil {
		t.Fatal(err)
	}
}

func TestRunInteractionScriptCapturesEventsAndSnapshots(t *testing.T) {
	screen := NewREPLScreen(30, 6, nil)
	permission := PermissionDialog(PermissionRequest{ID: "perm_1", ToolName: "Edit"})
	result, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{Message: &Message{Role: RoleAssistant, Text: "ready"}, SnapshotName: "initial", ExpectSnapshotContains: []string{"assistant: ready"}},
		{Keys: []string{"r", "u", "n"}},
		{Key: "\n", SnapshotName: "submitted", ExpectEvent: &ScreenEvent{Type: ScreenEventPromptSubmitted, Value: "run"}},
		{Dialog: &permission},
		{Key: "\t"},
		{Key: "\n", SnapshotName: "permission", ExpectEvent: &ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow Session", DialogID: "perm_1", DialogKind: DialogPermission}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("events = %#v", result.Events)
	}
	if result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "run" {
		t.Fatalf("submit event = %#v", result.Events[0])
	}
	if result.Events[1].Type != ScreenEventDialogAction || result.Events[1].DialogID != "perm_1" || result.Events[1].Value != "Allow Session" {
		t.Fatalf("dialog event = %#v", result.Events[1])
	}
	if len(result.Snapshots) != 3 || !strings.Contains(result.Snapshots[0].Text, "assistant: ready") {
		t.Fatalf("snapshots = %#v", result.Snapshots)
	}
}

func TestRunInteractionScriptCheckedFailsOnExpectationMismatch(t *testing.T) {
	screen := NewREPLScreen(30, 6, nil)
	_, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{Key: "\n", ExpectEvent: &ScreenEvent{Type: ScreenEventDialogAction}},
	})
	if err == nil || !strings.Contains(err.Error(), "event type") {
		t.Fatalf("err = %v", err)
	}
}
