package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"ccgo/internal/api/anthropic"
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/contracts"
	"ccgo/internal/memory"
	msgs "ccgo/internal/messages"
	"ccgo/internal/session"
	"ccgo/internal/tool"
)

func (r Runner) RunTurn(ctx context.Context, history []contracts.Message, user contracts.Message) (Result, error) {
	if r.Client == nil {
		return Result{}, fmt.Errorf("conversation runner missing client")
	}
	if user.Type == "" {
		user.Type = contracts.MessageUser
	}
	if user.UUID == "" {
		user.UUID = contracts.NewID()
	}
	if r.SessionID != "" {
		user.SessionID = r.SessionID
	}
	history, user = appendMessage(history, user)
	if err := r.appendTranscript(user); err != nil {
		return Result{}, err
	}
	r.emit(Event{Type: EventUserMessage, Message: &user})

	result := Result{Messages: []contracts.Message{user}}
	if compactedHistory, compactResult, ok, err := r.maybeAutoCompact(ctx, history); err != nil {
		return result, err
	} else if ok {
		history = compactedHistory
		result.Compacted = true
		result.Compact = &compactResult
		result.Messages = append(result.Messages, compactResult.Plan.Boundary, compactResult.Plan.Summary)
		if err := r.appendCompactTranscript(compactResult.Plan); err != nil {
			return result, err
		}
		r.emit(Event{Type: EventCompact, Compact: &compactResult})
	}
	toolMetadata := map[string]any{}
	for round := 0; ; round++ {
		if round >= r.maxToolRounds() {
			return result, fmt.Errorf("maximum tool rounds exceeded: %d", r.maxToolRounds())
		}

		request, attempts, response, err := r.send(ctx, history)
		result.FinalRequest = request
		result.ModelsAttempt = append(result.ModelsAttempt, attempts...)
		if err != nil {
			return result, err
		}

		assistant := messageFromResponse(r.SessionID, response)
		history, assistant = appendMessage(history, assistant)
		result.Messages = append(result.Messages, assistant)
		result.Assistant = assistant
		result.StopReason = response.StopReason
		result.Usage = response.Usage
		if err := r.appendTranscript(assistant); err != nil {
			return result, err
		}
		r.emit(Event{Type: EventAssistantMessage, Message: &assistant, Model: response.Model})

		uses := ToolUses(assistant)
		if len(uses) == 0 {
			return result, nil
		}
		toolMessages, toolResults := r.executeToolUses(ctx, uses, toolMetadata)
		for i := range toolMessages {
			history, toolMessages[i] = appendMessage(history, toolMessages[i])
			result.Messages = append(result.Messages, toolMessages[i])
			if err := r.appendTranscript(toolMessages[i]); err != nil {
				return result, err
			}
		}
		result.ToolResults = append(result.ToolResults, toolResults...)
	}
}

func (r Runner) maybeAutoCompact(ctx context.Context, history []contracts.Message) ([]contracts.Message, compactpkg.Result, bool, error) {
	if r.AutoCompact == nil {
		return history, compactpkg.Result{}, false, nil
	}
	config := *r.AutoCompact
	if config.TokenUsage <= 0 {
		config.TokenUsage = compactpkg.EstimateTokens(history)
	}
	if !compactpkg.ShouldRun(history, config) {
		return history, compactpkg.Result{}, false, nil
	}
	client := r.CompactClient
	if client == nil {
		client = r.Client
	}
	result, err := compactpkg.Runner{
		Client:            client,
		Model:             r.model(),
		MaxTokens:         r.CompactMaxTokens,
		KeepLast:          config.KeepLast,
		ExtraInstructions: config.ExtraInstructions,
	}.Compact(ctx, history, compactpkg.TriggerAuto, config.TokenUsage, "")
	if err != nil {
		compactpkg.RecordFailure(r.AutoCompact)
		r.emit(Event{Type: EventCompact, Compact: &result, Error: err})
		return history, result, false, nil
	}
	compactpkg.RecordSuccess(r.AutoCompact)
	return result.Plan.Output, result, true, nil
}

func (r Runner) appendCompactTranscript(plan compactpkg.Plan) error {
	if r.SessionPath != "" {
		if err := session.AppendTranscriptMessage(r.SessionPath, compactpkg.BoundaryTranscriptMessage(plan.Boundary, plan.Metadata)); err != nil {
			return err
		}
		if err := session.Append(r.SessionPath, session.EntryFromMessage(r.SessionID, plan.Summary)); err != nil {
			return err
		}
	}
	root := r.SessionMemoryRoot
	if root == "" {
		root = memory.DefaultSessionMemoryRoot(r.SessionPath)
	}
	if root == "" || r.SessionID == "" {
		return nil
	}
	_, err := memory.WriteSessionSummary(memory.SessionSummaryOptions{
		Root:            root,
		SessionID:       r.SessionID,
		Summary:         msgs.TextContent(plan.Summary),
		LastMessageUUID: plan.Summary.UUID,
		Metadata:        plan.Metadata,
	})
	return err
}

func (r Runner) send(ctx context.Context, history []contracts.Message) (anthropic.Request, []string, *anthropic.Response, error) {
	models := append([]string{r.model()}, r.FallbackModels...)
	var attempts []string
	var lastRequest anthropic.Request
	var lastErr error
	for i, model := range models {
		historyForRequest, err := r.applyToolResultBudget(history)
		if err != nil {
			return anthropic.Request{}, attempts, nil, err
		}
		request, err := r.BuildRequest(historyForRequest, model)
		if err != nil {
			return anthropic.Request{}, attempts, nil, err
		}
		lastRequest = request
		attempts = append(attempts, model)
		response, err := r.createMessage(ctx, request)
		if err == nil {
			return request, attempts, response, nil
		}
		lastErr = err
		if i == len(models)-1 || !isFallbackEligible(err) {
			return request, attempts, nil, err
		}
		r.emit(Event{Type: EventRetry, Model: model, Error: err})
	}
	return lastRequest, attempts, nil, lastErr
}

func (r Runner) applyToolResultBudget(history []contracts.Message) ([]contracts.Message, error) {
	if r.ContentBudget == nil {
		return history, nil
	}
	storeDir := r.ContentBudgetDir
	if storeDir == "" && r.SessionPath != "" && r.SessionID != "" {
		storeDir = filepath.Join(filepath.Dir(r.SessionPath), string(r.SessionID), "tool-results")
	}
	updated, records, err := session.ApplyToolResultBudget(history, r.ContentBudget, session.ToolResultBudgetOptions{
		LimitChars:    r.ContentBudgetMax,
		StoreDir:      storeDir,
		SkipToolNames: r.SkipBudgetTools,
	})
	if err != nil {
		return history, err
	}
	if len(records) > 0 && r.SessionPath != "" {
		if err := session.AppendContentReplacements(r.SessionPath, r.SessionID, records); err != nil {
			return history, err
		}
	}
	return updated, nil
}

func (r Runner) createMessage(ctx context.Context, request anthropic.Request) (*anthropic.Response, error) {
	if r.UseStreaming {
		if streamer, ok := r.Client.(StreamingMessageClient); ok {
			request.Stream = true
			acc := anthropic.NewStreamAccumulator()
			if err := streamer.StreamMessages(ctx, request, acc.Add); err != nil {
				return nil, err
			}
			return acc.Finish(), nil
		}
	}
	return r.Client.CreateMessage(ctx, request)
}

func (r Runner) executeToolUses(ctx context.Context, uses []contracts.ToolUse, metadata map[string]any) ([]contracts.Message, []contracts.ToolResult) {
	toolMessages := make([]contracts.Message, 0, len(uses))
	toolResults := make([]contracts.ToolResult, 0, len(uses))
	for _, use := range uses {
		use := use
		r.emit(Event{Type: EventToolUse, ToolUse: &use})
	}
	toolCtx := tool.Context{
		Context:          ctx,
		WorkingDirectory: r.WorkingDirectory,
		SessionID:        r.SessionID,
		Metadata:         metadata,
	}
	for update := range tool.RunTools(toolCtx, r.Tools, uses, nil, tool.RunOptions{}) {
		use := update.ToolUse
		result := update.Result
		err := update.Err
		if err != nil && result.Content == nil {
			result = tool.ErrorResult(use, err)
		}
		message := msgs.ToolResult(use.ID, result.Content, result.IsError)
		if r.SessionID != "" {
			message.SessionID = r.SessionID
		}
		toolMessages = append(toolMessages, message)
		toolResults = append(toolResults, result)
		r.emit(Event{Type: EventToolResult, Message: &message, ToolResult: &result})
	}
	return toolMessages, toolResults
}

func ToolUses(message contracts.Message) []contracts.ToolUse {
	var out []contracts.ToolUse
	for _, block := range message.Content {
		if block.Type != contracts.ContentToolUse {
			continue
		}
		id := contracts.ID(block.ID)
		if id == "" {
			id = contracts.NewID()
		}
		out = append(out, contracts.ToolUse{
			ID:    id,
			Name:  block.Name,
			Input: normalizeInput(block.Input),
		})
	}
	return out
}

func messageFromResponse(sessionID contracts.ID, response *anthropic.Response) contracts.Message {
	message := contracts.Message{
		ID:        response.ID,
		Type:      contracts.MessageAssistant,
		UUID:      contracts.NewID(),
		SessionID: sessionID,
		Model:     response.Model,
		Content:   response.Content,
		Usage:     &response.Usage,
		Raw: map[string]any{
			"id":            response.ID,
			"type":          response.Type,
			"stop_reason":   response.StopReason,
			"stop_sequence": response.StopSequence,
		},
	}
	return message
}

func appendMessage(history []contracts.Message, message contracts.Message) ([]contracts.Message, contracts.Message) {
	next := append([]contracts.Message(nil), history...)
	if message.UUID == "" {
		message.UUID = contracts.NewID()
	}
	if len(history) == 0 {
		return []contracts.Message{message}, message
	}
	last := next[len(next)-1]
	if last.UUID == "" {
		next[len(next)-1].UUID = contracts.NewID()
		last = next[len(next)-1]
	}
	parent := last.UUID
	message.ParentUUID = &parent
	next = append(next, message)
	return next, message
}

func (r Runner) appendTranscript(message contracts.Message) error {
	if r.SessionPath == "" {
		return nil
	}
	return session.Append(r.SessionPath, session.EntryFromMessage(r.SessionID, message))
}

func normalizeInput(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func isFallbackEligible(err error) bool {
	var apiErr anthropic.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}
	return false
}
