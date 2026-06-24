package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"ccgo/internal/agentfile"
	"ccgo/internal/api/anthropic"
	"ccgo/internal/auth"
	"ccgo/internal/bootstrap"
	"ccgo/internal/commands"
	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/costtrack"
	daemonpkg "ccgo/internal/daemon"
	integrationspkg "ccgo/internal/integrations"
	"ccgo/internal/mcp"
	"ccgo/internal/memory"
	"ccgo/internal/messages"
	"ccgo/internal/model"
	orchestrationpkg "ccgo/internal/orchestration"
	"ccgo/internal/permissions"
	"ccgo/internal/platform"
	pluginpkg "ccgo/internal/plugins"
	remotepkg "ccgo/internal/remote"
	"ccgo/internal/repl"
	"ccgo/internal/rewind"
	"ccgo/internal/sandbox"
	sdkpkg "ccgo/internal/sdk"
	"ccgo/internal/session"
	"ccgo/internal/settingswriter"
	"ccgo/internal/tool"
	filetools "ccgo/internal/tools/file"
	"ccgo/internal/mcp/reconnect"
	tasktools "ccgo/internal/tools/task"
	"ccgo/internal/tui"
)

const version = "0.0.0-dev"
const chromeNativeHostProtocolVersion = "1"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

type cliOptions struct {
	Model           string
	MaxTokens       int
	MaxTurns        int
	PermissionMode  string
	SkipPermissions bool
	MCPConfig       string
	Stream          bool
	Resume          string
	Continue        bool
	SystemPrompt    string
	AppendSystem    string
	AllowedTools    []string
	DeniedTools     []string
	AddDirs         []string

	// F2-C01: debug/mode flags
	Verbose bool
	Debug   string // optional category filter, e.g. "api,hooks"
	Bare    bool
	Thinking string // "enabled", "adaptive", "disabled"
	Effort   string // "low", "medium", "high", "max"

	// F2-C02: session flags
	SettingsPath         string // --settings <file-or-json>
	SessionID            string // --session-id <uuid>
	NoSessionPersistence bool   // --no-session-persistence (--print only)
	ForkSession          bool   // --fork-session
	SessionName          string // -n/--name

	// F2-C03: agent/model flags
	Agent         string // --agent <agent>
	Agents        string // --agents <json>
	FallbackModel string // --fallback-model <model>
	Betas         []string // --betas <betas...>
	Tools         []string // --tools <tools...>

	// F2-C04: output/control flags
	StrictMCPConfig       bool   // --strict-mcp-config
	SettingSources        string // --setting-sources <sources>
	IncludeHookEvents     bool   // --include-hook-events
	IncludePartialMessages bool  // --include-partial-messages
	ReplayUserMessages    bool   // --replay-user-messages
	PermissionPromptTool  string // --permission-prompt-tool <tool>
	JSONSchema            string // --json-schema <schema>
	MaxBudgetUSD          float64 // --max-budget-usd <amount>

	// F2-C05: system-prompt file / plugin / command flags
	SystemPromptFile       string // --system-prompt-file <file>
	AppendSystemPromptFile string // --append-system-prompt-file <file>
	PluginDirs             []string // --plugin-dir <path> (repeatable)
	DisableSlashCommands   bool   // --disable-slash-commands
	AllowDangerouslySkipPermissions bool // --allow-dangerously-skip-permissions

	// F2-C06: worktree / file flags
	Worktree string // --worktree/-w [name]
	Tmux     bool   // --tmux
	Files    []string // --file <specs>
}

type daemonOptions struct {
	Once              bool
	HeartbeatInterval time.Duration
}

type daemonTickOptions struct {
	SkipRemoteWhenWebSocket bool
}

type daemonControlOptions struct {
	StatePath         string
	Status            bool
	Stop              bool
	Tick              bool
	Start             bool
	Restart           bool
	HeartbeatInterval time.Duration
}

type daemonProcessStartOptions struct {
	WorkingDirectory string
	SessionID        contracts.ID
	StatePath        string
	Heartbeat        time.Duration
}

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedStringFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	// Sandbox child entrypoint: dispatched before flag parsing so that the
	// confined child re-exec (Linux only) applies landlock+seccomp and then
	// exec's the real command, replacing this process image.
	if len(args) >= 1 && args[0] == sandbox.ChildSentinel {
		if err := sandbox.RunChild(args[1:]); err != nil {
			fmt.Fprintf(stderr, "ccgo: sandbox child: %v\n", err)
			return 1
		}
		return 0 // unreachable on success — unix.Exec replaces the process
	}

	flags := flag.NewFlagSet("claude", flag.ContinueOnError)
	flags.SetOutput(stderr)

	showVersion := flags.Bool("version", false, "print version")
	flags.BoolVar(showVersion, "v", false, "print version")
	chromeNativeHost := flags.Bool("chrome-native-host", false, "run Chrome native messaging host")
	daemonMode := flags.Bool("daemon", false, "run daemon heartbeat loop")
	daemonOnce := flags.Bool("daemon-once", false, "write one daemon heartbeat and exit")
	daemonHeartbeat := flags.Duration("daemon-heartbeat", 5*time.Second, "daemon heartbeat interval")
	daemonStatus := flags.Bool("daemon-status", false, "print daemon status and exit")
	daemonStop := flags.Bool("daemon-stop", false, "stop the daemon recorded in daemon state")
	daemonTick := flags.Bool("daemon-tick", false, "trigger one daemon schedule tick")
	daemonStart := flags.Bool("daemon-start", false, "start daemon in the background")
	daemonRestart := flags.Bool("daemon-restart", false, "restart daemon in the background")
	daemonSession := flags.String("daemon-session", "", "daemon session id")
	daemonStatePath := flags.String("daemon-state", "", "daemon state path")
	cwd := flags.String("cwd", "", "working directory")
	printMode := flags.Bool("print", false, "print response and exit")
	flags.BoolVar(printMode, "p", false, "print response and exit")
	modelName := flags.String("model", "", "model to use")
	maxTokens := flags.Int("max-tokens", 0, "maximum output tokens")
	flags.IntVar(maxTokens, "maxTokens", 0, "maximum output tokens")
	maxTurns := flags.Int("max-turns", 0, "maximum tool-use turns in print mode")
	flags.IntVar(maxTurns, "maxTurns", 0, "maximum tool-use turns in print mode")
	permissionMode := flags.String("permission-mode", "", "permission mode")
	flags.StringVar(permissionMode, "permissionMode", "", "permission mode")
	skipPermissions := flags.Bool("dangerously-skip-permissions", false, "bypass tool permission prompts")
	flags.BoolVar(skipPermissions, "dangerouslySkipPermissions", false, "bypass tool permission prompts")
	mcpConfig := flags.String("mcp-config", "", "MCP configuration JSON file")
	flags.StringVar(mcpConfig, "mcpConfig", "", "MCP configuration JSON file")
	stream := flags.Bool("stream", false, "use streaming API")
	inputFormat := flags.String("input-format", "text", "input format: text, json, or stream-json")
	flags.StringVar(inputFormat, "inputFormat", "text", "input format: text, json, or stream-json")
	outputFormat := flags.String("output-format", "text", "output format: text, json, or stream-json")
	flags.StringVar(outputFormat, "outputFormat", "text", "output format: text, json, or stream-json")
	resume := flags.String("resume", "", "resume a session by ID or transcript path")
	continueMode := flags.Bool("continue", false, "continue the most recent session")
	systemPrompt := flags.String("system-prompt", "", "system prompt for the model request")
	flags.StringVar(systemPrompt, "systemPrompt", "", "system prompt for the model request")
	appendSystemPrompt := flags.String("append-system-prompt", "", "additional system prompt text")
	flags.StringVar(appendSystemPrompt, "appendSystemPrompt", "", "additional system prompt text")
	var allowedTools repeatedStringFlag
	flags.Var(&allowedTools, "allowedTools", "allowed tool rules")
	flags.Var(&allowedTools, "allowed-tools", "allowed tool rules")
	var deniedTools repeatedStringFlag
	flags.Var(&deniedTools, "disallowedTools", "disallowed tool rules")
	flags.Var(&deniedTools, "disallowed-tools", "disallowed tool rules")
	var addDirs repeatedStringFlag
	flags.Var(&addDirs, "add-dir", "additional working directory")
	flags.Var(&addDirs, "addDir", "additional working directory")

	// F2-C01: debug/mode flags
	verbose := flags.Bool("verbose", false, "override verbose mode setting from config")
	debug := flags.String("debug", "", "enable debug mode with optional category filter (e.g. \"api,hooks\")")
	flags.StringVar(debug, "d", "", "enable debug mode with optional category filter")
	bare := flags.Bool("bare", false, "minimal mode: skip hooks, LSP, plugin sync; sets CLAUDE_CODE_SIMPLE=1")
	thinking := flags.String("thinking", "", "thinking mode: enabled, adaptive, disabled")
	effort := flags.String("effort", "", "effort level: low, medium, high, max")

	// F2-C02: session flags
	settingsPath := flags.String("settings", "", "path to a settings JSON file or JSON string for additional settings")
	sessionID := flags.String("session-id", "", "use a specific UUID as the session ID")
	noSessionPersistence := flags.Bool("no-session-persistence", false, "disable session persistence (--print only)")
	forkSession := flags.Bool("fork-session", false, "when resuming, create a new session ID instead of reusing the original")
	sessionName := flags.String("name", "", "set a display name for this session")
	flags.StringVar(sessionName, "n", "", "set a display name for this session")

	// F2-C03: agent/model flags
	agentName := flags.String("agent", "", "agent for the current session")
	agentsJSON := flags.String("agents", "", "JSON object defining custom agents")
	fallbackModel := flags.String("fallback-model", "", "fallback model when default model is overloaded (--print only)")
	var betas repeatedStringFlag
	flags.Var(&betas, "betas", "beta headers to include in API requests (API key users only)")
	var tools repeatedStringFlag
	flags.Var(&tools, "tools", "list of available built-in tools (use \"\" to disable all, \"default\" for all)")

	// F2-C04: output/control flags
	strictMCPConfig := flags.Bool("strict-mcp-config", false, "only use MCP servers from --mcp-config, ignoring all other configs")
	settingSources := flags.String("setting-sources", "", "comma-separated list of setting sources to load (user,project,local)")
	includeHookEvents := flags.Bool("include-hook-events", false, "include hook lifecycle events in stream-json output")
	includePartialMessages := flags.Bool("include-partial-messages", false, "include partial message chunks (--print --output-format=stream-json)")
	replayUserMessages := flags.Bool("replay-user-messages", false, "re-emit user messages from stdin back on stdout (stream-json mode)")
	permissionPromptTool := flags.String("permission-prompt-tool", "", "MCP tool to use for permission prompts (--print only)")
	jsonSchema := flags.String("json-schema", "", "JSON Schema for structured output validation")
	maxBudgetUSD := flags.Float64("max-budget-usd", 0, "maximum dollar amount to spend on API calls (--print only)")

	// F2-C05: system-prompt file / plugin / command flags
	systemPromptFile := flags.String("system-prompt-file", "", "read system prompt from a file")
	appendSystemPromptFile := flags.String("append-system-prompt-file", "", "read and append system prompt from a file")
	var pluginDirs repeatedStringFlag
	flags.Var(&pluginDirs, "plugin-dir", "load plugins from a directory for this session (repeatable)")
	disableSlashCommands := flags.Bool("disable-slash-commands", false, "disable all skills/slash commands")
	allowDangerouslySkipPermissions := flags.Bool("allow-dangerously-skip-permissions", false, "enable bypassing permissions as an option without enabling it by default")

	// F2-C06: worktree / file flags (IDE/Chrome/from-pr are companion OUT-of-scope, registered as no-ops)
	worktree := flags.String("worktree", "", "create a new git worktree for this session (optionally specify name)")
	flags.StringVar(worktree, "w", "", "create a new git worktree for this session")
	tmux := flags.Bool("tmux", false, "create a tmux session for the worktree (requires --worktree)")
	var fileSpecs repeatedStringFlag
	flags.Var(&fileSpecs, "file", "file resources to download at startup (format: file_id:relative_path)")
	// Companion/cloud flags registered as no-ops (OUT-of-scope §10).
	_ = flags.Bool("ide", false, "auto-connect to IDE on startup (companion feature, not implemented)")
	_ = flags.Bool("chrome", false, "enable Claude in Chrome integration (companion feature, not implemented)")
	_ = flags.String("from-pr", "", "resume a session linked to a PR (companion feature, not implemented)")

	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	// CLI-FLAG-12: detect when --resume was explicitly set with no value.
	// CC maps `--resume` (no value) to boolean true and opens the picker.
	// We detect the explicit-but-empty case via flags.Visit after Parse.
	// CC ref: src/main.tsx `-r, --resume [value]` (value => value || true).
	resumeExplicit := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "resume" {
			resumeExplicit = true
		}
	})
	openResumePicker := resumeExplicit && strings.TrimSpace(*resume) == ""

	if *showVersion {
		fmt.Fprintf(stdout, "%s (ccgo)\n", version)
		return 0
	}
	if *chromeNativeHost {
		return runChromeNativeHost(stdin, stdout, stderr)
	}

	state, err := bootstrap.New()
	if err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}
	if err := applyCWDFlag(state, *cwd); err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}
	if strings.TrimSpace(*daemonSession) != "" {
		state.SetSessionID(contracts.ID(strings.TrimSpace(*daemonSession)))
	}
	if *daemonStatus || *daemonStop || *daemonTick || *daemonStart || *daemonRestart {
		return runDaemonControl(context.Background(), state, daemonControlOptions{
			StatePath:         strings.TrimSpace(*daemonStatePath),
			Status:            *daemonStatus,
			Stop:              *daemonStop,
			Tick:              *daemonTick,
			Start:             *daemonStart,
			Restart:           *daemonRestart,
			HeartbeatInterval: *daemonHeartbeat,
		}, stdout, stderr)
	}
	if *daemonMode || *daemonOnce {
		return runDaemon(context.Background(), state, daemonOptions{
			Once:              *daemonOnce,
			HeartbeatInterval: *daemonHeartbeat,
		}, stdout, stderr)
	}
	if !*printMode && len(flags.Args()) > 0 && strings.EqualFold(flags.Args()[0], "plugin") {
		return runPluginCLI(context.Background(), state, flags.Args()[1:], stdout, stderr)
	}
	if !*printMode && len(flags.Args()) > 0 && strings.EqualFold(flags.Args()[0], "auth") {
		return runAuthCLI(context.Background(), state, flags.Args()[1:], stdin, stdout, stderr)
	}
	if !*printMode && len(flags.Args()) > 0 && strings.EqualFold(flags.Args()[0], "mcp") {
		return runMCPCommand(flags.Args()[1:], stdout, stderr, defaultMCPCLIEnv(state.CWD()))
	}
	if !*printMode && len(flags.Args()) > 0 && strings.EqualFold(flags.Args()[0], "agents") {
		return runAgentsCLI(state.CWD(), flags.Args()[1:], stdout, stderr)
	}
	if !*printMode && len(flags.Args()) > 0 && strings.EqualFold(flags.Args()[0], "doctor") {
		return runDoctorCommand(flags.Args()[1:], state.CWD(), stdout, stderr)
	}
	// "update" and "upgrade" alias (SUBCMD-UPDATE-07 / F3-C03).
	if !*printMode && len(flags.Args()) > 0 &&
		(strings.EqualFold(flags.Args()[0], "update") || strings.EqualFold(flags.Args()[0], "upgrade")) {
		return runUpdateCLI(flags.Args()[1:], version, stdout, stderr)
	}
	if !*printMode && len(flags.Args()) > 0 && strings.EqualFold(flags.Args()[0], "completion") {
		return runCompletionCLI(flags.Args()[1:], stdout, stderr)
	}
	// "setup-token" (F3-C05 / SUBCMD-SETUP-TOKEN-01).
	if !*printMode && len(flags.Args()) > 0 && strings.EqualFold(flags.Args()[0], "setup-token") {
		store := auth.NewKeychainCredentialStore("")
		return runSetupTokenCLI(context.Background(), flags.Args()[1:], store, stdin, stdout, stderr)
	}
	// "install" (F3-C05 / SUBCMD-INSTALL-01).
	if !*printMode && len(flags.Args()) > 0 && strings.EqualFold(flags.Args()[0], "install") {
		return runInstallCLI(context.Background(), flags.Args()[1:], stdout, stderr)
	}
	if *printMode {
		normalizedOutputFormat, err := normalizeOutputFormat(*outputFormat)
		if err != nil {
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		startedAt := time.Now()
		format, err := normalizeInputFormat(*inputFormat)
		if err != nil {
			_ = writePrintError(stdout, conversation.Runner{}, err, normalizedOutputFormat, time.Since(startedAt), 0, nil)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		userMessage, err := promptMessageFromArgsOrStdin(flags.Args(), stdin, format)
		if err != nil {
			_ = writePrintError(stdout, conversation.Runner{}, err, normalizedOutputFormat, time.Since(startedAt), 0, nil)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		effectiveMode, err := effectivePermissionMode(*permissionMode, *skipPermissions)
		if err != nil {
			_ = writePrintError(stdout, conversation.Runner{}, err, normalizedOutputFormat, time.Since(startedAt), 0, nil)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		runner, err := headlessRunner(context.Background(), state, cliOptions{
			Model:           *modelName,
			MaxTokens:       *maxTokens,
			MaxTurns:        *maxTurns,
			PermissionMode:  effectiveMode,
			SkipPermissions: *skipPermissions,
			MCPConfig:       *mcpConfig,
			Stream:          *stream,
			SystemPrompt:    *systemPrompt,
			AppendSystem:    *appendSystemPrompt,
			AllowedTools:    append([]string(nil), allowedTools...),
			DeniedTools:     append([]string(nil), deniedTools...),
			AddDirs:         append([]string(nil), addDirs...),
			// F2 flags
			Verbose:                         *verbose,
			Debug:                           *debug,
			Bare:                            *bare,
			Thinking:                        *thinking,
			Effort:                          *effort,
			SettingsPath:                    *settingsPath,
			SessionID:                       *sessionID,
			NoSessionPersistence:            *noSessionPersistence,
			ForkSession:                     *forkSession,
			SessionName:                     *sessionName,
			Agent:                           *agentName,
			Agents:                          *agentsJSON,
			FallbackModel:                   *fallbackModel,
			Betas:                           append([]string(nil), betas...),
			Tools:                           append([]string(nil), tools...),
			StrictMCPConfig:                 *strictMCPConfig,
			SettingSources:                  *settingSources,
			IncludeHookEvents:               *includeHookEvents,
			IncludePartialMessages:          *includePartialMessages,
			ReplayUserMessages:              *replayUserMessages,
			PermissionPromptTool:            *permissionPromptTool,
			JSONSchema:                      *jsonSchema,
			MaxBudgetUSD:                    *maxBudgetUSD,
			SystemPromptFile:                *systemPromptFile,
			AppendSystemPromptFile:          *appendSystemPromptFile,
			PluginDirs:                      append([]string(nil), pluginDirs...),
			DisableSlashCommands:            *disableSlashCommands,
			AllowDangerouslySkipPermissions: *allowDangerouslySkipPermissions,
			Worktree:                        *worktree,
			Tmux:                            *tmux,
			Files:                           append([]string(nil), fileSpecs...),
		})
		if err != nil {
			_ = writePrintError(stdout, runner, err, normalizedOutputFormat, time.Since(startedAt), 0, nil)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		// CLI-FLAG-34: create git worktree when --worktree is provided.
		if err := createWorktreeIfRequested(cliOptions{Worktree: *worktree}, &runner.WorkingDirectory); err != nil {
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		history, err := resumeHistory(state, &runner, cliOptions{Resume: *resume, Continue: *continueMode})
		if err != nil {
			_ = writePrintError(stdout, runner, err, normalizedOutputFormat, time.Since(startedAt), 0, nil)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		// COST-02: restore accumulated cost when resuming a previous session.
		// Merge the prior session total into runner.AccumulatedUsage so that
		// /cost and savePrintCost include the full historical total.
		if runner.SessionID != "" {
			costOpts := costtrack.DefaultOptions(runner.WorkingDirectory)
			if prev, ok, cerr := costtrack.Restore(costOpts, runner.SessionID); ok && cerr == nil {
				runner.AccumulatedUsage = contracts.Usage{
					CostUSD:      prev.LastCost,
					InputTokens:  prev.LastTotalInputTokens,
					OutputTokens: prev.LastTotalOutputTokens,
				}
			}
		}
		// CLI-SDK-01/02: when both input and output formats are stream-json,
		// route through sdk.Query so the can_use_tool/interrupt/set_model
		// control protocol is active over stdin/stdout.
		if format == "stream-json" && normalizedOutputFormat == "stream-json" {
			prompt := extractTextPromptFromMessage(userMessage)
			runnerPtr := runner
			sdkErr := sdkpkg.Query(context.Background(), sdkpkg.Options{
				Prompt: prompt,
				In:     stdin,
				Out:    stdout,
				RunnerFactory: func() (*conversation.Runner, error) {
					return &runnerPtr, nil
				},
				// SDK-49: update_environment_variables → apply to the process
				// environment so subsequent tool calls see the updated vars.
				// CC ref: src/entrypoints/sdk/controlSchemas.ts:629-636.
				OnEnvMutation: func(vars map[string]string) {
					for k, v := range vars {
						_ = os.Setenv(k, v)
					}
				},
			})
			if sdkErr != nil {
				fmt.Fprintf(stderr, "ccgo: sdk: %v\n", sdkErr)
				return 1
			}
			// COST-02: persist cost after SDK turn.
			savePrintCost(runner)
			return 0
		}
		streamErr := func() error { return nil }
		if normalizedOutputFormat == "stream-json" {
			runner, streamErr = attachStreamJSON(stdout, runner, *includePartialMessages, *includeHookEvents)
		}
		// CLI-FLAG-43: --replay-user-messages echoes the user message back on stdout
		// in the stream-json output so SDK consumers see the prompt they sent.
		// CC ref: src/main.tsx:--replay-user-messages; print.ts replay behaviour.
		if normalizedOutputFormat == "stream-json" && *replayUserMessages {
			replayEnc := json.NewEncoder(stdout)
			msgCopy := userMessage
			_ = replayEnc.Encode(printStreamEvent{
				Type:    conversation.EventUserMessage,
				Message: &msgCopy,
			})
		}
		result, err := runner.RunTurn(context.Background(), history, userMessage)
		if err != nil {
			_ = writePrintError(stdout, runner, err, normalizedOutputFormat, time.Since(startedAt), result.APIDuration, result.ModelsAttempt)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		if err := streamErr(); err != nil {
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		if err := writePrintResult(stdout, runner, result, normalizedOutputFormat, time.Since(startedAt)); err != nil {
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		// COST-02: persist cost on --print session exit.
		savePrintCost(runner)
		return 0
	}
	effectiveMode, err := effectivePermissionMode(*permissionMode, *skipPermissions)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}
	ctx := context.Background()
	runner, err := interactiveRunner(ctx, state, cliOptions{
		Model:           *modelName,
		MaxTokens:       *maxTokens,
		MaxTurns:        *maxTurns,
		PermissionMode:  effectiveMode,
		SkipPermissions: *skipPermissions,
		MCPConfig:       *mcpConfig,
		Stream:          *stream,
		SystemPrompt:    *systemPrompt,
		AppendSystem:    *appendSystemPrompt,
		AllowedTools:    append([]string(nil), allowedTools...),
		DeniedTools:     append([]string(nil), deniedTools...),
		AddDirs:         append([]string(nil), addDirs...),
		// F2 flags
		Verbose:                         *verbose,
		Debug:                           *debug,
		Bare:                            *bare,
		Thinking:                        *thinking,
		Effort:                          *effort,
		SettingsPath:                    *settingsPath,
		SessionID:                       *sessionID,
		NoSessionPersistence:            *noSessionPersistence,
		ForkSession:                     *forkSession,
		SessionName:                     *sessionName,
		Agent:                           *agentName,
		Agents:                          *agentsJSON,
		FallbackModel:                   *fallbackModel,
		Betas:                           append([]string(nil), betas...),
		Tools:                           append([]string(nil), tools...),
		StrictMCPConfig:                 *strictMCPConfig,
		SettingSources:                  *settingSources,
		IncludeHookEvents:               *includeHookEvents,
		IncludePartialMessages:          *includePartialMessages,
		ReplayUserMessages:              *replayUserMessages,
		PermissionPromptTool:            *permissionPromptTool,
		JSONSchema:                      *jsonSchema,
		MaxBudgetUSD:                    *maxBudgetUSD,
		SystemPromptFile:                *systemPromptFile,
		AppendSystemPromptFile:          *appendSystemPromptFile,
		PluginDirs:                      append([]string(nil), pluginDirs...),
		DisableSlashCommands:            *disableSlashCommands,
		AllowDangerouslySkipPermissions: *allowDangerouslySkipPermissions,
		Worktree:                        *worktree,
		Tmux:                            *tmux,
		Files:                           append([]string(nil), fileSpecs...),
	})
	if err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}
	// CLI-FLAG-34: create git worktree when --worktree is provided.
	if err := createWorktreeIfRequested(cliOptions{Worktree: *worktree}, &runner.WorkingDirectory); err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}
	history, err := resumeHistory(state, &runner, cliOptions{Resume: *resume, Continue: *continueMode})
	if err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}
	term := repl.NewOSTerminal(os.Stdin, os.Stdout)
	cmdRegistry := commands.Load(commands.Options{
		CWD:            runner.WorkingDirectory,
		Settings:       runnerMergedSettings(runner),
		PolicySettings: runnerPolicySettings(runner),
	})
	writer := settingswriter.New(
		config.UserSettingsPath(),
		config.ProjectSettingsPath(runner.WorkingDirectory),
	)

	mergedSettings := runnerMergedSettings(runner)

	// CFG-13: cleanupPeriodDays — remove old transcript files at startup.
	// CC ref: utils/settings/types.ts cleanupPeriodDays.
	if mergedSettings.CleanupPeriodDays != nil {
		_ = session.CleanupOldTranscripts(*mergedSettings.CleanupPeriodDays)
	}

	// Load persisted prompt history (best-effort; nil on any error).
	var promptHistory []session.HistoryEntry
	if histEntries, err := session.LoadHistory(
		session.HistoryPath(),
		runner.WorkingDirectory,
		runner.SessionID,
		500,
		nil,
	); err == nil {
		promptHistory = histEntries
	}

	// Discover memory files (best-effort).
	var memoryFiles []string
	if claudeFiles, err := memory.DiscoverScopedClaudeFiles(memory.ScopeOptions{
		CWD: runner.WorkingDirectory,
	}); err == nil {
		for _, f := range claudeFiles {
			memoryFiles = append(memoryFiles, f.Path)
		}
	}

	// List resumable sessions (best-effort).
	var resumeEntries []repl.ResumeEntry
	if sessions, err := session.ListProjectSessions(runner.WorkingDirectory); err == nil {
		resumeEntries = make([]repl.ResumeEntry, 0, len(sessions))
		for _, s := range sessions {
			resumeEntries = append(resumeEntries, repl.ResumeEntry{
				ID:          string(s.ID),
				Summary:     s.Title,
				ProjectPath: s.ProjectPath,
			})
		}
	}

	// Load user keybindings (best-effort; absent file is silently ignored).
	var customKeymap *tui.Keymap
	keybindingsPath := filepath.Join(platform.ClaudeHomeDir(), "keybindings.json")
	if specs, err := tui.LoadKeyBindingSpecs(keybindingsPath); err == nil && len(specs) > 0 {
		if km, err := tui.KeymapFromSpecs(tui.DefaultKeymap(), specs); err == nil {
			customKeymap = &km
		}
	}

	// Pre-create a shared AgentRegistry so the REPL loop and the Task tool share
	// the same instance. The runner lazily initialises its own registry on the first
	// RunTurn call; injecting it here ensures any background tasks started during
	// the first turn are visible to the REPL's /tasks command and loop surfacing.
	sharedRegistry := orchestrationpkg.NewAgentRegistry()
	runner.AgentRegistry = sharedRegistry

	// CMD-MCP-01 (G25 audit fix): build and start a live MCP connection Manager
	// from the runner's MCP server configuration so the /mcp overlay can show
	// live connection status and dispatch enable/disable/reconnect actions.
	// The manager is started asynchronously — dial failures set per-server
	// status to "failed" but do not block the REPL from opening.
	// When runner.MCP is nil (no MCP servers configured), mcpMgr stays nil
	// and the /mcp overlay falls back to its text summary.
	mcpMgr := buildInteractiveMCPManager(runner.MCP)
	runner.MCPManager = mcpMgr
	if mcpMgr != nil {
		go func() { _ = mcpMgr.Start(ctx) }()
	}

	opts := repl.InteractiveOptions{
		AgentRegistry:        sharedRegistry,
		Settings:             writer,
		Registry:             cmdRegistry.Visible(),
		Mode:                 runner.PermissionMode,
		Engine:               engineFromDecider(runner.Permissions),
		EditorMode:           mergedSettings.EditorMode,
		PromptHistory:        promptHistory,
		MemoryFiles:          memoryFiles,
		ResumeEntries:        resumeEntries,
		CustomKeymap:         customKeymap,
		MCPApprovalPath:      config.LocalSettingsPath(runner.WorkingDirectory),
		MCPManager:           mcpMgr,
		// CFG-48: display enterprise announcements at session startup.
		// CC ref: utils/settings/types.ts companyAnnouncements; LogoV2.tsx:82-86.
		CompanyAnnouncements: mergedSettings.CompanyAnnouncements,
		// CFG-40: wire fileSuggestion command so its output populates the QuickOpen overlay.
		// CC ref: utils/settings/types.ts fileSuggestion:{type:"command",command:string}.
		FileSuggestionCmd: func() string {
			if mergedSettings.FileSuggestion != nil && mergedSettings.FileSuggestion.Type == "command" {
				return mergedSettings.FileSuggestion.Command
			}
			return ""
		}(),
		// REPL-59: wire preferredNotifChannel so OS notification/bell respects the setting.
		// CC ref: utils/configConstants.ts NOTIFICATION_CHANNELS.
		PreferredNotifChannel: mergedSettings.PreferredNotifChannel,
		// CLI-FLAG-12: open the resume picker at startup when --resume is passed
		// without a session ID. CC maps empty --resume to boolean true which opens
		// the picker immediately before the first prompt.
		// CC ref: src/main.tsx `-r, --resume [value]` (value => value || true).
		OpenResumePicker: openResumePicker,
		// CMD-FAST-01: keep the outer runner.Model in sync with model switches
		// (/fast, /model picker) for post-session bookkeeping (savePrintCost etc).
		OnModelChange: func(m string) {
			runner.Model = m
		},
		// COST-02: accumulate per-turn usage into runner.AccumulatedUsage so that
		// savePrintCost (called below) persists the full session total.
		OnTurnResult: func(result conversation.Result) {
			u := result.Usage
			runner.AccumulatedUsage.CostUSD += u.CostUSD
			runner.AccumulatedUsage.InputTokens += u.InputTokens
			runner.AccumulatedUsage.OutputTokens += u.OutputTokens
			runner.AccumulatedUsage.CacheCreationInputTokens += u.CacheCreationInputTokens
			runner.AccumulatedUsage.CacheReadInputTokens += u.CacheReadInputTokens
		},
	}
	if err := repl.RunInteractiveWithOptions(ctx, term, runner, history, opts); err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}
	// COST-02: persist the accumulated session cost after the interactive session ends.
	savePrintCost(runner)
	return 0
}

func runChromeNativeHost(stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	for {
		raw, err := integrationspkg.ReadChromeNativeMessage(stdin, 1<<20)
		if errors.Is(err, io.EOF) {
			return 0
		}
		if err != nil {
			fmt.Fprintf(stderr, "ccgo chrome native host: %v\n", err)
			return 1
		}
		response := handleChromeNativeHostMessage(raw)
		if err := integrationspkg.WriteChromeNativeMessage(stdout, response); err != nil {
			fmt.Fprintf(stderr, "ccgo chrome native host: %v\n", err)
			return 1
		}
	}
}

type pluginCLIListEntry struct {
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Scope       string   `json:"scope"`
	Enabled     bool     `json:"enabled"`
	InstallPath string   `json:"installPath"`
	MCPServers  []string `json:"mcpServers,omitempty"`
	Errors      []string `json:"errors,omitempty"`
}

type pluginCLIAvailableEntry struct {
	PluginID         string `json:"pluginId"`
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	MarketplaceName  string `json:"marketplaceName"`
	Version          string `json:"version,omitempty"`
	Source           string `json:"source,omitempty"`
	Status           string `json:"status,omitempty"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	InstallPath      string `json:"installPath,omitempty"`
}

type pluginCLIAvailableList struct {
	Installed []pluginCLIListEntry      `json:"installed"`
	Available []pluginCLIAvailableEntry `json:"available"`
}

type pluginCLIMarketplaceEntry struct {
	Name            string   `json:"name"`
	Source          string   `json:"source,omitempty"`
	Repo            string   `json:"repo,omitempty"`
	URL             string   `json:"url,omitempty"`
	Path            string   `json:"path,omitempty"`
	Package         string   `json:"package,omitempty"`
	SparsePaths     []string `json:"sparsePaths,omitempty"`
	InstallLocation string   `json:"installLocation,omitempty"`
}

// runAuthCLI handles the "claude auth" top-level subcommand with subcommands
// login, logout, and status. It mirrors the runPluginCLI dispatch pattern.
func runAuthCLI(ctx context.Context, _ *bootstrap.State, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "ccgo auth: missing subcommand (login|logout|status)")
		return 2
	}
	store := auth.NewKeychainCredentialStore("")
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "login":
		return runAuthLogin(ctx, args[1:], store, stdin, stdout, stderr)
	case "logout":
		return runAuthLogout(ctx, store, stderr, stdout)
	case "status":
		// Parse --json flag for auth status (F3-C02 / AUTH-CLI-07).
		statusFlags := flag.NewFlagSet("auth status", flag.ContinueOnError)
		statusFlags.SetOutput(stderr)
		jsonMode := statusFlags.Bool("json", false, "output JSON")
		if err := statusFlags.Parse(args[1:]); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 2
		}
		// apiKeyHelper is sourced from runner merged settings when available;
		// the auth subcommand has no runner, so we pass "" which covers
		// env-var → keychain → file — the same precedence as headlessRunner.
		return runAuthStatus(ctx, "" /* apiKeyHelperCmd */, stdout, stderr, *jsonMode)
	default:
		fmt.Fprintf(stderr, "ccgo auth: unknown subcommand %q (login|logout|status)\n", args[0])
		return 2
	}
}

// runAuthLogin implements "claude auth login" with a gray-zone consent gate.
// The user must explicitly confirm before any OAuth flow starts. Consent can
// be bypassed non-interactively with the --yes / -y flag.
//
// F3-C01: supports --console (API billing), --claudeai (default), --sso, --email.
func runAuthLogin(ctx context.Context, args []string, store auth.CredentialStore, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	loginFlags := flag.NewFlagSet("auth login", flag.ContinueOnError)
	loginFlags.SetOutput(stderr)
	yesFlag := loginFlags.Bool("yes", false, "skip the consent prompt")
	loginFlags.BoolVar(yesFlag, "y", false, "skip the consent prompt")
	consoleFlag := loginFlags.Bool("console", false, "use Console/API billing login (platform.claude.com)")
	claudeAIFlag := loginFlags.Bool("claudeai", false, "use claude.ai login (default)")
	ssoFlag := loginFlags.Bool("sso", false, "force SSO login method")
	emailFlag := loginFlags.String("email", "", "pre-fill email/login_hint on the login page")
	if err := loginFlags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	// Mutual exclusion: --console and --claudeai cannot be combined.
	if *consoleFlag && *claudeAIFlag {
		fmt.Fprintln(stderr, "ccgo auth: --console and --claudeai cannot be used together")
		fmt.Fprintln(stdout, "ccgo auth: --console and --claudeai cannot be used together")
		return 1
	}

	// Gray-zone consent gate: print the notice and require explicit confirmation.
	fmt.Fprintln(stdout, "OAuth login uses Anthropic's official client and endpoints.")
	fmt.Fprintln(stdout, "This is a ToS/account-policy gray area for unofficial clients.")
	fmt.Fprintln(stdout, "Proceeding means you accept responsibility for this usage.")
	if !*yesFlag {
		fmt.Fprint(stdout, "Continue? [y/N] ")
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil {
			fmt.Fprintln(stderr, "ccgo auth: login cancelled (no consent)")
			return 1
		}
		answer := strings.TrimSpace(line)
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Fprintln(stderr, "ccgo auth: login cancelled (no consent)")
			return 1
		}
	}

	// --console selects Console/API billing; --claudeai (or default) selects claude.ai.
	loginWithClaudeAI := !*consoleFlag

	loginMethod := ""
	if *ssoFlag {
		loginMethod = "sso"
	}

	creds, err := auth.RunLoginFlow(ctx, auth.LoginOptions{
		Browser:           auth.NewOSBrowserOpener(),
		Store:             store,
		LoginWithClaudeAI: loginWithClaudeAI,
		LoginHint:         strings.TrimSpace(*emailFlag),
		LoginMethod:       loginMethod,
		OrgUUID:           "",
		OnURL: func(u string) {
			fmt.Fprintf(stdout, "If your browser did not open, visit:\n%s\n", u)
		},
	})
	if err != nil {
		fmt.Fprintf(stderr, "ccgo auth: login failed: %v\n", err)
		return 1
	}
	_ = creds // never print tokens
	fmt.Fprintln(stdout, "Login successful.")
	return 0
}

// runAuthLogout implements "claude auth logout" by deleting stored credentials.
func runAuthLogout(ctx context.Context, store auth.CredentialStore, stderr io.Writer, stdout io.Writer) int {
	if err := store.Delete(ctx); err != nil {
		fmt.Fprintf(stderr, "ccgo auth: logout failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "Signed out. Stored credentials removed.")
	return 0
}

// runAuthStatus implements "claude auth status". It reports the resolved
// credential source using the same precedence as the headless runner:
//
//	apiKeyHelper (when configured) → env vars → keychain → credentials file
//
// When jsonMode is true (--json flag) it outputs a JSON object matching CC's
// authStatus shape (AUTH-CLI-07 / F3-C02). Token/key values are NEVER printed.
func runAuthStatus(ctx context.Context, apiKeyHelperCmd string, stdout io.Writer, stderr io.Writer, jsonMode bool) int {
	creds, _, err := credentialsFromEnvOrStore(ctx, apiKeyHelperCmd)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo auth: %v\n", err)
		return 1
	}

	loggedIn := creds.Source != auth.SourceNone

	if jsonMode {
		return runAuthStatusJSON(creds, apiKeyHelperCmd, loggedIn, stdout)
	}

	switch creds.Source {
	case auth.SourceOAuth:
		fmt.Fprintln(stdout, "Authenticated via OAuth (keychain).")
	case auth.SourceAPIKey:
		if strings.TrimSpace(apiKeyHelperCmd) != "" {
			fmt.Fprintln(stdout, "Authenticated via API key (apiKeyHelper or environment).")
		} else if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != "" {
			fmt.Fprintln(stdout, "Authenticated via API key (environment).")
		} else {
			fmt.Fprintln(stdout, "Authenticated via API key.")
		}
	default:
		fmt.Fprintln(stdout, "Not authenticated. Run `claude auth login` or set ANTHROPIC_API_KEY.")
	}
	if loggedIn {
		return 0
	}
	return 1
}

// authStatusJSON is the JSON shape for "claude auth status --json" (F3-C02 / AUTH-CLI-07).
// Mirrors CC's authStatus JSON output (auth.ts:296-316).
type authStatusJSON struct {
	LoggedIn       bool   `json:"loggedIn"`
	AuthMethod     string `json:"authMethod"`
	APIKeySource   string `json:"apiKeySource,omitempty"`
	Email          string `json:"email,omitempty"`
	OrgID          string `json:"orgId,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
}

// runAuthStatusJSON emits the JSON form of auth status, mirroring CC's output.
func runAuthStatusJSON(creds auth.Credentials, apiKeyHelperCmd string, loggedIn bool, stdout io.Writer) int {
	out := authStatusJSON{LoggedIn: loggedIn}

	switch creds.Source {
	case auth.SourceOAuth:
		out.AuthMethod = "claude.ai"
	case auth.SourceAPIKey:
		if strings.TrimSpace(apiKeyHelperCmd) != "" {
			out.AuthMethod = "api_key_helper"
			out.APIKeySource = "apiKeyHelper"
		} else if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != "" {
			out.AuthMethod = "api_key"
			out.APIKeySource = "ANTHROPIC_API_KEY"
		} else {
			out.AuthMethod = "api_key"
		}
	default:
		out.AuthMethod = "none"
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(stdout, `{"loggedIn":%v,"authMethod":"none"}`, loggedIn)
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", data)
	if loggedIn {
		return 0
	}
	return 1
}

func runPluginCLI(ctx context.Context, state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	_ = ctx
	if len(args) == 0 {
		fmt.Fprintln(stderr, "ccgo plugin: missing subcommand")
		fmt.Fprintln(stderr, pluginCLIUsage())
		return 2
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, pluginCLIUsage())
		return 0
	case "list", "ls":
		return runPluginListCLI(state, args[1:], stdout, stderr)
	case "install", "i":
		return runPluginInstallCLI(state, args[1:], stdout, stderr)
	case "update":
		return runPluginUpdateCLI(state, args[1:], stdout, stderr)
	case "uninstall", "remove", "rm":
		return runPluginUninstallCLI(state, args[1:], stdout, stderr)
	case "validate":
		return runPluginValidateCLI(state, args[1:], stdout, stderr)
	case "enable":
		return runPluginSetEnabledCLI(state, "enable", args[1:], stdout, stderr)
	case "disable":
		return runPluginSetEnabledCLI(state, "disable", args[1:], stdout, stderr)
	case "marketplace", "marketplaces":
		return runPluginMarketplaceCLI(state, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "ccgo plugin: unknown subcommand %s\n", args[0])
		fmt.Fprintln(stderr, pluginCLIUsage())
		return 2
	}
}

func pluginCLIUsage() string {
	return "Usage: claude plugin <list|install|update|uninstall|validate|enable|disable|marketplace>"
}

func runPluginListCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "output JSON")
	available := flags.Bool("available", false, "include available marketplace plugins")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "ccgo plugin list: unexpected argument %s\n", flags.Arg(0))
		return 2
	}
	if *available && !*jsonOutput {
		fmt.Fprintln(stderr, "ccgo plugin list: --available requires --json")
		return 2
	}
	runner, err := state.ConversationRunner()
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin list: %v\n", err)
		return 1
	}
	settings := runnerMergedSettings(runner)
	installedRoots := pluginpkg.InstalledPluginDirs(runner.WorkingDirectory)
	installedPlugins := pluginpkg.LoadPluginDirs(installedRoots)
	installed := pluginCLIInstalledEntriesFromRoots(installedRoots, installedPlugins, settings, runner.WorkingDirectory)
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if *available {
			payload := pluginCLIAvailableList{
				Installed: installed,
				Available: pluginCLIAvailableEntries(pluginpkg.LoadMarketplacePluginDirsWithSettings(settings), installedPlugins),
			}
			if err := encoder.Encode(payload); err != nil {
				fmt.Fprintf(stderr, "ccgo plugin list: %v\n", err)
				return 1
			}
			return 0
		}
		if err := encoder.Encode(installed); err != nil {
			fmt.Fprintf(stderr, "ccgo plugin list: %v\n", err)
			return 1
		}
		return 0
	}
	if len(installed) == 0 {
		fmt.Fprintln(stdout, "No plugins installed. Use `claude plugin install` to install a plugin.")
		return 0
	}
	fmt.Fprintln(stdout, "Installed plugins:")
	for _, plugin := range installed {
		stateText := "disabled"
		if plugin.Enabled {
			stateText = "enabled"
		}
		if len(plugin.Errors) > 0 {
			stateText = "failed to load"
		}
		fmt.Fprintf(stdout, "- %s %s (%s, %s)\n", plugin.ID, plugin.Version, plugin.Scope, stateText)
		for _, loadErr := range plugin.Errors {
			fmt.Fprintf(stdout, "  Error: %s\n", loadErr)
		}
	}
	return 0
}

func runPluginInstallCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	scope := ""
	flags.StringVar(&scope, "scope", scope, "installation scope")
	flags.StringVar(&scope, "s", scope, "installation scope")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "ccgo plugin install: usage: claude plugin install [--scope project|user|local] <plugin>")
		return 2
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope != "" && scope != "project" && scope != "user" && scope != "local" {
		fmt.Fprintf(stderr, "ccgo plugin install: scope %q is not supported; use project, user, or local\n", scope)
		return 2
	}
	settings, err := pluginCLISettingsFromFiles(state.CWD())
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin install: %v\n", err)
		return 1
	}
	pluginArg := strings.TrimSpace(flags.Arg(0))
	sourceType := pluginCLIInferMarketplaceSourceType(pluginArg)

	// PLUGIN-21: direct npm install (packageName starts with @ or inferred as npm).
	// PLUGIN-22: direct GitHub shorthand install (owner/repo format).
	// CC ref: src/utils/plugins/pluginLoader.ts:installFromNpm / installFromGitHub.
	switch sourceType {
	case "npm":
		return runPluginInstallDirectNpm(pluginArg, state.CWD(), scope, stdout, stderr)
	case "github":
		return runPluginInstallDirectGitHub(pluginArg, state.CWD(), scope, stdout, stderr)
	default:
		// Fall through to marketplace lookup.
	}

	result, err := pluginpkg.InstallMarketplacePluginInScope(pluginArg, state.CWD(), scope, settings)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin install: %v\n", err)
		return 1
	}
	writePluginInstallResult(stdout, result)
	return 0
}

// runPluginInstallDirectNpm installs a plugin from npm directly.
// PLUGIN-21. ⚠️ Requires live npm binary — tested with stub in plugins package.
func runPluginInstallDirectNpm(packageName string, cwd string, scope string, stdout io.Writer, stderr io.Writer) int {
	if scope == "" {
		scope = "project"
	}
	targetPluginsDir, err := pluginInstallDirForScope(cwd, scope)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin install: %v\n", err)
		return 1
	}
	// Use the package name (without version) as the directory name.
	dirName := pluginpkg.SafePluginInstallDirName(pluginpkg.LoadedPlugin{Name: packageName})
	targetPath := filepath.Join(targetPluginsDir, dirName)
	if err := pluginpkg.InstallFromNpm(packageName, targetPath, pluginpkg.InstallFromNpmOptions{}); err != nil {
		fmt.Fprintf(stderr, "ccgo plugin install: npm install %s: %v\n", packageName, err)
		return 1
	}
	fmt.Fprintf(stdout, "Installed npm plugin %s → %s\n", packageName, targetPath)
	return 0
}

// runPluginInstallDirectGitHub installs a plugin from a GitHub shorthand.
// PLUGIN-22.
func runPluginInstallDirectGitHub(repo string, cwd string, scope string, stdout io.Writer, stderr io.Writer) int {
	if scope == "" {
		scope = "project"
	}
	targetPluginsDir, err := pluginInstallDirForScope(cwd, scope)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin install: %v\n", err)
		return 1
	}
	// Use the repository name part (after last /) as directory name.
	parts := strings.Split(repo, "/")
	dirBase := parts[len(parts)-1]
	if dirBase == "" {
		dirBase = "plugin"
	}
	dirName := pluginpkg.SafePluginInstallDirName(pluginpkg.LoadedPlugin{Name: dirBase})
	targetPath := filepath.Join(targetPluginsDir, dirName)
	if err := pluginpkg.InstallFromGitHub(repo, targetPath, "", ""); err != nil {
		fmt.Fprintf(stderr, "ccgo plugin install: github install %s: %v\n", repo, err)
		return 1
	}
	fmt.Fprintf(stdout, "Installed GitHub plugin %s → %s\n", repo, targetPath)
	return 0
}

// pluginInstallDirForScope returns the plugins directory for a given scope.
func pluginInstallDirForScope(cwd string, scope string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "project", "local", "":
		if cwd == "" {
			return "", fmt.Errorf("working directory is unavailable")
		}
		return filepath.Join(cwd, ".claude", "plugins"), nil
	case "user":
		return filepath.Join(platform.ClaudeHomeDir(), "plugins"), nil
	default:
		return "", fmt.Errorf("scope %q is not supported; use project, user, or local", scope)
	}
}

func runPluginUpdateCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	scope := ""
	flags.StringVar(&scope, "scope", scope, "installation scope")
	flags.StringVar(&scope, "s", scope, "installation scope")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "ccgo plugin update: usage: claude plugin update [--scope project|user|local|all] <plugin>")
		return 2
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope != "" && scope != "project" && scope != "user" && scope != "local" && scope != "all" {
		fmt.Fprintf(stderr, "ccgo plugin update: scope %q is not supported; use project, user, local, or all\n", scope)
		return 2
	}
	settings, err := pluginCLISettingsFromFiles(state.CWD())
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin update: %v\n", err)
		return 1
	}
	result, err := pluginpkg.UpdateInstalledMarketplacePluginsInScope(strings.TrimSpace(flags.Arg(0)), state.CWD(), scope, settings)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin update: %v\n", err)
		return 1
	}
	writePluginUpdateResult(stdout, result)
	return 0
}

func runPluginValidateCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "ccgo plugin validate: usage: claude plugin validate <path>")
		return 2
	}
	result, err := pluginpkg.ValidateManifestPath(flags.Arg(0), state.CWD())
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin validate: %v\n", err)
		return 2
	}
	writePluginValidationResult(stdout, result)
	allSuccess := result.Success
	if pluginRoot, ok := pluginCLIValidationContentRoot(result); ok {
		for _, contentResult := range pluginpkg.ValidatePluginContents(pluginRoot) {
			fmt.Fprintln(stdout)
			writePluginValidationResult(stdout, contentResult)
			if !contentResult.Success {
				allSuccess = false
			}
		}
	}
	if !allSuccess {
		return 1
	}
	return 0
}

func runPluginUninstallCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin uninstall", flag.ContinueOnError)
	flags.SetOutput(stderr)
	scope := "user"
	keepData := false
	flags.StringVar(&scope, "scope", scope, "installation scope")
	flags.StringVar(&scope, "s", scope, "installation scope")
	flags.BoolVar(&keepData, "keep-data", keepData, "keep plugin data")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "ccgo plugin uninstall: usage: claude plugin uninstall [--scope user|project|local] [--keep-data] <plugin>")
		return 2
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope != "project" && scope != "user" && scope != "local" {
		fmt.Fprintf(stderr, "ccgo plugin uninstall: scope %q is not supported; use project, user, or local\n", scope)
		return 2
	}
	result, err := pluginpkg.UninstallInstalledPluginInScope(strings.TrimSpace(flags.Arg(0)), state.CWD(), scope)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin uninstall: %v\n", err)
		return 1
	}
	writePluginUninstallResult(stdout, result, keepData)
	return 0
}

func runPluginSetEnabledCLI(state *bootstrap.State, action string, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	scope := "user"
	flags.StringVar(&scope, "scope", scope, "settings scope")
	flags.StringVar(&scope, "s", scope, "settings scope")
	all := false
	if action == "disable" {
		flags.BoolVar(&all, "all", false, "disable all plugins")
		flags.BoolVar(&all, "a", false, "disable all plugins")
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "user"
	}
	settingsPath, err := pluginCLISettingsPathForScope(state, scope)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin %s: %v\n", action, err)
		return 2
	}
	if all {
		if flags.NArg() > 0 {
			fmt.Fprintln(stderr, "ccgo plugin disable: cannot use --all with a specific plugin")
			return 2
		}
		return runPluginDisableAllCLI(state, settingsPath, stdout, stderr)
	}
	if flags.NArg() != 1 {
		fmt.Fprintf(stderr, "ccgo plugin %s: usage: claude plugin %s [--scope user|project|local] <plugin>\n", action, action)
		return 2
	}
	name := strings.TrimSpace(flags.Arg(0))
	enabled := action == "enable"
	if err := config.SetPluginEnabledInSettingsFile(settingsPath, name, enabled); err != nil {
		fmt.Fprintf(stderr, "ccgo plugin %s: %v\n", action, err)
		return 1
	}
	stateText := "disabled"
	if enabled {
		stateText = "enabled"
	}
	fmt.Fprintf(stdout, "Plugin %s %s.\n", name, stateText)
	return 0
}

func runPluginDisableAllCLI(state *bootstrap.State, settingsPath string, stdout io.Writer, stderr io.Writer) int {
	settings, err := pluginCLISettingsFromFiles(state.CWD())
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin disable: %v\n", err)
		return 1
	}
	plugins := pluginpkg.LoadPluginDirs(pluginpkg.InstalledPluginDirs(state.CWD()))
	states := map[string]bool{}
	for _, plugin := range plugins {
		if pluginpkg.PluginEnabled(plugin, settings.EnabledPlugins) && strings.TrimSpace(plugin.Name) != "" {
			states[plugin.Name] = false
		}
	}
	if len(states) == 0 {
		fmt.Fprintln(stdout, "No enabled plugins to disable")
		return 0
	}
	if err := config.SetPluginsEnabledInSettingsFile(settingsPath, states); err != nil {
		fmt.Fprintf(stderr, "ccgo plugin disable: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Disabled %d %s\n", len(states), pluralWord(len(states), "plugin", "plugins"))
	return 0
}

func pluginCLISettingsPathForScope(state *bootstrap.State, scope string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "user":
		return config.UserSettingsPath(), nil
	case "project":
		return config.ProjectSettingsPath(state.CWD()), nil
	case "local":
		return config.LocalSettingsPath(state.CWD()), nil
	default:
		return "", fmt.Errorf("scope %q is not supported; use user, project, or local", scope)
	}
}

func pluralWord(count int, singular string, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func runPluginMarketplaceCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "ccgo plugin marketplace: missing subcommand")
		fmt.Fprintln(stderr, pluginMarketplaceCLIUsage())
		return 2
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, pluginMarketplaceCLIUsage())
		return 0
	case "add":
		return runPluginMarketplaceAddCLI(state, args[1:], stdout, stderr)
	case "list", "ls":
		return runPluginMarketplaceListCLI(state, args[1:], stdout, stderr)
	case "remove", "rm":
		return runPluginMarketplaceRemoveCLI(state, args[1:], stdout, stderr)
	case "update", "refresh", "reload":
		return runPluginMarketplaceUpdateCLI(state, args[1:], stdout, stderr)
	case "plugins", "available", "browse", "discover", "search", "find":
		return runPluginMarketplacePluginsCLI(state, args[1:], stdout, stderr)
	case "show", "info":
		return runPluginMarketplaceShowCLI(state, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "ccgo plugin marketplace: unknown subcommand %s\n", args[0])
		fmt.Fprintln(stderr, pluginMarketplaceCLIUsage())
		return 2
	}
}

func pluginMarketplaceCLIUsage() string {
	return "Usage: claude plugin marketplace <list|add|remove|update|plugins|show>"
}

func runPluginMarketplaceListCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin marketplace list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "output JSON")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "ccgo plugin marketplace list: unexpected argument %s\n", flags.Arg(0))
		return 2
	}
	settings, err := pluginCLISettingsFromFiles(state.CWD())
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin marketplace list: %v\n", err)
		return 1
	}
	marketplaces := pluginCLIMarketplaceEntries(settings)
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(marketplaces); err != nil {
			fmt.Fprintf(stderr, "ccgo plugin marketplace list: %v\n", err)
			return 1
		}
		return 0
	}
	if len(marketplaces) == 0 {
		fmt.Fprintln(stdout, "No marketplaces configured")
		return 0
	}
	fmt.Fprintln(stdout, "Configured marketplaces:")
	for _, marketplace := range marketplaces {
		fmt.Fprintf(stdout, "- %s\n", marketplace.Name)
		if source := pluginCLIMarketplaceSourceText(marketplace); source != "" {
			fmt.Fprintf(stdout, "  Source: %s\n", source)
		}
		if len(marketplace.SparsePaths) > 0 {
			fmt.Fprintf(stdout, "  Sparse paths: %s\n", strings.Join(marketplace.SparsePaths, ", "))
		}
		if marketplace.InstallLocation != "" {
			fmt.Fprintf(stdout, "  Install location: %s\n", marketplace.InstallLocation)
		}
	}
	return 0
}

func runPluginMarketplaceAddCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin marketplace add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	scope := "user"
	sourceType := ""
	installLocation := ""
	var sparsePaths repeatedStringFlag
	flags.StringVar(&scope, "scope", scope, "settings scope")
	flags.StringVar(&scope, "s", scope, "settings scope")
	flags.StringVar(&sourceType, "type", sourceType, "marketplace source type")
	flags.StringVar(&sourceType, "t", sourceType, "marketplace source type")
	flags.StringVar(&installLocation, "install-location", installLocation, "preferred install location")
	flags.Var(&sparsePaths, "sparse", "sparse checkout path for github/git marketplace sources")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 2 {
		fmt.Fprintln(stderr, "ccgo plugin marketplace add: usage: claude plugin marketplace add [--scope user|project|local] [--type url|github|git|npm|directory|file] [--sparse path] <name> <source>")
		return 2
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "user"
	}
	settingsPath, err := pluginCLISettingsPathForScope(state, scope)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin marketplace add: %v\n", err)
		return 2
	}
	name := strings.TrimSpace(flags.Arg(0))
	source, err := pluginCLIMarketplaceSourceFromArg(sourceType, strings.TrimSpace(flags.Arg(1)))
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin marketplace add: %v\n", err)
		return 2
	}
	if len(sparsePaths) > 0 {
		sourceKind := pluginCLIStringFromMap(source, "source")
		if sourceKind != "github" && sourceKind != "git" {
			fmt.Fprintf(stderr, "ccgo plugin marketplace add: --sparse is only supported for github and git marketplace sources (got: %s)\n", sourceKind)
			return 2
		}
		source["sparsePaths"] = compactPluginCLISparsePaths(sparsePaths)
	}
	existed, err := config.SetMarketplaceInSettingsFile(settingsPath, name, source, installLocation)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin marketplace add: %v\n", err)
		return 1
	}
	action := "added"
	if existed {
		action = "updated"
	}
	fmt.Fprintf(stdout, "Marketplace %s %s.\n", name, action)
	return 0
}

func runPluginMarketplaceRemoveCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin marketplace remove", flag.ContinueOnError)
	flags.SetOutput(stderr)
	scope := "user"
	flags.StringVar(&scope, "scope", scope, "settings scope")
	flags.StringVar(&scope, "s", scope, "settings scope")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "ccgo plugin marketplace remove: usage: claude plugin marketplace remove [--scope user|project|local] <name>")
		return 2
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "user"
	}
	settingsPath, err := pluginCLISettingsPathForScope(state, scope)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin marketplace remove: %v\n", err)
		return 2
	}
	name := strings.TrimSpace(flags.Arg(0))
	removed, err := config.RemoveMarketplaceFromSettingsFile(settingsPath, name)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin marketplace remove: %v\n", err)
		return 1
	}
	if !removed {
		fmt.Fprintf(stderr, "ccgo plugin marketplace remove: marketplace %q not found in %s settings\n", name, scope)
		return 1
	}
	fmt.Fprintf(stdout, "Marketplace %s removed.\n", name)
	return 0
}

func runPluginMarketplaceUpdateCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin marketplace update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintf(stderr, "ccgo plugin marketplace update: unexpected argument %s\n", flags.Arg(1))
		return 2
	}
	settings, err := pluginCLISettingsFromFiles(state.CWD())
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin marketplace update: %v\n", err)
		return 1
	}
	marketplaces := pluginCLIMarketplaceEntries(settings)
	name := strings.TrimSpace(flags.Arg(0))
	if name == "" {
		if len(marketplaces) == 0 {
			fmt.Fprintln(stdout, "No marketplaces configured")
			return 0
		}
		fmt.Fprintf(stdout, "Updating %d marketplace(s)...\n", len(marketplaces))
		pluginpkg.LoadMarketplacePluginDirsWithSettings(settings)
		fmt.Fprintf(stdout, "Successfully updated %d marketplace(s)\n", len(marketplaces))
		return 0
	}
	filtered, matchedName, ok := pluginCLIMarketplaceSettingsForName(settings, name)
	if !ok {
		fmt.Fprintf(stderr, "ccgo plugin marketplace update: marketplace %q not found. Available marketplaces: %s\n", name, pluginCLIMarketplaceAvailableNames(marketplaces))
		return 1
	}
	fmt.Fprintf(stdout, "Updating marketplace: %s...\n", matchedName)
	pluginpkg.LoadMarketplacePluginDirsWithSettings(filtered)
	fmt.Fprintf(stdout, "Successfully updated marketplace: %s\n", matchedName)
	return 0
}

func runPluginMarketplacePluginsCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin marketplace plugins", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "output JSON")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintf(stderr, "ccgo plugin marketplace plugins: unexpected argument %s\n", flags.Arg(1))
		return 2
	}
	query := ""
	if flags.NArg() == 1 {
		query = strings.TrimSpace(flags.Arg(0))
	}
	runner, err := state.ConversationRunner()
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin marketplace plugins: %v\n", err)
		return 1
	}
	settings := runnerMergedSettings(runner)
	marketplacePlugins := pluginpkg.LoadMarketplacePluginDirsWithSettings(settings)
	installedPlugins := pluginpkg.LoadPluginDirs(pluginpkg.InstalledPluginDirs(runner.WorkingDirectory))
	entries := pluginCLIMarketplacePluginEntries(marketplacePlugins, installedPlugins, query)
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(entries); err != nil {
			fmt.Fprintf(stderr, "ccgo plugin marketplace plugins: %v\n", err)
			return 1
		}
		return 0
	}
	if len(entries) == 0 {
		if len(marketplacePlugins) == 0 {
			fmt.Fprintln(stdout, "No marketplace plugins available from configured sources.")
			return 0
		}
		fmt.Fprintf(stdout, "No marketplace plugins matched %s.\n", query)
		return 0
	}
	fmt.Fprintln(stdout, "Marketplace plugins:")
	fmt.Fprintf(stdout, "Marketplace plugins: %d\n", len(marketplacePlugins))
	fmt.Fprintf(stdout, "Matches: %d\n", len(entries))
	if query != "" {
		fmt.Fprintf(stdout, "Query: %s\n", query)
	}
	for _, plugin := range entries {
		name := plugin.Name
		if plugin.Version != "" {
			name += "@" + plugin.Version
		}
		if plugin.MarketplaceName != "" {
			name += " [" + plugin.MarketplaceName + "]"
		}
		status := plugin.Status
		if status == "" {
			status = "available"
		}
		if status == "update available" && plugin.InstalledVersion != "" {
			status += ": installed " + plugin.InstalledVersion
		}
		line := "- " + name + " (" + status + ")"
		if description := pluginCLIShortDescription(plugin.Description); description != "" {
			line += ": " + description
		}
		fmt.Fprintln(stdout, line)
	}
	return 0
}

func runPluginMarketplaceShowCLI(state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude plugin marketplace show", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "output JSON")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	name := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if name == "" {
		fmt.Fprintln(stderr, "ccgo plugin marketplace show: usage: claude plugin marketplace show [--json] <plugin>")
		return 2
	}
	runner, err := state.ConversationRunner()
	if err != nil {
		fmt.Fprintf(stderr, "ccgo plugin marketplace show: %v\n", err)
		return 1
	}
	settings := runnerMergedSettings(runner)
	marketplacePlugins := pluginpkg.LoadMarketplacePluginDirsWithSettings(settings)
	installedPlugins := pluginpkg.LoadPluginDirs(pluginpkg.InstalledPluginDirs(runner.WorkingDirectory))
	entries := pluginCLIMarketplacePluginEntries(marketplacePlugins, installedPlugins, "")
	entry, ok := pluginCLIMarketplaceFindEntry(entries, name)
	if !ok {
		fmt.Fprintf(stderr, "ccgo plugin marketplace show: plugin %q not found in configured marketplaces\n", name)
		return 1
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(entry); err != nil {
			fmt.Fprintf(stderr, "ccgo plugin marketplace show: %v\n", err)
			return 1
		}
		return 0
	}
	writePluginMarketplaceShowResult(stdout, entry)
	return 0
}

func writePluginMarketplaceShowResult(stdout io.Writer, plugin pluginCLIAvailableEntry) {
	status := plugin.Status
	if status == "" {
		status = "available"
	}
	lines := []string{
		"Marketplace plugin",
		"Name: " + plugin.Name,
	}
	if plugin.PluginID != "" {
		lines = append(lines, "Plugin ID: "+plugin.PluginID)
	}
	if plugin.MarketplaceName != "" {
		lines = append(lines, "Marketplace: "+plugin.MarketplaceName)
	}
	if plugin.Version != "" {
		lines = append(lines, "Version: "+plugin.Version)
	}
	lines = append(lines, "Status: "+status)
	if plugin.InstalledVersion != "" {
		lines = append(lines, "Installed version: "+plugin.InstalledVersion)
	}
	if plugin.InstallPath != "" {
		lines = append(lines, "Installed path: "+plugin.InstallPath)
	}
	if plugin.Source != "" {
		lines = append(lines, "Source: "+plugin.Source)
	}
	if description := strings.TrimSpace(plugin.Description); description != "" {
		lines = append(lines, "Description: "+description)
	}
	fmt.Fprintln(stdout, strings.Join(lines, "\n"))
}

func writePluginInstallResult(stdout io.Writer, result pluginpkg.PluginInstallResult) {
	lines := []string{
		"Plugin installed",
		"Name: " + result.Plugin.Name,
		"Source: " + result.Plugin.Root,
		"Installed path: " + result.TargetPath,
	}
	if result.Plugin.Marketplace != "" {
		lines = append(lines, "Marketplace: "+result.Plugin.Marketplace)
	}
	if result.AlreadyInstalled {
		lines = append(lines, "Status: already installed")
	} else {
		lines = append(lines, "Status: installed")
	}
	fmt.Fprintln(stdout, strings.Join(lines, "\n"))
}

func writePluginUpdateResult(stdout io.Writer, result pluginpkg.PluginUpdateResult) {
	lines := []string{
		"Plugin update",
		fmt.Sprintf("Marketplace plugins: %d", result.MarketplacePluginCount),
		fmt.Sprintf("Updated plugins: %d", len(result.Updated)),
	}
	if len(result.Updated) > 0 {
		lines = append(lines, "Updated:")
		for _, item := range result.Updated {
			lines = append(lines, "- "+item.Plugin.Name+" -> "+item.TargetPath)
		}
	}
	fmt.Fprintln(stdout, strings.Join(lines, "\n"))
}

func writePluginUninstallResult(stdout io.Writer, result pluginpkg.PluginUninstallResult, keepData bool) {
	lines := []string{
		"Plugin uninstalled",
		"Name: " + result.Plugin.Name,
		"Removed path: " + result.TargetPath,
		"Scope: " + result.Scope,
		"Status: uninstalled",
	}
	if result.Plugin.Marketplace != "" {
		lines = append(lines, "Marketplace: "+result.Plugin.Marketplace)
	}
	if keepData {
		lines = append(lines, "Data: kept")
	}
	fmt.Fprintln(stdout, strings.Join(lines, "\n"))
}

func writePluginValidationResult(stdout io.Writer, result pluginpkg.ManifestValidationResult) {
	lines := []string{
		pluginCLIValidationHeader(result),
		"",
	}
	if len(result.Errors) > 0 {
		lines = append(lines, fmt.Sprintf("Found %d %s:", len(result.Errors), pluralWord(len(result.Errors), "error", "errors")))
		lines = append(lines, "")
		for _, item := range result.Errors {
			lines = append(lines, fmt.Sprintf("- %s: %s", pluginCLIValidationMessagePath(item.Path), item.Message))
		}
		lines = append(lines, "")
	}
	if len(result.Warnings) > 0 {
		lines = append(lines, fmt.Sprintf("Found %d %s:", len(result.Warnings), pluralWord(len(result.Warnings), "warning", "warnings")))
		lines = append(lines, "")
		for _, item := range result.Warnings {
			lines = append(lines, fmt.Sprintf("- %s: %s", pluginCLIValidationMessagePath(item.Path), item.Message))
		}
		lines = append(lines, "")
	}
	if result.Success && result.FileType == "plugin" {
		lines = append(lines, "Plugin: "+result.Plugin.Name)
		if result.Plugin.Version != "" {
			lines = append(lines, "Version: "+result.Plugin.Version)
		}
		lines = append(lines,
			fmt.Sprintf("Commands: %d", len(result.Plugin.Commands)+len(result.Plugin.PromptTemplates)),
			fmt.Sprintf("Skills: %d", len(result.Plugin.SkillCommands)),
			fmt.Sprintf("Agents: %d", len(result.Plugin.Agents)),
			fmt.Sprintf("MCP servers: %d", len(result.Plugin.MCPServers)),
			fmt.Sprintf("Output styles: %d", len(result.Plugin.OutputStyles)),
			fmt.Sprintf("Hooks: %d", len(result.Plugin.HookEvents)),
			"",
		)
	}
	if result.Success && result.FileType == "marketplace" {
		lines = append(lines, fmt.Sprintf("Marketplace plugins: %d", result.PluginCount))
		for _, name := range firstPluginCLIStrings(result.MarketplaceIDs, 10) {
			lines = append(lines, "- "+name)
		}
		if len(result.MarketplaceIDs) > 10 {
			lines = append(lines, fmt.Sprintf("Showing 10 of %d marketplace plugins.", len(result.MarketplaceIDs)))
		}
		lines = append(lines, "")
	}
	if result.Success {
		if len(result.Warnings) > 0 {
			lines = append(lines, "Validation passed with warnings")
		} else {
			lines = append(lines, "Validation passed")
		}
	} else {
		lines = append(lines, "Validation failed")
	}
	fmt.Fprintln(stdout, strings.TrimRight(strings.Join(lines, "\n"), "\n"))
}

func firstPluginCLIStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func pluginCLIValidationMessagePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "root"
	}
	return path
}

func pluginCLIValidationHeader(result pluginpkg.ManifestValidationResult) string {
	switch result.FileType {
	case "plugin", "marketplace":
		return fmt.Sprintf("Validating %s manifest: %s", result.FileType, result.FilePath)
	default:
		return fmt.Sprintf("Validating %s: %s", result.FileType, result.FilePath)
	}
}

func pluginCLIValidationContentRoot(result pluginpkg.ManifestValidationResult) (string, bool) {
	if result.FileType != "plugin" {
		return "", false
	}
	manifestDir := filepath.Dir(result.FilePath)
	if !strings.EqualFold(filepath.Base(manifestDir), ".claude-plugin") {
		return "", false
	}
	return filepath.Dir(manifestDir), true
}

func pluginCLISettingsFromFiles(cwd string) (contracts.Settings, error) {
	userSettings, err := pluginCLILoadOptionalSettings(config.UserSettingsPath())
	if err != nil {
		return contracts.Settings{}, err
	}
	projectSettings, err := pluginCLILoadOptionalSettings(config.ProjectSettingsPath(cwd))
	if err != nil {
		return contracts.Settings{}, err
	}
	localSettings, err := pluginCLILoadOptionalSettings(config.LocalSettingsPath(cwd))
	if err != nil {
		return contracts.Settings{}, err
	}
	policySettings, err := config.LoadPolicySettings()
	if err != nil {
		return contracts.Settings{}, err
	}
	return config.MergeSettings(userSettings, projectSettings, localSettings, policySettings), nil
}

func pluginCLILoadOptionalSettings(path string) (contracts.Settings, error) {
	settings, err := config.LoadSettingsFile(path)
	if err == nil {
		return settings, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return contracts.Settings{}, nil
	}
	return contracts.Settings{}, fmt.Errorf("load settings %s: %w", path, err)
}

func pluginCLIInstalledEntries(plugins []pluginpkg.LoadedPlugin, settings contracts.Settings, cwd string) []pluginCLIListEntry {
	out := make([]pluginCLIListEntry, 0, len(plugins))
	for _, plugin := range plugins {
		entry := pluginCLIListEntry{
			ID:          pluginCLIID(plugin),
			Version:     pluginCLIVersion(plugin.Version),
			Scope:       pluginpkg.InstalledPluginScope(cwd, plugin.Root),
			Enabled:     pluginpkg.PluginEnabled(plugin, settings.EnabledPlugins),
			InstallPath: plugin.Root,
		}
		if len(plugin.MCPServers) > 0 {
			entry.MCPServers = pluginCLIMCPServerNames(plugin.MCPServers)
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func pluginCLIInstalledEntriesFromRoots(roots []string, plugins []pluginpkg.LoadedPlugin, settings contracts.Settings, cwd string) []pluginCLIListEntry {
	out := pluginCLIInstalledEntries(plugins, settings, cwd)
	seen := map[string]struct{}{}
	for _, entry := range out {
		seen[pluginCLIPathKey(entry.InstallPath)] = struct{}{}
	}
	for _, root := range roots {
		key := pluginCLIPathKey(root)
		if _, ok := seen[key]; ok {
			continue
		}
		_, err := pluginpkg.LoadPluginDir(root)
		if err == nil {
			continue
		}
		out = append(out, pluginCLIListEntry{
			ID:          filepath.Base(root) + "@local",
			Version:     "unknown",
			Scope:       pluginpkg.InstalledPluginScope(cwd, root),
			Enabled:     false,
			InstallPath: pluginCLICleanAbs(root),
			Errors:      []string{err.Error()},
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].InstallPath < out[j].InstallPath
	})
	return out
}

func pluginCLIPathKey(path string) string {
	return strings.ToLower(filepath.ToSlash(pluginCLICleanAbs(path)))
}

func pluginCLICleanAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func pluginCLIAvailableEntries(marketplacePlugins []pluginpkg.LoadedPlugin, installedPlugins []pluginpkg.LoadedPlugin) []pluginCLIAvailableEntry {
	installed := map[string]struct{}{}
	for _, plugin := range installedPlugins {
		if key := pluginCLINameKey(plugin.Name); key != "" {
			installed[key] = struct{}{}
		}
	}
	var out []pluginCLIAvailableEntry
	for _, plugin := range marketplacePlugins {
		if _, ok := installed[pluginCLINameKey(plugin.Name)]; ok {
			continue
		}
		out = append(out, pluginCLIAvailableEntry{
			PluginID:        pluginCLIID(plugin),
			Name:            plugin.Name,
			Description:     strings.TrimSpace(plugin.Description),
			MarketplaceName: strings.TrimSpace(plugin.Marketplace),
			Version:         strings.TrimSpace(plugin.Version),
			Source:          plugin.Root,
			Status:          "available",
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PluginID != out[j].PluginID {
			return out[i].PluginID < out[j].PluginID
		}
		return out[i].Source < out[j].Source
	})
	return out
}

func pluginCLIMarketplacePluginEntries(marketplacePlugins []pluginpkg.LoadedPlugin, installedPlugins []pluginpkg.LoadedPlugin, query string) []pluginCLIAvailableEntry {
	installed := map[string]pluginpkg.LoadedPlugin{}
	for _, plugin := range installedPlugins {
		if key := pluginCLINameKey(plugin.Name); key != "" {
			installed[key] = plugin
		}
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var out []pluginCLIAvailableEntry
	for _, plugin := range marketplacePlugins {
		entry := pluginCLIAvailableEntry{
			PluginID:        pluginCLIID(plugin),
			Name:            plugin.Name,
			Description:     strings.TrimSpace(plugin.Description),
			MarketplaceName: strings.TrimSpace(plugin.Marketplace),
			Version:         strings.TrimSpace(plugin.Version),
			Source:          plugin.Root,
			Status:          "available",
		}
		if installedPlugin, ok := installed[pluginCLINameKey(plugin.Name)]; ok {
			entry.Status = "installed"
			entry.InstalledVersion = strings.TrimSpace(installedPlugin.Version)
			entry.InstallPath = pluginCLICleanAbs(installedPlugin.Root)
			if entry.Version != "" && entry.InstalledVersion != "" && entry.Version != entry.InstalledVersion {
				entry.Status = "update available"
			}
		}
		if query != "" && !pluginCLIMarketplaceEntryMatches(entry, query) {
			continue
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PluginID != out[j].PluginID {
			return out[i].PluginID < out[j].PluginID
		}
		return out[i].Source < out[j].Source
	})
	return out
}

func pluginCLIMarketplaceFindEntry(entries []pluginCLIAvailableEntry, name string) (pluginCLIAvailableEntry, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return pluginCLIAvailableEntry{}, false
	}
	for _, entry := range entries {
		for _, candidate := range pluginCLIMarketplaceEntryNames(entry) {
			if strings.EqualFold(candidate, name) {
				return entry, true
			}
		}
	}
	return pluginCLIAvailableEntry{}, false
}

func pluginCLIMarketplaceEntryNames(entry pluginCLIAvailableEntry) []string {
	var names []string
	for _, candidate := range []string{
		entry.PluginID,
		entry.Name,
		entry.Name + "@" + entry.MarketplaceName,
		entry.Name + "@" + entry.Version,
	} {
		candidate = strings.TrimSpace(strings.Trim(candidate, "@"))
		if candidate != "" {
			names = append(names, candidate)
		}
	}
	return names
}

func pluginCLIMarketplaceEntryMatches(entry pluginCLIAvailableEntry, query string) bool {
	fields := []string{
		entry.PluginID,
		entry.Name,
		entry.Description,
		entry.MarketplaceName,
		entry.Version,
		entry.Status,
		entry.InstalledVersion,
		entry.Source,
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func pluginCLIShortDescription(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, line := range strings.Split(value, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func pluginCLIMarketplaceSettingsForName(settings contracts.Settings, name string) (contracts.Settings, string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return settings, "", true
	}
	for marketplaceName, raw := range settings.ExtraKnownMarketplaces {
		if !strings.EqualFold(strings.TrimSpace(marketplaceName), name) {
			continue
		}
		filtered := settings
		filtered.ExtraKnownMarketplaces = map[string]any{marketplaceName: raw}
		return filtered, marketplaceName, true
	}
	return settings, "", false
}

func pluginCLIMarketplaceAvailableNames(marketplaces []pluginCLIMarketplaceEntry) string {
	names := make([]string, 0, len(marketplaces))
	for _, marketplace := range marketplaces {
		if strings.TrimSpace(marketplace.Name) != "" {
			names = append(names, marketplace.Name)
		}
	}
	if len(names) == 0 {
		return "none"
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func pluginCLIMarketplaceEntries(settings contracts.Settings) []pluginCLIMarketplaceEntry {
	names := make([]string, 0, len(settings.ExtraKnownMarketplaces))
	for name := range settings.ExtraKnownMarketplaces {
		if strings.TrimSpace(name) != "" {
			names = append(names, strings.TrimSpace(name))
		}
	}
	sort.Strings(names)
	out := make([]pluginCLIMarketplaceEntry, 0, len(names))
	for _, name := range names {
		rawEntry, _ := settings.ExtraKnownMarketplaces[name].(map[string]any)
		source := pluginCLIMarketplaceSource(rawEntry)
		sourceType := strings.TrimSpace(pluginCLIStringFromMap(source, "source"))
		entry := pluginCLIMarketplaceEntry{
			Name:            name,
			Source:          sourceType,
			InstallLocation: strings.TrimSpace(pluginCLIStringFromMap(rawEntry, "installLocation")),
		}
		switch sourceType {
		case "github":
			entry.Repo = pluginCLIStringFromMap(source, "repo")
			entry.SparsePaths = pluginCLIStringSliceFromMap(source, "sparsePaths")
		case "git":
			entry.URL = pluginCLIStringFromMap(source, "url")
			entry.SparsePaths = pluginCLIStringSliceFromMap(source, "sparsePaths")
		case "url":
			entry.URL = pluginCLIStringFromMap(source, "url")
		case "directory", "file":
			entry.Path = pluginCLIStringFromMap(source, "path")
		case "npm":
			entry.Package = pluginCLIStringFromMap(source, "package")
		}
		out = append(out, entry)
	}
	return out
}

func pluginCLIMarketplaceSource(rawEntry map[string]any) map[string]any {
	if len(rawEntry) == 0 {
		return nil
	}
	if source, ok := rawEntry["source"].(map[string]any); ok {
		return source
	}
	return rawEntry
}

func pluginCLIMarketplaceSourceText(marketplace pluginCLIMarketplaceEntry) string {
	switch marketplace.Source {
	case "github":
		if marketplace.Repo != "" {
			return "GitHub (" + marketplace.Repo + ")"
		}
	case "git":
		if marketplace.URL != "" {
			return "Git (" + marketplace.URL + ")"
		}
	case "url":
		if marketplace.URL != "" {
			return "URL (" + marketplace.URL + ")"
		}
	case "directory":
		if marketplace.Path != "" {
			return "Directory (" + marketplace.Path + ")"
		}
	case "file":
		if marketplace.Path != "" {
			return "File (" + marketplace.Path + ")"
		}
	case "npm":
		if marketplace.Package != "" {
			return "NPM (" + marketplace.Package + ")"
		}
	case "settings":
		return "Settings"
	}
	return strings.TrimSpace(marketplace.Source)
}

func pluginCLIMarketplaceSourceFromArg(sourceType string, value string) (map[string]any, error) {
	sourceType, value = pluginCLINormalizeMarketplaceSourceArg(sourceType, value)
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("marketplace source is required")
	}
	if sourceType == "" {
		sourceType = pluginCLIInferMarketplaceSourceType(value)
	}
	switch sourceType {
	case "url":
		return map[string]any{"source": "url", "url": value}, nil
	case "github":
		return map[string]any{"source": "github", "repo": value}, nil
	case "git":
		return map[string]any{"source": "git", "url": value}, nil
	case "npm":
		return map[string]any{"source": "npm", "package": value}, nil
	case "directory":
		return map[string]any{"source": "directory", "path": value}, nil
	case "file":
		return map[string]any{"source": "file", "path": value}, nil
	default:
		return nil, fmt.Errorf("unsupported marketplace source type %q; use --type url|github|git|npm|directory|file", sourceType)
	}
}

func pluginCLINormalizeMarketplaceSourceArg(sourceType string, value string) (string, string) {
	sourceType = strings.ToLower(strings.TrimSpace(sourceType))
	value = strings.TrimSpace(value)
	if sourceType != "" {
		return sourceType, value
	}
	for _, prefix := range []string{"github:", "git:", "npm:", "directory:", "file:", "url:"} {
		if !strings.HasPrefix(strings.ToLower(value), prefix) {
			continue
		}
		return strings.TrimSuffix(prefix, ":"), strings.TrimSpace(value[len(prefix):])
	}
	return "", value
}

func compactPluginCLISparsePaths(paths []string) []any {
	paths = compactPluginCLIStrings(paths)
	out := make([]any, 0, len(paths))
	for _, path := range paths {
		out = append(out, path)
	}
	return out
}

func pluginCLIInferMarketplaceSourceType(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if strings.HasSuffix(lower, ".git") {
			return "git"
		}
		return "url"
	}
	if strings.HasPrefix(value, "git@") || strings.HasSuffix(lower, ".git") {
		return "git"
	}
	if info, err := os.Stat(value); err == nil {
		if info.IsDir() {
			return "directory"
		}
		return "file"
	}
	if strings.HasPrefix(value, "@") {
		return "npm"
	}
	if strings.Count(value, "/") == 1 && !strings.ContainsAny(value, `\ :`) && !strings.HasPrefix(value, ".") {
		return "github"
	}
	return ""
}

func pluginCLIID(plugin pluginpkg.LoadedPlugin) string {
	name := strings.TrimSpace(plugin.Name)
	if name == "" {
		name = filepath.Base(plugin.Root)
	}
	marketplace := strings.TrimSpace(plugin.Marketplace)
	if marketplace == "" {
		marketplace = "local"
	}
	return name + "@" + marketplace
}

func pluginCLIVersion(version string) string {
	if strings.TrimSpace(version) == "" {
		return "unknown"
	}
	return strings.TrimSpace(version)
}

func pluginCLINameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func pluginCLIStringFromMap(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func pluginCLIStringSliceFromMap(values map[string]any, key string) []string {
	if len(values) == 0 {
		return nil
	}
	switch raw := values[key].(type) {
	case []string:
		return compactPluginCLIStrings(raw)
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if value, ok := item.(string); ok {
				out = append(out, value)
			}
		}
		return compactPluginCLIStrings(out)
	default:
		return nil
	}
}

func compactPluginCLIStrings(values []string) []string {
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
	return out
}

func pluginCLIMCPServerNames(servers map[string]contracts.MCPServer) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

var startDaemonProcess = startDaemonProcessDefault

func runDaemonControl(ctx context.Context, state *bootstrap.State, options daemonControlOptions, stdout io.Writer, stderr io.Writer) int {
	actionCount := 0
	for _, enabled := range []bool{options.Status, options.Stop, options.Tick, options.Start, options.Restart} {
		if enabled {
			actionCount++
		}
	}
	if actionCount > 1 {
		fmt.Fprintf(stderr, "ccgo daemon: daemon control actions are mutually exclusive\n")
		return 2
	}
	if options.Start || options.Restart {
		if err := startOrRestartDaemon(ctx, state, options, stdout); err != nil {
			fmt.Fprintf(stderr, "ccgo daemon: %v\n", err)
			return 1
		}
		return 0
	}
	statePath, err := resolveDaemonStatePath(state, options.StatePath)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo daemon: %v\n", err)
		return 1
	}
	if options.Status {
		if err := writeDaemonStatus(stdout, statePath); err != nil {
			fmt.Fprintf(stderr, "ccgo daemon: %v\n", err)
			return 1
		}
		return 0
	}
	if options.Tick {
		if err := tickDaemon(ctx, stdout, statePath); err != nil {
			fmt.Fprintf(stderr, "ccgo daemon: %v\n", err)
			return 1
		}
		return 0
	}
	if options.Stop {
		if err := stopDaemon(ctx, stdout, statePath); err != nil {
			fmt.Fprintf(stderr, "ccgo daemon: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stderr, "ccgo daemon: no daemon control action requested\n")
	return 2
}

func startOrRestartDaemon(ctx context.Context, state *bootstrap.State, options daemonControlOptions, stdout io.Writer) error {
	if options.HeartbeatInterval <= 0 {
		return errors.New("daemon heartbeat interval must be positive")
	}
	runner, err := state.ConversationRunner()
	if err != nil {
		return err
	}
	if runner.SessionPath == "" && runner.SessionID != "" {
		runner.SessionPath = session.TranscriptPath(runner.WorkingDirectory, runner.SessionID)
	}
	existingPath, err := resolveDaemonStatePath(state, options.StatePath)
	if err != nil {
		return err
	}
	existingState, err := daemonpkg.LoadState(existingPath)
	if err != nil {
		return err
	}
	existingRuntime := daemonpkg.RuntimeStateAt(existingState, time.Now().UTC(), 2*time.Minute)
	if options.Restart && existingState.GeneratedAt != "" && existingRuntime == daemonpkg.RuntimeRunning && strings.TrimSpace(existingState.Endpoint) != "" {
		if err := stopDaemon(ctx, io.Discard, existingPath); err != nil {
			return err
		}
	} else if options.Start && existingRuntime == daemonpkg.RuntimeRunning && existingState.GeneratedAt != "" {
		return writeAlreadyRunningDaemon(stdout, existingPath, existingState)
	}

	sessionID := contracts.NewID()
	sessionPath := session.TranscriptPath(runner.WorkingDirectory, sessionID)
	statePath := daemonpkg.SessionStatePath(sessionPath, sessionID)
	pid, err := startDaemonProcess(ctx, daemonProcessStartOptions{
		WorkingDirectory: runner.WorkingDirectory,
		SessionID:        sessionID,
		StatePath:        statePath,
		Heartbeat:        options.HeartbeatInterval,
	})
	if err != nil {
		return err
	}
	startCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	started := waitForDaemonRuntimeState(startCtx, statePath, daemonpkg.RuntimeRunning)
	if started.RuntimeState != daemonpkg.RuntimeRunning {
		return fmt.Errorf("daemon start did not report running state: %s", statePath)
	}
	lines := []string{
		"ccgo daemon started",
		"session_id=" + string(sessionID),
		"state_path=" + statePath,
		"runtime_state=" + daemonpkg.RuntimeRunning,
	}
	if started.PID > 0 {
		lines = append(lines, fmt.Sprintf("pid=%d", started.PID))
	} else if pid > 0 {
		lines = append(lines, fmt.Sprintf("pid=%d", pid))
	}
	if started.Endpoint != "" {
		lines = append(lines, "endpoint="+started.Endpoint)
	}
	if started.GeneratedAt != "" {
		lines = append(lines, "generated_at="+started.GeneratedAt)
	}
	_, err = fmt.Fprintln(stdout, strings.Join(lines, "\n"))
	return err
}

func writeAlreadyRunningDaemon(stdout io.Writer, statePath string, state daemonpkg.State) error {
	lines := []string{
		"ccgo daemon already running",
		"state_path=" + statePath,
		"runtime_state=" + daemonpkg.RuntimeRunning,
	}
	if state.PID > 0 {
		lines = append(lines, fmt.Sprintf("pid=%d", state.PID))
	}
	if state.Endpoint != "" {
		lines = append(lines, "endpoint="+state.Endpoint)
	}
	_, err := fmt.Fprintln(stdout, strings.Join(lines, "\n"))
	return err
}

func startDaemonProcessDefault(ctx context.Context, options daemonProcessStartOptions) (int, error) {
	executable, err := os.Executable()
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(options.WorkingDirectory) == "" {
		return 0, errors.New("daemon working directory is unavailable")
	}
	if options.SessionID == "" {
		return 0, errors.New("daemon session id is unavailable")
	}
	logPath := options.StatePath + ".log"
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return 0, err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()
	args := []string{
		"--cwd", options.WorkingDirectory,
		"--daemon",
		"--daemon-session", string(options.SessionID),
		"--daemon-heartbeat", options.Heartbeat.String(),
	}
	cmd := exec.Command(executable, args...)
	cmd.Dir = options.WorkingDirectory
	cmd.Env = os.Environ()
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if ctx != nil {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		return 0, err
	}
	return pid, nil
}

func resolveDaemonStatePath(state *bootstrap.State, explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return filepath.Clean(strings.TrimSpace(explicit)), nil
	}
	runner, err := state.ConversationRunner()
	if err != nil {
		return "", err
	}
	if runner.SessionPath == "" && runner.SessionID != "" {
		runner.SessionPath = session.TranscriptPath(runner.WorkingDirectory, runner.SessionID)
	}
	discoveredPath, err := daemonpkg.LatestStatePath(session.ProjectDir(runner.WorkingDirectory), time.Now().UTC(), 2*time.Minute)
	if err != nil {
		return "", err
	}
	if discoveredPath != "" {
		return discoveredPath, nil
	}
	statePath := daemonpkg.SessionStatePath(runner.SessionPath, runner.SessionID)
	if statePath == "" {
		return "", errors.New("session state path is unavailable")
	}
	return statePath, nil
}

func writeDaemonStatus(stdout io.Writer, statePath string) error {
	state, err := daemonpkg.LoadState(statePath)
	if err != nil {
		return err
	}
	lines := []string{
		"ccgo daemon status",
		"state_path=" + statePath,
	}
	if state.GeneratedAt == "" {
		lines = append(lines, "runtime_state="+daemonpkg.RuntimeDisabled)
		_, err := fmt.Fprintln(stdout, strings.Join(lines, "\n"))
		return err
	}
	runtimeState := daemonpkg.RuntimeStateAt(state, time.Now().UTC(), 2*time.Minute)
	lines = append(lines, "runtime_state="+runtimeState)
	if state.PID > 0 {
		lines = append(lines, fmt.Sprintf("pid=%d", state.PID))
	}
	if state.Endpoint != "" {
		lines = append(lines, "endpoint="+state.Endpoint)
	}
	if state.StartedAt != "" {
		lines = append(lines, "started_at="+state.StartedAt)
	}
	if state.HeartbeatAt != "" {
		lines = append(lines, "heartbeat_at="+state.HeartbeatAt)
	}
	if state.GeneratedAt != "" {
		lines = append(lines, "generated_at="+state.GeneratedAt)
	}
	if state.Error != "" {
		lines = append(lines, "error="+state.Error)
	}
	_, err = fmt.Fprintln(stdout, strings.Join(lines, "\n"))
	return err
}

func stopDaemon(ctx context.Context, stdout io.Writer, statePath string) error {
	state, err := daemonpkg.LoadState(statePath)
	if err != nil {
		return err
	}
	if state.GeneratedAt == "" {
		return fmt.Errorf("daemon state not found: %s", statePath)
	}
	if strings.TrimSpace(state.Endpoint) == "" {
		return fmt.Errorf("daemon endpoint is unavailable in %s", statePath)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	response, err := postDaemonStop(ctx, state.Endpoint)
	if err != nil {
		return err
	}
	stopped := waitForDaemonRuntimeState(ctx, statePath, daemonpkg.RuntimeDisabled)
	runtimeState := response.RuntimeState
	if stopped.RuntimeState != "" {
		runtimeState = stopped.RuntimeState
	}
	if runtimeState == "" {
		runtimeState = daemonpkg.RuntimeDisabled
	}
	lines := []string{
		"ccgo daemon stopped",
		"state_path=" + statePath,
		"runtime_state=" + runtimeState,
	}
	if stopped.GeneratedAt != "" {
		lines = append(lines, "generated_at="+stopped.GeneratedAt)
	}
	_, err = fmt.Fprintln(stdout, strings.Join(lines, "\n"))
	return err
}

func tickDaemon(ctx context.Context, stdout io.Writer, statePath string) error {
	state, err := daemonpkg.LoadState(statePath)
	if err != nil {
		return err
	}
	if state.GeneratedAt == "" {
		return fmt.Errorf("daemon state not found: %s", statePath)
	}
	if strings.TrimSpace(state.Endpoint) == "" {
		return fmt.Errorf("daemon endpoint is unavailable in %s", statePath)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	response, err := postDaemonTick(ctx, state.Endpoint)
	if err != nil {
		return err
	}
	lines := []string{
		"ccgo daemon tick",
		"state_path=" + statePath,
		"ok=true",
	}
	if response.CheckedAt != "" {
		lines = append(lines, "checked_at="+response.CheckedAt)
	}
	lines = append(lines,
		fmt.Sprintf("triggered_count=%d", response.TriggeredCount),
		fmt.Sprintf("error_count=%d", response.ErrorCount),
	)
	_, err = fmt.Fprintln(stdout, strings.Join(lines, "\n"))
	return err
}

func postDaemonStop(ctx context.Context, endpoint string) (daemonpkg.StopResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/stop", nil)
	if err != nil {
		return daemonpkg.StopResponse{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return daemonpkg.StopResponse{}, err
	}
	defer resp.Body.Close()
	var response daemonpkg.StopResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return daemonpkg.StopResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 || !response.OK {
		if response.Error == "" {
			response.Error = resp.Status
		}
		return daemonpkg.StopResponse{}, errors.New(response.Error)
	}
	return response, nil
}

func postDaemonTick(ctx context.Context, endpoint string) (daemonpkg.TickResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/tick", nil)
	if err != nil {
		return daemonpkg.TickResponse{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return daemonpkg.TickResponse{}, err
	}
	defer resp.Body.Close()
	var response daemonpkg.TickResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return daemonpkg.TickResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 || !response.OK {
		if response.Error == "" {
			response.Error = resp.Status
		}
		return daemonpkg.TickResponse{}, errors.New(response.Error)
	}
	return response, nil
}

func waitForDaemonRuntimeState(ctx context.Context, statePath string, runtimeState string) daemonpkg.State {
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := daemonpkg.LoadState(statePath)
		if err == nil && state.RuntimeState == runtimeState {
			return state
		}
		select {
		case <-ctx.Done():
			return state
		case <-deadline.C:
			return state
		case <-ticker.C:
		}
	}
}

func runDaemon(ctx context.Context, state *bootstrap.State, options daemonOptions, stdout io.Writer, stderr io.Writer) int {
	if options.HeartbeatInterval <= 0 {
		fmt.Fprintf(stderr, "ccgo: daemon heartbeat interval must be positive\n")
		return 1
	}
	runner, err := state.ConversationRunner()
	if err != nil {
		fmt.Fprintf(stderr, "ccgo daemon: %v\n", err)
		return 1
	}
	if runner.SessionPath == "" && runner.SessionID != "" {
		runner.SessionPath = session.TranscriptPath(runner.WorkingDirectory, runner.SessionID)
	}
	statePath := daemonpkg.SessionStatePath(runner.SessionPath, runner.SessionID)
	if statePath == "" {
		fmt.Fprintf(stderr, "ccgo daemon: session state path is unavailable\n")
		return 1
	}
	endpoint := ""
	var daemonServer *daemonpkg.Server
	var tickMu sync.Mutex
	stopRequested := make(chan struct{})
	var stopOnce sync.Once
	remoteStreamMode := false
	writeHeartbeat := func(now time.Time) error {
		return daemonpkg.WriteState(statePath, daemonpkg.BuildState(runner.SessionID, runner.WorkingDirectory, daemonpkg.RuntimeRunning, os.Getpid(), endpoint, now, nil))
	}
	runTick := func(now time.Time) (contracts.ToolResult, error) {
		tickMu.Lock()
		defer tickMu.Unlock()
		_, _ = runner.RefreshRemoteManagedPolicyIfConfigured()
		if err := writeHeartbeat(now); err != nil {
			return contracts.ToolResult{}, err
		}
		return runDaemonTickWithOptions(ctx, runner, now, daemonTickOptions{SkipRemoteWhenWebSocket: remoteStreamMode})
	}
	if !options.Once {
		daemonServer, err = daemonpkg.StartServer(daemonpkg.ServerOptions{
			StateFunc: func() daemonpkg.State {
				state, err := daemonpkg.LoadState(statePath)
				if err != nil || state.GeneratedAt == "" {
					return daemonpkg.BuildState(runner.SessionID, runner.WorkingDirectory, daemonpkg.RuntimeRunning, os.Getpid(), endpoint, time.Now().UTC(), err)
				}
				return state
			},
			TickFunc: func(context.Context) daemonpkg.TickResponse {
				result, err := runTick(time.Now().UTC())
				if err != nil {
					return daemonpkg.TickResponse{OK: false, Error: err.Error()}
				}
				return daemonTickResponse(result)
			},
			StopFunc: func(context.Context) daemonpkg.StopResponse {
				stopOnce.Do(func() { close(stopRequested) })
				return daemonpkg.StopResponse{OK: true, RuntimeState: daemonpkg.RuntimeDisabled}
			},
		})
		if err != nil {
			fmt.Fprintf(stderr, "ccgo daemon: %v\n", err)
			return 1
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = daemonServer.Close(shutdownCtx)
		}()
		endpoint = daemonServer.Endpoint()
	}
	if _, err := runTick(time.Now().UTC()); err != nil {
		fmt.Fprintf(stderr, "ccgo daemon: %v\n", err)
		return 1
	}
	remoteStreamMode = true
	fmt.Fprintf(stdout, "ccgo daemon running\nsession_id=%s\nstate_path=%s\n", runner.SessionID, statePath)
	if options.Once {
		return 0
	}
	streamCtx, cancelStream := context.WithCancel(ctx)
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		retryDelay := options.HeartbeatInterval
		if retryDelay < time.Second {
			retryDelay = time.Second
		}
		for streamCtx.Err() == nil {
			result := runDaemonRemoteStream(streamCtx, runner, time.Now().UTC(), remotepkg.WebSocketOptions{})
			if errText := stringMapValue(result.StructuredContent, "error"); errText != "" && streamCtx.Err() == nil {
				fmt.Fprintf(stderr, "ccgo daemon remote stream: %s\n", errText)
			}
			if streamCtx.Err() != nil {
				return
			}
			timer := time.NewTimer(retryDelay)
			select {
			case <-streamCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	defer func() {
		cancelStream()
		select {
		case <-streamDone:
		case <-time.After(time.Second):
		}
	}()
	ticker := time.NewTicker(options.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cancelStream()
			tickMu.Lock()
			_ = daemonpkg.WriteState(statePath, daemonpkg.BuildState(runner.SessionID, runner.WorkingDirectory, daemonpkg.RuntimeDisabled, os.Getpid(), endpoint, time.Now().UTC(), nil))
			tickMu.Unlock()
			return 0
		case <-stopRequested:
			cancelStream()
			tickMu.Lock()
			_ = daemonpkg.WriteState(statePath, daemonpkg.BuildState(runner.SessionID, runner.WorkingDirectory, daemonpkg.RuntimeDisabled, os.Getpid(), endpoint, time.Now().UTC(), nil))
			tickMu.Unlock()
			return 0
		case now := <-ticker.C:
			if _, err := runTick(now.UTC()); err != nil {
				fmt.Fprintf(stderr, "ccgo daemon: %v\n", err)
				return 1
			}
		}
	}
}

func runDaemonRemoteStream(ctx context.Context, runner conversation.Runner, now time.Time, streamOptions remotepkg.WebSocketOptions) contracts.ToolResult {
	structured := map[string]any{
		"type":            "remote_stream",
		"checked_at":      now.UTC().Format(time.RFC3339Nano),
		"runtime_state":   remotepkg.PumpDisabled,
		"transport":       "websocket",
		"event_count":     0,
		"delivered_count": 0,
		"duplicate_count": 0,
		"error_count":     0,
	}
	registrationPath := remotepkg.SessionRegistrationPath(runner.SessionPath, runner.SessionID)
	pumpPath := remotepkg.SessionPumpPath(runner.SessionPath, runner.SessionID)
	if registrationPath == "" || pumpPath == "" {
		structured["error_count"] = 1
		structured["error"] = "remote registration path is unavailable"
		return contracts.ToolResult{Content: "Remote stream is not configured.", StructuredContent: structured}
	}
	registration, err := remotepkg.LoadRegistrationState(registrationPath)
	if err != nil {
		structured["runtime_state"] = remotepkg.PumpFailed
		structured["error_count"] = 1
		structured["error"] = err.Error()
		_ = remotepkg.WritePumpState(pumpPath, remotepkg.PumpState{
			SessionID:    runner.SessionID,
			RuntimeState: remotepkg.PumpFailed,
			Transport:    "websocket",
			LastPollAt:   now.UTC().Format(time.RFC3339Nano),
			ErrorCount:   1,
			LastError:    err.Error(),
		})
		return contracts.ToolResult{Content: "Remote stream failed.", StructuredContent: structured}
	}
	if registration.RuntimeState != remotepkg.RegistrationRegistered || strings.TrimSpace(registration.WebSocketURL) == "" {
		return contracts.ToolResult{Content: "Remote stream is disabled.", StructuredContent: structured}
	}
	settings := runner.MergedSettings()
	authToken := ""
	if settings.Remote != nil {
		authToken = settings.Remote.AuthToken
	}
	options := streamOptions
	options.WebSocketURL = strings.TrimSpace(registration.WebSocketURL)
	options.AuthToken = authToken
	if options.ReconnectAttempts == 0 && options.MaxFrames == 0 {
		options.ReconnectAttempts = -1
	}
	if options.ReconnectInitialDelay <= 0 {
		options.ReconnectInitialDelay = time.Second
	}
	if options.ReconnectMaxDelay <= 0 {
		options.ReconnectMaxDelay = 30 * time.Second
	}
	streamStartedAt := now.UTC().Format(time.RFC3339Nano)
	pumpState := remotepkg.PumpState{
		SessionID:       runner.SessionID,
		RuntimeState:    remotepkg.PumpRunning,
		Transport:       "websocket_stream",
		PollURL:         remotepkg.DisplayEndpoint(registration.PollURL),
		WebSocketURL:    remotepkg.DisplayEndpoint(registration.WebSocketURL),
		LastPollAt:      streamStartedAt,
		StreamStartedAt: streamStartedAt,
	}
	writeStreamState := func() {
		_ = remotepkg.WritePumpState(pumpPath, pumpState)
	}
	streamStatusHandler := options.StatusHandler
	options.StatusHandler = func(status remotepkg.WebSocketResult) {
		pumpState.FrameCount = status.FrameCount
		pumpState.ConnectCount = status.ConnectCount
		pumpState.ReconnectCount = status.ReconnectCount
		pumpState.StatusCode = status.StatusCode
		pumpState.AttemptCount = status.AttemptCount
		pumpState.CloseCode = status.CloseCode
		pumpState.LastPollAt = time.Now().UTC().Format(time.RFC3339Nano)
		writeStreamState()
		if streamStatusHandler != nil {
			streamStatusHandler(status)
		}
	}
	writeStreamState()
	deliveryOptions := newDaemonRemoteDeliveryOptions(registration, authToken, time.Time{})
	result := remotepkg.StreamWebSocketEvents(ctx, options, func(events []remotepkg.PollEvent) error {
		delivery := deliverDaemonRemoteEvents(ctx, runner, events, deliveryOptions)
		pumpState.RuntimeState = remotepkg.PumpRunning
		pumpState.EventCount += len(events)
		pumpState.AckEventCount += delivery.AckEvents
		pumpState.AckSentCount += delivery.AckSent
		pumpState.AckErrorCount += delivery.AckErrors
		pumpState.LeaseEventCount += delivery.LeaseEvents
		pumpState.LeaseExpiredCount += delivery.LeaseExpired
		pumpState.LeaseRenewSent += delivery.LeaseRenewSent
		pumpState.LeaseRenewErrors += delivery.LeaseRenewErrors
		pumpState.DeliveredCount += delivery.Delivered
		pumpState.DuplicateCount += delivery.Duplicates
		pumpState.ErrorCount += len(delivery.ErrorsOut)
		pumpState.LastPollAt = time.Now().UTC().Format(time.RFC3339Nano)
		if len(delivery.ErrorsOut) > 0 {
			pumpState.LastError = fmt.Sprint(delivery.ErrorsOut[0]["error"])
		}
		writeStreamState()
		return nil
	})
	pumpState.FrameCount = result.FrameCount
	pumpState.ConnectCount = result.ConnectCount
	pumpState.ReconnectCount = result.ReconnectCount
	pumpState.StatusCode = result.StatusCode
	pumpState.AttemptCount = result.AttemptCount
	pumpState.CloseCode = result.CloseCode
	pumpState.LastPollAt = time.Now().UTC().Format(time.RFC3339Nano)
	pumpState.StreamEndedAt = pumpState.LastPollAt
	pumpState.StreamStopReason = daemonRemoteStreamStopReason(ctx, streamOptions, result)
	if result.Error != "" {
		pumpState.RuntimeState = remotepkg.PumpFailed
		pumpState.ErrorCount++
		pumpState.LastError = result.Error
	} else if ctx.Err() != nil {
		pumpState.RuntimeState = remotepkg.PumpDisabled
	}
	writeStreamState()
	structured["runtime_state"] = pumpState.RuntimeState
	structured["transport"] = pumpState.Transport
	structured["websocket_url"] = pumpState.WebSocketURL
	structured["poll_url"] = pumpState.PollURL
	structured["status_code"] = pumpState.StatusCode
	structured["attempt_count"] = pumpState.AttemptCount
	structured["frame_count"] = pumpState.FrameCount
	structured["connect_count"] = pumpState.ConnectCount
	structured["reconnect_count"] = pumpState.ReconnectCount
	structured["close_code"] = pumpState.CloseCode
	structured["ack_event_count"] = pumpState.AckEventCount
	structured["ack_sent_count"] = pumpState.AckSentCount
	structured["ack_error_count"] = pumpState.AckErrorCount
	structured["lease_event_count"] = pumpState.LeaseEventCount
	structured["lease_expired_count"] = pumpState.LeaseExpiredCount
	structured["lease_renew_sent_count"] = pumpState.LeaseRenewSent
	structured["lease_renew_error_count"] = pumpState.LeaseRenewErrors
	structured["stream_started_at"] = pumpState.StreamStartedAt
	structured["stream_ended_at"] = pumpState.StreamEndedAt
	structured["stream_stop_reason"] = pumpState.StreamStopReason
	structured["event_count"] = pumpState.EventCount
	structured["delivered_count"] = pumpState.DeliveredCount
	structured["duplicate_count"] = pumpState.DuplicateCount
	structured["error_count"] = pumpState.ErrorCount
	if result.Error != "" {
		structured["error"] = result.Error
	}
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Remote stream delivered %d event(s); %d duplicate(s); %d error(s).", pumpState.DeliveredCount, pumpState.DuplicateCount, pumpState.ErrorCount),
		StructuredContent: structured,
	}
}

func daemonRemoteStreamStopReason(ctx context.Context, options remotepkg.WebSocketOptions, result remotepkg.WebSocketResult) string {
	if strings.TrimSpace(result.Error) != "" {
		return "error"
	}
	if ctx.Err() != nil {
		return "context_cancelled"
	}
	if options.MaxFrames > 0 && result.FrameCount >= options.MaxFrames {
		return "max_frames"
	}
	return "closed"
}

func runDaemonDueSchedules(ctx context.Context, runner conversation.Runner, now time.Time) (contracts.ToolResult, error) {
	return tasktools.RunDueSchedules(tool.Context{
		Context:          ctx,
		WorkingDirectory: runner.WorkingDirectory,
		SessionID:        runner.SessionID,
		Metadata: map[string]any{
			tool.MetadataSessionPathKey: runner.SessionPath,
		},
	}, "", now, tool.NopProgressSink())
}

func runDaemonTick(ctx context.Context, runner conversation.Runner, now time.Time) (contracts.ToolResult, error) {
	return runDaemonTickWithOptions(ctx, runner, now, daemonTickOptions{})
}

func runDaemonTickWithOptions(ctx context.Context, runner conversation.Runner, now time.Time, options daemonTickOptions) (contracts.ToolResult, error) {
	scheduleResult, err := runDaemonDueSchedules(ctx, runner, now)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	var remoteResult contracts.ToolResult
	if options.SkipRemoteWhenWebSocket {
		remoteResult = runDaemonRemotePollUnlessStream(ctx, runner, now)
	} else {
		remoteResult = runDaemonRemotePoll(ctx, runner, now)
	}
	schedule := scheduleResult.StructuredContent
	remotePoll := remoteResult.StructuredContent
	scheduleTriggered := intMapValue(schedule, "triggered_count")
	scheduleErrors := intMapValue(schedule, "error_count")
	remoteDelivered := intMapValue(remotePoll, "delivered_count")
	remoteErrors := intMapValue(remotePoll, "error_count")
	structured := map[string]any{
		"type":                        "daemon_tick",
		"checked_at":                  now.UTC().Format(time.RFC3339Nano),
		"due_count":                   intMapValue(schedule, "due_count"),
		"triggered_count":             scheduleTriggered + remoteDelivered,
		"error_count":                 scheduleErrors + remoteErrors,
		"schedule_triggered_count":    scheduleTriggered,
		"schedule_error_count":        scheduleErrors,
		"remote_delivered_count":      remoteDelivered,
		"remote_poll_error_count":     remoteErrors,
		"remote_poll_duplicate_count": intMapValue(remotePoll, "duplicate_count"),
		"remote_transport":            stringMapValue(remotePoll, "transport"),
		"schedule":                    schedule,
		"remote_poll":                 remotePoll,
	}
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Triggered %d due schedule(s), delivered %d remote event(s); %d error(s).", scheduleTriggered, remoteDelivered, scheduleErrors+remoteErrors),
		StructuredContent: structured,
	}, nil
}

func runDaemonRemotePollUnlessStream(ctx context.Context, runner conversation.Runner, now time.Time) contracts.ToolResult {
	registrationPath := remotepkg.SessionRegistrationPath(runner.SessionPath, runner.SessionID)
	if registrationPath == "" {
		return runDaemonRemotePoll(ctx, runner, now)
	}
	registration, err := remotepkg.LoadRegistrationState(registrationPath)
	if err != nil || registration.RuntimeState != remotepkg.RegistrationRegistered || strings.TrimSpace(registration.WebSocketURL) == "" {
		return runDaemonRemotePoll(ctx, runner, now)
	}
	structured := map[string]any{
		"type":            "remote_poll",
		"checked_at":      now.UTC().Format(time.RFC3339Nano),
		"runtime_state":   remotepkg.PumpRunning,
		"transport":       "websocket_stream",
		"skipped":         true,
		"event_count":     0,
		"delivered_count": 0,
		"duplicate_count": 0,
		"error_count":     0,
	}
	return contracts.ToolResult{
		Content:           "Remote stream is handling websocket events.",
		StructuredContent: structured,
	}
}

func runDaemonRemotePoll(ctx context.Context, runner conversation.Runner, now time.Time) contracts.ToolResult {
	structured := map[string]any{
		"type":            "remote_poll",
		"checked_at":      now.UTC().Format(time.RFC3339Nano),
		"runtime_state":   remotepkg.PumpDisabled,
		"transport":       "",
		"event_count":     0,
		"delivered_count": 0,
		"duplicate_count": 0,
		"error_count":     0,
	}
	registrationPath := remotepkg.SessionRegistrationPath(runner.SessionPath, runner.SessionID)
	pumpPath := remotepkg.SessionPumpPath(runner.SessionPath, runner.SessionID)
	if registrationPath == "" || pumpPath == "" {
		structured["error_count"] = 1
		structured["error"] = "remote registration path is unavailable"
		return contracts.ToolResult{Content: "Remote poll is not configured.", StructuredContent: structured}
	}
	registration, err := remotepkg.LoadRegistrationState(registrationPath)
	if err != nil {
		structured["runtime_state"] = remotepkg.PumpFailed
		structured["error_count"] = 1
		structured["error"] = err.Error()
		_ = remotepkg.WritePumpState(pumpPath, remotepkg.PumpState{
			SessionID:    runner.SessionID,
			RuntimeState: remotepkg.PumpFailed,
			LastPollAt:   now.UTC().Format(time.RFC3339Nano),
			ErrorCount:   1,
			LastError:    err.Error(),
		})
		return contracts.ToolResult{Content: "Remote poll failed.", StructuredContent: structured}
	}
	if registration.RuntimeState != remotepkg.RegistrationRegistered || (strings.TrimSpace(registration.PollURL) == "" && strings.TrimSpace(registration.WebSocketURL) == "") {
		_ = remotepkg.WritePumpState(pumpPath, remotepkg.PumpState{
			SessionID:    runner.SessionID,
			RuntimeState: remotepkg.PumpDisabled,
			PollURL:      remotepkg.DisplayEndpoint(registration.PollURL),
			WebSocketURL: remotepkg.DisplayEndpoint(registration.WebSocketURL),
			LastPollAt:   now.UTC().Format(time.RFC3339Nano),
		})
		return contracts.ToolResult{Content: "Remote poll is disabled.", StructuredContent: structured}
	}
	previous, err := remotepkg.LoadPumpState(pumpPath)
	if err != nil {
		previous.LastError = err.Error()
	}
	settings := runner.MergedSettings()
	authToken := ""
	if settings.Remote != nil {
		authToken = settings.Remote.AuthToken
	}
	remoteFetch := fetchDaemonRemoteEvents(ctx, registration, previous.LastCursor, authToken)
	pumpState := remotepkg.PumpState{
		SessionID:      runner.SessionID,
		RuntimeState:   remotepkg.PumpRunning,
		Transport:      remoteFetch.Transport,
		PollURL:        remotepkg.DisplayEndpoint(registration.PollURL),
		WebSocketURL:   remotepkg.DisplayEndpoint(registration.WebSocketURL),
		LastCursor:     previous.LastCursor,
		LastPollAt:     now.UTC().Format(time.RFC3339Nano),
		StatusCode:     remoteFetch.StatusCode,
		AttemptCount:   remoteFetch.AttemptCount,
		CloseCode:      remoteFetch.CloseCode,
		FrameCount:     remoteFetch.FrameCount,
		ConnectCount:   remoteFetch.ConnectCount,
		ReconnectCount: remoteFetch.ReconnectCount,
		EventCount:     len(remoteFetch.Events),
	}
	if remoteFetch.Transport == "poll" {
		if remoteFetch.NextCursor != "" {
			pumpState.LastCursor = remoteFetch.NextCursor
		}
	} else {
		pumpState.LastCursor = ""
	}
	if remoteFetch.Error != "" {
		pumpState.RuntimeState = remotepkg.PumpFailed
		pumpState.ErrorCount = 1
		pumpState.LastError = remoteFetch.Error
		structured["runtime_state"] = pumpState.RuntimeState
		structured["transport"] = pumpState.Transport
		structured["poll_url"] = pumpState.PollURL
		structured["websocket_url"] = pumpState.WebSocketURL
		structured["status_code"] = remoteFetch.StatusCode
		structured["attempt_count"] = remoteFetch.AttemptCount
		structured["close_code"] = remoteFetch.CloseCode
		structured["frame_count"] = remoteFetch.FrameCount
		structured["connect_count"] = remoteFetch.ConnectCount
		structured["reconnect_count"] = remoteFetch.ReconnectCount
		structured["event_count"] = len(remoteFetch.Events)
		structured["error_count"] = 1
		structured["error"] = remoteFetch.Error
		if remoteFetch.FallbackError != "" {
			structured["fallback_error"] = remoteFetch.FallbackError
		}
		_ = remotepkg.WritePumpState(pumpPath, pumpState)
		return contracts.ToolResult{Content: "Remote poll failed.", StructuredContent: structured}
	}
	delivery := deliverDaemonRemoteEvents(ctx, runner, remoteFetch.Events, newDaemonRemoteDeliveryOptions(registration, authToken, now))
	pumpState.AckEventCount = delivery.AckEvents
	pumpState.AckSentCount = delivery.AckSent
	pumpState.AckErrorCount = delivery.AckErrors
	pumpState.LeaseEventCount = delivery.LeaseEvents
	pumpState.LeaseExpiredCount = delivery.LeaseExpired
	pumpState.LeaseRenewSent = delivery.LeaseRenewSent
	pumpState.LeaseRenewErrors = delivery.LeaseRenewErrors
	pumpState.DeliveredCount = delivery.Delivered
	pumpState.DuplicateCount = delivery.Duplicates
	pumpState.ErrorCount = len(delivery.ErrorsOut)
	if len(delivery.ErrorsOut) > 0 {
		pumpState.LastError = fmt.Sprint(delivery.ErrorsOut[0]["error"])
	}
	_ = remotepkg.WritePumpState(pumpPath, pumpState)
	structured["runtime_state"] = pumpState.RuntimeState
	structured["transport"] = pumpState.Transport
	structured["poll_url"] = pumpState.PollURL
	structured["websocket_url"] = pumpState.WebSocketURL
	structured["last_cursor"] = pumpState.LastCursor
	structured["status_code"] = pumpState.StatusCode
	structured["attempt_count"] = pumpState.AttemptCount
	structured["close_code"] = pumpState.CloseCode
	structured["frame_count"] = pumpState.FrameCount
	structured["connect_count"] = pumpState.ConnectCount
	structured["reconnect_count"] = pumpState.ReconnectCount
	structured["ack_event_count"] = pumpState.AckEventCount
	structured["ack_sent_count"] = pumpState.AckSentCount
	structured["ack_error_count"] = pumpState.AckErrorCount
	structured["lease_event_count"] = pumpState.LeaseEventCount
	structured["lease_expired_count"] = pumpState.LeaseExpiredCount
	structured["lease_renew_sent_count"] = pumpState.LeaseRenewSent
	structured["lease_renew_error_count"] = pumpState.LeaseRenewErrors
	structured["event_count"] = pumpState.EventCount
	structured["delivered_count"] = pumpState.DeliveredCount
	structured["duplicate_count"] = pumpState.DuplicateCount
	structured["error_count"] = pumpState.ErrorCount
	structured["errors"] = delivery.ErrorsOut
	if remoteFetch.FallbackError != "" {
		structured["fallback_error"] = remoteFetch.FallbackError
	}
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Remote %s delivered %d event(s); %d duplicate(s); %d error(s).", remoteFetch.Transport, delivery.Delivered, delivery.Duplicates, len(delivery.ErrorsOut)),
		StructuredContent: structured,
	}
}

type daemonRemoteFetch struct {
	Transport      string
	StatusCode     int
	AttemptCount   int
	NextCursor     string
	CloseCode      int
	FrameCount     int
	ConnectCount   int
	ReconnectCount int
	Events         []remotepkg.PollEvent
	Error          string
	FallbackError  string
}

type daemonRemoteDeliveryOptions struct {
	AuthToken      string
	AllowedOrigins []string
	LeaseRenewURL  string
	Now            time.Time
}

func newDaemonRemoteDeliveryOptions(registration remotepkg.RegistrationState, authToken string, now time.Time) daemonRemoteDeliveryOptions {
	return daemonRemoteDeliveryOptions{
		AuthToken: authToken,
		AllowedOrigins: []string{
			registration.RegistrationURL,
			registration.PollURL,
			registration.WebSocketURL,
		},
		LeaseRenewURL: registration.LeaseRenewURL,
		Now:           now,
	}
}

func fetchDaemonRemoteEvents(ctx context.Context, registration remotepkg.RegistrationState, cursor string, authToken string) daemonRemoteFetch {
	webSocketURL := strings.TrimSpace(registration.WebSocketURL)
	pollURL := strings.TrimSpace(registration.PollURL)
	if webSocketURL != "" {
		wsCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		ws := remotepkg.FetchWebSocketEvents(wsCtx, remotepkg.WebSocketOptions{
			WebSocketURL:          webSocketURL,
			AuthToken:             authToken,
			MaxFrames:             8,
			ReconnectAttempts:     2,
			ReconnectInitialDelay: 100 * time.Millisecond,
			ReconnectMaxDelay:     500 * time.Millisecond,
		})
		fetch := daemonRemoteFetch{
			Transport:      "websocket",
			StatusCode:     ws.StatusCode,
			AttemptCount:   ws.AttemptCount,
			FrameCount:     ws.FrameCount,
			ConnectCount:   ws.ConnectCount,
			ReconnectCount: ws.ReconnectCount,
			CloseCode:      ws.CloseCode,
			Events:         ws.Events,
			Error:          ws.Error,
		}
		if ws.Error == "" || pollURL == "" {
			return fetch
		}
		poll := remotepkg.FetchPollEvents(ctx, daemonRemotePollOptions(pollURL, cursor, authToken))
		return daemonRemoteFetch{
			Transport:     "poll",
			StatusCode:    poll.StatusCode,
			AttemptCount:  poll.AttemptCount,
			NextCursor:    poll.NextCursor,
			Events:        poll.Events,
			Error:         poll.Error,
			FallbackError: ws.Error,
		}
	}
	poll := remotepkg.FetchPollEvents(ctx, daemonRemotePollOptions(pollURL, cursor, authToken))
	return daemonRemoteFetch{
		Transport:    "poll",
		StatusCode:   poll.StatusCode,
		AttemptCount: poll.AttemptCount,
		NextCursor:   poll.NextCursor,
		Events:       poll.Events,
		Error:        poll.Error,
	}
}

func daemonRemotePollOptions(pollURL string, cursor string, authToken string) remotepkg.PollOptions {
	return remotepkg.PollOptions{
		PollURL:           pollURL,
		Cursor:            cursor,
		AuthToken:         authToken,
		RetryAttempts:     1,
		RetryInitialDelay: 100 * time.Millisecond,
		RetryMaxDelay:     500 * time.Millisecond,
	}
}

type daemonRemoteDeliveryResult struct {
	Delivered        int
	Duplicates       int
	AckEvents        int
	AckSent          int
	AckErrors        int
	LeaseEvents      int
	LeaseExpired     int
	LeaseRenewSent   int
	LeaseRenewErrors int
	ErrorsOut        []map[string]any
}

func deliverDaemonRemoteEvents(ctx context.Context, runner conversation.Runner, events []remotepkg.PollEvent, options daemonRemoteDeliveryOptions) daemonRemoteDeliveryResult {
	result := daemonRemoteDeliveryResult{ErrorsOut: make([]map[string]any, 0)}
	for _, event := range events {
		if event.AckURL != "" {
			result.AckEvents++
		}
		if event.LeaseID != "" || event.LeaseExpiresAt != "" {
			result.LeaseEvents++
		}
		if daemonRemoteLeaseExpired(event, options.Now) {
			result.LeaseExpired++
			errorText := "remote event lease expired"
			result.ErrorsOut = append(result.ErrorsOut, map[string]any{
				"type":             "remote_lease",
				"event_id":         event.EventID,
				"lease_id":         event.LeaseID,
				"lease_expires_at": event.LeaseExpiresAt,
				"error":            errorText,
			})
			sent, ackErr := acknowledgeDaemonRemoteEvent(ctx, event, options, "expired", 0, false, errorText)
			result.AckSent += sent
			if ackErr != nil {
				result.AckErrors++
				result.ErrorsOut = append(result.ErrorsOut, ackErr)
			}
			continue
		}
		sent, renewErr := renewDaemonRemoteLease(ctx, event, options)
		result.LeaseRenewSent += sent
		if renewErr != nil {
			result.LeaseRenewErrors++
			result.ErrorsOut = append(result.ErrorsOut, renewErr)
		}
		input, err := json.Marshal(map[string]any{
			"team_id":  event.TeamID,
			"target":   event.Target,
			"event_id": event.EventID,
			"source":   event.Source,
			"event":    event.Event,
			"message":  event.Message,
		})
		if err != nil {
			result.ErrorsOut = append(result.ErrorsOut, map[string]any{"event_id": event.EventID, "error": err.Error()})
			sent, ackErr := acknowledgeDaemonRemoteEvent(ctx, event, options, "failed", 0, false, err.Error())
			result.AckSent += sent
			if ackErr != nil {
				result.AckErrors++
				result.ErrorsOut = append(result.ErrorsOut, ackErr)
			}
			continue
		}
		toolResult, err := tasktools.RunRemoteTrigger(tool.Context{
			Context:          ctx,
			WorkingDirectory: runner.WorkingDirectory,
			SessionID:        runner.SessionID,
			Metadata: map[string]any{
				tool.MetadataSessionPathKey: runner.SessionPath,
			},
		}, input, tool.NopProgressSink())
		if err != nil {
			result.ErrorsOut = append(result.ErrorsOut, map[string]any{"event_id": event.EventID, "team_id": event.TeamID, "error": err.Error()})
			sent, ackErr := acknowledgeDaemonRemoteEvent(ctx, event, options, "failed", 0, false, err.Error())
			result.AckSent += sent
			if ackErr != nil {
				result.AckErrors++
				result.ErrorsOut = append(result.ErrorsOut, ackErr)
			}
			continue
		}
		if duplicate, _ := toolResult.StructuredContent["duplicate"].(bool); duplicate {
			result.Duplicates++
			sent, ackErr := acknowledgeDaemonRemoteEvent(ctx, event, options, "duplicate", 0, true, "")
			result.AckSent += sent
			if ackErr != nil {
				result.AckErrors++
				result.ErrorsOut = append(result.ErrorsOut, ackErr)
			}
			continue
		}
		sentCount := intMapValue(toolResult.StructuredContent, "sent_count")
		result.Delivered += sentCount
		sent, ackErr := acknowledgeDaemonRemoteEvent(ctx, event, options, "delivered", sentCount, false, "")
		result.AckSent += sent
		if ackErr != nil {
			result.AckErrors++
			result.ErrorsOut = append(result.ErrorsOut, ackErr)
		}
	}
	return result
}

func daemonRemoteLeaseExpired(event remotepkg.PollEvent, now time.Time) bool {
	if strings.TrimSpace(event.LeaseExpiresAt) == "" {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(event.LeaseExpiresAt))
	if err != nil {
		return false
	}
	return !expiresAt.After(now.UTC())
}

func renewDaemonRemoteLease(ctx context.Context, event remotepkg.PollEvent, options daemonRemoteDeliveryOptions) (int, map[string]any) {
	if strings.TrimSpace(event.LeaseID) == "" || strings.TrimSpace(options.LeaseRenewURL) == "" {
		return 0, nil
	}
	result := remotepkg.SendLeaseRenewal(ctx, remotepkg.LeaseRenewOptions{
		LeaseRenewURL:     options.LeaseRenewURL,
		AuthToken:         options.AuthToken,
		EventID:           event.EventID,
		LeaseID:           event.LeaseID,
		AllowedOrigins:    options.AllowedOrigins,
		RetryAttempts:     1,
		RetryInitialDelay: 100 * time.Millisecond,
		RetryMaxDelay:     500 * time.Millisecond,
	})
	if result.Error != "" {
		return 0, map[string]any{
			"type":     "remote_lease_renew",
			"event_id": event.EventID,
			"lease_id": event.LeaseID,
			"error":    result.Error,
		}
	}
	return 1, nil
}

func acknowledgeDaemonRemoteEvent(ctx context.Context, event remotepkg.PollEvent, options daemonRemoteDeliveryOptions, status string, sentCount int, duplicate bool, errorText string) (int, map[string]any) {
	if event.AckURL == "" {
		return 0, nil
	}
	result := remotepkg.SendAck(ctx, remotepkg.AckOptions{
		AckURL:            event.AckURL,
		AuthToken:         options.AuthToken,
		EventID:           event.EventID,
		Status:            status,
		SentCount:         sentCount,
		Duplicate:         duplicate,
		Error:             errorText,
		AllowedOrigins:    options.AllowedOrigins,
		RetryAttempts:     1,
		RetryInitialDelay: 100 * time.Millisecond,
		RetryMaxDelay:     500 * time.Millisecond,
	})
	if result.Error != "" {
		return 0, map[string]any{
			"type":     "remote_ack",
			"event_id": event.EventID,
			"status":   status,
			"error":    result.Error,
		}
	}
	return 1, nil
}

func daemonTickResponse(result contracts.ToolResult) daemonpkg.TickResponse {
	structured := result.StructuredContent
	return daemonpkg.TickResponse{
		OK:             true,
		CheckedAt:      stringMapValue(structured, "checked_at"),
		TriggeredCount: intMapValue(structured, "triggered_count"),
		ErrorCount:     intMapValue(structured, "error_count"),
		Structured:     structured,
	}
}

func stringMapValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func intMapValue(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func handleChromeNativeHostMessage(raw json.RawMessage) map[string]any {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return map[string]any{"type": "error", "ok": false, "error": "invalid JSON message"}
	}
	messageType, _ := message["type"].(string)
	switch strings.ToLower(strings.TrimSpace(messageType)) {
	case "ping":
		return map[string]any{"type": "pong", "ok": true}
	case "hello", "capabilities":
		return chromeNativeHostCapabilitiesResponse()
	case "runtime", "session":
		return chromeNativeHostRuntimeResponse(strings.ToLower(strings.TrimSpace(messageType)))
	case "status":
		response := chromeNativeHostRuntimeResponse("status")
		response["capabilities"] = chromeNativeHostCapabilities()
		return response
	default:
		if messageType == "" {
			messageType = "(missing)"
		}
		return map[string]any{"type": "error", "ok": false, "error": "unsupported message type: " + messageType}
	}
}

func chromeNativeHostCapabilitiesResponse() map[string]any {
	return map[string]any{
		"type":             "capabilities",
		"ok":               true,
		"runtime":          "ccgo",
		"version":          version,
		"protocol_version": chromeNativeHostProtocolVersion,
		"capabilities":     chromeNativeHostCapabilities(),
	}
}

func chromeNativeHostRuntimeResponse(responseType string) map[string]any {
	if responseType == "" {
		responseType = "runtime"
	}
	return map[string]any{
		"type":             responseType,
		"ok":               true,
		"runtime":          "ccgo",
		"version":          version,
		"protocol_version": chromeNativeHostProtocolVersion,
		"pid":              os.Getpid(),
	}
}

func chromeNativeHostCapabilities() map[string]any {
	return map[string]any{
		"ping":         true,
		"status":       true,
		"hello":        true,
		"capabilities": true,
		"runtime":      true,
		"session":      true,
	}
}

func applyCWDFlag(state *bootstrap.State, raw string) error {
	cwd := strings.TrimSpace(raw)
	if cwd == "" {
		return nil
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("invalid --cwd %q: %w", raw, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("invalid --cwd %q: %w", raw, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("invalid --cwd %q: not a directory", raw)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	state.SetCWD(abs)
	return nil
}

func promptFromArgsOrStdin(args []string, stdin io.Reader) (string, error) {
	if len(args) > 0 {
		prompt := strings.TrimSpace(strings.Join(args, " "))
		if prompt == "" {
			return "", fmt.Errorf("--print requires a prompt via arguments or stdin")
		}
		return prompt, nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", err
	}
	prompt := strings.TrimSpace(string(data))
	if prompt == "" {
		return "", fmt.Errorf("--print requires a prompt via arguments or stdin")
	}
	return prompt, nil
}

func promptMessageFromArgsOrStdin(args []string, stdin io.Reader, inputFormat string) (contracts.Message, error) {
	switch inputFormat {
	case "text":
		prompt, err := promptFromArgsOrStdin(args, stdin)
		if err != nil {
			return contracts.Message{}, err
		}
		return messages.UserText(prompt), nil
	case "json":
		data, err := rawStructuredInputFromArgsOrStdin(args, stdin, "--input-format json requires JSON input via arguments or stdin")
		if err != nil {
			return contracts.Message{}, err
		}
		return userMessageFromJSON(data)
	case "stream-json":
		data, err := rawStructuredInputFromArgsOrStdin(args, stdin, "--input-format stream-json requires NDJSON input via arguments or stdin")
		if err != nil {
			return contracts.Message{}, err
		}
		return userMessageFromStreamJSON(data)
	default:
		return contracts.Message{}, fmt.Errorf("unsupported input format %q", inputFormat)
	}
}

func rawStructuredInputFromArgsOrStdin(args []string, stdin io.Reader, emptyMessage string) ([]byte, error) {
	var data []byte
	var err error
	if len(args) > 0 {
		data = []byte(strings.TrimSpace(strings.Join(args, " ")))
	} else {
		data, err = io.ReadAll(stdin)
		if err != nil {
			return nil, err
		}
		data = []byte(strings.TrimSpace(string(data)))
	}
	if len(data) == 0 {
		return nil, errors.New(emptyMessage)
	}
	return data, nil
}

func userMessageFromJSON(data []byte) (contracts.Message, error) {
	var event contracts.SDKEvent
	if err := json.Unmarshal(data, &event); err == nil && event.Type == contracts.SDKEventUser && event.Message != nil {
		return normalizeInputUserMessage(*event.Message)
	}
	var message contracts.Message
	if err := json.Unmarshal(data, &message); err == nil {
		if normalized, err := normalizeInputUserMessage(message); err == nil {
			return normalized, nil
		}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return contracts.Message{}, err
	}
	if raw, ok := fields["messages"]; ok {
		if message, err := userMessageFromMessagesJSON(raw); err == nil {
			return message, nil
		}
	}
	for _, name := range []string{"message", "payload", "data", "body"} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			if text = strings.TrimSpace(text); text != "" {
				return messages.UserText(text), nil
			}
		}
		if message, err := userMessageFromJSON(raw); err == nil {
			return message, nil
		}
	}
	for _, name := range []string{"prompt", "query", "input", "text", "messageText", "message_text"} {
		if raw, ok := fields[name]; ok {
			var text string
			if err := json.Unmarshal(raw, &text); err != nil {
				return contracts.Message{}, fmt.Errorf("%s must be a string", name)
			}
			text = strings.TrimSpace(text)
			if text != "" {
				return messages.UserText(text), nil
			}
		}
	}
	return contracts.Message{}, fmt.Errorf("JSON input must contain a user message or prompt")
}

func userMessageFromMessagesJSON(data []byte) (contracts.Message, error) {
	var messages []contracts.Message
	if err := json.Unmarshal(data, &messages); err == nil {
		for i := len(messages) - 1; i >= 0; i-- {
			if normalized, err := normalizeInputUserMessage(messages[i]); err == nil {
				return normalized, nil
			}
		}
	}
	var events []contracts.SDKEvent
	if err := json.Unmarshal(data, &events); err == nil {
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Type != contracts.SDKEventUser || events[i].Message == nil {
				continue
			}
			if normalized, err := normalizeInputUserMessage(*events[i].Message); err == nil {
				return normalized, nil
			}
		}
	}
	return contracts.Message{}, fmt.Errorf("messages must contain a user message")
}

func userMessageFromStreamJSON(data []byte) (contracts.Message, error) {
	var last contracts.Message
	var found bool
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event contracts.SDKEvent
		if err := json.Unmarshal([]byte(line), &event); err == nil && event.Type != "" {
			if event.Type != contracts.SDKEventUser {
				continue
			}
			if event.Message == nil {
				return contracts.Message{}, fmt.Errorf("stream-json user event must contain a message")
			}
			message, err := normalizeInputUserMessage(*event.Message)
			if err != nil {
				return contracts.Message{}, err
			}
			last = message
			found = true
			continue
		}
		message, err := userMessageFromJSON([]byte(line))
		if err != nil {
			return contracts.Message{}, err
		}
		last = message
		found = true
	}
	if !found {
		return contracts.Message{}, fmt.Errorf("stream-json input must contain a user message")
	}
	return last, nil
}

func normalizeInputUserMessage(message contracts.Message) (contracts.Message, error) {
	if message.Type == "" {
		message.Type = contracts.MessageUser
	}
	if message.Type != contracts.MessageUser {
		return contracts.Message{}, fmt.Errorf("input message must be a user message")
	}
	if len(message.Content) == 0 {
		return contracts.Message{}, fmt.Errorf("input user message must contain content")
	}
	return message, nil
}

func normalizeInputFormat(raw string) (string, error) {
	format := normalizeCLIFormatValue(raw)
	if format == "" {
		format = "text"
	}
	switch format {
	case "text", "json", "stream-json":
		return format, nil
	default:
		return "", fmt.Errorf("unsupported input format %q", raw)
	}
}

func effectivePermissionMode(permissionMode string, skipPermissions bool) (string, error) {
	mode := strings.TrimSpace(permissionMode)
	if !skipPermissions {
		return mode, nil
	}
	if mode != "" && mode != string(contracts.PermissionBypassPermissions) {
		return "", fmt.Errorf("--dangerously-skip-permissions cannot be combined with --permission-mode %q", permissionMode)
	}
	return string(contracts.PermissionBypassPermissions), nil
}

func normalizeOutputFormat(raw string) (string, error) {
	format := normalizeCLIFormatValue(raw)
	if format == "" {
		format = "text"
	}
	switch format {
	case "text", "json", "stream-json":
		return format, nil
	default:
		return "", fmt.Errorf("unsupported output format %q", raw)
	}
}

func normalizeCLIFormatValue(raw string) string {
	format := strings.TrimSpace(strings.ToLower(raw))
	format = strings.ReplaceAll(format, "_", "-")
	format = strings.ReplaceAll(format, " ", "-")
	switch format {
	case "streamjson":
		return "stream-json"
	default:
		return format
	}
}

// interactiveRunner builds a fully-wired runner for the interactive REPL.
// It delegates to headlessRunner; kept as a separate seam for future
// interactive-only wiring (e.g., interactive default permission mode).
func interactiveRunner(ctx context.Context, state *bootstrap.State, options cliOptions) (conversation.Runner, error) {
	return headlessRunner(ctx, state, options)
}

func headlessRunner(ctx context.Context, state *bootstrap.State, options cliOptions) (conversation.Runner, error) {
	runner, err := state.ConversationRunner()
	if err != nil {
		return conversation.Runner{}, err
	}
	// CLI-FLAG-33: --setting-sources filters which settings files are loaded.
	// Only sources named in the comma-separated list are kept; others are zeroed.
	// CC ref: src/utils/settings/constants.ts parseSettingSourcesFlag;
	// bootstrap/state.ts setAllowedSettingSources controls which files are read.
	if options.SettingSources != "" {
		applySettingSourcesFilter(runner.MCP, options.SettingSources)
	}
	if err := applyMCPConfigFlag(&runner, options.MCPConfig); err != nil {
		return conversation.Runner{}, err
	}
	// F2-C02: --settings loads additional settings at highest precedence.
	// CC ref: src/main.tsx:--settings.
	if err := applySettingsFlag(&runner, options.SettingsPath); err != nil {
		return conversation.Runner{}, err
	}
	registry, err := tool.NewRegistry(filetools.BuiltinTools()...)
	if err != nil {
		return conversation.Runner{}, err
	}
	runner.Tools = tool.NewExecutor(registry)
	// CLI-FLAG-31: --tools specifies the whitelist of available built-in tools.
	// Any tool not named in the list is added to DeniedTools so the permission
	// decider blocks it. CC ref: src/utils/permissions/permissionSetup.ts
	// baseToolsCli: builds disallow filter from all tools not in the base set.
	// Special value "default" (empty list) means all tools are available.
	deniedToolsFromFlag := append([]string(nil), options.DeniedTools...)
	if len(options.Tools) > 0 {
		deniedToolsFromFlag = append(deniedToolsFromFlag, toolsNotInWhitelist(registry.Names(), parseToolRules(options.Tools...))...)
	}
	runner.Permissions, err = permissionDeciderFromSettings(
		runner.MCP,
		strings.TrimSpace(options.PermissionMode),
		parseToolRules(options.AllowedTools...),
		deniedToolsFromFlag,
		parsePathList(options.AddDirs),
	)
	if err != nil {
		return conversation.Runner{}, err
	}
	runner.PermissionMode = runnerPermissionModeFromDecider(runner.Permissions)
	runner.Model = resolveCLIModel(options.Model, runner.MCP)
	if runner.MCP != nil {
		merged := runner.MCP.MergedSettings()
		runner.FastMode = merged.FastMode != nil && *merged.FastMode
		// CFG-32: effortLevel from settings sets the initial effort level.
		// CC ref: utils/effort.ts getInitialEffortSetting.
		if merged.EffortLevel != "" {
			runner.EffortLevel = merged.EffortLevel
		}
		// CFG-33: alwaysThinkingEnabled forces thinking on every request.
		if merged.AlwaysThinkingEnabled != nil && *merged.AlwaysThinkingEnabled {
			runner.AlwaysThinkingEnabled = true
		}
		// CFG-07: availableModels enforces model whitelist when set by enterprise.
		// CC ref: utils/settings/types.ts availableModels; model selection validation.
		if err := checkAvailableModels(runner.Model, merged.AvailableModels); err != nil {
			return conversation.Runner{}, err
		}
		// CFG-08: modelOverrides remaps model IDs (e.g. to Bedrock ARNs).
		// CC ref: utils/settings/types.ts modelOverrides.
		if remapped := applyModelOverrides(runner.Model, merged.ModelOverrides); remapped != runner.Model {
			runner.Model = remapped
		}
		// CFG-46: minimumVersion enforces a minimum version constraint.
		// CC ref: utils/settings/types.ts minimumVersion.
		if err := checkMinimumVersion(merged.MinimumVersion); err != nil {
			return conversation.Runner{}, err
		}
		// CFG-18: language preference — inject into system prompt.
		// CC ref: constants/prompts.ts getLanguageSection.
		if merged.Language != "" {
			runner.Language = merged.Language
		}
		// CFG-16: includeGitInstructions — controls git sections in system prompt.
		// CC ref: utils/gitSettings.ts shouldIncludeGitInstructions.
		if merged.IncludeGitInstructions != nil {
			runner.IncludeGitInstructions = merged.IncludeGitInstructions
		}
		// CFG-53: verbose — enables detailed debug output.
		// CC ref: tools/ConfigTool/supportedSettings.ts verbose.
		if merged.Verbose != nil && *merged.Verbose {
			runner.Verbose = true
		}
	}
	if options.MaxTokens < 0 {
		return conversation.Runner{}, fmt.Errorf("invalid --max-tokens %d; must be non-negative", options.MaxTokens)
	}
	if options.MaxTokens > 0 {
		runner.MaxTokens = options.MaxTokens
	}
	if options.MaxTurns < 0 {
		return conversation.Runner{}, fmt.Errorf("invalid --max-turns %d; must be non-negative", options.MaxTurns)
	}
	if options.MaxTurns > 0 {
		runner.MaxToolRounds = options.MaxTurns
	}
	runner.UseStreaming = options.Stream

	// F2-C01: --verbose overrides the verbose setting from config.
	if options.Verbose {
		runner.Verbose = true
	}
	// F2-C01: --debug sets an env var (CLAUDE_CODE_DEBUG) that debug-aware code reads.
	// The optional filter string is stored in CLAUDE_CODE_DEBUG_FILTER.
	// CC ref: src/main.tsx:-d/--debug [filter].
	if options.Debug != "" {
		// Non-empty means debug was explicitly enabled (the flag value is the filter or "true").
		if options.Debug != "true" {
			// Only set FILTER when a real category string was given, not the bool sentinel.
			_ = os.Setenv("CLAUDE_CODE_DEBUG_FILTER", options.Debug)
		}
		_ = os.Setenv("CLAUDE_CODE_DEBUG", "1")
	}
	// F2-C01: --bare sets CLAUDE_CODE_SIMPLE=1 (skip hooks, LSP, plugin-sync, etc.).
	// CC ref: src/main.tsx:--bare sets process.env.CLAUDE_CODE_SIMPLE='1'.
	if options.Bare {
		_ = os.Setenv("CLAUDE_CODE_SIMPLE", "1")
	}
	// F2-C01: --effort sets the effort level for the session.
	// CC ref: src/main.tsx:--effort; utils/effort.ts resolveAppliedEffort.
	if options.Effort != "" {
		runner.EffortLevel = options.Effort
	}
	// F2-C01: --thinking sets thinking mode (enabled/adaptive/disabled).
	// CC ref: src/main.tsx:--thinking; betas.ts EFFORT_BETA_HEADER.
	// "enabled" and "adaptive" both activate thinking; "disabled" clears it.
	switch strings.ToLower(strings.TrimSpace(options.Thinking)) {
	case "enabled", "adaptive":
		runner.AlwaysThinkingEnabled = true
	case "disabled":
		runner.AlwaysThinkingEnabled = false
	}

	// SESS-08: --session-id combined with --continue or --resume requires
	// --fork-session; without it the caller would silently reuse both the
	// original session ID *and* a custom one — an impossible state.
	// CC ref: src/main.tsx:1279-1284.
	if options.SessionID != "" && (options.Continue || strings.TrimSpace(options.Resume) != "") && !options.ForkSession {
		return conversation.Runner{}, fmt.Errorf("--session-id can only be used with --continue or --resume if --fork-session is also specified")
	}
	// F2-C02: --session-id overrides the session ID set by bootstrap.
	// CC ref: src/main.tsx:--session-id.
	if options.SessionID != "" {
		runner.SessionID = contracts.ID(strings.TrimSpace(options.SessionID))
	}
	// F2-C02: --no-session-persistence clears the session path so no .jsonl is created.
	// CC ref: src/main.tsx:--no-session-persistence.
	if options.NoSessionPersistence {
		runner.SessionPath = ""
		runner.SessionID = ""
	}
	// F2-C02: --name sets the session display name via title metadata.
	// CC ref: src/main.tsx:--name; session title is stored in transcript.
	// Wired as env var; REPL and session persistence reads this for display.
	if options.SessionName != "" {
		_ = os.Setenv("CLAUDE_SESSION_NAME", options.SessionName)
	}

	// F2-C03: --fallback-model adds to FallbackModels for overload retry.
	// CC ref: src/main.tsx:--fallback-model; query.ts fallback model logic.
	if options.FallbackModel != "" {
		runner.FallbackModels = append(runner.FallbackModels, options.FallbackModel)
	}
	// F2-C03: --betas appends beta headers to the API request.
	// CC ref: src/main.tsx:--betas; api/client.ts beta header.
	if len(options.Betas) > 0 {
		runner.BetaHeaders = append(runner.BetaHeaders, options.Betas...)
	}

	// F2-C04: --strict-mcp-config clears all non-flag MCP configs.
	// CC ref: src/main.tsx:--strict-mcp-config.
	if options.StrictMCPConfig && runner.MCP != nil {
		runner.MCP.UserSettings.MCPServers = nil
		runner.MCP.ProjectSettings.MCPServers = nil
		runner.MCP.LocalSettings.MCPServers = nil
		runner.MCP.PluginServers = nil
	}
	// CLI-FLAG-40: --json-schema injects a structured output schema into the API request.
	// When set, output_config.format = {type:"json_schema", json_schema:<schema>} and the
	// structured-outputs-2025-12-15 beta header is added.
	// CC ref: src/services/api/claude.ts:1577-1586; src/constants/betas.ts:8.
	if options.JSONSchema != "" {
		var schema contracts.JSONSchema
		if err := json.Unmarshal([]byte(options.JSONSchema), &schema); err != nil {
			return conversation.Runner{}, fmt.Errorf("--json-schema: invalid JSON: %w", err)
		}
		runner.OutputSchema = schema
		runner.BetaHeaders = append(runner.BetaHeaders, "structured-outputs-2025-12-15")
	}
	// CLI-FLAG-44: --permission-prompt-tool delegates permission asks to a named MCP tool.
	// CC ref: src/main.tsx:--permission-prompt-tool; src/cli/structuredIO.ts:623.
	if options.PermissionPromptTool != "" {
		_ = os.Setenv("CLAUDE_PERMISSION_PROMPT_TOOL", options.PermissionPromptTool)
		runner.Tools.Asker = &conversation.MCPPermissionAsker{
			ToolName: strings.TrimSpace(options.PermissionPromptTool),
			Registry: runner.Tools.Registry,
		}
	}
	// F2-C04: --max-budget-usd sets a cost ceiling; turn loop checks this after each turn.
	// CC ref: src/main.tsx:--max-budget-usd.
	if options.MaxBudgetUSD > 0 {
		runner.MaxBudgetUSD = options.MaxBudgetUSD
	}

	// F2-C05: --system-prompt-file reads a file and uses its content as the system prompt.
	// CC ref: src/main.tsx:--system-prompt-file.
	if options.SystemPromptFile != "" {
		data, err := os.ReadFile(options.SystemPromptFile)
		if err != nil {
			return conversation.Runner{}, fmt.Errorf("--system-prompt-file: %w", err)
		}
		options.SystemPrompt = string(data)
		options.AppendSystem = ""
	}
	// F2-C05: --append-system-prompt-file reads a file and appends its content.
	// CC ref: src/main.tsx:--append-system-prompt-file.
	if options.AppendSystemPromptFile != "" {
		data, err := os.ReadFile(options.AppendSystemPromptFile)
		if err != nil {
			return conversation.Runner{}, fmt.Errorf("--append-system-prompt-file: %w", err)
		}
		options.AppendSystem = strings.TrimSpace(options.AppendSystem) + "\n" + string(data)
	}
	// F2-C05: --disable-slash-commands disables skill/slash command discovery.
	// CC ref: src/main.tsx:--disable-slash-commands.
	if options.DisableSlashCommands {
		runner.SkillDirs = nil
	}

	// CLI-FLAG-47: --plugin-dir adds extra plugin directories for this session.
	// Each directory is treated as a plugin root: its MCP servers are merged into
	// runner.MCP.PluginServers and its <dir>/skills sub-directory is added to
	// runner.SkillDirs for slash-command discovery.
	// CC ref: src/main.tsx:--plugin-dir (adds to installedPluginRoots).
	if len(options.PluginDirs) > 0 {
		pluginMergedSettings := runnerMergedSettings(runner)
		extraServers := pluginpkg.LoadMCPServersWithSettings(options.PluginDirs, pluginMergedSettings)
		if len(extraServers) > 0 {
			if runner.MCP == nil {
				runner.MCP = &conversation.MCPConfig{CWD: runner.WorkingDirectory}
			}
			for name, server := range extraServers {
				if runner.MCP.PluginServers == nil {
					runner.MCP.PluginServers = make(map[string]contracts.MCPServer)
				}
				runner.MCP.PluginServers[name] = server
			}
		}
		// Add the <plugin-dir>/skills sub-directory to the runner skill-dir scan
		// so that slash commands defined in the plugin are discoverable.
		// This matches CC's behaviour where installedPluginRoots are scanned for skills.
		for _, dir := range options.PluginDirs {
			skillsDir := filepath.Join(dir, "skills")
			if info, statErr := os.Stat(skillsDir); statErr == nil && info.IsDir() {
				runner.SkillDirs = append(runner.SkillDirs, skillsDir)
			}
		}
	}

	// CLI-FLAG-27/28: --agent looks up a named agent from the project or user agent dirs and
	// applies its model and system prompt. --agents provides inline JSON agent definitions as
	// a fallback when the named agent is not found on disk.
	// CC ref: src/main.tsx:--agent; src/agents/registry.ts lookupAgent.
	if options.Agent != "" {
		agentPrompt, agentModel := resolveAgentSettings(
			strings.TrimSpace(options.Agent),
			strings.TrimSpace(options.Agents),
			runner.WorkingDirectory,
		)
		if agentModel != "" {
			runner.Model = agentModel
		}
		if agentPrompt != "" {
			options.AppendSystem = joinNonEmpty(options.AppendSystem, agentPrompt)
		}
		_ = os.Setenv("CLAUDE_CODE_AGENT", strings.TrimSpace(options.Agent))
	}

	runner.SystemPrompt = combineSystemPrompt(options.SystemPrompt, options.AppendSystem)
	// ORCH-35: BaseSystemPrompt stores the system prompt before claudeMd is
	// appended, so sub-agents with omitClaudeMd:true can use a claudeMd-free prompt.
	runner.BaseSystemPrompt = runner.SystemPrompt
	// CFG-44: claudeMdExcludes patterns are read from merged settings.
	var claudeMdExcludes []string
	if runner.MCP != nil {
		claudeMdExcludes = runner.MCP.MergedSettings().ClaudeMdExcludes
	}
	if claudeCtx := loadClaudeMdContext(runner.WorkingDirectory, claudeMdExcludes...); claudeCtx != "" {
		if runner.SystemPrompt != "" {
			runner.SystemPrompt = runner.SystemPrompt + "\n\n" + claudeCtx
		} else {
			runner.SystemPrompt = claudeCtx
		}
	}
	if runner.SessionPath == "" && runner.SessionID != "" {
		runner.SessionPath = session.TranscriptPath(runner.WorkingDirectory, runner.SessionID)
	}

	// Wire rewind seams (REWIND-01): ReadState, RewindWriter, RewindStore.
	// ReadState accumulates file reads/writes across tool calls for post-compact
	// file re-attachment. RewindWriter/RewindStore record file-history snapshots
	// at each turn boundary so sessions can be rewound to any prior state.
	runner.ReadState = filetools.NewReadState()
	if runner.SessionPath != "" {
		store := rewind.NewStore(filepath.Dir(runner.SessionPath))
		runner.RewindWriter = &rewind.Writer{TranscriptPath: runner.SessionPath}
		runner.RewindStore = &store
	}

	mergedSettings := runnerMergedSettings(runner)
	client, apiKeySource, err := anthropicClientFromEnv(ctx, runner.FastMode, mergedSettings.APIKeyHelper)
	if err != nil {
		return runner, err
	}
	runner.Client = client
	runner.APIKeySource = apiKeySource
	runner.BetaHeaders = append(runner.BetaHeaders, client.Beta...)

	// SBX-35: AutoAllowBashIfSandboxed — when sandbox is enabled and autoAllowBashIfSandboxed
	// is true, Bash tool calls skip the permission prompt (the sandbox itself confines them).
	// CC ref: src/utils/sandbox/sandbox-adapter.ts:471.
	{
		policySettingsForSandbox := contracts.Settings{}
		if runner.MCP != nil {
			policySettingsForSandbox = runner.MCP.PolicySettings
		}
		sandboxPolicy := sandbox.PolicyFromSettings(policySettingsForSandbox)
		if sandboxPolicy.Enabled && sandboxPolicy.AutoAllowBashIfSandboxed {
			runner.Tools.SandboxBashAutoAllow = true
		}
	}

	// CLI-FLAG-38: --file <file_id:relative_path> downloads files from the Anthropic
	// Files API to disk before the session starts.
	// CC ref: src/services/api/filesApi.ts downloadSessionFiles; main.tsx:1304-1330.
	// Note: CC requires CLAUDE_CODE_SESSION_ACCESS_TOKEN (cloud-session ingress).
	// ccgo uses the standard API key via FilesClient — same endpoint, local-friendly.
	if len(options.Files) > 0 {
		fc := &anthropic.FilesClient{APIKey: client.APIKey}
		if err := downloadFileSpecs(ctx, fc, options.Files, runner.WorkingDirectory); err != nil {
			return runner, fmt.Errorf("--file: %w", err)
		}
	}
	return runner, nil
}

// engineFromDecider extracts the live *permissions.Engine from a PermissionDecider
// if it wraps an EnginePermissionDecider. Returns nil for other decider types.
func engineFromDecider(decider tool.PermissionDecider) *permissions.Engine {
	switch v := decider.(type) {
	case tool.EnginePermissionDecider:
		eng := v.Engine
		return &eng
	case *tool.EnginePermissionDecider:
		if v != nil {
			eng := v.Engine
			return &eng
		}
	}
	return nil
}

func runnerPermissionModeFromDecider(decider tool.PermissionDecider) contracts.PermissionMode {
	switch value := decider.(type) {
	case tool.EnginePermissionDecider:
		return value.Engine.Mode()
	case *tool.EnginePermissionDecider:
		if value != nil {
			return value.Engine.Mode()
		}
	}
	return ""
}

func applyMCPConfigFlag(runner *conversation.Runner, raw string) error {
	path := strings.TrimSpace(raw)
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		base := runner.WorkingDirectory
		if base == "" {
			base = "."
		}
		path = filepath.Join(base, path)
	}
	settings, err := config.LoadSettingsFile(path)
	if err != nil {
		return fmt.Errorf("load --mcp-config %s: %w", path, err)
	}
	if runner.MCP == nil {
		runner.MCP = &conversation.MCPConfig{CWD: runner.WorkingDirectory}
	}
	runner.MCP.LocalSettings = config.MergeSettings(runner.MCP.LocalSettings, settings)
	return nil
}

// applySettingsFlag loads an extra settings file (or JSON string) specified via
// --settings and merges it at the highest local precedence, matching CC behaviour
// where --settings overrides all file-based sources.
// CC ref: src/main.tsx:--settings option.
func applySettingsFlag(runner *conversation.Runner, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var settings contracts.Settings
	// If the value looks like a JSON object, parse it inline.
	if strings.HasPrefix(raw, "{") {
		if err := json.Unmarshal([]byte(raw), &settings); err != nil {
			return fmt.Errorf("--settings: invalid JSON: %w", err)
		}
	} else {
		// Otherwise treat it as a file path.
		path := raw
		if !filepath.IsAbs(path) {
			base := runner.WorkingDirectory
			if base == "" {
				base = "."
			}
			path = filepath.Join(base, path)
		}
		var err error
		settings, err = config.LoadSettingsFile(path)
		if err != nil {
			return fmt.Errorf("--settings: load %s: %w", path, err)
		}
	}
	if runner.MCP == nil {
		runner.MCP = &conversation.MCPConfig{CWD: runner.WorkingDirectory}
	}
	// Merge at highest local precedence (LocalSettings is applied last in MergedSettings).
	runner.MCP.LocalSettings = config.MergeSettings(runner.MCP.LocalSettings, settings)
	return nil
}

func combineSystemPrompt(systemPrompt string, appendSystem string) string {
	base := strings.TrimSpace(systemPrompt)
	extra := strings.TrimSpace(appendSystem)
	switch {
	case base != "" && extra != "":
		return base + "\n\n" + extra
	case base != "":
		return base
	default:
		return extra
	}
}

// resolveAgentSettings looks up the named agent from disk (project dir first,
// then user dir) and, if not found, from the inlineAgentsJSON string.
// Returns the agent's system prompt and model (either may be empty).
// CLI-FLAG-27/28: --agent and --agents.
// CC ref: src/main.tsx:--agent; src/agents/registry.ts lookupAgent.
func resolveAgentSettings(name, inlineAgentsJSON, cwd string) (prompt, model string) {
	// 1. Try disk: project agents dir, then user agents dir.
	projectDir := agentfile.ProjectDir(cwd)
	userDir, _ := agentfile.UserDir()
	agents, err := agentfile.List(projectDir, userDir)
	if err == nil {
		for _, a := range agents {
			if strings.EqualFold(a.Name, name) {
				return a.Prompt, a.Model
			}
		}
	}
	// 2. Fall back to inline JSON definitions provided via --agents.
	if inlineAgentsJSON == "" {
		return "", ""
	}
	var defs map[string]struct {
		Prompt string `json:"prompt"`
		Model  string `json:"model"`
	}
	if jsonErr := json.Unmarshal([]byte(inlineAgentsJSON), &defs); jsonErr != nil {
		return "", ""
	}
	if def, ok := defs[name]; ok {
		return def.Prompt, def.Model
	}
	return "", ""
}

// joinNonEmpty concatenates two strings with "\n\n" separator, skipping blank parts.
func joinNonEmpty(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	switch {
	case a != "" && b != "":
		return a + "\n\n" + b
	case a != "":
		return a
	default:
		return b
	}
}

// loadClaudeMdContext loads the scoped CLAUDE.md hierarchy for cwd and returns
// the concatenated content of all discovered documents (imports expanded, in
// precedence order). Returns an empty string when no CLAUDE.md files exist.
// excludePatterns is applied per CFG-44 (claudeMdExcludes).
// Errors are treated as non-fatal: the function logs to stderr and returns "".
func loadClaudeMdContext(cwd string, excludePatterns ...string) string {
	opts := memory.LoadOptions{
		Scope:           memory.DefaultScopeOptions(cwd),
		ExcludePatterns: excludePatterns,
	}
	docs, err := memory.LoadScopedClaudeContext(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load CLAUDE.md context: %v\n", err)
		return ""
	}
	var parts []string
	for _, doc := range docs {
		if trimmed := strings.TrimSpace(doc.Content); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, "\n\n")
}

func resumeHistory(state *bootstrap.State, runner *conversation.Runner, options cliOptions) ([]contracts.Message, error) {
	if strings.TrimSpace(options.Resume) == "" && !options.Continue {
		return nil, nil
	}
	if strings.TrimSpace(options.Resume) != "" && options.Continue {
		return nil, fmt.Errorf("--resume and --continue cannot be used together")
	}
	sessionID, transcriptPath, err := resolveResumeTarget(state.CWD(), options.Resume, options.Continue)
	if err != nil {
		return nil, err
	}
	resumed, err := session.BuildResumeConversation(transcriptPath, "")
	if err != nil {
		return nil, err
	}
	if !resumed.Found {
		return nil, fmt.Errorf("resume session %q has no resumable messages", sessionID)
	}
	// F2-C02: --fork-session creates a new session ID instead of reusing the original.
	// CC ref: src/main.tsx:--fork-session.
	if options.ForkSession {
		runner.SessionID = contracts.NewID()
		runner.SessionPath = session.TranscriptPath(state.CWD(), runner.SessionID)
	} else {
		runner.SessionID = sessionID
		runner.SessionPath = transcriptPath
	}
	return resumed.Messages, nil
}

func resolveResumeTarget(cwd string, resumeValue string, continueMode bool) (contracts.ID, string, error) {
	if continueMode {
		sessions, err := session.ListProjectSessions(cwd)
		if err != nil {
			return "", "", err
		}
		if len(sessions) == 0 {
			return "", "", fmt.Errorf("no sessions found for %s", cwd)
		}
		return sessions[0].ID, sessions[0].Path, nil
	}
	resumeValue = strings.TrimSpace(resumeValue)
	if resumeValue == "" {
		return "", "", nil
	}
	if strings.HasSuffix(resumeValue, ".jsonl") || strings.ContainsAny(resumeValue, `/\`) {
		path := resumeValue
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		id := contracts.ID(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
		return id, path, nil
	}
	id := contracts.ID(resumeValue)
	localPath := session.TranscriptPath(cwd, id)
	if _, err := os.Stat(localPath); err == nil {
		return id, localPath, nil
	}
	// Fall back: search all project directories.
	globalPath, found, err := session.FindSessionGlobally(resumeValue)
	if err != nil {
		return "", "", fmt.Errorf("searching for session %q: %w", resumeValue, err)
	}
	if found {
		return id, globalPath, nil
	}
	return id, localPath, nil
}

func parseToolRules(raw ...string) []string {
	return commands.ParseToolList(raw)
}

// applySettingSourcesFilter removes settings entries for sources not named in
// the comma-separated sources string. Policy settings (managed layer) are never
// removed. Valid source names: "user", "project", "local".
// CC ref: src/utils/settings/constants.ts parseSettingSourcesFlag.
func applySettingSourcesFilter(mcpConfig *conversation.MCPConfig, sources string) {
	if mcpConfig == nil || strings.TrimSpace(sources) == "" {
		return
	}
	allowed := make(map[string]bool)
	for _, part := range strings.Split(sources, ",") {
		allowed[strings.TrimSpace(part)] = true
	}
	if !allowed["user"] {
		mcpConfig.UserSettings = contracts.Settings{}
	}
	if !allowed["project"] {
		mcpConfig.ProjectSettings = contracts.Settings{}
	}
	if !allowed["local"] {
		mcpConfig.LocalSettings = contracts.Settings{}
	}
}

// toolsNotInWhitelist returns the subset of allNames not present in whitelist.
// It implements the CLI-FLAG-31 --tools deny logic: any tool whose canonical
// (lower-case) name is not in the whitelist set is returned for denial.
// CC ref: src/utils/permissions/permissionSetup.ts baseToolsCli deny filter.
func toolsNotInWhitelist(allNames []string, whitelist []string) []string {
	if len(whitelist) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(whitelist))
	for _, name := range whitelist {
		allowed[strings.ToLower(strings.TrimSpace(name))] = true
	}
	var denied []string
	for _, name := range allNames {
		if !allowed[strings.ToLower(name)] {
			denied = append(denied, name)
		}
	}
	return denied
}

func parsePathList(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		for _, field := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r'
		}) {
			trimmed := strings.TrimSpace(field)
			if trimmed == "" || seen[trimmed] {
				continue
			}
			seen[trimmed] = true
			out = append(out, trimmed)
		}
	}
	return out
}

func permissionDeciderFromSettings(mcpConfig *conversation.MCPConfig, permissionMode string, allowedTools []string, deniedTools []string, additionalDirs []string) (tool.PermissionDecider, error) {
	var sources []permissions.SettingsSource
	var managedRulesOnly bool
	if mcpConfig != nil {
		merged := mcpConfig.MergedSettings()
		managedRulesOnly = merged.AllowManagedPermissionRulesOnly != nil && *merged.AllowManagedPermissionRulesOnly
		sources = append(sources,
			permissions.SettingsSource{Source: contracts.PermissionSourceUserSettings, Permissions: mcpConfig.UserSettings.Permissions, Sandbox: mcpConfig.UserSettings.Sandbox},
			permissions.SettingsSource{Source: contracts.PermissionSourceProjectSettings, Permissions: mcpConfig.ProjectSettings.Permissions, Sandbox: mcpConfig.ProjectSettings.Sandbox},
			permissions.SettingsSource{Source: contracts.PermissionSourceLocalSettings, Permissions: mcpConfig.LocalSettings.Permissions, Sandbox: mcpConfig.LocalSettings.Sandbox},
			permissions.SettingsSource{Source: contracts.PermissionSourcePolicySettings, Permissions: mcpConfig.PolicySettings.Permissions, Sandbox: mcpConfig.PolicySettings.Sandbox},
		)
	}
	if permissionMode != "" {
		mode := contracts.PermissionMode(permissionMode)
		if !validPermissionMode(mode) {
			return nil, fmt.Errorf("invalid permission mode %q", permissionMode)
		}
		sources = append(sources, permissions.SettingsSource{
			Source:      contracts.PermissionSourceCLIArg,
			Permissions: cliPermissionsSetting(mode, allowedTools, deniedTools, additionalDirs),
		})
	} else if len(allowedTools) > 0 || len(deniedTools) > 0 || len(additionalDirs) > 0 {
		sources = append(sources, permissions.SettingsSource{
			Source:      contracts.PermissionSourceCLIArg,
			Permissions: cliPermissionsSetting("", allowedTools, deniedTools, additionalDirs),
		})
	}
	engine, err := permissions.NewEngineFromSettingsSources(managedRulesOnly, sources...)
	if err != nil {
		return nil, err
	}
	return tool.NewEnginePermissionDecider(engine), nil
}

func cliPermissionsSetting(mode contracts.PermissionMode, allowedTools []string, deniedTools []string, additionalDirs []string) *contracts.PermissionsSetting {
	return &contracts.PermissionsSetting{
		DefaultMode:           mode,
		Allow:                 append([]string(nil), allowedTools...),
		Deny:                  append([]string(nil), deniedTools...),
		AdditionalDirectories: append([]string(nil), additionalDirs...),
	}
}

func validPermissionMode(mode contracts.PermissionMode) bool {
	switch mode {
	case contracts.PermissionDefault,
		contracts.PermissionAcceptEdits,
		contracts.PermissionBypassPermissions,
		contracts.PermissionDontAsk,
		contracts.PermissionPlan,
		contracts.PermissionAuto,
		contracts.PermissionBubble:
		return true
	default:
		return false
	}
}

func resolveCLIModel(flagValue string, mcpConfig *conversation.MCPConfig) string {
	raw := firstNonEmpty(flagValue, os.Getenv("ANTHROPIC_MODEL"), os.Getenv("CLAUDE_MODEL"))
	if raw == "" && mcpConfig != nil {
		raw = mcpConfig.MergedSettings().Model
	}
	if capability, ok := model.DefaultRegistry().Resolve(raw); ok {
		return capability.Name
	}
	return strings.TrimSpace(raw)
}

func anthropicClientFromEnv(ctx context.Context, fastMode bool, apiKeyHelperCmd string) (*anthropic.Client, string, error) {
	credentials, credentialStore, err := credentialsFromEnvOrStore(ctx, apiKeyHelperCmd)
	if err != nil {
		return nil, "", err
	}
	if credentials.Source == auth.SourceNone {
		return nil, "", fmt.Errorf("missing Anthropic credentials; set ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_REFRESH_TOKEN")
	}
	var tokenProvider *auth.OAuthTokenProvider
	if credentials.Source == auth.SourceOAuth && strings.TrimSpace(credentials.RefreshToken) != "" {
		tokenProvider = auth.NewOAuthTokenProvider(auth.OAuthTokenProviderOptions{
			Credentials:     credentials,
			CredentialStore: credentialStore,
		})
	}
	if credentials.Source == auth.SourceOAuth && strings.TrimSpace(credentials.AccessToken) == "" && strings.TrimSpace(credentials.RefreshToken) != "" {
		token, err := tokenProvider.CurrentAccessToken(ctx)
		if err != nil {
			return nil, "", err
		}
		credentials.AccessToken = token
	}
	if err := credentials.Validate(); err != nil {
		return nil, "", err
	}
	options := []anthropic.Option{
		anthropic.WithCredentials(credentials),
		anthropic.WithUserAgent("ccgo/" + version),
	}
	if tokenProvider != nil {
		options = append(options, anthropic.WithAccessTokenProvider(tokenProvider))
	}
	if baseURL := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); baseURL != "" {
		options = append(options, anthropic.WithBaseURL(baseURL))
	}
	beta := splitEnvList(os.Getenv("ANTHROPIC_BETA"))
	if fastMode {
		beta = anthropic.MergeBetaHeaders(beta, []string{anthropic.FastModeBetaHeader})
	}
	if len(beta) > 0 {
		options = append(options, anthropic.WithBeta(beta...))
	}
	customHeaders, err := customHeadersFromEnv()
	if err != nil {
		return nil, "", err
	}
	if len(customHeaders) > 0 {
		options = append(options, anthropic.WithHeaders(customHeaders))
	}
	return anthropic.NewClient(options...), string(credentials.Source), nil
}

func credentialsFromEnvOrStore(ctx context.Context, apiKeyHelperCmd string) (auth.Credentials, auth.CredentialStore, error) {
	// apiKeyHelper wins over all other sources when configured (matches CC's
	// utils/auth.ts:320-335 precedence: helper > env > keychain).
	if helperCmd := strings.TrimSpace(apiKeyHelperCmd); helperCmd != "" {
		if key, err := auth.NewAPIKeyHelperResolver(helperCmd).Resolve(ctx); err == nil && key != "" {
			return auth.Credentials{Source: auth.SourceAPIKey, APIKey: key}, nil, nil
		}
		// On helper error, fall through to env/keychain (do not abort).
	}
	credentials := auth.FromEnv()
	if credentials.Source != auth.SourceNone {
		return credentials, nil, nil
	}
	// Try keychain-backed store (macOS keychain; file on other platforms).
	keychainStore := auth.NewKeychainCredentialStore("")
	stored, err := keychainStore.Load(ctx)
	if err != nil {
		return auth.Credentials{}, nil, err
	}
	if stored.Source != auth.SourceNone {
		return stored, keychainStore, nil
	}
	// Migration fallback: also check the plain credentials.json file so users
	// who stored credentials before the keychain store was introduced continue
	// to work without needing to re-login.
	fileStore := auth.NewFileCredentialStore("")
	fileCreds, err := fileStore.Load(ctx)
	if err != nil {
		return auth.Credentials{}, nil, err
	}
	if fileCreds.Source == auth.SourceNone {
		return fileCreds, nil, nil
	}
	return fileCreds, fileStore, nil
}

func splitEnvList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := fields[:0]
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func customHeadersFromEnv() (http.Header, error) {
	var headers http.Header
	for _, name := range []string{"ANTHROPIC_CUSTOM_HEADERS", "CLAUDE_CODE_CUSTOM_HEADERS"} {
		parsed, err := parseCustomHeadersEnv(name, os.Getenv(name))
		if err != nil {
			return nil, err
		}
		for key, values := range parsed {
			if headers == nil {
				headers = http.Header{}
			}
			for _, value := range values {
				headers.Add(key, value)
			}
		}
	}
	return headers, nil
}

func parseCustomHeadersEnv(name string, value string) (http.Header, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "{") {
		return parseCustomHeadersJSON(name, []byte(trimmed))
	}
	headers := http.Header{}
	for index, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		separator := strings.Index(line, ":")
		if separator < 0 {
			separator = strings.Index(line, "=")
		}
		if separator < 0 {
			return nil, fmt.Errorf("%s line %d must use `Header: value` or `Header=value`", name, index+1)
		}
		if err := addCustomHeader(headers, line[:separator], line[separator+1:]); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", name, index+1, err)
		}
	}
	if len(headers) == 0 {
		return nil, nil
	}
	return headers, nil
}

func parseCustomHeadersJSON(name string, data []byte) (http.Header, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s must be valid JSON object or header lines: %w", name, err)
	}
	headers := http.Header{}
	for key, value := range raw {
		switch typed := value.(type) {
		case string:
			if err := addCustomHeader(headers, key, typed); err != nil {
				return nil, fmt.Errorf("%s.%s: %w", name, key, err)
			}
		case []any:
			for index, item := range typed {
				text, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("%s.%s[%d] must be a string", name, key, index)
				}
				if err := addCustomHeader(headers, key, text); err != nil {
					return nil, fmt.Errorf("%s.%s[%d]: %w", name, key, index, err)
				}
			}
		case nil:
			continue
		default:
			return nil, fmt.Errorf("%s.%s must be a string or string array", name, key)
		}
	}
	if len(headers) == 0 {
		return nil, nil
	}
	return headers, nil
}

func addCustomHeader(headers http.Header, key string, value string) error {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if !validHeaderName(key) {
		return fmt.Errorf("invalid header name %q", key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("header %q contains a newline", key)
	}
	headers.Add(http.CanonicalHeaderKey(key), value)
	return nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type printJSONResult struct {
	Type            string                   `json:"type"`
	Subtype         string                   `json:"subtype"`
	IsError         bool                     `json:"is_error"`
	DurationMS      int64                    `json:"duration_ms"`
	DurationAPI     int64                    `json:"duration_api_ms"`
	NumTurns        int                      `json:"num_turns,omitempty"`
	TotalCost       float64                  `json:"total_cost_usd,omitempty"`
	SessionID       contracts.ID             `json:"session_id,omitempty"`
	CWD             string                   `json:"cwd,omitempty"`
	PermissionMode  string                   `json:"permission_mode,omitempty"`
	APIKeySource    string                   `json:"api_key_source,omitempty"`
	Betas           []string                 `json:"betas,omitempty"`
	FastMode        bool                     `json:"fast_mode,omitempty"`
	OutputStyle     string                   `json:"output_style,omitempty"`
	OutputStyles    []string                 `json:"available_output_styles,omitempty"`
	Result          string                   `json:"result"`
	Error           string                   `json:"error,omitempty"`
	ErrorType       string                   `json:"error_type,omitempty"`
	StatusCode      int                      `json:"status_code,omitempty"`
	RequestID       string                   `json:"request_id,omitempty"`
	Message         *contracts.Message       `json:"message,omitempty"`
	StopReason      string                   `json:"stop_reason,omitempty"`
	Model           string                   `json:"model,omitempty"`
	ModelsAttempted []string                 `json:"models_attempted,omitempty"`
	Usage           *contracts.Usage         `json:"usage,omitempty"`
	ToolResults     []contracts.ToolResult   `json:"tool_results,omitempty"`
	Cleared         bool                     `json:"cleared,omitempty"`
	Compacted       bool                     `json:"compacted,omitempty"`
	Compact         *session.CompactMetadata `json:"compact,omitempty"`
}

type printStreamEvent struct {
	Type            conversation.EventType   `json:"type"`
	Subtype         string                   `json:"subtype,omitempty"`
	SessionID       contracts.ID             `json:"session_id,omitempty"`
	CWD             string                   `json:"cwd,omitempty"`
	Tools           []string                 `json:"tools,omitempty"`
	MCPServers      []printStreamMCPServer   `json:"mcp_servers,omitempty"`
	SlashCommands   []string                 `json:"slash_commands,omitempty"`
	Agents          []string                 `json:"agents,omitempty"`
	Skills          []string                 `json:"skills,omitempty"`
	Plugins         []printStreamPlugin      `json:"plugins,omitempty"`
	PermissionMode  string                   `json:"permission_mode,omitempty"`
	APIKeySource    string                   `json:"api_key_source,omitempty"`
	Betas           []string                 `json:"betas,omitempty"`
	FastMode        bool                     `json:"fast_mode,omitempty"`
	OutputStyle     string                   `json:"output_style,omitempty"`
	OutputStyles    []string                 `json:"available_output_styles,omitempty"`
	Message         *contracts.Message       `json:"message,omitempty"`
	ToolUse         *contracts.ToolUse       `json:"tool_use,omitempty"`
	ToolResult      *contracts.ToolResult    `json:"tool_result,omitempty"`
	ToolUseID       contracts.ID             `json:"tool_use_id,omitempty"`
	ProgressType    string                   `json:"progress_type,omitempty"`
	Data            map[string]any           `json:"data,omitempty"`
	Retry           *printStreamRetry        `json:"retry,omitempty"`
	TokenWarning    *printStreamTokenWarning `json:"token_warning,omitempty"`
	Compact         any                      `json:"compact,omitempty"`
	StreamEvent     *anthropic.StreamEvent   `json:"stream_event,omitempty"`
	IsPartial       bool                     `json:"is_partial,omitempty"`
	Model           string                   `json:"model,omitempty"`
	ModelsAttempted []string                 `json:"models_attempted,omitempty"`
	Error           string                   `json:"error,omitempty"`
	ErrorType       string                   `json:"error_type,omitempty"`
	StatusCode      int                      `json:"status_code,omitempty"`
	RequestID       string                   `json:"request_id,omitempty"`
	IsError         bool                     `json:"is_error,omitempty"`
	DurationMS      *int64                   `json:"duration_ms,omitempty"`
	DurationAPI     *int64                   `json:"duration_api_ms,omitempty"`
}

type printStreamRetry struct {
	Attempt     int    `json:"attempt,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
	FailedModel string `json:"failed_model,omitempty"`
	NextModel   string `json:"next_model,omitempty"`
	Fallback    bool   `json:"fallback,omitempty"`
}

type printStreamTokenWarning struct {
	TokenUsage int                          `json:"token_usage"`
	Window     printStreamTokenWindow       `json:"window"`
	State      printStreamTokenWarningState `json:"state"`
}

type printStreamTokenWindow struct {
	ContextWindow       int      `json:"context_window,omitempty"`
	MaxOutputTokens     int      `json:"max_output_tokens,omitempty"`
	AutoCompactEnabled  bool     `json:"auto_compact_enabled,omitempty"`
	AutoCompactOverride *float64 `json:"auto_compact_override,omitempty"`
	BlockingLimit       int      `json:"blocking_limit,omitempty"`
}

type printStreamTokenWarningState struct {
	PercentLeft                 int  `json:"percent_left"`
	IsAboveWarningThreshold     bool `json:"is_above_warning_threshold"`
	IsAboveErrorThreshold       bool `json:"is_above_error_threshold"`
	IsAboveAutoCompactThreshold bool `json:"is_above_auto_compact_threshold"`
	IsAtBlockingLimit           bool `json:"is_at_blocking_limit"`
}

type printStreamPlugin struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Source string `json:"source"`
}

type printStreamMCPServer struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
	Type         string `json:"type,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Source       string `json:"source,omitempty"`
	PluginSource string `json:"plugin_source,omitempty"`
}

// attachStreamJSON wires an NDJSON stream-json event handler onto runner.
// When includePartialMessages is true, each text_delta EventStreamEvent also
// emits a synthetic {"type":"assistant_message","is_partial":true} event
// carrying the accumulated text so far — matching CC's --include-partial-messages
// behaviour (F2-C04 / partial-messages in 01-headless.md).
// When includeHookEvents is true, hook lifecycle events with any scope (not just
// "conversation") are emitted — matching CC's --include-hook-events behaviour
// (CLI-FLAG-41). This enables pre_turn/post_turn/setup hook events in the stream.
func attachStreamJSON(stdout io.Writer, runner conversation.Runner, includePartialMessages, includeHookEvents bool) (conversation.Runner, func() error) {
	encoder := json.NewEncoder(stdout)
	var eventErr error
	eventErr = encoder.Encode(printStreamEvent{
		Type:           "system",
		Subtype:        "init",
		SessionID:      runner.SessionID,
		CWD:            runner.WorkingDirectory,
		Tools:          runnerToolNames(runner),
		MCPServers:     runnerMCPServerSummaries(runner),
		SlashCommands:  runnerSlashCommandNames(runner),
		Agents:         runnerAgentNames(runner),
		Skills:         runnerSkillNames(runner),
		Plugins:        runnerPluginSummaries(runner),
		PermissionMode: string(runner.PermissionMode),
		APIKeySource:   runner.APIKeySource,
		Betas:          append([]string(nil), runner.BetaHeaders...),
		FastMode:       runner.FastMode,
		OutputStyle:    runner.EffectiveOutputStyleName(),
		OutputStyles:   runner.AvailableOutputStyleNames(),
		Model:          runner.Model,
	})

	// partialBuf accumulates text delta chunks when includePartialMessages is
	// true, so each synthetic partial assistant_message carries the full text
	// received so far (not just the latest chunk).
	var partialBuf string

	// SDK-59: track whether an LLM event has been seen so we can emit
	// session_state_changed/running before the first LLM event and
	// session_state_changed/idle in the teardown function.
	// CC ref: coreSchemas.ts:1735-1748 (SDKSessionStateChangedMessageSchema).
	// Use a sync.Once so the "running" event is emitted exactly once even if
	// multiple LLM events fire concurrently (safe under -race).
	var emitRunningOnce sync.Once
	var emittedRunning bool

	emitSessionState := func(state string) {
		_ = encoder.Encode(sdkSessionStateChangedEvent{
			Type:      "system",
			Subtype:   "session_state_changed",
			State:     state,
			SessionID: string(runner.SessionID),
		})
	}

	runner.OnEvent = func(event conversation.Event) {
		if eventErr != nil {
			return
		}
		// SDK-59: emit session_state_changed/running on the first LLM event
		// (assistant_message or stream_event signals LLM invocation started).
		// We do NOT emit for user-only events (like slash commands / /clear).
		// CC ref: coreSchemas.ts:1735-1748 (SDKSessionStateChangedMessageSchema).
		if isLLMEvent(event.Type) {
			emitRunningOnce.Do(func() {
				emittedRunning = true
				emitSessionState("running")
			})
		}
		// SDK-55: emit system/status "compacting" before the compact event so that
		// SDK consumers can display a "compacting…" indicator.
		// CC ref: coreSchemas.ts:1533-1542 (SDKStatusMessageSchema).
		if event.Type == conversation.EventCompact {
			statusMsg := sdkpkg.SDKStatusMessage{
				Type:      "system",
				Subtype:   "status",
				Status:    "compacting",
				SessionID: string(runner.SessionID),
			}
			if err := encoder.Encode(statusMsg); err != nil {
				eventErr = fmt.Errorf("sdk: write system/status: %w", err)
				return
			}
		}
		// --include-partial-messages: on each text_delta emit a synthetic
		// assistant_message event with the accumulated text so far, so SDK
		// consumers can display streaming output incrementally.
		if includePartialMessages && event.Type == conversation.EventStreamEvent && event.StreamEvent != nil {
			se := event.StreamEvent
			if se.Type == "content_block_delta" && se.Delta != nil {
				var chunk string
				switch se.Delta["type"] {
				case "text_delta":
					chunk, _ = se.Delta["text"].(string)
				case "thinking_delta":
					chunk, _ = se.Delta["thinking"].(string)
				}
				if chunk != "" {
					partialBuf += chunk
					partialMsg := contracts.Message{
						Type: contracts.MessageAssistant,
						Content: []contracts.ContentBlock{
							{Type: contracts.ContentText, Text: partialBuf},
						},
					}
					partialEvent := printStreamEvent{
						Type:      conversation.EventAssistantMessage,
						IsPartial: true,
						Message:   &partialMsg,
					}
					if err := encoder.Encode(partialEvent); err != nil {
						eventErr = fmt.Errorf("stream-json: write partial assistant_message: %w", err)
						return
					}
				}
			}
		}
		// Clear partial buffer when the final assistant message arrives.
		if event.Type == conversation.EventAssistantMessage {
			partialBuf = ""
		}
		eventErr = writePrintStreamEvent(encoder, event, includeHookEvents)
	}
	return runner, func() error {
		// SDK-59: emit session_state_changed/idle after the turn completes,
		// but only if we previously emitted "running" (i.e. an LLM turn ran).
		// Slash commands (like /clear) that only emit user_message do NOT
		// trigger running/idle transitions.
		if emittedRunning {
			emitSessionState("idle")
		}
		return eventErr
	}
}

// isLLMEvent reports whether an event type signals that the LLM has been
// invoked (i.e. the session is actively processing an assistant response).
// SDK-59 uses this to emit session_state_changed/running on the first LLM event.
// CC ref: coreSchemas.ts:1735-1748 (SDKSessionStateChangedMessageSchema).
func isLLMEvent(eventType conversation.EventType) bool {
	switch eventType {
	case conversation.EventAssistantMessage,
		conversation.EventStreamEvent,
		conversation.EventToolUse,
		conversation.EventToolResult,
		conversation.EventToolProgress,
		conversation.EventRetry,
		conversation.EventTokenWarning,
		conversation.EventCompact:
		return true
	default:
		return false
	}
}

func runnerToolNames(runner conversation.Runner) []string {
	if runner.Tools.Registry == nil {
		return nil
	}
	return runner.Tools.Registry.Names()
}

func runnerMCPServerSummaries(runner conversation.Runner) []printStreamMCPServer {
	if runner.MCP == nil {
		return nil
	}
	states := runnerMCPServerStates(runner.MCP)
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]printStreamMCPServer, 0, len(names))
	for _, name := range names {
		state := states[name]
		server := state.Server
		source := server.Scope
		if server.PluginSource != "" {
			source = "plugin"
		}
		out = append(out, printStreamMCPServer{
			Name:         name,
			Status:       state.Status,
			Reason:       state.Reason,
			Type:         mcp.Transport(server),
			Scope:        server.Scope,
			Source:       source,
			PluginSource: server.PluginSource,
		})
	}
	return out
}

type mcpServerInitState struct {
	Server contracts.MCPServer
	Status string
	Reason string
}

func runnerMCPServerStates(cfg *conversation.MCPConfig) map[string]mcpServerInitState {
	if cfg == nil {
		return nil
	}
	mcpLocked := config.IsRestrictedToPluginOnly(cfg.PolicySettings, config.CustomizationSurfaceMCP)
	var user map[string]contracts.MCPServer
	var project map[string]contracts.MCPServer
	var local map[string]contracts.MCPServer
	if !mcpLocked {
		user = loadMCPServersForInit(cfg.UserSettings, mcp.ScopeUser, cfg.ParseOptions)
		project = loadMCPServersForInit(cfg.ProjectSettings, mcp.ScopeProject, cfg.ParseOptions)
		local = loadMCPServersForInit(cfg.LocalSettings, mcp.ScopeLocal, cfg.ParseOptions)
		if cfg.CWD != "" {
			if chain, err := mcp.LoadProjectConfigChain(cfg.CWD, cfg.ParseOptions); err == nil {
				project = mcp.MergeServers(project, chain.Servers)
			}
		}
	}
	policySettings := cfg.PolicySettings
	if !mcpLocked {
		policySettings = mergeMCPPolicySettingsForInit(cfg.UserSettings, cfg.ProjectSettings, cfg.LocalSettings, cfg.PolicySettings)
	}
	manual := mcp.MergeServers(user, project, local)
	plugin := mcp.DedupPluginServers(cfg.PluginServers, manual).Servers
	for name := range plugin {
		if _, exists := manual[name]; exists {
			delete(plugin, name)
		}
	}
	servers := mcp.MergeServers(manual, plugin)
	policy := mcp.PolicyFromSettings(policySettings)
	out := make(map[string]mcpServerInitState, len(servers))
	for name, server := range servers {
		decision := mcp.EvaluatePolicy(name, server, policy)
		state := mcpServerInitState{Server: server, Status: "configured"}
		if !decision.Allowed {
			state.Status = "blocked"
			state.Reason = decision.Reason
		}
		out[name] = state
	}
	return out
}

func loadMCPServersForInit(settings contracts.Settings, scope string, options mcp.ParseOptions) map[string]contracts.MCPServer {
	result, err := mcp.LoadSettingsServers(settings, scope, options)
	if err != nil {
		return nil
	}
	return result.Servers
}

func mergeMCPPolicySettingsForInit(settings ...contracts.Settings) contracts.Settings {
	var out contracts.Settings
	for _, setting := range settings {
		if setting.AllowedMCPServers != nil && out.AllowedMCPServers == nil {
			out.AllowedMCPServers = []contracts.MCPServerPolicyEntry{}
		}
		out.AllowedMCPServers = append(out.AllowedMCPServers, setting.AllowedMCPServers...)
		out.DeniedMCPServers = append(out.DeniedMCPServers, setting.DeniedMCPServers...)
	}
	return out
}

// downloadFileSpecs downloads Files API resources specified via --file flags.
// Each spec has the form "file_id:relative/path".  Files are saved relative to
// cwd.  All downloads are attempted; the first error aborts and is returned.
// CC ref: src/services/api/filesApi.ts downloadSessionFiles (CLI-FLAG-38).
func downloadFileSpecs(ctx context.Context, fc *anthropic.FilesClient, specs []string, cwd string) error {
	for _, spec := range specs {
		fileID, relPath, ok := anthropic.ParseFileSpec(spec)
		if !ok {
			return fmt.Errorf("invalid --file spec %q (want file_id:relative/path)", spec)
		}
		data, err := fc.DownloadFile(ctx, fileID)
		if err != nil {
			return fmt.Errorf("download %s: %w", fileID, err)
		}
		dst := filepath.Join(cwd, relPath)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("--file mkdir %s: %w", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("--file write %s: %w", dst, err)
		}
	}
	return nil
}

// buildInteractiveMCPManager constructs a live mcp.Manager from the runner's
// MCP configuration for use by the interactive REPL's /mcp overlay.
// It collects all policy-allowed servers (user + project + local + plugin),
// reusing the same ClientOpenFunc already stored in cfg.ToolOptions.OpenClient.
// Returns nil when cfg is nil or no servers are configured.
// Callers must call Manager.Start before use.
func buildInteractiveMCPManager(cfg *conversation.MCPConfig) *mcp.Manager {
	if cfg == nil {
		return nil
	}
	states := runnerMCPServerStates(cfg)
	servers := make(map[string]contracts.MCPServer, len(states))
	for name, state := range states {
		if state.Status == "configured" {
			servers[name] = state.Server
		}
	}
	if len(servers) == 0 {
		return nil
	}
	return mcp.NewManager(servers, cfg.ToolOptions.OpenClient)
}

func runnerSlashCommandNames(runner conversation.Runner) []string {
	registry := commands.Load(commands.Options{CWD: runner.WorkingDirectory, Settings: runnerMergedSettings(runner), PolicySettings: runnerPolicySettings(runner)})
	var names []string
	for _, cmd := range registry.Visible() {
		name := strings.TrimSpace(commands.UserFacingName(cmd))
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func runnerSkillNames(runner conversation.Runner) []string {
	registry := commands.Load(commands.Options{CWD: runner.WorkingDirectory, Settings: runnerMergedSettings(runner), PolicySettings: runnerPolicySettings(runner)})
	var names []string
	for _, cmd := range registry.Visible() {
		if cmd.Type != contracts.CommandPrompt || cmd.Source == contracts.CommandSourceBuiltin {
			continue
		}
		name := strings.TrimSpace(commands.UserFacingName(cmd))
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func runnerAgentNames(runner conversation.Runner) []string {
	plugins := runnerLocalPlugins(runner)
	var names []string
	for _, plugin := range plugins {
		for _, agent := range plugin.Agents {
			name := strings.TrimSpace(agent.Name)
			if name != "" {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func runnerPluginSummaries(runner conversation.Runner) []printStreamPlugin {
	plugins := runnerLocalPlugins(runner)
	out := make([]printStreamPlugin, 0, len(plugins))
	for _, plugin := range plugins {
		out = append(out, printStreamPlugin{
			Name:   plugin.Name,
			Path:   plugin.Root,
			Source: "local",
		})
	}
	return out
}

func runnerLocalPlugins(runner conversation.Runner) []pluginpkg.LoadedPlugin {
	return pluginpkg.LoadPluginDirsWithSettings(pluginpkg.InstalledPluginDirs(runner.WorkingDirectory), runnerMergedSettings(runner))
}

func runnerMergedSettings(runner conversation.Runner) contracts.Settings {
	if runner.MCP == nil {
		return contracts.Settings{}
	}
	return runner.MCP.MergedSettings()
}

func runnerPolicySettings(runner conversation.Runner) contracts.Settings {
	if runner.MCP == nil {
		return contracts.Settings{}
	}
	return runner.MCP.PolicySettings
}

// sdkHookStartedEvent is the CC-wire shape for system/hook_started events.
// CC ref: coreSchemas.ts:1604-1614 (SDKHookStartedMessageSchema, SDK-65).
type sdkHookStartedEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	HookID    string `json:"hook_id"`
	HookName  string `json:"hook_name"`
	HookEvent string `json:"hook_event"`
	SessionID string `json:"session_id,omitempty"`
}

// sdkHookResponseEvent is the CC-wire shape for system/hook_response events.
// CC ref: coreSchemas.ts:1631-1646 (SDKHookResponseMessageSchema, SDK-65).
type sdkHookResponseEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	HookID    string `json:"hook_id"`
	HookName  string `json:"hook_name"`
	HookEvent string `json:"hook_event"`
	Output    string `json:"output"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Outcome   string `json:"outcome"` // success | error | cancelled
	SessionID string `json:"session_id,omitempty"`
}

// sdkSessionStateChangedEvent is the CC-wire shape for system/session_state_changed.
// CC ref: coreSchemas.ts:1735-1748 (SDKSessionStateChangedMessageSchema, SDK-59).
type sdkSessionStateChangedEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	State     string `json:"state"` // idle | running | requires_action
	SessionID string `json:"session_id,omitempty"`
}

// hookProgressType reports whether a ToolProgress event represents a hook
// lifecycle event and returns the hook phase and progress type.
// Hook events have ToolUseID starting with "hook_" and a scope=="conversation"
// data field (set by Runner.emitConversationHookProgress).
// CC ref: coreSchemas.ts:1604-1646 (SDK-65).
// hookProgressPhase extracts the hook phase from a ToolProgress event.
// When includeHookEvents is true (--include-hook-events flag), hooks with any
// scope are included. By default, only "conversation"-scoped hook events are
// emitted (they are the ones relevant to the active turn).
// CC ref: src/main.tsx:1231 (setAllHookEventsEnabled when includeHookEvents).
func hookProgressPhase(tp *contracts.ToolProgress, includeHookEvents bool) (phase string, isHook bool) {
	if tp == nil {
		return "", false
	}
	data := tp.Data
	if data == nil {
		return "", false
	}
	scope, _ := data["scope"].(string)
	if !includeHookEvents && scope != "conversation" {
		return "", false
	}
	// Only emit if the event has a recognisable hook phase tag.
	if scope == "" {
		return "", false
	}
	phase, _ = data["phase"].(string)
	return phase, phase != ""
}

func writePrintStreamEvent(encoder *json.Encoder, event conversation.Event, includeHookEvents bool) error {
	if !printStreamEventVisible(event.Type) {
		return nil
	}

	// SDK-65: hook lifecycle events are emitted in CC's system/hook_* wire format
	// instead of the generic tool_progress format. This matches CC's
	// SDKHookStartedMessageSchema / SDKHookResponseMessageSchema.
	// CLI-FLAG-41: when --include-hook-events is set, hooks with non-conversation
	// scopes (pre_turn, post_turn, etc.) are also emitted.
	if event.Type == conversation.EventToolProgress && event.ToolProgress != nil {
		phase, isHook := hookProgressPhase(event.ToolProgress, includeHookEvents)
		if isHook {
			return writeHookLifecycleEvent(encoder, event.ToolProgress, phase)
		}
		// Hook ToolProgress events that are not emitted (wrong scope / no phase)
		// are completely suppressed — they must not fall through to the generic
		// tool_progress path, which would expose internal hook scope details.
		if isHookToolProgress(event.ToolProgress) {
			return nil
		}
	}

	out := printStreamEvent{
		Type:         event.Type,
		Message:      event.Message,
		ToolUse:      event.ToolUse,
		ToolResult:   event.ToolResult,
		Retry:        printStreamRetryFrom(event.Retry),
		TokenWarning: printStreamTokenWarningFrom(event.TokenWarning),
		StreamEvent:  event.StreamEvent,
		Model:        event.Model,
	}
	if event.Compact != nil {
		out.Compact = printStreamCompactMetadataFrom(event)
	}
	if event.ToolProgress != nil {
		out.ToolUseID = event.ToolProgress.ToolUseID
		out.ProgressType = event.ToolProgress.Type
		out.Data = event.ToolProgress.Data
	}
	if event.Error != nil {
		out.Error = event.Error.Error()
		out.ErrorType, out.StatusCode, out.RequestID = printAPIErrorMetadata(event.Error)
	}
	return encoder.Encode(out)
}

// writeHookLifecycleEvent emits CC-compatible system/hook_* events for a hook
// tool-progress event. The mapping from ccgo's ToolProgress.Type to CC subtypes:
//
//	hook_started  → system/hook_started
//	hook_completed → system/hook_response (outcome=success)
//	hook_failed   → system/hook_response (outcome=error)
//	hook_blocked  → system/hook_response (outcome=cancelled)
//
// CC ref: coreSchemas.ts:1604-1646 (SDK-65).
func writeHookLifecycleEvent(encoder *json.Encoder, tp *contracts.ToolProgress, phase string) error {
	hookIndex := 0
	if v, ok := tp.Data["hook_index"]; ok {
		switch n := v.(type) {
		case int:
			hookIndex = n
		case float64:
			hookIndex = int(n)
		}
	}
	hookID := fmt.Sprintf("hook_%s_%d", phase, hookIndex)

	switch tp.Type {
	case "hook_started":
		return encoder.Encode(sdkHookStartedEvent{
			Type:      "system",
			Subtype:   "hook_started",
			HookID:    hookID,
			HookName:  phase,
			HookEvent: phase,
		})
	case "hook_completed":
		output := ""
		if msg, ok := tp.Data["message"].(string); ok {
			output = msg
		}
		return encoder.Encode(sdkHookResponseEvent{
			Type:      "system",
			Subtype:   "hook_response",
			HookID:    hookID,
			HookName:  phase,
			HookEvent: phase,
			Output:    output,
			Stdout:    output,
			Stderr:    "",
			Outcome:   "success",
		})
	case "hook_failed":
		errMsg := ""
		if e, ok := tp.Data["error"].(string); ok {
			errMsg = e
		}
		return encoder.Encode(sdkHookResponseEvent{
			Type:      "system",
			Subtype:   "hook_response",
			HookID:    hookID,
			HookName:  phase,
			HookEvent: phase,
			Output:    errMsg,
			Stdout:    "",
			Stderr:    errMsg,
			Outcome:   "error",
		})
	case "hook_blocked":
		msg := ""
		if m, ok := tp.Data["message"].(string); ok {
			msg = m
		}
		return encoder.Encode(sdkHookResponseEvent{
			Type:      "system",
			Subtype:   "hook_response",
			HookID:    hookID,
			HookName:  phase,
			HookEvent: phase,
			Output:    msg,
			Stdout:    "",
			Stderr:    msg,
			Outcome:   "cancelled",
		})
	default:
		// Unknown hook progress type: fall through to generic tool_progress.
		out := printStreamEvent{
			Type:         event_toolProgress,
			ToolUseID:    tp.ToolUseID,
			ProgressType: tp.Type,
			Data:         tp.Data,
		}
		return encoder.Encode(out)
	}
}

// event_toolProgress is a typed alias to avoid importing conversation in helpers.
const event_toolProgress conversation.EventType = conversation.EventToolProgress

// isHookToolProgress reports whether a ToolProgress event is a hook lifecycle
// event (i.e. it has a non-empty scope field in its data map). Hook events must
// either be emitted as system/hook_* (when scope+phase are valid) or suppressed
// entirely. They must never fall through to the generic tool_progress output.
func isHookToolProgress(tp *contracts.ToolProgress) bool {
	if tp == nil || tp.Data == nil {
		return false
	}
	scope, _ := tp.Data["scope"].(string)
	return scope != ""
}

func printStreamEventVisible(eventType conversation.EventType) bool {
	switch eventType {
	case conversation.EventToolSearchDecision, conversation.EventDeferredPoolChange:
		return false
	default:
		return true
	}
}

func printAPIErrorMetadata(err error) (string, int, string) {
	var apiErr anthropic.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Type, apiErr.StatusCode, apiErr.RequestID
	}
	var apiErrPtr *anthropic.APIError
	if errors.As(err, &apiErrPtr) && apiErrPtr != nil {
		return apiErrPtr.Type, apiErrPtr.StatusCode, apiErrPtr.RequestID
	}
	return "", 0, ""
}

func printStreamRetryFrom(retry *conversation.RetryInfo) *printStreamRetry {
	if retry == nil {
		return nil
	}
	return &printStreamRetry{
		Attempt:     retry.Attempt,
		MaxAttempts: retry.MaxAttempts,
		FailedModel: retry.FailedModel,
		NextModel:   retry.NextModel,
		Fallback:    retry.Fallback,
	}
}

func printStreamCompactMetadataFrom(event conversation.Event) *session.CompactMetadata {
	if event.Compact == nil {
		return nil
	}
	metadata := event.Compact.Plan.Metadata
	if metadata.Trigger == "" && metadata.PreTokens == 0 && metadata.UserContext == "" && metadata.MessagesSummarized == 0 && metadata.PreservedSegment == nil {
		return nil
	}
	return &metadata
}

func printStreamTokenWarningFrom(warning *conversation.TokenWarning) *printStreamTokenWarning {
	if warning == nil {
		return nil
	}
	return &printStreamTokenWarning{
		TokenUsage: warning.TokenUsage,
		Window: printStreamTokenWindow{
			ContextWindow:       warning.Window.ContextWindow,
			MaxOutputTokens:     warning.Window.MaxOutputTokens,
			AutoCompactEnabled:  warning.Window.AutoCompactEnabled,
			AutoCompactOverride: warning.Window.AutoCompactOverride,
			BlockingLimit:       warning.Window.BlockingLimit,
		},
		State: printStreamTokenWarningState{
			PercentLeft:                 warning.State.PercentLeft,
			IsAboveWarningThreshold:     warning.State.IsAboveWarningThreshold,
			IsAboveErrorThreshold:       warning.State.IsAboveErrorThreshold,
			IsAboveAutoCompactThreshold: warning.State.IsAboveAutoCompactThreshold,
			IsAtBlockingLimit:           warning.State.IsAtBlockingLimit,
		},
	}
}

func writePrintResult(stdout io.Writer, runner conversation.Runner, result conversation.Result, outputFormat string, duration time.Duration) error {
	text := resultOutputText(result)
	if text == "" {
		if (outputFormat != "json" && outputFormat != "stream-json") || !result.Cleared {
			return nil
		}
	}
	if outputFormat == "json" || outputFormat == "stream-json" {
		return writePrintJSONResult(stdout, runner, result, text, duration)
	}
	if _, err := fmt.Fprint(stdout, text); err != nil {
		return err
	}
	if !strings.HasSuffix(text, "\n") {
		_, err := fmt.Fprintln(stdout)
		return err
	}
	return nil
}

func resultOutputText(result conversation.Result) string {
	if text := messages.TextContent(result.Assistant); text != "" {
		return text
	}
	for i := len(result.Messages) - 1; i >= 0; i-- {
		message := result.Messages[i]
		text := messages.TextContent(message)
		if text == "" || isCommandMetadataText(text) {
			continue
		}
		if message.Type == contracts.MessageAssistant || result.Assistant.Type == "" {
			return text
		}
	}
	return ""
}

func isCommandMetadataText(text string) bool {
	return strings.Contains(text, "<command-name>") && strings.Contains(text, "</command-name>")
}

func writePrintError(stdout io.Writer, runner conversation.Runner, err error, outputFormat string, duration time.Duration, apiDuration time.Duration, modelsAttempted []string) error {
	if err == nil {
		return nil
	}
	switch outputFormat {
	case "json":
		return writePrintJSONError(stdout, runner, err, duration, apiDuration, modelsAttempted)
	case "stream-json":
		return writePrintStreamError(stdout, runner, err, duration, apiDuration, modelsAttempted)
	default:
		return nil
	}
}

func writePrintJSONError(stdout io.Writer, runner conversation.Runner, err error, duration time.Duration, apiDuration time.Duration, modelsAttempted []string) error {
	encoder := json.NewEncoder(stdout)
	errorType, statusCode, requestID := printAPIErrorMetadata(err)
	envelope := printJSONResult{
		Type:            "result",
		Subtype:         "error",
		IsError:         true,
		DurationMS:      durationMillis(duration),
		DurationAPI:     durationMillis(apiDuration),
		SessionID:       runner.SessionID,
		ModelsAttempted: append([]string(nil), modelsAttempted...),
		Error:           err.Error(),
		ErrorType:       errorType,
		StatusCode:      statusCode,
		RequestID:       requestID,
	}
	applyPrintJSONRuntime(&envelope, runner)
	return encoder.Encode(envelope)
}

func writePrintStreamError(stdout io.Writer, runner conversation.Runner, err error, duration time.Duration, apiDuration time.Duration, modelsAttempted []string) error {
	encoder := json.NewEncoder(stdout)
	durationMS := durationMillis(duration)
	durationAPI := durationMillis(apiDuration)
	errorType, statusCode, requestID := printAPIErrorMetadata(err)
	envelope := printStreamEvent{
		Type:            "error",
		SessionID:       runner.SessionID,
		ModelsAttempted: append([]string(nil), modelsAttempted...),
		Error:           err.Error(),
		ErrorType:       errorType,
		StatusCode:      statusCode,
		RequestID:       requestID,
		IsError:         true,
		DurationMS:      &durationMS,
		DurationAPI:     &durationAPI,
	}
	applyPrintStreamRuntime(&envelope, runner)
	return encoder.Encode(envelope)
}

func writePrintJSONResult(stdout io.Writer, runner conversation.Runner, result conversation.Result, text string, duration time.Duration) error {
	message := result.Assistant
	var messagePtr *contracts.Message
	if message.Type != "" {
		messagePtr = &message
	}
	sessionID := message.SessionID
	if sessionID == "" {
		for _, msg := range result.Messages {
			if msg.SessionID != "" {
				sessionID = msg.SessionID
				break
			}
		}
	}
	usage := message.Usage
	if usage == nil && hasUsage(result.Usage) {
		usage = &result.Usage
	}
	model := message.Model
	if model == "" {
		model = strings.TrimSpace(runner.Model)
	}
	envelope := printJSONResult{
		Type:            "result",
		Subtype:         "success",
		IsError:         false,
		DurationMS:      durationMillis(duration),
		DurationAPI:     durationMillis(result.APIDuration),
		NumTurns:        resultNumTurns(result),
		TotalCost:       usageCostUSD(usage),
		SessionID:       sessionID,
		Result:          text,
		Message:         messagePtr,
		StopReason:      result.StopReason,
		Model:           model,
		ModelsAttempted: append([]string(nil), result.ModelsAttempt...),
		Usage:           usage,
		ToolResults:     result.ToolResults,
		Cleared:         result.Cleared,
		Compacted:       result.Compacted,
	}
	applyPrintJSONRuntime(&envelope, runner)
	if result.Compact != nil {
		metadata := result.Compact.Plan.Metadata
		envelope.Compact = &metadata
	}
	encoder := json.NewEncoder(stdout)
	return encoder.Encode(envelope)
}

func applyPrintJSONRuntime(envelope *printJSONResult, runner conversation.Runner) {
	if envelope == nil || runner.WorkingDirectory == "" {
		return
	}
	envelope.CWD = runner.WorkingDirectory
	envelope.PermissionMode = string(runner.PermissionMode)
	envelope.APIKeySource = runner.APIKeySource
	envelope.Betas = append([]string(nil), runner.BetaHeaders...)
	envelope.FastMode = runner.FastMode
	envelope.OutputStyle = runner.EffectiveOutputStyleName()
	envelope.OutputStyles = runner.AvailableOutputStyleNames()
}

func applyPrintStreamRuntime(envelope *printStreamEvent, runner conversation.Runner) {
	if envelope == nil || runner.WorkingDirectory == "" {
		return
	}
	envelope.CWD = runner.WorkingDirectory
	envelope.PermissionMode = string(runner.PermissionMode)
	envelope.APIKeySource = runner.APIKeySource
	envelope.Betas = append([]string(nil), runner.BetaHeaders...)
	envelope.FastMode = runner.FastMode
	envelope.OutputStyle = runner.EffectiveOutputStyleName()
	envelope.OutputStyles = runner.AvailableOutputStyleNames()
}

func durationMillis(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}

func resultNumTurns(result conversation.Result) int {
	var turns int
	for _, message := range result.Messages {
		if message.Type == contracts.MessageAssistant {
			turns++
		}
	}
	if turns == 0 && result.Assistant.Type == contracts.MessageAssistant {
		return 1
	}
	return turns
}

func usageCostUSD(usage *contracts.Usage) float64 {
	if usage == nil {
		return 0
	}
	return usage.CostUSD
}

func hasUsage(usage contracts.Usage) bool {
	return usage.InputTokens != 0 ||
		usage.OutputTokens != 0 ||
		usage.CacheCreationInputTokens != 0 ||
		usage.CacheReadInputTokens != 0 ||
		usage.CacheDeletedInputTokens != 0 ||
		usage.ServerToolUse.WebSearchRequests != 0 ||
		usage.ServerToolUse.WebFetchRequests != 0 ||
		usage.ServiceTier != "" ||
		usage.CacheCreation.Ephemeral1hInputTokens != 0 ||
		usage.CacheCreation.Ephemeral5mInputTokens != 0 ||
		usage.InferenceGeo != "" ||
		usage.Iterations != 0 ||
		usage.Speed != "" ||
		usage.CostUSD != 0
}

// savePrintCost persists the runner's accumulated session cost to the
// per-project cost file (COST-02). Errors are logged but not fatal.
func savePrintCost(runner conversation.Runner) {
	if runner.WorkingDirectory == "" || runner.SessionID == "" {
		return
	}
	opts := costtrack.DefaultOptions(runner.WorkingDirectory)
	u := runner.AccumulatedUsage
	cost := costtrack.ProjectCost{
		LastSessionID:         runner.SessionID,
		LastCost:              u.CostUSD,
		LastTotalInputTokens:  u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
		LastTotalOutputTokens: u.OutputTokens,
	}
	if err := costtrack.Save(opts, cost); err != nil {
		fmt.Fprintf(os.Stderr, "ccgo: save cost: %v\n", err)
	}
}

// extractTextPromptFromMessage extracts the first text content from a message
// for use as the sdk.Query prompt. Returns the content string, or an empty
// string if the message has no text content.
func extractTextPromptFromMessage(msg contracts.Message) string {
	for _, block := range msg.Content {
		if block.Type == "text" && block.Text != "" {
			return block.Text
		}
	}
	return ""
}

// shouldReconnect wraps reconnect.ShouldReconnect (MCP-43): returns true for
// remote transports (HTTP/SSE/WS) that should use exponential-backoff reconnect.
func shouldReconnect(transport string) bool {
	return reconnect.ShouldReconnect(transport)
}

// checkAvailableModels returns an error when availableModels is non-empty and the
// requested model is not in the whitelist. Called in headlessRunner (CFG-07).
// CC ref: utils/settings/types.ts availableModels.
func checkAvailableModels(modelName string, availableModels []string) error {
	if len(availableModels) == 0 {
		return nil
	}
	for _, allowed := range availableModels {
		if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(modelName)) {
			return nil
		}
	}
	return fmt.Errorf("model %q is not in the enterprise availableModels list: %v", modelName, availableModels)
}

// applyModelOverrides remaps a model name using the modelOverrides map (CFG-08).
// Returns the original name when no override is configured.
// CC ref: utils/settings/types.ts modelOverrides (Bedrock ARN remapping).
func applyModelOverrides(modelName string, overrides map[string]string) string {
	if len(overrides) == 0 {
		return modelName
	}
	for src, dst := range overrides {
		if strings.EqualFold(strings.TrimSpace(src), strings.TrimSpace(modelName)) {
			return strings.TrimSpace(dst)
		}
	}
	return modelName
}

// checkMinimumVersion returns an error when the running version is below
// the minimum required version from managed settings (CFG-46).
// Uses a simple lexicographic semver comparison (sufficient for X.Y.Z strings).
// CC ref: utils/settings/types.ts minimumVersion.
func checkMinimumVersion(minimumVersion string) error {
	minimumVersion = strings.TrimSpace(minimumVersion)
	if minimumVersion == "" {
		return nil
	}
	current := strings.TrimPrefix(strings.TrimSpace(version), "v")
	minimum := strings.TrimPrefix(minimumVersion, "v")
	if compareSemver(current, minimum) < 0 {
		return fmt.Errorf("this version (%s) is below the required minimum version (%s); please upgrade claude", current, minimum)
	}
	return nil
}

// compareSemver returns -1, 0, or 1 for a < b, a == b, a > b.
// Handles X.Y.Z[-suffix] semver strings. Non-parseable parts fall back to
// lexicographic comparison. Sufficient for version gate enforcement.
func compareSemver(a, b string) int {
	pa := parseSemverParts(a)
	pb := parseSemverParts(b)
	for i := 0; i < 3; i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va < vb {
			return -1
		}
		if va > vb {
			return 1
		}
	}
	return 0
}

func parseSemverParts(v string) []int {
	// Strip pre-release suffix (e.g. "-dev", "-alpha.1")
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.SplitN(v, ".", 3)
	out := make([]int, 0, 3)
	for _, p := range parts {
		n := 0
		for _, c := range p {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			} else {
				break
			}
		}
		out = append(out, n)
	}
	return out
}
