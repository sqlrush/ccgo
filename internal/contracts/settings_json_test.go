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
	input := []byte(`{"advanced":{"bridge":true,"lsp":false,"telemetry":true,"computerUse":true}}`)
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
