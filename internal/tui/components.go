package tui

import (
	"fmt"
	"strings"
)

func RenderMessages(messages []Message, width int) []string {
	var lines []string
	for _, message := range messages {
		prefix := rolePrefix(message.Role)
		bodyWidth := width - len(prefix)
		if bodyWidth < 4 {
			bodyWidth = width
			prefix = ""
		}
		wrapped := wrapText(message.Text, bodyWidth)
		for i, line := range wrapped {
			if i == 0 {
				lines = append(lines, prefix+line)
			} else {
				lines = append(lines, strings.Repeat(" ", len(prefix))+line)
			}
		}
	}
	return lines
}

func RenderStatusLine(status string, width int) string {
	if strings.TrimSpace(status) == "" {
		status = "ready"
	}
	return reverseVideo(padOrTrim(" "+status, width))
}

func RenderPromptLine(prompt PromptState, width int) string {
	lines := RenderPromptLines(prompt, width)
	if len(lines) == 0 {
		return padOrTrim("> ", width)
	}
	return lines[0]
}

func RenderPromptLines(prompt PromptState, width int) []string {
	prefix := "> "
	continuation := "  "
	rawLines := strings.Split(prompt.Text, "\n")
	if len(rawLines) == 0 {
		rawLines = []string{""}
	}
	lines := make([]string, 0, len(rawLines))
	for index, line := range rawLines {
		linePrefix := continuation
		if index == 0 {
			linePrefix = prefix
		}
		lines = append(lines, padOrTrim(linePrefix+line, width))
	}
	return lines
}

func RenderReverseSearchLine(state ReverseSearchState, width int) string {
	current, ok := state.Current()
	if !ok {
		current = "no matches"
	}
	line := "(reverse-i-search) `" + state.Query + "': " + current
	return padOrTrim(line, width)
}

func RenderDialog(dialog Dialog, width int) []string {
	if width < 4 {
		return nil
	}
	inner := width - 4
	var lines []string
	title := strings.TrimSpace(dialog.Title)
	if title == "" {
		title = "Dialog"
	}
	lines = append(lines, "+"+strings.Repeat("-", width-2)+"+")
	lines = append(lines, "| "+padOrTrim(title, inner)+" |")
	lines = append(lines, "| "+padOrTrim("", inner)+" |")
	for _, line := range wrapText(dialog.Body, inner) {
		lines = append(lines, "| "+padOrTrim(line, inner)+" |")
	}
	if len(dialog.Actions) > 0 {
		lines = append(lines, "| "+padOrTrim("", inner)+" |")
		var actions []string
		for i, action := range dialog.Actions {
			label := fmt.Sprintf(" %s ", action)
			if i == dialog.Focused {
				label = "[" + action + "]"
			}
			actions = append(actions, label)
		}
		lines = append(lines, "| "+padOrTrim(strings.Join(actions, " "), inner)+" |")
	}
	lines = append(lines, "+"+strings.Repeat("-", width-2)+"+")
	return lines
}

func rolePrefix(role Role) string {
	switch role {
	case RoleUser:
		return "user: "
	case RoleAssistant:
		return "assistant: "
	case RoleTool:
		return "tool: "
	case RoleSystem:
		return "system: "
	default:
		return ""
	}
}

func reverseVideo(text string) string {
	return "\x1b[7m" + text + "\x1b[0m"
}
