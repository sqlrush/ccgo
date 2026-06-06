package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type SidechainMetadata struct {
	AgentType    string `json:"agentType,omitempty"`
	WorktreePath string `json:"worktreePath,omitempty"`
	Description  string `json:"description,omitempty"`
}

func (m *SidechainMetadata) UnmarshalJSON(data []byte) error {
	type sidechainMetadataJSON SidechainMetadata
	var canonical sidechainMetadataJSON
	if err := json.Unmarshal(data, &canonical); err != nil {
		return err
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	metadata := SidechainMetadata(canonical)
	if metadata.AgentType == "" {
		metadata.AgentType = firstStringField(fields, appendSidechainMetadataFallbackFields(sidechainLifecycleAgentTypeFields, "type")...)
	}
	if metadata.WorktreePath == "" {
		metadata.WorktreePath = firstStringField(fields, sidechainLifecycleWorktreeFields...)
	}
	if metadata.Description == "" {
		metadata.Description = firstStringField(fields, sidechainLifecycleDescriptionFields...)
	}
	*m = metadata
	return nil
}

func appendSidechainMetadataFallbackFields(fields []string, fallback ...string) []string {
	out := make([]string, 0, len(fields)+len(fallback))
	out = append(out, fields...)
	out = append(out, fallback...)
	return out
}

func (m SidechainMetadata) Empty() bool {
	return m.AgentType == "" && m.WorktreePath == "" && m.Description == ""
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
