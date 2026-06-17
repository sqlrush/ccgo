package conversation

import bridgepkg "ccgo/internal/bridge"

func (r *Runner) maybeWriteBridgeManifest() {
	settings := r.mergedSettings()
	if settings.Advanced == nil || !advancedBoolEnabled(settings.Advanced.Bridge) {
		return
	}
	path := bridgepkg.SessionManifestPath(r.SessionPath, r.SessionID)
	if path == "" {
		return
	}
	manifest := bridgepkg.BuildManifestFromSettings(r.SessionID, r.WorkingDirectory, settings)
	_ = bridgepkg.WriteManifest(path, manifest)
	r.maybeStartBridgeDirect(manifest)
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
	handler := bridgepkg.NewDirectHandlerFromSettings(r.SessionID, r.WorkingDirectory, r.mergedSettings())
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
