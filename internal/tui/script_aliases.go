package tui

import "encoding/json"

func (step *ScriptStep) UnmarshalJSON(data []byte) error {
	type alias ScriptStep
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*step = ScriptStep(raw)

	var fields struct {
		RequestPermission         *PermissionRequest        `json:"request_permission"`
		RequestPermissionCamel    *PermissionRequest        `json:"requestPermission"`
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
		CancelActiveDialog        *bool                     `json:"cancel_active_dialog"`
		CancelActiveDialogCamel   *bool                     `json:"cancelActiveDialog"`
		CancelActive              *bool                     `json:"cancel_active"`
		CancelPermissionID        *string                   `json:"cancel_permission_id"`
		CancelPermissionIDAlt     *string                   `json:"cancelPermissionId"`
		CancelPermissionIDUpper   *string                   `json:"cancelPermissionID"`
		CancelAllPermissions      *bool                     `json:"cancel_all_permissions"`
		CancelAllPermissionsCamel *bool                     `json:"cancelAllPermissions"`
		CancelAllTasks            *bool                     `json:"cancel_all_tasks"`
		CancelAllTasksCamel       *bool                     `json:"cancelAllTasks"`
		CancelTasksDetail         *string                   `json:"cancel_tasks_detail"`
		CancelTasksDetailCamel    *string                   `json:"cancelTasksDetail"`
		OpenTasksDialog           *bool                     `json:"open_tasks_dialog"`
		OpenTasksDialogCamel      *bool                     `json:"openTasksDialog"`
		ResizeWidth               *int                      `json:"resize_width"`
		ResizeWidthCamel          *int                      `json:"resizeWidth"`
		ResizeHeight              *int                      `json:"resize_height"`
		ResizeHeightCamel         *int                      `json:"resizeHeight"`
		SnapshotName              *string                   `json:"snapshot_name"`
		SnapshotNameCamel         *string                   `json:"snapshotName"`
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
		ExpectStatusContains      []string                  `json:"expect_status_contains"`
		ExpectStatusContainsCamel []string                  `json:"expectStatusContains"`
		ExpectStatusNotContains   []string                  `json:"expect_status_not_contains"`
		ExpectStatusNotCamel      []string                  `json:"expectStatusNotContains"`
		ExpectSnapshotContains    []string                  `json:"expect_snapshot_contains"`
		ExpectSnapshotCamel       []string                  `json:"expectSnapshotContains"`
		ExpectSnapshotNotContains []string                  `json:"expect_snapshot_not_contains"`
		ExpectSnapshotNotCamel    []string                  `json:"expectSnapshotNotContains"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.RequestPermission != nil {
		step.RequestPermission = fields.RequestPermission
	}
	if fields.RequestPermissionCamel != nil {
		step.RequestPermission = fields.RequestPermissionCamel
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
	if fields.RemoveTaskID != nil {
		step.RemoveTaskID = *fields.RemoveTaskID
	}
	if fields.RemoveTaskIDCamel != nil {
		step.RemoveTaskID = *fields.RemoveTaskIDCamel
	}
	if fields.RemoveTaskIDUpperCamel != nil {
		step.RemoveTaskID = *fields.RemoveTaskIDUpperCamel
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
	if fields.CancelPermissionID != nil {
		step.CancelPermissionID = *fields.CancelPermissionID
	}
	if fields.CancelPermissionIDAlt != nil {
		step.CancelPermissionID = *fields.CancelPermissionIDAlt
	}
	if fields.CancelPermissionIDUpper != nil {
		step.CancelPermissionID = *fields.CancelPermissionIDUpper
	}
	if fields.CancelAllPermissions != nil {
		step.CancelAllPermissions = *fields.CancelAllPermissions
	}
	if fields.CancelAllPermissionsCamel != nil {
		step.CancelAllPermissions = *fields.CancelAllPermissionsCamel
	}
	if fields.CancelAllTasks != nil {
		step.CancelAllTasks = *fields.CancelAllTasks
	}
	if fields.CancelAllTasksCamel != nil {
		step.CancelAllTasks = *fields.CancelAllTasksCamel
	}
	if fields.CancelTasksDetail != nil {
		step.CancelTasksDetail = *fields.CancelTasksDetail
	}
	if fields.CancelTasksDetailCamel != nil {
		step.CancelTasksDetail = *fields.CancelTasksDetailCamel
	}
	if fields.OpenTasksDialog != nil {
		step.OpenTasksDialog = *fields.OpenTasksDialog
	}
	if fields.OpenTasksDialogCamel != nil {
		step.OpenTasksDialog = *fields.OpenTasksDialogCamel
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
	if fields.SnapshotName != nil {
		step.SnapshotName = *fields.SnapshotName
	}
	if fields.SnapshotNameCamel != nil {
		step.SnapshotName = *fields.SnapshotNameCamel
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
		step.ExpectStatusContains = fields.ExpectStatusContains
	}
	if fields.ExpectStatusContainsCamel != nil {
		step.ExpectStatusContains = fields.ExpectStatusContainsCamel
	}
	if fields.ExpectStatusNotContains != nil {
		step.ExpectStatusNotContains = fields.ExpectStatusNotContains
	}
	if fields.ExpectStatusNotCamel != nil {
		step.ExpectStatusNotContains = fields.ExpectStatusNotCamel
	}
	if fields.ExpectSnapshotContains != nil {
		step.ExpectSnapshotContains = fields.ExpectSnapshotContains
	}
	if fields.ExpectSnapshotCamel != nil {
		step.ExpectSnapshotContains = fields.ExpectSnapshotCamel
	}
	if fields.ExpectSnapshotNotContains != nil {
		step.ExpectSnapshotNotContains = fields.ExpectSnapshotNotContains
	}
	if fields.ExpectSnapshotNotCamel != nil {
		step.ExpectSnapshotNotContains = fields.ExpectSnapshotNotCamel
	}
	return nil
}

func (image *ScriptImage) UnmarshalJSON(data []byte) error {
	type alias ScriptImage
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*image = ScriptImage(raw)

	var fields struct {
		MediaType *string `json:"media_type"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.MediaType != nil {
		image.MediaType = *fields.MediaType
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

	var fields struct {
		DialogID        *string     `json:"dialog_id"`
		DialogIDCamel   *string     `json:"dialogId"`
		DialogKind      *DialogKind `json:"dialog_kind"`
		DialogKindCamel *DialogKind `json:"dialogKind"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.DialogID != nil {
		event.DialogID = *fields.DialogID
	}
	if fields.DialogIDCamel != nil {
		event.DialogID = *fields.DialogIDCamel
	}
	if fields.DialogKind != nil {
		event.DialogKind = *fields.DialogKind
	}
	if fields.DialogKindCamel != nil {
		event.DialogKind = *fields.DialogKindCamel
	}
	return nil
}

func (request *PermissionRequest) UnmarshalJSON(data []byte) error {
	type alias PermissionRequest
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*request = PermissionRequest(raw)

	var fields struct {
		ToolName      *string `json:"tool_name"`
		ToolNameCamel *string `json:"toolName"`
		Tool          *string `json:"tool"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.ToolName != nil {
		request.ToolName = *fields.ToolName
	}
	if fields.ToolNameCamel != nil {
		request.ToolName = *fields.ToolNameCamel
	}
	if fields.Tool != nil {
		request.ToolName = *fields.Tool
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
		RegisterLinewise *bool `json:"register_linewise"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.RegisterLinewise != nil {
		expect.RegisterLinewise = fields.RegisterLinewise
	}
	return nil
}

func (expect *TasksExpectation) UnmarshalJSON(data []byte) error {
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
	if fields.StateCounts != nil {
		expect.StateCounts = fields.StateCounts
	}
	if fields.StateCountsCamel != nil {
		expect.StateCounts = fields.StateCountsCamel
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
	type alias ViewportExpectation
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*expect = ViewportExpectation(raw)

	var fields struct {
		VisibleLineCount   *int     `json:"visible_line_count"`
		VisibleContains    []string `json:"visible_contains"`
		VisibleNotContains []string `json:"visible_not_contains"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.VisibleLineCount != nil {
		expect.VisibleLineCount = *fields.VisibleLineCount
	}
	if fields.VisibleContains != nil {
		expect.VisibleContains = fields.VisibleContains
	}
	if fields.VisibleNotContains != nil {
		expect.VisibleNotContains = fields.VisibleNotContains
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
	if fields.ResultCount != nil {
		expect.ResultCount = *fields.ResultCount
	}
	if fields.NoResults != nil {
		expect.NoResults = *fields.NoResults
	}
	return nil
}
