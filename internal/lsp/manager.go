package lsp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const managerStatusFileName = "lsp-manager.json"

const (
	ServerRuntimeNotStarted       = "not_started"
	ServerRuntimeRunning          = "running"
	ServerRuntimeExited           = "exited"
	ServerRuntimeFailed           = "failed"
	ServerRuntimeNoWorkspaceMatch = "no_workspace_match"
	ServerRuntimeInvalid          = "invalid"
)

type ServerDefinition struct {
	Name           string   `json:"name"`
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	Languages      []string `json:"languages,omitempty"`
	FileExtensions []string `json:"file_extensions,omitempty"`
	RootMarkers    []string `json:"root_markers,omitempty"`
}

type ManagerStatus struct {
	SessionID        contracts.ID   `json:"session_id,omitempty"`
	WorkingDirectory string         `json:"working_directory,omitempty"`
	GeneratedAt      string         `json:"generated_at"`
	Servers          []ServerStatus `json:"servers,omitempty"`
}

type ServerStatus struct {
	Name           string   `json:"name"`
	Command        string   `json:"command,omitempty"`
	Args           []string `json:"args,omitempty"`
	Languages      []string `json:"languages,omitempty"`
	FileExtensions []string `json:"file_extensions,omitempty"`
	RootMarkers    []string `json:"root_markers,omitempty"`
	RuntimeState   string   `json:"runtime_state"`
	Reason         string   `json:"reason,omitempty"`
	MatchReasons   []string `json:"match_reasons,omitempty"`
	ProcessID      int      `json:"process_id,omitempty"`
	StartedAt      string   `json:"started_at,omitempty"`
	EndedAt        string   `json:"ended_at,omitempty"`
}

func SessionManagerStatusPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), managerStatusFileName)
}

func DefaultServerDefinitions() []ServerDefinition {
	return []ServerDefinition{
		{
			Name:           "clangd",
			Command:        "clangd",
			Languages:      []string{"c", "cpp"},
			FileExtensions: []string{".c", ".cc", ".cpp", ".cxx", ".h", ".hpp", ".hh"},
			RootMarkers:    []string{"compile_commands.json", "compile_flags.txt"},
		},
		{
			Name:           "gopls",
			Command:        "gopls",
			Languages:      []string{"go"},
			FileExtensions: []string{".go"},
			RootMarkers:    []string{"go.work", "go.mod"},
		},
		{
			Name:           "lua-language-server",
			Command:        "lua-language-server",
			Languages:      []string{"lua"},
			FileExtensions: []string{".lua"},
			RootMarkers:    []string{".luarc.json", ".luarc.jsonc"},
		},
		{
			Name:           "pylsp",
			Command:        "pylsp",
			Languages:      []string{"python"},
			FileExtensions: []string{".py"},
			RootMarkers:    []string{"pyproject.toml", "setup.py", "requirements.txt"},
		},
		{
			Name:           "rust-analyzer",
			Command:        "rust-analyzer",
			Languages:      []string{"rust"},
			FileExtensions: []string{".rs"},
			RootMarkers:    []string{"Cargo.toml"},
		},
		{
			Name:           "typescript-language-server",
			Command:        "typescript-language-server",
			Args:           []string{"--stdio"},
			Languages:      []string{"javascript", "typescript"},
			FileExtensions: []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx"},
			RootMarkers:    []string{"package.json", "tsconfig.json", "jsconfig.json"},
		},
	}
}

func BuildManagerStatus(sessionID contracts.ID, cwd string, definitions []ServerDefinition, files []string) ManagerStatus {
	if definitions == nil {
		definitions = DefaultServerDefinitions()
	}
	status := ManagerStatus{
		SessionID:        sessionID,
		WorkingDirectory: cwd,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	for _, definition := range normalizeServerDefinitions(definitions) {
		status.Servers = append(status.Servers, resolveServerStatus(cwd, definition, files))
	}
	return status
}

func WriteManagerStatus(path string, status ManagerStatus) error {
	if path == "" {
		return os.ErrInvalid
	}
	if status.GeneratedAt == "" {
		status.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadManagerStatus(path string) (ManagerStatus, error) {
	if path == "" {
		return ManagerStatus{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ManagerStatus{}, nil
	}
	if err != nil {
		return ManagerStatus{}, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return ManagerStatus{}, nil
	}
	var status ManagerStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return ManagerStatus{}, err
	}
	status.Servers = normalizeServerStatuses(status.Servers)
	return status, nil
}

func CountServerRuntimeStates(servers []ServerStatus) map[string]int {
	counts := make(map[string]int)
	for _, server := range servers {
		state := strings.TrimSpace(server.RuntimeState)
		if state == "" {
			state = ServerRuntimeInvalid
		}
		counts[state]++
	}
	return counts
}

func CountMatchedServers(servers []ServerStatus) int {
	count := 0
	for _, server := range servers {
		if len(server.MatchReasons) > 0 {
			count++
		}
	}
	return count
}

func UpsertServerStatus(status ManagerStatus, server ServerStatus) ManagerStatus {
	servers := normalizeServerStatuses([]ServerStatus{server})
	if len(servers) == 0 {
		return status
	}
	server = servers[0]
	replaced := false
	for i, existing := range status.Servers {
		if existing.Name == server.Name {
			status.Servers[i] = server
			replaced = true
			break
		}
	}
	if !replaced {
		status.Servers = append(status.Servers, server)
	}
	status.Servers = normalizeServerStatuses(status.Servers)
	if status.GeneratedAt == "" {
		status.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return status
}

func resolveServerStatus(cwd string, definition ServerDefinition, files []string) ServerStatus {
	status := ServerStatus{
		Name:           definition.Name,
		Command:        definition.Command,
		Args:           append([]string(nil), definition.Args...),
		Languages:      append([]string(nil), definition.Languages...),
		FileExtensions: append([]string(nil), definition.FileExtensions...),
		RootMarkers:    append([]string(nil), definition.RootMarkers...),
	}
	if status.Name == "" || strings.TrimSpace(status.Command) == "" {
		status.RuntimeState = ServerRuntimeInvalid
		status.Reason = "language server definition requires a name and command"
		return status
	}
	status.MatchReasons = serverMatchReasons(cwd, definition, files)
	if len(status.MatchReasons) == 0 {
		status.RuntimeState = ServerRuntimeNoWorkspaceMatch
		status.Reason = "no matching workspace marker or file extension"
		return status
	}
	status.RuntimeState = ServerRuntimeNotStarted
	status.Reason = "language server process has not been started"
	return status
}

func serverMatchReasons(cwd string, definition ServerDefinition, files []string) []string {
	reasons := map[string]struct{}{}
	if strings.TrimSpace(cwd) != "" {
		for _, marker := range definition.RootMarkers {
			marker = strings.TrimSpace(marker)
			if marker == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(cwd, marker)); err == nil {
				reasons["root:"+marker] = struct{}{}
			}
		}
	}
	extensions := map[string]struct{}{}
	for _, ext := range definition.FileExtensions {
		ext = normalizeExtension(ext)
		if ext != "" {
			extensions[ext] = struct{}{}
		}
	}
	for _, file := range files {
		ext := normalizeExtension(filepath.Ext(strings.TrimSpace(file)))
		if _, ok := extensions[ext]; ok {
			reasons["extension:"+ext] = struct{}{}
		}
	}
	out := make([]string, 0, len(reasons))
	for reason := range reasons {
		out = append(out, reason)
	}
	sort.Strings(out)
	return out
}

func normalizeServerDefinitions(definitions []ServerDefinition) []ServerDefinition {
	out := make([]ServerDefinition, 0, len(definitions))
	for _, definition := range definitions {
		definition.Name = strings.TrimSpace(definition.Name)
		definition.Command = strings.TrimSpace(definition.Command)
		definition.Args = sortedTrimmedStrings(definition.Args)
		definition.Languages = sortedTrimmedStrings(definition.Languages)
		definition.FileExtensions = normalizeExtensions(definition.FileExtensions)
		definition.RootMarkers = sortedTrimmedStrings(definition.RootMarkers)
		if definition.Name == "" && definition.Command == "" {
			continue
		}
		out = append(out, definition)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func normalizeServerStatuses(servers []ServerStatus) []ServerStatus {
	out := make([]ServerStatus, 0, len(servers))
	for _, server := range servers {
		server.Name = strings.TrimSpace(server.Name)
		server.Command = strings.TrimSpace(server.Command)
		server.Args = sortedTrimmedStrings(server.Args)
		server.Languages = sortedTrimmedStrings(server.Languages)
		server.FileExtensions = normalizeExtensions(server.FileExtensions)
		server.RootMarkers = sortedTrimmedStrings(server.RootMarkers)
		server.RuntimeState = strings.TrimSpace(server.RuntimeState)
		server.Reason = strings.TrimSpace(server.Reason)
		server.MatchReasons = sortedTrimmedStrings(server.MatchReasons)
		server.StartedAt = strings.TrimSpace(server.StartedAt)
		server.EndedAt = strings.TrimSpace(server.EndedAt)
		if server.Name == "" {
			continue
		}
		out = append(out, server)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func normalizeExtensions(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		ext := normalizeExtension(value)
		if ext == "" {
			continue
		}
		if _, ok := seen[ext]; ok {
			continue
		}
		seen[ext] = struct{}{}
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

func normalizeExtension(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, ".") {
		value = "." + value
	}
	return value
}

func sortedTrimmedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
