package conversation

import bridgepkg "ccgo/internal/bridge"

func (r Runner) maybeWriteBridgeManifest() {
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
}
