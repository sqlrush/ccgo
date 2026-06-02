package tui

import (
	"fmt"
	"strings"
)

type ScriptStep struct {
	Keys                      []string
	Key                       string
	Text                      string
	Paste                     string
	Image                     *ScriptImage
	Message                   *Message
	Dialog                    *Dialog
	Keybindings               []BindingSpec
	RequestPermission         *PermissionRequest
	UpsertTask                *TaskStatus
	RemoveTaskID              string
	CancelActiveDialog        bool
	CancelPermissionID        string
	CancelAllPermissions      bool
	CancelAllTasks            bool
	CancelTasksDetail         string
	OpenTasksDialog           bool
	ResizeWidth               int
	ResizeHeight              int
	SnapshotName              string
	ExpectEvent               *ScreenEvent
	ExpectEvents              []ScreenEvent
	ExpectDialogResult        *DialogResultExpectation
	ExpectDialogResults       []DialogResultExpectation
	ExpectDialog              *DialogExpectation
	ExpectPrompt              *PromptExpectation
	ExpectVim                 *VimExpectation
	ExpectTasks               *TasksExpectation
	ExpectReverseSearch       *ReverseSearchExpectation
	ExpectViewport            *ViewportExpectation
	ExpectScreen              *ScreenExpectation
	ExpectFocused             *bool
	ExpectStatusContains      []string
	ExpectStatusNotContains   []string
	ExpectSnapshotContains    []string
	ExpectSnapshotNotContains []string
}

type ScriptImage struct {
	Filename  string
	MediaType string
	Content   string
}

type DialogExpectation struct {
	Active bool
	ID     string
	Kind   DialogKind
	Title  string
}

type DialogResultExpectation struct {
	ID     string
	Kind   DialogKind
	Action string
	Status DialogResultStatus
	Found  *bool
	Stale  *bool
}

type PromptExpectation struct {
	Text     string
	Expanded string
	Cursor   *int
	Empty    bool
}

type VimExpectation struct {
	Enabled          *bool
	Mode             VimMode
	Register         string
	RegisterLinewise *bool
}

type TasksExpectation struct {
	Count       *int
	StateCounts map[string]int
	Contains    []TaskExpectation
}

type TaskExpectation struct {
	ID       string
	Title    string
	State    string
	Detail   string
	Progress *int
}

type ViewportExpectation struct {
	Offset             *int
	VisibleLineCount   int
	VisibleContains    []string
	VisibleNotContains []string
}

type ScreenExpectation struct {
	Width  int
	Height int
}

type ReverseSearchExpectation struct {
	Active      bool
	Query       string
	Cursor      *int
	Current     string
	ResultCount int
	NoResults   bool
}

type ScriptResult struct {
	Events    []ScreenEvent
	Snapshots []ANSISnapshot
}

type RuntimeScriptResult struct {
	ScriptResult
	DialogResults []DialogResult
}

func RunInteractionScript(screen *REPLScreen, steps []ScriptStep) ScriptResult {
	result, _ := RunInteractionScriptChecked(screen, steps)
	return result
}

func RunInteractionScriptChecked(screen *REPLScreen, steps []ScriptStep) (ScriptResult, error) {
	result, _, err := runInteractionScriptChecked(screen, steps, nil, "")
	return result, err
}

func RunDialogRuntimeScriptChecked(screen *REPLScreen, runtime *DialogRuntime, baseStatus string, steps []ScriptStep) (RuntimeScriptResult, error) {
	result, dialogResults, err := runInteractionScriptChecked(screen, steps, runtime, baseStatus)
	return RuntimeScriptResult{ScriptResult: result, DialogResults: dialogResults}, err
}

func runInteractionScriptChecked(screen *REPLScreen, steps []ScriptStep, runtime *DialogRuntime, baseStatus string) (ScriptResult, []DialogResult, error) {
	var result ScriptResult
	var dialogResults []DialogResult
	for index, step := range steps {
		var event ScreenEvent
		var stepEvents []ScreenEvent
		var stepDialogResults []DialogResult
		var snapshot ANSISnapshot
		var dialogResult *DialogResult
		recordEvent := func(next ScreenEvent) {
			event = next
			if next.Type == ScreenEventNone {
				return
			}
			result.Events = append(result.Events, next)
			stepEvents = append(stepEvents, next)
		}
		runtimeResults, err := applyRuntimeStep(index, runtime, step)
		if err != nil {
			return result, dialogResults, err
		}
		if len(runtimeResults) > 0 {
			dialogResults = append(dialogResults, runtimeResults...)
			stepDialogResults = append(stepDialogResults, runtimeResults...)
			last := runtimeResults[len(runtimeResults)-1]
			dialogResult = &last
		}
		if runtime != nil {
			runtime.ApplyToScreen(screen, baseStatus)
		}
		if len(step.Keybindings) > 0 {
			keymap, err := KeymapFromSpecs(screen.Keymap, step.Keybindings)
			if err != nil {
				return result, dialogResults, fmt.Errorf("script step %d keybindings: %w", index, err)
			}
			screen.Keymap = keymap
		}
		if step.ResizeWidth > 0 {
			width := step.ResizeWidth
			height := screen.Height
			if step.ResizeHeight > 0 {
				height = step.ResizeHeight
			}
			screen.Resize(width, height)
		} else if step.ResizeHeight > 0 {
			screen.Resize(screen.Width, step.ResizeHeight)
		}
		if step.Message != nil {
			screen.AppendMessage(*step.Message)
		}
		if step.Dialog != nil {
			dialog := *step.Dialog
			screen.Dialog = &dialog
		}
		keys := step.Keys
		if step.Key != "" {
			keys = append(keys, step.Key)
		}
		if step.Text != "" {
			for _, r := range step.Text {
				recordEvent(screen.ApplyKey(Key{Type: KeyRune, Rune: r}))
			}
		}
		if step.Paste != "" {
			recordEvent(screen.ApplyKey(Key{Type: KeyPaste, Text: step.Paste}))
		}
		if step.Image != nil {
			recordEvent(screen.ApplyKey(scriptImageKey(*step.Image)))
		}
		for _, rawKey := range keys {
			recordEvent(screen.ApplyKey(parseScriptKey(rawKey)))
		}
		if runtime != nil && (event.Type == ScreenEventDialogAction || event.Type == ScreenEventCancelled) {
			resolved := runtime.ResolveScreenEvent(screen, event, baseStatus)
			dialogResult = &resolved
			if resolved.ID != "" || resolved.Found || resolved.Stale {
				dialogResults = append(dialogResults, resolved)
				stepDialogResults = append(stepDialogResults, resolved)
			}
		}
		if step.ExpectEvent != nil {
			if err := compareEvent(index, event, *step.ExpectEvent); err != nil {
				return result, dialogResults, err
			}
		}
		if len(step.ExpectEvents) > 0 {
			if err := compareEvents(index, stepEvents, step.ExpectEvents); err != nil {
				return result, dialogResults, err
			}
		}
		if step.ExpectDialogResult != nil {
			if runtime == nil {
				return result, dialogResults, fmt.Errorf("script step %d dialog result expectation requires dialog runtime", index)
			}
			if dialogResult == nil {
				return result, dialogResults, fmt.Errorf("script step %d dialog result missing", index)
			}
			if err := compareDialogResult(index, *dialogResult, *step.ExpectDialogResult); err != nil {
				return result, dialogResults, err
			}
		}
		if len(step.ExpectDialogResults) > 0 {
			if runtime == nil {
				return result, dialogResults, fmt.Errorf("script step %d dialog result expectations require dialog runtime", index)
			}
			if err := compareDialogResults(index, stepDialogResults, step.ExpectDialogResults); err != nil {
				return result, dialogResults, err
			}
		}
		if step.ExpectDialog != nil {
			if err := compareDialog(index, screen.Dialog, *step.ExpectDialog); err != nil {
				return result, dialogResults, err
			}
		}
		if step.ExpectPrompt != nil {
			if err := comparePrompt(index, screen.Prompt, *step.ExpectPrompt); err != nil {
				return result, dialogResults, err
			}
		}
		if step.ExpectVim != nil {
			if err := compareVim(index, *screen, *step.ExpectVim); err != nil {
				return result, dialogResults, err
			}
		}
		if step.ExpectTasks != nil {
			if runtime == nil {
				return result, dialogResults, fmt.Errorf("script step %d tasks expectation requires dialog runtime", index)
			}
			if err := compareTasks(index, runtime, *step.ExpectTasks); err != nil {
				return result, dialogResults, err
			}
		}
		if step.ExpectReverseSearch != nil {
			if err := compareReverseSearch(index, screen.ReverseSearch, *step.ExpectReverseSearch); err != nil {
				return result, dialogResults, err
			}
		}
		if step.ExpectViewport != nil {
			if err := compareViewport(index, screen.Viewport, *step.ExpectViewport); err != nil {
				return result, dialogResults, err
			}
		}
		if step.ExpectScreen != nil {
			if err := compareScreen(index, *screen, *step.ExpectScreen); err != nil {
				return result, dialogResults, err
			}
		}
		if step.ExpectFocused != nil && screen.Focused != *step.ExpectFocused {
			return result, dialogResults, fmt.Errorf("script step %d focused = %v, want %v", index, screen.Focused, *step.ExpectFocused)
		}
		for _, want := range step.ExpectStatusContains {
			if !strings.Contains(screen.Status, want) {
				return result, dialogResults, fmt.Errorf("script step %d status missing %q in %q", index, want, screen.Status)
			}
		}
		for _, notWant := range step.ExpectStatusNotContains {
			if strings.Contains(screen.Status, notWant) {
				return result, dialogResults, fmt.Errorf("script step %d status unexpectedly contains %q in %q", index, notWant, screen.Status)
			}
		}
		if step.SnapshotName != "" {
			snapshot = CaptureANSISnapshot(step.SnapshotName, screen.Width, screen.Height, screen.Frame())
			result.Snapshots = append(result.Snapshots, snapshot)
		}
		if len(step.ExpectSnapshotContains) > 0 || len(step.ExpectSnapshotNotContains) > 0 {
			if snapshot.Name == "" {
				snapshot = CaptureANSISnapshot(step.SnapshotName, screen.Width, screen.Height, screen.Frame())
			}
			for _, want := range step.ExpectSnapshotContains {
				if !strings.Contains(snapshot.Text, want) {
					return result, dialogResults, fmt.Errorf("script step %d snapshot missing %q in %q", index, want, snapshot.Text)
				}
			}
			for _, notWant := range step.ExpectSnapshotNotContains {
				if strings.Contains(snapshot.Text, notWant) {
					return result, dialogResults, fmt.Errorf("script step %d snapshot unexpectedly contains %q in %q", index, notWant, snapshot.Text)
				}
			}
		}
	}
	return result, dialogResults, nil
}

func parseScriptKey(raw string) Key {
	keyType, err := ParseKeyName(raw)
	if err == nil {
		return Key{Type: keyType}
	}
	return ParseKey(raw)
}

func scriptImageKey(image ScriptImage) Key {
	text := ImageHintPlaceholder
	if image.Filename != "" {
		text = "[Image: " + image.Filename + "]"
	}
	return Key{
		Type:      KeyImageHint,
		Text:      text,
		Content:   image.Content,
		MediaType: image.MediaType,
		Filename:  image.Filename,
	}
}

func applyRuntimeStep(index int, runtime *DialogRuntime, step ScriptStep) ([]DialogResult, error) {
	needsRuntime := step.RequestPermission != nil || step.UpsertTask != nil || step.RemoveTaskID != "" || step.CancelActiveDialog || step.CancelPermissionID != "" || step.CancelAllPermissions || step.CancelAllTasks || step.OpenTasksDialog
	if !needsRuntime {
		return nil, nil
	}
	if runtime == nil {
		return nil, fmt.Errorf("script step %d runtime mutation requires dialog runtime", index)
	}
	var dialogResults []DialogResult
	if step.RequestPermission != nil {
		runtime.RequestPermission(*step.RequestPermission)
	}
	if step.UpsertTask != nil {
		runtime.UpsertTask(*step.UpsertTask)
	}
	if step.RemoveTaskID != "" {
		runtime.RemoveTask(step.RemoveTaskID)
	}
	if step.CancelAllTasks {
		runtime.CancelTasks(step.CancelTasksDetail)
	}
	if step.CancelPermissionID != "" {
		appendDialogResult(&dialogResults, runtime.CancelPermission(step.CancelPermissionID))
	}
	if step.CancelAllPermissions {
		for _, result := range runtime.CancelPermissions() {
			appendDialogResult(&dialogResults, result)
		}
	}
	if step.CancelActiveDialog {
		appendDialogResult(&dialogResults, runtime.CancelActive())
	}
	if step.OpenTasksDialog {
		runtime.OpenTasksDialog()
	}
	return dialogResults, nil
}

func appendDialogResult(results *[]DialogResult, result DialogResult) {
	if result.ID == "" && !result.Found && !result.Stale {
		return
	}
	*results = append(*results, result)
}

func compareDialogResult(index int, got DialogResult, want DialogResultExpectation) error {
	if want.ID != "" && got.ID != want.ID {
		return fmt.Errorf("script step %d dialog result id = %q, want %q", index, got.ID, want.ID)
	}
	if want.Kind != "" && got.Kind != want.Kind {
		return fmt.Errorf("script step %d dialog result kind = %q, want %q", index, got.Kind, want.Kind)
	}
	if want.Action != "" && got.Action != want.Action {
		return fmt.Errorf("script step %d dialog result action = %q, want %q", index, got.Action, want.Action)
	}
	if want.Status != "" && got.Status != want.Status {
		return fmt.Errorf("script step %d dialog result status = %q, want %q", index, got.Status, want.Status)
	}
	if want.Found != nil && got.Found != *want.Found {
		return fmt.Errorf("script step %d dialog result found = %v, want %v", index, got.Found, *want.Found)
	}
	if want.Stale != nil && got.Stale != *want.Stale {
		return fmt.Errorf("script step %d dialog result stale = %v, want %v", index, got.Stale, *want.Stale)
	}
	return nil
}

func compareDialogResults(index int, got []DialogResult, want []DialogResultExpectation) error {
	if len(got) != len(want) {
		return fmt.Errorf("script step %d dialog result count = %d, want %d", index, len(got), len(want))
	}
	for resultIndex, expected := range want {
		if err := compareDialogResult(index, got[resultIndex], expected); err != nil {
			return fmt.Errorf("script step %d dialog result %d mismatch: %w", index, resultIndex, err)
		}
	}
	return nil
}

func compareDialog(index int, got *Dialog, want DialogExpectation) error {
	if !want.Active {
		if got != nil {
			return fmt.Errorf("script step %d dialog active = %#v, want none", index, got)
		}
		return nil
	}
	if got == nil {
		return fmt.Errorf("script step %d dialog inactive, want active", index)
	}
	if want.ID != "" && got.ID != want.ID {
		return fmt.Errorf("script step %d dialog id = %q, want %q", index, got.ID, want.ID)
	}
	if want.Kind != "" && got.Kind != want.Kind {
		return fmt.Errorf("script step %d dialog kind = %q, want %q", index, got.Kind, want.Kind)
	}
	if want.Title != "" && got.Title != want.Title {
		return fmt.Errorf("script step %d dialog title = %q, want %q", index, got.Title, want.Title)
	}
	return nil
}

func compareVim(index int, got REPLScreen, want VimExpectation) error {
	if want.Enabled != nil && got.VimEnabled != *want.Enabled {
		return fmt.Errorf("script step %d vim enabled = %v, want %v", index, got.VimEnabled, *want.Enabled)
	}
	if want.Mode != "" && got.VimMode != want.Mode {
		return fmt.Errorf("script step %d vim mode = %q, want %q", index, got.VimMode, want.Mode)
	}
	if want.Register != "" && got.VimRegister != want.Register {
		return fmt.Errorf("script step %d vim register = %q, want %q", index, got.VimRegister, want.Register)
	}
	if want.RegisterLinewise != nil && got.VimRegisterLinewise != *want.RegisterLinewise {
		return fmt.Errorf("script step %d vim register linewise = %v, want %v", index, got.VimRegisterLinewise, *want.RegisterLinewise)
	}
	return nil
}

func compareTasks(index int, runtime *DialogRuntime, want TasksExpectation) error {
	if want.Count != nil && len(runtime.Tasks) != *want.Count {
		return fmt.Errorf("script step %d task count = %d, want %d", index, len(runtime.Tasks), *want.Count)
	}
	if len(want.StateCounts) > 0 {
		gotCounts := map[string]int{}
		for _, task := range runtime.Tasks {
			gotCounts[normalizedTaskState(task)]++
		}
		for state, count := range want.StateCounts {
			if gotCounts[state] != count {
				return fmt.Errorf("script step %d task state %q count = %d, want %d", index, state, gotCounts[state], count)
			}
		}
	}
	for _, expected := range want.Contains {
		task, ok := findExpectedTask(runtime, expected)
		if !ok {
			return fmt.Errorf("script step %d task missing id=%q title=%q", index, expected.ID, expected.Title)
		}
		if expected.Title != "" && task.Title != expected.Title {
			return fmt.Errorf("script step %d task %q title = %q, want %q", index, task.ID, task.Title, expected.Title)
		}
		if expected.State != "" && normalizedTaskState(task) != expected.State {
			return fmt.Errorf("script step %d task %q state = %q, want %q", index, task.ID, normalizedTaskState(task), expected.State)
		}
		if expected.Detail != "" && task.Detail != expected.Detail {
			return fmt.Errorf("script step %d task %q detail = %q, want %q", index, task.ID, task.Detail, expected.Detail)
		}
		if expected.Progress != nil && task.Progress != *expected.Progress {
			return fmt.Errorf("script step %d task %q progress = %d, want %d", index, task.ID, task.Progress, *expected.Progress)
		}
	}
	return nil
}

func findExpectedTask(runtime *DialogRuntime, want TaskExpectation) (TaskStatus, bool) {
	if want.ID != "" {
		task, ok := runtime.Tasks[want.ID]
		return task, ok
	}
	if want.Title == "" {
		return TaskStatus{}, false
	}
	for _, task := range runtime.Tasks {
		if task.Title == want.Title {
			return task, true
		}
	}
	return TaskStatus{}, false
}

func normalizedTaskState(task TaskStatus) string {
	if task.State == "" {
		return TaskPending
	}
	return task.State
}

func comparePrompt(index int, got PromptState, want PromptExpectation) error {
	if want.Empty {
		if got.Text != "" {
			return fmt.Errorf("script step %d prompt text = %q, want empty", index, got.Text)
		}
		return nil
	}
	if want.Text != "" && got.Text != want.Text {
		return fmt.Errorf("script step %d prompt text = %q, want %q", index, got.Text, want.Text)
	}
	if want.Expanded != "" && got.ExpandedText() != want.Expanded {
		return fmt.Errorf("script step %d prompt expanded = %q, want %q", index, got.ExpandedText(), want.Expanded)
	}
	if want.Cursor != nil && got.Cursor != *want.Cursor {
		return fmt.Errorf("script step %d prompt cursor = %d, want %d", index, got.Cursor, *want.Cursor)
	}
	return nil
}

func compareViewport(index int, got Viewport, want ViewportExpectation) error {
	if want.Offset != nil && got.Offset != *want.Offset {
		return fmt.Errorf("script step %d viewport offset = %d, want %d", index, got.Offset, *want.Offset)
	}
	visible := strings.Join(got.Visible(), "\n")
	if want.VisibleLineCount > 0 && len(got.Visible()) != want.VisibleLineCount {
		return fmt.Errorf("script step %d visible line count = %d, want %d", index, len(got.Visible()), want.VisibleLineCount)
	}
	for _, text := range want.VisibleContains {
		if !strings.Contains(visible, text) {
			return fmt.Errorf("script step %d viewport missing %q in %q", index, text, visible)
		}
	}
	for _, text := range want.VisibleNotContains {
		if strings.Contains(visible, text) {
			return fmt.Errorf("script step %d viewport unexpectedly contains %q in %q", index, text, visible)
		}
	}
	return nil
}

func compareScreen(index int, got REPLScreen, want ScreenExpectation) error {
	if want.Width > 0 && got.Width != want.Width {
		return fmt.Errorf("script step %d screen width = %d, want %d", index, got.Width, want.Width)
	}
	if want.Height > 0 && got.Height != want.Height {
		return fmt.Errorf("script step %d screen height = %d, want %d", index, got.Height, want.Height)
	}
	return nil
}

func compareReverseSearch(index int, got ReverseSearchState, want ReverseSearchExpectation) error {
	if got.Active != want.Active {
		return fmt.Errorf("script step %d reverse active = %v, want %v", index, got.Active, want.Active)
	}
	if want.Query != "" && got.Query != want.Query {
		return fmt.Errorf("script step %d reverse query = %q, want %q", index, got.Query, want.Query)
	}
	if want.Cursor != nil && got.Cursor != *want.Cursor {
		return fmt.Errorf("script step %d reverse cursor = %d, want %d", index, got.Cursor, *want.Cursor)
	}
	if want.ResultCount > 0 && len(got.Results) != want.ResultCount {
		return fmt.Errorf("script step %d reverse result count = %d, want %d", index, len(got.Results), want.ResultCount)
	}
	if want.NoResults && len(got.Results) != 0 {
		return fmt.Errorf("script step %d reverse results = %#v, want none", index, got.Results)
	}
	if want.Current != "" {
		current, _ := got.Current()
		if current != want.Current {
			return fmt.Errorf("script step %d reverse current = %q, want %q", index, current, want.Current)
		}
	}
	return nil
}

func compareEvent(index int, got ScreenEvent, want ScreenEvent) error {
	if got.Type != want.Type {
		return fmt.Errorf("script step %d event type = %q, want %q", index, got.Type, want.Type)
	}
	if want.Value != "" && got.Value != want.Value {
		return fmt.Errorf("script step %d event value = %q, want %q", index, got.Value, want.Value)
	}
	if want.DialogID != "" && got.DialogID != want.DialogID {
		return fmt.Errorf("script step %d dialog id = %q, want %q", index, got.DialogID, want.DialogID)
	}
	if want.DialogKind != "" && got.DialogKind != want.DialogKind {
		return fmt.Errorf("script step %d dialog kind = %q, want %q", index, got.DialogKind, want.DialogKind)
	}
	return nil
}

func compareEvents(index int, got []ScreenEvent, want []ScreenEvent) error {
	if len(got) != len(want) {
		return fmt.Errorf("script step %d event count = %d, want %d", index, len(got), len(want))
	}
	for eventIndex, expected := range want {
		if err := compareEvent(index, got[eventIndex], expected); err != nil {
			return fmt.Errorf("script step %d event %d mismatch: %w", index, eventIndex, err)
		}
	}
	return nil
}
