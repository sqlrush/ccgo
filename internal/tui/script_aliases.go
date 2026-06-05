package tui

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

type stringList []string
type intList []int

func (list *stringList) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*list = []string{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*list = many
	return nil
}

func stringListValue(list *stringList) []string {
	if list == nil {
		return nil
	}
	return []string(*list)
}

func (list *intList) UnmarshalJSON(data []byte) error {
	var single int
	if err := json.Unmarshal(data, &single); err == nil {
		*list = []int{single}
		return nil
	}
	var many []int
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*list = many
	return nil
}

func intListValue(list *intList) []int {
	if list == nil {
		return nil
	}
	return []int(*list)
}

func contentBlocksJSONField(fields map[string]json.RawMessage, names ...string) []contracts.ContentBlock {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var blocks []contracts.ContentBlock
		if err := json.Unmarshal(raw, &blocks); err == nil {
			return blocks
		}
		var block contracts.ContentBlock
		if err := json.Unmarshal(raw, &block); err == nil && block.Type != "" {
			return []contracts.ContentBlock{block}
		}
	}
	return nil
}

func pastedContentsJSONField(fields map[string]json.RawMessage, names ...string) map[int]session.PastedContent {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var byID map[int]session.PastedContent
		if err := json.Unmarshal(raw, &byID); err == nil && len(byID) > 0 {
			return normalizePastedContentIDs(byID)
		}
		var list []session.PastedContent
		if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
			return pastedContentListMap(list)
		}
		var single session.PastedContent
		if err := json.Unmarshal(raw, &single); err == nil && single.ID > 0 {
			return pastedContentListMap([]session.PastedContent{single})
		}
	}
	return nil
}

func normalizePastedContentIDs(contents map[int]session.PastedContent) map[int]session.PastedContent {
	out := make(map[int]session.PastedContent, len(contents))
	for id, content := range contents {
		if content.ID == 0 {
			content.ID = id
		}
		out[id] = content
	}
	return out
}

func pastedContentListMap(contents []session.PastedContent) map[int]session.PastedContent {
	out := make(map[int]session.PastedContent, len(contents))
	for _, content := range contents {
		if content.ID <= 0 {
			continue
		}
		out[content.ID] = content
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type scriptSize struct {
	Width  int
	Height int
}

func (size *scriptSize) UnmarshalJSON(data []byte) error {
	var pair []int
	if err := json.Unmarshal(data, &pair); err == nil {
		if len(pair) > 0 {
			size.Width = pair[0]
		}
		if len(pair) > 1 {
			size.Height = pair[1]
		}
		return nil
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if width := intPtrJSONField(fields, "width", "w", "columns", "cols", "screen_width", "screenWidth", "terminal_width", "terminalWidth", "resize_width", "resizeWidth"); width != nil {
		size.Width = *width
	}
	if height := intPtrJSONField(fields, "height", "h", "rows", "screen_height", "screenHeight", "terminal_height", "terminalHeight", "resize_height", "resizeHeight"); height != nil {
		size.Height = *height
	}
	return nil
}

func scriptSizeJSONField(fields map[string]json.RawMessage, names ...string) *scriptSize {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var size scriptSize
		if err := json.Unmarshal(raw, &size); err == nil {
			return &size
		}
	}
	return nil
}

func scriptFocusKey(focused bool) string {
	if focused {
		return "focus-in"
	}
	return "focus-out"
}

func permissionRequestJSONField(fields map[string]json.RawMessage, names ...string) *PermissionRequest {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if request, ok := scriptPermissionRequestFromJSON(raw, 0); ok {
			return request
		}
	}
	return nil
}

func taskStatusJSONField(fields map[string]json.RawMessage, names ...string) *TaskStatus {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if task, ok := scriptTaskStatusFromJSON(raw, 0); ok {
			return task
		}
	}
	return nil
}

func scriptKeybindingsJSONField(fields map[string]json.RawMessage) ([]BindingSpec, bool, error) {
	names := []string{
		"bindings",
		"keybindings",
		"key_bindings",
		"keyBindings",
		"keyboardBindings",
		"keyboardShortcuts",
		"keyboard_shortcuts",
		"keybinding_specs",
		"keybindingSpecs",
		"keymap",
		"keyMap",
		"keymaps",
		"keyMaps",
		"shortcutBindings",
		"hotkeys",
		"hotKeys",
		"hot_keys",
		"userKeybindings",
		"userKeyBindings",
		"user_keybindings",
		"customKeybindings",
		"customKeyBindings",
		"custom_keybindings",
		"keybinding",
		"keyBinding",
		"keybinding_config",
		"keybindingConfig",
		"keyboard",
		"keyboard_config",
		"keyboardConfig",
		"preferences",
		"userPreferences",
	}
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		specs, err := parseScriptKeybindingSpecs(raw)
		return specs, true, err
	}
	return nil, false, nil
}

func parseScriptKeybindingSpecs(data json.RawMessage) ([]BindingSpec, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}
	if len(data) > 0 && data[0] == '{' && isBindingSpecObject(data) {
		var spec BindingSpec
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil, err
		}
		return []BindingSpec{spec}, nil
	}
	return ParseKeyBindingSpecs(data)
}

func isBindingSpecObject(data json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return false
	}
	for _, name := range []string{
		"Key", "key", "keys", "key_sequence", "keySequence", "shortcut", "shortcut_key", "shortcutKey", "sequence",
		"Action", "action", "command", "action_name", "actionName", "command_name", "commandName", "command_id", "commandId",
	} {
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}

func (step *ScriptStep) UnmarshalJSON(data []byte) error {
	data = unwrapScriptStepJSON(data)
	data = mergeScriptStepExpectationJSON(data)
	rawStepData := data
	data = normalizeScriptStepJSON(data)
	type alias ScriptStep
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*step = ScriptStep(raw)

	var fields struct {
		RequestPermission         *PermissionRequest        `json:"request_permission"`
		RequestPermissionCamel    *PermissionRequest        `json:"requestPermission"`
		RawKey                    *string                   `json:"raw_key"`
		RawKeyCamel               *string                   `json:"rawKey"`
		KeySequence               *string                   `json:"key_sequence"`
		KeySequenceCamel          *string                   `json:"keySequence"`
		Press                     *string                   `json:"press"`
		PressKey                  *string                   `json:"press_key"`
		PressKeyCamel             *string                   `json:"pressKey"`
		KeyPress                  *string                   `json:"key_press"`
		KeyPressCamel             *string                   `json:"keyPress"`
		Keypress                  *string                   `json:"keypress"`
		Shortcut                  *string                   `json:"shortcut"`
		ShortcutKey               *string                   `json:"shortcut_key"`
		ShortcutKeyCamel          *string                   `json:"shortcutKey"`
		Presses                   []string                  `json:"presses"`
		KeyPresses                []string                  `json:"key_presses"`
		KeyPressesCamel           []string                  `json:"keyPresses"`
		Keypresses                []string                  `json:"keypresses"`
		Shortcuts                 []string                  `json:"shortcuts"`
		Input                     *string                   `json:"input"`
		InputText                 *string                   `json:"input_text"`
		InputTextCamel            *string                   `json:"inputText"`
		TextInput                 *string                   `json:"text_input"`
		TextInputCamel            *string                   `json:"textInput"`
		KeysText                  *string                   `json:"keys_text"`
		KeysTextCamel             *string                   `json:"keysText"`
		PasteText                 *string                   `json:"paste_text"`
		PasteTextCamel            *string                   `json:"pasteText"`
		PastedText                *string                   `json:"pasted_text"`
		PastedTextCamel           *string                   `json:"pastedText"`
		Clipboard                 *string                   `json:"clipboard"`
		Messages                  []Message                 `json:"messages"`
		AppendMessages            []Message                 `json:"append_messages"`
		AppendMessagesCamel       []Message                 `json:"appendMessages"`
		TranscriptMessages        []Message                 `json:"transcript_messages"`
		TranscriptMessagesCamel   []Message                 `json:"transcriptMessages"`
		Status                    *string                   `json:"status"`
		SetStatus                 *string                   `json:"set_status"`
		SetStatusCamel            *string                   `json:"setStatus"`
		StatusLine                *string                   `json:"status_line"`
		StatusLineCamel           *string                   `json:"statusLine"`
		BaseStatus                *string                   `json:"base_status"`
		BaseStatusCamel           *string                   `json:"baseStatus"`
		Mouse                     *ScriptMouse              `json:"mouse"`
		MouseEvent                *ScriptMouse              `json:"mouse_event"`
		MouseEventCamel           *ScriptMouse              `json:"mouseEvent"`
		Keybindings               []BindingSpec             `json:"keybindings"`
		KeyBindings               []BindingSpec             `json:"key_bindings"`
		KeyBindingsCamel          []BindingSpec             `json:"keyBindings"`
		KeybindingSpecs           []BindingSpec             `json:"keybinding_specs"`
		KeybindingSpecsCamel      []BindingSpec             `json:"keybindingSpecs"`
		UpsertTask                *TaskStatus               `json:"upsert_task"`
		UpsertTaskCamel           *TaskStatus               `json:"upsertTask"`
		RemoveTaskID              *json.RawMessage          `json:"remove_task_id"`
		RemoveTaskIDCamel         *json.RawMessage          `json:"removeTaskId"`
		RemoveTaskIDUpperCamel    *json.RawMessage          `json:"removeTaskID"`
		RemoveTask                *json.RawMessage          `json:"remove_task"`
		RemoveTaskCamel           *json.RawMessage          `json:"removeTask"`
		DeleteTask                *json.RawMessage          `json:"delete_task"`
		DeleteTaskCamel           *json.RawMessage          `json:"deleteTask"`
		CancelActiveDialog        *json.RawMessage          `json:"cancel_active_dialog"`
		CancelActiveDialogCamel   *json.RawMessage          `json:"cancelActiveDialog"`
		CancelActive              *json.RawMessage          `json:"cancel_active"`
		CancelActiveCamel         *json.RawMessage          `json:"cancelActive"`
		CancelDialog              *json.RawMessage          `json:"cancel_dialog"`
		CancelDialogCamel         *json.RawMessage          `json:"cancelDialog"`
		CloseDialog               *json.RawMessage          `json:"close_dialog"`
		CloseDialogCamel          *json.RawMessage          `json:"closeDialog"`
		CancelPermissionID        *json.RawMessage          `json:"cancel_permission_id"`
		CancelPermissionIDAlt     *json.RawMessage          `json:"cancelPermissionId"`
		CancelPermissionIDUpper   *json.RawMessage          `json:"cancelPermissionID"`
		CancelPermission          *json.RawMessage          `json:"cancel_permission"`
		CancelPermissionCamel     *json.RawMessage          `json:"cancelPermission"`
		CancelAllPermissions      *json.RawMessage          `json:"cancel_all_permissions"`
		CancelAllPermissionsCamel *json.RawMessage          `json:"cancelAllPermissions"`
		CancelPermissions         *json.RawMessage          `json:"cancel_permissions"`
		CancelPermissionsCamel    *json.RawMessage          `json:"cancelPermissions"`
		CancelAllTasks            *json.RawMessage          `json:"cancel_all_tasks"`
		CancelAllTasksCamel       *json.RawMessage          `json:"cancelAllTasks"`
		CancelTasks               *json.RawMessage          `json:"cancel_tasks"`
		CancelTasksCamel          *json.RawMessage          `json:"cancelTasks"`
		CancelTasksDetail         *json.RawMessage          `json:"cancel_tasks_detail"`
		CancelTasksDetailCamel    *json.RawMessage          `json:"cancelTasksDetail"`
		CancelReason              *json.RawMessage          `json:"cancel_reason"`
		CancelReasonCamel         *json.RawMessage          `json:"cancelReason"`
		OpenTasksDialog           *json.RawMessage          `json:"open_tasks_dialog"`
		OpenTasksDialogCamel      *json.RawMessage          `json:"openTasksDialog"`
		OpenTasks                 *json.RawMessage          `json:"open_tasks"`
		OpenTasksCamel            *json.RawMessage          `json:"openTasks"`
		ShowTasks                 *json.RawMessage          `json:"show_tasks"`
		ShowTasksCamel            *json.RawMessage          `json:"showTasks"`
		ResizeWidth               *int                      `json:"resize_width"`
		ResizeWidthCamel          *int                      `json:"resizeWidth"`
		ResizeHeight              *int                      `json:"resize_height"`
		ResizeHeightCamel         *int                      `json:"resizeHeight"`
		SnapshotName              *string                   `json:"snapshot_name"`
		SnapshotNameCamel         *string                   `json:"snapshotName"`
		Focus                     *bool                     `json:"focus"`
		Focused                   *bool                     `json:"focused"`
		FocusIn                   *bool                     `json:"focus_in"`
		FocusInCamel              *bool                     `json:"focusIn"`
		FocusOut                  *bool                     `json:"focus_out"`
		FocusOutCamel             *bool                     `json:"focusOut"`
		Blur                      *bool                     `json:"blur"`
		Blurred                   *bool                     `json:"blurred"`
		ExpectEvent               *ScreenEvent              `json:"expect_event"`
		ExpectEventCamel          *ScreenEvent              `json:"expectEvent"`
		ExpectEvents              []ScreenEvent             `json:"expect_events"`
		ExpectEventsCamel         []ScreenEvent             `json:"expectEvents"`
		ExpectNoEvent             *bool                     `json:"expect_no_event"`
		ExpectNoEventCamel        *bool                     `json:"expectNoEvent"`
		ExpectEventCount          *int                      `json:"expect_event_count"`
		ExpectEventCountCamel     *int                      `json:"expectEventCount"`
		ExpectTotalEventCount     *int                      `json:"expect_total_event_count"`
		ExpectTotalEventCamel     *int                      `json:"expectTotalEventCount"`
		ExpectDialogResult        *DialogResultExpectation  `json:"expect_dialog_result"`
		ExpectDialogResultCamel   *DialogResultExpectation  `json:"expectDialogResult"`
		ExpectDialogResults       []DialogResultExpectation `json:"expect_dialog_results"`
		ExpectDialogResultsCamel  []DialogResultExpectation `json:"expectDialogResults"`
		ExpectNoDialogResult      *bool                     `json:"expect_no_dialog_result"`
		ExpectNoDialogResultCamel *bool                     `json:"expectNoDialogResult"`
		ExpectNoDialogResults     *bool                     `json:"expect_no_dialog_results"`
		ExpectDialogResultCount   *int                      `json:"expect_dialog_result_count"`
		ExpectDialogCountCamel    *int                      `json:"expectDialogResultCount"`
		ExpectTotalDialogCount    *int                      `json:"expect_total_dialog_result_count"`
		ExpectTotalDialogCamel    *int                      `json:"expectTotalDialogResultCount"`
		ExpectDialog              *DialogExpectation        `json:"expect_dialog"`
		ExpectDialogCamel         *DialogExpectation        `json:"expectDialog"`
		ExpectPrompt              *PromptExpectation        `json:"expect_prompt"`
		ExpectPromptCamel         *PromptExpectation        `json:"expectPrompt"`
		ExpectVim                 *VimExpectation           `json:"expect_vim"`
		ExpectVimCamel            *VimExpectation           `json:"expectVim"`
		ExpectTasks               *TasksExpectation         `json:"expect_tasks"`
		ExpectTasksCamel          *TasksExpectation         `json:"expectTasks"`
		ExpectReverseSearch       *ReverseSearchExpectation `json:"expect_reverse_search"`
		ExpectReverseSearchCamel  *ReverseSearchExpectation `json:"expectReverseSearch"`
		ExpectViewport            *ViewportExpectation      `json:"expect_viewport"`
		ExpectViewportCamel       *ViewportExpectation      `json:"expectViewport"`
		ExpectScreen              *ScreenExpectation        `json:"expect_screen"`
		ExpectScreenCamel         *ScreenExpectation        `json:"expectScreen"`
		ExpectFocused             *bool                     `json:"expect_focused"`
		ExpectFocusedCamel        *bool                     `json:"expectFocused"`
		ExpectStatusContains      *stringList               `json:"expect_status_contains"`
		ExpectStatusContainsCamel *stringList               `json:"expectStatusContains"`
		ExpectStatusNotContains   *stringList               `json:"expect_status_not_contains"`
		ExpectStatusNotCamel      *stringList               `json:"expectStatusNotContains"`
		ExpectSnapshotContains    *stringList               `json:"expect_snapshot_contains"`
		ExpectSnapshotCamel       *stringList               `json:"expectSnapshotContains"`
		ExpectSnapshotNotContains *stringList               `json:"expect_snapshot_not_contains"`
		ExpectSnapshotNotCamel    *stringList               `json:"expectSnapshotNotContains"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	rawFieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(rawStepData, &rawFieldMap); err != nil {
		return err
	}
	if fields.RequestPermission != nil {
		step.RequestPermission = fields.RequestPermission
	}
	if fields.RequestPermissionCamel != nil {
		step.RequestPermission = fields.RequestPermissionCamel
	}
	if request := permissionRequestJSONField(fieldMap, "permission", "permission_request", "permissionRequest", "request"); request != nil {
		step.RequestPermission = request
	}
	if fields.RawKey != nil {
		step.Key = *fields.RawKey
	}
	if fields.RawKeyCamel != nil {
		step.Key = *fields.RawKeyCamel
	}
	if fields.KeySequence != nil {
		step.Key = *fields.KeySequence
	}
	if fields.KeySequenceCamel != nil {
		step.Key = *fields.KeySequenceCamel
	}
	if fields.Press != nil {
		step.Key = *fields.Press
	}
	if fields.PressKey != nil {
		step.Key = *fields.PressKey
	}
	if fields.PressKeyCamel != nil {
		step.Key = *fields.PressKeyCamel
	}
	if fields.KeyPress != nil {
		step.Key = *fields.KeyPress
	}
	if fields.KeyPressCamel != nil {
		step.Key = *fields.KeyPressCamel
	}
	if fields.Keypress != nil {
		step.Key = *fields.Keypress
	}
	if fields.Shortcut != nil {
		step.Key = *fields.Shortcut
	}
	if fields.ShortcutKey != nil {
		step.Key = *fields.ShortcutKey
	}
	if fields.ShortcutKeyCamel != nil {
		step.Key = *fields.ShortcutKeyCamel
	}
	if fields.Presses != nil {
		step.Keys = append(step.Keys, fields.Presses...)
	}
	if fields.KeyPresses != nil {
		step.Keys = append(step.Keys, fields.KeyPresses...)
	}
	if fields.KeyPressesCamel != nil {
		step.Keys = append(step.Keys, fields.KeyPressesCamel...)
	}
	if fields.Keypresses != nil {
		step.Keys = append(step.Keys, fields.Keypresses...)
	}
	if fields.Shortcuts != nil {
		step.Keys = append(step.Keys, fields.Shortcuts...)
	}
	if fields.Input != nil {
		step.Text = *fields.Input
	}
	if fields.InputText != nil {
		step.Text = *fields.InputText
	}
	if fields.InputTextCamel != nil {
		step.Text = *fields.InputTextCamel
	}
	if fields.TextInput != nil {
		step.Text = *fields.TextInput
	}
	if fields.TextInputCamel != nil {
		step.Text = *fields.TextInputCamel
	}
	if fields.KeysText != nil {
		step.Text = *fields.KeysText
	}
	if fields.KeysTextCamel != nil {
		step.Text = *fields.KeysTextCamel
	}
	if fields.PasteText != nil {
		step.Paste = *fields.PasteText
	}
	if fields.PasteTextCamel != nil {
		step.Paste = *fields.PasteTextCamel
	}
	if fields.PastedText != nil {
		step.Paste = *fields.PastedText
	}
	if fields.PastedTextCamel != nil {
		step.Paste = *fields.PastedTextCamel
	}
	if fields.Clipboard != nil {
		step.Paste = *fields.Clipboard
	}
	if fields.Messages != nil {
		step.Messages = fields.Messages
	}
	if fields.AppendMessages != nil {
		step.Messages = fields.AppendMessages
	}
	if fields.AppendMessagesCamel != nil {
		step.Messages = fields.AppendMessagesCamel
	}
	if fields.TranscriptMessages != nil {
		step.Messages = fields.TranscriptMessages
	}
	if fields.TranscriptMessagesCamel != nil {
		step.Messages = fields.TranscriptMessagesCamel
	}
	if fields.Status != nil {
		step.Status = *fields.Status
	}
	if fields.SetStatus != nil {
		step.Status = *fields.SetStatus
	}
	if fields.SetStatusCamel != nil {
		step.Status = *fields.SetStatusCamel
	}
	if fields.StatusLine != nil {
		step.Status = *fields.StatusLine
	}
	if fields.StatusLineCamel != nil {
		step.Status = *fields.StatusLineCamel
	}
	if fields.BaseStatus != nil {
		step.Status = *fields.BaseStatus
	}
	if fields.BaseStatusCamel != nil {
		step.Status = *fields.BaseStatusCamel
	}
	if fields.Mouse != nil {
		step.Mouse = fields.Mouse
	}
	if fields.MouseEvent != nil {
		step.Mouse = fields.MouseEvent
	}
	if fields.MouseEventCamel != nil {
		step.Mouse = fields.MouseEventCamel
	}
	if fields.Keybindings != nil {
		step.Keybindings = fields.Keybindings
	}
	if fields.KeyBindings != nil {
		step.Keybindings = fields.KeyBindings
	}
	if fields.KeyBindingsCamel != nil {
		step.Keybindings = fields.KeyBindingsCamel
	}
	if fields.KeybindingSpecs != nil {
		step.Keybindings = fields.KeybindingSpecs
	}
	if fields.KeybindingSpecsCamel != nil {
		step.Keybindings = fields.KeybindingSpecsCamel
	}
	if specs, ok, err := scriptKeybindingsJSONField(rawFieldMap); err != nil {
		return err
	} else if ok {
		step.Keybindings = specs
	}
	if fields.UpsertTask != nil {
		step.UpsertTask = fields.UpsertTask
	}
	if fields.UpsertTaskCamel != nil {
		step.UpsertTask = fields.UpsertTaskCamel
	}
	if task := taskStatusJSONField(fieldMap, "task", "task_status", "taskStatus"); task != nil {
		step.UpsertTask = task
	}
	if step.RemoveTaskID == "" && scriptHasAnyJSONField(fieldMap, "remove_task_id", "removeTaskId", "removeTaskID", "remove_task", "removeTask", "delete_task", "deleteTask") {
		step.RemoveTaskID = scriptActionIDField(fieldMap, "remove_task_id", "removeTaskId", "removeTaskID", "remove_task", "removeTask", "delete_task", "deleteTask", "task", "task_status", "taskStatus", "task_id", "taskId", "taskID", "job_id", "jobId", "jobID", "run_id", "runId", "runID", "id")
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "cancel_active_dialog", "cancelActiveDialog", "cancel_active", "cancelActive", "cancel_dialog", "cancelDialog", "close_dialog", "closeDialog"); ok {
		step.CancelActiveDialog = value
	}
	if step.CancelPermissionID == "" && scriptHasAnyJSONField(fieldMap, "cancel_permission_id", "cancelPermissionId", "cancelPermissionID", "cancel_permission", "cancelPermission") {
		step.CancelPermissionID = scriptActionIDField(fieldMap, "cancel_permission_id", "cancelPermissionId", "cancelPermissionID", "cancel_permission", "cancelPermission", "permission", "permission_request", "permissionRequest", "request", "permission_id", "permissionId", "permissionID", "request_id", "requestId", "requestID", "dialog_id", "dialogId", "dialogID", "tool_use_id", "toolUseId", "toolUseID", "operation_id", "operationId", "operationID", "id")
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "cancel_all_permissions", "cancelAllPermissions", "cancel_permissions", "cancelPermissions"); ok {
		step.CancelAllPermissions = value
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "cancel_all_tasks", "cancelAllTasks", "cancel_tasks", "cancelTasks"); ok {
		step.CancelAllTasks = value
	}
	if step.CancelTasksDetail == "" {
		step.CancelTasksDetail = scriptActionStringField(fieldMap, "cancel_all_tasks", "cancelAllTasks", "cancel_tasks", "cancelTasks", "cancel_tasks_detail", "cancelTasksDetail", "cancel_reason", "cancelReason", "reason", "reason_text", "reasonText", "detail", "message", "description", "body", "text", "status_text", "statusText")
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "open_tasks_dialog", "openTasksDialog", "open_tasks", "openTasks", "show_tasks", "showTasks"); ok {
		step.OpenTasksDialog = value
	}
	if fields.ResizeWidth != nil {
		step.ResizeWidth = *fields.ResizeWidth
	}
	if fields.ResizeWidthCamel != nil {
		step.ResizeWidth = *fields.ResizeWidthCamel
	}
	if fields.ResizeHeight != nil {
		step.ResizeHeight = *fields.ResizeHeight
	}
	if fields.ResizeHeightCamel != nil {
		step.ResizeHeight = *fields.ResizeHeightCamel
	}
	if size := scriptSizeJSONField(fieldMap, "resize", "resize_to", "resizeTo", "screen_size", "screenSize", "terminal_size", "terminalSize", "size"); size != nil {
		if size.Width > 0 {
			step.ResizeWidth = size.Width
		}
		if size.Height > 0 {
			step.ResizeHeight = size.Height
		}
	}
	if step.ResizeWidth <= 0 {
		if width := intPtrJSONField(fieldMap, "width", "columns", "cols", "screen_width", "screenWidth", "terminal_width", "terminalWidth"); width != nil {
			step.ResizeWidth = *width
		}
	}
	if step.ResizeHeight <= 0 {
		if height := intPtrJSONField(fieldMap, "height", "rows", "screen_height", "screenHeight", "terminal_height", "terminalHeight"); height != nil {
			step.ResizeHeight = *height
		}
	}
	if fields.SnapshotName != nil {
		step.SnapshotName = *fields.SnapshotName
	}
	if fields.SnapshotNameCamel != nil {
		step.SnapshotName = *fields.SnapshotNameCamel
	}
	if step.SnapshotName == "" {
		step.SnapshotName = stringJSONField(fieldMap, "snapshot", "snapshot_id", "snapshotId", "snapshot_label", "snapshotLabel", "capture_name", "captureName", "baseline_name", "baselineName")
	}
	if fields.Focus != nil {
		step.Keys = append(step.Keys, scriptFocusKey(*fields.Focus))
	}
	if fields.Focused != nil {
		step.Keys = append(step.Keys, scriptFocusKey(*fields.Focused))
	}
	if fields.FocusIn != nil && *fields.FocusIn {
		step.Keys = append(step.Keys, "focus-in")
	}
	if fields.FocusInCamel != nil && *fields.FocusInCamel {
		step.Keys = append(step.Keys, "focus-in")
	}
	if fields.FocusOut != nil && *fields.FocusOut {
		step.Keys = append(step.Keys, "focus-out")
	}
	if fields.FocusOutCamel != nil && *fields.FocusOutCamel {
		step.Keys = append(step.Keys, "focus-out")
	}
	if fields.Blur != nil && *fields.Blur {
		step.Keys = append(step.Keys, "focus-out")
	}
	if fields.Blurred != nil {
		step.Keys = append(step.Keys, scriptFocusKey(!*fields.Blurred))
	}
	if fields.ExpectEvent != nil {
		step.ExpectEvent = fields.ExpectEvent
	}
	if fields.ExpectEventCamel != nil {
		step.ExpectEvent = fields.ExpectEventCamel
	}
	if fields.ExpectEvents != nil {
		step.ExpectEvents = fields.ExpectEvents
	}
	if fields.ExpectEventsCamel != nil {
		step.ExpectEvents = fields.ExpectEventsCamel
	}
	if fields.ExpectNoEvent != nil {
		step.ExpectNoEvent = *fields.ExpectNoEvent
	}
	if fields.ExpectNoEventCamel != nil {
		step.ExpectNoEvent = *fields.ExpectNoEventCamel
	}
	if fields.ExpectEventCount != nil {
		step.ExpectEventCount = fields.ExpectEventCount
	}
	if fields.ExpectEventCountCamel != nil {
		step.ExpectEventCount = fields.ExpectEventCountCamel
	}
	if fields.ExpectTotalEventCount != nil {
		step.ExpectTotalEventCount = fields.ExpectTotalEventCount
	}
	if fields.ExpectTotalEventCamel != nil {
		step.ExpectTotalEventCount = fields.ExpectTotalEventCamel
	}
	if fields.ExpectDialogResult != nil {
		step.ExpectDialogResult = fields.ExpectDialogResult
	}
	if fields.ExpectDialogResultCamel != nil {
		step.ExpectDialogResult = fields.ExpectDialogResultCamel
	}
	if fields.ExpectDialogResults != nil {
		step.ExpectDialogResults = fields.ExpectDialogResults
	}
	if fields.ExpectDialogResultsCamel != nil {
		step.ExpectDialogResults = fields.ExpectDialogResultsCamel
	}
	if fields.ExpectNoDialogResult != nil {
		step.ExpectNoDialogResult = *fields.ExpectNoDialogResult
	}
	if fields.ExpectNoDialogResultCamel != nil {
		step.ExpectNoDialogResult = *fields.ExpectNoDialogResultCamel
	}
	if fields.ExpectNoDialogResults != nil {
		step.ExpectNoDialogResult = *fields.ExpectNoDialogResults
	}
	if fields.ExpectDialogResultCount != nil {
		step.ExpectDialogResultCount = fields.ExpectDialogResultCount
	}
	if fields.ExpectDialogCountCamel != nil {
		step.ExpectDialogResultCount = fields.ExpectDialogCountCamel
	}
	if fields.ExpectTotalDialogCount != nil {
		step.ExpectTotalDialogResultCount = fields.ExpectTotalDialogCount
	}
	if fields.ExpectTotalDialogCamel != nil {
		step.ExpectTotalDialogResultCount = fields.ExpectTotalDialogCamel
	}
	if fields.ExpectDialog != nil {
		step.ExpectDialog = fields.ExpectDialog
	}
	if fields.ExpectDialogCamel != nil {
		step.ExpectDialog = fields.ExpectDialogCamel
	}
	if fields.ExpectPrompt != nil {
		step.ExpectPrompt = fields.ExpectPrompt
	}
	if fields.ExpectPromptCamel != nil {
		step.ExpectPrompt = fields.ExpectPromptCamel
	}
	if fields.ExpectVim != nil {
		step.ExpectVim = fields.ExpectVim
	}
	if fields.ExpectVimCamel != nil {
		step.ExpectVim = fields.ExpectVimCamel
	}
	if fields.ExpectTasks != nil {
		step.ExpectTasks = fields.ExpectTasks
	}
	if fields.ExpectTasksCamel != nil {
		step.ExpectTasks = fields.ExpectTasksCamel
	}
	if fields.ExpectReverseSearch != nil {
		step.ExpectReverseSearch = fields.ExpectReverseSearch
	}
	if fields.ExpectReverseSearchCamel != nil {
		step.ExpectReverseSearch = fields.ExpectReverseSearchCamel
	}
	if fields.ExpectViewport != nil {
		step.ExpectViewport = fields.ExpectViewport
	}
	if fields.ExpectViewportCamel != nil {
		step.ExpectViewport = fields.ExpectViewportCamel
	}
	if fields.ExpectScreen != nil {
		step.ExpectScreen = fields.ExpectScreen
	}
	if fields.ExpectScreenCamel != nil {
		step.ExpectScreen = fields.ExpectScreenCamel
	}
	if fields.ExpectFocused != nil {
		step.ExpectFocused = fields.ExpectFocused
	}
	if fields.ExpectFocusedCamel != nil {
		step.ExpectFocused = fields.ExpectFocusedCamel
	}
	if fields.ExpectStatusContains != nil {
		step.ExpectStatusContains = stringListValue(fields.ExpectStatusContains)
	}
	if fields.ExpectStatusContainsCamel != nil {
		step.ExpectStatusContains = stringListValue(fields.ExpectStatusContainsCamel)
	}
	if fields.ExpectStatusNotContains != nil {
		step.ExpectStatusNotContains = stringListValue(fields.ExpectStatusNotContains)
	}
	if fields.ExpectStatusNotCamel != nil {
		step.ExpectStatusNotContains = stringListValue(fields.ExpectStatusNotCamel)
	}
	if fields.ExpectSnapshotContains != nil {
		step.ExpectSnapshotContains = stringListValue(fields.ExpectSnapshotContains)
	}
	if fields.ExpectSnapshotCamel != nil {
		step.ExpectSnapshotContains = stringListValue(fields.ExpectSnapshotCamel)
	}
	if fields.ExpectSnapshotNotContains != nil {
		step.ExpectSnapshotNotContains = stringListValue(fields.ExpectSnapshotNotContains)
	}
	if fields.ExpectSnapshotNotCamel != nil {
		step.ExpectSnapshotNotContains = stringListValue(fields.ExpectSnapshotNotCamel)
	}
	applyScriptStepActionAlias(step, fieldMap)
	return nil
}

func mergeScriptStepExpectationJSON(data []byte) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return data
	}
	changed := false
	for _, name := range []string{
		"expect",
		"expected",
		"expectation",
		"expectations",
		"assert",
		"asserts",
		"assertion",
		"assertions",
		"check",
		"checks",
		"verify",
		"verification",
		"then",
		"after",
	} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		if mergeScriptStepExpectationRaw(fields, raw) {
			changed = true
		}
	}
	if !changed {
		return data
	}
	merged, err := json.Marshal(fields)
	if err != nil {
		return data
	}
	return merged
}

func mergeScriptStepExpectationRaw(fields map[string]json.RawMessage, raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false
	}
	switch raw[0] {
	case '{':
		nested := map[string]json.RawMessage{}
		if err := json.Unmarshal(raw, &nested); err != nil {
			return false
		}
		return mergeScriptStepExpectationItem(fields, nested)
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return false
		}
		changed := false
		for _, item := range items {
			if mergeScriptStepExpectationRaw(fields, item) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func mergeScriptStepExpectationItem(fields map[string]json.RawMessage, item map[string]json.RawMessage) bool {
	changed := mergeScriptStepExpectationFields(fields, item)
	target := scriptStepExpectationItemTargetField(item)
	if target == "" {
		return changed
	}
	value, ok := scriptStepExpectationItemValue(item)
	if !ok {
		return changed
	}
	if _, exists := fields[target]; exists {
		return changed
	}
	fields[target] = value
	return true
}

func mergeScriptStepExpectationFields(fields map[string]json.RawMessage, nested map[string]json.RawMessage) bool {
	changed := false
	for name, raw := range nested {
		if target := scriptStepExpectationTargetField(name); target != "" {
			if _, exists := fields[target]; !exists {
				fields[target] = raw
				changed = true
			}
			continue
		}
		if strings.HasPrefix(name, "expect") || strings.HasPrefix(name, "Expect") {
			if _, exists := fields[name]; !exists {
				fields[name] = raw
				changed = true
			}
		}
	}
	return changed
}

func scriptStepExpectationItemTargetField(item map[string]json.RawMessage) string {
	for _, name := range []string{
		"target",
		"field",
		"expect",
		"expected",
		"assert",
		"assertion",
		"check",
		"verification",
		"kind",
		"name",
		"type",
	} {
		value := stringJSONField(item, name)
		if value == "" {
			continue
		}
		if target := scriptStepExpectationTargetField(value); target != "" {
			return target
		}
	}
	return ""
}

func scriptStepExpectationItemValue(item map[string]json.RawMessage) (json.RawMessage, bool) {
	for _, name := range []string{
		"value",
		"payload",
		"data",
		"body",
		"resource",
		"node",
		"attributes",
		"properties",
		"result",
		"response",
		"output",
		"expectedValue",
		"expected_value",
		"expected",
		"expectation",
		"assertionValue",
		"assertion_value",
		"checkValue",
		"check_value",
		"actual",
	} {
		raw, ok := item[name]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		return raw, true
	}
	return nil, false
}

func scriptStepExpectationTargetField(name string) string {
	switch canonicalScriptStepAction(name) {
	case "event":
		return "expectEvent"
	case "events":
		return "expectEvents"
	case "noevent", "no-event":
		return "expectNoEvent"
	case "eventcount", "event-count":
		return "expectEventCount"
	case "totaleventcount", "total-event-count":
		return "expectTotalEventCount"
	case "dialog":
		return "expectDialog"
	case "dialogresult", "dialog-result":
		return "expectDialogResult"
	case "dialogresults", "dialog-results":
		return "expectDialogResults"
	case "nodialogresult", "no-dialog-result", "nodialogresults", "no-dialog-results":
		return "expectNoDialogResult"
	case "dialogresultcount", "dialog-result-count":
		return "expectDialogResultCount"
	case "totaldialogresultcount", "total-dialog-result-count":
		return "expectTotalDialogResultCount"
	case "prompt", "input":
		return "expectPrompt"
	case "vim":
		return "expectVim"
	case "tasks":
		return "expectTasks"
	case "reversesearch", "reverse-search":
		return "expectReverseSearch"
	case "viewport":
		return "expectViewport"
	case "screen", "terminal":
		return "expectScreen"
	case "focused", "focus":
		return "expectFocused"
	case "statuscontains", "status-contains":
		return "expectStatusContains"
	case "statusnotcontains", "status-not-contains":
		return "expectStatusNotContains"
	case "snapshotcontains", "snapshot-contains":
		return "expectSnapshotContains"
	case "snapshotnotcontains", "snapshot-not-contains":
		return "expectSnapshotNotContains"
	default:
		return ""
	}
}

func unwrapScriptStepJSON(data []byte) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return data
	}
	for _, name := range []string{
		"step",
		"script_step",
		"scriptStep",
		"interaction_step",
		"interactionStep",
		"action_step",
		"actionStep",
		"record",
		"entry",
		"item",
		"event",
	} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) > 0 && raw[0] == '{' {
			return raw
		}
	}
	if scriptStepJSONHasDirectFields(fields) {
		return data
	}
	for _, name := range []string{
		"node",
		"resource",
		"edge",
		"attributes",
		"properties",
		"attrs",
	} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) > 0 && raw[0] == '{' {
			return unwrapScriptStepJSON(raw)
		}
	}
	return data
}

func scriptStepJSONHasDirectFields(fields map[string]json.RawMessage) bool {
	for _, name := range []string{
		"action",
		"step_action",
		"stepAction",
		"operation",
		"op",
		"command",
		"kind",
		"value",
		"key",
		"keys",
		"raw_key",
		"rawKey",
		"key_sequence",
		"keySequence",
		"press",
		"press_key",
		"pressKey",
		"key_press",
		"keyPress",
		"keypress",
		"shortcut",
		"shortcut_key",
		"shortcutKey",
		"presses",
		"key_presses",
		"keyPresses",
		"keypresses",
		"shortcuts",
		"text",
		"input",
		"input_text",
		"inputText",
		"text_input",
		"textInput",
		"keys_text",
		"keysText",
		"paste",
		"paste_text",
		"pasteText",
		"pasted_text",
		"pastedText",
		"clipboard",
		"messages",
		"append_messages",
		"appendMessages",
		"transcript_messages",
		"transcriptMessages",
		"status",
		"set_status",
		"setStatus",
		"status_line",
		"statusLine",
		"base_status",
		"baseStatus",
		"mouse",
		"mouse_event",
		"mouseEvent",
		"image",
		"request_permission",
		"requestPermission",
		"permission",
		"permissionRequest",
		"task",
		"task_status",
		"taskStatus",
		"dialog",
		"expect_event",
		"expectEvent",
		"expect_events",
		"expectEvents",
		"expect_prompt",
		"expectPrompt",
		"expect_dialog",
		"expectDialog",
		"expect_screen",
		"expectScreen",
		"expect_viewport",
		"expectViewport",
		"expect_vim",
		"expectVim",
		"snapshot",
		"snapshot_name",
		"snapshotName",
		"keybindings",
		"key_bindings",
		"keyBindings",
		"keymap",
		"keyMap",
		"keyboardShortcuts",
		"hotkeys",
	} {
		if raw, ok := fields[name]; ok && len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
			return true
		}
	}
	return false
}

func applyScriptStepActionAlias(step *ScriptStep, fields map[string]json.RawMessage) {
	action := canonicalScriptStepAction(stringJSONField(fields,
		"action",
		"step_action",
		"stepAction",
		"operation",
		"op",
		"command",
		"kind",
		"name",
		"type",
	))
	switch action {
	case "key", "press", "presskey", "keypress", "key-press", "shortcut", "shortcutkey", "shortcut-key":
		if step.Key == "" && len(step.Keys) == 0 {
			if values := stringListJSONField(fields, "value", "payload", "data", "key", "keys", "shortcut", "sequence", "input", "text"); len(values) == 1 {
				step.Key = values[0]
			} else if len(values) > 1 {
				step.Keys = append(step.Keys, values...)
			}
		}
	case "keys", "presses", "shortcuts", "sequence", "keysequence", "key-sequence", "keyseq", "key-seq":
		if step.Key == "" && len(step.Keys) == 0 {
			step.Keys = append(step.Keys, stringListJSONField(fields, "value", "payload", "data", "keys", "sequence", "key_sequence", "keySequence", "shortcuts")...)
		}
	case "text", "type", "typetext", "type-text", "input", "inputtext", "textinput", "insert", "inserttext", "write", "writetext":
		if step.Text == "" {
			step.Text = stringJSONField(fields, "value", "text", "input", "content", "body", "message", "data", "payload")
		}
	case "paste", "pastetext", "pastedtext", "clipboard", "clipboardtext":
		if step.Paste == "" {
			step.Paste = stringJSONField(fields, "value", "paste", "clipboard", "text", "content", "data", "payload")
		}
	case "status", "setstatus", "set-status", "statusline", "status-line":
		if step.Status == "" {
			step.Status = stringJSONField(fields, "value", "status", "text", "content", "message", "data", "payload")
		}
	case "snapshot", "capture", "capture-snapshot":
		if step.SnapshotName == "" {
			step.SnapshotName = stringJSONField(fields, "value", "snapshot", "name", "label", "id", "data", "payload")
		}
	case "resize", "terminalsize", "terminal-size", "screensize", "screen-size":
		if step.ResizeWidth <= 0 || step.ResizeHeight <= 0 {
			if size := scriptSizeJSONField(fields, "value", "size", "dimensions", "payload", "data"); size != nil {
				if step.ResizeWidth <= 0 && size.Width > 0 {
					step.ResizeWidth = size.Width
				}
				if step.ResizeHeight <= 0 && size.Height > 0 {
					step.ResizeHeight = size.Height
				}
			}
		}
	case "mouse", "mouseevent", "mouse-event":
		if step.Mouse == nil {
			step.Mouse = scriptMouseJSONField(fields, "value", "mouse", "event", "payload", "data")
		}
	case "image", "pasteimage", "paste-image", "imagepaste", "image-paste":
		if step.Image == nil {
			step.Image = scriptImageJSONField(fields, "value", "image", "payload", "data")
		}
	case "permission", "permissionrequest", "permission-request", "requestpermission", "request-permission":
		if step.RequestPermission == nil {
			step.RequestPermission = scriptPermissionRequestActionField(fields)
		}
	case "task", "taskstatus", "task-status", "upserttask", "upsert-task":
		if step.UpsertTask == nil {
			step.UpsertTask = scriptTaskStatusActionField(fields)
		}
	case "removetask", "remove-task", "deletetask", "delete-task":
		if step.RemoveTaskID == "" {
			step.RemoveTaskID = scriptActionIDField(fields, "task", "task_status", "taskStatus", "task_id", "taskId", "taskID", "job_id", "jobId", "jobID", "run_id", "runId", "runID", "id")
		}
	case "opentasks", "open-tasks", "opentasksdialog", "open-tasks-dialog", "showtasks", "show-tasks":
		step.OpenTasksDialog = scriptActionBoolField(fields, true)
	case "cancelactivedialog", "cancel-active-dialog", "canceldialog", "cancel-dialog", "closedialog", "close-dialog":
		step.CancelActiveDialog = scriptActionBoolField(fields, true)
	case "cancelpermission", "cancel-permission":
		if step.CancelPermissionID == "" {
			step.CancelPermissionID = scriptActionIDField(fields, "permission", "permission_request", "permissionRequest", "request", "permission_id", "permissionId", "permissionID", "request_id", "requestId", "requestID", "dialog_id", "dialogId", "dialogID", "tool_use_id", "toolUseId", "toolUseID", "operation_id", "operationId", "operationID", "id")
		}
	case "cancelpermissions", "cancel-permissions", "cancelallpermissions", "cancel-all-permissions":
		step.CancelAllPermissions = scriptActionBoolField(fields, true)
	case "canceltasks", "cancel-tasks", "cancelalltasks", "cancel-all-tasks":
		step.CancelAllTasks = scriptActionBoolField(fields, true)
		if step.CancelTasksDetail == "" {
			step.CancelTasksDetail = scriptActionStringField(fields, "cancel_reason", "cancelReason", "reason", "reason_text", "reasonText", "detail", "message", "description", "body", "text", "status_text", "statusText")
		}
	case "dialog", "showdialog", "show-dialog", "opendialog", "open-dialog":
		if step.Dialog == nil {
			step.Dialog = scriptDialogActionField(fields)
		}
	case "focus", "focusin", "focus-in":
		if !scriptStepHasFocusKey(step) {
			step.Keys = append(step.Keys, "focus-in")
		}
	case "blur", "focusout", "focus-out":
		if !scriptStepHasFocusKey(step) {
			step.Keys = append(step.Keys, "focus-out")
		}
	}
}

func canonicalScriptStepAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	action = strings.ReplaceAll(action, "_", "-")
	action = strings.ReplaceAll(action, " ", "-")
	return action
}

func scriptPermissionRequestActionField(fields map[string]json.RawMessage) *PermissionRequest {
	for _, raw := range scriptActionRawFields(fields, "request", "permission", "permission_request", "permissionRequest") {
		if request, ok := scriptPermissionRequestFromJSON(raw, 0); ok {
			return request
		}
	}
	var request PermissionRequest
	if scriptUnmarshalStepFields(fields, &request) && scriptPermissionRequestHasData(request) {
		return &request
	}
	return nil
}

func scriptPermissionRequestHasData(request PermissionRequest) bool {
	return request.ID != "" || request.ToolName != "" || request.Path != "" || request.Description != "" || len(request.Actions) > 0
}

func scriptTaskStatusActionField(fields map[string]json.RawMessage) *TaskStatus {
	for _, raw := range scriptActionRawFields(fields, "task", "task_status", "taskStatus") {
		if task, ok := scriptTaskStatusFromJSON(raw, 0); ok {
			return task
		}
	}
	var task TaskStatus
	if scriptUnmarshalStepFields(fields, &task) && scriptTaskStatusHasData(task) {
		return &task
	}
	return nil
}

func scriptTaskStatusHasData(task TaskStatus) bool {
	return task.ID != "" || task.Title != "" || task.State != "" || task.Detail != "" || task.Progress != 0
}

func scriptPermissionRequestFromJSON(raw json.RawMessage, depth int) (*PermissionRequest, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	var request PermissionRequest
	hasRequest := json.Unmarshal(raw, &request) == nil && scriptPermissionRequestHasData(request)
	if depth >= 8 {
		if hasRequest {
			return &request, true
		}
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if request, ok := scriptPermissionRequestFromJSON(item, depth+1); ok {
				return request, true
			}
		}
		return nil, false
	}
	if raw[0] != '{' {
		if hasRequest {
			return &request, true
		}
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		if hasRequest {
			return &request, true
		}
		return nil, false
	}
	if hasRequest && !scriptPermissionRequestShouldTryWrappers(fields, request) {
		return &request, true
	}
	for _, name := range scriptRuntimePayloadWrapperNames("request", "permission", "permission_request", "permissionRequest") {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if request, ok := scriptPermissionRequestFromJSON(nested, depth+1); ok {
			if request.ID == "" {
				request.ID = scalarStringJSONField(fields, "request_id", "requestId", "requestID", "permission_id", "permissionId", "permissionID", "tool_use_id", "toolUseId", "toolUseID", "operation_id", "operationId", "operationID", "id")
			}
			return request, true
		}
	}
	if hasRequest {
		return &request, true
	}
	return nil, false
}

func scriptTaskStatusFromJSON(raw json.RawMessage, depth int) (*TaskStatus, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	var task TaskStatus
	hasTask := json.Unmarshal(raw, &task) == nil && scriptTaskStatusHasData(task)
	if depth >= 8 {
		if hasTask {
			return &task, true
		}
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if task, ok := scriptTaskStatusFromJSON(item, depth+1); ok {
				return task, true
			}
		}
		return nil, false
	}
	if raw[0] != '{' {
		if hasTask {
			return &task, true
		}
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		if hasTask {
			return &task, true
		}
		return nil, false
	}
	if hasTask && !scriptTaskStatusShouldTryWrappers(fields, task) {
		return &task, true
	}
	for _, name := range scriptRuntimePayloadWrapperNames("task", "task_status", "taskStatus") {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if task, ok := scriptTaskStatusFromJSON(nested, depth+1); ok {
			if task.ID == "" {
				task.ID = scalarStringJSONField(fields, "task_id", "taskId", "taskID", "job_id", "jobId", "jobID", "run_id", "runId", "runID", "id")
			}
			return task, true
		}
	}
	if hasTask {
		return &task, true
	}
	return nil, false
}

func scriptPermissionRequestShouldTryWrappers(fields map[string]json.RawMessage, request PermissionRequest) bool {
	return request.ID != "" &&
		request.ToolName == "" &&
		request.Path == "" &&
		request.Description == "" &&
		len(request.Actions) == 0 &&
		scriptHasStructuredRuntimePayloadWrapper(fields)
}

func scriptTaskStatusShouldTryWrappers(fields map[string]json.RawMessage, task TaskStatus) bool {
	return task.ID != "" &&
		task.Title == "" &&
		task.State == "" &&
		task.Detail == "" &&
		task.Progress == 0 &&
		scriptHasStructuredRuntimePayloadWrapper(fields)
}

func scriptHasStructuredRuntimePayloadWrapper(fields map[string]json.RawMessage) bool {
	for _, name := range scriptRuntimePayloadWrapperNames() {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) > 0 && (raw[0] == '{' || raw[0] == '[') {
			return true
		}
	}
	return false
}

func scriptRuntimePayloadWrapperNames(names ...string) []string {
	wrappers := []string{
		"value",
		"payload",
		"data",
		"body",
		"result",
		"response",
		"output",
		"resource",
		"node",
		"edge",
		"attributes",
		"properties",
		"attrs",
		"record",
		"entry",
		"item",
		"items",
		"records",
		"entries",
		"nodes",
		"edges",
		"results",
	}
	return append(wrappers, names...)
}

func scriptDialogActionField(fields map[string]json.RawMessage) *Dialog {
	for _, raw := range scriptActionRawFields(fields, "dialog") {
		var dialog Dialog
		if err := json.Unmarshal(raw, &dialog); err == nil {
			return &dialog
		}
	}
	var dialog Dialog
	if scriptUnmarshalStepFields(fields, &dialog) && scriptDialogHasData(dialog) {
		return &dialog
	}
	return nil
}

func scriptDialogHasData(dialog Dialog) bool {
	return dialog.ID != "" || dialog.Kind != "" || dialog.Title != "" || dialog.Body != "" || len(dialog.Actions) > 0 || dialog.Focused != 0
}

func scriptActionRawFields(fields map[string]json.RawMessage, names ...string) []json.RawMessage {
	allNames := append([]string{"value", "payload", "data", "body"}, names...)
	raws := make([]json.RawMessage, 0, len(allNames))
	for _, name := range allNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		raws = append(raws, raw)
	}
	return raws
}

func scriptUnmarshalStepFields(fields map[string]json.RawMessage, target any) bool {
	raw, err := json.Marshal(fields)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}

func scriptActionIDField(fields map[string]json.RawMessage, idNames ...string) string {
	if value := scalarStringJSONField(fields, append([]string{"value", "payload", "data", "body"}, idNames...)...); value != "" {
		return value
	}
	for _, raw := range scriptActionRawFields(fields) {
		if value := scriptActionIDFromJSON(raw, idNames, 0); value != "" {
			return value
		}
	}
	for _, name := range idNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if value := scriptActionIDFromJSON(raw, idNames, 0); value != "" {
			return value
		}
	}
	return ""
}

func scriptActionStringField(fields map[string]json.RawMessage, objectNames ...string) string {
	if value := stringJSONField(fields, append([]string{"value", "payload", "data", "body"}, objectNames...)...); value != "" {
		return value
	}
	for _, raw := range scriptActionRawFields(fields) {
		if value := scriptActionStringFromJSON(raw, objectNames, 0); value != "" {
			return value
		}
	}
	for _, name := range objectNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if value := scriptActionStringFromJSON(raw, objectNames, 0); value != "" {
			return value
		}
	}
	return ""
}

func scriptRuntimeMutationBoolField(fields map[string]json.RawMessage, names ...string) (bool, bool) {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if value, ok := scriptActionBoolFromJSON(raw, 0); ok {
			return value, true
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) > 0 && (raw[0] == '{' || raw[0] == '[') {
			return true, true
		}
	}
	return false, false
}

func scriptActionBoolFromJSON(raw json.RawMessage, depth int) (bool, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false, false
	}
	if value, ok := scriptParseJSONBool(raw); ok {
		return value, true
	}
	if depth >= 8 {
		return false, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return false, false
		}
		for _, item := range items {
			if value, ok := scriptActionBoolFromJSON(item, depth+1); ok {
				return value, true
			}
		}
		return false, false
	}
	if raw[0] != '{' {
		return false, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false, false
	}
	for _, name := range scriptRuntimePayloadWrapperNames("enabled", "active", "open", "visible", "checked", "selected", "value") {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if value, ok := scriptActionBoolFromJSON(nested, depth+1); ok {
			return value, true
		}
	}
	return false, false
}

func scriptHasAnyJSONField(fields map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}

func scriptActionIDFromJSON(raw json.RawMessage, idNames []string, depth int) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if value := scriptScalarStringFromJSON(raw); value != "" {
		return value
	}
	if depth >= 8 {
		return ""
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return ""
		}
		for _, item := range items {
			if value := scriptActionIDFromJSON(item, idNames, depth+1); value != "" {
				return value
			}
		}
		return ""
	}
	if raw[0] != '{' {
		return ""
	}
	nested := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &nested); err != nil {
		return ""
	}
	if value := scalarStringJSONField(nested, idNames...); value != "" {
		return value
	}
	for _, name := range scriptRuntimePayloadWrapperNames(idNames...) {
		raw, ok := nested[name]
		if !ok {
			continue
		}
		if value := scriptActionIDFromJSON(raw, idNames, depth+1); value != "" {
			return value
		}
	}
	return ""
}

func scriptActionStringFromJSON(raw json.RawMessage, objectNames []string, depth int) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if value := scriptStringFromJSON(raw); value != "" {
		return value
	}
	if depth >= 8 {
		return ""
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return ""
		}
		for _, item := range items {
			if value := scriptActionStringFromJSON(item, objectNames, depth+1); value != "" {
				return value
			}
		}
		return ""
	}
	if raw[0] != '{' {
		return ""
	}
	nested := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &nested); err != nil {
		return ""
	}
	if value := stringJSONField(nested, objectNames...); value != "" {
		return value
	}
	for _, name := range scriptRuntimePayloadWrapperNames(objectNames...) {
		raw, ok := nested[name]
		if !ok {
			continue
		}
		if value := scriptActionStringFromJSON(raw, objectNames, depth+1); value != "" {
			return value
		}
	}
	return ""
}

func scriptScalarStringFromJSON(raw json.RawMessage) string {
	if value := scriptStringFromJSON(raw); value != "" {
		return value
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var scalar any
	if err := decoder.Decode(&scalar); err != nil {
		return ""
	}
	if value, ok := scalar.(json.Number); ok {
		return value.String()
	}
	return ""
}

func scriptStringFromJSON(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return ""
}

func scriptActionBoolField(fields map[string]json.RawMessage, fallback bool) bool {
	for _, name := range []string{"value", "payload", "data", "body", "enabled", "active", "open"} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if value, ok := scriptActionBoolFromJSON(raw, 0); ok {
			return value
		}
	}
	for _, raw := range scriptActionRawFields(fields) {
		if value, ok := scriptActionBoolFromJSON(raw, 0); ok {
			return value
		}
	}
	return fallback
}

func scriptMouseJSONField(fields map[string]json.RawMessage, names ...string) *ScriptMouse {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var mouse ScriptMouse
		if err := json.Unmarshal(raw, &mouse); err == nil {
			return &mouse
		}
	}
	return nil
}

func scriptImageJSONField(fields map[string]json.RawMessage, names ...string) *ScriptImage {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var image ScriptImage
		if err := json.Unmarshal(raw, &image); err == nil {
			return &image
		}
	}
	return nil
}

func scriptStepHasFocusKey(step *ScriptStep) bool {
	for _, key := range step.Keys {
		if key == "focus-in" || key == "focus-out" {
			return true
		}
	}
	return step.Key == "focus-in" || step.Key == "focus-out"
}

func normalizeScriptStepJSON(data []byte) []byte {
	data = normalizeStringFieldsToArray(data,
		"keys",
		"presses",
		"key_presses",
		"keyPresses",
		"keypresses",
		"shortcuts",
		"expect_status_contains",
		"expectStatusContains",
		"expect_status_not_contains",
		"expectStatusNotContains",
		"expect_snapshot_contains",
		"expectSnapshotContains",
		"expect_snapshot_not_contains",
		"expectSnapshotNotContains",
	)
	data = normalizeBoolFields(data,
		"cancel_active_dialog",
		"cancelActiveDialog",
		"cancel_active",
		"cancelActive",
		"cancel_dialog",
		"cancelDialog",
		"close_dialog",
		"closeDialog",
		"cancel_all_permissions",
		"cancelAllPermissions",
		"cancel_permissions",
		"cancelPermissions",
		"cancel_all_tasks",
		"cancelAllTasks",
		"cancel_tasks",
		"cancelTasks",
		"open_tasks_dialog",
		"openTasksDialog",
		"open_tasks",
		"openTasks",
		"show_tasks",
		"showTasks",
		"focus",
		"focused",
		"focus_in",
		"focusIn",
		"focus_out",
		"focusOut",
		"blur",
		"blurred",
		"expect_no_event",
		"expectNoEvent",
		"expect_no_dialog_result",
		"expectNoDialogResult",
		"expect_no_dialog_results",
		"expect_focused",
		"expectFocused",
	)
	return normalizeObjectFieldsToArray(data,
		"keybindings",
		"key_bindings",
		"keyBindings",
		"keybinding_specs",
		"keybindingSpecs",
		"messages",
		"append_messages",
		"appendMessages",
		"transcript_messages",
		"transcriptMessages",
		"expect_events",
		"expectEvents",
		"expect_dialog_results",
		"expectDialogResults",
	)
}

func normalizeStringFieldsToArray(data []byte, names ...string) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return data
	}
	changed := false
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if normalized, ok := normalizeStringFieldToArray(raw); ok {
			fields[name] = normalized
			changed = true
		}
	}
	if !changed {
		return data
	}
	normalized, err := json.Marshal(fields)
	if err != nil {
		return data
	}
	return normalized
}

func normalizeBoolFields(data []byte, names ...string) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return data
	}
	changed := false
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		value, ok := scriptParseJSONBool(raw)
		if !ok {
			continue
		}
		if value {
			fields[name] = json.RawMessage("true")
		} else {
			fields[name] = json.RawMessage("false")
		}
		changed = true
	}
	if !changed {
		return data
	}
	normalized, err := json.Marshal(fields)
	if err != nil {
		return data
	}
	return normalized
}

func normalizeStringFieldToArray(raw json.RawMessage) (json.RawMessage, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw, false
	}
	normalized, err := json.Marshal([]string{value})
	if err != nil {
		return raw, false
	}
	return normalized, true
}

func normalizeObjectFieldsToArray(data []byte, names ...string) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return data
	}
	changed := false
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if normalized, ok := normalizeObjectFieldToArray(raw); ok {
			fields[name] = normalized
			changed = true
		}
	}
	if !changed {
		return data
	}
	normalized, err := json.Marshal(fields)
	if err != nil {
		return data
	}
	return normalized
}

func normalizeObjectFieldToArray(raw json.RawMessage) (json.RawMessage, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return raw, false
	}
	normalized := make([]byte, 0, len(raw)+2)
	normalized = append(normalized, '[')
	normalized = append(normalized, raw...)
	normalized = append(normalized, ']')
	return normalized, true
}

func (image *ScriptImage) UnmarshalJSON(data []byte) error {
	type alias ScriptImage
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*image = ScriptImage(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if image.Filename == "" {
		image.Filename = stringJSONField(fields, "file_name", "fileName", "name")
	}
	if image.MediaType == "" {
		image.MediaType = stringJSONField(fields, "media_type", "mime_type", "mimeType", "content_type", "contentType")
	}
	if image.Content == "" {
		image.Content = stringJSONField(fields, "data", "base64", "base64Content", "contentBase64")
	}
	if image.SourcePath == "" {
		image.SourcePath = stringJSONField(fields, "source_path", "sourcePath", "path", "filePath", "file_path")
	}
	if image.Dimensions == nil {
		image.Dimensions = imageDimensionsJSONField(fields, "dimensions", "imageDimensions", "image_dimensions")
	}
	return nil
}

func (mouse *ScriptMouse) UnmarshalJSON(data []byte) error {
	data = normalizeBoolFields(data,
		"Release",
		"release",
		"released",
		"is_release",
		"isRelease",
		"mouse_release",
		"mouseRelease",
		"mouse_up",
		"mouseUp",
		"up",
		"release_event",
		"releaseEvent",
		"released_event",
		"releasedEvent",
	)
	type alias ScriptMouse
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*mouse = ScriptMouse(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if button := intPtrJSONField(fields, "button_code", "buttonCode", "button_mask", "buttonMask", "mouse_button", "mouseButton", "button", "btn", "code", "mask"); button != nil {
		mouse.Button = *button
	}
	if x := intPtrJSONField(fields, "column", "col", "mouse_x", "mouseX", "client_x", "clientX", "screen_x", "screenX", "page_x", "pageX", "offset_x", "offsetX", "viewport_x", "viewportX"); x != nil {
		mouse.X = *x
	}
	if y := intPtrJSONField(fields, "row", "line", "mouse_y", "mouseY", "client_y", "clientY", "screen_y", "screenY", "page_y", "pageY", "offset_y", "offsetY", "viewport_y", "viewportY"); y != nil {
		mouse.Y = *y
	}
	if release := boolPtrJSONField(fields, "released", "is_release", "isRelease", "mouse_release", "mouseRelease", "mouse_up", "mouseUp", "up", "release_event", "releaseEvent", "released_event", "releasedEvent"); release != nil {
		mouse.Release = *release
	}
	return nil
}

func (event *ScreenEvent) UnmarshalJSON(data []byte) error {
	type alias ScreenEvent
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*event = ScreenEvent(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if event.Type == "" {
		if eventType := stringJSONField(fields, "event_type", "eventType", "event", "name"); eventType != "" {
			event.Type = ScreenEventType(eventType)
		}
	}
	if event.Value == "" {
		event.Value = stringJSONField(fields, "payload", "text", "message", "data")
	}
	if event.DialogID == "" {
		event.DialogID = scalarStringJSONField(fields, "dialog_id", "dialogId", "dialogID", "permission_id", "permissionId", "permissionID", "request_id", "requestId", "requestID", "tool_use_id", "toolUseId", "toolUseID", "operation_id", "operationId", "operationID")
	}
	if event.DialogKind == "" {
		if dialogKind := stringJSONField(fields, "dialog_kind", "dialogKind"); dialogKind != "" {
			event.DialogKind = DialogKind(dialogKind)
		}
	}
	return nil
}

func (expect *DialogResultExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeBoolFields(data,
		"Found",
		"found",
		"Stale",
		"stale",
		"exists",
		"matched",
		"is_stale",
		"isStale",
	)
	type alias DialogResultExpectation
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*expect = DialogResultExpectation(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if expect.ID == "" {
		expect.ID = scalarStringJSONField(fields, "dialog_id", "dialogId", "dialogID", "permission_id", "permissionId", "permissionID", "request_id", "requestId", "requestID", "tool_use_id", "toolUseId", "toolUseID", "operation_id", "operationId", "operationID")
	}
	if expect.Kind == "" {
		if dialogKind := stringJSONField(fields, "dialog_kind", "dialogKind"); dialogKind != "" {
			expect.Kind = DialogKind(dialogKind)
		}
	}
	if expect.Action == "" {
		expect.Action = stringJSONField(fields, "value", "action_value", "actionValue", "selected_action", "selectedAction")
	}
	if expect.Status == "" {
		if status := stringJSONField(fields, "result_status", "resultStatus", "state"); status != "" {
			expect.Status = DialogResultStatus(status)
		}
	}
	if expect.Found == nil {
		expect.Found = boolPtrJSONField(fields, "exists", "matched")
	}
	if expect.Stale == nil {
		expect.Stale = boolPtrJSONField(fields, "is_stale", "isStale")
	}
	return nil
}

func (expect *DialogExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeBoolFields(data,
		"Active",
		"active",
		"is_active",
		"isActive",
		"visible",
		"exists",
		"present",
	)
	data = normalizeStringFieldsToArray(data,
		"actions",
		"Actions",
		"body_contains",
		"bodyContains",
		"body_not_contains",
		"bodyNotContains",
		"action_contains",
		"actionContains",
		"action_not_contains",
		"actionNotContains",
	)
	type alias DialogExpectation
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*expect = DialogExpectation(raw)

	var fields struct {
		BodyContains        *stringList `json:"body_contains"`
		BodyContainsCamel   *stringList `json:"bodyContains"`
		BodyNotContains     *stringList `json:"body_not_contains"`
		BodyNotCamel        *stringList `json:"bodyNotContains"`
		Actions             *stringList `json:"actions"`
		ActionContains      *stringList `json:"action_contains"`
		ActionContainsCamel *stringList `json:"actionContains"`
		ActionNotContains   *stringList `json:"action_not_contains"`
		ActionNotCamel      *stringList `json:"actionNotContains"`
		ActionCount         *int        `json:"action_count"`
		ActionCountCamel    *int        `json:"actionCount"`
		ActionsCount        *int        `json:"actions_count"`
		ActionsCountCamel   *int        `json:"actionsCount"`
		FocusedIndex        *int        `json:"focused_index"`
		FocusedIndexCamel   *int        `json:"focusedIndex"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if active := boolPtrJSONField(fieldMap, "is_active", "isActive", "visible", "exists", "present"); active != nil {
		expect.Active = *active
	}
	if expect.ID == "" {
		expect.ID = scalarStringJSONField(fieldMap, "dialog_id", "dialogId", "dialogID", "permission_id", "permissionId", "permissionID", "request_id", "requestId", "requestID", "tool_use_id", "toolUseId", "toolUseID", "operation_id", "operationId", "operationID")
	}
	if expect.Kind == "" {
		if dialogKind := stringJSONField(fieldMap, "dialog_kind", "dialogKind"); dialogKind != "" {
			expect.Kind = DialogKind(dialogKind)
		}
	}
	if expect.Title == "" {
		expect.Title = stringJSONField(fieldMap, "heading", "header", "label", "name")
	}
	if expect.Body == "" {
		expect.Body = stringJSONField(fieldMap, "content", "text", "message", "description")
	}
	if fields.BodyContains != nil {
		expect.BodyContains = stringListValue(fields.BodyContains)
	}
	if fields.BodyContainsCamel != nil {
		expect.BodyContains = stringListValue(fields.BodyContainsCamel)
	}
	if fields.BodyNotContains != nil {
		expect.BodyNotContains = stringListValue(fields.BodyNotContains)
	}
	if fields.BodyNotCamel != nil {
		expect.BodyNotContains = stringListValue(fields.BodyNotCamel)
	}
	if fields.Actions != nil {
		expect.Actions = stringListValue(fields.Actions)
	}
	if fields.ActionContains != nil {
		expect.ActionContains = stringListValue(fields.ActionContains)
	}
	if fields.ActionContainsCamel != nil {
		expect.ActionContains = stringListValue(fields.ActionContainsCamel)
	}
	if fields.ActionNotContains != nil {
		expect.ActionNotContains = stringListValue(fields.ActionNotContains)
	}
	if fields.ActionNotCamel != nil {
		expect.ActionNotContains = stringListValue(fields.ActionNotCamel)
	}
	if fields.ActionCount != nil {
		expect.ActionCount = fields.ActionCount
	}
	if fields.ActionCountCamel != nil {
		expect.ActionCount = fields.ActionCountCamel
	}
	if fields.ActionsCount != nil {
		expect.ActionCount = fields.ActionsCount
	}
	if fields.ActionsCountCamel != nil {
		expect.ActionCount = fields.ActionsCountCamel
	}
	if fields.FocusedIndex != nil {
		expect.Focused = fields.FocusedIndex
	}
	if fields.FocusedIndexCamel != nil {
		expect.Focused = fields.FocusedIndexCamel
	}
	return nil
}

func (dialog *Dialog) UnmarshalJSON(data []byte) error {
	data = normalizeStringFieldsToArray(data,
		"actions",
		"Actions",
		"options",
		"choices",
		"buttons",
	)
	type alias Dialog
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*dialog = Dialog(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if dialog.Title == "" {
		dialog.Title = stringJSONField(fields, "heading", "header", "label", "name")
	}
	if dialog.Body == "" {
		dialog.Body = stringJSONField(fields, "content", "text", "message", "description")
	}
	if len(dialog.Actions) == 0 {
		dialog.Actions = stringListJSONField(fields, "options", "choices", "buttons")
	}
	if focused := intPtrJSONField(fields, "focused_index", "focusedIndex", "selected_index", "selectedIndex", "focus", "selected"); focused != nil {
		dialog.Focused = *focused
	}
	if dialog.ID == "" {
		dialog.ID = scalarStringJSONField(fields, "dialog_id", "dialogId", "dialogID", "permission_id", "permissionId", "permissionID", "request_id", "requestId", "requestID", "tool_use_id", "toolUseId", "toolUseID", "operation_id", "operationId", "operationID")
	}
	if dialog.Kind == "" {
		if dialogKind := stringJSONField(fields, "dialog_kind", "dialogKind"); dialogKind != "" {
			dialog.Kind = DialogKind(dialogKind)
		}
	}
	return nil
}

func (message *Message) UnmarshalJSON(data []byte) error {
	type alias Message
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*message = Message(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if message.Role == "" {
		if role, ok := roleJSONField(fields, "type", "speaker"); ok {
			message.Role = role
		}
	}
	if message.Text == "" {
		message.Text = stringJSONField(fields, "content", "body", "message")
	}
	if len(message.ContentBlocks) == 0 {
		message.ContentBlocks = contentBlocksJSONField(fields, "content", "contentBlocks", "content_blocks", "blocks", "messageContent", "message_content")
	}
	if len(message.ImagePasteIDs) == 0 {
		message.ImagePasteIDs = intListJSONField(
			fields,
			"imagePasteId",
			"imagePasteID",
			"image_paste_id",
			"imagePasteIds",
			"imagePasteIDs",
			"image_paste_ids",
			"imageId",
			"imageID",
			"image_id",
			"imageIds",
			"imageIDs",
			"image_ids",
			"pastedImageId",
			"pastedImageID",
			"pasted_image_id",
			"pastedImageIds",
			"pastedImageIDs",
			"pasted_image_ids",
		)
	}
	if len(message.PastedContents) == 0 {
		message.PastedContents = pastedContentsJSONField(
			fields,
			"pastedContents",
			"pasted_contents",
			"pastedContent",
			"pasted_content",
			"pastes",
			"pasteContents",
			"paste_contents",
			"pasteContent",
			"paste_content",
			"attachments",
			"attachment",
		)
	}
	return nil
}

func (request *PermissionRequest) UnmarshalJSON(data []byte) error {
	data = normalizeStringFieldsToArray(data, "Actions", "actions")
	type alias PermissionRequest
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*request = PermissionRequest(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if request.ID == "" {
		request.ID = scalarStringJSONField(fields, "request_id", "requestId", "requestID", "permission_id", "permissionId", "permissionID", "tool_use_id", "toolUseId", "toolUseID", "operation_id", "operationId", "operationID", "id")
	}
	if request.ToolName == "" {
		request.ToolName = stringJSONField(fields, "tool_name", "toolName", "tool", "name", "operation", "command", "command_name", "commandName", "tool_title", "toolTitle")
	}
	if request.Path == "" {
		request.Path = stringJSONField(fields, "file_path", "filePath", "target_path", "targetPath", "resource_path", "resourcePath", "working_directory", "workingDirectory", "cwd", "path", "target", "resource", "uri", "url", "file", "filename")
	}
	if request.Description == "" {
		request.Description = stringJSONField(fields, "prompt", "message", "reason", "reason_text", "reasonText", "summary", "description", "details", "body", "text", "content")
	}
	if len(request.Actions) == 0 {
		request.Actions = stringListJSONField(fields, "options", "choices", "allowed_actions", "allowedActions", "available_actions", "availableActions", "action_choices", "actionChoices", "buttons", "actions_list", "actionsList")
	}
	return nil
}

func stringJSONField(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return ""
}

func scalarStringJSONField(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var scalar any
		if err := decoder.Decode(&scalar); err != nil {
			continue
		}
		if value, ok := scalar.(json.Number); ok {
			return value.String()
		}
	}
	return ""
}

func stringListJSONField(fields map[string]json.RawMessage, names ...string) []string {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value stringList
		if err := json.Unmarshal(raw, &value); err == nil {
			return stringListValue(&value)
		}
	}
	return nil
}

func intListJSONField(fields map[string]json.RawMessage, names ...string) []int {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value intList
		if err := json.Unmarshal(raw, &value); err == nil {
			return intListValue(&value)
		}
	}
	return nil
}

func boolPtrJSONField(fields map[string]json.RawMessage, names ...string) *bool {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if value, ok := scriptParseJSONBool(raw); ok {
			return &value
		}
	}
	return nil
}

func scriptParseJSONBool(raw json.RawMessage) (bool, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "true", "1", "yes", "y", "on":
			return true, true
		case "false", "0", "no", "n", "off":
			return false, true
		default:
			return false, false
		}
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err == nil {
		value, err := strconv.ParseFloat(number.String(), 64)
		if err != nil {
			return false, false
		}
		switch value {
		case 1:
			return true, true
		case 0:
			return false, true
		default:
			return false, false
		}
	}
	return false, false
}

func intPtrJSONField(fields map[string]json.RawMessage, names ...string) *int {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value int
		if err := json.Unmarshal(raw, &value); err == nil {
			return &value
		}
		var stringValue string
		if err := json.Unmarshal(raw, &stringValue); err == nil {
			if value, err := strconv.Atoi(strings.TrimSpace(stringValue)); err == nil {
				return &value
			}
		}
	}
	return nil
}

func imageDimensionsJSONField(fields map[string]json.RawMessage, names ...string) *session.ImageDimensions {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value session.ImageDimensions
		if err := json.Unmarshal(raw, &value); err == nil {
			return &value
		}
	}
	return nil
}

func intMapJSONField(fields map[string]json.RawMessage, names ...string) map[string]int {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value map[string]int
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return nil
}

func roleJSONField(fields map[string]json.RawMessage, names ...string) (Role, bool) {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value Role
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, true
		}
	}
	return "", false
}

func (expect *PromptExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeBoolFields(data,
		"Empty",
		"empty",
		"is_empty",
		"isEmpty",
		"empty_prompt",
		"emptyPrompt",
		"blank",
	)
	data = normalizeObjectFieldsToArray(data, "pasted_contents", "pastedContents")
	type alias PromptExpectation
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*expect = PromptExpectation(raw)

	var fields struct {
		PastedContentCount      *int                       `json:"pasted_content_count"`
		PastedContentCountCamel *int                       `json:"pastedContentCount"`
		PastedContents          []PastedContentExpectation `json:"pasted_contents"`
		PastedContentsCamel     []PastedContentExpectation `json:"pastedContents"`
		NextPastedID            *int                       `json:"next_pasted_id"`
		NextPastedIDAlt         *int                       `json:"nextPastedId"`
		NextPastedIDUpper       *int                       `json:"nextPastedID"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if expect.Text == "" {
		expect.Text = stringJSONField(fieldMap, "value", "input", "content", "message", "prompt", "prompt_text", "promptText", "input_text", "inputText")
	}
	if expect.Expanded == "" {
		expect.Expanded = stringJSONField(fieldMap, "expanded_text", "expandedText", "expanded_prompt", "expandedPrompt", "expanded_value", "expandedValue", "full_text", "fullText")
	}
	if expect.Cursor == nil {
		expect.Cursor = intPtrJSONField(fieldMap, "cursor_index", "cursorIndex", "cursor_position", "cursorPosition", "caret", "position")
	}
	if empty := boolPtrJSONField(fieldMap, "is_empty", "isEmpty", "empty_prompt", "emptyPrompt", "blank"); empty != nil {
		expect.Empty = *empty
	}
	if fields.PastedContentCount != nil {
		expect.PastedContentCount = fields.PastedContentCount
	}
	if fields.PastedContentCountCamel != nil {
		expect.PastedContentCount = fields.PastedContentCountCamel
	}
	if fields.PastedContents != nil {
		expect.PastedContents = fields.PastedContents
	}
	if fields.PastedContentsCamel != nil {
		expect.PastedContents = fields.PastedContentsCamel
	}
	if fields.NextPastedID != nil {
		expect.NextPastedID = fields.NextPastedID
	}
	if fields.NextPastedIDAlt != nil {
		expect.NextPastedID = fields.NextPastedIDAlt
	}
	if fields.NextPastedIDUpper != nil {
		expect.NextPastedID = fields.NextPastedIDUpper
	}
	return nil
}

func (expect *PastedContentExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeStringFieldsToArray(data,
		"content_contains",
		"contentContains",
		"contains",
		"text_contains",
		"textContains",
		"value_contains",
		"valueContains",
	)
	type alias PastedContentExpectation
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*expect = PastedContentExpectation(raw)

	var fields struct {
		ContentContains      *stringList `json:"content_contains"`
		ContentContainsCamel *stringList `json:"contentContains"`
		Contains             *stringList `json:"contains"`
		TextContains         *stringList `json:"text_contains"`
		TextContainsCamel    *stringList `json:"textContains"`
		ValueContains        *stringList `json:"value_contains"`
		ValueContainsCamel   *stringList `json:"valueContains"`
		MediaType            *string     `json:"media_type"`
		MediaTypeCamel       *string     `json:"mediaType"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if expect.ID == 0 {
		if id := intPtrJSONField(fieldMap, "pasted_id", "pastedId", "pastedID", "pasted_content_id", "pastedContentId", "pastedContentID", "content_id", "contentId", "contentID"); id != nil {
			expect.ID = *id
		}
	}
	if expect.Type == "" {
		expect.Type = stringJSONField(fieldMap, "kind", "content_kind", "contentKind", "item_type", "itemType", "pasted_type", "pastedType")
	}
	if expect.Content == "" {
		expect.Content = stringJSONField(fieldMap, "value", "text", "body", "message", "data", "base64")
	}
	if expect.SourcePath == "" {
		expect.SourcePath = stringJSONField(fieldMap, "source_path", "sourcePath", "path", "filePath", "file_path")
	}
	if expect.Dimensions == nil {
		expect.Dimensions = imageDimensionsJSONField(fieldMap, "dimensions", "imageDimensions", "image_dimensions")
	}
	if fields.ContentContains != nil {
		expect.ContentContains = stringListValue(fields.ContentContains)
	}
	if fields.ContentContainsCamel != nil {
		expect.ContentContains = stringListValue(fields.ContentContainsCamel)
	}
	if fields.Contains != nil {
		expect.ContentContains = stringListValue(fields.Contains)
	}
	if fields.TextContains != nil {
		expect.ContentContains = stringListValue(fields.TextContains)
	}
	if fields.TextContainsCamel != nil {
		expect.ContentContains = stringListValue(fields.TextContainsCamel)
	}
	if fields.ValueContains != nil {
		expect.ContentContains = stringListValue(fields.ValueContains)
	}
	if fields.ValueContainsCamel != nil {
		expect.ContentContains = stringListValue(fields.ValueContainsCamel)
	}
	if fields.MediaType != nil {
		expect.MediaType = *fields.MediaType
	}
	if fields.MediaTypeCamel != nil {
		expect.MediaType = *fields.MediaTypeCamel
	}
	if expect.MediaType == "" {
		expect.MediaType = stringJSONField(fieldMap, "mime_type", "mimeType", "content_type", "contentType", "media", "mime", "file_type", "fileType")
	}
	if expect.Filename == "" {
		expect.Filename = stringJSONField(fieldMap, "file_name", "fileName", "name", "path")
	}
	return nil
}

func (task *TaskStatus) UnmarshalJSON(data []byte) error {
	type alias TaskStatus
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*task = TaskStatus(raw)

	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if id := scalarStringJSONField(fieldMap, "task_id", "taskId", "taskID", "job_id", "jobId", "jobID", "run_id", "runId", "runID", "id"); id != "" {
		task.ID = id
	}
	if title := stringJSONField(fieldMap, "task_title", "taskTitle", "title", "name", "label", "display_name", "displayName"); title != "" {
		task.Title = title
	}
	if state := stringJSONField(fieldMap, "status", "state", "phase", "lifecycle", "task_state", "taskState"); state != "" {
		task.State = state
	}
	if detail := stringJSONField(fieldMap, "status_text", "statusText", "detail", "message", "description", "summary", "current_step", "currentStep"); detail != "" {
		task.Detail = detail
	}
	if progress := intPtrJSONField(fieldMap, "progress_percent", "progressPercent", "percent", "percentage", "progress", "pct"); progress != nil {
		task.Progress = *progress
	}
	return nil
}

func (expect *VimExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeBoolFields(data,
		"Enabled",
		"enabled",
		"vim_enabled",
		"vimEnabled",
		"is_enabled",
		"isEnabled",
		"active",
		"RegisterLinewise",
		"register_linewise",
		"registerLinewise",
		"linewise",
		"is_linewise",
		"isLinewise",
		"register_line_wise",
		"registerLineWise",
	)
	type alias VimExpectation
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*expect = VimExpectation(raw)

	var fields struct {
		RegisterLinewise      *bool `json:"register_linewise"`
		RegisterLinewiseCamel *bool `json:"registerLinewise"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if expect.Enabled == nil {
		expect.Enabled = boolPtrJSONField(fieldMap, "vim_enabled", "vimEnabled", "is_enabled", "isEnabled", "enabled", "active")
	}
	if expect.Mode == "" {
		if mode := stringJSONField(fieldMap, "vim_mode", "vimMode", "mode_name", "modeName", "current_mode", "currentMode", "state"); mode != "" {
			expect.Mode = VimMode(mode)
		}
	}
	if expect.Register == "" {
		expect.Register = stringJSONField(fieldMap, "vim_register", "vimRegister", "register_value", "registerValue", "yank_register", "yankRegister")
	}
	if fields.RegisterLinewise != nil {
		expect.RegisterLinewise = fields.RegisterLinewise
	}
	if fields.RegisterLinewiseCamel != nil {
		expect.RegisterLinewise = fields.RegisterLinewiseCamel
	}
	if expect.RegisterLinewise == nil {
		expect.RegisterLinewise = boolPtrJSONField(fieldMap, "linewise", "is_linewise", "isLinewise", "register_line_wise", "registerLineWise")
	}
	return nil
}

func (expect *TasksExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeObjectFieldsToArray(data, "contains")
	type alias TasksExpectation
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*expect = TasksExpectation(raw)

	var fields struct {
		StateCounts      map[string]int `json:"state_counts"`
		StateCountsCamel map[string]int `json:"stateCounts"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if expect.Count == nil {
		expect.Count = intPtrJSONField(fieldMap, "task_count", "taskCount", "total", "size", "length")
	}
	if fields.StateCounts != nil {
		expect.StateCounts = fields.StateCounts
	}
	if fields.StateCountsCamel != nil {
		expect.StateCounts = fields.StateCountsCamel
	}
	if len(expect.StateCounts) == 0 {
		expect.StateCounts = intMapJSONField(fieldMap, "status_counts", "statusCounts", "counts", "counts_by_state", "countsByState", "by_state", "byState")
	}
	return nil
}

func (expect *TaskExpectation) UnmarshalJSON(data []byte) error {
	type alias TaskExpectation
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*expect = TaskExpectation(raw)

	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if id := scalarStringJSONField(fieldMap, "task_id", "taskId", "taskID", "job_id", "jobId", "jobID", "run_id", "runId", "runID", "id"); id != "" {
		expect.ID = id
	}
	if title := stringJSONField(fieldMap, "task_title", "taskTitle", "title", "name", "label", "display_name", "displayName"); title != "" {
		expect.Title = title
	}
	if state := stringJSONField(fieldMap, "status", "state", "phase", "lifecycle", "task_state", "taskState"); state != "" {
		expect.State = state
	}
	if detail := stringJSONField(fieldMap, "status_text", "statusText", "detail", "message", "description", "summary", "current_step", "currentStep"); detail != "" {
		expect.Detail = detail
	}
	if progress := intPtrJSONField(fieldMap, "progress_percent", "progressPercent", "percent", "percentage", "progress", "pct"); progress != nil {
		expect.Progress = progress
	}
	return nil
}

func (expect *ViewportExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeStringFieldsToArray(data,
		"visible_contains",
		"visibleContains",
		"visible_not_contains",
		"visibleNotContains",
	)
	type alias ViewportExpectation
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*expect = ViewportExpectation(raw)

	var fields struct {
		VisibleLineCount        *int        `json:"visible_line_count"`
		VisibleContains         *stringList `json:"visible_contains"`
		VisibleContainsCamel    *stringList `json:"visibleContains"`
		VisibleNotContains      *stringList `json:"visible_not_contains"`
		VisibleNotContainsCamel *stringList `json:"visibleNotContains"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if expect.Offset == nil {
		expect.Offset = intPtrJSONField(fieldMap, "scroll_offset", "scrollOffset", "viewport_offset", "viewportOffset", "top", "start_line", "startLine")
	}
	if fields.VisibleLineCount != nil {
		expect.VisibleLineCount = *fields.VisibleLineCount
	}
	if expect.VisibleLineCount == 0 {
		if visibleLineCount := intPtrJSONField(fieldMap, "line_count", "lineCount", "visible_rows", "visibleRows", "visible_lines", "visibleLines", "rows"); visibleLineCount != nil {
			expect.VisibleLineCount = *visibleLineCount
		}
	}
	if fields.VisibleContains != nil {
		expect.VisibleContains = stringListValue(fields.VisibleContains)
	}
	if fields.VisibleContainsCamel != nil {
		expect.VisibleContains = stringListValue(fields.VisibleContainsCamel)
	}
	if fields.VisibleNotContains != nil {
		expect.VisibleNotContains = stringListValue(fields.VisibleNotContains)
	}
	if fields.VisibleNotContainsCamel != nil {
		expect.VisibleNotContains = stringListValue(fields.VisibleNotContainsCamel)
	}
	return nil
}

func (expect *ScreenExpectation) UnmarshalJSON(data []byte) error {
	type alias ScreenExpectation
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*expect = ScreenExpectation(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if expect.Width == 0 {
		if width := intPtrJSONField(fields, "screen_width", "screenWidth", "columns", "cols", "column_count", "columnCount"); width != nil {
			expect.Width = *width
		}
	}
	if expect.Height == 0 {
		if height := intPtrJSONField(fields, "screen_height", "screenHeight", "rows", "lines", "row_count", "rowCount", "line_count", "lineCount"); height != nil {
			expect.Height = *height
		}
	}
	return nil
}

func (expect *ReverseSearchExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeBoolFields(data,
		"Active",
		"active",
		"is_active",
		"isActive",
		"open",
		"visible",
		"NoResults",
		"noResults",
		"no_results",
		"no_matches",
		"noMatches",
		"empty",
		"empty_results",
		"emptyResults",
	)
	type alias ReverseSearchExpectation
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*expect = ReverseSearchExpectation(raw)

	var fields struct {
		ResultCount *int  `json:"result_count"`
		NoResults   *bool `json:"no_results"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if active := boolPtrJSONField(fieldMap, "is_active", "isActive", "open", "visible"); active != nil {
		expect.Active = *active
	}
	if expect.Query == "" {
		expect.Query = stringJSONField(fieldMap, "search", "term", "pattern", "text", "input", "value")
	}
	if expect.Cursor == nil {
		expect.Cursor = intPtrJSONField(fieldMap, "cursor_index", "cursorIndex", "cursor_position", "cursorPosition", "caret", "position")
	}
	if expect.Current == "" {
		expect.Current = stringJSONField(fieldMap, "current_result", "currentResult", "current_match", "currentMatch", "match", "selected", "selection")
	}
	if fields.ResultCount != nil {
		expect.ResultCount = *fields.ResultCount
	}
	if expect.ResultCount == 0 {
		if resultCount := intPtrJSONField(fieldMap, "match_count", "matchCount", "matches", "resultCount", "results", "total"); resultCount != nil {
			expect.ResultCount = *resultCount
		}
	}
	if fields.NoResults != nil {
		expect.NoResults = *fields.NoResults
	}
	if noResults := boolPtrJSONField(fieldMap, "no_matches", "noMatches", "empty", "empty_results", "emptyResults"); noResults != nil {
		expect.NoResults = *noResults
	}
	return nil
}
