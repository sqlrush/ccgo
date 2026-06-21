package memory

import (
	"os"
	"path/filepath"
	"sort"
)

// Scope identifies which configuration layer a CLAUDE.md file belongs to.
type Scope string

const (
	ScopeManaged Scope = "managed"
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopeLocal   Scope = "local"
)

// Display labels mirror CC (claudemd.ts:1170-1177).
const (
	labelProject = " (project instructions, checked into the codebase)"
	labelLocal   = " (user's private project instructions, not checked in)"
	labelUser    = " (user's private global instructions for all projects)"
	labelManaged = " (managed policy instructions for all projects)"
)

const localClaudeFilename = "CLAUDE.local.md"

// ScopeOptions injects every base directory so tests never read real paths.
// All fields are optional: empty strings mean the corresponding scope is skipped.
type ScopeOptions struct {
	CWD        string
	ManagedDir string
	UserDir    string
}

// DiscoverScopedClaudeFiles returns CLAUDE.md sources in lowest→highest
// precedence order so callers can merge later entries over earlier ones.
//
// Precedence order:
//  1. Managed CLAUDE.md
//  2. Managed .claude/rules/*.md (sorted)
//  3. User CLAUDE.md
//  4. User rules/*.md (sorted)
//  5. For each dir root→cwd: CLAUDE.md, .claude/CLAUDE.md, .claude/rules/*.md (sorted)
//  6. For each dir root→cwd: CLAUDE.local.md
func DiscoverScopedClaudeFiles(opts ScopeOptions) ([]ClaudeFile, error) {
	if opts.CWD == "" {
		var err error
		if opts.CWD, err = os.Getwd(); err != nil {
			return nil, err
		}
	}
	cwd, err := filepath.Abs(opts.CWD)
	if err != nil {
		return nil, err
	}

	var out []ClaudeFile

	// add appends path if it exists and is a regular file.
	add := func(path string, scope Scope, label string) {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return
		}
		out = append(out, ClaudeFile{
			Path:  path,
			Root:  filepath.Dir(path),
			Scope: scope,
			Label: label,
		})
	}

	// addRules appends all *.md files in dir, sorted by name.
	addRules := func(dir string, scope Scope, label string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		var names []string
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			p := filepath.Join(dir, n)
			out = append(out, ClaudeFile{
				Path:  p,
				Root:  dir,
				Scope: scope,
				Label: label,
			})
		}
	}

	// 1 & 2. Managed (lowest precedence).
	if opts.ManagedDir != "" {
		add(filepath.Join(opts.ManagedDir, DefaultClaudeMemoryFilename), ScopeManaged, labelManaged)
		addRules(filepath.Join(opts.ManagedDir, ".claude", "rules"), ScopeManaged, labelManaged)
	}

	// 3 & 4. User.
	if opts.UserDir != "" {
		add(filepath.Join(opts.UserDir, DefaultClaudeMemoryFilename), ScopeUser, labelUser)
		addRules(filepath.Join(opts.UserDir, "rules"), ScopeUser, labelUser)
	}

	// Build ancestor chain root→cwd (filesystem root first).
	dirs := ancestorDirsRootFirst(cwd)

	// 5. Project: each dir's CLAUDE.md, .claude/CLAUDE.md, .claude/rules/*.md.
	for _, dir := range dirs {
		add(filepath.Join(dir, DefaultClaudeMemoryFilename), ScopeProject, labelProject)
		add(filepath.Join(dir, ".claude", DefaultClaudeMemoryFilename), ScopeProject, labelProject)
		addRules(filepath.Join(dir, ".claude", "rules"), ScopeProject, labelProject)
	}

	// 6. Local: each dir's CLAUDE.local.md (highest precedence).
	for _, dir := range dirs {
		add(filepath.Join(dir, localClaudeFilename), ScopeLocal, labelLocal)
	}

	return out, nil
}

// ancestorDirsRootFirst returns all directories from the filesystem root down
// to (and including) cwd, in root-first order.
func ancestorDirsRootFirst(cwd string) []string {
	var dirs []string
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	// Reverse so root is first.
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}

// DefaultScopeOptions reads the real platform paths. This is the ONLY place
// the global path helpers are called, keeping DiscoverScopedClaudeFiles
// fully injectable for tests.
func DefaultScopeOptions(cwd string) ScopeOptions {
	return ScopeOptions{
		CWD:        cwd,
		ManagedDir: defaultManagedDir(),
		UserDir:    defaultUserDir(),
	}
}
