package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

type SidechainMetadata struct {
	AgentType             string   `json:"agentType,omitempty"`
	WorktreePath          string   `json:"worktreePath,omitempty"`
	WorktreeOwned         bool     `json:"worktreeOwned,omitempty"`
	WorktreeCleanupStatus string   `json:"worktreeCleanupStatus,omitempty"`
	WorktreeCleanupReason string   `json:"worktreeCleanupReason,omitempty"`
	WorktreeCleanupAt     string   `json:"worktreeCleanupAt,omitempty"`
	Description           string   `json:"description,omitempty"`
	AgentPath             string   `json:"agentPath,omitempty"`
	AgentPrompt           string   `json:"agentPrompt,omitempty"`
	AgentModel            string   `json:"agentModel,omitempty"`
	AgentPermissionMode   string   `json:"agentPermissionMode,omitempty"`
	AgentAllowedTools     []string `json:"agentAllowedTools,omitempty"`
}

func (m *SidechainMetadata) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var fields any
	if err := decoder.Decode(&fields); err != nil {
		return err
	}
	metadata := SidechainMetadata{}
	metadata.AgentType = firstStringField(fields, sidechainLifecycleAgentTypeFields...)
	if metadata.AgentType == "" {
		metadata.AgentType = topLevelStringField(fields, "type")
	}
	metadata.WorktreePath = firstStringField(fields, sidechainLifecycleWorktreeFields...)
	metadata.WorktreeOwned = firstBoolField(fields, sidechainLifecycleWorktreeOwnedFields...)
	metadata.WorktreeCleanupStatus = firstStringField(fields, sidechainLifecycleWorktreeCleanupStatusFields...)
	metadata.WorktreeCleanupReason = firstTextField(fields, sidechainLifecycleWorktreeCleanupReasonFields...)
	metadata.WorktreeCleanupAt = firstStringField(fields, sidechainLifecycleWorktreeCleanupTimeFields...)
	metadata.Description = firstTextField(fields, sidechainLifecycleDescriptionFields...)
	metadata.AgentPath = firstStringField(fields, sidechainLifecycleAgentPathFields...)
	metadata.AgentPrompt = firstTextField(fields, sidechainLifecycleAgentPromptFields...)
	metadata.AgentModel = firstStringField(fields, sidechainLifecycleAgentModelFields...)
	metadata.AgentPermissionMode = firstStringField(fields, sidechainLifecycleAgentPermissionModeFields...)
	metadata.AgentAllowedTools = firstStringSliceField(fields, sidechainLifecycleAgentAllowedToolsFields...)
	*m = metadata
	return nil
}

func topLevelStringField(value any, key string) string {
	fields, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return scalarStringField(fields[key])
}

func (m SidechainMetadata) Empty() bool {
	return m.AgentType == "" &&
		m.WorktreePath == "" &&
		!m.WorktreeOwned &&
		m.WorktreeCleanupStatus == "" &&
		m.WorktreeCleanupReason == "" &&
		m.WorktreeCleanupAt == "" &&
		m.Description == "" &&
		m.AgentPath == "" &&
		m.AgentPrompt == "" &&
		m.AgentModel == "" &&
		m.AgentPermissionMode == "" &&
		len(m.AgentAllowedTools) == 0
}

func WriteSidechainMetadata(path string, metadata SidechainMetadata) error {
	if path == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func ReadSidechainMetadata(path string) (SidechainMetadata, error) {
	if path == "" {
		return SidechainMetadata{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SidechainMetadata{}, nil
		}
		return SidechainMetadata{}, err
	}
	var metadata SidechainMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return SidechainMetadata{}, err
	}
	return metadata, nil
}
