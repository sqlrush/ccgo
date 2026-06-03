package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	DefaultMaxMemoryFiles       = 200
	DefaultFrontmatterMaxLines  = 30
	DefaultClaudeMemoryFilename = "CLAUDE.md"
)

func ScanDirectory(root string, options ScanOptions) ([]Header, error) {
	if options.MaxFiles <= 0 {
		options.MaxFiles = DefaultMaxMemoryFiles
	}
	if options.FrontmatterMaxLines <= 0 {
		options.FrontmatterMaxLines = DefaultFrontmatterMaxLines
	}

	var headers []Header
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == ".claude" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		if !options.IncludeMemoryDotFile && entry.Name() == "MEMORY.md" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		frontmatter, _ := ParseFrontmatter(firstLines(string(data), options.FrontmatterMaxLines))
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = entry.Name()
		}
		headers = append(headers, Header{
			Filename:    filepath.ToSlash(rel),
			Path:        path,
			Mtime:       info.ModTime(),
			Description: frontmatter["description"],
			Type:        ParseType(frontmatter["type"]),
		})
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	sort.SliceStable(headers, func(i, j int) bool {
		return headers[i].Mtime.After(headers[j].Mtime)
	})
	if len(headers) > options.MaxFiles {
		headers = headers[:options.MaxFiles]
	}
	return headers, nil
}

func FormatManifest(headers []Header) string {
	var lines []string
	for _, header := range headers {
		prefix := ""
		if header.Type != "" {
			prefix = fmt.Sprintf("[%s] ", header.Type)
		}
		ts := header.Mtime.UTC().Format("2006-01-02T15:04:05.000Z")
		if header.Description != "" {
			lines = append(lines, fmt.Sprintf("- %s%s (%s): %s", prefix, header.Filename, ts, header.Description))
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s%s (%s)", prefix, header.Filename, ts))
	}
	return strings.Join(lines, "\n")
}

func ReadDocuments(headers []Header, maxBytes int64) ([]Document, error) {
	return ReadDocumentsWithOptions(headers, ReadOptions{MaxBytes: maxBytes})
}

func ReadDocumentsWithOptions(headers []Header, options ReadOptions) ([]Document, error) {
	var docs []Document
	now := options.Now
	for _, header := range headers {
		info, err := os.Stat(header.Path)
		if err != nil || info.IsDir() {
			continue
		}
		if options.MaxBytes > 0 && info.Size() > options.MaxBytes {
			continue
		}
		data, err := os.ReadFile(header.Path)
		if err != nil {
			continue
		}
		_, body := ParseFrontmatter(string(data))
		if body == "" && len(data) > 0 {
			body = string(data)
		}
		if options.IncludeFreshnessNote {
			body = MemoryFreshnessNote(header.Mtime, now) + body
		}
		docs = append(docs, Document{Header: header, Content: body})
	}
	return docs, nil
}
