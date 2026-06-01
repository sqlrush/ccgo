package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Audit struct {
	SourceRoot           string          `json:"source_root"`
	SrcBytes             int64           `json:"src_bytes"`
	AssetsBytes          int64           `json:"assets_bytes,omitempty"`
	SourceFiles          int             `json:"source_files"`
	SourceLines          int             `json:"source_lines"`
	ExtensionCounts      map[string]int  `json:"extension_counts"`
	TopLevelCounts       map[string]int  `json:"top_level_counts"`
	InlineSourceMapFiles int             `json:"inline_source_map_files"`
	MissingImports       []MissingImport `json:"missing_imports"`
	FeatureGates         map[string]int  `json:"feature_gates"`
	EnvironmentKeys      map[string]int  `json:"environment_keys"`
	LargestFiles         []FileMetric    `json:"largest_files"`
	LongestFiles         []FileMetric    `json:"longest_files"`
}

type MissingImport struct {
	Target     string   `json:"target"`
	References []string `json:"references"`
}

type FileMetric struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes,omitempty"`
	Lines int    `json:"lines,omitempty"`
}

var (
	importRE  = regexp.MustCompile("(?:import|export)\\s+(?:type\\s+)?(?:[^\"'`;]*?\\s+from\\s+)?[\"']([^\"']+)[\"']|import\\([\"']([^\"']+)[\"']\\)|require\\([\"']([^\"']+)[\"']\\)")
	featureRE = regexp.MustCompile("feature\\([\"']([^\"']+)[\"']\\)")
	envRE     = regexp.MustCompile("\\b(?:CLAUDE_CODE|ANTHROPIC|MCP|ENABLE|DISABLE|VOICE_STREAM|CLAUDE)_[A-Z0-9_]+\\b|\\bUSER_TYPE\\b|\\bNODE_ENV\\b")
)

func AuditSource(root string) (Audit, error) {
	info, err := os.Stat(root)
	if err != nil {
		return Audit{}, err
	}
	if !info.IsDir() {
		return Audit{}, errors.New("source root is not a directory")
	}

	a := Audit{
		SourceRoot:      root,
		ExtensionCounts: map[string]int{},
		TopLevelCounts:  map[string]int{},
		FeatureGates:    map[string]int{},
		EnvironmentKeys: map[string]int{},
	}

	srcRoot := filepath.Join(root, "src")
	missing := map[string]map[string]bool{}

	err = filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".ts" && ext != ".tsx" && ext != ".js" {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		stat, err := d.Info()
		if err != nil {
			return err
		}

		a.SourceFiles++
		a.SrcBytes += stat.Size()
		a.SourceLines += countLines(data)
		a.ExtensionCounts[strings.TrimPrefix(ext, ".")]++
		a.TopLevelCounts[topLevel(rel)]++
		if bytes.Contains(data, []byte("sourceMappingURL=data")) {
			a.InlineSourceMapFiles++
		}
		a.LargestFiles = append(a.LargestFiles, FileMetric{Path: rel, Bytes: stat.Size()})
		a.LongestFiles = append(a.LongestFiles, FileMetric{Path: rel, Lines: countLines(data)})

		for _, m := range importRE.FindAllSubmatch(data, -1) {
			spec := firstSubmatch(m[1], m[2], m[3])
			target, local := resolveImportTarget(root, path, string(spec))
			if !local {
				continue
			}
			if !targetExists(target) {
				key := strings.TrimSuffix(relPath(root, target), ".js")
				if missing[key] == nil {
					missing[key] = map[string]bool{}
				}
				missing[key][rel] = true
			}
		}
		for _, m := range featureRE.FindAllSubmatch(data, -1) {
			a.FeatureGates[string(m[1])]++
		}
		for _, m := range envRE.FindAll(data, -1) {
			a.EnvironmentKeys[string(m)]++
		}
		return nil
	})
	if err != nil {
		return Audit{}, err
	}

	a.AssetsBytes = dirSize(filepath.Join(root, "assets"))
	a.MissingImports = flattenMissing(missing)
	sortFileMetrics(a.LargestFiles, "bytes")
	sortFileMetrics(a.LongestFiles, "lines")
	a.LargestFiles = trimMetrics(a.LargestFiles, 40)
	a.LongestFiles = trimMetrics(a.LongestFiles, 40)
	return a, nil
}

func WriteAuditJSON(a Audit, path string) error {
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(append(data, '\n'))
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func firstSubmatch(items ...[]byte) []byte {
	for _, item := range items {
		if len(item) > 0 {
			return item
		}
	}
	return nil
}

func resolveImportTarget(root, file, spec string) (string, bool) {
	if strings.HasPrefix(spec, ".") {
		return filepath.Clean(filepath.Join(filepath.Dir(file), spec)), true
	}
	if strings.HasPrefix(spec, "src/") {
		return filepath.Clean(filepath.Join(root, spec)), true
	}
	return "", false
}

func targetExists(base string) bool {
	candidates := []string{base}
	stripped := strings.TrimSuffix(base, ".js")
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".json"} {
		candidates = append(candidates, stripped+ext)
	}
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
		candidates = append(candidates, filepath.Join(base, "index"+ext))
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}

func topLevel(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return rel
	}
	return parts[1]
}

func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func flattenMissing(missing map[string]map[string]bool) []MissingImport {
	out := make([]MissingImport, 0, len(missing))
	for target, refs := range missing {
		references := make([]string, 0, len(refs))
		for ref := range refs {
			references = append(references, ref)
		}
		sort.Strings(references)
		out = append(out, MissingImport{Target: target, References: references})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].References) == len(out[j].References) {
			return out[i].Target < out[j].Target
		}
		return len(out[i].References) > len(out[j].References)
	})
	return out
}

func sortFileMetrics(metrics []FileMetric, kind string) {
	sort.Slice(metrics, func(i, j int) bool {
		if kind == "bytes" {
			return metrics[i].Bytes > metrics[j].Bytes
		}
		return metrics[i].Lines > metrics[j].Lines
	})
}

func trimMetrics(in []FileMetric, n int) []FileMetric {
	if len(in) <= n {
		return in
	}
	return in[:n]
}
