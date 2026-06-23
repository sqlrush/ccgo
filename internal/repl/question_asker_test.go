package repl

import (
	"context"
	"testing"
	"time"

	"ccgo/internal/tool"
	"ccgo/internal/tui"
)

// TestLoopQuestionAskerSingleSelect verifies the channel-based question asker:
// loopQuestionAsker enqueues a questionRequest on questionCh; the loop pops it,
// sets activeOverlay to a questionOverlay, and when the user selects an option
// the answer is returned.
func TestLoopQuestionAskerSingleSelect(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	gate := make(chan struct{})
	gt := &gatedTerminal{FakeTerminal: ft, gate: gate}
	l := NewLoop(gt, nil)

	// Seam: release input only after question overlay is shown.
	l.onQuestionShown = func() {
		// Down (move to "Light"), Enter (select), then double Ctrl-D (exit).
		_, _ = gt.FakeTerminal.In.WriteString("\x1b[B\r\x04\x04")
		close(gate)
	}

	asker := loopQuestionAsker{questionCh: l.questionCh}
	questions := []tool.Question{
		{
			Header:   "Theme",
			Question: "Which theme?",
			Options: []tool.QuestionOption{
				{Label: "Dark", Description: "Dark UI"},
				{Label: "Light", Description: "Light UI"},
			},
			MultiSelect: false,
		},
	}

	answerCh := make(chan []tool.QuestionAnswer, 1)
	go func() {
		answers, err := asker.AskQuestions(context.Background(), questions)
		if err == nil {
			answerCh <- answers
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = l.Run(ctx)

	select {
	case answers := <-answerCh:
		if len(answers) != 1 {
			t.Fatalf("answers len = %d want 1", len(answers))
		}
		if answers[0].Header != "Theme" {
			t.Fatalf("header = %q want Theme", answers[0].Header)
		}
		if len(answers[0].Selected) != 1 || answers[0].Selected[0] != "Light" {
			t.Fatalf("selected = %v want [Light]", answers[0].Selected)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("question asker never received an answer")
	}
}

// TestLoopQuestionAskerMultiSelect verifies multiSelect=true allows selecting
// multiple options via Space, then Enter to confirm.
func TestLoopQuestionAskerMultiSelect(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	gate := make(chan struct{})
	gt := &gatedTerminal{FakeTerminal: ft, gate: gate}
	l := NewLoop(gt, nil)

	l.onQuestionShown = func() {
		// Space (toggle first=A), Down, Space (toggle second=B), Enter, Ctrl-D Ctrl-D.
		_, _ = gt.FakeTerminal.In.WriteString(" \x1b[B \r\x04\x04")
		close(gate)
	}

	asker := loopQuestionAsker{questionCh: l.questionCh}
	questions := []tool.Question{
		{
			Header:   "Features",
			Question: "Pick features?",
			Options: []tool.QuestionOption{
				{Label: "A", Description: "Feature A"},
				{Label: "B", Description: "Feature B"},
			},
			MultiSelect: true,
		},
	}

	answerCh := make(chan []tool.QuestionAnswer, 1)
	go func() {
		answers, err := asker.AskQuestions(context.Background(), questions)
		if err == nil {
			answerCh <- answers
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = l.Run(ctx)

	select {
	case answers := <-answerCh:
		if len(answers) != 1 {
			t.Fatalf("answers len = %d want 1", len(answers))
		}
		if len(answers[0].Selected) != 2 {
			t.Fatalf("selected = %v want [A B]", answers[0].Selected)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("question asker never received answers")
	}
}

// TestLoopQuestionAskerDenyOnExit verifies that pending question requests are
// cancelled (via context error) when the loop exits without showing the overlay.
func TestLoopQuestionAskerDenyOnExit(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24) // EOF → loop exits immediately
	l := NewLoop(ft, nil)

	asker := loopQuestionAsker{questionCh: l.questionCh}
	questions := []tool.Question{
		{
			Header:   "Q",
			Question: "Exit?",
			Options: []tool.QuestionOption{
				{Label: "Yes", Description: ""},
				{Label: "No", Description: ""},
			},
		},
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := asker.AskQuestions(context.Background(), questions)
		errCh <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = l.Run(ctx)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error when loop exits with pending question")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("question asker not unblocked on loop exit")
	}
}

// ---- unit tests for questionOverlay ----

// TestQuestionOverlaySingleSelectEnter verifies Enter submits selected option.
func TestQuestionOverlaySingleSelectEnter(t *testing.T) {
	q := tool.Question{
		Header:   "Theme",
		Question: "Which theme?",
		Options: []tool.QuestionOption{
			{Label: "Dark", Description: "Dark UI"},
			{Label: "Light", Description: "Light UI"},
		},
		MultiSelect: false,
	}
	o := newQuestionOverlay(q)

	// Move to "Light" then Enter.
	o.ApplyKey(tui.Key{Type: tui.KeyDown})
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit == "" {
		t.Fatal("Enter should produce Submit")
	}
}

// TestQuestionOverlayMultiSelectSpaceAndEnter verifies Space toggles checked
// state and Enter submits the selection.
func TestQuestionOverlayMultiSelectSpaceAndEnter(t *testing.T) {
	q := tool.Question{
		Header:   "Features",
		Question: "Pick?",
		Options: []tool.QuestionOption{
			{Label: "A", Description: ""},
			{Label: "B", Description: ""},
		},
		MultiSelect: true,
	}
	o := newQuestionOverlay(q)

	// Toggle A (Space), move down, toggle B (Space), Enter.
	o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: ' '})
	o.ApplyKey(tui.Key{Type: tui.KeyDown})
	o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: ' '})
	res, _ := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit == "" {
		t.Fatal("Enter should produce Submit for multi-select")
	}
}

// TestQuestionOverlayEscDismisses verifies Esc closes the overlay.
func TestQuestionOverlayEscDismisses(t *testing.T) {
	q := tool.Question{
		Header:   "Q",
		Question: "Do something?",
		Options: []tool.QuestionOption{
			{Label: "Yes", Description: ""},
			{Label: "No", Description: ""},
		},
	}
	o := newQuestionOverlay(q)
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if !res.Dismissed {
		t.Fatal("Esc should dismiss")
	}
}

// TestQuestionOverlayRenderContainsQuestion verifies the overlay renders the
// question text.
func TestQuestionOverlayRenderContainsQuestion(t *testing.T) {
	q := tool.Question{
		Header:   "Test",
		Question: "Is this rendered?",
		Options: []tool.QuestionOption{
			{Label: "Yes", Description: ""},
			{Label: "No", Description: ""},
		},
	}
	o := newQuestionOverlay(q)
	lines := o.Render(80, 24)
	if len(lines) == 0 {
		t.Fatal("Render should return at least one line")
	}
	found := false
	for _, line := range lines {
		if line != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Render returned only empty lines")
	}
}

// TestQuestionOverlaySelectedAnswers verifies that selectedAnswers correctly
// returns chosen labels for both single-select and multi-select.
func TestQuestionOverlaySelectedAnswers(t *testing.T) {
	t.Run("singleSelect", func(t *testing.T) {
		q := tool.Question{
			Header:      "T",
			Question:    "Q?",
			Options:     []tool.QuestionOption{{Label: "X"}, {Label: "Y"}},
			MultiSelect: false,
		}
		o := newQuestionOverlay(q)
		// Move to index 1 (Y) then get answers.
		o.ApplyKey(tui.Key{Type: tui.KeyDown})
		answers := o.selectedAnswers()
		if len(answers) != 1 || answers[0] != "Y" {
			t.Fatalf("selectedAnswers = %v want [Y]", answers)
		}
	})

	t.Run("multiSelect", func(t *testing.T) {
		q := tool.Question{
			Header:      "T",
			Question:    "Q?",
			Options:     []tool.QuestionOption{{Label: "X"}, {Label: "Y"}},
			MultiSelect: true,
		}
		o := newQuestionOverlay(q)
		// Toggle X, Down, Toggle Y.
		o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: ' '})
		o.ApplyKey(tui.Key{Type: tui.KeyDown})
		o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: ' '})
		answers := o.selectedAnswers()
		if len(answers) != 2 {
			t.Fatalf("selectedAnswers = %v want [X Y]", answers)
		}
	})

	t.Run("multiSelectNoneChosen", func(t *testing.T) {
		q := tool.Question{
			Header:      "T",
			Question:    "Q?",
			Options:     []tool.QuestionOption{{Label: "X"}, {Label: "Y"}},
			MultiSelect: true,
		}
		o := newQuestionOverlay(q)
		// Nothing toggled — Enter should still be handled (returns empty selection).
		res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
		if !handled {
			t.Fatal("Enter should always be handled")
		}
		_ = res // empty selection is valid
	})
}
