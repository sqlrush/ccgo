package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

type stringList []string
type intList []int

var scriptTaskIDFieldAliases = []string{
	"task_id", "taskId", "taskID",
	"job_id", "jobId", "jobID",
	"run_id", "runId", "runID",
	"operation_id", "operationId", "operationID",
	"request_id", "requestId", "requestID",
	"thread_id", "threadId", "threadID",
	"workflow_id", "workflowId", "workflowID",
	"tool_use_id", "toolUseId", "toolUseID",
	"id",
}

func scriptTaskIDFields(prefix ...string) []string {
	fields := make([]string, 0, len(prefix)+len(scriptTaskIDFieldAliases))
	fields = append(fields, prefix...)
	fields = append(fields, scriptTaskIDFieldAliases...)
	return fields
}

var scriptTaskTitleFields = []string{
	"Title",
	"task_title", "taskTitle",
	"title",
	"name",
	"label",
	"display_name", "displayName",
	"display_title", "displayTitle",
	"task_name", "taskName",
	"operation",
	"operation_name", "operationName",
	"command",
	"command_name", "commandName",
	"activity",
	"activity_name", "activityName",
	"job_name", "jobName",
	"run_name", "runName",
	"workflow_name", "workflowName",
}

var scriptTaskStateFields = []string{
	"State",
	"status",
	"state",
	"phase",
	"lifecycle",
	"task_state", "taskState",
	"task_status", "taskStatus",
	"job_status", "jobStatus",
	"run_status", "runStatus",
	"operation_status", "operationStatus",
	"result_state", "resultState",
	"result_status", "resultStatus",
	"outcome",
}

var scriptTaskDetailFields = []string{
	"Detail",
	"status_text", "statusText",
	"status_message", "statusMessage",
	"detail",
	"detail_text", "detailText",
	"message",
	"description",
	"summary",
	"current_step", "currentStep",
	"current_action", "currentAction",
	"progress_message", "progressMessage",
	"body",
	"text",
	"output",
	"output_text", "outputText",
	"result",
	"result_text", "resultText",
	"reason",
	"reason_text", "reasonText",
	"note",
	"notes",
}

var scriptTaskProgressFields = []string{
	"Progress",
	"progress_percent", "progressPercent",
	"percent",
	"percentage",
	"progress",
	"pct",
	"completion",
	"completion_percent", "completionPercent",
	"completion_percentage", "completionPercentage",
	"completed_percent", "completedPercent",
	"done_percent", "donePercent",
}

func scriptTaskStatusAliasFields() []string {
	fields := scriptTaskIDFields("ID")
	fields = append(fields, scriptTaskTitleFields...)
	fields = append(fields, scriptTaskStateFields...)
	fields = append(fields, scriptTaskDetailFields...)
	fields = append(fields, scriptTaskProgressFields...)
	return fields
}

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
	if width := intPtrJSONField(fields, "width", "w", "columns", "cols", "screen_width", "screenWidth", "terminal_width", "terminalWidth", "resize_width", "resizeWidth", "inner_width", "innerWidth", "outer_width", "outerWidth", "client_width", "clientWidth", "offset_width", "offsetWidth", "content_width", "contentWidth", "rect_width", "rectWidth", "inline_size", "inlineSize"); width != nil {
		size.Width = *width
	}
	if height := intPtrJSONField(fields, "height", "h", "rows", "screen_height", "screenHeight", "terminal_height", "terminalHeight", "resize_height", "resizeHeight", "inner_height", "innerHeight", "outer_height", "outerHeight", "client_height", "clientHeight", "offset_height", "offsetHeight", "content_height", "contentHeight", "rect_height", "rectHeight", "block_size", "blockSize"); height != nil {
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
		if size, ok := scriptSizeFromJSON(raw, 0); ok {
			return size
		}
	}
	return nil
}

func scriptSizeFromJSON(raw json.RawMessage, depth int) (*scriptSize, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	var size scriptSize
	if err := json.Unmarshal(raw, &size); err == nil && scriptSizeHasData(size) {
		return &size, true
	}
	if depth >= 8 {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if size, ok := scriptSizeFromJSON(item, depth+1); ok {
				return size, true
			}
		}
		return nil, false
	}
	if raw[0] != '{' {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	for _, name := range scriptRuntimePayloadWrapperNames(
		"size",
		"dimensions",
		"resize",
		"resize_to",
		"resizeTo",
		"screen_size",
		"screenSize",
		"terminal_size",
		"terminalSize",
		"viewport",
		"screen",
		"terminal",
		"content_rect",
		"contentRect",
		"border_box_size",
		"borderBoxSize",
		"content_box_size",
		"contentBoxSize",
		"rect",
		"bounds",
		"box",
		"target",
		"current_target",
		"currentTarget",
	) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if size, ok := scriptSizeFromJSON(nested, depth+1); ok {
			return size, true
		}
	}
	return nil, false
}

func scriptSizeHasData(size scriptSize) bool {
	return size.Width > 0 || size.Height > 0
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
		"Keybindings",
		"keybindings",
		"KeyBindings",
		"key_bindings",
		"keyBindings",
		"keyboardBindings",
		"keyboardShortcuts",
		"keyboard_shortcuts",
		"KeybindingSpecs",
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

func scriptMessagesJSONField(fields map[string]json.RawMessage, names ...string) ([]Message, bool, error) {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		messages, ok, err := scriptMessagesFromJSON(raw, 0)
		if err != nil {
			return nil, true, fmt.Errorf("%s: %w", name, err)
		}
		return messages, ok, nil
	}
	return nil, false, nil
}

func scriptMessagesFromJSON(raw json.RawMessage, depth int) ([]Message, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false, nil
	}
	if depth >= 8 {
		return nil, false, nil
	}
	switch raw[0] {
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false, err
		}
		messages := make([]Message, 0, len(items))
		for _, item := range items {
			parsed, ok, err := scriptMessagesFromJSON(item, depth+1)
			if err != nil {
				return nil, false, err
			}
			if ok {
				messages = append(messages, parsed...)
			}
		}
		return messages, true, nil
	case '{':
		fields := map[string]json.RawMessage{}
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, false, err
		}
		if scriptMessageJSONHasDirectFields(fields) {
			var message Message
			if err := json.Unmarshal(raw, &message); err != nil {
				return nil, false, err
			}
			if scriptMessageHasData(message) {
				return []Message{message}, true, nil
			}
		}
		for _, name := range scriptMessagePayloadWrapperNames() {
			nested, ok := fields[name]
			if !ok {
				continue
			}
			parsed, ok, err := scriptMessagesFromJSON(nested, depth+1)
			if err != nil {
				return nil, false, err
			}
			if ok {
				return parsed, true, nil
			}
		}
	}
	return nil, false, nil
}

func scriptMessageJSONHasDirectFields(fields map[string]json.RawMessage) bool {
	for _, name := range []string{
		"Role",
		"role",
		"type",
		"speaker",
		"Text",
		"text",
		"content",
		"body",
		"message",
		"contentBlocks",
		"content_blocks",
		"blocks",
		"messageContent",
		"message_content",
		"imagePasteId",
		"imagePasteID",
		"image_paste_id",
		"imagePasteIds",
		"imagePasteIDs",
		"image_paste_ids",
		"pastedContents",
		"pasted_contents",
		"pastedContent",
		"pasted_content",
		"attachments",
		"attachment",
	} {
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}

func scriptMessageHasData(message Message) bool {
	return message.Text != "" ||
		len(message.ContentBlocks) > 0 ||
		len(message.ImagePasteIDs) > 0 ||
		len(message.PastedContents) > 0
}

func scriptMessagePayloadWrapperNames() []string {
	return []string{
		"messages",
		"append_messages",
		"appendMessages",
		"transcript_messages",
		"transcriptMessages",
		"message",
		"record",
		"entry",
		"item",
		"event",
		"data",
		"payload",
		"result",
		"response",
		"output",
		"body",
		"resources",
		"included",
		"collection",
		"collections",
		"list",
		"lists",
		"children",
		"values",
		"nodes",
		"edges",
		"edge",
		"node",
		"resource",
		"attributes",
		"properties",
		"attrs",
	}
}

func (step *ScriptStep) UnmarshalJSON(data []byte) error {
	data = unwrapScriptStepJSON(data)
	data = mergeScriptStepExpectationJSON(data)
	rawStepData := data
	data = normalizeScriptStepJSON(data)
	type alias ScriptStep
	var raw alias
	if err := json.Unmarshal(stripScriptStepRawScalarAliasFields(data), &raw); err != nil {
		return err
	}
	*step = ScriptStep(raw)

	var fields struct {
		RequestPermission         *json.RawMessage `json:"request_permission"`
		RequestPermissionCamel    *json.RawMessage `json:"requestPermission"`
		Key                       *json.RawMessage `json:"key"`
		Keys                      *json.RawMessage `json:"keys"`
		RawKey                    *json.RawMessage `json:"raw_key"`
		RawKeyCamel               *json.RawMessage `json:"rawKey"`
		KeySequence               *json.RawMessage `json:"key_sequence"`
		KeySequenceCamel          *json.RawMessage `json:"keySequence"`
		Press                     *json.RawMessage `json:"press"`
		PressKey                  *json.RawMessage `json:"press_key"`
		PressKeyCamel             *json.RawMessage `json:"pressKey"`
		KeyPress                  *json.RawMessage `json:"key_press"`
		KeyPressCamel             *json.RawMessage `json:"keyPress"`
		Keypress                  *json.RawMessage `json:"keypress"`
		Shortcut                  *json.RawMessage `json:"shortcut"`
		ShortcutKey               *json.RawMessage `json:"shortcut_key"`
		ShortcutKeyCamel          *json.RawMessage `json:"shortcutKey"`
		Presses                   *json.RawMessage `json:"presses"`
		KeyPresses                *json.RawMessage `json:"key_presses"`
		KeyPressesCamel           *json.RawMessage `json:"keyPresses"`
		Keypresses                *json.RawMessage `json:"keypresses"`
		Shortcuts                 *json.RawMessage `json:"shortcuts"`
		Input                     *json.RawMessage `json:"input"`
		InputText                 *json.RawMessage `json:"input_text"`
		InputTextCamel            *json.RawMessage `json:"inputText"`
		TextInput                 *json.RawMessage `json:"text_input"`
		TextInputCamel            *json.RawMessage `json:"textInput"`
		KeysText                  *json.RawMessage `json:"keys_text"`
		KeysTextCamel             *json.RawMessage `json:"keysText"`
		PasteText                 *json.RawMessage `json:"paste_text"`
		PasteTextCamel            *json.RawMessage `json:"pasteText"`
		PastedText                *json.RawMessage `json:"pasted_text"`
		PastedTextCamel           *json.RawMessage `json:"pastedText"`
		Clipboard                 *json.RawMessage `json:"clipboard"`
		ClipboardData             *json.RawMessage `json:"clipboard_data"`
		ClipboardDataCamel        *json.RawMessage `json:"clipboardData"`
		DataTransfer              *json.RawMessage `json:"data_transfer"`
		DataTransferCamel         *json.RawMessage `json:"dataTransfer"`
		Messages                  *json.RawMessage `json:"messages"`
		AppendMessages            *json.RawMessage `json:"append_messages"`
		AppendMessagesCamel       *json.RawMessage `json:"appendMessages"`
		TranscriptMessages        *json.RawMessage `json:"transcript_messages"`
		TranscriptMessagesCamel   *json.RawMessage `json:"transcriptMessages"`
		Status                    *json.RawMessage `json:"status"`
		SetStatus                 *json.RawMessage `json:"set_status"`
		SetStatusCamel            *json.RawMessage `json:"setStatus"`
		StatusLine                *json.RawMessage `json:"status_line"`
		StatusLineCamel           *json.RawMessage `json:"statusLine"`
		BaseStatus                *json.RawMessage `json:"base_status"`
		BaseStatusCamel           *json.RawMessage `json:"baseStatus"`
		Mouse                     *json.RawMessage `json:"mouse"`
		MouseEvent                *json.RawMessage `json:"mouse_event"`
		MouseEventCamel           *json.RawMessage `json:"mouseEvent"`
		Keybindings               *json.RawMessage `json:"keybindings"`
		KeyBindings               *json.RawMessage `json:"key_bindings"`
		KeyBindingsCamel          *json.RawMessage `json:"keyBindings"`
		KeybindingSpecs           *json.RawMessage `json:"keybinding_specs"`
		KeybindingSpecsCamel      *json.RawMessage `json:"keybindingSpecs"`
		UpsertTask                *json.RawMessage `json:"upsert_task"`
		UpsertTaskCamel           *json.RawMessage `json:"upsertTask"`
		RemoveTaskID              *json.RawMessage `json:"remove_task_id"`
		RemoveTaskIDCamel         *json.RawMessage `json:"removeTaskId"`
		RemoveTaskIDUpperCamel    *json.RawMessage `json:"removeTaskID"`
		RemoveTask                *json.RawMessage `json:"remove_task"`
		RemoveTaskCamel           *json.RawMessage `json:"removeTask"`
		DeleteTask                *json.RawMessage `json:"delete_task"`
		DeleteTaskCamel           *json.RawMessage `json:"deleteTask"`
		CancelActiveDialog        *json.RawMessage `json:"cancel_active_dialog"`
		CancelActiveDialogCamel   *json.RawMessage `json:"cancelActiveDialog"`
		CancelActive              *json.RawMessage `json:"cancel_active"`
		CancelActiveCamel         *json.RawMessage `json:"cancelActive"`
		CancelDialog              *json.RawMessage `json:"cancel_dialog"`
		CancelDialogCamel         *json.RawMessage `json:"cancelDialog"`
		CloseDialog               *json.RawMessage `json:"close_dialog"`
		CloseDialogCamel          *json.RawMessage `json:"closeDialog"`
		CancelPermissionID        *json.RawMessage `json:"cancel_permission_id"`
		CancelPermissionIDAlt     *json.RawMessage `json:"cancelPermissionId"`
		CancelPermissionIDUpper   *json.RawMessage `json:"cancelPermissionID"`
		CancelPermission          *json.RawMessage `json:"cancel_permission"`
		CancelPermissionCamel     *json.RawMessage `json:"cancelPermission"`
		CancelAllPermissions      *json.RawMessage `json:"cancel_all_permissions"`
		CancelAllPermissionsCamel *json.RawMessage `json:"cancelAllPermissions"`
		CancelPermissions         *json.RawMessage `json:"cancel_permissions"`
		CancelPermissionsCamel    *json.RawMessage `json:"cancelPermissions"`
		CancelAllTasks            *json.RawMessage `json:"cancel_all_tasks"`
		CancelAllTasksCamel       *json.RawMessage `json:"cancelAllTasks"`
		CancelTasks               *json.RawMessage `json:"cancel_tasks"`
		CancelTasksCamel          *json.RawMessage `json:"cancelTasks"`
		CancelTasksDetail         *json.RawMessage `json:"cancel_tasks_detail"`
		CancelTasksDetailCamel    *json.RawMessage `json:"cancelTasksDetail"`
		CancelReason              *json.RawMessage `json:"cancel_reason"`
		CancelReasonCamel         *json.RawMessage `json:"cancelReason"`
		OpenTasksDialog           *json.RawMessage `json:"open_tasks_dialog"`
		OpenTasksDialogCamel      *json.RawMessage `json:"openTasksDialog"`
		OpenTasks                 *json.RawMessage `json:"open_tasks"`
		OpenTasksCamel            *json.RawMessage `json:"openTasks"`
		ShowTasks                 *json.RawMessage `json:"show_tasks"`
		ShowTasksCamel            *json.RawMessage `json:"showTasks"`
		ResizeWidth               *json.RawMessage `json:"resize_width"`
		ResizeWidthCamel          *json.RawMessage `json:"resizeWidth"`
		ResizeHeight              *json.RawMessage `json:"resize_height"`
		ResizeHeightCamel         *json.RawMessage `json:"resizeHeight"`
		SnapshotName              *json.RawMessage `json:"snapshot_name"`
		SnapshotNameCamel         *json.RawMessage `json:"snapshotName"`
		Focus                     *json.RawMessage `json:"focus"`
		Focused                   *json.RawMessage `json:"focused"`
		FocusIn                   *json.RawMessage `json:"focus_in"`
		FocusInCamel              *json.RawMessage `json:"focusIn"`
		FocusOut                  *json.RawMessage `json:"focus_out"`
		FocusOutCamel             *json.RawMessage `json:"focusOut"`
		Blur                      *json.RawMessage `json:"blur"`
		Blurred                   *json.RawMessage `json:"blurred"`
		ExpectEvent               *json.RawMessage `json:"expect_event"`
		ExpectEventCamel          *json.RawMessage `json:"expectEvent"`
		ExpectEvents              *json.RawMessage `json:"expect_events"`
		ExpectEventsCamel         *json.RawMessage `json:"expectEvents"`
		ExpectNoEvent             *json.RawMessage `json:"expect_no_event"`
		ExpectNoEventCamel        *json.RawMessage `json:"expectNoEvent"`
		ExpectEventCount          *json.RawMessage `json:"expect_event_count"`
		ExpectEventCountCamel     *json.RawMessage `json:"expectEventCount"`
		ExpectTotalEventCount     *json.RawMessage `json:"expect_total_event_count"`
		ExpectTotalEventCamel     *json.RawMessage `json:"expectTotalEventCount"`
		ExpectDialogResult        *json.RawMessage `json:"expect_dialog_result"`
		ExpectDialogResultCamel   *json.RawMessage `json:"expectDialogResult"`
		ExpectDialogResults       *json.RawMessage `json:"expect_dialog_results"`
		ExpectDialogResultsCamel  *json.RawMessage `json:"expectDialogResults"`
		ExpectNoDialogResult      *json.RawMessage `json:"expect_no_dialog_result"`
		ExpectNoDialogResultCamel *json.RawMessage `json:"expectNoDialogResult"`
		ExpectNoDialogResults     *json.RawMessage `json:"expect_no_dialog_results"`
		ExpectDialogResultCount   *json.RawMessage `json:"expect_dialog_result_count"`
		ExpectDialogCountCamel    *json.RawMessage `json:"expectDialogResultCount"`
		ExpectTotalDialogCount    *json.RawMessage `json:"expect_total_dialog_result_count"`
		ExpectTotalDialogCamel    *json.RawMessage `json:"expectTotalDialogResultCount"`
		ExpectDialog              *json.RawMessage `json:"expect_dialog"`
		ExpectDialogCamel         *json.RawMessage `json:"expectDialog"`
		ExpectPrompt              *json.RawMessage `json:"expect_prompt"`
		ExpectPromptCamel         *json.RawMessage `json:"expectPrompt"`
		ExpectVim                 *json.RawMessage `json:"expect_vim"`
		ExpectVimCamel            *json.RawMessage `json:"expectVim"`
		ExpectTasks               *json.RawMessage `json:"expect_tasks"`
		ExpectTasksCamel          *json.RawMessage `json:"expectTasks"`
		ExpectReverseSearch       *json.RawMessage `json:"expect_reverse_search"`
		ExpectReverseSearchCamel  *json.RawMessage `json:"expectReverseSearch"`
		ExpectViewport            *json.RawMessage `json:"expect_viewport"`
		ExpectViewportCamel       *json.RawMessage `json:"expectViewport"`
		ExpectScreen              *json.RawMessage `json:"expect_screen"`
		ExpectScreenCamel         *json.RawMessage `json:"expectScreen"`
		ExpectFocused             *json.RawMessage `json:"expect_focused"`
		ExpectFocusedCamel        *json.RawMessage `json:"expectFocused"`
		ExpectStatusContains      *json.RawMessage `json:"expect_status_contains"`
		ExpectStatusContainsCamel *json.RawMessage `json:"expectStatusContains"`
		ExpectStatusNotContains   *json.RawMessage `json:"expect_status_not_contains"`
		ExpectStatusNotCamel      *json.RawMessage `json:"expectStatusNotContains"`
		ExpectSnapshotContains    *json.RawMessage `json:"expect_snapshot_contains"`
		ExpectSnapshotCamel       *json.RawMessage `json:"expectSnapshotContains"`
		ExpectSnapshotNotContains *json.RawMessage `json:"expect_snapshot_not_contains"`
		ExpectSnapshotNotCamel    *json.RawMessage `json:"expectSnapshotNotContains"`
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
	if request := permissionRequestJSONField(fieldMap, "RequestPermission", "request_permission", "requestPermission", "permission", "permission_request", "permissionRequest", "request"); request != nil {
		step.RequestPermission = request
	}
	if values := scriptNamedStringListField(rawFieldMap,
		[]string{
			"Key",
			"key",
			"raw_key",
			"rawKey",
			"key_sequence",
			"keySequence",
			"sequence",
			"press",
			"press_key",
			"pressKey",
			"key_press",
			"keyPress",
			"keypress",
			"shortcut",
			"shortcut_key",
			"shortcutKey",
		},
		"key",
		"keys",
		"sequence",
		"key_sequence",
		"keySequence",
		"shortcut",
		"input",
		"text",
		"content",
		"body",
		"message",
		"value",
	); len(values) == 1 {
		step.Key = values[0]
	} else if len(values) > 1 {
		step.Keys = append(step.Keys, values...)
	}
	if values := scriptNamedStringListField(rawFieldMap,
		[]string{
			"Keys",
			"keys",
			"presses",
			"key_presses",
			"keyPresses",
			"keypresses",
			"shortcuts",
		},
		"keys",
		"sequence",
		"key_sequence",
		"keySequence",
		"shortcuts",
		"key",
		"shortcut",
		"input",
		"text",
		"content",
		"body",
		"message",
		"value",
	); len(values) > 0 {
		step.Keys = append(step.Keys, values...)
	}
	if step.Text == "" {
		step.Text = scriptNamedStringField(rawFieldMap,
			[]string{
				"Text",
				"text",
				"input",
				"input_text",
				"inputText",
				"text_input",
				"textInput",
				"keys_text",
				"keysText",
			},
			"text",
			"input",
			"content",
			"body",
			"message",
			"value",
		)
	}
	if step.Paste == "" {
		step.Paste = scriptNamedStringField(rawFieldMap,
			[]string{
				"Paste",
				"paste",
				"paste_text",
				"pasteText",
				"pasted_text",
				"pastedText",
				"clipboard",
				"clipboard_data",
				"clipboardData",
				"data_transfer",
				"dataTransfer",
			},
			"paste",
			"clipboard",
			"clipboard_data",
			"clipboardData",
			"data_transfer",
			"dataTransfer",
			"text/plain",
			"textPlain",
			"plain_text",
			"plainText",
			"clipboard_text",
			"clipboardText",
			"items",
			"item",
			"clipboard_items",
			"clipboardItems",
			"text",
			"content",
			"body",
			"message",
			"value",
		)
	}
	if messages, ok, err := scriptMessagesJSONField(rawFieldMap, "Message", "message"); err != nil {
		return err
	} else if ok {
		if len(messages) == 1 {
			message := messages[0]
			step.Message = &message
		} else {
			step.Messages = messages
		}
	}
	if messages, ok, err := scriptMessagesJSONField(rawFieldMap, "Messages", "messages"); err != nil {
		return err
	} else if ok {
		step.Messages = messages
	}
	if messages, ok, err := scriptMessagesJSONField(rawFieldMap, "append_messages"); err != nil {
		return err
	} else if ok {
		step.Messages = messages
	}
	if messages, ok, err := scriptMessagesJSONField(rawFieldMap, "appendMessages"); err != nil {
		return err
	} else if ok {
		step.Messages = messages
	}
	if messages, ok, err := scriptMessagesJSONField(rawFieldMap, "transcript_messages"); err != nil {
		return err
	} else if ok {
		step.Messages = messages
	}
	if messages, ok, err := scriptMessagesJSONField(rawFieldMap, "transcriptMessages"); err != nil {
		return err
	} else if ok {
		step.Messages = messages
	}
	if step.Status == "" {
		step.Status = scriptNamedStringField(rawFieldMap,
			[]string{
				"Status",
				"status",
				"set_status",
				"setStatus",
				"status_line",
				"statusLine",
				"base_status",
				"baseStatus",
			},
			"status",
			"text",
			"message",
			"content",
			"body",
			"value",
		)
	}
	if mouse := scriptMouseJSONField(rawFieldMap, "Mouse", "mouse", "mouse_event", "mouseEvent"); mouse != nil {
		step.Mouse = mouse
	}
	if dialog := scriptDialogJSONField(rawFieldMap, "Dialog", "dialog"); dialog != nil {
		step.Dialog = dialog
	}
	if image := scriptImageJSONField(rawFieldMap, "Image", "image"); image != nil {
		step.Image = image
	}
	if specs, ok, err := scriptKeybindingsJSONField(rawFieldMap); err != nil {
		return err
	} else if ok {
		step.Keybindings = specs
	}
	if task := taskStatusJSONField(fieldMap, "UpsertTask", "upsert_task", "upsertTask", "task", "task_status", "taskStatus"); task != nil {
		step.UpsertTask = task
	}
	if step.RemoveTaskID == "" && scriptHasAnyJSONField(fieldMap, "RemoveTaskID", "remove_task_id", "removeTaskId", "removeTaskID", "RemoveTask", "remove_task", "removeTask", "DeleteTask", "delete_task", "deleteTask") {
		step.RemoveTaskID = scriptActionIDField(fieldMap, scriptTaskIDFields("RemoveTaskID", "remove_task_id", "removeTaskId", "removeTaskID", "RemoveTask", "remove_task", "removeTask", "DeleteTask", "delete_task", "deleteTask", "task", "task_status", "taskStatus")...)
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "CancelActiveDialog", "cancel_active_dialog", "cancelActiveDialog", "CancelActive", "cancel_active", "cancelActive", "CancelDialog", "cancel_dialog", "cancelDialog", "CloseDialog", "close_dialog", "closeDialog"); ok {
		step.CancelActiveDialog = value
	}
	if step.CancelPermissionID == "" && scriptHasAnyJSONField(fieldMap, "CancelPermissionID", "cancel_permission_id", "cancelPermissionId", "cancelPermissionID", "CancelPermission", "cancel_permission", "cancelPermission") {
		step.CancelPermissionID = scriptActionIDField(fieldMap, "CancelPermissionID", "cancel_permission_id", "cancelPermissionId", "cancelPermissionID", "CancelPermission", "cancel_permission", "cancelPermission", "permission", "permission_request", "permissionRequest", "request", "permission_id", "permissionId", "permissionID", "request_id", "requestId", "requestID", "dialog_id", "dialogId", "dialogID", "tool_use_id", "toolUseId", "toolUseID", "operation_id", "operationId", "operationID", "id")
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "CancelAllPermissions", "cancel_all_permissions", "cancelAllPermissions", "CancelPermissions", "cancel_permissions", "cancelPermissions"); ok {
		step.CancelAllPermissions = value
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "CancelAllTasks", "cancel_all_tasks", "cancelAllTasks", "CancelTasks", "cancel_tasks", "cancelTasks"); ok {
		step.CancelAllTasks = value
	}
	if step.CancelTasksDetail == "" {
		step.CancelTasksDetail = scriptActionStringField(fieldMap, "CancelAllTasks", "cancel_all_tasks", "cancelAllTasks", "CancelTasks", "cancel_tasks", "cancelTasks", "CancelTasksDetail", "cancel_tasks_detail", "cancelTasksDetail", "CancelReason", "cancel_reason", "cancelReason", "reason", "reason_text", "reasonText", "detail", "message", "description", "body", "text", "status_text", "statusText")
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "OpenTasksDialog", "open_tasks_dialog", "openTasksDialog", "OpenTasks", "open_tasks", "openTasks", "ShowTasks", "show_tasks", "showTasks"); ok {
		step.OpenTasksDialog = value
	}
	if step.ResizeWidth <= 0 {
		if width, ok := scriptNamedIntField(rawFieldMap,
			[]string{"ResizeWidth", "resize_width", "resizeWidth"},
			"width",
			"w",
			"columns",
			"cols",
			"screen_width",
			"screenWidth",
			"terminal_width",
			"terminalWidth",
			"resize_width",
			"resizeWidth",
			"inner_width",
			"innerWidth",
			"outer_width",
			"outerWidth",
			"client_width",
			"clientWidth",
			"offset_width",
			"offsetWidth",
			"content_width",
			"contentWidth",
			"rect_width",
			"rectWidth",
			"inline_size",
			"inlineSize",
			"value",
		); ok {
			step.ResizeWidth = width
		}
	}
	if step.ResizeHeight <= 0 {
		if height, ok := scriptNamedIntField(rawFieldMap,
			[]string{"ResizeHeight", "resize_height", "resizeHeight"},
			"height",
			"h",
			"rows",
			"screen_height",
			"screenHeight",
			"terminal_height",
			"terminalHeight",
			"resize_height",
			"resizeHeight",
			"inner_height",
			"innerHeight",
			"outer_height",
			"outerHeight",
			"client_height",
			"clientHeight",
			"offset_height",
			"offsetHeight",
			"content_height",
			"contentHeight",
			"rect_height",
			"rectHeight",
			"block_size",
			"blockSize",
			"value",
		); ok {
			step.ResizeHeight = height
		}
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
		if width, ok := scriptNamedIntField(rawFieldMap,
			[]string{"width", "columns", "cols", "screen_width", "screenWidth", "terminal_width", "terminalWidth", "inner_width", "innerWidth", "outer_width", "outerWidth", "client_width", "clientWidth", "offset_width", "offsetWidth", "content_width", "contentWidth", "rect_width", "rectWidth", "inline_size", "inlineSize"},
			"width",
			"w",
			"columns",
			"cols",
			"screen_width",
			"screenWidth",
			"terminal_width",
			"terminalWidth",
			"resize_width",
			"resizeWidth",
			"inner_width",
			"innerWidth",
			"outer_width",
			"outerWidth",
			"client_width",
			"clientWidth",
			"offset_width",
			"offsetWidth",
			"content_width",
			"contentWidth",
			"rect_width",
			"rectWidth",
			"inline_size",
			"inlineSize",
			"value",
		); ok {
			step.ResizeWidth = width
		}
	}
	if step.ResizeHeight <= 0 {
		if height, ok := scriptNamedIntField(rawFieldMap,
			[]string{"height", "rows", "screen_height", "screenHeight", "terminal_height", "terminalHeight", "inner_height", "innerHeight", "outer_height", "outerHeight", "client_height", "clientHeight", "offset_height", "offsetHeight", "content_height", "contentHeight", "rect_height", "rectHeight", "block_size", "blockSize"},
			"height",
			"h",
			"rows",
			"screen_height",
			"screenHeight",
			"terminal_height",
			"terminalHeight",
			"resize_height",
			"resizeHeight",
			"inner_height",
			"innerHeight",
			"outer_height",
			"outerHeight",
			"client_height",
			"clientHeight",
			"offset_height",
			"offsetHeight",
			"content_height",
			"contentHeight",
			"rect_height",
			"rectHeight",
			"block_size",
			"blockSize",
			"value",
		); ok {
			step.ResizeHeight = height
		}
	}
	if step.SnapshotName == "" {
		step.SnapshotName = scriptNamedStringField(rawFieldMap,
			[]string{
				"SnapshotName",
				"snapshotName",
				"snapshot_name",
				"snapshot",
				"snapshot_id",
				"snapshotId",
				"snapshot_label",
				"snapshotLabel",
				"capture_name",
				"captureName",
				"baseline_name",
				"baselineName",
			},
			"snapshot",
			"name",
			"label",
			"id",
			"value",
		)
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "focus", "focused"); ok {
		step.Keys = append(step.Keys, scriptFocusKey(value))
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "focus_in", "focusIn"); ok && value {
		step.Keys = append(step.Keys, "focus-in")
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "focus_out", "focusOut", "blur"); ok && value {
		step.Keys = append(step.Keys, "focus-out")
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "blurred"); ok {
		step.Keys = append(step.Keys, scriptFocusKey(!value))
	}
	if events := scriptNamedEventListField(fieldMap,
		[]string{"ExpectEvent", "expect_event", "expectEvent"},
		"event",
		"expected_event",
		"expectedEvent",
		"expected",
		"items",
		"entries",
		"nodes",
		"results",
		"value",
	); len(events) > 0 {
		event := events[0]
		step.ExpectEvent = &event
	}
	if events := scriptNamedEventListField(fieldMap,
		[]string{"ExpectEvents", "expect_events", "expectEvents"},
		"events",
		"event",
		"expected_events",
		"expectedEvents",
		"items",
		"entries",
		"nodes",
		"results",
		"value",
	); len(events) > 0 {
		step.ExpectEvents = events
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "ExpectNoEvent", "expect_no_event", "expectNoEvent"); ok {
		step.ExpectNoEvent = value
	}
	if count, ok := scriptNamedIntField(fieldMap,
		[]string{"ExpectEventCount", "expect_event_count", "expectEventCount"},
		"count",
		"event_count",
		"eventCount",
		"expected",
		"value",
	); ok {
		step.ExpectEventCount = &count
	}
	if count, ok := scriptNamedIntField(fieldMap,
		[]string{"ExpectTotalEventCount", "expect_total_event_count", "expectTotalEventCount"},
		"total",
		"total_count",
		"totalCount",
		"event_count",
		"eventCount",
		"expected",
		"value",
	); ok {
		step.ExpectTotalEventCount = &count
	}
	if results := scriptNamedDialogResultListField(fieldMap,
		[]string{"ExpectDialogResult", "expect_dialog_result", "expectDialogResult"},
		"dialog_result",
		"dialogResult",
		"result",
		"expected_result",
		"expectedResult",
		"expected",
		"items",
		"entries",
		"nodes",
		"value",
	); len(results) > 0 {
		result := results[0]
		step.ExpectDialogResult = &result
	}
	if results := scriptNamedDialogResultListField(fieldMap,
		[]string{"ExpectDialogResults", "expect_dialog_results", "expectDialogResults"},
		"dialog_results",
		"dialogResults",
		"results",
		"result",
		"expected_results",
		"expectedResults",
		"items",
		"entries",
		"nodes",
		"value",
	); len(results) > 0 {
		step.ExpectDialogResults = results
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "ExpectNoDialogResult", "expect_no_dialog_result", "expectNoDialogResult", "ExpectNoDialogResults", "expect_no_dialog_results"); ok {
		step.ExpectNoDialogResult = value
	}
	if count, ok := scriptNamedIntField(fieldMap,
		[]string{"ExpectDialogResultCount", "expect_dialog_result_count", "expectDialogResultCount"},
		"count",
		"dialog_result_count",
		"dialogResultCount",
		"result_count",
		"resultCount",
		"expected",
		"value",
	); ok {
		step.ExpectDialogResultCount = &count
	}
	if count, ok := scriptNamedIntField(fieldMap,
		[]string{"ExpectTotalDialogResultCount", "expect_total_dialog_result_count", "expectTotalDialogResultCount"},
		"total",
		"total_count",
		"totalCount",
		"dialog_result_count",
		"dialogResultCount",
		"result_count",
		"resultCount",
		"expected",
		"value",
	); ok {
		step.ExpectTotalDialogResultCount = &count
	}
	if dialog := scriptNamedDialogExpectationField(fieldMap,
		[]string{"ExpectDialog", "expect_dialog", "expectDialog"},
		"dialog",
		"modal",
		"expectation",
		"expected",
		"expect_dialog",
		"expectDialog",
	); dialog != nil {
		step.ExpectDialog = dialog
	}
	if prompt := scriptNamedPromptExpectationField(fieldMap,
		[]string{"ExpectPrompt", "expect_prompt", "expectPrompt"},
		"prompt",
		"expectation",
		"expected",
		"expect_prompt",
		"expectPrompt",
	); prompt != nil {
		step.ExpectPrompt = prompt
	}
	if vim := scriptNamedVimExpectationField(fieldMap,
		[]string{"ExpectVim", "expect_vim", "expectVim"},
		"vim",
		"vim_state",
		"vimState",
		"expectation",
		"expected",
		"expect_vim",
		"expectVim",
	); vim != nil {
		step.ExpectVim = vim
	}
	if tasks := scriptNamedTasksExpectationField(fieldMap,
		[]string{"ExpectTasks", "expect_tasks", "expectTasks"},
		"tasks",
		"task_expectation",
		"taskExpectation",
		"expectation",
		"expected",
		"expect_tasks",
		"expectTasks",
	); tasks != nil {
		step.ExpectTasks = tasks
	}
	if reverse := scriptNamedReverseSearchExpectationField(fieldMap,
		[]string{"ExpectReverseSearch", "expect_reverse_search", "expectReverseSearch"},
		"reverse_search",
		"reverseSearch",
		"search",
		"expectation",
		"expected",
		"expect_reverse_search",
		"expectReverseSearch",
	); reverse != nil {
		step.ExpectReverseSearch = reverse
	}
	if viewport := scriptNamedViewportExpectationField(fieldMap,
		[]string{"ExpectViewport", "expect_viewport", "expectViewport"},
		"viewport",
		"view",
		"expectation",
		"expected",
		"expect_viewport",
		"expectViewport",
	); viewport != nil {
		step.ExpectViewport = viewport
	}
	if screen := scriptNamedScreenExpectationField(fieldMap,
		[]string{"ExpectScreen", "expect_screen", "expectScreen"},
		"screen",
		"terminal",
		"terminal_size",
		"terminalSize",
		"size",
		"expectation",
		"expected",
		"expect_screen",
		"expectScreen",
	); screen != nil {
		step.ExpectScreen = screen
	}
	if value, ok := scriptRuntimeMutationBoolField(fieldMap, "ExpectFocused", "expect_focused", "expectFocused"); ok {
		focused := value
		step.ExpectFocused = &focused
	}
	if values := scriptNamedStringListField(fieldMap,
		[]string{"ExpectStatusContains", "expect_status_contains", "expectStatusContains"},
		"contains",
		"status",
		"text",
		"content",
		"message",
		"values",
		"items",
		"value",
	); len(values) > 0 {
		step.ExpectStatusContains = values
	}
	if values := scriptNamedStringListField(fieldMap,
		[]string{"ExpectStatusNotContains", "expect_status_not_contains", "expectStatusNotContains"},
		"not_contains",
		"notContains",
		"contains",
		"status",
		"text",
		"content",
		"message",
		"values",
		"items",
		"value",
	); len(values) > 0 {
		step.ExpectStatusNotContains = values
	}
	if values := scriptNamedStringListField(fieldMap,
		[]string{"ExpectSnapshotContains", "expect_snapshot_contains", "expectSnapshotContains"},
		"contains",
		"snapshot",
		"text",
		"content",
		"message",
		"values",
		"items",
		"value",
	); len(values) > 0 {
		step.ExpectSnapshotContains = values
	}
	if values := scriptNamedStringListField(fieldMap,
		[]string{"ExpectSnapshotNotContains", "expect_snapshot_not_contains", "expectSnapshotNotContains"},
		"not_contains",
		"notContains",
		"contains",
		"snapshot",
		"text",
		"content",
		"message",
		"values",
		"items",
		"value",
	); len(values) > 0 {
		step.ExpectSnapshotNotContains = values
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
		"clipboard_data",
		"clipboardData",
		"data_transfer",
		"dataTransfer",
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
			if values := scriptActionStringListField(fields, "key", "keys", "shortcut", "sequence", "input", "text", "data", "payload", "value"); len(values) == 1 {
				step.Key = values[0]
			} else if len(values) > 1 {
				step.Keys = append(step.Keys, values...)
			}
		}
	case "keys", "presses", "shortcuts", "sequence", "keysequence", "key-sequence", "keyseq", "key-seq":
		if step.Key == "" && len(step.Keys) == 0 {
			step.Keys = append(step.Keys, scriptActionStringListField(fields, "keys", "sequence", "key_sequence", "keySequence", "shortcuts", "data", "payload", "value")...)
		}
	case "text", "type", "typetext", "type-text", "input", "inputtext", "textinput", "insert", "inserttext", "write", "writetext":
		if step.Text == "" {
			step.Text = scriptActionStringField(fields, "text", "input", "content", "body", "message", "data", "payload", "value")
		}
	case "paste", "pastetext", "pastedtext", "clipboard", "clipboardtext":
		if step.Paste == "" {
			step.Paste = scriptActionStringField(fields, "paste", "clipboard", "clipboard_data", "clipboardData", "data_transfer", "dataTransfer", "text/plain", "textPlain", "plain_text", "plainText", "clipboard_text", "clipboardText", "items", "item", "clipboard_items", "clipboardItems", "text", "content", "body", "message", "data", "payload", "value")
		}
	case "status", "setstatus", "set-status", "statusline", "status-line":
		if step.Status == "" {
			step.Status = scriptActionStringField(fields, "status", "text", "content", "body", "message", "data", "payload", "value")
		}
	case "snapshot", "capture", "capture-snapshot":
		if step.SnapshotName == "" {
			step.SnapshotName = scriptActionStringField(fields, "snapshot", "name", "label", "id", "data", "payload", "value")
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
			step.RemoveTaskID = scriptActionIDField(fields, scriptTaskIDFields("task", "task_status", "taskStatus")...)
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
	case "focus", "focusin", "focus-in", "focused", "setfocus", "set-focus", "focusstate", "focus-state":
		if !scriptStepHasFocusKey(step) {
			step.Keys = append(step.Keys, scriptFocusKey(scriptActionBoolField(fields, true)))
		}
	case "blur", "focusout", "focus-out", "blurred":
		if !scriptStepHasFocusKey(step) {
			step.Keys = append(step.Keys, scriptFocusKey(!scriptActionBoolField(fields, true)))
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
	if resourceType := normalizedScriptRuntimeResourceType(fields); resourceType != "" && !scriptPermissionResourceType(resourceType) {
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
	if resourceType := normalizedScriptRuntimeResourceType(fields); resourceType != "" && !scriptTaskResourceType(resourceType) {
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
				task.ID = scalarStringJSONField(fields, scriptTaskIDFields()...)
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
		"resources",
		"included",
		"collection",
		"collections",
		"list",
		"lists",
		"children",
		"values",
		"records",
		"entries",
		"nodes",
		"edges",
		"results",
	}
	return append(wrappers, names...)
}

func normalizedScriptRuntimeResourceType(fields map[string]json.RawMessage) string {
	value := stringJSONField(fields, "type", "resource_type", "resourceType", "kind")
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func scriptPermissionResourceType(resourceType string) bool {
	return strings.Contains(resourceType, "permission") ||
		strings.Contains(resourceType, "approval") ||
		strings.Contains(resourceType, "allowance") ||
		strings.Contains(resourceType, "toolrequest")
}

func scriptTaskResourceType(resourceType string) bool {
	return strings.Contains(resourceType, "task") ||
		strings.Contains(resourceType, "job") ||
		strings.Contains(resourceType, "run") ||
		strings.Contains(resourceType, "status")
}

func scriptDialogActionField(fields map[string]json.RawMessage) *Dialog {
	for _, raw := range scriptActionRawFields(fields, "dialog") {
		if dialog, ok := scriptDialogFromJSON(raw, 0); ok {
			return dialog
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

func scriptDialogJSONField(fields map[string]json.RawMessage, names ...string) *Dialog {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if dialog, ok := scriptDialogFromJSON(raw, 0); ok {
			return dialog
		}
	}
	return nil
}

func scriptDialogFromJSON(raw json.RawMessage, depth int) (*Dialog, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	var dialog Dialog
	hasDialog := json.Unmarshal(raw, &dialog) == nil && scriptDialogHasData(dialog)
	if depth >= 8 {
		if hasDialog {
			return &dialog, true
		}
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if dialog, ok := scriptDialogFromJSON(item, depth+1); ok {
				return dialog, true
			}
		}
		return nil, false
	}
	if raw[0] != '{' {
		if hasDialog {
			return &dialog, true
		}
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		if hasDialog {
			return &dialog, true
		}
		return nil, false
	}
	if hasDialog && !scriptHasStructuredRuntimePayloadWrapper(fields) {
		return &dialog, true
	}
	for _, name := range scriptRuntimePayloadWrapperNames("dialog") {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if dialog, ok := scriptDialogFromJSON(nested, depth+1); ok {
			return dialog, true
		}
	}
	if hasDialog {
		return &dialog, true
	}
	return nil, false
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

func scriptNamedStringField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) string {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if value := scriptActionStringFromJSON(raw, nestedNames, 0); value != "" {
			return value
		}
	}
	return ""
}

func scriptNamedIntField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) (int, bool) {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if value, ok := scriptActionIntFromJSON(raw, nestedNames, 0); ok {
			return value, true
		}
	}
	return 0, false
}

func scriptNamedEventListField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) []ScreenEvent {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if events := scriptEventListFromJSON(raw, nestedNames, 0); len(events) > 0 {
			return events
		}
	}
	return nil
}

func scriptEventListFromJSON(raw json.RawMessage, names []string, depth int) []ScreenEvent {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil
		}
		out := []ScreenEvent{}
		for _, item := range items {
			out = append(out, scriptEventListFromJSON(item, names, depth+1)...)
		}
		return out
	}
	if depth >= 8 || raw[0] != '{' {
		return nil
	}
	var event ScreenEvent
	if err := json.Unmarshal(raw, &event); err == nil && scriptScreenEventHasData(event) {
		return []ScreenEvent{event}
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	for _, name := range scriptRuntimePayloadWrapperNames(names...) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if events := scriptEventListFromJSON(nested, names, depth+1); len(events) > 0 {
			return events
		}
	}
	return nil
}

func scriptScreenEventHasData(event ScreenEvent) bool {
	return event.Type != "" || event.Value != "" || event.DialogID != "" || event.DialogKind != "" || event.Display != "" || len(event.PastedContents) > 0
}

func scriptNamedDialogResultListField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) []DialogResultExpectation {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if results := scriptDialogResultListFromJSON(raw, nestedNames, 0); len(results) > 0 {
			return results
		}
	}
	return nil
}

func scriptDialogResultListFromJSON(raw json.RawMessage, names []string, depth int) []DialogResultExpectation {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil
		}
		out := []DialogResultExpectation{}
		for _, item := range items {
			out = append(out, scriptDialogResultListFromJSON(item, names, depth+1)...)
		}
		return out
	}
	if depth >= 8 || raw[0] != '{' {
		return nil
	}
	var result DialogResultExpectation
	if err := json.Unmarshal(raw, &result); err == nil && scriptDialogResultExpectationHasData(result) {
		return []DialogResultExpectation{result}
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	for _, name := range scriptRuntimePayloadWrapperNames(names...) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if results := scriptDialogResultListFromJSON(nested, names, depth+1); len(results) > 0 {
			return results
		}
	}
	return nil
}

func scriptDialogResultExpectationHasData(result DialogResultExpectation) bool {
	return result.ID != "" || result.Kind != "" || result.Action != "" || result.Status != "" || result.Found != nil || result.Stale != nil
}

func scriptNamedDialogExpectationField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) *DialogExpectation {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if dialog, ok := scriptDialogExpectationFromJSON(raw, nestedNames, 0); ok {
			return dialog
		}
	}
	return nil
}

func scriptDialogExpectationFromJSON(raw json.RawMessage, names []string, depth int) (*DialogExpectation, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if dialog, ok := scriptDialogExpectationFromJSON(item, names, depth+1); ok {
				return dialog, true
			}
		}
		return nil, false
	}
	if depth >= 8 || raw[0] != '{' {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	var dialog DialogExpectation
	if err := json.Unmarshal(raw, &dialog); err == nil && scriptDialogExpectationHasData(dialog, fields) {
		return &dialog, true
	}
	for _, name := range scriptRuntimePayloadWrapperNames(names...) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if dialog, ok := scriptDialogExpectationFromJSON(nested, names, depth+1); ok {
			return dialog, true
		}
	}
	return nil, false
}

func scriptDialogExpectationHasData(dialog DialogExpectation, fields map[string]json.RawMessage) bool {
	if dialog.Active ||
		dialog.ID != "" ||
		dialog.Kind != "" ||
		dialog.Title != "" ||
		dialog.Body != "" ||
		len(dialog.BodyContains) > 0 ||
		len(dialog.BodyNotContains) > 0 ||
		len(dialog.Actions) > 0 ||
		len(dialog.ActionContains) > 0 ||
		len(dialog.ActionNotContains) > 0 ||
		dialog.ActionCount != nil ||
		dialog.Focused != nil {
		return true
	}
	for _, name := range []string{
		"Active",
		"active",
		"IsActive",
		"is_active",
		"isActive",
		"Visible",
		"visible",
		"Exists",
		"exists",
		"Present",
		"present",
	} {
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}

func scriptNamedPromptExpectationField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) *PromptExpectation {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if prompt, ok := scriptPromptExpectationFromJSON(raw, nestedNames, 0); ok {
			return prompt
		}
	}
	return nil
}

func scriptPromptExpectationFromJSON(raw json.RawMessage, names []string, depth int) (*PromptExpectation, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if prompt, ok := scriptPromptExpectationFromJSON(item, names, depth+1); ok {
				return prompt, true
			}
		}
		return nil, false
	}
	if depth >= 8 || raw[0] != '{' {
		return nil, false
	}
	var prompt PromptExpectation
	if err := json.Unmarshal(raw, &prompt); err == nil && scriptPromptExpectationHasData(prompt) {
		return &prompt, true
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	for _, name := range scriptRuntimePayloadWrapperNames(names...) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if prompt, ok := scriptPromptExpectationFromJSON(nested, names, depth+1); ok {
			return prompt, true
		}
	}
	return nil, false
}

func scriptPromptExpectationHasData(prompt PromptExpectation) bool {
	return prompt.Text != "" ||
		prompt.Expanded != "" ||
		prompt.Cursor != nil ||
		prompt.Empty ||
		prompt.PastedContentCount != nil ||
		len(prompt.PastedContents) > 0 ||
		prompt.NextPastedID != nil
}

func scriptNamedVimExpectationField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) *VimExpectation {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if vim, ok := scriptVimExpectationFromJSON(raw, nestedNames, 0); ok {
			return vim
		}
	}
	return nil
}

func scriptVimExpectationFromJSON(raw json.RawMessage, names []string, depth int) (*VimExpectation, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if vim, ok := scriptVimExpectationFromJSON(item, names, depth+1); ok {
				return vim, true
			}
		}
		return nil, false
	}
	if depth >= 8 || raw[0] != '{' {
		return nil, false
	}
	var vim VimExpectation
	if err := json.Unmarshal(raw, &vim); err == nil && scriptVimExpectationHasData(vim) {
		return &vim, true
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	for _, name := range scriptRuntimePayloadWrapperNames(names...) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if vim, ok := scriptVimExpectationFromJSON(nested, names, depth+1); ok {
			return vim, true
		}
	}
	return nil, false
}

func scriptVimExpectationHasData(vim VimExpectation) bool {
	return vim.Enabled != nil ||
		vim.Mode != "" ||
		vim.Register != "" ||
		vim.RegisterLinewise != nil
}

func scriptNamedViewportExpectationField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) *ViewportExpectation {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if viewport, ok := scriptViewportExpectationFromJSON(raw, nestedNames, 0); ok {
			return viewport
		}
	}
	return nil
}

func scriptViewportExpectationFromJSON(raw json.RawMessage, names []string, depth int) (*ViewportExpectation, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if viewport, ok := scriptViewportExpectationFromJSON(item, names, depth+1); ok {
				return viewport, true
			}
		}
		return nil, false
	}
	if depth >= 8 || raw[0] != '{' {
		return nil, false
	}
	var viewport ViewportExpectation
	if err := json.Unmarshal(raw, &viewport); err == nil && scriptViewportExpectationHasData(viewport) {
		return &viewport, true
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	for _, name := range scriptRuntimePayloadWrapperNames(names...) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if viewport, ok := scriptViewportExpectationFromJSON(nested, names, depth+1); ok {
			return viewport, true
		}
	}
	return nil, false
}

func scriptViewportExpectationHasData(viewport ViewportExpectation) bool {
	return viewport.Offset != nil ||
		viewport.VisibleLineCount > 0 ||
		len(viewport.VisibleContains) > 0 ||
		len(viewport.VisibleNotContains) > 0
}

func scriptNamedScreenExpectationField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) *ScreenExpectation {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if screen, ok := scriptScreenExpectationFromJSON(raw, nestedNames, 0); ok {
			return screen
		}
	}
	return nil
}

func scriptScreenExpectationFromJSON(raw json.RawMessage, names []string, depth int) (*ScreenExpectation, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if screen, ok := scriptScreenExpectationFromJSON(item, names, depth+1); ok {
				return screen, true
			}
		}
		return nil, false
	}
	if depth >= 8 || raw[0] != '{' {
		return nil, false
	}
	var screen ScreenExpectation
	if err := json.Unmarshal(raw, &screen); err == nil && scriptScreenExpectationHasData(screen) {
		return &screen, true
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	for _, name := range scriptRuntimePayloadWrapperNames(names...) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if screen, ok := scriptScreenExpectationFromJSON(nested, names, depth+1); ok {
			return screen, true
		}
	}
	return nil, false
}

func scriptScreenExpectationHasData(screen ScreenExpectation) bool {
	return screen.Width > 0 || screen.Height > 0
}

func scriptNamedTasksExpectationField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) *TasksExpectation {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if tasks, ok := scriptTasksExpectationFromJSON(raw, nestedNames, 0); ok {
			return tasks
		}
	}
	return nil
}

func scriptTasksExpectationFromJSON(raw json.RawMessage, names []string, depth int) (*TasksExpectation, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if tasks, ok := scriptTasksExpectationFromJSON(item, names, depth+1); ok {
				return tasks, true
			}
		}
		return nil, false
	}
	if depth >= 8 || raw[0] != '{' {
		return nil, false
	}
	var tasks TasksExpectation
	if err := json.Unmarshal(raw, &tasks); err == nil && scriptTasksExpectationHasData(tasks) {
		return &tasks, true
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	for _, name := range scriptRuntimePayloadWrapperNames(names...) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if tasks, ok := scriptTasksExpectationFromJSON(nested, names, depth+1); ok {
			return tasks, true
		}
	}
	return nil, false
}

func scriptTasksExpectationHasData(tasks TasksExpectation) bool {
	return tasks.Count != nil ||
		len(tasks.StateCounts) > 0 ||
		len(tasks.Contains) > 0
}

func scriptNamedReverseSearchExpectationField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) *ReverseSearchExpectation {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if reverse, ok := scriptReverseSearchExpectationFromJSON(raw, nestedNames, 0); ok {
			return reverse
		}
	}
	return nil
}

func scriptReverseSearchExpectationFromJSON(raw json.RawMessage, names []string, depth int) (*ReverseSearchExpectation, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if reverse, ok := scriptReverseSearchExpectationFromJSON(item, names, depth+1); ok {
				return reverse, true
			}
		}
		return nil, false
	}
	if depth >= 8 || raw[0] != '{' {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	var reverse ReverseSearchExpectation
	if err := json.Unmarshal(raw, &reverse); err == nil && scriptReverseSearchExpectationHasData(reverse, fields) {
		return &reverse, true
	}
	for _, name := range scriptRuntimePayloadWrapperNames(names...) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if reverse, ok := scriptReverseSearchExpectationFromJSON(nested, names, depth+1); ok {
			return reverse, true
		}
	}
	return nil, false
}

func scriptReverseSearchExpectationHasData(reverse ReverseSearchExpectation, fields map[string]json.RawMessage) bool {
	if reverse.Active ||
		reverse.Query != "" ||
		reverse.Cursor != nil ||
		reverse.Current != "" ||
		reverse.ResultCount > 0 ||
		reverse.NoResults {
		return true
	}
	for _, name := range []string{
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
	} {
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}

func scriptActionIntFromJSON(raw json.RawMessage, names []string, depth int) (int, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}
	if value, ok := scriptIntFromJSON(raw); ok {
		return value, true
	}
	if depth >= 8 {
		return 0, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return 0, false
		}
		for _, item := range items {
			if value, ok := scriptActionIntFromJSON(item, names, depth+1); ok {
				return value, true
			}
		}
		return 0, false
	}
	if raw[0] != '{' {
		return 0, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return 0, false
	}
	if value := intPtrJSONField(fields, names...); value != nil {
		return *value, true
	}
	for _, name := range scriptRuntimePayloadWrapperNames(names...) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if value, ok := scriptActionIntFromJSON(nested, names, depth+1); ok {
			return value, true
		}
	}
	return 0, false
}

func scriptIntFromJSON(raw json.RawMessage) (int, bool) {
	var value int
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, true
	}
	var stringValue string
	if err := json.Unmarshal(raw, &stringValue); err == nil {
		if value, err := strconv.Atoi(strings.TrimSpace(stringValue)); err == nil {
			return value, true
		}
	}
	return 0, false
}

func scriptActionStringListField(fields map[string]json.RawMessage, names ...string) []string {
	for _, name := range append([]string{"value", "payload", "data", "body"}, names...) {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if values := scriptActionStringListFromJSON(raw, names, 0); len(values) > 0 {
			return values
		}
	}
	return nil
}

func scriptNamedStringListField(fields map[string]json.RawMessage, directNames []string, nestedNames ...string) []string {
	for _, name := range directNames {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if values := scriptActionStringListFromJSON(raw, nestedNames, 0); len(values) > 0 {
			return values
		}
	}
	return nil
}

func scriptActionStringListFromJSON(raw json.RawMessage, names []string, depth int) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var values stringList
	if err := json.Unmarshal(raw, &values); err == nil {
		return stringListValue(&values)
	}
	if depth >= 8 {
		return nil
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil
		}
		out := []string{}
		for _, item := range items {
			out = append(out, scriptActionStringListFromJSON(item, names, depth+1)...)
		}
		if len(out) > 0 {
			return out
		}
		return nil
	}
	if raw[0] != '{' {
		return nil
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	if key, ok := scriptKeyEventNameFromFields(fields); ok {
		if key == "" {
			return nil
		}
		return []string{key}
	}
	if values := stringListJSONField(fields, names...); len(values) > 0 {
		return values
	}
	for _, name := range scriptRuntimePayloadWrapperNames(names...) {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if values := scriptActionStringListFromJSON(nested, names, depth+1); len(values) > 0 {
			return values
		}
	}
	return nil
}

func scriptKeyEventNameFromFields(fields map[string]json.RawMessage) (string, bool) {
	if scriptKeyEventIsReplayNoop(fields) {
		return "", true
	}
	key := stringJSONField(fields,
		"key", "Key", "keyName", "key_name", "name", "value",
		"code", "Code", "keyCodeName", "key_code_name",
		"keyIdentifier", "key_identifier", "KeyIdentifier",
	)
	if scriptKeyEventNoopName(key) {
		return "", true
	}
	if scriptKeyEventUnknownName(key) {
		key = ""
	}
	if key == "" {
		var ok bool
		key, ok = scriptKeyEventNumericNameFromFields(fields)
		if !ok {
			return "", false
		}
	}
	base, modifierOnly := scriptKeyEventBaseName(key)
	if base == "" || modifierOnly {
		return "", true
	}
	modifiers := scriptKeyEventModifiers(fields)
	switch {
	case modifiers["ctrl"] && scriptKeyEventCanUseCtrl(base):
		return "ctrl-" + strings.ToLower(base), true
	case modifiers["alt"] && scriptKeyEventCanUseAlt(base):
		return "alt-" + strings.ToLower(base), true
	case modifiers["meta"] && scriptKeyEventCanUseAlt(base):
		return "meta-" + strings.ToLower(base), true
	case modifiers["shift"] && scriptKeyEventCanUseShift(base):
		return "shift-" + strings.ToLower(base), true
	case modifiers["shift"] && len([]rune(base)) == 1 && !modifiers["ctrl"] && !modifiers["alt"] && !modifiers["meta"]:
		return strings.ToUpper(base), true
	default:
		return base, true
	}
}

func scriptKeyEventUnknownName(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(key), "_", "-"), " ", "-"))
	switch normalized {
	case "", "unidentified", "unknown", "keydown", "key-down", "keyup", "key-up", "keypress", "key-press":
		return true
	default:
		return false
	}
}

func scriptKeyEventNumericNameFromFields(fields map[string]json.RawMessage) (string, bool) {
	if value := intPtrJSONField(fields, "charCode", "char_code", "charcode", "CharCode"); value != nil && *value > 0 {
		return scriptKeyEventCharCodeName(*value)
	}
	if value := intPtrJSONField(fields, "keyCode", "key_code", "keycode", "KeyCode"); value != nil && *value > 0 {
		return scriptKeyEventKeyCodeName(*value)
	}
	if value := intPtrJSONField(fields, "which", "Which"); value != nil && *value > 0 {
		if scriptKeyEventWhichUsesCharCode(fields) {
			return scriptKeyEventCharCodeName(*value)
		}
		return scriptKeyEventKeyCodeName(*value)
	}
	return "", false
}

func scriptKeyEventIsReplayNoop(fields map[string]json.RawMessage) bool {
	return scriptKeyEventIsRelease(fields) || scriptKeyEventIsComposing(fields) || scriptKeyEventIsCompositionEvent(fields)
}

func scriptKeyEventIsRelease(fields map[string]json.RawMessage) bool {
	for _, name := range []string{"type", "eventType", "event_type", "event", "kind", "action", "name"} {
		value := stringJSONField(fields, name)
		normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"), " ", "-"))
		switch normalized {
		case "keyup", "key-up", "keyrelease", "key-release", "keyreleased", "key-released", "keyboardup", "keyboard-up", "keyboardrelease", "keyboard-release", "release", "released":
			return true
		}
	}
	return false
}

func scriptKeyEventIsComposing(fields map[string]json.RawMessage) bool {
	for _, name := range []string{"isComposing", "is_composing", "composing", "composition", "imeComposing", "ime_composing"} {
		if value, ok := scriptBoolJSONField(fields, name); ok && value {
			return true
		}
	}
	return false
}

func scriptKeyEventIsCompositionEvent(fields map[string]json.RawMessage) bool {
	for _, name := range []string{"type", "eventType", "event_type", "event", "kind", "action", "name"} {
		value := stringJSONField(fields, name)
		normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"), " ", "-"))
		switch normalized {
		case "compositionstart", "composition-start", "compositionupdate", "composition-update", "compositionend", "composition-end":
			return true
		}
	}
	return false
}

func scriptKeyEventNoopName(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(key), "_", "-"), " ", "-"))
	switch normalized {
	case "dead", "process", "compose", "composition", "ime", "nonconvert", "non-convert", "convert", "modechange", "mode-change":
		return true
	default:
		return false
	}
}

func scriptKeyEventWhichUsesCharCode(fields map[string]json.RawMessage) bool {
	for _, name := range []string{"type", "eventType", "event_type", "event", "kind", "action", "name"} {
		value := stringJSONField(fields, name)
		normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"), " ", "-"))
		if normalized == "keypress" || normalized == "key-press" {
			return true
		}
	}
	return false
}

func scriptKeyEventCharCodeName(code int) (string, bool) {
	switch code {
	case 8:
		return "backspace", true
	case 9:
		return "tab", true
	case 10, 13:
		return "enter", true
	case 27:
		return "escape", true
	}
	if code == 32 {
		return "space", true
	}
	if code >= 32 && code <= 126 {
		return string(rune(code)), true
	}
	return "", false
}

func scriptKeyEventKeyCodeName(code int) (string, bool) {
	switch code {
	case 8:
		return "backspace", true
	case 9:
		return "tab", true
	case 13:
		return "enter", true
	case 16, 17, 18, 91, 92, 93, 224:
		return "", true
	case 27:
		return "escape", true
	case 32:
		return "space", true
	case 33:
		return "page-up", true
	case 34:
		return "page-down", true
	case 35:
		return "end", true
	case 36:
		return "home", true
	case 37:
		return "left", true
	case 38:
		return "up", true
	case 39:
		return "right", true
	case 40:
		return "down", true
	case 46:
		return "delete", true
	}
	switch {
	case code >= 65 && code <= 90:
		return string(rune('a' + code - 65)), true
	case code >= 48 && code <= 57:
		return string(rune('0' + code - 48)), true
	case code >= 96 && code <= 105:
		return string(rune('0' + code - 96)), true
	}
	switch code {
	case 59, 186:
		return ";", true
	case 61, 187:
		return "=", true
	case 106:
		return "*", true
	case 107:
		return "+", true
	case 109, 173, 189:
		return "-", true
	case 110:
		return ".", true
	case 111:
		return "/", true
	case 188:
		return ",", true
	case 190:
		return ".", true
	case 191:
		return "/", true
	case 192:
		return "`", true
	case 219:
		return "[", true
	case 220:
		return "\\", true
	case 221:
		return "]", true
	case 222:
		return "'", true
	}
	return "", false
}

func scriptKeyEventBaseName(key string) (string, bool) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", false
	}
	if base, modifierOnly, ok := scriptKeyIdentifierBaseName(trimmed); ok {
		return base, modifierOnly
	}
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(trimmed, "_", "-"), " ", "-"))
	switch normalized {
	case "control", "ctrl", "shift", "alt", "option", "meta", "command", "cmd", "super":
		return "", true
	case "escape", "esc":
		return "escape", false
	case "enter", "return", "numpadenter":
		return "enter", false
	case "tab":
		return "tab", false
	case "backspace", "backspace-key", "deletebackward", "delete-backward", "backwarddelete", "backward-delete":
		return "backspace", false
	case "delete", "del", "delete-key", "deleteforward", "delete-forward", "forwarddelete", "forward-delete":
		return "delete", false
	case "arrowleft", "arrow-left", "left":
		return "left", false
	case "arrowright", "arrow-right", "right":
		return "right", false
	case "arrowup", "arrow-up", "up":
		return "up", false
	case "arrowdown", "arrow-down", "down":
		return "down", false
	case "pageup", "page-up", "pgup", "pg-up", "prior", "pageupkey", "page-up-key", "pgupkey", "pg-up-key":
		return "page-up", false
	case "pagedown", "page-down", "pgdn", "pg-dn", "next", "pagedownkey", "page-down-key", "pgdnkey", "pg-dn-key":
		return "page-down", false
	case "home", "homekey", "home-key":
		return "home", false
	case "end", "endkey", "end-key":
		return "end", false
	case "space", "spacebar":
		return " ", false
	}
	if base, ok := scriptDOMCodeBaseName(normalized); ok {
		return base, false
	}
	if base, ok := scriptNumpadKeyBaseName(normalized); ok {
		return base, false
	}
	if strings.HasPrefix(normalized, "key") && len([]rune(normalized)) == 4 {
		return string([]rune(normalized)[3]), false
	}
	if strings.HasPrefix(normalized, "digit") && len([]rune(normalized)) == 6 {
		return string([]rune(normalized)[5]), false
	}
	if len([]rune(trimmed)) == 1 {
		return trimmed, false
	}
	return trimmed, false
}

func scriptDOMCodeBaseName(normalized string) (string, bool) {
	switch normalized {
	case "backquote":
		return "`", true
	case "minus":
		return "-", true
	case "equal":
		return "=", true
	case "bracketleft", "bracket-left":
		return "[", true
	case "bracketright", "bracket-right":
		return "]", true
	case "backslash", "intlbackslash", "intl-backslash":
		return "\\", true
	case "semicolon":
		return ";", true
	case "quote":
		return "'", true
	case "comma":
		return ",", true
	case "period":
		return ".", true
	case "slash":
		return "/", true
	default:
		return "", false
	}
}

func scriptNumpadKeyBaseName(normalized string) (string, bool) {
	suffix, ok := strings.CutPrefix(normalized, "numpad")
	if !ok {
		return "", false
	}
	suffix = strings.TrimPrefix(suffix, "-")
	switch suffix {
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return suffix, true
	case "decimal":
		return ".", true
	case "comma":
		return ",", true
	case "add", "plus":
		return "+", true
	case "subtract", "minus":
		return "-", true
	case "multiply", "asterisk", "star":
		return "*", true
	case "divide", "slash":
		return "/", true
	case "equal", "equals":
		return "=", true
	case "hash", "number", "pound":
		return "#", true
	case "parenleft", "paren-left", "leftparen", "left-paren", "openparen", "open-paren":
		return "(", true
	case "parenright", "paren-right", "rightparen", "right-paren", "closeparen", "close-paren":
		return ")", true
	case "backspace", "deletebackward", "delete-backward":
		return "backspace", true
	default:
		return "", false
	}
}

func scriptKeyIdentifierBaseName(trimmed string) (string, bool, bool) {
	upper := strings.ToUpper(strings.TrimSpace(trimmed))
	if !strings.HasPrefix(upper, "U+") || len(upper) <= 2 {
		return "", false, false
	}
	parsed, err := strconv.ParseInt(upper[2:], 16, 32)
	if err != nil {
		return "", false, false
	}
	code := int(parsed)
	switch code {
	case 8, 127:
		return "backspace", false, true
	case 9:
		return "tab", false, true
	case 10, 13:
		return "enter", false, true
	case 16, 17, 18, 91, 92, 93, 224:
		return "", true, true
	case 27:
		return "escape", false, true
	case 32:
		return " ", false, true
	}
	if code >= 32 && code <= 0x10ffff {
		return string(rune(code)), false, true
	}
	return "", false, false
}

func scriptKeyEventModifiers(fields map[string]json.RawMessage) map[string]bool {
	modifiers := map[string]bool{}
	for _, name := range []string{"ctrlKey", "controlKey", "ctrl", "control", "isCtrl", "isControl"} {
		if value, ok := scriptBoolJSONField(fields, name); ok && value {
			modifiers["ctrl"] = true
		}
	}
	for _, name := range []string{"altKey", "optionKey", "alt", "option", "isAlt", "isOption"} {
		if value, ok := scriptBoolJSONField(fields, name); ok && value {
			modifiers["alt"] = true
		}
	}
	for _, name := range []string{"metaKey", "cmdKey", "commandKey", "meta", "cmd", "command", "superKey"} {
		if value, ok := scriptBoolJSONField(fields, name); ok && value {
			modifiers["meta"] = true
		}
	}
	for _, name := range []string{"shiftKey", "shift", "isShift"} {
		if value, ok := scriptBoolJSONField(fields, name); ok && value {
			modifiers["shift"] = true
		}
	}
	for _, modifier := range stringListJSONField(fields, "modifiers", "modifier", "mods") {
		switch strings.ToLower(strings.TrimSpace(modifier)) {
		case "ctrl", "control":
			modifiers["ctrl"] = true
		case "alt", "option":
			modifiers["alt"] = true
		case "meta", "cmd", "command", "super":
			modifiers["meta"] = true
		case "shift":
			modifiers["shift"] = true
		}
	}
	return modifiers
}

func scriptBoolJSONField(fields map[string]json.RawMessage, name string) (bool, bool) {
	raw, ok := fields[name]
	if !ok {
		return false, false
	}
	return scriptParseJSONBool(raw)
}

func scriptKeyEventCanUseCtrl(base string) bool {
	normalized := strings.ToLower(base)
	if len([]rune(normalized)) == 1 {
		r := []rune(normalized)[0]
		return r >= 'a' && r <= 'z'
	}
	return normalized == "left" || normalized == "right"
}

func scriptKeyEventCanUseAlt(base string) bool {
	normalized := strings.ToLower(base)
	if len([]rune(normalized)) == 1 {
		r := []rune(normalized)[0]
		return r >= 'a' && r <= 'z'
	}
	return normalized == "left" || normalized == "right" || normalized == "backspace"
}

func scriptKeyEventCanUseShift(base string) bool {
	normalized := strings.ToLower(base)
	return normalized == "enter" || normalized == "tab"
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
	for _, name := range scriptRuntimePayloadWrapperNames("enabled", "active", "open", "visible", "focused", "focus", "blurred", "blur", "checked", "selected", "value") {
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
		if mouse, ok := scriptMouseFromJSON(raw, 0); ok {
			return mouse
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
		if image, ok := scriptImageFromJSON(raw, 0); ok {
			return image
		}
	}
	return nil
}

func scriptMouseFromJSON(raw json.RawMessage, depth int) (*ScriptMouse, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	var mouse ScriptMouse
	if err := json.Unmarshal(raw, &mouse); err == nil && scriptMouseHasData(mouse) {
		return &mouse, true
	}
	if depth >= 8 {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if mouse, ok := scriptMouseFromJSON(item, depth+1); ok {
				return mouse, true
			}
		}
		return nil, false
	}
	if raw[0] != '{' {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	for _, name := range scriptRuntimePayloadWrapperNames("mouse", "mouse_event", "mouseEvent", "event", "input") {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if mouse, ok := scriptMouseFromJSON(nested, depth+1); ok {
			return mouse, true
		}
	}
	return nil, false
}

func scriptImageFromJSON(raw json.RawMessage, depth int) (*ScriptImage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	var image ScriptImage
	if err := json.Unmarshal(raw, &image); err == nil && scriptImageHasData(image) {
		return &image, true
	}
	if depth >= 8 {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if image, ok := scriptImageFromJSON(item, depth+1); ok {
				return image, true
			}
		}
		return nil, false
	}
	if raw[0] != '{' {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	for _, name := range scriptRuntimePayloadWrapperNames("image", "paste_image", "pasteImage", "image_paste", "imagePaste", "attachment", "media") {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if image, ok := scriptImageFromJSON(nested, depth+1); ok {
			return image, true
		}
	}
	return nil, false
}

func scriptMouseHasData(mouse ScriptMouse) bool {
	return mouse.Button != 0 || mouse.X != 0 || mouse.Y != 0 || mouse.Release
}

func scriptImageHasData(image ScriptImage) bool {
	return image.Filename != "" || image.MediaType != "" || image.Content != "" || image.SourcePath != "" || image.Dimensions != nil
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
		"Keys",
		"keys",
		"presses",
		"key_presses",
		"keyPresses",
		"keypresses",
		"shortcuts",
		"ExpectStatusContains",
		"expect_status_contains",
		"expectStatusContains",
		"ExpectStatusNotContains",
		"expect_status_not_contains",
		"expectStatusNotContains",
		"ExpectSnapshotContains",
		"expect_snapshot_contains",
		"expectSnapshotContains",
		"ExpectSnapshotNotContains",
		"expect_snapshot_not_contains",
		"expectSnapshotNotContains",
	)
	data = normalizeBoolFields(data,
		"CancelActiveDialog",
		"cancel_active_dialog",
		"cancelActiveDialog",
		"CancelActive",
		"cancel_active",
		"cancelActive",
		"CancelDialog",
		"cancel_dialog",
		"cancelDialog",
		"CloseDialog",
		"close_dialog",
		"closeDialog",
		"CancelAllPermissions",
		"cancel_all_permissions",
		"cancelAllPermissions",
		"CancelPermissions",
		"cancel_permissions",
		"cancelPermissions",
		"CancelAllTasks",
		"cancel_all_tasks",
		"cancelAllTasks",
		"CancelTasks",
		"cancel_tasks",
		"cancelTasks",
		"OpenTasksDialog",
		"open_tasks_dialog",
		"openTasksDialog",
		"OpenTasks",
		"open_tasks",
		"openTasks",
		"ShowTasks",
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
		"ExpectNoEvent",
		"expect_no_event",
		"expectNoEvent",
		"ExpectNoDialogResult",
		"expect_no_dialog_result",
		"expectNoDialogResult",
		"ExpectNoDialogResults",
		"expect_no_dialog_results",
		"ExpectFocused",
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

func stripScriptStepRawScalarAliasFields(data []byte) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return data
	}
	changed := false
	for _, name := range []string{
		"Key",
		"key",
		"Keys",
		"keys",
		"Text",
		"text",
		"Paste",
		"paste",
		"Status",
		"status",
		"Message",
		"message",
		"Messages",
		"messages",
		"append_messages",
		"appendMessages",
		"transcript_messages",
		"transcriptMessages",
		"Mouse",
		"mouse",
		"mouse_event",
		"mouseEvent",
		"Dialog",
		"dialog",
		"Image",
		"image",
		"Keybindings",
		"keybindings",
		"KeyBindings",
		"key_bindings",
		"keyBindings",
		"KeybindingSpecs",
		"keybinding_specs",
		"keybindingSpecs",
		"SnapshotName",
		"snapshotName",
		"snapshot_name",
		"RequestPermission",
		"requestPermission",
		"request_permission",
		"UpsertTask",
		"upsertTask",
		"upsert_task",
		"RemoveTaskID",
		"removeTaskID",
		"removeTaskId",
		"remove_task_id",
		"RemoveTask",
		"removeTask",
		"remove_task",
		"DeleteTask",
		"deleteTask",
		"delete_task",
		"CancelActiveDialog",
		"cancelActiveDialog",
		"cancel_active_dialog",
		"CancelActive",
		"cancelActive",
		"cancel_active",
		"CancelDialog",
		"cancelDialog",
		"cancel_dialog",
		"CloseDialog",
		"closeDialog",
		"close_dialog",
		"CancelPermissionID",
		"cancelPermissionID",
		"cancelPermissionId",
		"cancel_permission_id",
		"CancelPermission",
		"cancelPermission",
		"cancel_permission",
		"CancelAllPermissions",
		"cancelAllPermissions",
		"cancel_all_permissions",
		"CancelPermissions",
		"cancelPermissions",
		"cancel_permissions",
		"CancelAllTasks",
		"cancelAllTasks",
		"cancel_all_tasks",
		"CancelTasks",
		"cancelTasks",
		"cancel_tasks",
		"CancelTasksDetail",
		"cancelTasksDetail",
		"cancel_tasks_detail",
		"CancelReason",
		"cancelReason",
		"cancel_reason",
		"OpenTasksDialog",
		"openTasksDialog",
		"open_tasks_dialog",
		"OpenTasks",
		"openTasks",
		"open_tasks",
		"ShowTasks",
		"showTasks",
		"show_tasks",
		"ResizeWidth",
		"resizeWidth",
		"resize_width",
		"ResizeHeight",
		"resizeHeight",
		"resize_height",
		"ExpectNoEvent",
		"expectNoEvent",
		"expect_no_event",
		"ExpectEventCount",
		"expectEventCount",
		"expect_event_count",
		"ExpectTotalEventCount",
		"expectTotalEventCount",
		"expect_total_event_count",
		"ExpectEvent",
		"expectEvent",
		"expect_event",
		"ExpectDialogResult",
		"expectDialogResult",
		"expect_dialog_result",
		"ExpectDialogResults",
		"expectDialogResults",
		"expect_dialog_results",
		"ExpectNoDialogResult",
		"expectNoDialogResult",
		"expect_no_dialog_result",
		"ExpectNoDialogResults",
		"expectNoDialogResults",
		"expect_no_dialog_results",
		"ExpectDialogResultCount",
		"expectDialogResultCount",
		"expect_dialog_result_count",
		"ExpectTotalDialogResultCount",
		"expectTotalDialogResultCount",
		"expect_total_dialog_result_count",
		"ExpectFocused",
		"expectFocused",
		"expect_focused",
		"ExpectDialog",
		"expectDialog",
		"expect_dialog",
		"ExpectPrompt",
		"expectPrompt",
		"expect_prompt",
		"ExpectVim",
		"expectVim",
		"expect_vim",
		"ExpectTasks",
		"expectTasks",
		"expect_tasks",
		"ExpectReverseSearch",
		"expectReverseSearch",
		"expect_reverse_search",
		"ExpectViewport",
		"expectViewport",
		"expect_viewport",
		"ExpectScreen",
		"expectScreen",
		"expect_screen",
		"ExpectStatusContains",
		"expectStatusContains",
		"expect_status_contains",
		"ExpectStatusNotContains",
		"expectStatusNotContains",
		"expect_status_not_contains",
		"ExpectSnapshotContains",
		"expectSnapshotContains",
		"expect_snapshot_contains",
		"ExpectSnapshotNotContains",
		"expectSnapshotNotContains",
		"expect_snapshot_not_contains",
	} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		if raw[0] != '{' && raw[0] != '[' {
			continue
		}
		delete(fields, name)
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

func stripJSONAliasFields(data []byte, names ...string) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return data
	}
	changed := false
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			continue
		}
		delete(fields, name)
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
		"Released",
		"released",
		"IsRelease",
		"is_release",
		"isRelease",
		"MouseRelease",
		"mouse_release",
		"mouseRelease",
		"MouseUp",
		"mouse_up",
		"mouseUp",
		"Up",
		"up",
		"ReleaseEvent",
		"release_event",
		"releaseEvent",
		"ReleasedEvent",
		"released_event",
		"releasedEvent",
	)
	type alias ScriptMouse
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"Button",
		"button",
		"ButtonCode",
		"button_code",
		"buttonCode",
		"ButtonMask",
		"button_mask",
		"buttonMask",
		"MouseButton",
		"mouse_button",
		"mouseButton",
		"Btn",
		"btn",
		"Code",
		"code",
		"Mask",
		"mask",
		"X",
		"x",
		"Column",
		"column",
		"Col",
		"col",
		"MouseX",
		"mouse_x",
		"mouseX",
		"ClientX",
		"client_x",
		"clientX",
		"ScreenX",
		"screen_x",
		"screenX",
		"PageX",
		"page_x",
		"pageX",
		"OffsetX",
		"offset_x",
		"offsetX",
		"ViewportX",
		"viewport_x",
		"viewportX",
		"Y",
		"y",
		"Row",
		"row",
		"Line",
		"line",
		"MouseY",
		"mouse_y",
		"mouseY",
		"ClientY",
		"client_y",
		"clientY",
		"ScreenY",
		"screen_y",
		"screenY",
		"PageY",
		"page_y",
		"pageY",
		"OffsetY",
		"offset_y",
		"offsetY",
		"ViewportY",
		"viewport_y",
		"viewportY",
		"Release",
		"release",
		"Released",
		"released",
		"IsRelease",
		"is_release",
		"isRelease",
		"MouseRelease",
		"mouse_release",
		"mouseRelease",
		"MouseUp",
		"mouse_up",
		"mouseUp",
		"Up",
		"up",
		"ReleaseEvent",
		"release_event",
		"releaseEvent",
		"ReleasedEvent",
		"released_event",
		"releasedEvent",
	), &raw); err != nil {
		return err
	}
	*mouse = ScriptMouse(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if button := intPtrJSONField(fields, "ButtonCode", "button_code", "buttonCode", "ButtonMask", "button_mask", "buttonMask", "MouseButton", "mouse_button", "mouseButton"); button != nil {
		mouse.Button = *button
	} else if button, ok := scriptMouseWheelButtonFromFields(fields); ok {
		mouse.Button = button
	} else if button, ok := scriptMouseDOMButtonFromFields(fields); ok {
		mouse.Button = button
	} else if button := intPtrJSONField(fields, "Button", "button", "Btn", "btn", "Code", "code", "Mask", "mask"); button != nil {
		mouse.Button = *button
	}
	touchPoint := scriptMouseTouchPointFields(fields)
	if x := scriptMouseXJSONField(fields); x != nil {
		mouse.X = *x
	} else if touchPoint != nil {
		if x := scriptMouseXJSONField(touchPoint); x != nil {
			mouse.X = *x
		}
	}
	if y := scriptMouseYJSONField(fields); y != nil {
		mouse.Y = *y
	} else if touchPoint != nil {
		if y := scriptMouseYJSONField(touchPoint); y != nil {
			mouse.Y = *y
		}
	}
	if release := boolPtrJSONField(fields, "Release", "release", "Released", "released", "IsRelease", "is_release", "isRelease", "MouseRelease", "mouse_release", "mouseRelease", "MouseUp", "mouse_up", "mouseUp", "Up", "up", "ReleaseEvent", "release_event", "releaseEvent", "ReleasedEvent", "released_event", "releasedEvent"); release != nil {
		mouse.Release = *release
	} else if release, ok := scriptMouseReleaseFromType(fields); ok {
		mouse.Release = release
	}
	return nil
}

func scriptMouseReleaseFromType(fields map[string]json.RawMessage) (bool, bool) {
	switch scriptMouseEventName(fields) {
	case "mouseup", "mouse-up", "pointerup", "pointer-up", "touchend", "touch-end", "touchcancel", "touch-cancel", "release", "released", "buttonup", "button-up":
		return true, true
	case "mousedown", "mouse-down", "pointerdown", "pointer-down", "touchstart", "touch-start", "click", "mousemove", "mouse-move", "pointermove", "pointer-move", "touchmove", "touch-move", "drag", "dragstart", "drag-start", "dragmove", "drag-move":
		return false, true
	default:
		return false, false
	}
}

func scriptMouseWheelButtonFromFields(fields map[string]json.RawMessage) (int, bool) {
	switch scriptMouseEventName(fields) {
	case "wheelup", "wheel-up", "scrollup", "scroll-up", "mousewheelup", "mouse-wheel-up":
		return sgrMouseWheelMask, true
	case "wheeldown", "wheel-down", "scrolldown", "scroll-down", "mousewheeldown", "mouse-wheel-down":
		return sgrMouseWheelMask | 1, true
	}
	switch scriptMouseWheelDirection(fields) {
	case -1:
		return sgrMouseWheelMask, true
	case 1:
		return sgrMouseWheelMask | 1, true
	default:
		return 0, false
	}
}

func scriptMouseDOMButtonFromFields(fields map[string]json.RawMessage) (int, bool) {
	motion := scriptMouseIsMotionEvent(fields)
	drag := scriptMouseIsDragEvent(fields)
	if which := intPtrJSONField(fields, "Which", "which"); which != nil {
		if button, ok := scriptMouseDOMWhichButton(*which); ok {
			if motion || drag {
				return button | 32, true
			}
			return button, true
		}
	}
	if buttons := intPtrJSONField(fields, "Buttons", "buttons", "ButtonState", "button_state", "buttonState", "ButtonsMask", "buttons_mask", "buttonsMask"); buttons != nil {
		if button, ok := scriptMouseDOMButtonsButton(*buttons); ok {
			if motion || drag {
				return button | 32, true
			}
			return button, true
		}
		if motion {
			return 35, true
		}
	}
	if drag {
		if button := intPtrJSONField(fields, "Button", "button", "Btn", "btn"); button != nil {
			if *button >= 0 && *button <= 2 {
				return *button | 32, true
			}
		}
		return 32, true
	}
	if motion {
		return 35, true
	}
	return 0, false
}

func scriptMouseDOMWhichButton(value int) (int, bool) {
	switch value {
	case 1:
		return 0, true
	case 2:
		return 1, true
	case 3:
		return 2, true
	default:
		return 0, false
	}
}

func scriptMouseDOMButtonsButton(value int) (int, bool) {
	switch {
	case value&1 != 0:
		return 0, true
	case value&4 != 0:
		return 1, true
	case value&2 != 0:
		return 2, true
	default:
		return 0, false
	}
}

func scriptMouseIsMotionEvent(fields map[string]json.RawMessage) bool {
	switch scriptMouseEventName(fields) {
	case "mousemove", "mouse-move", "pointermove", "pointer-move":
		return true
	default:
		return false
	}
}

func scriptMouseIsDragEvent(fields map[string]json.RawMessage) bool {
	switch scriptMouseEventName(fields) {
	case "touchmove", "touch-move", "drag", "dragstart", "drag-start", "dragmove", "drag-move":
		return true
	default:
		return false
	}
}

func scriptMouseXJSONField(fields map[string]json.RawMessage) *int {
	return intPtrJSONField(fields, "X", "x", "Column", "column", "Col", "col", "MouseX", "mouse_x", "mouseX", "ClientX", "client_x", "clientX", "ScreenX", "screen_x", "screenX", "PageX", "page_x", "pageX", "OffsetX", "offset_x", "offsetX", "ViewportX", "viewport_x", "viewportX")
}

func scriptMouseYJSONField(fields map[string]json.RawMessage) *int {
	return intPtrJSONField(fields, "Y", "y", "Row", "row", "Line", "line", "MouseY", "mouse_y", "mouseY", "ClientY", "client_y", "clientY", "ScreenY", "screen_y", "screenY", "PageY", "page_y", "pageY", "OffsetY", "offset_y", "offsetY", "ViewportY", "viewport_y", "viewportY")
}

func scriptMouseTouchPointFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	eventName := scriptMouseEventName(fields)
	names := []string{"Touches", "touches", "TargetTouches", "target_touches", "targetTouches", "ChangedTouches", "changed_touches", "changedTouches", "Touch", "touch", "Point", "point", "Contact", "contact"}
	if eventName == "touchend" || eventName == "touch-end" || eventName == "touchcancel" || eventName == "touch-cancel" {
		names = []string{"ChangedTouches", "changed_touches", "changedTouches", "Touches", "touches", "TargetTouches", "target_touches", "targetTouches", "Touch", "touch", "Point", "point", "Contact", "contact"}
	}
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if point, ok := scriptMousePointFieldsFromJSON(raw, 0); ok {
			return point
		}
	}
	return nil
}

func scriptMousePointFieldsFromJSON(raw json.RawMessage, depth int) (map[string]json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || depth >= 8 {
		return nil, false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false
		}
		for _, item := range items {
			if point, ok := scriptMousePointFieldsFromJSON(item, depth+1); ok {
				return point, true
			}
		}
		return nil, false
	}
	if raw[0] != '{' {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	if scriptMouseXJSONField(fields) != nil || scriptMouseYJSONField(fields) != nil {
		return fields, true
	}
	for _, name := range []string{"value", "payload", "data", "body", "resource", "attributes", "properties", "attrs", "edge", "node", "touch", "point", "contact"} {
		nested, ok := fields[name]
		if !ok {
			continue
		}
		if point, ok := scriptMousePointFieldsFromJSON(nested, depth+1); ok {
			return point, true
		}
	}
	return nil, false
}

func scriptMouseWheelDirection(fields map[string]json.RawMessage) int {
	direction := stringJSONField(fields, "Direction", "direction", "WheelDirection", "wheel_direction", "wheelDirection", "ScrollDirection", "scroll_direction", "scrollDirection")
	switch scriptNormalizeName(direction) {
	case "up", "wheelup", "wheel-up", "scrollup", "scroll-up", "north", "negative":
		return -1
	case "down", "wheeldown", "wheel-down", "scrolldown", "scroll-down", "south", "positive":
		return 1
	}
	if value, ok := numberJSONField(fields, "DeltaY", "delta_y", "deltaY", "ScrollDeltaY", "scroll_delta_y", "scrollDeltaY", "YDelta", "y_delta", "yDelta", "DY", "dy", "Detail", "detail", "Delta", "delta"); ok {
		return scriptMouseWheelDirectionFromDelta(value)
	}
	if value, ok := numberJSONField(fields, "WheelDeltaY", "wheel_delta_y", "wheelDeltaY", "WheelDelta", "wheel_delta", "wheelDelta"); ok {
		return scriptMouseWheelDirectionFromDelta(-value)
	}
	return 0
}

func scriptMouseWheelDirectionFromDelta(value float64) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}

func scriptMouseEventName(fields map[string]json.RawMessage) string {
	return scriptNormalizeName(stringJSONField(fields, "Type", "type", "Event", "event", "EventType", "event_type", "eventType", "Name", "name", "Kind", "kind", "Action", "action"))
}

func scriptNormalizeName(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"), " ", "-"))
}

func (event *ScreenEvent) UnmarshalJSON(data []byte) error {
	type alias ScreenEvent
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"Type",
		"type",
		"EventType",
		"event_type",
		"eventType",
		"Event",
		"event",
		"Name",
		"name",
		"EventName",
		"event_name",
		"eventName",
		"Value",
		"value",
		"EventValue",
		"event_value",
		"eventValue",
		"Payload",
		"payload",
		"EventPayload",
		"event_payload",
		"eventPayload",
		"ActionValue",
		"action_value",
		"actionValue",
		"Result",
		"result",
		"Selection",
		"selection",
		"Input",
		"input",
		"Prompt",
		"prompt",
		"Text",
		"text",
		"Message",
		"message",
		"Data",
		"data",
		"Display",
		"display",
		"ID",
		"id",
		"DialogID",
		"dialog_id",
		"dialogId",
		"dialogID",
		"PermissionID",
		"permission_id",
		"permissionId",
		"permissionID",
		"RequestID",
		"request_id",
		"requestId",
		"requestID",
		"ToolUseID",
		"tool_use_id",
		"toolUseId",
		"toolUseID",
		"OperationID",
		"operation_id",
		"operationId",
		"operationID",
		"Kind",
		"kind",
		"DialogKind",
		"dialog_kind",
		"dialogKind",
	), &raw); err != nil {
		return err
	}
	*event = ScreenEvent(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if event.Type == "" {
		if eventType := stringJSONField(fields, "Type", "type", "EventType", "event_type", "eventType", "Event", "event", "Name", "name", "EventName", "event_name", "eventName"); eventType != "" {
			event.Type = normalizeScreenEventType(eventType)
		}
	}
	if event.Type != "" {
		event.Type = normalizeScreenEventType(string(event.Type))
	}
	if event.Value == "" {
		event.Value = scalarStringJSONField(fields,
			"Value", "value",
			"EventValue", "event_value", "eventValue",
			"Payload", "payload", "EventPayload", "event_payload", "eventPayload",
			"ActionValue", "action_value", "actionValue",
			"Result", "result",
			"Selection", "selection",
			"Input", "input",
			"Prompt", "prompt",
			"Text", "text",
			"Message", "message",
			"Data", "data",
		)
	}
	if event.Display == "" {
		event.Display = scalarStringJSONField(fields, "Display", "display")
	}
	if event.DialogID == "" {
		event.DialogID = scalarStringJSONField(fields, "ID", "id", "DialogID", "dialog_id", "dialogId", "dialogID", "PermissionID", "permission_id", "permissionId", "permissionID", "RequestID", "request_id", "requestId", "requestID", "ToolUseID", "tool_use_id", "toolUseId", "toolUseID", "OperationID", "operation_id", "operationId", "operationID")
	}
	if event.DialogKind == "" {
		if dialogKind := stringJSONField(fields, "Kind", "kind", "DialogKind", "dialog_kind", "dialogKind"); dialogKind != "" {
			event.DialogKind = DialogKind(dialogKind)
		}
	}
	return nil
}

func normalizeScreenEventType(raw string) ScreenEventType {
	name := normalizeActionName(raw)
	switch name {
	case "", "none", "no_event", "noop", "no_op":
		return ScreenEventNone
	case "prompt_submitted", "prompt_submit", "submit_prompt", "submit", "submitted", "message_submitted", "input_submitted":
		return ScreenEventPromptSubmitted
	case "dialog_action", "dialog_action_selected", "dialog_selected", "dialog_submit", "dialog_button", "dialog_button_clicked", "action_selected", "action_clicked":
		return ScreenEventDialogAction
	case "cancelled", "canceled", "cancel", "prompt_cancelled", "prompt_canceled", "dismissed", "escape":
		return ScreenEventCancelled
	case "interrupted", "interrupt", "stop", "stopped", "stop_generation", "cancel_generation":
		return ScreenEventInterrupted
	case "exit_pending", "pending_exit", "confirm_exit":
		return ScreenEventExitPending
	case "exit", "quit", "closed":
		return ScreenEventExit
	case "redraw", "clear", "clear_screen", "refresh", "refresh_screen":
		return ScreenEventRedraw
	case "toggle_transcript", "transcript_toggled", "show_transcript":
		return ScreenEventToggleTranscript
	case "toggle_todos", "toggle_todo", "toggle_tasks", "tasks_toggled":
		return ScreenEventToggleTodos
	case "external_editor", "open_editor", "editor_opened":
		return ScreenEventExternalEditor
	case "stash_prompt", "prompt_stashed", "stash":
		return ScreenEventStashPrompt
	case "kill_agents", "agents_killed", "cancel_agents":
		return ScreenEventKillAgents
	case "reverse_search", "history_search", "search_history":
		return ScreenEventReverseSearch
	case "reverse_search_selected", "history_search_selected", "search_selected":
		return ScreenEventReverseSelected
	case "focus_in", "focus", "focused":
		return ScreenEventFocusIn
	case "focus_out", "blur", "blurred":
		return ScreenEventFocusOut
	case "viewport_selected", "viewport_selection", "selection", "selected":
		return ScreenEventViewportSelected
	default:
		return ScreenEventType(raw)
	}
}

func (expect *DialogResultExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeBoolFields(data,
		"Found",
		"found",
		"Exists",
		"exists",
		"Matched",
		"matched",
		"Stale",
		"stale",
		"IsStale",
		"is_stale",
		"isStale",
	)
	type alias DialogResultExpectation
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"ID",
		"id",
		"DialogID",
		"dialog_id",
		"dialogId",
		"dialogID",
		"PermissionID",
		"permission_id",
		"permissionId",
		"permissionID",
		"RequestID",
		"request_id",
		"requestId",
		"requestID",
		"ToolUseID",
		"tool_use_id",
		"toolUseId",
		"toolUseID",
		"OperationID",
		"operation_id",
		"operationId",
		"operationID",
		"Kind",
		"kind",
		"DialogKind",
		"dialog_kind",
		"dialogKind",
		"Action",
		"action",
		"Value",
		"value",
		"ActionValue",
		"action_value",
		"actionValue",
		"SelectedAction",
		"selected_action",
		"selectedAction",
		"Status",
		"status",
		"ResultStatus",
		"result_status",
		"resultStatus",
		"State",
		"state",
		"Found",
		"found",
		"Exists",
		"exists",
		"Matched",
		"matched",
		"Stale",
		"stale",
		"IsStale",
		"is_stale",
		"isStale",
	), &raw); err != nil {
		return err
	}
	*expect = DialogResultExpectation(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if expect.ID == "" {
		expect.ID = scalarStringJSONField(fields, "ID", "id", "DialogID", "dialog_id", "dialogId", "dialogID", "PermissionID", "permission_id", "permissionId", "permissionID", "RequestID", "request_id", "requestId", "requestID", "ToolUseID", "tool_use_id", "toolUseId", "toolUseID", "OperationID", "operation_id", "operationId", "operationID")
	}
	if expect.Kind == "" {
		if dialogKind := stringJSONField(fields, "Kind", "kind", "DialogKind", "dialog_kind", "dialogKind"); dialogKind != "" {
			expect.Kind = DialogKind(dialogKind)
		}
	}
	if expect.Action == "" {
		expect.Action = stringJSONField(fields, "Action", "action", "Value", "value", "ActionValue", "action_value", "actionValue", "SelectedAction", "selected_action", "selectedAction")
	}
	if expect.Status == "" {
		if status := stringJSONField(fields, "Status", "status", "ResultStatus", "result_status", "resultStatus", "State", "state"); status != "" {
			expect.Status = DialogResultStatus(status)
		}
	}
	if expect.Found == nil {
		expect.Found = boolPtrJSONField(fields, "Found", "found", "Exists", "exists", "Matched", "matched")
	}
	if expect.Stale == nil {
		expect.Stale = boolPtrJSONField(fields, "Stale", "stale", "IsStale", "is_stale", "isStale")
	}
	return nil
}

func (expect *DialogExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeBoolFields(data,
		"Active",
		"active",
		"IsActive",
		"is_active",
		"isActive",
		"Visible",
		"visible",
		"Exists",
		"exists",
		"Present",
		"present",
	)
	data = normalizeStringFieldsToArray(data,
		"actions",
		"Actions",
		"Options",
		"options",
		"Choices",
		"choices",
		"Buttons",
		"buttons",
		"BodyContains",
		"body_contains",
		"bodyContains",
		"BodyNotContains",
		"body_not_contains",
		"bodyNotContains",
		"ActionContains",
		"action_contains",
		"actionContains",
		"ActionNotContains",
		"action_not_contains",
		"actionNotContains",
	)
	type alias DialogExpectation
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"Active",
		"active",
		"IsActive",
		"is_active",
		"isActive",
		"Visible",
		"visible",
		"Exists",
		"exists",
		"Present",
		"present",
		"ID",
		"id",
		"DialogID",
		"dialog_id",
		"dialogId",
		"dialogID",
		"PermissionID",
		"permission_id",
		"permissionId",
		"permissionID",
		"RequestID",
		"request_id",
		"requestId",
		"requestID",
		"ToolUseID",
		"tool_use_id",
		"toolUseId",
		"toolUseID",
		"OperationID",
		"operation_id",
		"operationId",
		"operationID",
		"Kind",
		"kind",
		"DialogKind",
		"dialog_kind",
		"dialogKind",
		"Title",
		"title",
		"Heading",
		"heading",
		"Header",
		"header",
		"Label",
		"label",
		"Name",
		"name",
		"Body",
		"body",
		"Content",
		"content",
		"Text",
		"text",
		"Message",
		"message",
		"Description",
		"description",
		"BodyContains",
		"body_contains",
		"bodyContains",
		"BodyNotContains",
		"body_not_contains",
		"bodyNotContains",
		"Actions",
		"actions",
		"Options",
		"options",
		"Choices",
		"choices",
		"Buttons",
		"buttons",
		"ActionContains",
		"action_contains",
		"actionContains",
		"ActionNotContains",
		"action_not_contains",
		"actionNotContains",
		"ActionCount",
		"action_count",
		"actionCount",
		"ActionsCount",
		"actions_count",
		"actionsCount",
		"Focused",
		"focused",
		"FocusedIndex",
		"focused_index",
		"focusedIndex",
	), &raw); err != nil {
		return err
	}
	*expect = DialogExpectation(raw)

	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if active := boolPtrJSONField(fieldMap, "Active", "active", "IsActive", "is_active", "isActive", "Visible", "visible", "Exists", "exists", "Present", "present"); active != nil {
		expect.Active = *active
	}
	if expect.ID == "" {
		expect.ID = scalarStringJSONField(fieldMap, "ID", "id", "DialogID", "dialog_id", "dialogId", "dialogID", "PermissionID", "permission_id", "permissionId", "permissionID", "RequestID", "request_id", "requestId", "requestID", "ToolUseID", "tool_use_id", "toolUseId", "toolUseID", "OperationID", "operation_id", "operationId", "operationID")
	}
	if expect.Kind == "" {
		if dialogKind := stringJSONField(fieldMap, "Kind", "kind", "DialogKind", "dialog_kind", "dialogKind"); dialogKind != "" {
			expect.Kind = DialogKind(dialogKind)
		}
	}
	if expect.Title == "" {
		expect.Title = stringJSONField(fieldMap, "Title", "title", "Heading", "heading", "Header", "header", "Label", "label", "Name", "name")
	}
	if expect.Body == "" {
		expect.Body = stringJSONField(fieldMap, "Body", "body", "Content", "content", "Text", "text", "Message", "message", "Description", "description")
	}
	if values := stringListJSONField(fieldMap, "BodyContains", "body_contains", "bodyContains"); values != nil {
		expect.BodyContains = values
	}
	if values := stringListJSONField(fieldMap, "BodyNotContains", "body_not_contains", "bodyNotContains"); values != nil {
		expect.BodyNotContains = values
	}
	if values := stringListJSONField(fieldMap, "Actions", "actions", "Options", "options", "Choices", "choices", "Buttons", "buttons"); values != nil {
		expect.Actions = values
	}
	if values := stringListJSONField(fieldMap, "ActionContains", "action_contains", "actionContains"); values != nil {
		expect.ActionContains = values
	}
	if values := stringListJSONField(fieldMap, "ActionNotContains", "action_not_contains", "actionNotContains"); values != nil {
		expect.ActionNotContains = values
	}
	if expect.ActionCount == nil {
		expect.ActionCount = intPtrJSONField(fieldMap, "ActionCount", "action_count", "actionCount", "ActionsCount", "actions_count", "actionsCount")
	}
	if expect.Focused == nil {
		expect.Focused = intPtrJSONField(fieldMap, "Focused", "focused", "FocusedIndex", "focused_index", "focusedIndex")
	}
	return nil
}

func (dialog *Dialog) UnmarshalJSON(data []byte) error {
	data = normalizeStringFieldsToArray(data,
		"Actions",
		"actions",
		"Options",
		"options",
		"Choices",
		"choices",
		"Buttons",
		"buttons",
	)
	type alias Dialog
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"ID",
		"id",
		"DialogID",
		"dialog_id",
		"dialogId",
		"dialogID",
		"PermissionID",
		"permission_id",
		"permissionId",
		"permissionID",
		"RequestID",
		"request_id",
		"requestId",
		"requestID",
		"ToolUseID",
		"tool_use_id",
		"toolUseId",
		"toolUseID",
		"OperationID",
		"operation_id",
		"operationId",
		"operationID",
		"Kind",
		"kind",
		"DialogKind",
		"dialog_kind",
		"dialogKind",
		"DialogType",
		"dialog_type",
		"dialogType",
		"Title",
		"title",
		"Heading",
		"heading",
		"Header",
		"header",
		"Label",
		"label",
		"Name",
		"name",
		"Body",
		"body",
		"Content",
		"content",
		"Text",
		"text",
		"Message",
		"message",
		"Description",
		"description",
		"Actions",
		"actions",
		"Options",
		"options",
		"Choices",
		"choices",
		"Buttons",
		"buttons",
		"Focused",
		"focused",
		"FocusedIndex",
		"focused_index",
		"focusedIndex",
		"FocusIndex",
		"focus_index",
		"focusIndex",
		"SelectedIndex",
		"selected_index",
		"selectedIndex",
		"Focus",
		"focus",
		"Selected",
		"selected",
	), &raw); err != nil {
		return err
	}
	*dialog = Dialog(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if dialog.ID == "" {
		dialog.ID = scalarStringJSONField(fields, "ID", "id", "DialogID", "dialog_id", "dialogId", "dialogID", "PermissionID", "permission_id", "permissionId", "permissionID", "RequestID", "request_id", "requestId", "requestID", "ToolUseID", "tool_use_id", "toolUseId", "toolUseID", "OperationID", "operation_id", "operationId", "operationID")
	}
	if dialog.Kind == "" {
		if dialogKind := stringJSONField(fields, "Kind", "kind", "DialogKind", "dialog_kind", "dialogKind", "DialogType", "dialog_type", "dialogType"); dialogKind != "" {
			dialog.Kind = DialogKind(dialogKind)
		}
	}
	if dialog.Title == "" {
		dialog.Title = stringJSONField(fields, "Title", "title", "Heading", "heading", "Header", "header", "Label", "label", "Name", "name")
	}
	if dialog.Body == "" {
		dialog.Body = stringJSONField(fields, "Body", "body", "Content", "content", "Text", "text", "Message", "message", "Description", "description")
	}
	if len(dialog.Actions) == 0 {
		dialog.Actions = stringListJSONField(fields, "Actions", "actions", "Options", "options", "Choices", "choices", "Buttons", "buttons")
	}
	if focused := intPtrJSONField(fields, "Focused", "focused", "FocusedIndex", "focused_index", "focusedIndex", "FocusIndex", "focus_index", "focusIndex", "SelectedIndex", "selected_index", "selectedIndex", "Focus", "focus", "Selected", "selected"); focused != nil {
		dialog.Focused = *focused
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
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"ID",
		"id",
		"RequestID",
		"request_id",
		"requestId",
		"requestID",
		"PermissionID",
		"permission_id",
		"permissionId",
		"permissionID",
		"ToolUseID",
		"tool_use_id",
		"toolUseId",
		"toolUseID",
		"OperationID",
		"operation_id",
		"operationId",
		"operationID",
		"ToolName",
		"tool_name",
		"toolName",
		"Tool",
		"tool",
		"Name",
		"name",
		"Operation",
		"operation",
		"Command",
		"command",
		"CommandName",
		"command_name",
		"commandName",
		"ToolTitle",
		"tool_title",
		"toolTitle",
		"Path",
		"path",
		"FilePath",
		"file_path",
		"filePath",
		"TargetPath",
		"target_path",
		"targetPath",
		"ResourcePath",
		"resource_path",
		"resourcePath",
		"WorkingDirectory",
		"working_directory",
		"workingDirectory",
		"Cwd",
		"cwd",
		"Target",
		"target",
		"Resource",
		"resource",
		"URI",
		"uri",
		"URL",
		"url",
		"File",
		"file",
		"Filename",
		"filename",
		"Description",
		"description",
		"Prompt",
		"prompt",
		"Message",
		"message",
		"Reason",
		"reason",
		"ReasonText",
		"reason_text",
		"reasonText",
		"Summary",
		"summary",
		"Details",
		"details",
		"Body",
		"body",
		"Text",
		"text",
		"Content",
		"content",
		"Actions",
		"actions",
		"Options",
		"options",
		"Choices",
		"choices",
		"AllowedActions",
		"allowed_actions",
		"allowedActions",
		"AvailableActions",
		"available_actions",
		"availableActions",
		"ActionChoices",
		"action_choices",
		"actionChoices",
		"Buttons",
		"buttons",
		"ActionsList",
		"actions_list",
		"actionsList",
	), &raw); err != nil {
		return err
	}
	*request = PermissionRequest(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if request.ID == "" {
		request.ID = scalarStringJSONField(fields, "ID", "id", "RequestID", "request_id", "requestId", "requestID", "PermissionID", "permission_id", "permissionId", "permissionID", "ToolUseID", "tool_use_id", "toolUseId", "toolUseID", "OperationID", "operation_id", "operationId", "operationID")
	}
	if request.ToolName == "" {
		request.ToolName = stringJSONField(fields, "ToolName", "tool_name", "toolName", "Tool", "tool", "Name", "name", "Operation", "operation", "Command", "command", "CommandName", "command_name", "commandName", "ToolTitle", "tool_title", "toolTitle")
	}
	if request.Path == "" {
		request.Path = stringJSONField(fields, "Path", "path", "FilePath", "file_path", "filePath", "TargetPath", "target_path", "targetPath", "ResourcePath", "resource_path", "resourcePath", "WorkingDirectory", "working_directory", "workingDirectory", "Cwd", "cwd", "Target", "target", "Resource", "resource", "URI", "uri", "URL", "url", "File", "file", "Filename", "filename")
	}
	if request.Description == "" {
		request.Description = stringJSONField(fields, "Description", "description", "Prompt", "prompt", "Message", "message", "Reason", "reason", "ReasonText", "reason_text", "reasonText", "Summary", "summary", "Details", "details", "Body", "body", "Text", "text", "Content", "content")
	}
	if len(request.Actions) == 0 {
		request.Actions = stringListJSONField(fields, "Actions", "actions", "Options", "options", "Choices", "choices", "AllowedActions", "allowed_actions", "allowedActions", "AvailableActions", "available_actions", "availableActions", "ActionChoices", "action_choices", "actionChoices", "Buttons", "buttons", "ActionsList", "actions_list", "actionsList")
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

func numberJSONField(fields map[string]json.RawMessage, names ...string) (float64, bool) {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			continue
		}
		switch value := value.(type) {
		case json.Number:
			number, err := strconv.ParseFloat(value.String(), 64)
			if err == nil {
				return number, true
			}
		case string:
			number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err == nil {
				return number, true
			}
		}
	}
	return 0, false
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
		var rawValues map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rawValues); err != nil {
			continue
		}
		value = make(map[string]int, len(rawValues))
		for key, rawValue := range rawValues {
			var intValue int
			if err := json.Unmarshal(rawValue, &intValue); err == nil {
				value[key] = intValue
				continue
			}
			var stringValue string
			if err := json.Unmarshal(rawValue, &stringValue); err != nil {
				return nil
			}
			parsed, err := strconv.Atoi(strings.TrimSpace(stringValue))
			if err != nil {
				return nil
			}
			value[key] = parsed
		}
		return value
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
	data = normalizeObjectFieldsToArray(data, "PastedContents", "pasted_contents", "pastedContents")
	type alias PromptExpectation
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"Text",
		"text",
		"value",
		"input",
		"content",
		"message",
		"prompt",
		"prompt_text",
		"promptText",
		"input_text",
		"inputText",
		"Expanded",
		"expanded",
		"expanded_text",
		"expandedText",
		"expanded_prompt",
		"expandedPrompt",
		"expanded_value",
		"expandedValue",
		"full_text",
		"fullText",
		"Cursor",
		"cursor",
		"cursor_index",
		"cursorIndex",
		"cursor_position",
		"cursorPosition",
		"caret",
		"position",
		"Empty",
		"empty",
		"is_empty",
		"isEmpty",
		"empty_prompt",
		"emptyPrompt",
		"blank",
		"PastedContentCount",
		"pasted_content_count",
		"pastedContentCount",
		"PastedContents",
		"pasted_contents",
		"pastedContents",
		"NextPastedID",
		"next_pasted_id",
		"nextPastedId",
		"nextPastedID",
	), &raw); err != nil {
		return err
	}
	*expect = PromptExpectation(raw)

	var fields struct {
		PastedContents      []PastedContentExpectation `json:"PastedContents"`
		PastedContentsSnake []PastedContentExpectation `json:"pasted_contents"`
		PastedContentsCamel []PastedContentExpectation `json:"pastedContents"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if expect.Text == "" {
		expect.Text = stringJSONField(fieldMap, "Text", "text", "value", "input", "content", "message", "prompt", "prompt_text", "promptText", "input_text", "inputText")
	}
	if expect.Expanded == "" {
		expect.Expanded = stringJSONField(fieldMap, "Expanded", "expanded", "expanded_text", "expandedText", "expanded_prompt", "expandedPrompt", "expanded_value", "expandedValue", "full_text", "fullText")
	}
	if expect.Cursor == nil {
		expect.Cursor = intPtrJSONField(fieldMap, "Cursor", "cursor", "cursor_index", "cursorIndex", "cursor_position", "cursorPosition", "caret", "position")
	}
	if empty := boolPtrJSONField(fieldMap, "Empty", "empty", "is_empty", "isEmpty", "empty_prompt", "emptyPrompt", "blank"); empty != nil {
		expect.Empty = *empty
	}
	if expect.PastedContentCount == nil {
		expect.PastedContentCount = intPtrJSONField(fieldMap, "PastedContentCount", "pasted_content_count", "pastedContentCount")
	}
	if fields.PastedContents != nil {
		expect.PastedContents = fields.PastedContents
	}
	if fields.PastedContentsSnake != nil {
		expect.PastedContents = fields.PastedContentsSnake
	}
	if fields.PastedContentsCamel != nil {
		expect.PastedContents = fields.PastedContentsCamel
	}
	if expect.NextPastedID == nil {
		expect.NextPastedID = intPtrJSONField(fieldMap, "NextPastedID", "next_pasted_id", "nextPastedId", "nextPastedID")
	}
	return nil
}

func (expect *PastedContentExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeStringFieldsToArray(data,
		"ContentContains",
		"content_contains",
		"contentContains",
		"Contains",
		"contains",
		"TextContains",
		"text_contains",
		"textContains",
		"ValueContains",
		"value_contains",
		"valueContains",
	)
	type alias PastedContentExpectation
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"ID",
		"id",
		"PastedID",
		"pasted_id",
		"pastedId",
		"pastedID",
		"PastedContentID",
		"pasted_content_id",
		"pastedContentId",
		"pastedContentID",
		"ContentID",
		"content_id",
		"contentId",
		"contentID",
		"Type",
		"type",
		"Kind",
		"kind",
		"ContentKind",
		"content_kind",
		"contentKind",
		"ItemType",
		"item_type",
		"itemType",
		"PastedType",
		"pasted_type",
		"pastedType",
		"Content",
		"content",
		"Value",
		"value",
		"Text",
		"text",
		"Body",
		"body",
		"Message",
		"message",
		"Data",
		"data",
		"Base64",
		"base64",
		"ContentContains",
		"content_contains",
		"contentContains",
		"Contains",
		"contains",
		"TextContains",
		"text_contains",
		"textContains",
		"ValueContains",
		"value_contains",
		"valueContains",
		"MediaType",
		"media_type",
		"mediaType",
		"MimeType",
		"mime_type",
		"mimeType",
		"ContentType",
		"content_type",
		"contentType",
		"Media",
		"media",
		"Mime",
		"mime",
		"FileType",
		"file_type",
		"fileType",
		"Filename",
		"filename",
		"FileName",
		"file_name",
		"fileName",
		"Name",
		"name",
		"SourcePath",
		"source_path",
		"sourcePath",
		"Path",
		"path",
		"FilePath",
		"filePath",
		"file_path",
		"Dimensions",
		"dimensions",
		"ImageDimensions",
		"imageDimensions",
		"image_dimensions",
	), &raw); err != nil {
		return err
	}
	*expect = PastedContentExpectation(raw)

	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if expect.ID == 0 {
		if id := intPtrJSONField(fieldMap, "ID", "id", "PastedID", "pasted_id", "pastedId", "pastedID", "PastedContentID", "pasted_content_id", "pastedContentId", "pastedContentID", "ContentID", "content_id", "contentId", "contentID"); id != nil {
			expect.ID = *id
		}
	}
	if expect.Type == "" {
		expect.Type = stringJSONField(fieldMap, "Type", "type", "Kind", "kind", "ContentKind", "content_kind", "contentKind", "ItemType", "item_type", "itemType", "PastedType", "pasted_type", "pastedType")
	}
	if expect.Content == "" {
		expect.Content = scalarStringJSONField(fieldMap, "Content", "content", "Value", "value", "Text", "text", "Body", "body", "Message", "message", "Data", "data", "Base64", "base64")
	}
	if expect.SourcePath == "" {
		expect.SourcePath = stringJSONField(fieldMap, "SourcePath", "source_path", "sourcePath", "Path", "path", "FilePath", "filePath", "file_path")
	}
	if expect.Dimensions == nil {
		expect.Dimensions = imageDimensionsJSONField(fieldMap, "Dimensions", "dimensions", "ImageDimensions", "imageDimensions", "image_dimensions")
	}
	if values := stringListJSONField(fieldMap, "ContentContains", "content_contains", "contentContains", "Contains", "contains", "TextContains", "text_contains", "textContains", "ValueContains", "value_contains", "valueContains"); values != nil {
		expect.ContentContains = values
	}
	if expect.MediaType == "" {
		expect.MediaType = stringJSONField(fieldMap, "MediaType", "media_type", "mediaType", "MimeType", "mime_type", "mimeType", "ContentType", "content_type", "contentType", "Media", "media", "Mime", "mime", "FileType", "file_type", "fileType")
	}
	if expect.Filename == "" {
		expect.Filename = stringJSONField(fieldMap, "Filename", "filename", "FileName", "file_name", "fileName", "Name", "name", "Path", "path")
	}
	return nil
}

func (task *TaskStatus) UnmarshalJSON(data []byte) error {
	type alias TaskStatus
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data, scriptTaskStatusAliasFields()...), &raw); err != nil {
		return err
	}
	*task = TaskStatus(raw)

	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if id := scalarStringJSONField(fieldMap, scriptTaskIDFields("ID")...); id != "" {
		task.ID = id
	}
	if title := stringJSONField(fieldMap, scriptTaskTitleFields...); title != "" {
		task.Title = title
	}
	if state := stringJSONField(fieldMap, scriptTaskStateFields...); state != "" {
		task.State = state
	}
	if detail := stringJSONField(fieldMap, scriptTaskDetailFields...); detail != "" {
		task.Detail = detail
	}
	if progress := intPtrJSONField(fieldMap, scriptTaskProgressFields...); progress != nil {
		task.Progress = *progress
	}
	return nil
}

func (expect *VimExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeBoolFields(data,
		"Enabled",
		"enabled",
		"VimEnabled",
		"vim_enabled",
		"vimEnabled",
		"IsEnabled",
		"is_enabled",
		"isEnabled",
		"Active",
		"active",
		"RegisterLinewise",
		"register_linewise",
		"registerLinewise",
		"Linewise",
		"linewise",
		"IsLinewise",
		"is_linewise",
		"isLinewise",
		"RegisterLineWise",
		"register_line_wise",
		"registerLineWise",
	)
	type alias VimExpectation
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"Enabled",
		"enabled",
		"VimEnabled",
		"vim_enabled",
		"vimEnabled",
		"IsEnabled",
		"is_enabled",
		"isEnabled",
		"Active",
		"active",
		"Mode",
		"mode",
		"VimMode",
		"vim_mode",
		"vimMode",
		"ModeName",
		"mode_name",
		"modeName",
		"CurrentMode",
		"current_mode",
		"currentMode",
		"State",
		"state",
		"Register",
		"register",
		"VimRegister",
		"vim_register",
		"vimRegister",
		"RegisterValue",
		"register_value",
		"registerValue",
		"YankRegister",
		"yank_register",
		"yankRegister",
		"RegisterLinewise",
		"register_linewise",
		"registerLinewise",
		"Linewise",
		"linewise",
		"IsLinewise",
		"is_linewise",
		"isLinewise",
		"RegisterLineWise",
		"register_line_wise",
		"registerLineWise",
	), &raw); err != nil {
		return err
	}
	*expect = VimExpectation(raw)

	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if expect.Enabled == nil {
		expect.Enabled = boolPtrJSONField(fieldMap, "Enabled", "enabled", "VimEnabled", "vim_enabled", "vimEnabled", "IsEnabled", "is_enabled", "isEnabled", "Active", "active")
	}
	if expect.Mode == "" {
		if mode := stringJSONField(fieldMap, "Mode", "mode", "VimMode", "vim_mode", "vimMode", "ModeName", "mode_name", "modeName", "CurrentMode", "current_mode", "currentMode", "State", "state"); mode != "" {
			expect.Mode = normalizeVimMode(mode)
		}
	}
	if expect.Register == "" {
		expect.Register = stringJSONField(fieldMap, "Register", "register", "VimRegister", "vim_register", "vimRegister", "RegisterValue", "register_value", "registerValue", "YankRegister", "yank_register", "yankRegister")
	}
	if expect.RegisterLinewise == nil {
		expect.RegisterLinewise = boolPtrJSONField(fieldMap, "RegisterLinewise", "register_linewise", "registerLinewise", "Linewise", "linewise", "IsLinewise", "is_linewise", "isLinewise", "RegisterLineWise", "register_line_wise", "registerLineWise")
	}
	return nil
}

func (expect *TasksExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeObjectFieldsToArray(data, "Contains", "contains")
	type alias TasksExpectation
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"Count",
		"count",
		"TaskCount",
		"task_count",
		"taskCount",
		"Total",
		"total",
		"Size",
		"size",
		"Length",
		"length",
		"StateCounts",
		"state_counts",
		"stateCounts",
		"StatusCounts",
		"status_counts",
		"statusCounts",
		"Counts",
		"counts",
		"CountsByState",
		"counts_by_state",
		"countsByState",
		"ByState",
		"by_state",
		"byState",
	), &raw); err != nil {
		return err
	}
	*expect = TasksExpectation(raw)

	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if expect.Count == nil {
		expect.Count = intPtrJSONField(fieldMap, "Count", "count", "TaskCount", "task_count", "taskCount", "Total", "total", "Size", "size", "Length", "length")
	}
	if len(expect.StateCounts) == 0 {
		expect.StateCounts = intMapJSONField(fieldMap, "StateCounts", "state_counts", "stateCounts", "StatusCounts", "status_counts", "statusCounts", "Counts", "counts", "CountsByState", "counts_by_state", "countsByState", "ByState", "by_state", "byState")
	}
	return nil
}

func (expect *TaskExpectation) UnmarshalJSON(data []byte) error {
	type alias TaskExpectation
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data, scriptTaskStatusAliasFields()...), &raw); err != nil {
		return err
	}
	*expect = TaskExpectation(raw)

	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if id := scalarStringJSONField(fieldMap, scriptTaskIDFields("ID")...); id != "" {
		expect.ID = id
	}
	if title := stringJSONField(fieldMap, scriptTaskTitleFields...); title != "" {
		expect.Title = title
	}
	if state := stringJSONField(fieldMap, scriptTaskStateFields...); state != "" {
		expect.State = state
	}
	if detail := stringJSONField(fieldMap, scriptTaskDetailFields...); detail != "" {
		expect.Detail = detail
	}
	if progress := intPtrJSONField(fieldMap, scriptTaskProgressFields...); progress != nil {
		expect.Progress = progress
	}
	return nil
}

func (expect *ViewportExpectation) UnmarshalJSON(data []byte) error {
	data = normalizeStringFieldsToArray(data,
		"VisibleContains",
		"visible_contains",
		"visibleContains",
		"VisibleNotContains",
		"visible_not_contains",
		"visibleNotContains",
	)
	type alias ViewportExpectation
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"Offset",
		"offset",
		"scroll_offset",
		"scrollOffset",
		"viewport_offset",
		"viewportOffset",
		"top",
		"start_line",
		"startLine",
		"VisibleLineCount",
		"visible_line_count",
		"visibleLineCount",
		"line_count",
		"lineCount",
		"visible_rows",
		"visibleRows",
		"visible_lines",
		"visibleLines",
		"rows",
		"VisibleContains",
		"visible_contains",
		"visibleContains",
		"VisibleNotContains",
		"visible_not_contains",
		"visibleNotContains",
	), &raw); err != nil {
		return err
	}
	*expect = ViewportExpectation(raw)

	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if expect.Offset == nil {
		expect.Offset = intPtrJSONField(fieldMap, "Offset", "offset", "scroll_offset", "scrollOffset", "viewport_offset", "viewportOffset", "top", "start_line", "startLine")
	}
	if expect.VisibleLineCount == 0 {
		if visibleLineCount := intPtrJSONField(fieldMap, "VisibleLineCount", "visible_line_count", "visibleLineCount", "line_count", "lineCount", "visible_rows", "visibleRows", "visible_lines", "visibleLines", "rows"); visibleLineCount != nil {
			expect.VisibleLineCount = *visibleLineCount
		}
	}
	if len(expect.VisibleContains) == 0 {
		expect.VisibleContains = stringListJSONField(fieldMap, "VisibleContains", "visible_contains", "visibleContains")
	}
	if len(expect.VisibleNotContains) == 0 {
		expect.VisibleNotContains = stringListJSONField(fieldMap, "VisibleNotContains", "visible_not_contains", "visibleNotContains")
	}
	return nil
}

func (expect *ScreenExpectation) UnmarshalJSON(data []byte) error {
	type alias ScreenExpectation
	var raw alias
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"Width",
		"width",
		"screen_width",
		"screenWidth",
		"columns",
		"cols",
		"column_count",
		"columnCount",
		"Height",
		"height",
		"screen_height",
		"screenHeight",
		"rows",
		"lines",
		"row_count",
		"rowCount",
		"line_count",
		"lineCount",
	), &raw); err != nil {
		return err
	}
	*expect = ScreenExpectation(raw)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if expect.Width == 0 {
		if width := intPtrJSONField(fields, "Width", "width", "screen_width", "screenWidth", "columns", "cols", "column_count", "columnCount"); width != nil {
			expect.Width = *width
		}
	}
	if expect.Height == 0 {
		if height := intPtrJSONField(fields, "Height", "height", "screen_height", "screenHeight", "rows", "lines", "row_count", "rowCount", "line_count", "lineCount"); height != nil {
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
	if err := json.Unmarshal(stripJSONAliasFields(data,
		"Active",
		"active",
		"is_active",
		"isActive",
		"open",
		"visible",
		"Cursor",
		"cursor",
		"cursor_index",
		"cursorIndex",
		"cursor_position",
		"cursorPosition",
		"caret",
		"position",
		"ResultCount",
		"result_count",
		"resultCount",
		"match_count",
		"matchCount",
		"matches",
		"results",
		"total",
		"NoResults",
		"noResults",
		"no_results",
		"no_matches",
		"noMatches",
		"empty",
		"empty_results",
		"emptyResults",
	), &raw); err != nil {
		return err
	}
	*expect = ReverseSearchExpectation(raw)

	fieldMap := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fieldMap); err != nil {
		return err
	}
	if active := boolPtrJSONField(fieldMap, "Active", "active", "is_active", "isActive", "open", "visible"); active != nil {
		expect.Active = *active
	}
	if expect.Query == "" {
		expect.Query = stringJSONField(fieldMap, "search", "term", "pattern", "text", "input", "value")
	}
	if expect.Cursor == nil {
		expect.Cursor = intPtrJSONField(fieldMap, "Cursor", "cursor", "cursor_index", "cursorIndex", "cursor_position", "cursorPosition", "caret", "position")
	}
	if expect.Current == "" {
		expect.Current = stringJSONField(fieldMap, "current_result", "currentResult", "current_match", "currentMatch", "match", "selected", "selection")
	}
	if expect.ResultCount == 0 {
		if resultCount := intPtrJSONField(fieldMap, "ResultCount", "result_count", "resultCount", "match_count", "matchCount", "matches", "results", "total"); resultCount != nil {
			expect.ResultCount = *resultCount
		}
	}
	if noResults := boolPtrJSONField(fieldMap, "NoResults", "noResults", "no_results", "no_matches", "noMatches", "empty", "empty_results", "emptyResults"); noResults != nil {
		expect.NoResults = *noResults
	}
	return nil
}
