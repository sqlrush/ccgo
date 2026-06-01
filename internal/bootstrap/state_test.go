package bootstrap

import "testing"

func TestStateFeature(t *testing.T) {
	state, err := New()
	if err != nil {
		t.Fatal(err)
	}
	state.SetFeature("BUDDY", true)
	if !state.Feature("BUDDY") {
		t.Fatal("feature not enabled")
	}
	if state.SessionID() == "" || state.CWD() == "" {
		t.Fatalf("state missing ids: %#v", state)
	}
}
