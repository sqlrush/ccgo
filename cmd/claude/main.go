package main

import (
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

	"ccgo/internal/api/anthropic"
	"ccgo/internal/auth"
	"ccgo/internal/bootstrap"
	"ccgo/internal/commands"
	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	daemonpkg "ccgo/internal/daemon"
	integrationspkg "ccgo/internal/integrations"
	"ccgo/internal/mcp"
	"ccgo/internal/messages"
	"ccgo/internal/model"
	"ccgo/internal/permissions"
	pluginpkg "ccgo/internal/plugins"
	remotepkg "ccgo/internal/remote"
	"ccgo/internal/session"
	"ccgo/internal/tool"
	filetools "ccgo/internal/tools/file"
	tasktools "ccgo/internal/tools/task"
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
		})
		if err != nil {
			_ = writePrintError(stdout, runner, err, normalizedOutputFormat, time.Since(startedAt), 0, nil)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		history, err := resumeHistory(state, &runner, cliOptions{Resume: *resume, Continue: *continueMode})
		if err != nil {
			_ = writePrintError(stdout, runner, err, normalizedOutputFormat, time.Since(startedAt), 0, nil)
			fmt.Fprintf(stderr, "ccgo: %v\n", err)
			return 1
		}
		streamErr := func() error { return nil }
		if normalizedOutputFormat == "stream-json" {
			runner, streamErr = attachStreamJSON(stdout, runner)
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
		merged := runner.MCP.MergedSettings()
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

func anthropicClientFromEnv(ctx context.Context, fastMode bool) (*anthropic.Client, string, error) {
	credentials, credentialStore, err := credentialsFromEnvOrStore(ctx)
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
	return pluginpkg.LoadPluginDirsWithSettings(pluginpkg.ProjectPluginDirs(runner.WorkingDirectory), runnerMergedSettings(runner))
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

func writePrintStreamEvent(encoder *json.Encoder, event conversation.Event) error {
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
