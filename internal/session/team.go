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

type TeamState struct {
	ID          string       `json:"id"`
	SessionID   contracts.ID `json:"sessionId,omitempty"`
	Description string       `json:"description,omitempty"`
	TaskIDs     []string     `json:"taskIds,omitempty"`
	CreatedAt   string       `json:"createdAt,omitempty"`
	UpdatedAt   string       `json:"updatedAt,omitempty"`
}

type TeamManifest struct {
	SessionID contracts.ID `json:"sessionId,omitempty"`
	Teams     []TeamState  `json:"teams,omitempty"`
}

type TeamOptions struct {
	ID          string
	Description string
	TaskIDs     []string
	Timestamp   time.Time
}

func TeamManifestPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), "teams.json")
}

func LoadTeamManifest(sessionPath string, sessionID contracts.ID) (TeamManifest, error) {
	path := TeamManifestPath(sessionPath, sessionID)
	if path == "" {
		return TeamManifest{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return TeamManifest{SessionID: sessionID}, nil
	}
	if err != nil {
		return TeamManifest{}, err
	}
	var manifest TeamManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return TeamManifest{}, err
	}
	manifest.SessionID = sessionID
	for i := range manifest.Teams {
		manifest.Teams[i].SessionID = sessionID
		manifest.Teams[i].ID = sanitizeSidechainID(manifest.Teams[i].ID)
		manifest.Teams[i].Description = strings.TrimSpace(manifest.Teams[i].Description)
		manifest.Teams[i].TaskIDs = cleanTeamTaskIDs(manifest.Teams[i].TaskIDs)
	}
	sort.SliceStable(manifest.Teams, func(i, j int) bool {
		return manifest.Teams[i].ID < manifest.Teams[j].ID
	})
	return manifest, nil
}

func SaveTeamManifest(sessionPath string, manifest TeamManifest) error {
	path := TeamManifestPath(sessionPath, manifest.SessionID)
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

func CreateTeam(sessionPath string, sessionID contracts.ID, options TeamOptions) (TeamState, TeamManifest, error) {
	manifest, err := LoadTeamManifest(sessionPath, sessionID)
	if err != nil {
		return TeamState{}, TeamManifest{}, err
	}
	id := sanitizeSidechainID(options.ID)
	if id == "" {
		id = string(contracts.NewID())
	}
	now := options.Timestamp
	if now.IsZero() {
		now = time.Now().UTC()
	}
	team := TeamState{
		ID:          id,
		SessionID:   sessionID,
		Description: strings.TrimSpace(options.Description),
		TaskIDs:     cleanTeamTaskIDs(options.TaskIDs),
		CreatedAt:   now.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   now.UTC().Format(time.RFC3339Nano),
	}
	replaced := false
	for i := range manifest.Teams {
		if manifest.Teams[i].ID != id {
			continue
		}
		if manifest.Teams[i].CreatedAt != "" {
			team.CreatedAt = manifest.Teams[i].CreatedAt
		}
		manifest.Teams[i] = team
		replaced = true
		break
	}
	if !replaced {
		manifest.Teams = append(manifest.Teams, team)
	}
	sort.SliceStable(manifest.Teams, func(i, j int) bool {
		return manifest.Teams[i].ID < manifest.Teams[j].ID
	})
	if err := SaveTeamManifest(sessionPath, manifest); err != nil {
		return TeamState{}, TeamManifest{}, err
	}
	return team, manifest, nil
}

func DeleteTeam(sessionPath string, sessionID contracts.ID, teamID string) (TeamState, TeamManifest, error) {
	manifest, err := LoadTeamManifest(sessionPath, sessionID)
	if err != nil {
		return TeamState{}, TeamManifest{}, err
	}
	id := sanitizeSidechainID(teamID)
	if id == "" {
		return TeamState{}, TeamManifest{}, fmt.Errorf("team_id is required")
	}
	next := manifest.Teams[:0]
	var deleted TeamState
	found := false
	for _, team := range manifest.Teams {
		if team.ID == id {
			deleted = team
			found = true
			continue
		}
		next = append(next, team)
	}
	if !found {
		return TeamState{}, TeamManifest{}, fmt.Errorf("team not found: %s", id)
	}
	manifest.Teams = next
	if err := SaveTeamManifest(sessionPath, manifest); err != nil {
		return TeamState{}, TeamManifest{}, err
	}
	return deleted, manifest, nil
}

func cleanTeamTaskIDs(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		id := sanitizeSidechainID(value)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
