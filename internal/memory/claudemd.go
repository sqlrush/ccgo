package memory

import (
	"os"
	"path/filepath"
)

type ClaudeFile struct {
	Path  string
	Root  string
	Depth int
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
