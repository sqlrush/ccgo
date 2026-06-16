package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

type RemoteTriggerReceipt struct {
	EventID         string       `json:"eventId"`
	SessionID       contracts.ID `json:"sessionId,omitempty"`
	Source          string       `json:"source,omitempty"`
	Event           string       `json:"event,omitempty"`
	TeamID          string       `json:"teamId,omitempty"`
	Target          string       `json:"target,omitempty"`
	ReceivedAt      string       `json:"receivedAt,omitempty"`
	SentCount       int          `json:"sentCount,omitempty"`
	MessageChars    int          `json:"messageChars,omitempty"`
	DuplicateCount  int          `json:"duplicateCount,omitempty"`
	LastDuplicateAt string       `json:"lastDuplicateAt,omitempty"`
}

type RemoteTriggerManifest struct {
	SessionID contracts.ID           `json:"sessionId,omitempty"`
	Receipts  []RemoteTriggerReceipt `json:"receipts,omitempty"`
}

type RemoteTriggerReceiptOptions struct {
	EventID      string
	Source       string
	Event        string
	TeamID       string
	Target       string
	SentCount    int
	MessageChars int
	Timestamp    time.Time
}

func RemoteTriggerManifestPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), "remote_triggers.json")
}

func LoadRemoteTriggerManifest(sessionPath string, sessionID contracts.ID) (RemoteTriggerManifest, error) {
	path := RemoteTriggerManifestPath(sessionPath, sessionID)
	if path == "" {
		return RemoteTriggerManifest{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return RemoteTriggerManifest{SessionID: sessionID}, nil
	}
	if err != nil {
		return RemoteTriggerManifest{}, err
	}
	var manifest RemoteTriggerManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return RemoteTriggerManifest{}, err
	}
	manifest.SessionID = sessionID
	for i := range manifest.Receipts {
		manifest.Receipts[i].SessionID = sessionID
		manifest.Receipts[i].EventID = strings.TrimSpace(manifest.Receipts[i].EventID)
		manifest.Receipts[i].Source = strings.TrimSpace(manifest.Receipts[i].Source)
		manifest.Receipts[i].Event = strings.TrimSpace(manifest.Receipts[i].Event)
		manifest.Receipts[i].TeamID = sanitizeSidechainID(manifest.Receipts[i].TeamID)
		manifest.Receipts[i].Target = strings.TrimSpace(manifest.Receipts[i].Target)
	}
	sort.SliceStable(manifest.Receipts, func(i, j int) bool {
		return manifest.Receipts[i].EventID < manifest.Receipts[j].EventID
	})
	return manifest, nil
}

func SaveRemoteTriggerManifest(sessionPath string, manifest RemoteTriggerManifest) error {
	path := RemoteTriggerManifestPath(sessionPath, manifest.SessionID)
	if path == "" {
		return os.ErrInvalid
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return platform.AtomicWriteFile(path, data, 0o644)
}

func FindRemoteTriggerReceipt(sessionPath string, sessionID contracts.ID, eventID string) (RemoteTriggerReceipt, bool, error) {
	manifest, err := LoadRemoteTriggerManifest(sessionPath, sessionID)
	if err != nil {
		return RemoteTriggerReceipt{}, false, err
	}
	id := strings.TrimSpace(eventID)
	if id == "" {
		return RemoteTriggerReceipt{}, false, nil
	}
	for _, receipt := range manifest.Receipts {
		if receipt.EventID == id {
			return receipt, true, nil
		}
	}
	return RemoteTriggerReceipt{}, false, nil
}

func RecordRemoteTriggerReceipt(sessionPath string, sessionID contracts.ID, options RemoteTriggerReceiptOptions) (RemoteTriggerReceipt, RemoteTriggerManifest, error) {
	manifest, err := LoadRemoteTriggerManifest(sessionPath, sessionID)
	if err != nil {
		return RemoteTriggerReceipt{}, RemoteTriggerManifest{}, err
	}
	id := strings.TrimSpace(options.EventID)
	if id == "" {
		return RemoteTriggerReceipt{}, RemoteTriggerManifest{}, fmt.Errorf("event_id is required")
	}
	now := options.Timestamp
	if now.IsZero() {
		now = time.Now().UTC()
	}
	receipt := RemoteTriggerReceipt{
		EventID:      id,
		SessionID:    sessionID,
		Source:       strings.TrimSpace(options.Source),
		Event:        strings.TrimSpace(options.Event),
		TeamID:       sanitizeSidechainID(options.TeamID),
		Target:       strings.TrimSpace(options.Target),
		ReceivedAt:   now.UTC().Format(time.RFC3339Nano),
		SentCount:    options.SentCount,
		MessageChars: options.MessageChars,
	}
	for i := range manifest.Receipts {
		if manifest.Receipts[i].EventID != id {
			continue
		}
		receipt.DuplicateCount = manifest.Receipts[i].DuplicateCount
		receipt.LastDuplicateAt = manifest.Receipts[i].LastDuplicateAt
		manifest.Receipts[i] = receipt
		if err := SaveRemoteTriggerManifest(sessionPath, manifest); err != nil {
			return RemoteTriggerReceipt{}, RemoteTriggerManifest{}, err
		}
		return receipt, manifest, nil
	}
	manifest.Receipts = append(manifest.Receipts, receipt)
	sort.SliceStable(manifest.Receipts, func(i, j int) bool {
		return manifest.Receipts[i].EventID < manifest.Receipts[j].EventID
	})
	if err := SaveRemoteTriggerManifest(sessionPath, manifest); err != nil {
		return RemoteTriggerReceipt{}, RemoteTriggerManifest{}, err
	}
	return receipt, manifest, nil
}

func RecordRemoteTriggerDuplicate(sessionPath string, sessionID contracts.ID, eventID string, timestamp time.Time) (RemoteTriggerReceipt, RemoteTriggerManifest, error) {
	manifest, err := LoadRemoteTriggerManifest(sessionPath, sessionID)
	if err != nil {
		return RemoteTriggerReceipt{}, RemoteTriggerManifest{}, err
	}
	id := strings.TrimSpace(eventID)
	if id == "" {
		return RemoteTriggerReceipt{}, RemoteTriggerManifest{}, fmt.Errorf("event_id is required")
	}
	now := timestamp
	if now.IsZero() {
		now = time.Now().UTC()
	}
	for i := range manifest.Receipts {
		if manifest.Receipts[i].EventID != id {
			continue
		}
		manifest.Receipts[i].DuplicateCount++
		manifest.Receipts[i].LastDuplicateAt = now.UTC().Format(time.RFC3339Nano)
		if err := SaveRemoteTriggerManifest(sessionPath, manifest); err != nil {
			return RemoteTriggerReceipt{}, RemoteTriggerManifest{}, err
		}
		return manifest.Receipts[i], manifest, nil
	}
	return RemoteTriggerReceipt{}, RemoteTriggerManifest{}, fmt.Errorf("remote trigger receipt not found: %s", id)
}
