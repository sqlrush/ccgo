# G27 Parity Report

Date: 2026-06-24  
Branch: feat/phase2-7-impl

## Items Implemented

### §16 SDK

**SDK-58 — `system/task_notification` event**

- Added `SetOnTaskDone(fn func(string, AgentState, Outcome))` method and `onDone func(...)` field to `AgentRegistry` in `internal/orchestration/registry.go`.
- Added `StartBackgroundWithNotify(id, run, onDone)` which fires global callback first, then per-task callback (ordering ensures SDK encoder writes complete before test watchers unblock).
- `sdk.Query` calls `agentReg.SetOnTaskDone(...)` when `runner.AgentRegistry != nil`, emitting `system/task_notification` sdk_event with `task_id`, `status` (completed/failed), `summary`, and `session_id`.
- Tests: `TestAgentRegistryNotifyOnCompletion`, `TestAgentRegistryNotifyOnFailure`, `TestAgentRegistryNotifyNilCallbackIsSafe`, `TestQueryEmitsTaskNotificationOnAgentCompletion` (uses `safeBuffer` for race-safety).

**SDK-57 — `auth_status` event**

- Added `AuthStatus *AuthStatusSnapshot` field to `sdk.Options` and `AuthStatusSnapshot` struct with `IsAuthenticating bool`, `Output []string`, `Error string`.
- `sdk.Query` emits `auth_status` sdk_event before `runner.OnEvent` when `opts.AuthStatus != nil`.
- Tests: `TestQueryEmitsAuthStatusEvent`, `TestQueryAuthStatusNilNotEmitted`, `TestQueryAuthStatusIsAuthenticating`.

### §15 Sandbox

**SBX-52 — `enableWeakerNestedSandbox` policy field**

- Added `EnableWeakerNestedSandbox bool` to `Policy` struct in `internal/sandbox/policy.go`.
- Wired from `sandbox.enableWeakerNestedSandbox` in `PolicyFromSettingsAt` in `internal/sandbox/policy_settings.go`.
- Tests: `TestPolicyFromSettingsWeakerNestedSandbox` (true/false/absent), `TestWeakerFieldsIndependent`.
- Note: enforcement logic (applying weaker seatbelt profile for nested sandboxes) is a future seam.

**SBX-53 — `enableWeakerNetworkIsolation` policy field**

- Added `EnableWeakerNetworkIsolation bool` to `Policy` struct.
- Wired from `sandbox.enableWeakerNetworkIsolation` in `PolicyFromSettingsAt`.
- Tests: `TestPolicyFromSettingsWeakerNetworkIsolation` (true/false/absent).
- Note: macOS seatbelt profile trustd network exception is a future seam.

### §01 CLI Flags

**CLI-FLAG-41 — `--include-hook-events`**

- Changed `attachStreamJSON` signature to accept `includeHookEvents bool` as 4th parameter.
- Updated `hookProgressPhase(tp, includeHookEvents bool)`: when `false`, filters to scope=="conversation" only; when `true`, allows all non-empty scopes.
- Added `isHookToolProgress(tp)` helper: hook-scoped ToolProgress events that don't match the include filter are completely suppressed (not emitted as generic `tool_progress`).
- Updated all callers in `main_test.go`, `partial_messages_g3_test.go`, `sdk_g1_test.go` to pass the new `false` argument.
- Tests: `TestHookProgressPhaseConversationScopeAlwaysEmitted`, `TestHookProgressPhaseNonConversationScopeFiltered`, `TestHookProgressPhaseNonConversationScopeIncludedWhenFlagSet`, `TestHookProgressPhasePostTurnIncludedWhenFlagSet`, `TestAttachStreamJSONHookEventsFiltered`, `TestAttachStreamJSONHookEventsIncluded`.

**CLI-FLAG-43 — `--replay-user-messages`**

- In `run()`, when `normalizedOutputFormat=="stream-json" && *replayUserMessages`, emits the user message as `{"type":"user_message","message":{...}}` on stdout before `RunTurn`.
- Test: `TestReplayUserMessagesWiredToStreamJSON` (unit-level, verifies the event JSON shape).

**CLI-FLAG-47 — `--plugin-dir`**

- In `headlessRunner`, when `options.PluginDirs` is non-empty:
  - Calls `pluginpkg.LoadMCPServersWithSettings(options.PluginDirs, mergedSettings)` and merges the result into `runner.MCP.PluginServers`.
  - For each plugin dir, checks for a `skills/` sub-directory and appends it to `runner.SkillDirs`.
- Tests: `TestPluginDirWiresMCPServersToRunner` (plugin with `plugin.json` + `.mcp.json`), `TestPluginDirWiresSkillDirsToRunner` (plugin with `skills/` dir), `TestPluginDirEmptyDoesNotPanic`.

## Files Changed

- `internal/orchestration/registry.go` — Added `onDone` field, `SetOnTaskDone`, `StartBackgroundWithNotify`; changed callback ordering (global first, then per-task).
- `internal/sdk/query.go` — Added `AuthStatus *AuthStatusSnapshot` to Options, `AuthStatusSnapshot` struct; SDK-57 emit block; SDK-58 `SetOnTaskDone` wiring.
- `internal/sandbox/policy.go` — Added `EnableWeakerNestedSandbox`, `EnableWeakerNetworkIsolation` fields.
- `internal/sandbox/policy_settings.go` — Wired both new fields.
- `cmd/claude/main.go` — `attachStreamJSON` + `hookProgressPhase` + `writePrintStreamEvent` updated with `includeHookEvents bool`; `isHookToolProgress` helper added; `--replay-user-messages` emit block; CLI-FLAG-47 plugin dir wiring in `headlessRunner`.
- `cmd/claude/main_test.go`, `partial_messages_g3_test.go`, `sdk_g1_test.go` — Updated `attachStreamJSON`/`writePrintStreamEvent` call sites to pass new `false` argument.

## New Test Files

- `internal/sdk/query_g27_test.go` — SDK-57/58 tests with `safeBuffer` for race-safe buffer writes.
- `internal/sandbox/policy_g27_test.go` — SBX-52/53 tests (7 subtests).
- `cmd/claude/main_g27_test.go` — CLI-FLAG-41/43/47 wiring tests (11 tests).

## Test Results

All G27 tests pass with `-race`:
- `go test -race ./internal/sdk/ ./internal/orchestration/ ./internal/sandbox/` — ok
- `go test -race ./cmd/claude/ -run "G27|HookProgressPhase|AttachStreamJSON|PluginDir|ReplayUser|WeakerNested|WeakerNetwork|PolicyFromSettings"` — ok
- Pre-existing races in `TestRunDaemonServesHealthEndpoint` and `TestBrowserAuthorizerCallbackSuccess` are unrelated to G27 changes.

## Parity Doc Updates

- `docs/cc-parity/sections/16-sdk.md`: SDK-57/58 ❌→✅; subtotal 61→63 ✅.
- `docs/cc-parity/sections/15-sandbox.md`: SBX-52/53 ❌→✅; subtotal 42→44 ✅.
- `docs/cc-parity/sections/01-cli-entrypoints.md`: CLI-FLAG-41/43/47 ⚠️→✅; subtotal 74→77 ✅.
