package conversation

import (
	"testing"

	"ccgo/internal/orchestration"
	"ccgo/internal/tool"
)

// TestToolMetadataInjectsTeamRunner verifies that toolMetadata() injects a
// *orchestration.TeamRunner under MetadataTeamRunnerKey when SessionPath and
// SessionID are set (TEAM-01 / ORCH-23 / ORCH-24 production wiring).
func TestToolMetadataInjectsTeamRunner(t *testing.T) {
	r := Runner{
		SessionPath: "/tmp/test-session",
		SessionID:   "sess-abc",
		Model:       "test-model",
	}

	metadata := r.toolMetadata()

	v, ok := metadata[tool.MetadataTeamRunnerKey]
	if !ok {
		t.Fatalf("toolMetadata() missing MetadataTeamRunnerKey (TEAM-01): TeamRunner not injected into production runner")
	}
	tr, ok := v.(*orchestration.TeamRunner)
	if !ok {
		t.Fatalf("MetadataTeamRunnerKey value is %T, want *orchestration.TeamRunner", v)
	}
	if tr.Factory == nil {
		t.Error("TeamRunner.Factory must be non-nil (ORCH-23: real teammate turns require a factory)")
	}
	if tr.Persist == nil {
		t.Error("TeamRunner.Persist must be non-nil (ORCH-24: teammate results must be persisted)")
	}
}

// TestToolMetadataTeamRunnerFactoryBuildsRunTurnFunc verifies that the
// injected TeamRunner.Factory returns a non-nil RunTurnFunc (ORCH-23).
func TestToolMetadataTeamRunnerFactoryBuildsRunTurnFunc(t *testing.T) {
	r := Runner{
		SessionPath: "/tmp/test-session",
		SessionID:   "sess-abc",
		Model:       "test-model",
	}

	metadata := r.toolMetadata()
	v, ok := metadata[tool.MetadataTeamRunnerKey]
	if !ok {
		t.Skip("TeamRunner not injected — skipping factory test")
	}
	tr := v.(*orchestration.TeamRunner)

	fn, err := tr.Factory("general-purpose", "")
	if err != nil {
		t.Fatalf("Factory returned error: %v", err)
	}
	if fn == nil {
		t.Error("Factory must return a non-nil RunTurnFunc")
	}
}

// TestToolMetadataTeamRunnerNotInjectedWithoutSession verifies that when
// SessionPath is empty (no session), TeamRunner is NOT injected (it would
// have no place to persist).
func TestToolMetadataTeamRunnerNotInjectedWithoutSession(t *testing.T) {
	r := Runner{
		Model: "test-model",
		// No SessionPath / SessionID
	}

	metadata := r.toolMetadata()

	if _, ok := metadata[tool.MetadataTeamRunnerKey]; ok {
		t.Error("toolMetadata() must NOT inject TeamRunner when SessionPath is empty")
	}
}
