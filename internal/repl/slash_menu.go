package repl

import (
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

// SlashMenu is an overlay listing slash commands filtered by a typed query.
type SlashMenu struct {
	all      []contracts.Command
	filtered []contracts.Command
	query    string
	cursor   int
}

// NewSlashMenu constructs a SlashMenu pre-filtered to commands whose names
// start with query.
func NewSlashMenu(cmds []contracts.Command, query string) *SlashMenu {
	m := &SlashMenu{all: cmds, query: query}
	m.refilter()
	return m
}

func (m *SlashMenu) refilter() {
	q := strings.ToLower(strings.TrimSpace(m.query))
	filtered := make([]contracts.Command, 0, len(m.all))
	for _, cmd := range m.all {
		if cmd.Hidden {
			continue
		}
		if q == "" || strings.HasPrefix(strings.ToLower(cmd.Name), q) {
			filtered = append(filtered, cmd)
		}
	}
	m.filtered = filtered
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// ApplyKey handles a key event for the slash menu. It returns (result, true)
// when the key was consumed. Submit is non-empty when the user selected a
// command; Dismissed is true when they pressed Esc.
func (m *SlashMenu) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true
	case tui.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		if len(m.filtered) == 0 {
			return OverlayResult{Dismissed: true}, true
		}
		return OverlayResult{Submit: "/" + m.filtered[m.cursor].Name}, true
	case tui.KeyBackspace:
		if len(m.query) > 0 {
			runes := []rune(m.query)
			m.query = string(runes[:len(runes)-1])
			m.refilter()
		}
		return OverlayResult{}, true
	case tui.KeyRune:
		m.query += string(key.Rune)
		m.refilter()
		return OverlayResult{}, true
	default:
		return OverlayResult{}, false
	}
}

// Render returns display lines for the slash menu, capped to height rows and
// width columns.
func (m *SlashMenu) Render(width, height int) []string {
	header := "Commands (" + m.query + "):"
	lines := []string{header}
	max := height - 2
	if max < 1 {
		max = 1
	}
	for i, cmd := range m.filtered {
		if i >= max {
			break
		}
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		line := marker + "/" + cmd.Name
		if cmd.Description != "" {
			line += " — " + cmd.Description
		}
		line = truncateToWidth(line, width)
		lines = append(lines, line)
	}
	return lines
}

// truncateToWidth shortens s to at most width terminal columns.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	runes := []rune(s)
	if tui.TerminalVisibleWidth(s) <= width {
		return s
	}
	// Binary-search trim by runes (plain ASCII commands; width approximation is fine).
	for len(runes) > 0 && tui.TerminalVisibleWidth(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
