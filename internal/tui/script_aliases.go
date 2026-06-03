package tui

import (
	"encoding/json"

	"ccgo/internal/session"
)

type stringList []string

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
		var request PermissionRequest
		if err := json.Unmarshal(raw, &request); err == nil {
			return &request
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
		var task TaskStatus
		if err := json.Unmarshal(raw, &task); err == nil {
			return &task
		}
	}
	return nil
}

func (step *ScriptStep) UnmarshalJSON(data []byte) error {
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
		RemoveTaskID              *string                   `json:"remove_task_id"`
		RemoveTaskIDCamel         *string                   `json:"removeTaskId"`
		RemoveTaskIDUpperCamel    *string                   `json:"removeTaskID"`
		RemoveTask                *string                   `json:"remove_task"`
		RemoveTaskCamel           *string                   `json:"removeTask"`
		DeleteTask                *string                   `json:"delete_task"`
		DeleteTaskCamel           *string                   `json:"deleteTask"`
		CancelActiveDialog        *bool                     `json:"cancel_active_dialog"`
		CancelActiveDialogCamel   *bool                     `json:"cancelActiveDialog"`
		CancelActive              *bool                     `json:"cancel_active"`
		CancelActiveCamel         *bool                     `json:"cancelActive"`
		CancelDialog              *bool                     `json:"cancel_dialog"`
		CancelDialogCamel         *bool                     `json:"cancelDialog"`
		CloseDialog               *bool                     `json:"close_dialog"`
		CloseDialogCamel          *bool                     `json:"closeDialog"`
		CancelPermissionID        *string                   `json:"cancel_permission_id"`
		CancelPermissionIDAlt     *string                   `json:"cancelPermissionId"`
		CancelPermissionIDUpper   *string                   `json:"cancelPermissionID"`
		CancelPermission          *string                   `json:"cancel_permission"`
		CancelPermissionCamel     *string                   `json:"cancelPermission"`
		CancelAllPermissions      *bool                     `json:"cancel_all_permissions"`
		CancelAllPermissionsCamel *bool                     `json:"cancelAllPermissions"`
		CancelPermissions         *bool                     `json:"cancel_permissions"`
		CancelPermissionsCamel    *bool                     `json:"cancelPermissions"`
		CancelAllTasks            *bool                     `json:"cancel_all_tasks"`
		CancelAllTasksCamel       *bool                     `json:"cancelAllTasks"`
		CancelTasks               *bool                     `json:"cancel_tasks"`
		CancelTasksCamel          *bool                     `json:"cancelTasks"`
		CancelTasksDetail         *string                   `json:"cancel_tasks_detail"`
		CancelTasksDetailCamel    *string                   `json:"cancelTasksDetail"`
		CancelReason              *string                   `json:"cancel_reason"`
		CancelReasonCamel         *string                   `json:"cancelReason"`
		OpenTasksDialog           *bool                     `json:"open_tasks_dialog"`
		OpenTasksDialogCamel      *bool                     `json:"openTasksDialog"`
		OpenTasks                 *bool                     `json:"open_tasks"`
		OpenTasksCamel            *bool                     `json:"openTasks"`
		ShowTasks                 *bool                     `json:"show_tasks"`
		ShowTasksCamel            *bool                     `json:"showTasks"`
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
	if fields.UpsertTask != nil {
		step.UpsertTask = fields.UpsertTask
	}
	if fields.UpsertTaskCamel != nil {
		step.UpsertTask = fields.UpsertTaskCamel
	}
	if task := taskStatusJSONField(fieldMap, "task", "task_status", "taskStatus"); task != nil {
		step.UpsertTask = task
	}
	if fields.RemoveTaskID != nil {
		step.RemoveTaskID = *fields.RemoveTaskID
	}
	if fields.RemoveTaskIDCamel != nil {
		step.RemoveTaskID = *fields.RemoveTaskIDCamel
	}
	if fields.RemoveTaskIDUpperCamel != nil {
		step.RemoveTaskID = *fields.RemoveTaskIDUpperCamel
	}
	if fields.RemoveTask != nil {
		step.RemoveTaskID = *fields.RemoveTask
	}
	if fields.RemoveTaskCamel != nil {
		step.RemoveTaskID = *fields.RemoveTaskCamel
	}
	if fields.DeleteTask != nil {
		step.RemoveTaskID = *fields.DeleteTask
	}
	if fields.DeleteTaskCamel != nil {
		step.RemoveTaskID = *fields.DeleteTaskCamel
	}
	if fields.CancelActiveDialog != nil {
		step.CancelActiveDialog = *fields.CancelActiveDialog
	}
	if fields.CancelActiveDialogCamel != nil {
		step.CancelActiveDialog = *fields.CancelActiveDialogCamel
	}
	if fields.CancelActive != nil {
		step.CancelActiveDialog = *fields.CancelActive
	}
	if fields.CancelActiveCamel != nil {
		step.CancelActiveDialog = *fields.CancelActiveCamel
	}
	if fields.CancelDialog != nil {
		step.CancelActiveDialog = *fields.CancelDialog
	}
	if fields.CancelDialogCamel != nil {
		step.CancelActiveDialog = *fields.CancelDialogCamel
	}
	if fields.CloseDialog != nil {
		step.CancelActiveDialog = *fields.CloseDialog
	}
	if fields.CloseDialogCamel != nil {
		step.CancelActiveDialog = *fields.CloseDialogCamel
	}
	if fields.CancelPermissionID != nil {
		step.CancelPermissionID = *fields.CancelPermissionID
	}
	if fields.CancelPermissionIDAlt != nil {
		step.CancelPermissionID = *fields.CancelPermissionIDAlt
	}
	if fields.CancelPermissionIDUpper != nil {
		step.CancelPermissionID = *fields.CancelPermissionIDUpper
	}
	if fields.CancelPermission != nil {
		step.CancelPermissionID = *fields.CancelPermission
	}
	if fields.CancelPermissionCamel != nil {
		step.CancelPermissionID = *fields.CancelPermissionCamel
	}
	if fields.CancelAllPermissions != nil {
		step.CancelAllPermissions = *fields.CancelAllPermissions
	}
	if fields.CancelAllPermissionsCamel != nil {
		step.CancelAllPermissions = *fields.CancelAllPermissionsCamel
	}
	if fields.CancelPermissions != nil {
		step.CancelAllPermissions = *fields.CancelPermissions
	}
	if fields.CancelPermissionsCamel != nil {
		step.CancelAllPermissions = *fields.CancelPermissionsCamel
	}
	if fields.CancelAllTasks != nil {
		step.CancelAllTasks = *fields.CancelAllTasks
	}
	if fields.CancelAllTasksCamel != nil {
		step.CancelAllTasks = *fields.CancelAllTasksCamel
	}
	if fields.CancelTasks != nil {
		step.CancelAllTasks = *fields.CancelTasks
	}
	if fields.CancelTasksCamel != nil {
		step.CancelAllTasks = *fields.CancelTasksCamel
	}
	if fields.CancelTasksDetail != nil {
		step.CancelTasksDetail = *fields.CancelTasksDetail
	}
	if fields.CancelTasksDetailCamel != nil {
		step.CancelTasksDetail = *fields.CancelTasksDetailCamel
	}
	if fields.CancelReason != nil {
		step.CancelTasksDetail = *fields.CancelReason
	}
	if fields.CancelReasonCamel != nil {
		step.CancelTasksDetail = *fields.CancelReasonCamel
	}
	if fields.OpenTasksDialog != nil {
		step.OpenTasksDialog = *fields.OpenTasksDialog
	}
	if fields.OpenTasksDialogCamel != nil {
		step.OpenTasksDialog = *fields.OpenTasksDialogCamel
	}
	if fields.OpenTasks != nil {
		step.OpenTasksDialog = *fields.OpenTasks
	}
	if fields.OpenTasksCamel != nil {
		step.OpenTasksDialog = *fields.OpenTasksCamel
	}
	if fields.ShowTasks != nil {
		step.OpenTasksDialog = *fields.ShowTasks
	}
	if fields.ShowTasksCamel != nil {
		step.OpenTasksDialog = *fields.ShowTasksCamel
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
	return nil
}

func normalizeScriptStepJSON(data []byte) []byte {
	data = normalizeStringFieldsToArray(data,
		"keys",
		"expect_status_contains",
		"expectStatusContains",
		"expect_status_not_contains",
		"expectStatusNotContains",
		"expect_snapshot_contains",
		"expectSnapshotContains",
		"expect_snapshot_not_contains",
		"expectSnapshotNotContains",
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
	if x := intPtrJSONField(fields, "column", "col", "mouse_x", "mouseX", "client_x", "clientX", "screen_x", "screenX"); x != nil {
		mouse.X = *x
	}
	if y := intPtrJSONField(fields, "row", "line", "mouse_y", "mouseY", "client_y", "clientY", "screen_y", "screenY"); y != nil {
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
		event.DialogID = stringJSONField(fields, "dialog_id", "dialogId", "dialogID")
	}
	if event.DialogKind == "" {
		if dialogKind := stringJSONField(fields, "dialog_kind", "dialogKind"); dialogKind != "" {
			event.DialogKind = DialogKind(dialogKind)
		}
	}
	return nil
}

func (expect *DialogResultExpectation) UnmarshalJSON(data []byte) error {
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
		expect.ID = stringJSONField(fields, "dialog_id", "dialogId", "dialogID", "permission_id", "permissionId", "permissionID", "request_id", "requestId", "requestID")
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
		expect.ID = stringJSONField(fieldMap, "dialog_id", "dialogId", "dialogID", "permission_id", "permissionId", "permissionID", "request_id", "requestId", "requestID")
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
		dialog.ID = stringJSONField(fields, "dialog_id", "dialogId", "dialogID", "permission_id", "permissionId", "permissionID", "request_id", "requestId", "requestID")
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
		request.ID = stringJSONField(fields, "request_id", "requestId", "permission_id", "permissionId", "tool_use_id", "toolUseId", "toolUseID")
	}
	if request.ToolName == "" {
		request.ToolName = stringJSONField(fields, "tool_name", "toolName", "tool", "name")
	}
	if request.Path == "" {
		request.Path = stringJSONField(fields, "file_path", "filePath", "target_path", "targetPath", "working_directory", "workingDirectory", "cwd")
	}
	if request.Description == "" {
		request.Description = stringJSONField(fields, "prompt", "message", "reason", "summary")
	}
	if len(request.Actions) == 0 {
		request.Actions = stringListJSONField(fields, "options", "choices", "allowed_actions", "allowedActions")
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

func boolPtrJSONField(fields map[string]json.RawMessage, names ...string) *bool {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value bool
		if err := json.Unmarshal(raw, &value); err == nil {
			return &value
		}
	}
	return nil
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

	var fields struct {
		TaskID          *string `json:"task_id"`
		TaskIDCamel     *string `json:"taskId"`
		TaskTitle       *string `json:"task_title"`
		TaskTitleCamel  *string `json:"taskTitle"`
		Name            *string `json:"name"`
		Status          *string `json:"status"`
		StatusText      *string `json:"status_text"`
		StatusTextCamel *string `json:"statusText"`
		ProgressPercent *int    `json:"progress_percent"`
		ProgressCamel   *int    `json:"progressPercent"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.TaskID != nil {
		task.ID = *fields.TaskID
	}
	if fields.TaskIDCamel != nil {
		task.ID = *fields.TaskIDCamel
	}
	if fields.TaskTitle != nil {
		task.Title = *fields.TaskTitle
	}
	if fields.TaskTitleCamel != nil {
		task.Title = *fields.TaskTitleCamel
	}
	if fields.Name != nil {
		task.Title = *fields.Name
	}
	if fields.Status != nil {
		task.State = *fields.Status
	}
	if fields.StatusText != nil {
		task.Detail = *fields.StatusText
	}
	if fields.StatusTextCamel != nil {
		task.Detail = *fields.StatusTextCamel
	}
	if fields.ProgressPercent != nil {
		task.Progress = *fields.ProgressPercent
	}
	if fields.ProgressCamel != nil {
		task.Progress = *fields.ProgressCamel
	}
	return nil
}

func (expect *VimExpectation) UnmarshalJSON(data []byte) error {
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

	var fields struct {
		TaskID          *string `json:"task_id"`
		TaskIDCamel     *string `json:"taskId"`
		TaskTitle       *string `json:"task_title"`
		TaskTitleCamel  *string `json:"taskTitle"`
		Name            *string `json:"name"`
		Status          *string `json:"status"`
		StatusText      *string `json:"status_text"`
		StatusTextCamel *string `json:"statusText"`
		ProgressPercent *int    `json:"progress_percent"`
		ProgressCamel   *int    `json:"progressPercent"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.TaskID != nil {
		expect.ID = *fields.TaskID
	}
	if fields.TaskIDCamel != nil {
		expect.ID = *fields.TaskIDCamel
	}
	if fields.TaskTitle != nil {
		expect.Title = *fields.TaskTitle
	}
	if fields.TaskTitleCamel != nil {
		expect.Title = *fields.TaskTitleCamel
	}
	if fields.Name != nil {
		expect.Title = *fields.Name
	}
	if fields.Status != nil {
		expect.State = *fields.Status
	}
	if fields.StatusText != nil {
		expect.Detail = *fields.StatusText
	}
	if fields.StatusTextCamel != nil {
		expect.Detail = *fields.StatusTextCamel
	}
	if fields.ProgressPercent != nil {
		expect.Progress = fields.ProgressPercent
	}
	if fields.ProgressCamel != nil {
		expect.Progress = fields.ProgressCamel
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
