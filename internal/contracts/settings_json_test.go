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
