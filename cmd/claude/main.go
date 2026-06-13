package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/auth"
	"ccgo/internal/bootstrap"
	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/messages"
	"ccgo/internal/model"
	"ccgo/internal/permissions"
	"ccgo/internal/tool"
	filetools "ccgo/internal/tools/file"
)

const version = "0.0.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

type cliOptions struct {
	Model          string
	MaxTokens      int
	PermissionMode string
	Stream         bool
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("claude", flag.ContinueOnError)
	flags.SetOutput(stderr)

	showVersion := flags.Bool("version", false, "print version")
	flags.BoolVar(showVersion, "v", false, "print version")
	printMode := flags.Bool("print", false, "print response and exit")
	flags.BoolVar(printMode, "p", false, "print response and exit")
	modelName := flags.String("model", "", "model to use")
	maxTokens := flags.Int("max-tokens", 0, "maximum output tokens")
	permissionMode := flags.String("permission-mode", "", "permission mode")
	stream := flags.Bool("stream", false, "use streaming API")
	outputFormat := flags.String("output-format", "text", "output format: text, json, or stream-json")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		fmt.Fprintf(stdout, "%s (ccgo)\n", version)
		return 0
	}

	state, err := bootstrap.New()
	if err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}
	if *printMode {
		prompt, err := promptFromArgsOrStdin(flags.Args(), stdin)
		if err != nil {
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		format, err := normalizeOutputFormat(*outputFormat)
		if err != nil {
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		runner, err := headlessRunner(context.Background(), state, cliOptions{
			Model:          *modelName,
			MaxTokens:      *maxTokens,
			PermissionMode: *permissionMode,
			Stream:         *stream,
		})
		if err != nil {
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		streamErr := func() error { return nil }
		if format == "stream-json" {
			runner, streamErr = attachStreamJSON(stdout, runner)
		}
		result, err := runner.RunTurn(context.Background(), nil, messages.UserText(prompt))
		if err != nil {
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		if err := streamErr(); err != nil {
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		if err := writePrintResult(stdout, result, format); err != nil {
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

func normalizeOutputFormat(raw string) (string, error) {
	format := strings.TrimSpace(strings.ToLower(raw))
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

func headlessRunner(ctx context.Context, state *bootstrap.State, options cliOptions) (conversation.Runner, error) {
	runner, err := state.ConversationRunner()
	if err != nil {
		return conversation.Runner{}, err
	}
	registry, err := tool.NewRegistry(filetools.BuiltinTools()...)
	if err != nil {
		return conversation.Runner{}, err
	}
	runner.Tools = tool.NewExecutor(registry)
	runner.Permissions, err = permissionDeciderFromSettings(runner.MCP, strings.TrimSpace(options.PermissionMode))
	if err != nil {
		return conversation.Runner{}, err
	}
	runner.Model = resolveCLIModel(options.Model, runner.MCP)
	if options.MaxTokens > 0 {
		runner.MaxTokens = options.MaxTokens
	}
	runner.UseStreaming = options.Stream

	client, err := anthropicClientFromEnv(ctx)
	if err != nil {
		return conversation.Runner{}, err
	}
	runner.Client = client
	return runner, nil
}

func permissionDeciderFromSettings(mcpConfig *conversation.MCPConfig, permissionMode string) (tool.PermissionDecider, error) {
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
			Permissions: &contracts.PermissionsSetting{DefaultMode: mode},
		})
	}
	engine, err := permissions.NewEngineFromSettingsSources(managedRulesOnly, sources...)
	if err != nil {
		return nil, err
	}
	return tool.NewEnginePermissionDecider(engine), nil
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

func anthropicClientFromEnv(ctx context.Context) (*anthropic.Client, error) {
	credentials := auth.FromEnv()
	if credentials.Source == auth.SourceNone {
		return nil, fmt.Errorf("missing Anthropic credentials; set ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_REFRESH_TOKEN")
	}
	if credentials.Source == auth.SourceOAuth && strings.TrimSpace(credentials.AccessToken) == "" && strings.TrimSpace(credentials.RefreshToken) != "" {
		provider := auth.NewOAuthTokenProvider(auth.OAuthTokenProviderOptions{Credentials: credentials})
		token, err := provider.CurrentAccessToken(ctx)
		if err != nil {
			return nil, err
		}
		credentials.AccessToken = token
	}
	if err := credentials.Validate(); err != nil {
		return nil, err
	}
	options := []anthropic.Option{
		anthropic.WithCredentials(credentials),
		anthropic.WithUserAgent("ccgo/" + version),
	}
	if baseURL := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); baseURL != "" {
		options = append(options, anthropic.WithBaseURL(baseURL))
	}
	if beta := splitEnvList(os.Getenv("ANTHROPIC_BETA")); len(beta) > 0 {
		options = append(options, anthropic.WithBeta(beta...))
	}
	return anthropic.NewClient(options...), nil
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
	Type        string                 `json:"type"`
	Subtype     string                 `json:"subtype"`
	SessionID   contracts.ID           `json:"session_id,omitempty"`
	Result      string                 `json:"result"`
	Message     *contracts.Message     `json:"message,omitempty"`
	StopReason  string                 `json:"stop_reason,omitempty"`
	Model       string                 `json:"model,omitempty"`
	Usage       *contracts.Usage       `json:"usage,omitempty"`
	ToolResults []contracts.ToolResult `json:"tool_results,omitempty"`
}

type printStreamEvent struct {
	Type         conversation.EventType     `json:"type"`
	Message      *contracts.Message         `json:"message,omitempty"`
	ToolUse      *contracts.ToolUse         `json:"tool_use,omitempty"`
	ToolResult   *contracts.ToolResult      `json:"tool_result,omitempty"`
	TokenWarning *conversation.TokenWarning `json:"token_warning,omitempty"`
	Compact      any                        `json:"compact,omitempty"`
	StreamEvent  *anthropic.StreamEvent     `json:"stream_event,omitempty"`
	Model        string                     `json:"model,omitempty"`
	Error        string                     `json:"error,omitempty"`
}

func attachStreamJSON(stdout io.Writer, runner conversation.Runner) (conversation.Runner, func() error) {
	encoder := json.NewEncoder(stdout)
	var eventErr error
	runner.OnEvent = func(event conversation.Event) {
		if eventErr != nil {
			return
		}
		eventErr = writePrintStreamEvent(encoder, event)
	}
	return runner, func() error { return eventErr }
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

func writePrintResult(stdout io.Writer, result conversation.Result, outputFormat string) error {
	text := messages.TextContent(result.Assistant)
	if text == "" {
		for i := len(result.Messages) - 1; i >= 0; i-- {
			if result.Messages[i].Type == contracts.MessageAssistant {
				text = messages.TextContent(result.Messages[i])
				break
			}
		}
	}
	if text == "" {
		return nil
	}
	if outputFormat == "json" || outputFormat == "stream-json" {
		return writePrintJSONResult(stdout, result, text)
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

func writePrintJSONResult(stdout io.Writer, result conversation.Result, text string) error {
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
	envelope := printJSONResult{
		Type:        "result",
		Subtype:     "success",
		SessionID:   sessionID,
		Result:      text,
		Message:     messagePtr,
		StopReason:  result.StopReason,
		Model:       message.Model,
		Usage:       usage,
		ToolResults: result.ToolResults,
	}
	encoder := json.NewEncoder(stdout)
	return encoder.Encode(envelope)
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
