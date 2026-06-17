package lsp

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"ccgo/internal/contracts"
)

type ServerProcessOptions struct {
	SessionID         contracts.ID
	Definition        ServerDefinition
	WorkingDirectory  string
	SnapshotPath      string
	ManagerStatusPath string
	FrameLimit        int64
}

type ServerProcess struct {
	cmd               *exec.Cmd
	cancel            context.CancelFunc
	done              chan ServerProcessResult
	stdin             io.WriteCloser
	managerStatusPath string
}

type ServerProcessResult struct {
	Name              string              `json:"name"`
	RuntimeState      string              `json:"runtime_state"`
	Diagnostics       StreamProcessResult `json:"diagnostics"`
	Error             string              `json:"error,omitempty"`
	StartedAt         string              `json:"started_at,omitempty"`
	EndedAt           string              `json:"ended_at,omitempty"`
	ProcessID         int                 `json:"process_id,omitempty"`
	ManagerStatusPath string              `json:"manager_status_path,omitempty"`
	DiagnosticsPath   string              `json:"diagnostics_path,omitempty"`
}

func StartServerProcess(ctx context.Context, opts ServerProcessOptions) (*ServerProcess, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	definition := normalizeServerDefinition(opts.Definition)
	if definition.Name == "" || definition.Command == "" || strings.TrimSpace(opts.SnapshotPath) == "" {
		return nil, os.ErrInvalid
	}
	procCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(procCtx, definition.Command, definition.Args...)
	if strings.TrimSpace(opts.WorkingDirectory) != "" {
		cmd.Dir = opts.WorkingDirectory
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	startedAt := time.Now().UTC()
	_ = writeProcessStatus(opts, definition, ServerRuntimeRunning, "", cmd.Process.Pid, startedAt, time.Time{})
	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()
	process := &ServerProcess{
		cmd:               cmd,
		cancel:            cancel,
		done:              make(chan ServerProcessResult, 1),
		stdin:             stdin,
		managerStatusPath: opts.ManagerStatusPath,
	}
	go process.wait(procCtx, opts, definition, stdout, startedAt)
	return process, nil
}

func (p *ServerProcess) Stdin() io.WriteCloser {
	if p == nil {
		return nil
	}
	return p.stdin
}

func (p *ServerProcess) Done() <-chan ServerProcessResult {
	if p == nil {
		ch := make(chan ServerProcessResult)
		close(ch)
		return ch
	}
	return p.done
}

func (p *ServerProcess) Stop(ctx context.Context) (ServerProcessResult, error) {
	if p == nil {
		return ServerProcessResult{}, os.ErrInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.cancel()
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	select {
	case result := <-p.done:
		return result, nil
	case <-ctx.Done():
		return ServerProcessResult{}, ctx.Err()
	}
}

func (p *ServerProcess) wait(ctx context.Context, opts ServerProcessOptions, definition ServerDefinition, stdout io.Reader, startedAt time.Time) {
	diagnostics, streamErr := ProcessDiagnosticsStreamLimit(ctx, stdout, opts.SnapshotPath, opts.FrameLimit)
	waitErr := p.cmd.Wait()
	endedAt := time.Now().UTC()
	state := ServerRuntimeExited
	errorText := ""
	if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
		state = ServerRuntimeFailed
		errorText = streamErr.Error()
	}
	if waitErr != nil {
		state = ServerRuntimeFailed
		if errorText == "" {
			errorText = waitErr.Error()
		}
	}
	_ = writeProcessStatus(opts, definition, state, errorText, p.cmd.Process.Pid, startedAt, endedAt)
	p.done <- ServerProcessResult{
		Name:              definition.Name,
		RuntimeState:      state,
		Diagnostics:       diagnostics,
		Error:             errorText,
		StartedAt:         startedAt.Format(time.RFC3339Nano),
		EndedAt:           endedAt.Format(time.RFC3339Nano),
		ProcessID:         p.cmd.Process.Pid,
		ManagerStatusPath: opts.ManagerStatusPath,
		DiagnosticsPath:   opts.SnapshotPath,
	}
	close(p.done)
}

func writeProcessStatus(opts ServerProcessOptions, definition ServerDefinition, state string, reason string, pid int, startedAt time.Time, endedAt time.Time) error {
	if strings.TrimSpace(opts.ManagerStatusPath) == "" {
		return nil
	}
	status, err := LoadManagerStatus(opts.ManagerStatusPath)
	if err != nil {
		return err
	}
	if status.SessionID == "" {
		status.SessionID = opts.SessionID
	}
	if status.WorkingDirectory == "" {
		status.WorkingDirectory = opts.WorkingDirectory
	}
	server := serverStatusFromDefinition(definition)
	server.RuntimeState = state
	server.Reason = reason
	server.ProcessID = pid
	if !startedAt.IsZero() {
		server.StartedAt = startedAt.Format(time.RFC3339Nano)
	}
	if !endedAt.IsZero() {
		server.EndedAt = endedAt.Format(time.RFC3339Nano)
	}
	status = UpsertServerStatus(status, server)
	return WriteManagerStatus(opts.ManagerStatusPath, status)
}

func serverStatusFromDefinition(definition ServerDefinition) ServerStatus {
	return ServerStatus{
		Name:           definition.Name,
		Command:        definition.Command,
		Args:           append([]string(nil), definition.Args...),
		Languages:      append([]string(nil), definition.Languages...),
		FileExtensions: append([]string(nil), definition.FileExtensions...),
		RootMarkers:    append([]string(nil), definition.RootMarkers...),
	}
}

func normalizeServerDefinition(definition ServerDefinition) ServerDefinition {
	definition.Name = strings.TrimSpace(definition.Name)
	definition.Command = strings.TrimSpace(definition.Command)
	definition.Args = trimmedProcessArgs(definition.Args)
	definition.Languages = sortedTrimmedStrings(definition.Languages)
	definition.FileExtensions = normalizeExtensions(definition.FileExtensions)
	definition.RootMarkers = sortedTrimmedStrings(definition.RootMarkers)
	if definition.Name == "" && definition.Command == "" {
		return ServerDefinition{}
	}
	return definition
}

func trimmedProcessArgs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
