package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/auth"
	"ccgo/internal/bootstrap"
	"ccgo/internal/commands"
	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	integrationspkg "ccgo/internal/integrations"
	"ccgo/internal/mcp"
	"ccgo/internal/messages"
	"ccgo/internal/model"
	"ccgo/internal/permissions"
	pluginpkg "ccgo/internal/plugins"
	"ccgo/internal/session"
	"ccgo/internal/tool"
	filetools "ccgo/internal/tools/file"
)

const version = "0.0.0-dev"

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
	flags := flag.NewFlagSet("claude", flag.ContinueOnError)
	flags.SetOutput(stderr)

	showVersion := flags.Bool("version", false, "print version")
	flags.BoolVar(showVersion, "v", false, "print version")
	chromeNativeHost := flags.Bool("chrome-native-host", false, "run Chrome native messaging host")
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
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

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
	if *printMode {
		normalizedOutputFormat, err := normalizeOutputFormat(*outputFormat)
		if err != nil {
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		startedAt := time.Now()
		format, err := normalizeInputFormat(*inputFormat)
		if err != nil {
			_ = writePrintError(stdout, conversation.Runner{}, err, normalizedOutputFormat, time.Since(startedAt), 0)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		userMessage, err := promptMessageFromArgsOrStdin(flags.Args(), stdin, format)
		if err != nil {
			_ = writePrintError(stdout, conversation.Runner{}, err, normalizedOutputFormat, time.Since(startedAt), 0)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		effectiveMode, err := effectivePermissionMode(*permissionMode, *skipPermissions)
		if err != nil {
			_ = writePrintError(stdout, conversation.Runner{}, err, normalizedOutputFormat, time.Since(startedAt), 0)
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
		})
		if err != nil {
			_ = writePrintError(stdout, runner, err, normalizedOutputFormat, time.Since(startedAt), 0)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		history, err := resumeHistory(state, &runner, cliOptions{Resume: *resume, Continue: *continueMode})
		if err != nil {
			_ = writePrintError(stdout, runner, err, normalizedOutputFormat, time.Since(startedAt), 0)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		streamErr := func() error { return nil }
		if normalizedOutputFormat == "stream-json" {
			runner, streamErr = attachStreamJSON(stdout, runner)
		}
		result, err := runner.RunTurn(context.Background(), history, userMessage)
		if err != nil {
			_ = writePrintError(stdout, runner, err, normalizedOutputFormat, time.Since(startedAt), result.APIDuration)
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
		return 0
	}
	if _, err := state.ConversationRunner(); err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "ccgo scaffold ready\nsession_id=%s\ncwd=%s\n", state.SessionID(), state.CWD())
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

func handleChromeNativeHostMessage(raw json.RawMessage) map[string]any {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return map[string]any{"type": "error", "ok": false, "error": "invalid JSON message"}
	}
	messageType, _ := message["type"].(string)
	switch strings.ToLower(strings.TrimSpace(messageType)) {
	case "ping":
		return map[string]any{"type": "pong", "ok": true}
	case "status":
		return map[string]any{"type": "status", "ok": true, "runtime": "ccgo", "version": version}
	default:
		if messageType == "" {
			messageType = "(missing)"
		}
		return map[string]any{"type": "error", "ok": false, "error": "unsupported message type: " + messageType}
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

func headlessRunner(ctx context.Context, state *bootstrap.State, options cliOptions) (conversation.Runner, error) {
	runner, err := state.ConversationRunner()
	if err != nil {
		return conversation.Runner{}, err
	}
	if err := applyMCPConfigFlag(&runner, options.MCPConfig); err != nil {
		return conversation.Runner{}, err
	}
	registry, err := tool.NewRegistry(filetools.BuiltinTools()...)
	if err != nil {
		return conversation.Runner{}, err
	}
	runner.Tools = tool.NewExecutor(registry)
	runner.Permissions, err = permissionDeciderFromSettings(
		runner.MCP,
		strings.TrimSpace(options.PermissionMode),
		parseToolRules(options.AllowedTools...),
		parseToolRules(options.DeniedTools...),
		parsePathList(options.AddDirs),
	)
	if err != nil {
		return conversation.Runner{}, err
	}
	runner.PermissionMode = runnerPermissionModeFromDecider(runner.Permissions)
	runner.Model = resolveCLIModel(options.Model, runner.MCP)
	if runner.MCP != nil {
		merged := config.MergeSettings(runner.MCP.UserSettings, runner.MCP.ProjectSettings, runner.MCP.LocalSettings)
		runner.FastMode = merged.FastMode != nil && *merged.FastMode
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
	runner.SystemPrompt = combineSystemPrompt(options.SystemPrompt, options.AppendSystem)
	if runner.SessionPath == "" && runner.SessionID != "" {
		runner.SessionPath = session.TranscriptPath(runner.WorkingDirectory, runner.SessionID)
	}

	client, apiKeySource, err := anthropicClientFromEnv(ctx, runner.FastMode)
	if err != nil {
		return runner, err
	}
	runner.Client = client
	runner.APIKeySource = apiKeySource
	runner.BetaHeaders = append([]string(nil), client.Beta...)
	return runner, nil
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
	runner.SessionID = sessionID
	runner.SessionPath = transcriptPath
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
	return id, session.TranscriptPath(cwd, id), nil
}

func parseToolRules(raw ...string) []string {
	return commands.ParseToolList(raw)
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
		merged := config.MergeSettings(mcpConfig.UserSettings, mcpConfig.ProjectSettings, mcpConfig.LocalSettings)
		managedRulesOnly = merged.AllowManagedPermissionRulesOnly != nil && *merged.AllowManagedPermissionRulesOnly
		sources = append(sources,
			permissions.SettingsSource{Source: contracts.PermissionSourceUserSettings, Permissions: mcpConfig.UserSettings.Permissions, Sandbox: mcpConfig.UserSettings.Sandbox},
			permissions.SettingsSource{Source: contracts.PermissionSourceProjectSettings, Permissions: mcpConfig.ProjectSettings.Permissions, Sandbox: mcpConfig.ProjectSettings.Sandbox},
			permissions.SettingsSource{Source: contracts.PermissionSourceLocalSettings, Permissions: mcpConfig.LocalSettings.Permissions, Sandbox: mcpConfig.LocalSettings.Sandbox},
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
		raw = config.MergeSettings(mcpConfig.UserSettings, mcpConfig.ProjectSettings, mcpConfig.LocalSettings).Model
	}
	if capability, ok := model.DefaultRegistry().Resolve(raw); ok {
		return capability.Name
	}
	return strings.TrimSpace(raw)
}

func anthropicClientFromEnv(ctx context.Context, fastMode bool) (*anthropic.Client, string, error) {
	credentials, credentialStore, err := credentialsFromEnvOrStore(ctx)
	if err != nil {
		return nil, "", err
	}
	if credentials.Source == auth.SourceNone {
		return nil, "", fmt.Errorf("missing Anthropic credentials; set ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_REFRESH_TOKEN")
	}
	if credentials.Source == auth.SourceOAuth && strings.TrimSpace(credentials.AccessToken) == "" && strings.TrimSpace(credentials.RefreshToken) != "" {
		provider := auth.NewOAuthTokenProvider(auth.OAuthTokenProviderOptions{
			Credentials:     credentials,
			CredentialStore: credentialStore,
		})
		token, err := provider.CurrentAccessToken(ctx)
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
	return anthropic.NewClient(options...), string(credentials.Source), nil
}

func credentialsFromEnvOrStore(ctx context.Context) (auth.Credentials, auth.CredentialStore, error) {
	credentials := auth.FromEnv()
	if credentials.Source != auth.SourceNone {
		return credentials, nil, nil
	}
	store := auth.NewFileCredentialStore("")
	stored, err := store.Load(ctx)
	if err != nil {
		return auth.Credentials{}, nil, err
	}
	if stored.Source == auth.SourceNone {
		return stored, nil, nil
	}
	return stored, store, nil
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type printJSONResult struct {
	Type           string                   `json:"type"`
	Subtype        string                   `json:"subtype"`
	IsError        bool                     `json:"is_error"`
	DurationMS     int64                    `json:"duration_ms"`
	DurationAPI    int64                    `json:"duration_api_ms"`
	NumTurns       int                      `json:"num_turns,omitempty"`
	TotalCost      float64                  `json:"total_cost_usd,omitempty"`
	SessionID      contracts.ID             `json:"session_id,omitempty"`
	CWD            string                   `json:"cwd,omitempty"`
	PermissionMode string                   `json:"permission_mode,omitempty"`
	APIKeySource   string                   `json:"api_key_source,omitempty"`
	Betas          []string                 `json:"betas,omitempty"`
	FastMode       bool                     `json:"fast_mode,omitempty"`
	OutputStyle    string                   `json:"output_style,omitempty"`
	OutputStyles   []string                 `json:"available_output_styles,omitempty"`
	Result         string                   `json:"result"`
	Error          string                   `json:"error,omitempty"`
	Message        *contracts.Message       `json:"message,omitempty"`
	StopReason     string                   `json:"stop_reason,omitempty"`
	Model          string                   `json:"model,omitempty"`
	Usage          *contracts.Usage         `json:"usage,omitempty"`
	ToolResults    []contracts.ToolResult   `json:"tool_results,omitempty"`
	Cleared        bool                     `json:"cleared,omitempty"`
	Compacted      bool                     `json:"compacted,omitempty"`
	Compact        *session.CompactMetadata `json:"compact,omitempty"`
}

type printStreamEvent struct {
	Type           conversation.EventType     `json:"type"`
	Subtype        string                     `json:"subtype,omitempty"`
	SessionID      contracts.ID               `json:"session_id,omitempty"`
	CWD            string                     `json:"cwd,omitempty"`
	Tools          []string                   `json:"tools,omitempty"`
	MCPServers     []printStreamMCPServer     `json:"mcp_servers,omitempty"`
	SlashCommands  []string                   `json:"slash_commands,omitempty"`
	Agents         []string                   `json:"agents,omitempty"`
	Skills         []string                   `json:"skills,omitempty"`
	Plugins        []printStreamPlugin        `json:"plugins,omitempty"`
	PermissionMode string                     `json:"permission_mode,omitempty"`
	APIKeySource   string                     `json:"api_key_source,omitempty"`
	Betas          []string                   `json:"betas,omitempty"`
	FastMode       bool                       `json:"fast_mode,omitempty"`
	OutputStyle    string                     `json:"output_style,omitempty"`
	OutputStyles   []string                   `json:"available_output_styles,omitempty"`
	Message        *contracts.Message         `json:"message,omitempty"`
	ToolUse        *contracts.ToolUse         `json:"tool_use,omitempty"`
	ToolResult     *contracts.ToolResult      `json:"tool_result,omitempty"`
	TokenWarning   *conversation.TokenWarning `json:"token_warning,omitempty"`
	Compact        any                        `json:"compact,omitempty"`
	StreamEvent    *anthropic.StreamEvent     `json:"stream_event,omitempty"`
	Model          string                     `json:"model,omitempty"`
	Error          string                     `json:"error,omitempty"`
	IsError        bool                       `json:"is_error,omitempty"`
	DurationMS     *int64                     `json:"duration_ms,omitempty"`
	DurationAPI    *int64                     `json:"duration_api_ms,omitempty"`
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

func attachStreamJSON(stdout io.Writer, runner conversation.Runner) (conversation.Runner, func() error) {
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
	runner.OnEvent = func(event conversation.Event) {
		if eventErr != nil {
			return
		}
		eventErr = writePrintStreamEvent(encoder, event)
	}
	return runner, func() error { return eventErr }
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

func runnerMCPServerStates(config *conversation.MCPConfig) map[string]mcpServerInitState {
	if config == nil {
		return nil
	}
	user := loadMCPServersForInit(config.UserSettings, mcp.ScopeUser, config.ParseOptions)
	project := loadMCPServersForInit(config.ProjectSettings, mcp.ScopeProject, config.ParseOptions)
	local := loadMCPServersForInit(config.LocalSettings, mcp.ScopeLocal, config.ParseOptions)
	if config.CWD != "" {
		if chain, err := mcp.LoadProjectConfigChain(config.CWD, config.ParseOptions); err == nil {
			project = mcp.MergeServers(project, chain.Servers)
		}
	}
	policySettings := mergeMCPPolicySettingsForInit(config.UserSettings, config.ProjectSettings, config.LocalSettings)
	manual := mcp.MergeServers(user, project, local)
	plugin := mcp.DedupPluginServers(config.PluginServers, manual).Servers
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

func runnerSlashCommandNames(runner conversation.Runner) []string {
	registry := commands.Load(commands.Options{CWD: runner.WorkingDirectory, Settings: runnerMergedSettings(runner)})
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
	registry := commands.Load(commands.Options{CWD: runner.WorkingDirectory, Settings: runnerMergedSettings(runner)})
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
	return pluginpkg.LoadPluginDirsWithSettings(pluginpkg.ProjectPluginDirs(runner.WorkingDirectory), runnerMergedSettings(runner))
}

func runnerMergedSettings(runner conversation.Runner) contracts.Settings {
	if runner.MCP == nil {
		return contracts.Settings{}
	}
	return config.MergeSettings(runner.MCP.UserSettings, runner.MCP.ProjectSettings, runner.MCP.LocalSettings)
}

func writePrintStreamEvent(encoder *json.Encoder, event conversation.Event) error {
	out := printStreamEvent{
		Type:         event.Type,
		Message:      event.Message,
		ToolUse:      event.ToolUse,
		ToolResult:   event.ToolResult,
		TokenWarning: event.TokenWarning,
		StreamEvent:  event.StreamEvent,
		Model:        event.Model,
	}
	if event.Compact != nil {
		out.Compact = event.Compact
	}
	if event.Error != nil {
		out.Error = event.Error.Error()
	}
	return encoder.Encode(out)
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

func writePrintError(stdout io.Writer, runner conversation.Runner, err error, outputFormat string, duration time.Duration, apiDuration time.Duration) error {
	if err == nil {
		return nil
	}
	switch outputFormat {
	case "json":
		return writePrintJSONError(stdout, runner, err, duration, apiDuration)
	case "stream-json":
		return writePrintStreamError(stdout, runner, err, duration, apiDuration)
	default:
		return nil
	}
}

func writePrintJSONError(stdout io.Writer, runner conversation.Runner, err error, duration time.Duration, apiDuration time.Duration) error {
	encoder := json.NewEncoder(stdout)
	envelope := printJSONResult{
		Type:        "result",
		Subtype:     "error",
		IsError:     true,
		DurationMS:  durationMillis(duration),
		DurationAPI: durationMillis(apiDuration),
		SessionID:   runner.SessionID,
		Error:       err.Error(),
	}
	applyPrintJSONRuntime(&envelope, runner)
	return encoder.Encode(envelope)
}

func writePrintStreamError(stdout io.Writer, runner conversation.Runner, err error, duration time.Duration, apiDuration time.Duration) error {
	encoder := json.NewEncoder(stdout)
	durationMS := durationMillis(duration)
	durationAPI := durationMillis(apiDuration)
	envelope := printStreamEvent{
		Type:        "error",
		SessionID:   runner.SessionID,
		Error:       err.Error(),
		IsError:     true,
		DurationMS:  &durationMS,
		DurationAPI: &durationAPI,
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
		Type:        "result",
		Subtype:     "success",
		IsError:     false,
		DurationMS:  durationMillis(duration),
		DurationAPI: durationMillis(result.APIDuration),
		NumTurns:    resultNumTurns(result),
		TotalCost:   usageCostUSD(usage),
		SessionID:   sessionID,
		Result:      text,
		Message:     messagePtr,
		StopReason:  result.StopReason,
		Model:       model,
		Usage:       usage,
		ToolResults: result.ToolResults,
		Cleared:     result.Cleared,
		Compacted:   result.Compacted,
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
