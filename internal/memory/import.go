package memory

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultMaxImportDepth = 5

// importPattern mirrors CC's claudemd.ts — @ at start-of-line or after
// whitespace, capturing a path that may contain escaped spaces.
// Regex: /(?:^|\s)@((?:[^\s\\]|\\ )+)/g
var importPattern = regexp.MustCompile(`(?:^|\s)@((?:[^\s\\]|\\ )+)`)

// fencePattern detects the start/end of fenced code blocks.
var fencePattern = regexp.MustCompile("^\\s*```")

// ImportOptions controls how ResolveImports behaves.
type ImportOptions struct {
	BaseDir       string // dir of the importing file (relative-path root)
	HomeDir       string // expansion root for ~/ (empty => os.UserHomeDir)
	MaxDepth      int    // max recursion depth; 0 means defaultMaxImportDepth (5)
	AllowExternal bool   // when false, imports outside AllowedRoot are rejected
	AllowedRoot   string // imports must stay within this root unless AllowExternal
}

// extractImports returns raw import targets in source order, skipping fenced
// code blocks and inline code spans. It does NOT resolve or validate paths.
func extractImports(content string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		if fencePattern.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		// Strip inline code spans so @paths inside backticks are ignored.
		processed := stripInlineCode(line)
		for _, m := range importPattern.FindAllStringSubmatch(processed, -1) {
			target := m[1]
			// Unescape escaped spaces (CC: path.replace(/\\ /g, ' '))
			target = strings.ReplaceAll(target, `\ `, " ")
			// Strip fragment identifiers (CC: path.substring(0, hashIndex))
			if i := strings.IndexByte(target, '#'); i >= 0 {
				target = target[:i]
			}
			if target == "" {
				continue
			}
			if isImportTarget(target) {
				out = append(out, target)
			}
		}
	}
	return out
}

// stripInlineCode removes inline code spans (` ... `) from a line, replacing
// each span with a single space so surrounding tokens remain separated.
func stripInlineCode(line string) string {
	for {
		i := strings.IndexByte(line, '`')
		if i < 0 {
			return line
		}
		j := strings.IndexByte(line[i+1:], '`')
		if j < 0 {
			// Unclosed backtick: drop everything from here onward.
			return line[:i]
		}
		line = line[:i] + " " + line[i+1+j+1:]
	}
}

// isImportTarget mirrors CC's isValidPath check:
//
//	path.startsWith('./') || path.startsWith('~/') ||
//	(path.startsWith('/') && path !== '/') ||
//	(!path.startsWith('@') && !path.match(/^[#%^&*()]+/) &&
//	 path.match(/^[a-zA-Z0-9._-]/))
func isImportTarget(p string) bool {
	if p == "" || p == "/" {
		return false
	}
	switch {
	case strings.HasPrefix(p, "./"), strings.HasPrefix(p, "~/"), strings.HasPrefix(p, "/"):
		return true
	}
	c := p[0]
	return c != '@' &&
		(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '-')
}

// ResolveImports returns imported documents (recursively) ordered before the
// host doc, de-duped, cycle-safe, depth-capped, and path-validated.
//
// CC order: imports are prepended (placed before the host doc), depth-first.
func ResolveImports(doc Document, opts ImportOptions) ([]Document, error) {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxImportDepth
	}
	// Mark the host document's own path as visited so it cannot be re-imported.
	visited := make(map[string]bool)
	if doc.Path != "" {
		if abs, err := filepath.Abs(doc.Path); err == nil {
			visited[abs] = true
		}
	}
	var out []Document
	if err := resolveInto(doc.Content, opts, 0, visited, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// resolveInto recursively collects imported Documents into out, depth-first,
// so that each import appears before its parent (CC ordering).
func resolveInto(content string, opts ImportOptions, depth int, visited map[string]bool, out *[]Document) error {
	if depth >= opts.MaxDepth {
		return nil // depth cap: stop without error
	}
	for _, target := range extractImports(content) {
		abs, ok := resolveImportPath(target, opts)
		if !ok {
			continue // path rejected (traversal outside AllowedRoot, or bad ~)
		}
		if visited[abs] {
			continue // cycle or duplicate: skip
		}
		visited[abs] = true

		data, err := os.ReadFile(abs)
		if err != nil {
			continue // missing import: warn by skipping (matches CC behaviour)
		}
		body := string(data)

		// Recurse with the imported file's directory as the new BaseDir.
		childOpts := opts
		childOpts.BaseDir = filepath.Dir(abs)
		if err := resolveInto(body, childOpts, depth+1, visited, out); err != nil {
			return err
		}

		// Append after children so children appear first (CC order).
		*out = append(*out, Document{
			Header:  Header{Path: abs, Filename: filepath.Base(abs), Type: TypeProject},
			Content: body,
		})
	}
	return nil
}

// resolveImportPath converts a raw import target into an absolute path and
// validates it against AllowedRoot when AllowExternal is false.
// Returns ("", false) when the path should be skipped.
func resolveImportPath(target string, opts ImportOptions) (string, bool) {
	var raw string
	switch {
	case strings.HasPrefix(target, "~/"):
		home := opts.HomeDir
		if home == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return "", false
			}
			home = h
		}
		raw = filepath.Join(home, target[2:])
	case filepath.IsAbs(target):
		raw = target
	default:
		raw = filepath.Join(opts.BaseDir, target)
	}

	abs, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		return "", false
	}

	// Resolve symlinks so a symlink inside AllowedRoot that points outside is
	// caught before the containment check. EvalSymlinks errors on missing files;
	// treat that the same as a missing import (skip, don't crash).
	if !opts.AllowExternal {
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			// File does not exist yet or symlink is dangling — skip gracefully.
			// (If AllowExternal is true we let the ReadFile below handle it.)
			return "", false
		}
		abs = resolved
	}

	// Path traversal defence: reject paths that escape AllowedRoot when
	// AllowExternal is false.
	if !opts.AllowExternal {
		// Default AllowedRoot to BaseDir if not set (security: no traversal by default).
		root := opts.AllowedRoot
		if root == "" {
			root = opts.BaseDir
		}
		// If both are empty, reject the import (no safe default).
		if root == "" {
			return "", false
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return "", false
		}
		// Resolve symlinks on the root too so both sides of the comparison use
		// the same canonical form (critical on macOS where /var → /private/var).
		if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
			absRoot = resolved
		}
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil {
			return "", false
		}
		// rel starting with ".." means the path escapes the allowed root.
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
	}

	return abs, true
}
