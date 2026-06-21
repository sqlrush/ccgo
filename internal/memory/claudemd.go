package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ClaudeFile struct {
	Path  string
	Root  string
	Depth int
	Scope Scope
	Label string
}

func DiscoverClaudeFiles(cwd string) ([]ClaudeFile, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}

	var dirs []string
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	var out []ClaudeFile
	for i := len(dirs) - 1; i >= 0; i-- {
		dir := dirs[i]
		path := filepath.Join(dir, DefaultClaudeMemoryFilename)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		out = append(out, ClaudeFile{
			Path:  path,
			Root:  dir,
			Depth: len(dirs) - 1 - i,
		})
	}
	return out, nil
}

// LoadOptions bundles the discovery and import-expansion configuration for
// LoadScopedClaudeContext.
type LoadOptions struct {
	Scope  ScopeOptions
	Import ImportOptions
}

// LoadScopedClaudeContext discovers all scoped CLAUDE.md sources in
// precedence order (T1) and expands @imports for each file (T2), placing
// imported docs immediately before their host doc. The per-file BaseDir is
// set to the host file's directory so that relative import paths resolve
// correctly, while AllowedRoot from opts.Import constrains traversal.
func LoadScopedClaudeContext(opts LoadOptions) ([]Document, error) {
	files, err := DiscoverScopedClaudeFiles(opts.Scope)
	if err != nil {
		return nil, fmt.Errorf("LoadScopedClaudeContext: discover: %w", err)
	}
	var docs []Document
	for _, f := range files {
		info, err := os.Stat(f.Path)
		if err != nil {
			// File disappeared between discovery and read — skip silently.
			continue
		}
		data, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		host := Document{
			Header: Header{
				Filename:    filepath.Base(f.Path),
				Path:        f.Path,
				Mtime:       info.ModTime(),
				Description: f.Label,
				Type:        scopeToType(f.Scope),
			},
			Content: string(data),
		}
		// Build per-file ImportOptions: BaseDir is the host file's directory
		// so relative @import paths resolve correctly. AllowedRoot defaults
		// to the host's directory if the caller did not provide one, keeping
		// traversal-protection active.
		imp := opts.Import
		imp.BaseDir = filepath.Dir(f.Path)
		if imp.AllowedRoot == "" {
			imp.AllowedRoot = filepath.Dir(f.Path)
		}
		imported, err := ResolveImports(host, imp)
		if err != nil {
			return nil, fmt.Errorf("LoadScopedClaudeContext: resolve imports for %s: %w", f.Path, err)
		}
		// Strip @import lines from the host doc so the content reflects only its
		// own prose (imported content is present via the separate imported docs).
		stripped := stripImportLines(host.Content)
		hostStripped := Document{
			Header:  host.Header,
			Content: stripped,
		}
		// Imported docs are placed immediately before the host doc (T2 contract).
		docs = append(docs, imported...)
		docs = append(docs, hostStripped)
	}
	return docs, nil
}

// stripImportLines returns content with @import directive lines removed.
// Lines that contain only an @path reference (possibly preceded by whitespace)
// are dropped; all other lines are kept. Fenced code blocks are preserved
// unchanged (their @-lines are not imports and should not be stripped).
func stripImportLines(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		if fencePattern.MatchString(line) {
			inFence = !inFence
			out = append(out, line)
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		// Keep lines that are not solely an @import directive.
		if !isImportOnlyLine(line) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// isImportOnlyLine reports whether a line consists solely of an @import
// directive (optional leading whitespace, then @path, then nothing else).
func isImportOnlyLine(line string) bool {
	targets := extractImports(line)
	if len(targets) == 0 {
		return false
	}
	// Check that the line has no significant content besides the @target.
	trimmed := strings.TrimSpace(line)
	for _, t := range targets {
		// Escape spaces back for matching.
		escaped := strings.ReplaceAll(t, " ", `\ `)
		if trimmed == "@"+escaped || trimmed == "@"+t {
			return true
		}
	}
	return false
}

// scopeToType maps a Scope to the appropriate document Type.
func scopeToType(s Scope) Type {
	switch s {
	case ScopeUser, ScopeManaged:
		return TypeUser
	default:
		return TypeProject
	}
}

func LoadClaudeContext(cwd string) ([]Document, error) {
	files, err := DiscoverClaudeFiles(cwd)
	if err != nil {
		return nil, err
	}
	var docs []Document
	for _, file := range files {
		info, err := os.Stat(file.Path)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(file.Path)
		if err != nil {
			continue
		}
		docs = append(docs, Document{
			Header: Header{
				Filename: DefaultClaudeMemoryFilename,
				Path:     file.Path,
				Mtime:    info.ModTime(),
				Type:     TypeProject,
			},
			Content: string(data),
		})
	}
	return docs, nil
}
