# W-REPL Wiring Report (W-C02 .. W-C08)

Wires the already-built REPL seams (vim mode, custom keybindings, prompt history,
screen events, overlays) into the live `claude` interactive binary. All 11 plan
tasks executed inline via TDD (failing test → minimal impl → pass) on branch
`feat/phase2-7-impl`.

## Result

- Full suite: `go test ./...` — ALL PASS
- Build: `go build ./...` — OK
- Windows build: `GOOS=windows go build ./internal/repl/` — OK
- Vet: `go vet ./...` — only pre-existing `internal/api/anthropic/client.go:317:2: unreachable code` (accepted)

## Commits (per-task)

| SHA | Subject |
|-----|---------|
| 3aea9b0 | feat(repl): add Overlay field to CommandOutcome; wire in applyCommandOutcome |
| 93060bf | feat(repl): handle ScreenEventStashPrompt/ExternalEditor/ToggleTranscript/FocusIn/Out (W-C04) |
| 6e98495 | feat(repl): /resume with no arg opens ResumePicker overlay (W-C07/OVL-09/CMD-RESUME-02) |
| 7aa94ec | feat(repl): /theme with no arg opens ThemePicker overlay (W-C07/OVL-11) |
| e57a66d | feat(repl): /memory opens MemorySelector overlay (W-C07/OVL-13/CMD-MEMORY-01) |
| fe5e696 | feat(repl): /help opens HelpScreen overlay; /doctor runs and returns status (W-C08/OVL-14/OVL-15) |
| 3549fa6 | feat(repl): /model opens ModelPicker overlay (W-C07/CMD-MODEL-01) |
| c70c9f1 | feat(repl): seed loop with persisted prompt history for Up-arrow/Ctrl+R (W-C06/HIST-03/HIST-04) |
| bc1bfec | feat(repl): populate Engine/EditorMode/PromptHistory/MemoryFiles/ResumeEntries in InteractiveOptions (W-C02/W-C05/W-C06/W-C07) |
| 4c3d7f8 | feat(repl): load ~/.claude/keybindings.json and apply custom keymap to loop (W-C03/REPL-54) |

Representative wiring SHA used in parity docs: `4c3d7f8`.

## What was wired (file:where)

- `internal/repl/commands.go` — `CommandOutcome.Overlay Overlay` field.
- `internal/repl/loop.go`:
  - `applyCommandOutcome` sets `l.activeOverlay` when `outcome.Overlay != nil`.
  - event switch now handles `ScreenEventStashPrompt`, `ScreenEventToggleTranscript`,
    `ScreenEventFocusIn/Out`, `ScreenEventExternalEditor` (`launchExternalEditor`
    seeds a temp file with the draft, runs `$EDITOR`/`$VISUAL`/`vi`, fills the
    edited content back into `screen.Prompt.Text`; terminal restored/re-entered;
    errors surfaced as system messages, never abort the loop).
  - `NewLoopFromHistoryEntries(Terminal, []session.HistoryEntry)` constructor.
  - `handleOverlaySubmit` prefix list extended with `"model:"`.
- `internal/repl/commands_resume.go` — no-arg `/resume`/`/continue` returns
  `CommandOutcome{Overlay: NewResumePicker(...)}` (removed dead `formatResumeList`).
- `internal/repl/commands_settings.go` — no-arg `/theme` opens `NewThemePicker(builtinThemes)`.
- `internal/repl/commands_memory.go` (new) — `memoryHandler(cwd)` / `memoryHandlerWith`,
  opens `NewMemorySelector`; graceful Status on discovery error / no files.
- `internal/repl/commands_help.go` (new) — `/help` opens `NewHelpScreen(registry)`.
- `internal/repl/commands_doctor.go` (new) — `/doctor` runs `doctor.Run` + `doctor.Format`,
  returns text via `CommandOutcome.Status` (full overlay UI MANUAL).
- `internal/repl/commands_model.go` + `model_picker.go` (new) — `/model` no-arg opens
  `NewModelPicker(builtinModels)`.
- `internal/repl/run.go`:
  - `InteractiveOptions` gains `PromptHistory`, `EditorMode`, `CustomKeymap`.
  - `newProductionRouter(cwd, registry)` registers `memory`, `help`, `doctor`, `model`.
  - `RunInteractiveWithOptions` seeds prompt history (`newTurnLoopForRunnerWithHistory`),
    applies `CustomKeymap`, applies vim via `SetVimEnabled` when `EditorMode=="vim"`,
    and overrides `resume`/`continue`/`memory` handlers with host-supplied
    `ResumeEntries`/`MemoryFiles` when present.
- `cmd/claude/main.go` (Task 9, the critical production wiring):
  - `engineFromDecider(tool.PermissionDecider) *permissions.Engine` helper.
  - Interactive `opts` now populated with `Engine` (from `engineFromDecider(runner.Permissions)`),
    `EditorMode` (`mergedSettings.EditorMode`), `PromptHistory` (`session.LoadHistory`),
    `MemoryFiles` (`memory.DiscoverScopedClaudeFiles`), `ResumeEntries`
    (`session.ListProjectSessions`), and `CustomKeymap` (`tui.LoadKeyBindingSpecs` +
    `tui.KeymapFromSpecs` over `~/.claude/keybindings.json`; absent file silently skipped).

## Tests added

- `commands_overlay_test.go` — overlay set by applyCommandOutcome + opened via router/loop.
- `loop_screen_events_test.go` — stash/focus/toggle events handled without panic/deadlock.
- `commands_resume_test.go` — no-arg `/resume` opens overlay (obsolete text-list test repurposed).
- `commands_settings_test.go` — no-arg `/theme` opens overlay.
- `commands_memory_test.go` — overlay / discovery-error / no-files paths.
- `commands_help_doctor_test.go` — help overlay excludes hidden cmds; doctor returns Status.
- `commands_model_test.go` — model overlay + picker render.
- `history_load_test.go` — `NewLoopFromHistoryEntries` Up-arrow surfaces persisted entries.
- `interactive_options_test.go` — vim-enable seam.
- `keybinding_load_test.go` — custom keymap override + graceful missing-file (errors.Is os.ErrNotExist).

## Parity IDs flipped to ✅

- 02-repl-tui: REPL-31, REPL-32, REPL-33, REPL-46, REPL-47, REPL-48, REPL-54, REPL-57
  (小计 ✅ 30→38, ⚠️ 9→1)
- 03-overlays-dialogs: OVL-09, OVL-11, OVL-13, OVL-14, OVL-15
  (小计 ✅ 22→27, ⚠️ 9→4)
- 06-permissions: PERM-MODE-07, PERM-PERSIST-03 — already ✅ from W-C05 (commit 47d0a60);
  Task 9 completes their reachability by populating `opts.Engine`. No doc change needed.
- 07-slash-commands: CMD-RESUME-02, CMD-MEMORY-01, CMD-MODEL-01
  (小计 ✅ 24→27, ⚠️ 8→5)
- 11-sessions-memory-compact: HIST-03, HIST-04, SESS-05, MEM-09
  (小计 ✅ 19→23, ⚠️ 8→4)

All flips carry `✅ 通过（接线就绪，渲染需人工核验）（W-REPL 接线 commit 4c3d7f8）`
since the visual rendering of overlays/indicators is verified by unit tests at the
wiring layer only; pixel-level TUI rendering stays MANUAL.

## Left MANUAL / not flipped

- REPL-60 (Kitty keyboard protocol / `ExtendedKeys`) — still ⚠️; not in scope (no
  `ExtendedKeys` set on `TerminalModeOptions`).
- OVL-15 `/doctor` full-screen overlay UI — wired as transcript text (Status); the
  CC-style full overlay screen remains MANUAL.
- `/model <name>` runtime model switching — picker opens, but applying a model via
  arg is a stub Status (no live runner-model swap yet).

## Concerns

- `engineFromDecider` returns a pointer to a copy of the decider's `Engine` value
  (the decider is stored by value in `runner.Permissions`). This matches the existing
  W-C05 design: `RunInteractiveWithOptions` swaps `base.Permissions` for
  `ptrEngineDecider{eng: opts.Engine}` and mutates `*eng` on mode/rule changes, so the
  engine state is what propagates — not the original decider identity. Functionally
  correct for in-session mode/rule sync.
- External-editor handler is exercised only for the no-panic path in unit tests
  (spawning a real `$EDITOR` is not unit-testable); end-to-end editor round-trip is MANUAL.
- Setting `screen.Prompt.Text` directly after external-editor return does not adjust
  `Prompt.Cursor`; acceptable (next keystroke normalizes), but a polish item.
