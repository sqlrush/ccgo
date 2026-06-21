package bashtools

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"ccgo/internal/tool"
)

// MetadataBashCWDKey injects a session-scoped *CWDState so `cd` persists across
// Bash calls, emulating CC's persistent shell session.
const MetadataBashCWDKey = "ccgo.tools.bash.cwd"

// CWDState holds the current working directory for a bash session.
// It is thread-safe: multiple concurrent bash calls may read/write it.
type CWDState struct {
	mu  sync.RWMutex
	dir string
}

// NewCWDState creates a new CWDState seeded with the given initial directory.
func NewCWDState(initial string) *CWDState {
	return &CWDState{dir: initial}
}

// Get returns the current persisted directory.
func (s *CWDState) Get() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dir
}

// Set updates the persisted directory. Empty strings are ignored.
func (s *CWDState) Set(dir string) {
	if s == nil || strings.TrimSpace(dir) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dir = dir
}

// bashCWDState extracts the *CWDState from ctx.Metadata, or nil if absent.
func bashCWDState(ctx tool.Context) *CWDState {
	if ctx.Metadata == nil {
		return nil
	}
	state, _ := ctx.Metadata[MetadataBashCWDKey].(*CWDState)
	return state
}

// bashEffectiveCWD returns the persisted cwd when a CWDState is present and
// non-empty; otherwise falls back to ctx.WorkingDirectory.
func bashEffectiveCWD(ctx tool.Context) string {
	if state := bashCWDState(ctx); state != nil {
		if dir := state.Get(); dir != "" {
			return dir
		}
	}
	return ctx.WorkingDirectory
}

// updateBashCWD detects a leading standalone "cd <dir>" in command and, if
// the target directory exists on disk, updates the persisted cwd state.
//
// Best-effort: covers the common cases:
//   - cd /abs/path
//   - cd relative/path
//   - cd (no arg) — home dir, no-op when HOME unknown
//   - cd - — ignored (no-op)
//   - "cd foo && ..." — only a standalone "cd foo" without && chains is tracked
//     (compound commands with pipes/semicolons are not tracked to avoid guessing)
//   - Quoted paths: cd "my dir" or cd 'my dir'
//
// Non-existent targets are silently ignored (cwd unchanged).
func updateBashCWD(ctx tool.Context, command string) {
	state := bashCWDState(ctx)
	if state == nil {
		return
	}
	target, ok := extractCDTarget(command)
	if !ok {
		return
	}
	// Special cases.
	if target == "-" {
		// cd - (previous dir) — we don't track history; ignore.
		return
	}
	if target == "" {
		// cd with no arg → HOME.
		home := os.Getenv("HOME")
		if home == "" {
			return
		}
		target = home
	}
	// Resolve relative paths against the current effective cwd.
	if !filepath.IsAbs(target) {
		target = filepath.Join(bashEffectiveCWD(ctx), target)
	}
	target = filepath.Clean(target)
	// Only update if the directory actually exists.
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		return
	}
	state.Set(target)
}

// extractCDTarget parses a command string and returns the argument to "cd" if
// the command is a standalone/leading "cd" call.  Returns ("", false) when the
// command is not a simple cd invocation.
func extractCDTarget(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", false
	}

	// Strip a leading "cd <arg>" prefix, but only accept the command if it is
	// either exactly "cd [arg]" or "cd [arg] &&" (the remainder is the
	// subsequent command, which we don't try to track).
	//
	// We do NOT track: "echo foo; cd bar", "foo | cd bar", "foo && cd bar" etc.
	if !strings.HasPrefix(command, "cd") {
		return "", false
	}
	rest := command[2:] // after "cd"
	if rest == "" {
		// bare "cd"
		return "", true
	}
	// Must be followed by whitespace or end-of-command.
	if rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	rest = strings.TrimLeft(rest, " \t")

	// Accept: "cd <arg>" and "cd <arg> &&..." (ignore trailing chain).
	// Reject compound separators before the arg: ";", "|", "&" (not "&&").
	if rest == "" {
		// "cd   " → home
		return "", true
	}
	// Extract the target — handle single and double quotes.
	target, _, ok := shellWordAt(rest)
	if !ok {
		return "", false
	}
	return target, true
}

// shellWordAt extracts the first shell word from s (handles single/double
// quotes, basic escape). Returns (word, remaining, ok).
func shellWordAt(s string) (string, string, bool) {
	var b strings.Builder
	i := 0
	for i < len(s) {
		ch := s[i]
		switch {
		case ch == '\'':
			// Single-quoted string: no escaping.
			i++
			for i < len(s) && s[i] != '\'' {
				b.WriteByte(s[i])
				i++
			}
			if i >= len(s) {
				return "", "", false // unterminated
			}
			i++ // closing '
		case ch == '"':
			// Double-quoted string: only backslash-escape matters.
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
					b.WriteByte(s[i])
				} else {
					b.WriteByte(s[i])
				}
				i++
			}
			if i >= len(s) {
				return "", "", false // unterminated
			}
			i++ // closing "
		case ch == '\\':
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				i++
			} else {
				i++
			}
		case unicode.IsSpace(rune(ch)):
			// End of word.
			return b.String(), s[i:], true
		default:
			b.WriteByte(ch)
			i++
		}
	}
	return b.String(), "", true
}
