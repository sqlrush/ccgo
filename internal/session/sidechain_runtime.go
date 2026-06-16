package session

import (
	"fmt"
	"os"
	"time"

	"ccgo/internal/contracts"
)

const (
	SidechainStatusUnknown   = "unknown"
	SidechainStatusRunning   = "running"
	SidechainStatusCompleted = "completed"
	SidechainStatusCancelled = "cancelled"
	SidechainStatusFailed    = "failed"
)

type SidechainRuntime struct {
	SessionPath string
	SessionID   contracts.ID
}

type SidechainRun struct {
	ID           string
	SessionID    contracts.ID
	Path         string
	MetadataPath string
	Subdir       string
	ParentUUID   *contracts.ID
	Status       string
	StartedAt    string
	EndedAt      string
	Metadata     SidechainMetadata
}

type SidechainOptions struct {
	ID           string
	Subdir       string
	ParentUUID   *contracts.ID
	StartedAt    time.Time
	AgentType    string
	WorktreePath string
	Description  string
	AgentPath    string
	AgentPrompt  string
}

func (r SidechainRuntime) Start(options SidechainOptions) (SidechainRun, error) {
	if r.SessionPath == "" || r.SessionID == "" {
		return SidechainRun{}, os.ErrInvalid
	}
	id := sanitizeSidechainID(options.ID)
	if id == "" {
		id = string(contracts.NewID())
	}
	if state, err := FindSidechainState(r.SessionPath, r.SessionID, id); err != nil {
		return SidechainRun{}, err
	} else if state.MessageCount > 0 && state.Status == SidechainStatusRunning {
		return SidechainRun{}, fmt.Errorf("sidechain %s is already running", state.ID)
	}
	startedAt := options.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	run := SidechainRun{
		ID:           id,
		SessionID:    r.SessionID,
		Path:         SidechainTranscriptPathWithSubdir(r.SessionPath, r.SessionID, id, options.Subdir),
		MetadataPath: SidechainMetadataPathWithSubdir(r.SessionPath, r.SessionID, id, options.Subdir),
		Subdir:       sanitizeSidechainSubdir(options.Subdir),
		ParentUUID:   options.ParentUUID,
		Status:       SidechainStatusRunning,
		StartedAt:    startedAt.UTC().Format(time.RFC3339Nano),
		Metadata: SidechainMetadata{
			AgentType:    options.AgentType,
			WorktreePath: options.WorktreePath,
			Description:  options.Description,
			AgentPath:    options.AgentPath,
			AgentPrompt:  options.AgentPrompt,
		},
	}
	if !run.Metadata.Empty() {
		if err := WriteSidechainMetadata(run.MetadataPath, run.Metadata); err != nil {
			return SidechainRun{}, err
		}
	}
	if err := AppendSidechainMessageInSubdir(r.SessionPath, r.SessionID, id, run.Subdir, TranscriptMessage{
		Type:        "system",
		UUID:        contracts.NewID(),
		ParentUUID:  options.ParentUUID,
		SessionID:   r.SessionID,
		Timestamp:   run.StartedAt,
		Subtype:     "sidechain_start",
		IsSidechain: true,
		Content: map[string]any{
			"sidechainId":  id,
			"agentId":      id,
			"status":       run.Status,
			"agentType":    options.AgentType,
			"worktreePath": options.WorktreePath,
			"description":  options.Description,
			"agentPath":    options.AgentPath,
			"agentPrompt":  options.AgentPrompt,
		},
	}); err != nil {
		return SidechainRun{}, err
	}
	return run, nil
}

func (r SidechainRuntime) Append(run SidechainRun, message TranscriptMessage) error {
	if run.ID == "" {
		return os.ErrInvalid
	}
	path := r.sidechainPath(run)
	if message.ParentUUID == nil {
		parent, err := latestTranscriptUUID(path)
		if err != nil {
			return err
		}
		if parent != "" {
			message.ParentUUID = &parent
		} else {
			message.ParentUUID = run.ParentUUID
		}
	}
	return AppendSidechainMessageInSubdir(r.SessionPath, r.SessionID, run.ID, run.Subdir, message)
}

func (r SidechainRuntime) Finish(run SidechainRun, status string, summary string, endedAt time.Time) (TranscriptMessage, error) {
	if run.ID == "" {
		return TranscriptMessage{}, os.ErrInvalid
	}
	status = normalizeSidechainStatus(status)
	if status == "" {
		status = SidechainStatusCompleted
	}
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}
	parent := run.ParentUUID
	if latest, err := latestTranscriptUUID(r.sidechainPath(run)); err != nil {
		return TranscriptMessage{}, err
	} else if latest != "" {
		parent = &latest
	}
	message := TranscriptMessage{
		Type:        "system",
		UUID:        contracts.NewID(),
		ParentUUID:  parent,
		SessionID:   r.SessionID,
		Timestamp:   endedAt.UTC().Format(time.RFC3339Nano),
		Subtype:     "sidechain_summary",
		IsSidechain: true,
		AgentID:     run.ID,
		Content: map[string]any{
			"sidechainId": run.ID,
			"agentId":     run.ID,
			"status":      status,
			"summary":     summary,
		},
	}
	if err := AppendSidechainMessageInSubdir(r.SessionPath, r.SessionID, run.ID, run.Subdir, message); err != nil {
		return TranscriptMessage{}, err
	}
	if err := AppendTranscriptMessage(r.SessionPath, message); err != nil {
		return TranscriptMessage{}, err
	}
	return message, nil
}

func (r SidechainRuntime) sidechainPath(run SidechainRun) string {
	if run.Path != "" {
		return run.Path
	}
	return SidechainTranscriptPathWithSubdir(r.SessionPath, r.SessionID, run.ID, run.Subdir)
}

func latestTranscriptUUID(path string) (contracts.ID, error) {
	if path == "" {
		return "", nil
	}
	transcript, err := LoadTranscript(path)
	if err != nil {
		return "", err
	}
	if len(transcript.Order) == 0 {
		return "", nil
	}
	return transcript.Order[len(transcript.Order)-1], nil
}
