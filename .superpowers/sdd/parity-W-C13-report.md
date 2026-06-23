# W-C13 Parity Report: AskUserQuestion + ExitPlanMode Interactive Presentation

## IDs Flipped

| ID | From | To | Note |
|----|------|----|------|
| TOOL-ASK-01 | ⚠️ | ✅ 接线就绪 | wiring + state layer tested; pixel rendering requires manual verification |
| TOOL-ASK-03 | ⚠️ | ✅ 接线就绪 | multiSelect Space-toggle + Enter confirm tested |
| TOOL-PLAN-02 | ⚠️ | ✅ 接线就绪 | plan content injected into PermissionAsk.Message; dialog rendering requires manual verification |

## Where Wired (file:line)

### TOOL-ASK-01 / TOOL-ASK-03 — AskUserQuestion overlay

| File | Role |
|------|------|
| `internal/repl/question_overlay.go` | `questionOverlay` implements `Overlay`; handles single/multi-select (Space=toggle, Enter=confirm, Esc=dismiss); encodes answer as `"question:[...]"` submit string |
| `internal/repl/question_asker.go` | `loopQuestionAsker` implements `tool.QuestionAsker`; enqueues `questionRequest` on `questionCh`; serializes questions one at a time |
| `internal/repl/loop.go:49` | `questionCh chan questionRequest` field added to `Loop` struct |
| `internal/repl/loop.go:activeQuestion` | `*questionRequest` tracks pending question whose reply is awaiting |
| `internal/repl/loop.go:onQuestionShown` | test seam; called after overlay is set so tests can gate input delivery |
| `internal/repl/loop.go:Run()` | select case on `questionCh` → `showQuestion()` |
| `internal/repl/loop.go:showQuestion()` | sets `activeOverlay = newQuestionOverlay(qr.q)`, stores `activeQuestion` |
| `internal/repl/loop.go:handleOverlaySubmit()` | routes `"question:..."` submit → decodes selected labels → sends to `activeQuestion.reply` |
| `internal/repl/loop.go:handleKey()` (Dismissed path) | cancels `activeQuestion` with error when Esc dismisses the overlay |
| `internal/repl/loop.go:denyPendingQuestions()` | unblocks all waiting askers on loop exit |
| `internal/repl/run.go:mergeQuestionAsker()` | builds a new metadata map with `MetadataQuestionAskerKey` → `loopQuestionAsker` |
| `internal/repl/run.go:newTurnLoop` + `newTurnLoopForRunnerWithHistory` | `r.ExtraToolMetadata = mergeQuestionAsker(...)` per turn (immutable copy) |
| `internal/conversation/types.go:Runner.ExtraToolMetadata` | new `map[string]any` field merged last in `toolMetadata()` |
| `internal/conversation/run.go:toolMetadata()` | merges `r.ExtraToolMetadata` after all other keys |

### TOOL-PLAN-02 — ExitPlanMode plan preview in permission dialog

| File | Role |
|------|------|
| `internal/tools/plan/tools.go:NewExitPlanModeTool().PermissionFunc` | reads plan from disk via `ReadPlan(sessionPath, sessionID)`; injects plan content into `PermissionDecision.Message` (e.g. `"Exit plan mode?...\n\nPlan:\n<content>"`); existing `loopAsker` dialog already shows `Description: decision.Message` |

## Overlay → Result Round-Trip

**AskUserQuestion**:
1. Model calls `AskUserQuestion` tool → `callAsk()` reads `MetadataQuestionAskerKey` from metadata
2. `loopQuestionAsker.AskQuestions()` sends `questionRequest{q, reply}` to `loop.questionCh`
3. Loop select case receives request → `showQuestion(qr)` → sets `activeOverlay = newQuestionOverlay(q)`
4. Keystrokes routed to overlay: Up/Down moves cursor; Space toggles checked (multiSelect); Enter submits `"question:[\"Label\"]"` 
5. `handleKey` → `handleOverlaySubmit("question:...")` → decodes JSON → sends `questionReply{selected}` to `activeQuestion.reply`
6. `loopQuestionAsker.AskQuestions()` receives reply → builds `QuestionAnswer{Header, Selected}` → returns to tool
7. `callAsk()` formats answer → tool result returned to model

**ExitPlanMode**:
1. `CheckPermissions()` reads plan from disk → builds `PermissionDecision{Behavior: Ask, Message: "...Plan:\n<content>"}`
2. `executor.Asker.Ask(...)` called with `Description: decision.Message` (plan content included)
3. Existing `loopAsker` dialog shows the description (plan text) in the permission dialog
4. User approves → `PermissionAllow` → `Call()` invoked → tool result returned

## Tests

### New test files

| File | Tests |
|------|-------|
| `internal/repl/question_asker_test.go` | `TestLoopQuestionAskerSingleSelect`, `TestLoopQuestionAskerMultiSelect`, `TestLoopQuestionAskerDenyOnExit`, `TestQuestionOverlay{SingleSelectEnter,MultiSelectSpaceAndEnter,EscDismisses,RenderContainsQuestion,SelectedAnswers}` |
| `internal/repl/question_wiring_test.go` | `TestMergeQuestionAskerInjectsKey`, `TestMergeQuestionAskerProducesLoopQuestionAsker` |
| `internal/tools/plan/exit_plan_mode_message_test.go` | `TestExitPlanModePermissionMessageIncludesPlan`, `TestExitPlanModePermissionMessageNoPlanFile` |

## Full Suite Results

```
go test ./... -timeout 120s
# ALL PASS (61 packages)
go vet ./...
# Only pre-existing: internal/api/anthropic/client.go:317: unreachable code
go build ./...
# Clean
```

## Status Flips

- TOOL-ASK-01: ⚠️ → ✅ 通过（接线就绪，渲染需人工核验）
- TOOL-ASK-03: ⚠️ → ✅ 通过（接线就绪，渲染需人工核验）
- TOOL-PLAN-02: ⚠️ → ✅ 通过（接线就绪，渲染需人工核验）

Section 05 subtotal updated: ✅ 45→48, ⚠️ 8→5

## Concerns

1. **Pixel rendering (manual verification needed)**: The `questionOverlay.Render()` uses plain ASCII ("> " cursor, "[ ]"/"[x]" toggles). CC renders rich chip-based dialogs. The wiring is correct but visual parity requires manual TUI inspection.
2. **ExitPlanMode permission dialog layout**: Plan content is injected into `PermissionDecision.Message` which the existing `tui.DialogRuntime` shows in its description field. The plan may be long and the current dialog doesn't truncate or scroll it — acceptable for wiring-ready status but worth noting for full visual parity.
3. **Multi-question sequential display**: AskUserQuestion supports 1-4 questions; `loopQuestionAsker` shows them one at a time (sequential). CC may show all questions in one panel. This is a UX difference but functionally correct.
