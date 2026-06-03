package tui

import (
	"strings"
	"unicode"

	"ccgo/internal/session"
)

const DefaultReverseSearchLimit = 20

func NewReverseSearchState(history []string, query string) ReverseSearchState {
	return NewReverseSearchStateFromEntries(reverseSearchEntriesFromStrings(history), query)
}

func NewReverseSearchStateFromEntries(history []session.HistoryEntry, query string) ReverseSearchState {
	state := ReverseSearchState{Active: true, Query: query, Cursor: len([]rune(query))}
	state.ResultEntries = FilterHistoryEntriesForReverseSearch(history, query, DefaultReverseSearchLimit)
	state.Results = reverseSearchEntryDisplays(state.ResultEntries)
	return state
}

func FilterHistoryForReverseSearch(history []string, query string, limit int) []string {
	return reverseSearchEntryDisplays(FilterHistoryEntriesForReverseSearch(reverseSearchEntriesFromStrings(history), query, limit))
}

func FilterHistoryEntriesForReverseSearch(history []session.HistoryEntry, query string, limit int) []session.HistoryEntry {
	if limit <= 0 {
		limit = DefaultReverseSearchLimit
	}
	query = strings.ToLower(strings.TrimSpace(query))
	seen := map[string]struct{}{}
	out := make([]session.HistoryEntry, 0, limit)
	for i := len(history) - 1; i >= 0; i-- {
		entry := cloneHistoryEntry(history[i])
		item := strings.TrimSpace(entry.Display)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item), query) {
			continue
		}
		seen[item] = struct{}{}
		entry.Display = item
		out = append(out, entry)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (s *ReverseSearchState) Current() (string, bool) {
	if !s.Active || len(s.Results) == 0 || s.Focused < 0 || s.Focused >= len(s.Results) {
		return "", false
	}
	return s.Results[s.Focused], true
}

func (s *ReverseSearchState) CurrentEntry() (session.HistoryEntry, bool) {
	if !s.Active || s.Focused < 0 {
		return session.HistoryEntry{}, false
	}
	if s.Focused < len(s.ResultEntries) {
		return cloneHistoryEntry(s.ResultEntries[s.Focused]), true
	}
	if selected, ok := s.Current(); ok {
		return session.HistoryEntry{Display: selected}, true
	}
	return session.HistoryEntry{}, false
}

func (s *ReverseSearchState) Move(delta int) {
	if len(s.Results) == 0 {
		s.Focused = 0
		return
	}
	s.Focused += delta
	if s.Focused < 0 {
		s.Focused = 0
	}
	if s.Focused >= len(s.Results) {
		s.Focused = len(s.Results) - 1
	}
}

func (s *ReverseSearchState) AppendRune(history []session.HistoryEntry, r rune) {
	runes := []rune(s.Query)
	s.clampCursor()
	runes = append(runes[:s.Cursor], append([]rune{r}, runes[s.Cursor:]...)...)
	s.Query = string(runes)
	s.Cursor++
	s.refresh(history)
}

func (s *ReverseSearchState) Backspace(history []session.HistoryEntry) {
	runes := []rune(s.Query)
	s.clampCursor()
	if s.Cursor == 0 {
		return
	}
	runes = append(runes[:s.Cursor-1], runes[s.Cursor:]...)
	s.Query = string(runes)
	s.Cursor--
	s.refresh(history)
}

func (s *ReverseSearchState) DeleteForward(history []session.HistoryEntry) {
	runes := []rune(s.Query)
	s.clampCursor()
	if s.Cursor >= len(runes) {
		return
	}
	runes = append(runes[:s.Cursor], runes[s.Cursor+1:]...)
	s.Query = string(runes)
	s.refresh(history)
}

func (s *ReverseSearchState) MoveCursor(delta int) {
	s.Cursor += delta
	s.clampCursor()
}

func (s *ReverseSearchState) MoveWordBackward() {
	runes := []rune(s.Query)
	s.clampCursor()
	s.Cursor = reverseSearchWordStart(runes, s.Cursor)
}

func (s *ReverseSearchState) MoveWordForward() {
	runes := []rune(s.Query)
	s.clampCursor()
	s.Cursor = reverseSearchWordForward(runes, s.Cursor)
}

func (s *ReverseSearchState) MoveStart() {
	s.Cursor = 0
}

func (s *ReverseSearchState) MoveEnd() {
	s.Cursor = len([]rune(s.Query))
}

func (s *ReverseSearchState) DeleteToEnd(history []session.HistoryEntry) {
	runes := []rune(s.Query)
	s.clampCursor()
	killed := string(runes[s.Cursor:])
	s.Query = string(runes[:s.Cursor])
	sharedKillRing.push(killed, killRingAppend)
	s.refresh(history)
}

func (s *ReverseSearchState) DeleteToStart(history []session.HistoryEntry) {
	runes := []rune(s.Query)
	s.clampCursor()
	killed := string(runes[:s.Cursor])
	s.Query = string(runes[s.Cursor:])
	s.Cursor = 0
	sharedKillRing.push(killed, killRingPrepend)
	s.refresh(history)
}

func (s *ReverseSearchState) DeleteWordBackward(history []session.HistoryEntry) {
	runes := []rune(s.Query)
	s.clampCursor()
	end := s.Cursor
	start := reverseSearchWordStart(runes, end)
	if start == end {
		return
	}
	killed := string(runes[start:end])
	runes = append(runes[:start], runes[end:]...)
	s.Query = string(runes)
	s.Cursor = start
	sharedKillRing.push(killed, killRingPrepend)
	s.refresh(history)
}

func (s *ReverseSearchState) DeleteWordForward(history []session.HistoryEntry) {
	runes := []rune(s.Query)
	s.clampCursor()
	start := s.Cursor
	end := reverseSearchWordForward(runes, start)
	if start == end {
		return
	}
	runes = append(runes[:start], runes[end:]...)
	s.Query = string(runes)
	s.Cursor = start
	s.refresh(history)
}

func (s *ReverseSearchState) YankLastKill(history []session.HistoryEntry) {
	text := sharedKillRing.lastKill()
	if text == "" {
		return
	}
	runes := []rune(s.Query)
	insert := []rune(text)
	s.clampCursor()
	start := s.Cursor
	runes = append(runes[:s.Cursor], append(insert, runes[s.Cursor:]...)...)
	s.Query = string(runes)
	s.Cursor += len(insert)
	sharedKillRing.recordYank(start, len(insert))
	s.refresh(history)
}

func (s *ReverseSearchState) YankPop(history []session.HistoryEntry) {
	text, start, length, ok := sharedKillRing.nextYankPop()
	if !ok {
		return
	}
	runes := []rune(s.Query)
	if start < 0 {
		start = 0
	}
	if start > len(runes) {
		start = len(runes)
	}
	end := start + length
	if end > len(runes) {
		end = len(runes)
	}
	insert := []rune(text)
	runes = append(runes[:start], append(insert, runes[end:]...)...)
	s.Query = string(runes)
	s.Cursor = start + len(insert)
	sharedKillRing.updateYankLength(len(insert))
	s.refresh(history)
}

func (s *ReverseSearchState) clampCursor() {
	runes := []rune(s.Query)
	if s.Cursor < 0 {
		s.Cursor = 0
	}
	if s.Cursor > len(runes) {
		s.Cursor = len(runes)
	}
}

func (s *ReverseSearchState) refresh(history []session.HistoryEntry) {
	s.ResultEntries = FilterHistoryEntriesForReverseSearch(history, s.Query, DefaultReverseSearchLimit)
	s.Results = reverseSearchEntryDisplays(s.ResultEntries)
	if s.Focused >= len(s.Results) {
		s.Focused = len(s.Results) - 1
	}
	if s.Focused < 0 {
		s.Focused = 0
	}
	s.clampCursor()
}

func reverseSearchEntriesFromStrings(history []string) []session.HistoryEntry {
	entries := make([]session.HistoryEntry, 0, len(history))
	for _, display := range history {
		entries = append(entries, session.HistoryEntry{Display: display})
	}
	return entries
}

func reverseSearchEntryDisplays(entries []session.HistoryEntry) []string {
	displays := make([]string, 0, len(entries))
	for _, entry := range entries {
		displays = append(displays, entry.Display)
	}
	return displays
}

func reverseSearchWordStart(runes []rune, end int) int {
	if end > len(runes) {
		end = len(runes)
	}
	i := end
	for i > 0 && unicode.IsSpace(runes[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(runes[i-1]) {
		i--
	}
	return i
}

func reverseSearchWordForward(runes []rune, start int) int {
	if start < 0 {
		start = 0
	}
	if start > len(runes) {
		start = len(runes)
	}
	i := start
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}
	for i < len(runes) && !unicode.IsSpace(runes[i]) {
		i++
	}
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}
	return i
}
