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

type ScheduleState struct {
	ID          string       `json:"id"`
	SessionID   contracts.ID `json:"sessionId,omitempty"`
	Description string       `json:"description,omitempty"`
	Cron        string       `json:"cron,omitempty"`
	Message     string       `json:"message,omitempty"`
	TeamID      string       `json:"teamId,omitempty"`
	Target      string       `json:"target,omitempty"`
	Enabled     bool         `json:"enabled"`
	CreatedAt   string       `json:"createdAt,omitempty"`
	UpdatedAt   string       `json:"updatedAt,omitempty"`
}

type ScheduleManifest struct {
	SessionID contracts.ID    `json:"sessionId,omitempty"`
	Schedules []ScheduleState `json:"schedules,omitempty"`
}

type ScheduleOptions struct {
	ID          string
	Description string
	Cron        string
	Message     string
	TeamID      string
	Target      string
	Enabled     bool
	Timestamp   time.Time
}

func ScheduleManifestPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), "schedules.json")
}

func LoadScheduleManifest(sessionPath string, sessionID contracts.ID) (ScheduleManifest, error) {
	path := ScheduleManifestPath(sessionPath, sessionID)
	if path == "" {
		return ScheduleManifest{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ScheduleManifest{SessionID: sessionID}, nil
	}
	if err != nil {
		return ScheduleManifest{}, err
	}
	var manifest ScheduleManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ScheduleManifest{}, err
	}
	manifest.SessionID = sessionID
	for i := range manifest.Schedules {
		manifest.Schedules[i].SessionID = sessionID
		manifest.Schedules[i].ID = sanitizeSidechainID(manifest.Schedules[i].ID)
		manifest.Schedules[i].Description = strings.TrimSpace(manifest.Schedules[i].Description)
		manifest.Schedules[i].Cron = strings.TrimSpace(manifest.Schedules[i].Cron)
		manifest.Schedules[i].Message = strings.TrimSpace(manifest.Schedules[i].Message)
		manifest.Schedules[i].TeamID = sanitizeSidechainID(manifest.Schedules[i].TeamID)
		manifest.Schedules[i].Target = strings.TrimSpace(manifest.Schedules[i].Target)
	}
	sort.SliceStable(manifest.Schedules, func(i, j int) bool {
		return manifest.Schedules[i].ID < manifest.Schedules[j].ID
	})
	return manifest, nil
}

func SaveScheduleManifest(sessionPath string, manifest ScheduleManifest) error {
	path := ScheduleManifestPath(sessionPath, manifest.SessionID)
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

func UpsertSchedule(sessionPath string, sessionID contracts.ID, options ScheduleOptions) (ScheduleState, ScheduleManifest, error) {
	manifest, err := LoadScheduleManifest(sessionPath, sessionID)
	if err != nil {
		return ScheduleState{}, ScheduleManifest{}, err
	}
	id := sanitizeSidechainID(options.ID)
	if id == "" {
		id = string(contracts.NewID())
	}
	now := options.Timestamp
	if now.IsZero() {
		now = time.Now().UTC()
	}
	schedule := ScheduleState{
		ID:          id,
		SessionID:   sessionID,
		Description: strings.TrimSpace(options.Description),
		Cron:        strings.TrimSpace(options.Cron),
		Message:     strings.TrimSpace(options.Message),
		TeamID:      sanitizeSidechainID(options.TeamID),
		Target:      strings.TrimSpace(options.Target),
		Enabled:     options.Enabled,
		CreatedAt:   now.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   now.UTC().Format(time.RFC3339Nano),
	}
	replaced := false
	for i := range manifest.Schedules {
		if manifest.Schedules[i].ID != id {
			continue
		}
		if manifest.Schedules[i].CreatedAt != "" {
			schedule.CreatedAt = manifest.Schedules[i].CreatedAt
		}
		manifest.Schedules[i] = schedule
		replaced = true
		break
	}
	if !replaced {
		manifest.Schedules = append(manifest.Schedules, schedule)
	}
	sort.SliceStable(manifest.Schedules, func(i, j int) bool {
		return manifest.Schedules[i].ID < manifest.Schedules[j].ID
	})
	if err := SaveScheduleManifest(sessionPath, manifest); err != nil {
		return ScheduleState{}, ScheduleManifest{}, err
	}
	return schedule, manifest, nil
}

func DeleteSchedule(sessionPath string, sessionID contracts.ID, scheduleID string) (ScheduleState, ScheduleManifest, error) {
	manifest, err := LoadScheduleManifest(sessionPath, sessionID)
	if err != nil {
		return ScheduleState{}, ScheduleManifest{}, err
	}
	id := sanitizeSidechainID(scheduleID)
	if id == "" {
		return ScheduleState{}, ScheduleManifest{}, fmt.Errorf("schedule_id is required")
	}
	next := manifest.Schedules[:0]
	var deleted ScheduleState
	found := false
	for _, schedule := range manifest.Schedules {
		if schedule.ID == id {
			deleted = schedule
			found = true
			continue
		}
		next = append(next, schedule)
	}
	if !found {
		return ScheduleState{}, ScheduleManifest{}, fmt.Errorf("schedule not found: %s", id)
	}
	manifest.Schedules = next
	if err := SaveScheduleManifest(sessionPath, manifest); err != nil {
		return ScheduleState{}, ScheduleManifest{}, err
	}
	return deleted, manifest, nil
}
