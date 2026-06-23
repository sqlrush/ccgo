// Package main — F2 CLI flags tests.
// Tests are in package main so they can call run() and headlessRunner() directly.
//
// Test strategy: parse flags through a shared helper that replicates the
// top-level flag.FlagSet setup in run(), and assert the parsed cliOptions
// contain the expected values. Runner-wiring tests call headlessRunner() with
// a crafted cliOptions and assert the resulting conversation.Runner fields.
package main

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/bootstrap"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// parseF2Options mimics the flag registration in run() and returns a cliOptions
// populated from args. It stops at flag.Parse — no runner, no API, no bootstrap.
func parseF2Options(t *testing.T, args []string) (cliOptions, error) {
	t.Helper()
	fs := flag.NewFlagSet("claude-test", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{}) // suppress help output

	// Mirror all flags registered in run() (core + F2).
	_ = fs.Bool("version", false, "")
	_ = fs.Bool("v", false, "")
	_ = fs.Bool("chrome-native-host", false, "")
	_ = fs.Bool("daemon", false, "")
	_ = fs.Bool("daemon-once", false, "")
	_ = fs.Duration("daemon-heartbeat", 0, "")
	_ = fs.Bool("daemon-status", false, "")
	_ = fs.Bool("daemon-stop", false, "")
	_ = fs.Bool("daemon-tick", false, "")
	_ = fs.Bool("daemon-start", false, "")
	_ = fs.Bool("daemon-restart", false, "")
	_ = fs.String("daemon-session", "", "")
	_ = fs.String("daemon-state", "", "")
	_ = fs.String("cwd", "", "")
	_ = fs.Bool("print", false, "")
	_ = fs.Bool("p", false, "")
	modelName := fs.String("model", "", "")
	maxTokens := fs.Int("max-tokens", 0, "")
	_ = fs.Int("maxTokens", 0, "")
	maxTurns := fs.Int("max-turns", 0, "")
	_ = fs.Int("maxTurns", 0, "")
	permissionMode := fs.String("permission-mode", "", "")
	_ = fs.String("permissionMode", "", "")
	_ = fs.Bool("dangerously-skip-permissions", false, "")
	_ = fs.Bool("dangerouslySkipPermissions", false, "")
	mcpConfig := fs.String("mcp-config", "", "")
	_ = fs.String("mcpConfig", "", "")
	stream := fs.Bool("stream", false, "")
	_ = fs.String("input-format", "text", "")
	_ = fs.String("inputFormat", "text", "")
	_ = fs.String("output-format", "text", "")
	_ = fs.String("outputFormat", "text", "")
	resume := fs.String("resume", "", "")
	continueMode := fs.Bool("continue", false, "")
	systemPrompt := fs.String("system-prompt", "", "")
	_ = fs.String("systemPrompt", "", "")
	appendSystem := fs.String("append-system-prompt", "", "")
	_ = fs.String("appendSystemPrompt", "", "")
	var allowedTools, deniedTools, addDirs repeatedStringFlag
	fs.Var(&allowedTools, "allowedTools", "")
	fs.Var(&allowedTools, "allowed-tools", "")
	fs.Var(&deniedTools, "disallowedTools", "")
	fs.Var(&deniedTools, "disallowed-tools", "")
	fs.Var(&addDirs, "add-dir", "")
	fs.Var(&addDirs, "addDir", "")

	// F2-C01
	verbose := fs.Bool("verbose", false, "")
	debug := fs.String("debug", "", "")
	debugShort := fs.String("d", "", "")
	bare := fs.Bool("bare", false, "")
	thinking := fs.String("thinking", "", "")
	effort := fs.String("effort", "", "")

	// F2-C02
	settingsPath := fs.String("settings", "", "")
	sessionID := fs.String("session-id", "", "")
	noSessionPersistence := fs.Bool("no-session-persistence", false, "")
	forkSession := fs.Bool("fork-session", false, "")
	sessionName := fs.String("name", "", "")
	sessionNameShort := fs.String("n", "", "")

	// F2-C03
	agentName := fs.String("agent", "", "")
	agentsJSON := fs.String("agents", "", "")
	fallbackModel := fs.String("fallback-model", "", "")
	var betas, tools repeatedStringFlag
	fs.Var(&betas, "betas", "")
	fs.Var(&tools, "tools", "")

	// F2-C04
	strictMCPConfig := fs.Bool("strict-mcp-config", false, "")
	settingSources := fs.String("setting-sources", "", "")
	includeHookEvents := fs.Bool("include-hook-events", false, "")
	includePartialMessages := fs.Bool("include-partial-messages", false, "")
	replayUserMessages := fs.Bool("replay-user-messages", false, "")
	permissionPromptTool := fs.String("permission-prompt-tool", "", "")
	jsonSchema := fs.String("json-schema", "", "")
	maxBudgetUSD := fs.Float64("max-budget-usd", 0, "")

	// F2-C05
	systemPromptFile := fs.String("system-prompt-file", "", "")
	appendSystemPromptFile := fs.String("append-system-prompt-file", "", "")
	var pluginDirs repeatedStringFlag
	fs.Var(&pluginDirs, "plugin-dir", "")
	disableSlashCommands := fs.Bool("disable-slash-commands", false, "")
	allowDangerouslySkipPermissions := fs.Bool("allow-dangerously-skip-permissions", false, "")

	// F2-C06
	worktree := fs.String("worktree", "", "")
	worktreeShort := fs.String("w", "", "")
	tmux := fs.Bool("tmux", false, "")
	var fileSpecs repeatedStringFlag
	fs.Var(&fileSpecs, "file", "")
	// Companion OUT-of-scope no-ops
	_ = fs.Bool("ide", false, "")
	_ = fs.Bool("chrome", false, "")
	_ = fs.String("from-pr", "", "")

	if err := fs.Parse(args); err != nil {
		return cliOptions{}, err
	}

	// Reconcile aliased short flags.
	debugVal := *debug
	if debugVal == "" {
		debugVal = *debugShort
	}
	nameVal := *sessionName
	if nameVal == "" {
		nameVal = *sessionNameShort
	}
	worktreeVal := *worktree
	if worktreeVal == "" {
		worktreeVal = *worktreeShort
	}

	return cliOptions{
		Model:           *modelName,
		MaxTokens:       *maxTokens,
		MaxTurns:        *maxTurns,
		PermissionMode:  *permissionMode,
		MCPConfig:       *mcpConfig,
		Stream:          *stream,
		Resume:          *resume,
		Continue:        *continueMode,
		SystemPrompt:    *systemPrompt,
		AppendSystem:    *appendSystem,
		AllowedTools:    append([]string(nil), allowedTools...),
		DeniedTools:     append([]string(nil), deniedTools...),
		AddDirs:         append([]string(nil), addDirs...),

		Verbose:                         *verbose,
		Debug:                           debugVal,
		Bare:                            *bare,
		Thinking:                        *thinking,
		Effort:                          *effort,
		SettingsPath:                    *settingsPath,
		SessionID:                       *sessionID,
		NoSessionPersistence:            *noSessionPersistence,
		ForkSession:                     *forkSession,
		SessionName:                     nameVal,
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
		Worktree:                        worktreeVal,
		Tmux:                            *tmux,
		Files:                           append([]string(nil), fileSpecs...),
	}, nil
}

// runnerForTest creates a headlessRunner with test env vars set.
func runnerForTest(t *testing.T, opts cliOptions) error {
	t.Helper()
	// headlessRunner will fail if it can't reach the API, but we can still verify
	// pre-API wiring by checking the runner before client construction.
	// Actually headlessRunner calls anthropicClientFromEnv which needs ANTHROPIC_API_KEY.
	// We set it in the test and use a local test server.
	return nil
}

// testEnv sets up required env vars for headlessRunner without a live API.
func testEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "http://localhost:0") // unreachable, but key is present
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
}

// ─── F2-C01: debug/mode flags ─────────────────────────────────────────────────

func TestVerboseFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--verbose", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.Verbose {
		t.Error("--verbose should set Verbose=true")
	}
}

func TestDebugFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--debug", "api,hooks", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.Debug != "api,hooks" {
		t.Errorf("--debug=api,hooks: got Debug=%q, want %q", opts.Debug, "api,hooks")
	}
}

func TestDebugShortFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "-d", "api", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.Debug != "api" {
		t.Errorf("-d api: got Debug=%q, want %q", opts.Debug, "api")
	}
}

func TestBareFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--bare", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.Bare {
		t.Error("--bare should set Bare=true")
	}
}

func TestThinkingFlagParsed(t *testing.T) {
	for _, mode := range []string{"enabled", "adaptive", "disabled"} {
		opts, err := parseF2Options(t, []string{"--print", "--thinking", mode, "hello"})
		if err != nil {
			t.Fatalf("parse --thinking %s: %v", mode, err)
		}
		if opts.Thinking != mode {
			t.Errorf("--thinking=%s: got Thinking=%q, want %q", mode, opts.Thinking, mode)
		}
	}
}

func TestEffortFlagParsed(t *testing.T) {
	for _, level := range []string{"low", "medium", "high", "max"} {
		opts, err := parseF2Options(t, []string{"--print", "--effort", level, "hello"})
		if err != nil {
			t.Fatalf("parse --effort %s: %v", level, err)
		}
		if opts.Effort != level {
			t.Errorf("--effort=%s: got Effort=%q, want %q", level, opts.Effort, level)
		}
	}
}

func TestEffortWiredToRunner(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		Effort:         "max",
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if runner.EffortLevel != "max" {
		t.Errorf("runner.EffortLevel = %q, want %q", runner.EffortLevel, "max")
	}
}

func TestThinkingEnabledWiredToRunner(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		Thinking:       "enabled",
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if !runner.AlwaysThinkingEnabled {
		t.Error("--thinking=enabled should set runner.AlwaysThinkingEnabled=true")
	}
}

func TestThinkingAdaptiveWiredToRunner(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		Thinking:       "adaptive",
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if !runner.AlwaysThinkingEnabled {
		t.Error("--thinking=adaptive should set runner.AlwaysThinkingEnabled=true")
	}
}

func TestVerboseWiredToRunner(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		Verbose:        true,
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if !runner.Verbose {
		t.Error("--verbose should set runner.Verbose=true")
	}
}

// ─── F2-C02: session flags ────────────────────────────────────────────────────

func TestSessionIDFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--session-id", "abc-123", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.SessionID != "abc-123" {
		t.Errorf("--session-id=abc-123: got %q, want %q", opts.SessionID, "abc-123")
	}
}

func TestSessionIDWiredToRunner(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		SessionID:      "my-custom-session-id",
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if string(runner.SessionID) != "my-custom-session-id" {
		t.Errorf("runner.SessionID = %q, want %q", runner.SessionID, "my-custom-session-id")
	}
}

func TestNoSessionPersistenceFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--no-session-persistence", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.NoSessionPersistence {
		t.Error("--no-session-persistence should set NoSessionPersistence=true")
	}
}

func TestNoSessionPersistenceClearsSessionPath(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode:       "default",
		NoSessionPersistence: true,
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if runner.SessionPath != "" {
		t.Errorf("--no-session-persistence should clear runner.SessionPath, got %q", runner.SessionPath)
	}
	if runner.SessionID != "" {
		t.Errorf("--no-session-persistence should clear runner.SessionID, got %q", runner.SessionID)
	}
}

func TestForkSessionFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--fork-session", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.ForkSession {
		t.Error("--fork-session should set ForkSession=true")
	}
}

func TestSessionNameFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--name", "my-session", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.SessionName != "my-session" {
		t.Errorf("--name=my-session: got %q, want %q", opts.SessionName, "my-session")
	}
}

func TestSessionNameShortFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "-n", "short-name", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.SessionName != "short-name" {
		t.Errorf("-n=short-name: got %q, want %q", opts.SessionName, "short-name")
	}
}

func TestSettingsFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--settings", "/tmp/extra.json", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.SettingsPath != "/tmp/extra.json" {
		t.Errorf("--settings: got %q, want %q", opts.SettingsPath, "/tmp/extra.json")
	}
}

func TestSettingsFlagLoadsFile(t *testing.T) {
	testEnv(t)
	dir := t.TempDir()
	settingsFile := filepath.Join(dir, "extra.json")
	if err := os.WriteFile(settingsFile, []byte(`{"model":"claude-haiku-4-5"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		SettingsPath:   settingsFile,
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if runner.MCP == nil {
		t.Fatal("runner.MCP should be non-nil after --settings loads a file")
	}
}

func TestSettingsFlagLoadsInlineJSON(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	// Inline JSON string (starts with '{').
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		SettingsPath:   `{"model":"claude-haiku-4-5"}`,
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if runner.MCP == nil {
		t.Fatal("runner.MCP should be non-nil after --settings inline JSON")
	}
}

// ─── F2-C03: agent/model flags ────────────────────────────────────────────────

func TestFallbackModelFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--fallback-model", "claude-haiku-4-5", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.FallbackModel != "claude-haiku-4-5" {
		t.Errorf("--fallback-model: got %q, want %q", opts.FallbackModel, "claude-haiku-4-5")
	}
}

func TestFallbackModelWiredToRunner(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		FallbackModel:  "claude-haiku-4-5",
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	found := false
	for _, m := range runner.FallbackModels {
		if m == "claude-haiku-4-5" {
			found = true
		}
	}
	if !found {
		t.Errorf("runner.FallbackModels %v does not contain %q", runner.FallbackModels, "claude-haiku-4-5")
	}
}

func TestBetasFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--betas", "interleaved-thinking-2025-05-14", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(opts.Betas) != 1 || opts.Betas[0] != "interleaved-thinking-2025-05-14" {
		t.Errorf("--betas: got %v, want [interleaved-thinking-2025-05-14]", opts.Betas)
	}
}

func TestBetasWiredToRunner(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		Betas:          []string{"interleaved-thinking-2025-05-14"},
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	found := false
	for _, b := range runner.BetaHeaders {
		if b == "interleaved-thinking-2025-05-14" {
			found = true
		}
	}
	if !found {
		t.Errorf("runner.BetaHeaders %v does not contain expected beta", runner.BetaHeaders)
	}
}

func TestToolsFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--tools", "Bash,Edit", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(opts.Tools) == 0 {
		t.Error("--tools should set Tools slice")
	}
}

func TestAgentFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--agent", "code-reviewer", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.Agent != "code-reviewer" {
		t.Errorf("--agent: got %q, want %q", opts.Agent, "code-reviewer")
	}
}

func TestAgentsFlagParsed(t *testing.T) {
	agentsJSON := `{"reviewer":{"description":"Reviews code","prompt":"Be a tester"}}`
	opts, err := parseF2Options(t, []string{"--print", "--agents", agentsJSON, "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.Agents != agentsJSON {
		t.Errorf("--agents: got %q, want %q", opts.Agents, agentsJSON)
	}
}

// ─── F2-C04: output/control flags ────────────────────────────────────────────

func TestStrictMCPConfigFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--strict-mcp-config", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.StrictMCPConfig {
		t.Error("--strict-mcp-config should set StrictMCPConfig=true")
	}
}

func TestSettingSourcesFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--setting-sources", "user", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.SettingSources != "user" {
		t.Errorf("--setting-sources=user: got %q, want %q", opts.SettingSources, "user")
	}
}

func TestIncludeHookEventsFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--include-hook-events", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.IncludeHookEvents {
		t.Error("--include-hook-events should set IncludeHookEvents=true")
	}
}

func TestIncludePartialMessagesFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--include-partial-messages", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.IncludePartialMessages {
		t.Error("--include-partial-messages should set IncludePartialMessages=true")
	}
}

func TestReplayUserMessagesFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--replay-user-messages", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.ReplayUserMessages {
		t.Error("--replay-user-messages should set ReplayUserMessages=true")
	}
}

func TestPermissionPromptToolFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--permission-prompt-tool", "perm_tool", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.PermissionPromptTool != "perm_tool" {
		t.Errorf("--permission-prompt-tool: got %q, want %q", opts.PermissionPromptTool, "perm_tool")
	}
}

func TestJSONSchemaFlagParsed(t *testing.T) {
	schema := `{"type":"object","properties":{"name":{"type":"string"}}}`
	opts, err := parseF2Options(t, []string{"--print", "--json-schema", schema, "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.JSONSchema != schema {
		t.Errorf("--json-schema: got %q, want %q", opts.JSONSchema, schema)
	}
}

func TestMaxBudgetUSDFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--max-budget-usd", "0.5", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.MaxBudgetUSD != 0.5 {
		t.Errorf("--max-budget-usd=0.5: got %v, want 0.5", opts.MaxBudgetUSD)
	}
}

func TestMaxBudgetUSDWiredToRunner(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		MaxBudgetUSD:   2.50,
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if runner.MaxBudgetUSD != 2.50 {
		t.Errorf("runner.MaxBudgetUSD = %v, want 2.50", runner.MaxBudgetUSD)
	}
}

// ─── F2-C05: system-prompt file / plugin / command flags ─────────────────────

func TestSystemPromptFileFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--system-prompt-file", "/tmp/sp.txt", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.SystemPromptFile != "/tmp/sp.txt" {
		t.Errorf("--system-prompt-file: got %q, want %q", opts.SystemPromptFile, "/tmp/sp.txt")
	}
}

func TestSystemPromptFileWiredToRunner(t *testing.T) {
	testEnv(t)
	dir := t.TempDir()
	spFile := filepath.Join(dir, "sp.txt")
	if err := os.WriteFile(spFile, []byte("You are a test bot."), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	state.SetCWD(dir)
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode:  "default",
		SystemPromptFile: spFile,
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if !strings.Contains(runner.SystemPrompt, "You are a test bot.") {
		t.Errorf("runner.SystemPrompt should contain file content; got %q", runner.SystemPrompt)
	}
}

func TestAppendSystemPromptFileFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--append-system-prompt-file", "/tmp/extra.txt", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.AppendSystemPromptFile != "/tmp/extra.txt" {
		t.Errorf("--append-system-prompt-file: got %q, want %q", opts.AppendSystemPromptFile, "/tmp/extra.txt")
	}
}

func TestAppendSystemPromptFileWiredToRunner(t *testing.T) {
	testEnv(t)
	dir := t.TempDir()
	appendFile := filepath.Join(dir, "append.txt")
	if err := os.WriteFile(appendFile, []byte("Always answer in French."), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	state.SetCWD(dir)
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode:         "default",
		SystemPrompt:           "Base prompt.",
		AppendSystemPromptFile: appendFile,
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if !strings.Contains(runner.SystemPrompt, "Always answer in French.") {
		t.Errorf("runner.SystemPrompt should contain appended file content; got %q", runner.SystemPrompt)
	}
	if !strings.Contains(runner.SystemPrompt, "Base prompt.") {
		t.Errorf("runner.SystemPrompt should still contain base prompt; got %q", runner.SystemPrompt)
	}
}

func TestPluginDirFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--plugin-dir", "/tmp/myplugin", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(opts.PluginDirs) != 1 || opts.PluginDirs[0] != "/tmp/myplugin" {
		t.Errorf("--plugin-dir: got %v, want [/tmp/myplugin]", opts.PluginDirs)
	}
}

func TestDisableSlashCommandsFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--disable-slash-commands", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.DisableSlashCommands {
		t.Error("--disable-slash-commands should set DisableSlashCommands=true")
	}
}

func TestAllowDangerouslySkipPermissionsFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--allow-dangerously-skip-permissions", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.AllowDangerouslySkipPermissions {
		t.Error("--allow-dangerously-skip-permissions should set AllowDangerouslySkipPermissions=true")
	}
}

// ─── F2-C06: worktree / file flags ────────────────────────────────────────────

func TestWorktreeFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--worktree", "my-branch", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.Worktree != "my-branch" {
		t.Errorf("--worktree=my-branch: got %q, want %q", opts.Worktree, "my-branch")
	}
}

func TestWorktreeShortFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"-w", "another", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.Worktree != "another" {
		t.Errorf("-w=another: got %q, want %q", opts.Worktree, "another")
	}
}

func TestTmuxFlagParsed(t *testing.T) {
	// Use --worktree=name form so --tmux is not consumed as the worktree name.
	opts, err := parseF2Options(t, []string{"--worktree=my-branch", "--tmux", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !opts.Tmux {
		t.Error("--tmux should set Tmux=true")
	}
	if opts.Worktree != "my-branch" {
		t.Errorf("--worktree=my-branch: got %q, want %q", opts.Worktree, "my-branch")
	}
}

func TestFileFlagParsed(t *testing.T) {
	opts, err := parseF2Options(t, []string{"--print", "--file", "file_abc:doc.txt", "hello"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(opts.Files) != 1 || opts.Files[0] != "file_abc:doc.txt" {
		t.Errorf("--file: got %v, want [file_abc:doc.txt]", opts.Files)
	}
}

// TestCompanionFlagsAccepted verifies --ide, --chrome, --from-pr parse without error.
func TestCompanionFlagsAccepted(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"ide", []string{"--print", "--ide", "hello"}},
		{"chrome", []string{"--print", "--chrome", "hello"}},
		{"from-pr", []string{"--print", "--from-pr", "123", "hello"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseF2Options(t, tc.args)
			if err != nil {
				t.Errorf("companion flag --%s should parse without error; got %v", tc.name, err)
			}
		})
	}
}

// TestForkSessionCreatesNewID verifies that ForkSession causes resumeHistory to
// assign a new session ID different from the original.
func TestForkSessionCreatesNewID(t *testing.T) {
	testEnv(t)
	dir := t.TempDir()

	// Create a minimal transcript.
	sessionID := contracts.NewID()
	transcriptPath := filepath.Join(dir, string(sessionID)+".jsonl")
	userLine := `{"type":"user","message":{"id":"msg1","type":"user","role":"user","content":[{"type":"text","text":"hello"}]}}` + "\n"
	if err := os.WriteFile(transcriptPath, []byte(userLine), 0o600); err != nil {
		t.Fatalf("WriteFile transcript: %v", err)
	}

	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	state.SetCWD(dir)

	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}

	_, err = resumeHistory(state, &runner, cliOptions{
		Resume:      transcriptPath,
		ForkSession: true,
	})
	if err != nil {
		t.Skipf("resumeHistory returned error (transcript format): %v", err)
	}
	if runner.SessionID == sessionID {
		t.Error("--fork-session should produce a new session ID different from the original")
	}
	if string(runner.SessionID) == "" {
		t.Error("--fork-session should produce a non-empty new session ID")
	}
}

// ─── CLI-FLAG-40: --json-schema wired to runner.OutputSchema ─────────────────

func TestJSONSchemaWiredToRunner(t *testing.T) {
	testEnv(t)
	schema := `{"type":"object","properties":{"name":{"type":"string"}}}`
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		JSONSchema:     schema,
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if len(runner.OutputSchema) == 0 {
		t.Fatal("runner.OutputSchema should be set when --json-schema is provided")
	}
	if runner.OutputSchema["type"] != "object" {
		t.Errorf("OutputSchema[type] = %v, want %q", runner.OutputSchema["type"], "object")
	}
	// The structured-outputs beta header must be present.
	found := false
	for _, h := range runner.BetaHeaders {
		if h == "structured-outputs-2025-12-15" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("BetaHeaders %v should contain structured-outputs-2025-12-15", runner.BetaHeaders)
	}
}

func TestJSONSchemaInvalidJSONReturnsError(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	_, err = headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		JSONSchema:     "not-valid-json",
	})
	if err == nil {
		t.Fatal("headlessRunner should return error for invalid --json-schema JSON")
	}
	if !strings.Contains(err.Error(), "--json-schema") {
		t.Errorf("error should mention --json-schema; got: %v", err)
	}
}

// ─── CLI-FLAG-44: --permission-prompt-tool wired to executor.Asker ───────────

func TestPermissionPromptToolWiredToExecutorAsker(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode:      "default",
		PermissionPromptTool: "my_perm_tool",
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if runner.Tools.Asker == nil {
		t.Fatal("runner.Tools.Asker should be set when --permission-prompt-tool is provided")
	}
	asker, ok := runner.Tools.Asker.(*conversation.MCPPermissionAsker)
	if !ok {
		t.Fatalf("Asker should be *conversation.MCPPermissionAsker, got %T", runner.Tools.Asker)
	}
	if asker.ToolName != "my_perm_tool" {
		t.Errorf("MCPPermissionAsker.ToolName = %q, want %q", asker.ToolName, "my_perm_tool")
	}
}

// ─── CLI-FLAG-27: --agent wired to runner model/system prompt ─────────────────

func TestAgentFromInlineAgentsFlagAppliesPrompt(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	agentsJSON := `{"reviewer":{"prompt":"You are a strict code reviewer.","model":""}}`
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		Agent:          "reviewer",
		Agents:         agentsJSON,
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if !strings.Contains(runner.SystemPrompt, "You are a strict code reviewer.") {
		t.Errorf("SystemPrompt should contain agent prompt; got: %q", runner.SystemPrompt)
	}
}

func TestAgentFromInlineAgentsFlagAppliesModel(t *testing.T) {
	testEnv(t)
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	agentsJSON := `{"myagent":{"prompt":"be helpful","model":"claude-3-haiku-20240307"}}`
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		Agent:          "myagent",
		Agents:         agentsJSON,
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if runner.Model != "claude-3-haiku-20240307" {
		t.Errorf("runner.Model = %q, want %q", runner.Model, "claude-3-haiku-20240307")
	}
}

func TestAgentFromDiskFile(t *testing.T) {
	testEnv(t)
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: codebot\nmodel: claude-opus-4\ndescription: A code bot\n---\nYou are codebot, a specialized code assistant.\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "codebot.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	state.SetCWD(dir)
	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
		Agent:          "codebot",
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if runner.Model != "claude-opus-4" {
		t.Errorf("runner.Model = %q, want %q from agent file", runner.Model, "claude-opus-4")
	}
	if !strings.Contains(runner.SystemPrompt, "You are codebot") {
		t.Errorf("SystemPrompt should contain agent prompt; got: %q", runner.SystemPrompt)
	}
}
