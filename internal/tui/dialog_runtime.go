package tui

import "sort"

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

func (r *DialogRuntime) RemoveTask(id string) {
	delete(r.Tasks, id)
}

func (r *DialogRuntime) CancelActive() DialogResult {
	if r.Active == nil {
		return DialogResult{}
	}
	return r.Resolve(ScreenEvent{Type: ScreenEventCancelled, DialogID: r.Active.ID, DialogKind: r.Active.Kind})
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
	return result
}

func taskStateRank(state string) int {
	switch state {
	case "running":
		return 0
	case "pending":
		return 1
	case "failed":
		return 2
	case "completed", "done":
		return 3
	default:
		return 4
	}
}
