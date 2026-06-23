package repl

import (
	"context"
	"fmt"

	"ccgo/internal/tool"
)

// questionRequest is a single question that the executor wants shown to the
// user. reply receives the selected option labels when the user confirms.
type questionRequest struct {
	q     tool.Question
	reply chan questionReply
}

// questionReply carries either the selected option labels or an error.
type questionReply struct {
	selected []string
	err      error
}

// loopQuestionAsker implements tool.QuestionAsker by forwarding each question
// to the event loop over questionCh. The loop renders a questionOverlay for
// each; when the user confirms, the selection is returned.
//
// Questions are serialised: AskQuestions shows each question one at a time and
// accumulates the results before returning to the caller.
type loopQuestionAsker struct {
	questionCh chan questionRequest
}

// Compile-time check that loopQuestionAsker satisfies tool.QuestionAsker.
var _ tool.QuestionAsker = loopQuestionAsker{}

// AskQuestions implements tool.QuestionAsker. It shows each question
// sequentially and returns the collected answers.
func (a loopQuestionAsker) AskQuestions(ctx context.Context, questions []tool.Question) ([]tool.QuestionAnswer, error) {
	answers := make([]tool.QuestionAnswer, 0, len(questions))
	for _, q := range questions {
		reply := make(chan questionReply, 1)
		select {
		case a.questionCh <- questionRequest{q: q, reply: reply}:
		case <-ctx.Done():
			return nil, fmt.Errorf("ask questions: %w", ctx.Err())
		}
		select {
		case r := <-reply:
			if r.err != nil {
				return nil, fmt.Errorf("ask questions: %w", r.err)
			}
			answers = append(answers, tool.QuestionAnswer{
				Header:   q.Header,
				Selected: r.selected,
			})
		case <-ctx.Done():
			return nil, fmt.Errorf("ask questions: %w", ctx.Err())
		}
	}
	return answers, nil
}
