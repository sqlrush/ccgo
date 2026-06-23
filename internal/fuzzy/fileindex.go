package fuzzy

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// defaultIgnoredDirs lists directory names that should never be indexed.
// Mirrors typical .gitignore-ish conventions.
var defaultIgnoredDirs = map[string]bool{
	".git":         true,
	".hg":          true,
	".svn":         true,
	"node_modules": true,
	".cache":       true,
	"vendor":       true,
	".idea":        true,
	".vscode":      true,
	"__pycache__":  true,
	".mypy_cache":  true,
	".pytest_cache": true,
	"dist":         true,
	"build":        true,
	".build":       true,
	"target":       true,
}

// defaultIgnoredExts lists file extensions to skip during indexing.
var defaultIgnoredExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".bmp": true, ".ico": true, ".svg": true, ".webp": true,
	".mp3": true, ".mp4": true, ".wav": true, ".ogg": true,
	".avi": true, ".mov": true, ".mkv": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".pdf": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".a": true,
	".class": true, ".pyc": true,
}

// WalkOptions controls the file-tree walk performed by WalkFiles.
type WalkOptions struct {
	// MaxFiles caps the total number of files returned. Zero means no limit.
	MaxFiles int
	// ExtraIgnoreDirs is merged with the built-in ignore list.
	ExtraIgnoreDirs map[string]bool
}

// WalkFiles walks root and returns all non-ignored file paths relative to root.
// The returned paths use forward slashes regardless of OS.
// If root does not exist or cannot be read, an empty slice is returned (no error
// is propagated — the overlay must degrade gracefully).
func WalkFiles(root string, opts WalkOptions) []string {
	limit := opts.MaxFiles
	if limit == 0 {
		limit = 20_000
	}

	ignoreDirs := defaultIgnoredDirs
	if len(opts.ExtraIgnoreDirs) > 0 {
		// Build a merged map; do not mutate the default map.
		merged := make(map[string]bool, len(defaultIgnoredDirs)+len(opts.ExtraIgnoreDirs))
		for k, v := range defaultIgnoredDirs {
			merged[k] = v
		}
		for k, v := range opts.ExtraIgnoreDirs {
			merged[k] = v
		}
		ignoreDirs = merged
	}

	results := make([]string, 0, 256)

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable entries; keep walking.
			if os.IsPermission(err) {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
			}
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name != "." && (ignoreDirs[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		// Skip ignored extensions.
		ext := strings.ToLower(filepath.Ext(p))
		if defaultIgnoredExts[ext] {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		// Normalise to forward slashes.
		rel = filepath.ToSlash(rel)
		results = append(results, rel)
		if len(results) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return results
}

// FilterFiles is a convenience function that walks root and returns fuzzy-
// matched file paths in ranked order for query q.
func FilterFiles(root, q string, opts WalkOptions) []string {
	files := WalkFiles(root, opts)
	return Values(files, q)
}
