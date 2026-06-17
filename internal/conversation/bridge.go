package conversation

import (
	"context"
	"encoding/json"
	"net/http"

	bridgepkg "ccgo/internal/bridge"
	"ccgo/internal/commands"
	"ccgo/internal/contracts"
	daemonpkg "ccgo/internal/daemon"
	remotepkg "ccgo/internal/remote"
	"ccgo/internal/tool"
	tasktools "ccgo/internal/tools/task"
)

func (r *Runner) maybeWriteBridgeManifest() {
	settings := r.mergedSettings()
	if settings.Advanced == nil || !advancedBoolEnabled(settings.Advanced.Bridge) {
		return
	}
	path := bridgepkg.SessionManifestPath(r.SessionPath, r.SessionID)
	if path == "" {
		return
	}
	manifest := bridgepkg.WithRemoteTriggerCapability(bridgepkg.WithWebSocketProtocolCapability(bridgepkg.BuildManifestFromSettings(r.SessionID, r.WorkingDirectory, settings)))
	_ = bridgepkg.WriteManifest(path, manifest)
	r.maybeStartBridgeDirect(manifest)
	r.maybeWriteRemoteManifest(manifest)
}

func (r *Runner) maybeStartBridgeDirect(manifest bridgepkg.Manifest) {
	statePath := bridgepkg.SessionDirectStatePath(r.SessionPath, r.SessionID)
	if statePath == "" {
		return
	}
	if r.BridgeDirectServer != nil {
		_ = bridgepkg.WriteDirectState(statePath, bridgepkg.BuildDirectState(r.SessionID, r.WorkingDirectory, manifest, r.BridgeDirectServer, r.BridgeDirectToken, bridgepkg.DirectRuntimeRunning, nil))
		return
	}
	settings := r.mergedSettings()
	registry := commands.Load(commands.Options{CWD: r.WorkingDirectory, Settings: settings})
	handler := bridgepkg.NewDirectHandler(bridgepkg.DirectOptions{
		SessionID:     r.SessionID,
		Manifest:      manifest,
		Registry:      registry,
		RemoteTrigger: r.bridgeRemoteTriggerFunc(),
	})
	server, err := bridgepkg.StartDirectServer(bridgepkg.DirectServerOptions{
		Addr:    r.BridgeDirectAddr,
		Token:   r.BridgeDirectToken,
		Handler: handler,
	})
	if err != nil {
		_ = bridgepkg.WriteDirectState(statePath, bridgepkg.BuildDirectState(r.SessionID, r.WorkingDirectory, manifest, nil, r.BridgeDirectToken, bridgepkg.DirectRuntimeFailed, err))
		return
	}
	r.BridgeDirectServer = server
	_ = bridgepkg.WriteDirectState(statePath, bridgepkg.BuildDirectState(r.SessionID, r.WorkingDirectory, manifest, server, r.BridgeDirectToken, bridgepkg.DirectRuntimeRunning, nil))
}

func (r Runner) maybeWriteRemoteManifest(bridgeManifest bridgepkg.Manifest) {
	path := remotepkg.SessionManifestPath(r.SessionPath, r.SessionID)
	if path == "" {
		return
	}
	settings := r.mergedSettings()
	environmentID := ""
	if settings.Remote != nil {
		environmentID = settings.Remote.DefaultEnvironmentID
	}
	bridgeManifestPath := bridgepkg.SessionManifestPath(r.SessionPath, r.SessionID)
	bridgeStatePath := bridgepkg.SessionDirectStatePath(r.SessionPath, r.SessionID)
	bridgeState, _ := bridgepkg.LoadDirectState(bridgeStatePath)
	daemonStatePath := daemonpkg.SessionStatePath(r.SessionPath, r.SessionID)
	daemonState, _ := daemonpkg.LoadState(daemonStatePath)
	_ = remotepkg.WriteManifest(path, remotepkg.BuildManifest(remotepkg.BuildInput{
		SessionID:             r.SessionID,
		WorkingDirectory:      r.WorkingDirectory,
		EnvironmentID:         environmentID,
		BridgeManifestPath:    bridgeManifestPath,
		BridgeDirectStatePath: bridgeStatePath,
		BridgeManifest:        bridgeManifest,
		BridgeDirectState:     bridgeState,
		DaemonStatePath:       daemonStatePath,
		DaemonState:           daemonState,
	}))
}

func (r Runner) bridgeRemoteTriggerFunc() bridgepkg.DirectRemoteTriggerFunc {
	return func(ctx context.Context, req bridgepkg.DirectRemoteTriggerRequest) (bridgepkg.DirectRemoteTriggerResponse, int) {
		raw, err := json.Marshal(req)
		if err != nil {
			return bridgepkg.DirectRemoteTriggerResponse{Accepted: false, Error: err.Error()}, http.StatusBadRequest
		}
		progressSink := tool.ProgressFunc(func(progress contracts.ToolProgress) error {
			progressCopy := progress
			if progressCopy.ToolUseID == "" {
				progressCopy.ToolUseID = "bridge_remote_trigger"
			}
			r.emit(Event{Type: EventToolProgress, ToolProgress: &progressCopy})
			return nil
		})
		result, err := tasktools.RunRemoteTrigger(tool.Context{
			Context:          ctx,
			WorkingDirectory: r.WorkingDirectory,
			SessionID:        r.SessionID,
			Metadata: map[string]any{
				tool.MetadataSessionPathKey: r.SessionPath,
			},
		}, raw, progressSink)
		if err != nil {
			return bridgepkg.DirectRemoteTriggerResponse{Accepted: false, Error: err.Error()}, http.StatusBadRequest
		}
		return directRemoteTriggerResponse(result.StructuredContent), http.StatusOK
	}
}

func directRemoteTriggerResponse(structured map[string]any) bridgepkg.DirectRemoteTriggerResponse {
	return bridgepkg.DirectRemoteTriggerResponse{
		Accepted:   true,
		Duplicate:  structuredBool(structured, "duplicate"),
		TeamID:     structuredString(structured, "team_id"),
		Target:     structuredString(structured, "target"),
		EventID:    structuredString(structured, "event_id"),
		Source:     structuredString(structured, "source"),
		Event:      structuredString(structured, "event"),
		SentCount:  structuredInt(structured, "sent_count"),
		Structured: structured,
	}
}

func structuredString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func structuredBool(values map[string]any, key string) bool {
	if values == nil {
		return false
	}
	value, _ := values[key].(bool)
	return value
}

func structuredInt(values map[string]any, key string) int {
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
