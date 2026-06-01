package tui

import (
	"fmt"
	"strings"
)

type PermissionRequest struct {
	ToolName    string
	Path        string
	Description string
	Actions     []string
}

type TaskStatus struct {
	ID       string
	Title    string
	State    string
	Detail   string
	Progress int
}

func PermissionDialog(request PermissionRequest) Dialog {
	actions := request.Actions
	if len(actions) == 0 {
		actions = []string{"Allow", "Allow Session", "Deny"}
	}
	var parts []string
	if request.ToolName != "" {
		parts = append(parts, "Tool: "+request.ToolName)
	}
	if request.Path != "" {
		parts = append(parts, "Path: "+request.Path)
	}
	if request.Description != "" {
		parts = append(parts, request.Description)
	}
	body := strings.Join(parts, "\n")
	if body == "" {
		body = "Permission required."
	}
	return Dialog{Title: "Permission", Body: body, Actions: append([]string(nil), actions...)}
}

func TaskDialog(tasks []TaskStatus) Dialog {
	if len(tasks) == 0 {
		return Dialog{Title: "Tasks", Body: "No active tasks.", Actions: []string{"Close"}}
	}
	lines := make([]string, 0, len(tasks))
	for _, task := range tasks {
		title := task.Title
		if title == "" {
			title = task.ID
		}
		line := fmt.Sprintf("%s [%s]", title, task.State)
		if task.Progress > 0 {
			line = fmt.Sprintf("%s %d%%", line, clampPercent(task.Progress))
		}
		if task.Detail != "" {
			line += " - " + task.Detail
		}
		lines = append(lines, line)
	}
	return Dialog{Title: "Tasks", Body: strings.Join(lines, "\n"), Actions: []string{"Close"}}
}

func clampPercent(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}
