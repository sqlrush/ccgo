package tui

import (
	"strings"
)

const DefaultReverseSearchLimit = 20

func NewReverseSearchState(history []string, query string) ReverseSearchState {
	state := ReverseSearchState{Active: true, Query: query}
	state.Results = FilterHistoryForReverseSearch(history, query, DefaultReverseSearchLimit)
	return state
}

func FilterHistoryForReverseSearch(history []string, query string, limit int) []string {
	if limit <= 0 {
		limit = DefaultReverseSearchLimit
	}
	query = strings.ToLower(strings.TrimSpace(query))
	seen := map[string]struct{}{}
	out := make([]string, 0, limit)
	for i := len(history) - 1; i >= 0; i-- {
		item := strings.TrimSpace(history[i])
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
		out = append(out, item)
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

func (s *ReverseSearchState) AppendRune(history []string, r rune) {
	s.Query += string(r)
	s.refresh(history)
}

func (s *ReverseSearchState) Backspace(history []string) {
	runes := []rune(s.Query)
	if len(runes) == 0 {
		return
	}
	s.Query = string(runes[:len(runes)-1])
	s.refresh(history)
}

func (s *ReverseSearchState) refresh(history []string) {
	s.Results = FilterHistoryForReverseSearch(history, s.Query, DefaultReverseSearchLimit)
	if s.Focused >= len(s.Results) {
		s.Focused = len(s.Results) - 1
	}
	if s.Focused < 0 {
		s.Focused = 0
	}
}
