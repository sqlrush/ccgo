package tui

import (
	"sort"
	"strings"
)

type DialogResultStatus string

const (
	DialogResultNone      DialogResultStatus = ""
	DialogResultAllowed   DialogResultStatus = "allowed"
	DialogResultDenied    DialogResultStatus = "denied"
	DialogResultCancelled DialogResultStatus = "cancelled"
	DialogResultClosed    DialogResultStatus = "closed"
)

type DialogResult struct {
	ID     string
	Kind   DialogKind
	Action string
	Status DialogResultStatus
	Found  bool
	Stale  bool
}

type DialogRuntime struct {
	Permissions map[string]PermissionRequest
	Tasks       map[string]TaskStatus
	Active      *Dialog
}

func NewDialogRuntime() *DialogRuntime {
	return &DialogRuntime{
		Permissions: map[string]PermissionRequest{},
		Tasks:       map[string]TaskStatus{},
	}
}

func (r *DialogRuntime) RequestPermission(request PermissionRequest) Dialog {
	if r.Permissions == nil {
		r.Permissions = map[string]PermissionRequest{}
	}
	if request.ID == "" {
		request.ID = "permission"
	}
	r.Permissions[request.ID] = request
	dialog := PermissionDialog(request)
	r.Active = &dialog
	return dialog
}

func (r *DialogRuntime) UpsertTask(task TaskStatus) {
	if r.Tasks == nil {
		r.Tasks = map[string]TaskStatus{}
	}
	if task.ID == "" {
		task.ID = task.Title
	}
	if task.ID == "" {
		return
	}
	r.Tasks[task.ID] = task
}

func (r *DialogRuntime) StartTask(id string, title string, detail string) TaskStatus {
	return r.updateTask(id, title, TaskRunning, detail, 0)
}

func (r *DialogRuntime) UpdateTaskProgress(id string, detail string, progress int) TaskStatus {
	task := r.Tasks[id]
	return r.updateTask(id, task.Title, task.State, detail, progress)
}

func (r *DialogRuntime) CompleteTask(id string, detail string) TaskStatus {
	task := r.Tasks[id]
	return r.updateTask(id, task.Title, TaskCompleted, detail, 100)
}

func (r *DialogRuntime) FailTask(id string, detail string) TaskStatus {
	task := r.Tasks[id]
	return r.updateTask(id, task.Title, TaskFailed, detail, task.Progress)
}

func (r *DialogRuntime) CancelTask(id string, detail string) TaskStatus {
	task := r.Tasks[id]
	return r.updateTask(id, task.Title, TaskCancelled, detail, task.Progress)
}

func (r *DialogRuntime) RemoveTask(id string) {
	delete(r.Tasks, id)
}

func (r *DialogRuntime) CancelActive() DialogResult {
	if r.Active == nil {
		return DialogResult{}
	}
	return r.Resolve(ScreenEvent{Type: ScreenEventCancelled, DialogID: r.Active.ID, DialogKind: r.Active.Kind})
}

func (r *DialogRuntime) CancelPermission(id string) DialogResult {
	if id == "" && r.Active != nil && r.Active.Kind == DialogPermission {
		id = r.Active.ID
	}
	result := DialogResult{ID: id, Kind: DialogPermission}
	if id == "" {
		return result
	}
	if _, ok := r.Permissions[id]; !ok {
		return result
	}
	result.Found = true
	result.Status = DialogResultCancelled
	delete(r.Permissions, id)
	if r.Active != nil && r.Active.Kind == DialogPermission && r.Active.ID == id {
		r.Active = nil
	}
	r.promoteNextPermission()
	return result
}

func (r *DialogRuntime) CancelPermissions() []DialogResult {
	if len(r.Permissions) == 0 {
		return nil
	}
	ids := make([]string, 0, len(r.Permissions))
	for id := range r.Permissions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	results := make([]DialogResult, 0, len(ids))
	for _, id := range ids {
		results = append(results, r.CancelPermission(id))
	}
	return results
}

func (r *DialogRuntime) OpenTasksDialog() Dialog {
	tasks := r.SortedTasks()
	dialog := TaskDialog(tasks)
	r.Active = &dialog
	return dialog
}

func (r *DialogRuntime) SortedTasks() []TaskStatus {
	tasks := make([]TaskStatus, 0, len(r.Tasks))
	for _, task := range r.Tasks {
		tasks = append(tasks, task)
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].State != tasks[j].State {
			return taskStateRank(tasks[i].State) < taskStateRank(tasks[j].State)
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks
}

func (r *DialogRuntime) StatusLine(base string) string {
	var parts []string
	if strings.TrimSpace(base) != "" {
		parts = append(parts, strings.TrimSpace(base))
	}
	if r.Active != nil && r.Active.Title != "" {
		parts = append(parts, "dialog: "+strings.ToLower(r.Active.Title))
	}
	if pending := len(r.Permissions); pending > 0 {
		parts = append(parts, "permissions: "+itoa(pending))
	}
	for _, part := range taskStateSummary(r.SortedTasks()) {
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "ready"
	}
	return strings.Join(parts, " | ")
}

func (r *DialogRuntime) ApplyToScreen(screen *REPLScreen, baseStatus string) {
	if screen == nil {
		return
	}
	screen.Status = r.StatusLine(baseStatus)
	if r.Active == nil {
		screen.Dialog = nil
		return
	}
	dialog := *r.Active
	screen.Dialog = &dialog
}

func (r *DialogRuntime) ResolveScreenEvent(screen *REPLScreen, event ScreenEvent, baseStatus string) DialogResult {
	result := r.Resolve(event)
	r.ApplyToScreen(screen, baseStatus)
	return result
}

func (r *DialogRuntime) Resolve(event ScreenEvent) DialogResult {
	if event.Type != ScreenEventDialogAction && event.Type != ScreenEventCancelled {
		return DialogResult{}
	}
	id := event.DialogID
	if id == "" && r.Active != nil {
		id = r.Active.ID
	}
	kind := event.DialogKind
	if kind == "" && r.Active != nil {
		kind = r.Active.Kind
	}
	result := DialogResult{ID: id, Kind: kind, Action: event.Value}
	if r.Active != nil && (r.Active.ID != id || r.Active.Kind != kind) {
		result.Stale = true
		return result
	}
	switch kind {
	case DialogPermission:
		_, ok := r.Permissions[id]
		if !ok {
			return result
		}
		result.Found = true
		switch {
		case event.Type == ScreenEventCancelled:
			result.Status = DialogResultCancelled
		case event.Value == "Deny":
			result.Status = DialogResultDenied
		default:
			result.Status = DialogResultAllowed
		}
		delete(r.Permissions, id)
	case DialogTask:
		result.Found = true
		if event.Type == ScreenEventCancelled {
			result.Status = DialogResultCancelled
		} else {
			result.Status = DialogResultClosed
		}
	default:
		result.Found = true
		if event.Type == ScreenEventCancelled {
			result.Status = DialogResultCancelled
		} else {
			result.Status = DialogResultClosed
		}
	}
	r.Active = nil
	r.promoteNextPermission()
	return result
}

func (r *DialogRuntime) promoteNextPermission() {
	if r.Active != nil || len(r.Permissions) == 0 {
		return
	}
	ids := make([]string, 0, len(r.Permissions))
	for id := range r.Permissions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	dialog := PermissionDialog(r.Permissions[ids[0]])
	r.Active = &dialog
}

func taskStateRank(state string) int {
	switch state {
	case TaskRunning:
		return 0
	case TaskPending:
		return 1
	case TaskFailed:
		return 2
	case TaskCancelled:
		return 3
	case TaskCompleted, "done":
		return 3
	default:
		return 4
	}
}

func taskStateSummary(tasks []TaskStatus) []string {
	if len(tasks) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, task := range tasks {
		state := task.State
		if state == "" {
			state = TaskPending
		}
		counts[state]++
	}
	order := []string{TaskRunning, TaskPending, TaskFailed, TaskCancelled, TaskCompleted}
	var out []string
	for _, state := range order {
		if counts[state] > 0 {
			out = append(out, state+": "+itoa(counts[state]))
		}
	}
	return out
}

func (r *DialogRuntime) updateTask(id string, title string, state string, detail string, progress int) TaskStatus {
	if r.Tasks == nil {
		r.Tasks = map[string]TaskStatus{}
	}
	if id == "" {
		id = title
	}
	if id == "" {
		return TaskStatus{}
	}
	if title == "" {
		title = id
	}
	task := TaskStatus{ID: id, Title: title, State: state, Detail: detail, Progress: clampPercent(progress)}
	if task.State == "" {
		task.State = TaskPending
	}
	r.Tasks[id] = task
	return task
}
