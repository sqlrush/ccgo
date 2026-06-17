package remote

import (
	"path/filepath"
	"testing"
	"time"

	bridgepkg "ccgo/internal/bridge"
	"ccgo/internal/contracts"
	daemonpkg "ccgo/internal/daemon"
)

func TestBuildManifestAggregatesBridgeAndDaemonServices(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	manifest := BuildManifest(BuildInput{
		SessionID:             "sess_remote",
		WorkingDirectory:      "/work",
		EnvironmentID:         "env-prod",
		BridgeManifestPath:    "/state/bridge-manifest.json",
		BridgeDirectStatePath: "/state/bridge-direct.json",
		BridgeManifest: bridgepkg.Manifest{
			SessionID: "sess_remote",
			Commands:  []bridgepkg.Command{{Name: "compact", Type: contracts.CommandLocal}},
			Capabilities: []bridgepkg.Capability{
				{Name: "websocket_protocol"},
				{Name: "remote_trigger"},
			},
		},
		BridgeDirectState: bridgepkg.DirectState{
			RuntimeState:  bridgepkg.DirectRuntimeRunning,
			URL:           "http://127.0.0.1:3333",
			WebSocketURL:  "ws://127.0.0.1:3333/ws",
			TokenRequired: true,
			Commands:      1,
			GeneratedAt:   now.Format(time.RFC3339Nano),
		},
		DaemonStatePath: "/state/daemon-state.json",
		DaemonState:     daemonpkg.BuildState("sess_remote", "/work", daemonpkg.RuntimeRunning, 4242, "http://127.0.0.1:4444", now, nil),
		Now:             now,
	})
	if manifest.SessionID != "sess_remote" || manifest.EnvironmentID != "env-prod" || len(manifest.Services) != 2 {
		t.Fatalf("manifest = %#v", manifest)
	}
	bridge := manifest.Services[0]
	if bridge.Name != "bridge" || bridge.RuntimeState != bridgepkg.DirectRuntimeRunning || bridge.Endpoint != "http://127.0.0.1:3333" || !bridge.TokenRequired || len(bridge.Capabilities) != 2 {
		t.Fatalf("bridge service = %#v", bridge)
	}
	daemon := manifest.Services[1]
	if daemon.Name != "daemon" || daemon.RuntimeState != daemonpkg.RuntimeRunning || daemon.PID != 4242 || ServiceCapabilityNames(daemon) != "health, status, tick, stop" {
		t.Fatalf("daemon service = %#v", daemon)
	}
}

func TestWriteAndLoadManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_remote", manifestFileName)
	manifest := BuildManifest(BuildInput{SessionID: "sess_remote", WorkingDirectory: "/work"})
	if err := WriteManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_remote" || loaded.GeneratedAt == "" {
		t.Fatalf("loaded = %#v", loaded)
	}
	missing, err := LoadManifest(filepath.Join(t.TempDir(), manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if missing.GeneratedAt != "" {
		t.Fatalf("missing = %#v", missing)
	}
}
