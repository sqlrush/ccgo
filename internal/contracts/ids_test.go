package contracts

import (
	"encoding/json"
	"testing"
)

func TestIDUnmarshalAcceptsStringNumberAndNull(t *testing.T) {
	var id ID
	if err := json.Unmarshal([]byte(`"evt_1"`), &id); err != nil || id != "evt_1" {
		t.Fatalf("string id = %q, %v", id, err)
	}
	if err := json.Unmarshal([]byte(`42`), &id); err != nil || id != "42" {
		t.Fatalf("numeric id = %q, %v", id, err)
	}
	if err := json.Unmarshal([]byte(`null`), &id); err != nil || id != "" {
		t.Fatalf("null id = %q, %v", id, err)
	}
	if err := json.Unmarshal([]byte(`true`), &id); err == nil {
		t.Fatal("expected bool id to fail")
	}
}
