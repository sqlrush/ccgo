package native

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const (
	fileIndexName       = "native-file-index.json"
	defaultMaxIndexFile = 2000
)

type FileIndexOptions struct {
	MaxFiles int
}

type FileIndex struct {
	SessionID        contracts.ID `json:"session_id,omitempty"`
	WorkingDirectory string       `json:"working_directory"`
	GeneratedAt      string       `json:"generated_at"`
	Files            []FileEntry  `json:"files,omitempty"`
	Truncated        bool         `json:"truncated,omitempty"`
}

type FileEntry struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Modified string `json:"modified,omitempty"`
}

func SessionFileIndexPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), fileIndexName)
}

func BuildFileIndex(sessionID contracts.ID, root string, opts FileIndexOptions) (FileIndex, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return FileIndex{}, os.ErrInvalid
	}
	maxFiles := opts.MaxFiles
	if maxFiles <= 0 {
		maxFiles = defaultMaxIndexFile
	}
	index := FileIndex{
		SessionID:        sessionID,
		WorkingDirectory: root,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	errStop := errors.New("native file index limit reached")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != root && shouldSkipIndexDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeType != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || strings.HasPrefix(rel, "../") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		index.Files = append(index.Files, FileEntry{
			Path:     rel,
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format(time.RFC3339Nano),
		})
		if len(index.Files) >= maxFiles {
			index.Truncated = true
			return errStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return FileIndex{}, err
	}
	sort.SliceStable(index.Files, func(i, j int) bool {
		return index.Files[i].Path < index.Files[j].Path
	})
	return index, nil
}

func WriteFileIndex(path string, index FileIndex) error {
	if path == "" {
		return os.ErrInvalid
	}
	if index.GeneratedAt == "" {
		index.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadFileIndex(path string) (FileIndex, error) {
	if path == "" {
		return FileIndex{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return FileIndex{}, nil
	}
	if err != nil {
		return FileIndex{}, err
	}
	var index FileIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return FileIndex{}, err
	}
	return index, nil
}

func shouldSkipIndexDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case ".git", ".hg", ".svn", ".jj", "node_modules", ".cache", "vendor":
		return true
	default:
		return false
	}
}
