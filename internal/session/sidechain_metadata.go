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
		metadata.AgentType = firstStringField(fields, "agent_type", "agentKind", "agent_kind", "agent", "type", "subagentType", "subagent_type", "agentName", "agent_name", "name")
	}
	if metadata.WorktreePath == "" {
		metadata.WorktreePath = firstStringField(fields, "worktree_path", "worktree", "worktreeDir", "worktree_dir", "workingDirectory", "working_directory", "cwd", "workspacePath", "workspace_path", "workspace", "path", "directory")
	}
	if metadata.Description == "" {
		metadata.Description = firstStringField(fields, "description_text", "descriptionText", "desc", "summary", "task", "taskDescription", "task_description", "prompt", "input", "command", "title")
	}
	*m = metadata
	return nil
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
