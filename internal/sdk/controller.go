package sdk

import (
	"fmt"
	"os"

	"ccgo/internal/contracts"
)

// MCPServerStatus is the CC-wire shape for a single MCP server's connection state.
// CC ref: coreSchemas.ts:167-220 (McpServerStatusSchema).
type MCPServerStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Scope  string `json:"scope,omitempty"`
}

// ContextUsage is the CC-wire shape for get_context_usage responses.
// Only the fields ccgo can populate are included; the rest are zero-valued so
// SDK consumers receive a structurally valid (but partially populated) response.
// CC ref: controlSchemas.ts:175-306 (SDKControlGetContextUsageResponseSchema).
type ContextUsage struct {
	Categories           []ContextCategory `json:"categories"`
	TotalTokens          int               `json:"totalTokens"`
	MaxTokens            int               `json:"maxTokens"`
	RawMaxTokens         int               `json:"rawMaxTokens"`
	Percentage           float64           `json:"percentage"`
	GridRows             [][]any           `json:"gridRows"`
	Model                string            `json:"model"`
	MemoryFiles          []any             `json:"memoryFiles"`
	MCPTools             []any             `json:"mcpTools"`
	Agents               []any             `json:"agents"`
	IsAutoCompactEnabled bool              `json:"isAutoCompactEnabled"`
	APIUsage             any               `json:"apiUsage"`
}

// ContextCategory is one entry in ContextUsage.Categories.
type ContextCategory struct {
	Name       string  `json:"name"`
	Tokens     int     `json:"tokens"`
	Color      string  `json:"color"`
	IsDeferred bool    `json:"isDeferred,omitempty"`
}

// RewindFilesResult is the CC-wire shape for rewind_files responses.
// CC ref: controlSchemas.ts:308-328 (SDKControlRewindFilesResponseSchema).
type RewindFilesResult struct {
	CanRewind    bool     `json:"canRewind"`
	Error        string   `json:"error,omitempty"`
	FilesChanged []string `json:"filesChanged,omitempty"`
	Insertions   int      `json:"insertions,omitempty"`
	Deletions    int      `json:"deletions,omitempty"`
}

// MCPSetServersResult is the CC-wire shape for mcp_set_servers responses.
// CC ref: controlSchemas.ts:384-403 (SDKControlMcpSetServersResponseSchema).
type MCPSetServersResult struct {
	Added   []string          `json:"added"`
	Removed []string          `json:"removed"`
	Errors  map[string]string `json:"errors"`
}

// SettingsSource is one entry in the get_settings response sources list.
type SettingsSource struct {
	Source   string         `json:"source"`
	Settings map[string]any `json:"settings"`
}

// SettingsResult is the CC-wire shape for get_settings responses.
// CC ref: controlSchemas.ts:475-519 (SDKControlGetSettingsResponseSchema).
type SettingsResult struct {
	Effective map[string]any   `json:"effective"`
	Sources   []SettingsSource `json:"sources"`
}

// ElicitationResult is the CC-wire shape for elicitation responses.
// CC ref: controlSchemas.ts:522-545 (SDKControlElicitationResponseSchema).
type ElicitationResult struct {
	Action  string         `json:"action"` // accept | decline | cancel
	Content map[string]any `json:"content,omitempty"`
}

// ReloadPluginsResult is the CC-wire shape for reload_plugins responses.
// CC ref: controlSchemas.ts:405-433 (SDKControlReloadPluginsResponseSchema).
type ReloadPluginsResult struct {
	Commands   []any  `json:"commands"`
	Agents     []any  `json:"agents"`
	Plugins    []any  `json:"plugins"`
	MCPServers []any  `json:"mcpServers"`
	ErrorCount int    `json:"error_count"`
}

// Controller dispatches inbound control requests to live session callbacks.
// Wire contract: CC bridgeMessaging.ts (all subtype handlers).
//
// All callback fields are optional — if nil the subtype is accepted but returns
// an ⚠️ "not supported in this context" error, matching CC's own behavior when
// a handler is unregistered (bridgeMessaging.ts:339).
type Controller struct {
	// Existing callbacks (F1-C00 baseline).
	interrupt func()
	setModel  func(string) error

	// F1-C02 callbacks.
	setPermissionMode    func(contracts.PermissionMode) error
	setMaxThinkingTokens func(int) error // nil max_thinking_tokens → 0
	mcpStatus            func() ([]MCPServerStatus, error)
	getContextUsage      func() (*ContextUsage, error)

	// F1-C03 callbacks.
	rewindFiles        func(userMessageID string, dryRun bool) (*RewindFilesResult, error)
	cancelAsyncMessage func(messageUUID string) (bool, error)
	seedReadState      func(path string, mtime int64) error
	hookCallback       func(callbackID string, input map[string]any) error

	// F1-C04 callbacks.
	mcpMessage    func(serverName string, message map[string]any) error
	mcpSetServers func(servers map[string]any) (*MCPSetServersResult, error)
	mcpReconnect  func(serverName string) error
	mcpToggle     func(serverName string, enabled bool) error

	// F1-C05 callbacks.
	reloadPlugins    func() (*ReloadPluginsResult, error)
	getSettings      func() (*SettingsResult, error)
	applyFlagSettings func(settings map[string]any) error
	stopTask         func(taskID string) error
	elicitation      func(serverName, message string, extra map[string]any) (*ElicitationResult, error)
}

// NewController wires the interrupt and set_model callbacks for a live session.
// Additional callbacks are set via With* options (or directly on the struct for
// packages in the same module).
func NewController(interrupt func(), setModel func(string) error) *Controller {
	return &Controller{interrupt: interrupt, setModel: setModel}
}

// unregistered returns an error response matching CC's "not supported in this
// context" pattern (bridgeMessaging.ts:339).
func unregistered(requestID, subtype string) ControlResponse {
	return ErrorResponse(requestID,
		fmt.Sprintf("%s is not supported in this context (callback not registered)", subtype))
}

// Handle dispatches one control request and returns the response to write back.
func (c *Controller) Handle(req ControlRequest) ControlResponse {
	id := req.RequestID

	switch req.Subtype() {

	// ── F1-C00 (baseline) ────────────────────────────────────────────────────

	case "interrupt":
		if c.interrupt != nil {
			c.interrupt()
		}
		return SuccessResponse(id, nil)

	case "set_model":
		model, _ := req.Request["model"].(string)
		if c.setModel != nil {
			if err := c.setModel(model); err != nil {
				return ErrorResponse(id, err.Error())
			}
		}
		return SuccessResponse(id, map[string]any{"model": model})

	case "initialize":
		// Return the full CC-required initialize response shape.
		// CC ref: bridgeMessaging.ts:286-303; controlSchemas.ts:77-95.
		return SuccessResponse(id, map[string]any{
			"capabilities":            []string{"interrupt", "set_model", "can_use_tool"},
			"commands":                []any{},
			"models":                  []any{},
			"account":                 map[string]any{},
			"output_style":            "normal",
			"available_output_styles": []string{"normal"},
			"pid":                     os.Getpid(),
		})

	// ── F1-C02 ───────────────────────────────────────────────────────────────

	case "set_permission_mode":
		// CC ref: bridgeMessaging.ts:328-358; controlSchemas.ts:124-135.
		if c.setPermissionMode == nil {
			return unregistered(id, "set_permission_mode")
		}
		mode, _ := req.Request["mode"].(string)
		if err := c.setPermissionMode(contracts.PermissionMode(mode)); err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, nil)

	case "set_max_thinking_tokens":
		// CC ref: bridgeMessaging.ts:317-326; controlSchemas.ts:146-155.
		if c.setMaxThinkingTokens == nil {
			return unregistered(id, "set_max_thinking_tokens")
		}
		// max_thinking_tokens may be a JSON number or null.
		var tokens int
		switch v := req.Request["max_thinking_tokens"].(type) {
		case float64:
			tokens = int(v)
		case nil:
			tokens = 0
		}
		if err := c.setMaxThinkingTokens(tokens); err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, nil)

	case "mcp_status":
		// CC ref: controlSchemas.ts:157-173; coreSchemas.ts:167-220.
		if c.mcpStatus == nil {
			return SuccessResponse(id, map[string]any{"mcpServers": []any{}})
		}
		statuses, err := c.mcpStatus()
		if err != nil {
			return ErrorResponse(id, err.Error())
		}
		// Convert []MCPServerStatus to []any for the response map.
		servers := make([]any, len(statuses))
		for i, s := range statuses {
			servers[i] = map[string]any{
				"name":   s.Name,
				"status": s.Status,
				"error":  s.Error,
				"scope":  s.Scope,
			}
		}
		return SuccessResponse(id, map[string]any{"mcpServers": servers})

	case "get_context_usage":
		// CC ref: controlSchemas.ts:175-306.
		if c.getContextUsage == nil {
			// Return a minimal valid response so SDK consumers don't crash.
			return SuccessResponse(id, map[string]any{
				"categories":           []any{},
				"totalTokens":          0,
				"maxTokens":            0,
				"rawMaxTokens":         0,
				"percentage":           0.0,
				"gridRows":             []any{},
				"model":                "",
				"memoryFiles":          []any{},
				"mcpTools":             []any{},
				"agents":               []any{},
				"isAutoCompactEnabled": false,
				"apiUsage":             nil,
			})
		}
		usage, err := c.getContextUsage()
		if err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, map[string]any{
			"categories":           usage.Categories,
			"totalTokens":          usage.TotalTokens,
			"maxTokens":            usage.MaxTokens,
			"rawMaxTokens":         usage.RawMaxTokens,
			"percentage":           usage.Percentage,
			"gridRows":             usage.GridRows,
			"model":                usage.Model,
			"memoryFiles":          usage.MemoryFiles,
			"mcpTools":             usage.MCPTools,
			"agents":               usage.Agents,
			"isAutoCompactEnabled": usage.IsAutoCompactEnabled,
			"apiUsage":             usage.APIUsage,
		})

	// ── F1-C03 ───────────────────────────────────────────────────────────────

	case "rewind_files":
		// CC ref: controlSchemas.ts:308-328.
		if c.rewindFiles == nil {
			return SuccessResponse(id, map[string]any{
				"canRewind": false,
				"error":     "rewind_files is not supported in this context (callback not registered)",
			})
		}
		msgID, _ := req.Request["user_message_id"].(string)
		dryRun, _ := req.Request["dry_run"].(bool)
		result, err := c.rewindFiles(msgID, dryRun)
		if err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, map[string]any{
			"canRewind":    result.CanRewind,
			"error":        result.Error,
			"filesChanged": result.FilesChanged,
			"insertions":   result.Insertions,
			"deletions":    result.Deletions,
		})

	case "cancel_async_message":
		// CC ref: controlSchemas.ts:330-349.
		if c.cancelAsyncMessage == nil {
			return SuccessResponse(id, map[string]any{"cancelled": false})
		}
		uuid, _ := req.Request["message_uuid"].(string)
		cancelled, err := c.cancelAsyncMessage(uuid)
		if err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, map[string]any{"cancelled": cancelled})

	case "seed_read_state":
		// CC ref: controlSchemas.ts:351-362.
		if c.seedReadState == nil {
			return unregistered(id, "seed_read_state")
		}
		path, _ := req.Request["path"].(string)
		var mtime int64
		switch v := req.Request["mtime"].(type) {
		case float64:
			mtime = int64(v)
		}
		if err := c.seedReadState(path, mtime); err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, nil)

	case "hook_callback":
		// CC ref: controlSchemas.ts:363-372.
		if c.hookCallback == nil {
			return unregistered(id, "hook_callback")
		}
		cbID, _ := req.Request["callback_id"].(string)
		input, _ := req.Request["input"].(map[string]any)
		if err := c.hookCallback(cbID, input); err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, nil)

	// ── F1-C04 ───────────────────────────────────────────────────────────────

	case "mcp_message":
		// CC ref: controlSchemas.ts:374-383.
		if c.mcpMessage == nil {
			return unregistered(id, "mcp_message")
		}
		serverName, _ := req.Request["server_name"].(string)
		message, _ := req.Request["message"].(map[string]any)
		if err := c.mcpMessage(serverName, message); err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, nil)

	case "mcp_set_servers":
		// CC ref: controlSchemas.ts:384-403.
		if c.mcpSetServers == nil {
			return SuccessResponse(id, map[string]any{
				"added":   []any{},
				"removed": []any{},
				"errors":  map[string]any{},
			})
		}
		servers, _ := req.Request["servers"].(map[string]any)
		result, err := c.mcpSetServers(servers)
		if err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, map[string]any{
			"added":   result.Added,
			"removed": result.Removed,
			"errors":  result.Errors,
		})

	case "mcp_reconnect":
		// CC ref: controlSchemas.ts:435-441.
		if c.mcpReconnect == nil {
			return unregistered(id, "mcp_reconnect")
		}
		serverName, _ := req.Request["serverName"].(string)
		if err := c.mcpReconnect(serverName); err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, nil)

	case "mcp_toggle":
		// CC ref: controlSchemas.ts:443-451.
		if c.mcpToggle == nil {
			return unregistered(id, "mcp_toggle")
		}
		serverName, _ := req.Request["serverName"].(string)
		enabled, _ := req.Request["enabled"].(bool)
		if err := c.mcpToggle(serverName, enabled); err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, nil)

	// ── F1-C05 ───────────────────────────────────────────────────────────────

	case "reload_plugins":
		// CC ref: controlSchemas.ts:405-433.
		if c.reloadPlugins == nil {
			return SuccessResponse(id, map[string]any{
				"commands":    []any{},
				"agents":      []any{},
				"plugins":     []any{},
				"mcpServers":  []any{},
				"error_count": 0,
			})
		}
		result, err := c.reloadPlugins()
		if err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, map[string]any{
			"commands":    result.Commands,
			"agents":      result.Agents,
			"plugins":     result.Plugins,
			"mcpServers":  result.MCPServers,
			"error_count": result.ErrorCount,
		})

	case "get_settings":
		// CC ref: controlSchemas.ts:475-519.
		if c.getSettings == nil {
			return SuccessResponse(id, map[string]any{
				"effective": map[string]any{},
				"sources":   []any{},
			})
		}
		result, err := c.getSettings()
		if err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, map[string]any{
			"effective": result.Effective,
			"sources":   result.Sources,
		})

	case "apply_flag_settings":
		// CC ref: controlSchemas.ts:464-473.
		if c.applyFlagSettings == nil {
			return unregistered(id, "apply_flag_settings")
		}
		settings, _ := req.Request["settings"].(map[string]any)
		if err := c.applyFlagSettings(settings); err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, nil)

	case "stop_task":
		// CC ref: controlSchemas.ts:455-462.
		if c.stopTask == nil {
			return unregistered(id, "stop_task")
		}
		taskID, _ := req.Request["task_id"].(string)
		if err := c.stopTask(taskID); err != nil {
			return ErrorResponse(id, err.Error())
		}
		return SuccessResponse(id, nil)

	case "elicitation":
		// CC ref: controlSchemas.ts:522-545.
		// elicitation is server-initiated: the server sends this to the SDK consumer
		// asking for user input. ccgo has no elicitation backend so we return an
		// ⚠️ unregistered error unless a callback is provided.
		if c.elicitation == nil {
			return unregistered(id, "elicitation")
		}
		serverName, _ := req.Request["mcp_server_name"].(string)
		message, _ := req.Request["message"].(string)
		extra := map[string]any{}
		for k, v := range req.Request {
			if k != "subtype" && k != "mcp_server_name" && k != "message" {
				extra[k] = v
			}
		}
		result, err := c.elicitation(serverName, message, extra)
		if err != nil {
			return ErrorResponse(id, err.Error())
		}
		resp := map[string]any{"action": result.Action}
		if result.Content != nil {
			resp["content"] = result.Content
		}
		return SuccessResponse(id, resp)

	default:
		return ErrorResponse(id, fmt.Sprintf("unsupported control subtype %q", req.Subtype()))
	}
}
