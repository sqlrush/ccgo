package contracts

import (
	"encoding/json"
	"testing"
)

func TestSettingsPreservesUnknownFields(t *testing.T) {
	input := []byte(`{"model":"sonnet","customFlag":{"enabled":true}}`)
	var settings Settings
	if err := json.Unmarshal(input, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Model != "sonnet" {
		t.Fatalf("model = %q", settings.Model)
	}
	if settings.Extra["customFlag"] == nil {
		t.Fatal("expected customFlag in Extra")
	}

	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip["customFlag"] == nil {
		t.Fatalf("unknown field not preserved: %s", encoded)
	}
}

func TestSettingsParsesAdvancedFeatureGates(t *testing.T) {
	input := []byte(`{"advanced":{"bridge":true,"lsp":false,"telemetry":true,"computerUse":true},"telemetryExport":{"path":"/tmp/events.jsonl","url":"https://example.com/telemetry","headers":{"Authorization":"Bearer token"}},"remote":{"defaultEnvironmentId":"env-prod","registrationUrl":"https://example.com/register","authToken":"remote-token"}}`)
	var settings Settings
	if err := json.Unmarshal(input, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Advanced == nil || settings.Advanced.Bridge == nil || !*settings.Advanced.Bridge {
		t.Fatalf("advanced bridge = %#v", settings.Advanced)
	}
	if settings.Advanced.LSP == nil || *settings.Advanced.LSP {
		t.Fatalf("advanced lsp = %#v", settings.Advanced)
	}
	if settings.Advanced.Telemetry == nil || !*settings.Advanced.Telemetry || settings.Advanced.ComputerUse == nil || !*settings.Advanced.ComputerUse {
		t.Fatalf("advanced gates = %#v", settings.Advanced)
	}
	if settings.Extra["advanced"] != nil {
		t.Fatalf("advanced should be a known setting, extra = %#v", settings.Extra)
	}
	if settings.TelemetryExport == nil ||
		settings.TelemetryExport.Path != "/tmp/events.jsonl" ||
		settings.TelemetryExport.URL != "https://example.com/telemetry" ||
		settings.TelemetryExport.Headers["Authorization"] != "Bearer token" {
		t.Fatalf("telemetry export = %#v", settings.TelemetryExport)
	}
	if settings.Extra["telemetryExport"] != nil {
		t.Fatalf("telemetryExport should be a known setting, extra = %#v", settings.Extra)
	}
	if settings.Remote == nil ||
		settings.Remote.DefaultEnvironmentID != "env-prod" ||
		settings.Remote.RegistrationURL != "https://example.com/register" ||
		settings.Remote.AuthToken != "remote-token" {
		t.Fatalf("remote = %#v", settings.Remote)
	}
	if settings.Extra["remote"] != nil {
		t.Fatalf("remote should be a known setting, extra = %#v", settings.Extra)
	}
}

func TestSettingsParsesOfficialMCPPolicyEntries(t *testing.T) {
	input := []byte(`{
		"allowedMcpServers": [
			{"serverName": "github"},
			{"serverCommand": ["node", "server.js"]},
			{"serverUrl": "https://*.example.com/*"}
		],
		"deniedMcpServers": [
			{"serverName": "blocked"}
		]
	}`)

	var settings Settings
	if err := json.Unmarshal(input, &settings); err != nil {
		t.Fatal(err)
	}
	if got := settings.AllowedMCPServers[0].ServerName; got != "github" {
		t.Fatalf("serverName = %q", got)
	}
	if got := settings.AllowedMCPServers[1].ServerCommand; len(got) != 2 || got[0] != "node" || got[1] != "server.js" {
		t.Fatalf("serverCommand = %#v", got)
	}
	if got := settings.AllowedMCPServers[2].ServerURL; got != "https://*.example.com/*" {
		t.Fatalf("serverUrl = %q", got)
	}
	if got := settings.DeniedMCPServers[0].ServerName; got != "blocked" {
		t.Fatalf("denied serverName = %q", got)
	}
}
