package repl

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"ccgo/internal/fuzzy"
	"ccgo/internal/tui"
)

// GlobalSearchMatch is a single line match from a cross-file content search.
// Mirrors CC GlobalSearchDialog.Match.
type GlobalSearchMatch struct {
	// File is the path relative to the search root.
	File string
	// Line is the 1-based line number of the match.
	Line int
	// Text is the content of the matching line.
	Text string
}

const (
	// SearchMaxMatchesPerFile caps per-file results (mirrors CC MAX_MATCHES_PER_FILE=10).
	SearchMaxMatchesPerFile = 10
	// SearchMaxTotalMatches caps total results (mirrors CC MAX_TOTAL_MATCHES=500).
	SearchMaxTotalMatches = 500
	// searchMaxLineBytes is the maximum line length in bytes before we skip
	// the line. Binary files routinely have very long "lines".
	searchMaxLineBytes = 8192
)

// SearchOptions controls GlobalSearchFiles.
type SearchOptions struct {
	// CaseSensitive, when true, uses a case-sensitive string match.
	// When false (default) the match is case-insensitive (ToLower comparison).
	CaseSensitive bool
	// MaxMatchesPerFile caps per-file results (default: SearchMaxMatchesPerFile).
	MaxMatchesPerFile int
	// MaxTotalMatches caps total results (default: SearchMaxTotalMatches).
	MaxTotalMatches int
}

// GlobalSearchFiles performs a line-by-line substring search across all text
// files under root, skipping the same binary/ignored directories as WalkFiles.
// The search is cancellable via ctx. Empty query returns nil, false immediately.
//
// This is the pure-Go backend for OVL-08 (GlobalSearchDialog).
// CC ref: src/components/GlobalSearchDialog.tsx (ripGrepStream backend).
func GlobalSearchFiles(ctx context.Context, root, query string, opts SearchOptions) ([]GlobalSearchMatch, bool) {
	if query == "" {
		return nil, false
	}
	maxPerFile := opts.MaxMatchesPerFile
	if maxPerFile <= 0 {
		maxPerFile = SearchMaxMatchesPerFile
	}
	maxTotal := opts.MaxTotalMatches
	if maxTotal <= 0 {
		maxTotal = SearchMaxTotalMatches
	}

	needle := query
	if !opts.CaseSensitive {
		needle = strings.ToLower(query)
	}

	var results []GlobalSearchMatch
	truncated := false

	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsPermission(err) && d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// Check context cancellation.
		select {
		case <-ctx.Done():
			return fs.SkipAll
		default:
		}
		if d.IsDir() {
			if fuzzy.IsIgnoredDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		// Skip non-text files by extension.
		if fuzzy.IsIgnoredExt(filepath.Ext(p)) {
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)

		matches, err := searchFileLines(p, needle, opts.CaseSensitive, maxPerFile)
		if err != nil {
			return nil // skip unreadable files
		}
		for _, m := range matches {
			m.File = rel
			results = append(results, m)
			if len(results) >= maxTotal {
				truncated = true
				return fs.SkipAll
			}
		}
		return nil
	})

	return results, truncated
}

// searchFileLines scans the file at path for lines containing needle.
// Returns up to maxPerFile matches (1-based line numbers).
func searchFileLines(path, needle string, caseSensitive bool, maxPerFile int) ([]GlobalSearchMatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []GlobalSearchMatch
	scanner := bufio.NewScanner(f)
	// 64 KiB line buffer for long lines.
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, cap(buf))

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Bytes()
		if len(raw) > searchMaxLineBytes {
			continue // possibly binary
		}
		if !utf8.Valid(raw) {
			continue // binary content
		}
		text := string(raw)
		haystack := text
		if !caseSensitive {
			haystack = strings.ToLower(text)
		}
		if strings.Contains(haystack, needle) {
			out = append(out, GlobalSearchMatch{
				Line: lineNo,
				Text: strings.TrimRight(text, "\r\n"),
			})
			if len(out) >= maxPerFile {
				break
			}
		}
	}
	// Scan errors (e.g. line too long with buffer) — return what we have.
	return out, nil
}

// ── GlobalSearchOverlay ──────────────────────────────────────────────────────

// searchResult is the value sent from the background search goroutine to the
// overlay via resultCh. Using a channel avoids data races: only the goroutine
// writes to resultCh; ApplyKey drains it on every call (single-threaded loop).
type searchResult struct {
	results   []GlobalSearchMatch
	truncated bool
}

// GlobalSearchOverlay is the state layer for the GlobalSearch dialog (OVL-08).
// The backend GlobalSearchFiles is called on query change; results are
// delivered via a buffered channel and drained on each ApplyKey call so the
// overlay state is only ever mutated from the loop goroutine.
// Render is MANUAL — full TUI rendering is human-verified.
//
// State transitions:
//
//	rune input  → update query, run search, reset cursor
//	↑ / ↓      → move cursor through results
//	Enter       → Submit = "globalsearch:<file>:<line>"
//	Esc         → Dismissed
type GlobalSearchOverlay struct {
	root      string
	query     string
	results   []GlobalSearchMatch
	truncated bool
	cursor    int
	searching bool
	cancel    context.CancelFunc
	// resultCh receives the output of the background search goroutine.
	// Buffered (size 1) so the goroutine never blocks.
	resultCh chan searchResult
}

// NewGlobalSearchOverlay creates a GlobalSearchOverlay for the given root.
func NewGlobalSearchOverlay(root string) *GlobalSearchOverlay {
	return &GlobalSearchOverlay{
		root:     root,
		resultCh: make(chan searchResult, 1),
	}
}

// drainResults collects any pending search results from the background goroutine.
// Must be called from the loop goroutine only (single-threaded).
func (o *GlobalSearchOverlay) drainResults() {
	select {
	case r := <-o.resultCh:
		o.results = r.results
		o.truncated = r.truncated
		o.searching = false
	default:
	}
}

// ApplyKey handles a keypress for the overlay. Implements Overlay.
func (o *GlobalSearchOverlay) ApplyKey(k tui.Key) (OverlayResult, bool) {
	// Collect any results that arrived since the last key event.
	o.drainResults()
	switch k.Type {
	case tui.KeyEsc:
		o.stopSearch()
		return OverlayResult{Dismissed: true}, true

	case tui.KeyEnter:
		if len(o.results) > 0 {
			m := o.results[o.cursor]
			return OverlayResult{Submit: globalSearchSubmit(m)}, true
		}
		return OverlayResult{}, true

	case tui.KeyUp:
		if o.cursor > 0 {
			o.cursor--
		}
		return OverlayResult{}, true

	case tui.KeyDown:
		if o.cursor < len(o.results)-1 {
			o.cursor++
		}
		return OverlayResult{}, true

	case tui.KeyBackspace:
		if len(o.query) > 0 {
			runes := []rune(o.query)
			o.query = string(runes[:len(runes)-1])
			o.startSearch()
		}
		return OverlayResult{}, true

	case tui.KeyRune:
		o.query += string(k.Rune)
		o.startSearch()
		return OverlayResult{}, true
	}
	return OverlayResult{}, false
}

// Render returns a human-readable description of the overlay state.
// Full TUI rendering is MANUAL. Implements Overlay.
func (o *GlobalSearchOverlay) Render(_ int, _ int) []string {
	// Drain any pending results so the display stays fresh.
	o.drainResults()
	if o.query == "" {
		return []string{"Global search: type to search"}
	}
	status := "Global search: " + o.query
	if o.searching {
		return []string{status + " [searching…]"}
	}
	if len(o.results) == 0 {
		return []string{status + " — no results"}
	}
	suffix := ""
	if o.truncated {
		suffix = " (truncated)"
	}
	lines := []string{fmt.Sprintf("%s — %d result(s)%s", status, len(o.results), suffix)}
	for i, m := range o.results {
		prefix := "  "
		if i == o.cursor {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s:%d: %s", prefix, m.File, m.Line, m.Text))
	}
	return lines
}

// stopSearch cancels any in-progress search.
func (o *GlobalSearchOverlay) stopSearch() {
	if o.cancel != nil {
		o.cancel()
		o.cancel = nil
	}
	o.searching = false
}

// startSearch runs GlobalSearchFiles in a goroutine and delivers results via
// resultCh. The overlay fields are only mutated from the loop goroutine
// (via drainResults), so there are no data races.
func (o *GlobalSearchOverlay) startSearch() {
	o.stopSearch()
	o.cursor = 0
	if o.query == "" {
		o.results = nil
		o.truncated = false
		return
	}
	o.searching = true
	ctx, cancel := context.WithCancel(context.Background())
	o.cancel = cancel
	query := o.query
	root := o.root
	ch := o.resultCh
	go func() {
		results, trunc := GlobalSearchFiles(ctx, root, query, SearchOptions{})
		// Non-blocking send: if the channel is full (a previous result is
		// pending), discard the stale one and replace it.
		select {
		case ch <- searchResult{results: results, truncated: trunc}:
		default:
			// Drain the stale result and re-send.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- searchResult{results: results, truncated: trunc}:
			default:
			}
		}
	}()
}

// globalSearchSubmit returns the overlay submit string for a match.
func globalSearchSubmit(m GlobalSearchMatch) string {
	return fmt.Sprintf("globalsearch:%s:%d", m.File, m.Line)
}
