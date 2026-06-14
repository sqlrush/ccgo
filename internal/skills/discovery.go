package skills

import (
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/platform"
)

func ProjectSkillDirs(cwd string) []string {
	cwd = cleanAbs(cwd)
	home := cleanAbs(userHomeDir())
	gitRoot := findGitRoot(cwd)
	var out []string
	seen := map[string]struct{}{}
	for current := cwd; ; current = filepath.Dir(current) {
		if samePath(current, home) {
			break
		}
		out = appendSkillRoots(out, seen, filepath.Join(current, ".claude", "skills"))
		if gitRoot != "" && samePath(current, gitRoot) {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return out
}

func UserSkillDirs() []string {
	var out []string
	seen := map[string]struct{}{}
	return appendSkillRoots(out, seen, filepath.Join(platform.ClaudeHomeDir(), "skills"))
}

func DiscoverSkillDirsForPaths(paths []string, cwd string) []string {
	cwd = cleanAbs(cwd)
	if cwd == "" {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = cleanAbsForCwd(path, cwd)
		if path == "" {
			continue
		}
		current := filepath.Dir(path)
		for pathInside(current, cwd) && !samePath(current, cwd) {
			out = appendSkillRoots(out, seen, filepath.Join(current, ".claude", "skills"))
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return out
}

func appendSkillRoots(out []string, seen map[string]struct{}, skillsDir string) []string {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		root := filepath.Join(skillsDir, entry.Name())
		if _, err := os.Stat(filepath.Join(root, "SKILL.md")); err != nil {
			continue
		}
		key := normalizePath(root)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, root)
	}
	return out
}

func findGitRoot(cwd string) string {
	for current := cwd; current != ""; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return ""
}

func cleanAbs(path string) string {
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func cleanAbsForCwd(path string, cwd string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func samePath(a string, b string) bool {
	return normalizePath(a) == normalizePath(b)
}

func pathInside(path string, root string) bool {
	path = normalizePath(path)
	root = normalizePath(root)
	if path == root {
		return true
	}
	if !strings.HasSuffix(root, string(filepath.Separator)) {
		root += string(filepath.Separator)
	}
	return strings.HasPrefix(path, root)
}

func normalizePath(path string) string {
	return strings.ToLower(filepath.Clean(path))
}
