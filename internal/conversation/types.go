package conversation

import (
	"context"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/auth"
	bridgepkg "ccgo/internal/bridge"
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/config"
	"ccgo/internal/contracts"
	hookpkg "ccgo/internal/hooks"
	integrationspkg "ccgo/internal/integrations"
	lsppkg "ccgo/internal/lsp"
	"ccgo/internal/mcp"
	"ccgo/internal/memory"
	nativepkg "ccgo/internal/native"
	"ccgo/internal/orchestration"
	"ccgo/internal/rewind"
	"ccgo/internal/session"
	"ccgo/internal/tool"
	filetools "ccgo/internal/tools/file"
)

type MessageClient interface {
	CreateMessage(context.Context, anthropic.Request) (*anthropic.Response, error)
}

type StreamingMessageClient interface {
	StreamMessages(context.Context, anthropic.Request, func(anthropic.StreamEvent) error) error
}

type TokenCountingMessageClient interface {
	CountTokens(context.Context, anthropic.CountTokensRequest) (*anthropic.CountTokensResponse, error)
}

type PromptDumpProvider interface {
	CachedPromptDumpRequests() []anthropic.PromptDumpCacheEntry
	PromptDumpPath() string
}

type EventType string

const (
	EventUserMessage        EventType = "user_message"
	EventAssistantMessage   EventType = "assistant_message"
	EventToolUse            EventType = "tool_use"
	EventToolResult         EventType = "tool_result"
	EventToolProgress       EventType = "tool_progress"
	EventRetry              EventType = "retry"
	EventTokenWarning       EventType = "token_warning"
	EventCompact            EventType = "compact"
	EventStreamEvent        EventType = "stream_event"
	EventDeferredPoolChange EventType = "tengu_deferred_tools_pool_change"
	EventToolSearchDecision EventType = "tengu_tool_search_mode_decision"
)

type TokenWarning struct {
	TokenUsage int
	Window     compactpkg.WindowConfig
	State      compactpkg.WarningState
}

type RetryInfo struct {
	Attempt     int
	MaxAttempts int
	FailedModel string
	NextModel   string
	Fallback    bool
}

type DeferredToolsPoolChange struct {
	AddedCount              int
	RemovedCount            int
	PriorAnnouncedCount     int
	MessagesLength          int
	AttachmentCount         int
	DeferredToolsDeltaCount int
	CallSite                string
	QuerySource             string
	AttachmentTypesSeen     string
}

type ToolSearchModeDecision struct {
	Enabled                      bool
	Mode                         string
	Reason                       string
	CheckedModel                 string
	MCPToolCount                 int
	UserType                     string
	DeferredToolTokens           int
	Threshold                    int
	DeferredToolDescriptionChars int
	CharThreshold                int
}

type Event struct {
	Type                    EventType
	Message                 *contracts.Message
	ToolUse                 *contracts.ToolUse
	ToolResult              *contracts.ToolResult
	ToolProgress            *contracts.ToolProgress
	TokenWarning            *TokenWarning
	Retry                   *RetryInfo
	Compact                 *compactpkg.Result
	StreamEvent             *anthropic.StreamEvent
	DeferredToolsPoolChange *DeferredToolsPoolChange
	ToolSearchModeDecision  *ToolSearchModeDecision
	Model                   string
	Error                   error
}

type Runner struct {
	Client                    MessageClient
	Tools                     tool.Executor
	MCP                       *MCPConfig
	Permissions               tool.PermissionDecider
	PermissionMode            contracts.PermissionMode
	APIKeySource              string
	BetaHeaders               []string
	FastMode                  bool
	Model                     string
	FallbackModels            []string
	SystemPrompt              string
	// BaseSystemPrompt holds the system prompt without the CLAUDE.md content.
	// It is set alongside SystemPrompt when claudeMd is injected, allowing
	// sub-agents with OmitClaudeMd=true to use a prompt free of CLAUDE.md rules.
	// ORCH-35: CC ref: src/tools/AgentTool/runAgent.ts shouldOmitClaudeMd.
	BaseSystemPrompt string
	MaxTokens                 int
	ThinkingBudgetTokens      int
	// EffortLevel sets the effort level sent in output_config.effort (CFG-32).
	// Valid values: "low", "medium", "high", "max". Empty means unset.
	// CC ref: utils/effort.ts resolveAppliedEffort; betas.ts EFFORT_BETA_HEADER.
	EffortLevel string
	// OutputSchema is the JSON schema for structured output (CLI-FLAG-40, --json-schema).
	// When non-nil, output_config.format is set to {type:"json_schema", json_schema: schema}
	// and the structured-outputs-2025-12-15 beta header is added to BetaHeaders.
	// CC ref: src/services/api/claude.ts:1577-1586; src/constants/betas.ts:8.
	OutputSchema contracts.JSONSchema
	// AlwaysThinkingEnabled forces thinking for every request (CFG-33).
	// When true and the model supports thinking, a default thinking budget is used.
	// CC ref: settings/types.ts alwaysThinkingEnabled.
	AlwaysThinkingEnabled bool
	// Language injects a preferred-language section into the system prompt (CFG-18).
	// When non-empty, Claude is instructed to respond in this language.
	// CC ref: constants/prompts.ts getLanguageSection; settings/types.ts language.
	Language string
	// IncludeGitInstructions controls whether git instructions are included in the
	// system prompt sections that reference git usage (CFG-16). Defaults to true.
	// When false, git-related sections are omitted from the system prompt.
	// CC ref: utils/gitSettings.ts shouldIncludeGitInstructions.
	IncludeGitInstructions *bool
	// Verbose enables detailed debug output (CFG-53).
	// CC ref: tools/ConfigTool/supportedSettings.ts verbose; global config key.
	Verbose bool
	MaxToolRounds             int
	UseStreaming              bool
	// MaxBudgetUSD is the maximum dollar amount to spend on API calls in --print mode.
	// When positive, the turn loop checks accumulated cost after each turn and stops
	// if the budget is exceeded. CC ref: src/main.tsx:--max-budget-usd.
	MaxBudgetUSD float64
	EnablePromptCaching       bool
	PromptCacheTTL            string
	SessionID                 contracts.ID
	SessionPath               string
	WorkingDirectory          string
	ContentBudget             *session.ContentReplacementState
	ContentBudgetDir          string
	ContentBudgetMax          int
	SkipBudgetTools           map[string]struct{}
	AutoCompact               *compactpkg.AutoConfig
	CompactClient             compactpkg.MessageClient
	CompactMaxTokens          int
	EnableMicroCompact        bool
	MicroCompactKeepLast      int
	MicroCompactMaxChars      int
	MicroCompactDir           string
	SessionMemoryRoot         string
	EnableSessionMemoryRecall bool
	SessionMemoryRecallRoot   string
	SessionMemoryRecallLimit  int
	RelevantMemoryDir         string
	RelevantMemoryLimit       int
	SkillDirs                 []string
	LSPServerDefinitions      []lsppkg.ServerDefinition
	LSPStartupDocuments       []lsppkg.OpenDocument
	LSPProcesses              map[string]*lsppkg.ServerProcess
	BridgeDirectServer        *bridgepkg.DirectServer
	BridgeDirectAddr          string
	BridgeDirectToken         string
	EnableMemoryExtraction    bool
	MemoryAgentClient         memory.MessageClient
	MemoryExtractLimit        int

	NativeClipboardRunner       nativepkg.ClipboardCommandRunner
	NativeVoiceRunner           integrationspkg.VoiceCommandRunner
	NativeVoiceTranscribeRunner integrationspkg.VoiceTranscriptionRunner
	NativeComputerUseRunner     integrationspkg.ComputerUseCommandRunner

	OnEvent func(Event)

	// CredentialStore is used by /logout to delete stored OAuth credentials.
	// When nil, /logout reports that no credentials are stored (safe default).
	CredentialStore auth.CredentialStore

	// settingsOverride bypasses MCP settings entirely when non-nil. Used only
	// in tests to inject a known Settings without needing a full MCPConfig.
	settingsOverride *contracts.Settings

	// ReadState tracks files read/edited by tools across all turns. When non-nil
	// it is injected into every tool execution context so tools can record their
	// file accesses. Used by BuildPostCompactAttachments (COMPACT-05) to re-attach
	// recently-read files after compaction.
	ReadState *filetools.ReadState

	// RewindWriter, when non-nil, records a file-history snapshot at the start of
	// each user turn (REWIND-01). When nil, rewind snapshot writing is disabled.
	RewindWriter *rewind.Writer

	// RewindStore, when non-nil, holds the content-addressed backup store used by
	// RewindWriter.Capture. Must be set together with RewindWriter.
	RewindStore *rewind.Store

	// AgentRegistry tracks in-process background agents started via Task
	// run_in_background=true (ORCH-03). When non-nil, background tasks are
	// dispatched to this registry and callers return immediately with
	// status:"async_launched". When nil, a session-local registry is allocated
	// automatically on first use.
	AgentRegistry *orchestration.AgentRegistry

	// AsyncHookRegistry tracks hooks that returned {"async":true} and are
	// running in the background (HOOK-12 runtime). When non-nil, async hooks
	// are enqueued here instead of blocking the turn. When nil, a registry is
	// allocated automatically on first use.
	// CC ref: src/utils/hooks.ts:184-264 (executeInBackground).
	AsyncHookRegistry *hookpkg.AsyncHookRegistry

	// ExtraToolMetadata holds additional key-value pairs that are merged into
	// the tool execution context metadata on every turn. Callers use this to
	// inject per-session dependencies (e.g. tool.QuestionAsker for the TUI)
	// without adding new typed fields to Runner. Keys added here take
	// precedence over auto-generated metadata with the same key.
	ExtraToolMetadata map[string]any

	// MCPManager, when non-nil, holds the live connection manager for this
	// session. The /mcp slash panel reads live status (connected/failed/disabled)
	// from it. SDK Controller mcp_* subtypes are wired to it via sdk.Options.
	// G11: live MCP connection manager.
	MCPManager *mcp.Manager
}

type MCPConfig struct {
	UserSettings    contracts.Settings
	ProjectSettings contracts.Settings
	LocalSettings   contracts.Settings
	PolicySettings  contracts.Settings
	PluginServers   map[string]contracts.MCPServer
	CWD             string
	ParseOptions    mcp.ParseOptions
	ToolOptions     mcp.ServerToolOptions

	settingsFileDetector *config.SettingsChangeDetector
}

type Result struct {
	Messages      []contracts.Message
	Assistant     contracts.Message
	ToolResults   []contracts.ToolResult
	StopReason    string
	Usage         contracts.Usage
	APIDuration   time.Duration
	FinalRequest  anthropic.Request
	ModelsAttempt []string
	Compacted     bool
	Compact       *compactpkg.Result
	// MicroCompact holds the result of ephemeral micro-compaction when it fired
	// during this turn. It is set for observability only — the micro boundary and
	// summary are intentionally NOT appended to Messages (unlike auto-compaction)
	// because the caller persists result.Messages to its history, which would make
	// the compaction permanent and defeat micro-compaction's per-turn ephemeral
	// semantics (see CC query.ts:412-426). MicroCompact is nil when micro-compaction
	// is disabled or did not fire.
	MicroCompact  *compactpkg.MicroResult
	Cleared       bool
	// Login signals that the caller (interactive REPL or CLI) should run the OAuth
	// login ceremony. The runner itself does not open a browser — it has no terminal.
	Login     bool
	// LoggedOut is true when /logout successfully deleted (or confirmed absence of)
	// stored credentials.
	LoggedOut bool
}

func (r Runner) maxTokens() int {
	if r.MaxTokens > 0 {
		return r.MaxTokens
	}
	return 4096
}

func (r Runner) model() string {
	if r.Model != "" {
		return r.Model
	}
	return "sonnet"
}

func (r Runner) maxToolRounds() int {
	if r.MaxToolRounds > 0 {
		return r.MaxToolRounds
	}
	return 8
}

func (r Runner) emit(event Event) {
	r.recordTelemetry(event)
	if r.OnEvent != nil {
		r.OnEvent(event)
	}
}
