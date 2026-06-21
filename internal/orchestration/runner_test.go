package orchestration

import (
	"context"
	"errors"
	"testing"

	"ccgo/internal/contracts"
)

var errFactory = errors.New("factory boom")

// stubRunTurn returns a fixed TurnResult, proving a REAL RunTurnFunc was invoked
// (vs the old append-only stub that never ran a model loop).
func stubRunTurn(reply string) RunTurnFunc {
	return func(ctx context.Context, history []contracts.Message, user contracts.Message) (TurnResult, error) {
		assistant := contracts.Message{
			Type: contracts.MessageAssistant,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentText, Text: reply},
			},
		}
		msgs := append(append([]contracts.Message{}, history...), user, assistant)
		return TurnResult{
			Messages:   msgs,
			Assistant:  assistant,
			StopReason: "end_turn",
		}, nil
	}
}

func TestRunTeammateExecutesRealTurn(t *testing.T) {
	var persistedID string
	var persisted []contracts.Message

	tr := TeamRunner{
		Factory: func(agentType, model string) (RunTurnFunc, error) {
			return stubRunTurn("done"), nil
		},
		Persist: func(sidechainID string, msgs []contracts.Message) error {
			persistedID = sidechainID
			persisted = append(persisted, msgs...)
			return nil
		},
	}

	out, err := tr.RunTeammate(context.Background(),
		Teammate{SidechainID: "t1", AgentType: "worker", Model: "test-model"},
		nil, "do the thing")
	if err != nil {
		t.Fatalf("RunTeammate err: %v", err)
	}
	if out.Summary == "" {
		t.Fatal("expected a non-empty teammate summary from a real turn")
	}
	if out.Summary != "done" {
		t.Fatalf("expected summary %q, got %q", "done", out.Summary)
	}
	if persistedID != "t1" {
		t.Fatalf("expected Persist called with sidechain t1, got %q", persistedID)
	}
	if len(persisted) == 0 {
		t.Fatal("teammate result was not persisted to the sidechain")
	}
}

func TestRunTeammateInputHistoryNotMutated(t *testing.T) {
	original := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{{Type: contracts.ContentText, Text: "existing"}}},
	}
	origLen := len(original)
	origType := original[0].Type

	tr := TeamRunner{
		Factory: func(agentType, model string) (RunTurnFunc, error) {
			return stubRunTurn("ok"), nil
		},
		Persist: func(_ string, _ []contracts.Message) error { return nil },
	}

	_, err := tr.RunTeammate(context.Background(),
		Teammate{SidechainID: "t2", AgentType: "worker"},
		original, "follow-up")
	if err != nil {
		t.Fatalf("RunTeammate err: %v", err)
	}
	if len(original) != origLen {
		t.Fatalf("input history was mutated: len changed from %d to %d", origLen, len(original))
	}
	if original[0].Type != origType {
		t.Fatalf("input history[0].Type mutated: was %q, now %q", origType, original[0].Type)
	}
}

func TestRunTeammateFactoryError(t *testing.T) {
	tr := TeamRunner{
		Factory: func(string, string) (RunTurnFunc, error) {
			return nil, errFactory
		},
	}
	_, err := tr.RunTeammate(context.Background(), Teammate{SidechainID: "t1"}, nil, "hi")
	if err == nil {
		t.Fatal("expected factory error to propagate")
	}
	if !errors.Is(err, errFactory) {
		t.Fatalf("expected errFactory in chain, got: %v", err)
	}
}

func TestRunTeammatePersistError(t *testing.T) {
	persistErr := errors.New("persist failed")
	tr := TeamRunner{
		Factory: func(agentType, model string) (RunTurnFunc, error) {
			return stubRunTurn("ok"), nil
		},
		Persist: func(_ string, _ []contracts.Message) error {
			return persistErr
		},
	}
	_, err := tr.RunTeammate(context.Background(),
		Teammate{SidechainID: "t3", AgentType: "worker"},
		nil, "test")
	if err == nil {
		t.Fatal("expected persist error to propagate")
	}
	if !errors.Is(err, persistErr) {
		t.Fatalf("expected persistErr in chain, got: %v", err)
	}
}
