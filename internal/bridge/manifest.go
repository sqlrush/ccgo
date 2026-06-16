package bridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccgo/internal/commands"
	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const manifestFileName = "bridge-manifest.json"

type Manifest struct {
	SessionID        contracts.ID `json:"session_id,omitempty"`
	WorkingDirectory string       `json:"working_directory,omitempty"`
	GeneratedAt      string       `json:"generated_at"`
	Commands         []Command    `json:"commands,omitempty"`
}

type Command struct {
	Name                   string                  `json:"name"`
	DisplayName            string                  `json:"display_name,omitempty"`
	Type                   contracts.CommandType   `json:"type"`
	Source                 contracts.CommandSource `json:"source,omitempty"`
	LoadedFrom             string                  `json:"loaded_from,omitempty"`
	Aliases                []string                `json:"aliases,omitempty"`
	ArgumentHint           string                  `json:"argument_hint,omitempty"`
	SupportsNonInteractive bool                    `json:"supports_non_interactive,omitempty"`
	Immediate              bool                    `json:"immediate,omitempty"`
}

func SessionManifestPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), manifestFileName)
}

func BuildManifest(sessionID contracts.ID, cwd string, registry commands.Registry) Manifest {
	manifest := Manifest{
		SessionID:        sessionID,
		WorkingDirectory: cwd,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	for _, command := range registry.Visible() {
		if !commands.IsBridgeSafeCommand(command) {
			continue
		}
		manifest.Commands = append(manifest.Commands, Command{
			Name:                   command.Name,
			DisplayName:            command.DisplayName,
			Type:                   command.Type,
			Source:                 command.Source,
			LoadedFrom:             command.LoadedFrom,
			Aliases:                append([]string(nil), command.Aliases...),
			ArgumentHint:           command.ArgumentHint,
			SupportsNonInteractive: command.SupportsNonInteractive,
			Immediate:              command.Immediate,
		})
	}
	return manifest
}

func BuildManifestFromSettings(sessionID contracts.ID, cwd string, settings contracts.Settings) Manifest {
	return BuildManifest(sessionID, cwd, commands.Load(commands.Options{CWD: cwd, Settings: settings}))
}

func WriteManifest(path string, manifest Manifest) error {
	if path == "" {
		return os.ErrInvalid
	}
	if manifest.GeneratedAt == "" {
		manifest.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadManifest(path string) (Manifest, error) {
	if path == "" {
		return Manifest{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) FindCommand(raw string) (Command, bool) {
	name := normalizeCommandName(raw)
	if name == "" {
		return Command{}, false
	}
	for _, command := range m.Commands {
		if commandMatches(command, name) {
			return command, true
		}
	}
	return Command{}, false
}

func (m Manifest) AllowsCommand(raw string) bool {
	_, ok := m.FindCommand(raw)
	return ok
}

func commandMatches(command Command, name string) bool {
	if strings.EqualFold(command.Name, name) || strings.EqualFold(command.DisplayName, name) {
		return true
	}
	for _, alias := range command.Aliases {
		if strings.EqualFold(alias, name) {
			return true
		}
	}
	return false
}

func normalizeCommandName(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" {
		return ""
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}
