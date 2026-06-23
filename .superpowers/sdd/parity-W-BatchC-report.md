# Parity W-Batch-C Report

**Commit:** `e735e5e` — feat(tools): Task background dispatch + model override, LSP operations, Bash stdout truncation (W-C14/15/16)

---

## W-C15 — Task async/background path (TOOL-TASK-02/04, ORCH-03/05)

### What was wired

**TOOL-TASK-04 / ORCH-05 — model override** (`internal/tools/task/tools.go:1372–1376`):
- `callTask` computes `effectiveModel = agent.Model` then overrides with `input.Model` when non-empty
- Passes `effectiveModel` into `SidechainOptions.AgentModel` and reports `model_override` in structured content
- Was already implemented in the branch before this session; tests confirmed passing

**TOOL-TASK-02 / ORCH-03 — background dispatch** (`internal/conversation/task_agent.go:36–57`):
- `callTask` sets `run_in_background: true` in structured content when requested
- `maybeRunTaskSubagent` detects `run_in_background` via `isBackgroundTask()`, calls `r.AgentRegistry.StartBackground(sidechainID, fn)` and returns immediately with `status:async_launched`
- Was already implemented in the branch before this session; tests confirmed passing

### Background dispatch concurrency model
`AgentRegistry.StartBackground` launches a goroutine that runs `runTaskSubagentOnce` under a child context derived from the background context. The registry stores the `Outcome` in a sync-protected map. Callers use `Snapshot()` to poll status and `Harvest(id)` to collect results. The goroutine holds no shared state with the main turn; all communication is through the registry map and sidechain transcript on disk.

### Tests
- `TestTaskToolModelOverrideWritesToSidechainMetadata` — verifies agentfile model=sonnet overridden by input model=haiku
- `TestTaskToolRunBackgroundSetsStructuredFlag` / `TestBackgroundTaskRunInBackgroundFlag` — verifies structured content flag
- `TestAgentRegistryBackgroundDispatch` — registry goroutine lifecycle (start → running → done → harvest)
- `TestAgentRegistryBackgroundRoundtrip` — conversation-layer roundtrip
- All pass with `-race`

---

## W-C16 — Bash stdout truncation (TOOL-BASH-07)

### What was implemented (`internal/tools/bash/tools.go:1231–1258`)

`truncateBashOutput(s string) (string, int)`:
- Limit: `1 << 25` = **33,554,432 chars** (2^25), matching CC's `EndTruncatingAccumulator` (`src/utils/stringUtils.ts:88`)
- Format: `s[:maxBashOutputChars] + "\n... [output truncated - NKB removed]"` where N is KB rounded up
- Returns `(original, 0)` when no truncation needed

`formatBashContent` calls `truncateBashOutput(combined)` before trimming/annotating output.

### Tests
- `TestTruncateBashOutputShortOutput` — short input unchanged
- `TestTruncateBashOutputAtLimit` — exactly-at-limit unchanged
- `TestTruncateBashOutputExceedsLimit` — 1 byte over: marker present, KB count reported
- `TestTruncateBashOutputLarge` — 2x limit: prefix preserved
- `TestFormatBashContentTruncation` — formatBashContent surfaces marker for oversized stdout

---

## W-C14 — LSP tool operations (TOOL-LSP-01..05)

### What was implemented

**`internal/lsp/navigation.go`** (new file, 350 lines):
- `NavigationClient` interface with `SendRequest(ctx, filePath, method, params) (json.RawMessage, error)`
- `NavigationParams(operation, fileURI, line, character)` — maps 9 ops to LSP methods with 1-based→0-based coord conversion:
  - goToDefinition → textDocument/definition
  - findReferences → textDocument/references (includeDeclaration:true)
  - hover → textDocument/hover
  - documentSymbol → textDocument/documentSymbol
  - workspaceSymbol → workspace/symbol
  - goToImplementation → textDocument/implementation
  - prepareCallHierarchy/incomingCalls/outgoingCalls → textDocument/prepareCallHierarchy
- `FormatNavigationResult(operation, raw)` — formats Location[], hover, DocumentSymbol[], WorkspaceSymbol[], CallHierarchyItem[]
- `uriToPath` with percent-decoding

**`internal/tool/types.go`**:
- Added `MetadataLSPNavigationKey = "ccgo.lsp.navigation"` for injecting client via metadata

**`internal/tools/lsp/lsp_tool.go`** (updated `dispatchLSP`):
- Extracts `lsp.NavigationClient` from `ctx.Metadata[MetadataLSPNavigationKey]`
- When present: calls `NavigationParams`, sends request, formats result
- Handles two-step callHierarchy/incomingCalls + outgoingCalls (prepareCallHierarchy → callHierarchy/incomingCalls)
- When absent: returns `(zero, false)` → callLSP renders graceful "not supported" (unchanged behaviour)

### Which LSP ops truly work vs deferred

| Op | Status | Note |
|---|---|---|
| TOOL-LSP-01 goToDefinition | ⚠️ seam only | dispatch correct; needs MetadataLSPNavigationKey + live server |
| TOOL-LSP-02 findReferences | ⚠️ seam only | same |
| TOOL-LSP-03 hover | ⚠️ seam only | same |
| TOOL-LSP-04 documentSymbol | ⚠️ seam only | same |
| TOOL-LSP-05 LSPDiagnostics | ⚠️ existing | LSP server manager runtime wiring still needed |

**Why not ✅**: No bidirectional LSP client exists in `internal/lsp`. The package processes a one-way diagnostics stream; it has no `sendRequest` + awaiting-response machinery wired to a live server process. `NavigationClient` is an interface that the runtime must satisfy; no concrete implementation provided (would require a running language server).

### Tests
- `internal/lsp/navigation_test.go` (new, 174 lines): all 9 NavigationParams mappings, coordinate conversion, FormatNavigationResult for Location/hover/symbol/callHierarchy, uriToPath
- `internal/tools/lsp/lsp_tool_test.go` (new seam tests): goToDefinition dispatch, findReferences method, hover content, documentSymbol, server error surfacing, null response formatting, no-client graceful degrade
- All pass with `-race`

---

## Test summary

```
go test ./internal/tools/... ./internal/orchestration/ ./internal/lsp/  — all ok
go test -race ./internal/tools/task/ ./internal/orchestration/ ./internal/tools/lsp/ ./internal/lsp/  — all ok
go test ./...  — all 55 packages ok
go build ./...  — ok
go vet ./...  — only pre-existing client.go:317 unreachable code
```

---

## Status flips (IDs → ✅)

| ID | Section | Was | Now |
|---|---|---|---|
| TOOL-TASK-02 | 05-tools | ⚠️ | ✅ |
| TOOL-TASK-04 | 05-tools | ⚠️ | ✅ |
| TOOL-BASH-07 | 05-tools | ⚠️ | ✅ |
| ORCH-03 | 14-orchestration | ⚠️ | ✅ |
| ORCH-05 | 14-orchestration | ⚠️ | ✅ |

LSP ops TOOL-LSP-01..04 remain ⚠️ (dispatch seam implemented + tested; ✅ requires live NavigationClient at runtime).

---

## Concerns

1. **LSP NavigationClient concrete implementation**: `dispatchLSP` is fully wired but no concrete `NavigationClient` exists. The runtime would need to maintain a bidirectional LSP connection (RPCMux + stdin writer + goroutine) and inject it via `MetadataLSPNavigationKey`. This is a separate work item.
2. **TOOL-LSP-05 LSPDiagnostics**: The tool reads session snapshot from disk (already works) but the write path depends on the LSP server manager being started at session init, which is not yet done.
3. **Background task harvest**: `Harvest(id)` removes the agent from the registry. If the caller never harvests (e.g., crashes), the goroutine result is lost. This is consistent with CC's behavior but worth noting for observability.
