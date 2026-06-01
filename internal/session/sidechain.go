package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ccgo/internal/contracts"
)

type SidechainInfo struct {
	ID           string
	Path         string
	MetadataPath string
	Subdir       string
	Legacy       bool
}

func SidechainDir(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), "subagents")
}

func LegacySidechainDir(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), "sidechains")
}

func SidechainTranscriptPath(sessionPath string, sessionID contracts.ID, sidechainID string) string {
	return SidechainTranscriptPathWithSubdir(sessionPath, sessionID, sidechainID, "")
}

func SidechainTranscriptPathWithSubdir(sessionPath string, sessionID contracts.ID, sidechainID string, subdir string) string {
	dir := SidechainDir(sessionPath, sessionID)
	if dir == "" {
		return ""
	}
	if cleanSubdir := sanitizeSidechainSubdir(subdir); cleanSubdir != "" {
		dir = filepath.Join(dir, cleanSubdir)
	}
	id := sanitizeSidechainID(sidechainID)
	if id == "" {
		id = "sidechain"
	}
	return filepath.Join(dir, "agent-"+id+".jsonl")
}

func SidechainMetadataPath(sessionPath string, sessionID contracts.ID, sidechainID string) string {
	return SidechainMetadataPathWithSubdir(sessionPath, sessionID, sidechainID, "")
}

func SidechainMetadataPathWithSubdir(sessionPath string, sessionID contracts.ID, sidechainID string, subdir string) string {
	path := SidechainTranscriptPathWithSubdir(sessionPath, sessionID, sidechainID, subdir)
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(path, ".jsonl") + ".meta.json"
}

func legacySidechainTranscriptPath(sessionPath string, sessionID contracts.ID, sidechainID string) string {
	dir := LegacySidechainDir(sessionPath, sessionID)
	if dir == "" {
		return ""
	}
	id := sanitizeSidechainID(sidechainID)
	if id == "" {
		id = "sidechain"
	}
	return filepath.Join(dir, id+".jsonl")
}

func AppendSidechainMessage(sessionPath string, sessionID contracts.ID, sidechainID string, message TranscriptMessage) error {
	return AppendSidechainMessageInSubdir(sessionPath, sessionID, sidechainID, "", message)
}

func AppendSidechainMessageInSubdir(sessionPath string, sessionID contracts.ID, sidechainID string, subdir string, message TranscriptMessage) error {
	id := sanitizeSidechainID(sidechainID)
	if id == "" {
		id = "sidechain"
	}
	path := SidechainTranscriptPathWithSubdir(sessionPath, sessionID, id, subdir)
	if path == "" {
		return os.ErrInvalid
	}
	message.SessionID = sessionID
	message.IsSidechain = true
	message.AgentID = id
	return AppendTranscriptMessage(path, message)
}

func ListSidechainTranscripts(sessionPath string, sessionID contracts.ID) ([]SidechainInfo, error) {
	dir := SidechainDir(sessionPath, sessionID)
	if dir == "" && LegacySidechainDir(sessionPath, sessionID) == "" {
		return nil, nil
	}
	var out []SidechainInfo
	if dir != "" {
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
				return nil
			}
			name := strings.TrimSuffix(entry.Name(), ".jsonl")
			if !strings.HasPrefix(name, "agent-") {
				return nil
			}
			id := strings.TrimPrefix(name, "agent-")
			rel, err := filepath.Rel(dir, filepath.Dir(path))
			if err != nil || rel == "." {
				rel = ""
			}
			out = append(out, SidechainInfo{
				ID:           id,
				Path:         path,
				MetadataPath: strings.TrimSuffix(path, ".jsonl") + ".meta.json",
				Subdir:       rel,
			})
			return nil
		})
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	legacyDir := LegacySidechainDir(sessionPath, sessionID)
	if legacyDir != "" {
		entries, err := os.ReadDir(legacyDir)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".jsonl")
			path := filepath.Join(legacyDir, entry.Name())
			out = append(out, SidechainInfo{ID: id, Path: path, MetadataPath: strings.TrimSuffix(path, ".jsonl") + ".meta.json", Legacy: true})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].Path < out[j].Path
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func sanitizeSidechainID(id string) string {
	id = strings.TrimSpace(id)
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\x00", "")
	return replacer.Replace(id)
}

func sanitizeSidechainSubdir(subdir string) string {
	subdir = strings.TrimSpace(strings.ReplaceAll(subdir, "\\", "/"))
	if subdir == "" {
		return ""
	}
	parts := strings.Split(subdir, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		part = sanitizeSidechainID(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		cleaned = append(cleaned, part)
	}
	if len(cleaned) == 0 {
		return ""
	}
	return filepath.Join(cleaned...)
}
