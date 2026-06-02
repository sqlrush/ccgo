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
		UpsertTask                *TaskStatus               `json:"upsert_task"`
		RemoveTaskID              *string                   `json:"remove_task_id"`
		CancelAllTasks            *bool                     `json:"cancel_all_tasks"`
		CancelTasksDetail         *string                   `json:"cancel_tasks_detail"`
		OpenTasksDialog           *bool                     `json:"open_tasks_dialog"`
		ResizeWidth               *int                      `json:"resize_width"`
		ResizeHeight              *int                      `json:"resize_height"`
		SnapshotName              *string                   `json:"snapshot_name"`
		ExpectEvent               *ScreenEvent              `json:"expect_event"`
		ExpectEvents              []ScreenEvent             `json:"expect_events"`
		ExpectDialogResult        *DialogResultExpectation  `json:"expect_dialog_result"`
		ExpectDialog              *DialogExpectation        `json:"expect_dialog"`
		ExpectPrompt              *PromptExpectation        `json:"expect_prompt"`
		ExpectVim                 *VimExpectation           `json:"expect_vim"`
		ExpectTasks               *TasksExpectation         `json:"expect_tasks"`
		ExpectReverseSearch       *ReverseSearchExpectation `json:"expect_reverse_search"`
		ExpectViewport            *ViewportExpectation      `json:"expect_viewport"`
		ExpectScreen              *ScreenExpectation        `json:"expect_screen"`
		ExpectFocused             *bool                     `json:"expect_focused"`
		ExpectStatusContains      []string                  `json:"expect_status_contains"`
		ExpectStatusNotContains   []string                  `json:"expect_status_not_contains"`
		ExpectSnapshotContains    []string                  `json:"expect_snapshot_contains"`
		ExpectSnapshotNotContains []string                  `json:"expect_snapshot_not_contains"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.RequestPermission != nil {
		step.RequestPermission = fields.RequestPermission
	}
	if fields.UpsertTask != nil {
		step.UpsertTask = fields.UpsertTask
	}
	if fields.RemoveTaskID != nil {
		step.RemoveTaskID = *fields.RemoveTaskID
	}
	if fields.CancelAllTasks != nil {
		step.CancelAllTasks = *fields.CancelAllTasks
	}
	if fields.CancelTasksDetail != nil {
		step.CancelTasksDetail = *fields.CancelTasksDetail
	}
	if fields.OpenTasksDialog != nil {
		step.OpenTasksDialog = *fields.OpenTasksDialog
	}
	if fields.ResizeWidth != nil {
		step.ResizeWidth = *fields.ResizeWidth
	}
	if fields.ResizeHeight != nil {
		step.ResizeHeight = *fields.ResizeHeight
	}
	if fields.SnapshotName != nil {
		step.SnapshotName = *fields.SnapshotName
	}
	if fields.ExpectEvent != nil {
		step.ExpectEvent = fields.ExpectEvent
	}
	if fields.ExpectEvents != nil {
		step.ExpectEvents = fields.ExpectEvents
	}
	if fields.ExpectDialogResult != nil {
		step.ExpectDialogResult = fields.ExpectDialogResult
	}
	if fields.ExpectDialog != nil {
		step.ExpectDialog = fields.ExpectDialog
	}
	if fields.ExpectPrompt != nil {
		step.ExpectPrompt = fields.ExpectPrompt
	}
	if fields.ExpectVim != nil {
		step.ExpectVim = fields.ExpectVim
	}
	if fields.ExpectTasks != nil {
		step.ExpectTasks = fields.ExpectTasks
	}
	if fields.ExpectReverseSearch != nil {
		step.ExpectReverseSearch = fields.ExpectReverseSearch
	}
	if fields.ExpectViewport != nil {
		step.ExpectViewport = fields.ExpectViewport
	}
	if fields.ExpectScreen != nil {
		step.ExpectScreen = fields.ExpectScreen
	}
	if fields.ExpectFocused != nil {
		step.ExpectFocused = fields.ExpectFocused
	}
	if fields.ExpectStatusContains != nil {
		step.ExpectStatusContains = fields.ExpectStatusContains
	}
	if fields.ExpectStatusNotContains != nil {
		step.ExpectStatusNotContains = fields.ExpectStatusNotContains
	}
	if fields.ExpectSnapshotContains != nil {
		step.ExpectSnapshotContains = fields.ExpectSnapshotContains
	}
	if fields.ExpectSnapshotNotContains != nil {
		step.ExpectSnapshotNotContains = fields.ExpectSnapshotNotContains
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
		DialogID   *string     `json:"dialog_id"`
		DialogKind *DialogKind `json:"dialog_kind"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.DialogID != nil {
		event.DialogID = *fields.DialogID
	}
	if fields.DialogKind != nil {
		event.DialogKind = *fields.DialogKind
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
		ToolName *string `json:"tool_name"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.ToolName != nil {
		request.ToolName = *fields.ToolName
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
		StateCounts map[string]int `json:"state_counts"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.StateCounts != nil {
		expect.StateCounts = fields.StateCounts
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
