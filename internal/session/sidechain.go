package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ccgo/internal/contracts"
)

type SidechainInfo struct {
	ID   string
	Path string
}

func SidechainDir(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), "sidechains")
}

func SidechainTranscriptPath(sessionPath string, sessionID contracts.ID, sidechainID string) string {
	dir := SidechainDir(sessionPath, sessionID)
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
	path := SidechainTranscriptPath(sessionPath, sessionID, sidechainID)
	if path == "" {
		return os.ErrInvalid
	}
	message.SessionID = sessionID
	message.IsSidechain = true
	return AppendTranscriptMessage(path, message)
}

func ListSidechainTranscripts(sessionPath string, sessionID contracts.ID) ([]SidechainInfo, error) {
	dir := SidechainDir(sessionPath, sessionID)
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []SidechainInfo
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		out = append(out, SidechainInfo{ID: id, Path: filepath.Join(dir, entry.Name())})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func sanitizeSidechainID(id string) string {
	id = strings.TrimSpace(id)
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\x00", "")
	return replacer.Replace(id)
}
