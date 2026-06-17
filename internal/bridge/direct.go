package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"ccgo/internal/commands"
	"ccgo/internal/contracts"
)

type DirectOptions struct {
	SessionID     contracts.ID
	Manifest      Manifest
	Registry      commands.Registry
	RemoteTrigger DirectRemoteTriggerFunc
}

type DirectHandler struct {
	sessionID     contracts.ID
	manifest      Manifest
	registry      commands.Registry
	remoteTrigger DirectRemoteTriggerFunc
}

type DirectRemoteTriggerFunc func(context.Context, DirectRemoteTriggerRequest) (DirectRemoteTriggerResponse, int)

type DirectHealthResponse struct {
	OK        bool         `json:"ok"`
	SessionID contracts.ID `json:"session_id,omitempty"`
	Commands  int          `json:"commands"`
}

type DirectCommandRequest struct {
	Command string       `json:"command"`
	UUID    contracts.ID `json:"uuid,omitempty"`
}

type DirectRemoteTriggerRequest struct {
	TeamID  string `json:"team_id"`
	Target  string `json:"target,omitempty"`
	EventID string `json:"event_id,omitempty"`
	Source  string `json:"source,omitempty"`
	Event   string `json:"event,omitempty"`
	Message string `json:"message"`
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

type DirectRemoteTriggerResponse struct {
	Accepted   bool           `json:"accepted"`
	Duplicate  bool           `json:"duplicate,omitempty"`
	TeamID     string         `json:"team_id,omitempty"`
	Target     string         `json:"target,omitempty"`
	EventID    string         `json:"event_id,omitempty"`
	Source     string         `json:"source,omitempty"`
	Event      string         `json:"event,omitempty"`
	SentCount  int            `json:"sent_count,omitempty"`
	Structured map[string]any `json:"structured,omitempty"`
	Error      string         `json:"error,omitempty"`
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
	manifest := opts.Manifest
	if opts.RemoteTrigger != nil {
		manifest = WithRemoteTriggerCapability(manifest)
	}
	return &DirectHandler{
		sessionID:     sessionID,
		manifest:      manifest,
		registry:      opts.Registry,
		remoteTrigger: opts.RemoteTrigger,
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
	case "/remote-trigger":
		h.handleRemoteTrigger(w, r)
	case "/ws":
		h.handleWebSocket(w, r)
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
	response, status := h.execute(req)
	writeDirectJSON(w, status, response)
}

func (h *DirectHandler) handleRemoteTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDirectError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.remoteTrigger == nil {
		writeDirectError(w, http.StatusNotImplemented, "remote trigger endpoint is not configured")
		return
	}
	req, ok := decodeDirectRemoteTriggerRequest(w, r)
	if !ok {
		return
	}
	response, status := h.remoteTrigger(r.Context(), req)
	if status == 0 {
		status = http.StatusOK
	}
	writeDirectJSON(w, status, response)
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

func (h *DirectHandler) execute(req DirectCommandRequest) (DirectExecuteResponse, int) {
	resolved := h.resolve(req.Command)
	if !resolved.Allowed || resolved.Command == nil {
		return DirectExecuteResponse{
			Allowed: false,
			Error:   resolved.Reason,
		}, http.StatusForbidden
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
		return response, http.StatusInternalServerError
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
	return response, http.StatusOK
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

func decodeDirectRemoteTriggerRequest(w http.ResponseWriter, r *http.Request) (DirectRemoteTriggerRequest, bool) {
	defer r.Body.Close()
	var req DirectRemoteTriggerRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeDirectError(w, http.StatusBadRequest, "invalid JSON request: "+err.Error())
		return DirectRemoteTriggerRequest{}, false
	}
	req.TeamID = strings.TrimSpace(req.TeamID)
	req.Target = strings.TrimSpace(req.Target)
	req.EventID = strings.TrimSpace(req.EventID)
	req.Source = strings.TrimSpace(req.Source)
	req.Event = strings.TrimSpace(req.Event)
	req.Message = strings.TrimSpace(req.Message)
	normalized, err := normalizeDirectRemoteTriggerRequest(req)
	if err != nil {
		writeDirectError(w, http.StatusBadRequest, err.Error())
		return DirectRemoteTriggerRequest{}, false
	}
	return normalized, true
}

func normalizeDirectRemoteTriggerRequest(req DirectRemoteTriggerRequest) (DirectRemoteTriggerRequest, error) {
	req.TeamID = strings.TrimSpace(req.TeamID)
	req.Target = strings.TrimSpace(req.Target)
	req.EventID = strings.TrimSpace(req.EventID)
	req.Source = strings.TrimSpace(req.Source)
	req.Event = strings.TrimSpace(req.Event)
	req.Message = strings.TrimSpace(req.Message)
	if req.TeamID == "" {
		return DirectRemoteTriggerRequest{}, fmt.Errorf("team_id is required")
	}
	if req.Message == "" {
		return DirectRemoteTriggerRequest{}, fmt.Errorf("message is required")
	}
	return req, nil
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
