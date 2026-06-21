package repl

import (
	"fmt"

	"ccgo/internal/tui"
)

// ResumeEntry is one prior session shown in the resume picker.
type ResumeEntry struct {
	ID           string
	Summary      string
	ProjectPath  string
	ModifiedUnix int64
}

// ResumePicker is an overlay listing resumable sessions, newest first.
type ResumePicker struct {
	entries []ResumeEntry
	cursor  int
}

func NewResumePicker(entries []ResumeEntry) *ResumePicker {
	// Make a defensive copy to ensure immutability
	copy := make([]ResumeEntry, len(entries))
	for i := range entries {
		copy[i] = entries[i]
	}
	return &ResumePicker{entries: copy}
}

func (p *ResumePicker) Selected() (ResumeEntry, bool) {
	if p.cursor < 0 || p.cursor >= len(p.entries) {
		return ResumeEntry{}, false
	}
	return p.entries[p.cursor], true
}

func (p *ResumePicker) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true
	case tui.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if p.cursor < len(p.entries)-1 {
			p.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		if entry, ok := p.Selected(); ok {
			return OverlayResult{Submit: "resume:" + entry.ID}, true
		}
		return OverlayResult{Dismissed: true}, true
	default:
		return OverlayResult{}, false
	}
}

func (p *ResumePicker) Render(width, height int) []string {
	lines := []string{"Resume a conversation:"}
	max := height - 2
	if max < 1 {
		max = 1
	}
	for i, e := range p.entries {
		if i >= max {
			break
		}
		marker := "  "
		if i == p.cursor {
			marker = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s · %s", marker, e.Summary, e.ProjectPath))
	}
	return lines
}
