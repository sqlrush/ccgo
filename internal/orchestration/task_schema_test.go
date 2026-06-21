package orchestration

import "testing"

func TestValidateIsolation(t *testing.T) {
	if got, err := ValidateIsolation(""); err != nil || got != IsolationNone {
		t.Fatalf(`"" => %q,%v`, got, err)
	}
	if got, err := ValidateIsolation("worktree"); err != nil || got != IsolationWorktree {
		t.Fatalf(`"worktree" => %q,%v`, got, err)
	}
	if _, err := ValidateIsolation("remote"); err == nil {
		t.Fatal("remote isolation is out of scope and must be rejected")
	}
	if _, err := ValidateIsolation("bogus"); err == nil {
		t.Fatal("unknown isolation must be rejected")
	}
}

func TestValidateModelAlias(t *testing.T) {
	for _, ok := range []string{"", "sonnet", "opus", "haiku"} {
		if _, err := ValidateModelAlias(ok); err != nil {
			t.Fatalf("model %q should be valid: %v", ok, err)
		}
	}
	if _, err := ValidateModelAlias("gpt-4"); err == nil {
		t.Fatal("unknown model alias must be rejected")
	}
}
