package bridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"ccgo/internal/commands"
	"ccgo/internal/contracts"
)

type DirectOptions struct {
	SessionID contracts.ID
	Manifest  Manifest
	Registry  commands.Registry
}

type DirectHandler struct {
	sessionID contracts.ID
	manifest  Manifest
	registry  commands.Registry
}

type DirectHealthResponse struct {
	OK        bool         `json:"ok"`
	SessionID contracts.ID `json:"session_id,omitempty"`
	Commands  int          `json:"commands"`
}

type DirectCommandRequest struct {
	Command string       `json:"command"`
	UUID    contracts.ID `json:"uuid,omitempty"`
}

type DirectResolveResponse struct {
	Allowed bool     `json:"allowed"`
	Command *Command `json:"command,omitempty"`
	Name    string   `json:"name,omitempty"`
	Args    string   `json:"args,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

type DirectExecuteResponse struct {
	Allowed      bool               `json:"allowed"`
	Handled      bool               `json:"handled"`
	Command      *Command           `json:"command,omitempty"`
	Name         string             `json:"name,omitempty"`
	Args         string             `json:"args,omitempty"`
	ShouldQuery  bool               `json:"should_query"`
	Model        string             `json:"model,omitempty"`
	AllowedTools []string           `json:"allowed_tools,omitempty"`
	LocalResult  *DirectLocalResult `json:"local_result,omitempty"`
	ResultText   string             `json:"result_text,omitempty"`
	Messages     int                `json:"messages"`
	Unknown      bool               `json:"unknown,omitempty"`
	Unsupported  bool               `json:"unsupported,omitempty"`
	Error        string             `json:"error,omitempty"`
}

type DirectLocalResult struct {
	Type     commands.LocalCommandResultType `json:"type"`
	HasValue bool                            `json:"has_value"`
}

type DirectErrorResponse struct {
	Error string `json:"error"`
}

func NewDirectHandler(opts DirectOptions) *DirectHandler {
	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = opts.Manifest.SessionID
	}
	return &DirectHandler{
		sessionID: sessionID,
		manifest:  opts.Manifest,
		registry:  opts.Registry,
	}
}

func NewDirectHandlerFromSettings(sessionID contracts.ID, cwd string, settings contracts.Settings) *DirectHandler {
	registry := commands.Load(commands.Options{CWD: cwd, Settings: settings})
	return NewDirectHandler(DirectOptions{
		SessionID: sessionID,
		Manifest:  BuildManifest(sessionID, cwd, registry),
		Registry:  registry,
	})
}

func (h *DirectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch normalizeDirectPath(r.URL.Path) {
	case "/health":
		h.handleHealth(w, r)
	case "/manifest":
		h.handleManifest(w, r)
	case "/resolve":
		h.handleResolve(w, r)
	case "/execute":
		h.handleExecute(w, r)
	default:
		writeDirectError(w, http.StatusNotFound, "bridge endpoint not found")
	}
}

func (h *DirectHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDirectError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeDirectJSON(w, http.StatusOK, DirectHealthResponse{
		OK:        true,
		SessionID: h.sessionID,
		Commands:  len(h.manifest.Commands),
	})
}

func (h *DirectHandler) handleManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDirectError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeDirectJSON(w, http.StatusOK, h.manifest)
}

func (h *DirectHandler) handleResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDirectError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req, ok := decodeDirectCommandRequest(w, r)
	if !ok {
		return
	}
	resolved := h.resolve(req.Command)
	writeDirectJSON(w, http.StatusOK, resolved)
}

func (h *DirectHandler) handleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDirectError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req, ok := decodeDirectCommandRequest(w, r)
	if !ok {
		return
	}
	resolved := h.resolve(req.Command)
	if !resolved.Allowed || resolved.Command == nil {
		writeDirectJSON(w, http.StatusForbidden, DirectExecuteResponse{
			Allowed: false,
			Error:   resolved.Reason,
		})
		return
	}
	canonical := canonicalSlashCommand(*resolved.Command, resolved.Args)
	result, handled, err := commands.ExecuteSlashCommand(h.registry, canonical, commands.SlashOptions{
		SessionID: h.sessionID,
		UUID:      req.UUID,
	})
	response := DirectExecuteResponse{
		Allowed: true,
		Handled: handled,
		Command: resolved.Command,
		Name:    resolved.Name,
		Args:    resolved.Args,
	}
	if err != nil {
		response.Error = err.Error()
		writeDirectJSON(w, http.StatusInternalServerError, response)
		return
	}
	response.ShouldQuery = result.ShouldQuery
	response.Model = result.Model
	response.AllowedTools = append([]string(nil), result.AllowedTools...)
	response.Messages = len(result.Messages)
	response.Unknown = result.Unknown
	response.Unsupported = result.Unsupported
	response.ResultText = result.ResultText
	if result.LocalResult != nil {
		response.LocalResult = &DirectLocalResult{
			Type:     result.LocalResult.Type,
			HasValue: strings.TrimSpace(result.LocalResult.Value) != "",
		}
	}
	writeDirectJSON(w, http.StatusOK, response)
}

func (h *DirectHandler) resolve(raw string) DirectResolveResponse {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DirectResolveResponse{Allowed: false, Reason: "command is required"}
	}
	command, ok := h.manifest.FindCommand(raw)
	if !ok {
		return DirectResolveResponse{Allowed: false, Reason: "command is not bridge-safe or is not registered"}
	}
	_, args := splitBridgeCommand(raw)
	return DirectResolveResponse{
		Allowed: true,
		Command: &command,
		Name:    command.Name,
		Args:    args,
	}
}

func decodeDirectCommandRequest(w http.ResponseWriter, r *http.Request) (DirectCommandRequest, bool) {
	defer r.Body.Close()
	var req DirectCommandRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeDirectError(w, http.StatusBadRequest, "invalid JSON request: "+err.Error())
		return DirectCommandRequest{}, false
	}
	if strings.TrimSpace(req.Command) == "" {
		writeDirectError(w, http.StatusBadRequest, "command is required")
		return DirectCommandRequest{}, false
	}
	return req, true
}

func canonicalSlashCommand(command Command, args string) string {
	if strings.TrimSpace(args) == "" {
		return "/" + command.Name
	}
	return "/" + command.Name + " " + strings.TrimSpace(args)
}

func splitBridgeCommand(raw string) (string, string) {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "/"))
	if raw == "" {
		return "", ""
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return "", ""
	}
	name := fields[0]
	offset := strings.Index(raw, name) + len(name)
	return name, strings.TrimSpace(raw[offset:])
}

func normalizeDirectPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/"
	}
	if path == "/v1" {
		return "/"
	}
	if strings.HasPrefix(path, "/v1/") {
		path = strings.TrimPrefix(path, "/v1")
	}
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func writeDirectJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		_, _ = fmt.Fprintf(w, `{"error":%q}`+"\n", err.Error())
	}
}

func writeDirectError(w http.ResponseWriter, status int, message string) {
	writeDirectJSON(w, status, DirectErrorResponse{Error: message})
}
