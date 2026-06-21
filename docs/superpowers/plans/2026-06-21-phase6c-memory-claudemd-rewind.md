# Phase 6c — Memory: CLAUDE.md hierarchy + @import + rewind — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Parent doc:** `2026-06-21-00-master-roadmap.md` (§5 "Phase 6c", §6 Global Constraints, §8 gate). **Format exemplar:** `2026-06-21-interactive-runtime-phase1.md`.

**Goal:** Bring ccgo's memory subsystem to CC parity: (1) a full CLAUDE.md scope hierarchy (Managed/User/project-walk/`.claude`/`rules`/`*.local`) with correct precedence/merge; (2) `@import` resolution with a cycle guard, depth limit, and safe relative/`~` path handling; (3) a rewind/checkpoint snapshot **writer** (the transcript parser already reads `file-history-snapshot` lines but nothing emits them); (4) rewind **restore** (apply a snapshot to the working tree); (5) a `/rewind`-style entry point wired to the command/UI seam (Phase 6b / Phase 2 dependency, lands behind the seam now); (6) cost persistence + restore-on-resume (a new project-config store); (7) post-compact file restoration; (8) wiring `~/.claude/history.jsonl` prompt-history into the running path (the store already exists but has zero callers).

**Architecture:** Memory discovery and import resolution are pure functions over the filesystem in the existing `internal/memory/` package — extend `DiscoverClaudeFiles`/`LoadClaudeContext` (currently a parent-only bare-`CLAUDE.md` walk) into a layered, precedence-ordered loader, and add a new `import.go` resolver invoked during load. Rewind lives in a new `internal/rewind/` package that owns the snapshot **format** (a `file-history-snapshot` transcript line whose JSON shape matches what `internal/session`'s parser already reads), a content-addressed backup store under the session dir, a writer that appends snapshot lines via the existing `session.AppendTranscriptMessage`, and a restorer that applies a snapshot to disk. Cost persistence is a small JSON store in a new `internal/costtrack/` reading/writing `~/.claude/projects/<sanitized-cwd>/cost.json` keyed by session id (mirrors CC's `lastSessionId` guard). Post-compact restoration is a pure builder in `internal/compact/` that turns a recent-read-file set into attachment messages. History wiring connects the existing `session.BufferedHistoryWriter` to the submit path via a tiny seam. Every task is independently testable with `t.TempDir()`; **no task touches the real `~/.claude`.**

**Tech Stack:** Go 1.26; **no new third-party deps**. Existing packages: `internal/memory`, `internal/session`, `internal/compact`, `internal/config`, `internal/platform`, `internal/contracts`. New packages: `internal/rewind`, `internal/costtrack`.

---

## Global Constraints

Copied verbatim from the master roadmap §6:

- **Module/toolchain:** `ccgo`, `go 1.26` (from `go.mod`).
- **Immutability (CRITICAL):** never mutate shared structs in place; return new copies. Copy the `conversation.Runner` value per turn before setting `OnEvent`/`Tools.Asker` (existing pattern). `permissions.Engine.ApplyUpdate` already returns a **new** engine — honor that.
- **Many small files:** one responsibility per file; target 150–350 lines (800 hard max).
- **Errors handled explicitly at every level; never swallow.** Terminal raw-mode `restore` and any acquired resource MUST be released on every exit path (`defer`).
- **Input validation at boundaries:** validate all external data (API responses, user input, file content, MCP server output); fail fast with clear messages.
- **No new third-party deps** unless the plan justifies it explicitly. Phase 1 added only `golang.org/x/term`. No bubbletea/tcell/charm.
- **Non-TTY safety:** interactive paths MUST NOT call `term.MakeRaw` when stdin/stdout isn't a tty; fall back to line mode. Tests MUST NOT depend on a real tty.
- **TDD:** every task writes a failing test first, then minimal code. Commit after each task. Run package tests with `go test ./internal/<pkg>/ -run TestName -v`; full suite `go test ./...`.
- **Verify against real code, distrust roadmap docs:** every assumed type name, field, constant, or CC behavior MUST be confirmed with `go doc`/`grep` (ccgo side) or by reading `/Users/sqlrush/agent/claude-code/src` (CC side) before writing the test — flag the exact command at the point of use, as Phase 1's plan does.
- **Security:** no hardcoded secrets; tokens in keychain not plaintext (Phase 4); sandbox flag must actually enforce (Phase 7); never leak sensitive data in errors.

**Phase-6c-specific constraints:**
- **Filesystem isolation:** every filesystem test MUST use `t.TempDir()`. NEVER read or write the real `~/.claude`, `/Library/Application Support/ClaudeCode`, `/etc/claude-code`, or the developer's CLAUDE.md. Where production reads platform paths (`platform.ClaudeHomeDir`, `config.ManagedSettingsDir`), the new APIs MUST accept explicit base paths/options so tests inject temp dirs. Do NOT call the global path helpers from inside testable functions.
- **Path-traversal & cycle defense (CRITICAL):** the `@import` resolver MUST guard against import cycles (visited-set keyed by resolved absolute path), cap recursion depth, and refuse to follow imports outside an allowed root unless explicitly permitted (mirrors CC's external-include approval). Validate every resolved path; never read a file you cannot `filepath.Abs` + clean.

---

## Code-verified anchors (confirm before editing; do NOT trust this list blindly — re-run the greps)

**ccgo (current state):**
- `internal/memory/claudemd.go:14` `DiscoverClaudeFiles(cwd string) ([]ClaudeFile, error)` — walks parent dirs only, looks for one bare `CLAUDE.md` per dir (`claudemd.go:39`). **No** User/Managed/`.claude`/`rules`/`*.local` scopes. **No** `@import`.
- `internal/memory/claudemd.go:53` `LoadClaudeContext(cwd string) ([]Document, error)` — reads each discovered file verbatim into `Document`; no import expansion.
- `internal/memory/types.go:5-13` `Type` consts: `TypeProject`/`TypeUser`/`TypeTeam`/`TypeAuto`/`TypeSession`. `Document{ Header; Content string }`; `Header{ Filename, Path string; Mtime time.Time; Description string; Type Type }`.
- `internal/memory/scan.go:11-14` consts `DefaultMaxMemoryFiles=200`, `DefaultFrontmatterMaxLines=30`, `DefaultClaudeMemoryFilename="CLAUDE.md"`.
- `internal/memory/frontmatter.go:8` `ParseFrontmatter(content string) (map[string]string, string)`.
- `internal/session/transcript.go:46-67` `TranscriptMessage` struct (the JSONL line type) — fields incl. `Type`, `UUID contracts.ID`, `ParentUUID *contracts.ID`, `SessionID`, `Timestamp`, `Content any`, `Message *contracts.Message`, `CWD`.
- `internal/session/transcript.go:272-283` parser **reads** `"file-history-snapshot"` and `"attribution-snapshot"` lines into `Transcript.FileHistorySnapshots`/`FileHistoryByMessageID` (`transcript.go:36-39`). **No writer emits these** (grep confirms: writers in `append_transcript.go` only emit message + the metadata types in `sessionMetadataEntries`).
- `internal/session/transcript_metadata_fields.go:415` `parseSnapshotMessageID(line []byte) contracts.ID` — reads `messageId`/`messageID`/… from a snapshot line.
- `internal/session/append_transcript.go:14` `AppendTranscriptMessage(path string, message TranscriptMessage) error` — the one transcript-line writer.
- `internal/session/transcript_resume.go:9-41` `ResumeConversation{ Leaf, Found, Messages []contracts.Message, Chain []TranscriptMessage, … }`; `BuildResumeConversation(path, leaf)`; `BuildIndexedResumeConversation(path, leaf, maxBytes)`. **No cost field.**
- `internal/session/history.go:64-70` `LogEntry{ Display, PastedContents map[int]StoredPastedContent, Timestamp int64, Project string, SessionID contracts.ID }`; `history.go:104` `HistoryPath() = ~/.claude/history.jsonl`; `history.go:336` `AppendHistory`; `history.go:546` `AddToHistory(path, project, sessionID, entry) (bool, error)`; `BufferedHistoryWriter` (Queue/Flush). **Store exists; grep shows zero callers in `cmd/`, `internal/repl/`, `internal/bootstrap/` → not wired into the running path.**
- `internal/contracts/messages.go:620-633` `Usage{ … CostUSD float64 \`json:"cost_usd,omitempty"\` }`; `messages.go:351` `Message.Usage *Usage`. Cost is computed per call (`internal/api/anthropic/cost.go`) but **never persisted/restored**.
- `internal/compact/runner.go:40-45` `Result{ Plan; Response *anthropic.Response; Request anthropic.Request; Usage contracts.Usage }`. `internal/compact/plan.go` builds boundary+summary; **no post-compact file restoration** (grep for `PostCompact`/`readFileState`/`Attachment` in `internal/compact/` → 0 hits).
- `internal/platform/paths.go:10` `ClaudeHomeDir()` (honors `CLAUDE_CONFIG_DIR`); `paths.go:21` `ExpandPath`; `paths.go:44` `SanitizeProjectPath`.
- `internal/config/paths.go:23` `ManagedSettingsDir()` → `/Library/Application Support/ClaudeCode` (darwin) / `C:\Program Files\ClaudeCode` (windows) / `/etc/claude-code` (linux). Reuse for the Managed CLAUDE.md scope.

**CC reference (TypeScript) — behavior to replicate:**
- `src/utils/claudemd.ts:803-1007` scope loaders. **Precedence (lowest→highest, loaded so closest/most-specific wins):** Managed → User → project-walk (root→cwd, each level: `CLAUDE.md`, `.claude/CLAUDE.md`, `.claude/rules/*.md`) → Local (`CLAUDE.local.md`, root→cwd). Managed path = `<ManagedSettingsDir>/CLAUDE.md` + `<…>/.claude/rules/*.md`; User path = `~/.claude/CLAUDE.md` + `~/.claude/rules/*.md`.
- Display label strings (`claudemd.ts:1170-1177`): project `" (project instructions, checked into the codebase)"`; local `" (user's private project instructions, not checked in)"`; user `" (user's private global instructions for all projects)"`. (These mirror the labels already in this repo's CLAUDE.md preamble — match them.)
- `src/utils/claudemd.ts:459` `@import` matcher regex `/(?:^|\s)@((?:[^\s\\]|\\ )+)/g`; valid prefixes `./`, `~/`, `/…`, or bare `[A-Za-z0-9._-]` (relative). Rejects leading `[#%^&*()]`. Strips `#fragment`; unescapes `\ ` → space. Skips matches inside code blocks/spans (`claudemd.ts:496-519`). `MAX_INCLUDE_DEPTH = 5` (`claudemd.ts:537`); cycle guard via `processedPaths: Set<string>` keyed by normalized path (`claudemd.ts:629,645-648`); imported files emitted **before** the importing file (`claudemd.ts:681`).
- `src/utils/sessionStorage.ts:1090-1098` `recordFileHistorySnapshot` writes line `{ type:'file-history-snapshot', messageId, snapshot, isSnapshotUpdate }`; `snapshot = { messageId, trackedFileBackups: Record<path, { backupFileName: string|null, version: number, backupTime }>, timestamp }` (`src/types/logs.ts:188-193`). Restore: `src/utils/fileHistory.ts:347-397` finds the snapshot by `messageId`, calls `applySnapshot` to rewrite files from backups; the command layer truncates the message chain (`src/commands/rewind/rewind.ts`).
- `src/cost-tracker.ts:139-175` `saveCurrentSessionCosts` writes project config fields `lastCost`, `lastSessionId`, `lastTotalInputTokens`, … `lastModelUsage`; `src/cost-tracker.ts:87-137` `getStoredSessionCosts(sessionId)` returns stored cost **only if `lastSessionId === sessionId`** (`config.ts:76-105`).
- `src/services/compact/compact.ts:1415-1464` `createPostCompactFileAttachments(readFileState, ctx, maxFiles=5, preserved)` — recent-read files sorted by timestamp desc, skip files already in the preserved tail, cap `POST_COMPACT_MAX_FILES_TO_RESTORE=5`, `POST_COMPACT_TOKEN_BUDGET=50_000`, `POST_COMPACT_MAX_TOKENS_PER_FILE=5_000` (`compact.ts:122-124`); re-read with the file tool; return `AttachmentMessage[]`.
- `src/history.ts:219-225` `LogEntry{ display, pastedContents, timestamp, project, sessionId? }` at `~/.claude/history.jsonl` (`history.ts:115`); writer `addToHistory` (`history.ts:411`); skip when `CLAUDE_CODE_SKIP_PROMPT_HISTORY=true` (`history.ts:414`). **ccgo already matches this record shape** — this phase only wires it in.

---

## File Structure

**`internal/memory/` (extend):**
- `scopes.go` — `Scope` enum + `ScopeOptions` (injectable base dirs); `DiscoverScopedClaudeFiles(opts) ([]ClaudeFile, error)` building the full precedence-ordered list. (new)
- `import.go` — `@import` matcher + `ResolveImports(doc Document, opts ImportOptions) ([]Document, error)` with cycle guard + depth cap + path validation. (new)
- `claudemd.go` — keep `DiscoverClaudeFiles`/`LoadClaudeContext` (back-compat), add `LoadScopedClaudeContext(opts)` that discovers scopes then expands imports. (modify)
- `types.go` — add `Scope`-related fields to `ClaudeFile`/`Header` if needed (a `Scope`/`Label` field). (modify)

**`internal/rewind/` (new package):**
- `snapshot.go` — `Snapshot`/`FileBackup`/`TrackedFileBackups` types + the `file-history-snapshot` transcript-line shape; `SnapshotLine(...)` builder.
- `backup_store.go` — content-addressed backup store under `<sessionDir>/file-history/`; `Capture(paths) (Snapshot, error)`.
- `writer.go` — `Writer.Record(transcriptPath, snapshot, isUpdate) error` (appends via `session.AppendTranscriptMessage`).
- `restore.go` — `Restore(snapshot, store) (changed []string, err error)` applies a snapshot to disk; `Rewind(transcriptPath, messageID) (Result, error)` ties read→restore→chain-truncation point.

**`internal/costtrack/` (new package):**
- `store.go` — `ProjectCost` JSON shape (`LastCost`, `LastSessionID`, token totals); `Save(opts, cost) error`; `Restore(opts, sessionID) (ProjectCost, bool, error)` with the `lastSessionId` guard.

**`internal/compact/` (extend):**
- `postcompact.go` — `BuildPostCompactAttachments(readFiles []ReadFileEntry, opts) []contracts.Message` pure builder. (new)

**Seam (wired in Task 8, behind interfaces — does not require Phase 2/6b UI):**
- `internal/memory/claudemd.go` `LoadScopedClaudeContext` is callable by bootstrap; rewind/history/cost expose plain functions the REPL/commands call once those land.

---

## Task 1: Full CLAUDE.md scope hierarchy (Managed/User/project-walk/.claude/rules/*.local)

**Files:**
- Create: `internal/memory/scopes.go`
- Modify: `internal/memory/types.go` (add `Scope` + `Label` to `ClaudeFile`)
- Test: `internal/memory/scopes_test.go`

**Pre-flight verification (run first):**
```bash
cd /Users/sqlrush/ccgo
go doc ./internal/memory ClaudeFile          # confirm fields Path/Root/Depth
go doc ./internal/memory Type                # confirm TypeUser/TypeProject exist
grep -n "DefaultClaudeMemoryFilename" internal/memory/scan.go   # = "CLAUDE.md"
go doc ./internal/config ManagedSettingsDir  # confirm signature () string
go doc ./internal/platform ClaudeHomeDir     # confirm signature () string
```

**Interfaces produced:**
- `type Scope string` with `ScopeManaged/ScopeUser/ScopeProject/ScopeLocal` (string values `"managed"`,`"user"`,`"project"`,`"local"`).
- `type ScopeOptions struct { CWD, ManagedDir, UserDir string }` — all injectable so tests use temp dirs (no global path calls inside the testable function).
- `func DefaultScopeOptions(cwd string) ScopeOptions` — fills `ManagedDir`/`UserDir` from `config.ManagedSettingsDir()`/`platform.ClaudeHomeDir()` (the ONLY place those globals are read).
- `func DiscoverScopedClaudeFiles(opts ScopeOptions) ([]ClaudeFile, error)` — ordered lowest→highest precedence.

**Precedence (lowest first; later entries override earlier on merge):** Managed `CLAUDE.md` → Managed `.claude/rules/*.md` (sorted) → User `CLAUDE.md` → User `rules/*.md` (sorted) → for each dir root→cwd: `CLAUDE.md`, `.claude/CLAUDE.md`, `.claude/rules/*.md` (sorted) → for each dir root→cwd: `CLAUDE.local.md`.

- [ ] **Step 1: Write the failing test**

Create `internal/memory/scopes_test.go`:
```go
package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverScopedClaudeFilesPrecedence(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, "managed")
	user := filepath.Join(root, "user")
	proj := filepath.Join(root, "proj")
	sub := filepath.Join(proj, "a", "b")

	writeFile(t, filepath.Join(managed, "CLAUDE.md"), "managed")
	writeFile(t, filepath.Join(managed, ".claude", "rules", "policy.md"), "managed-rule")
	writeFile(t, filepath.Join(user, "CLAUDE.md"), "user")
	writeFile(t, filepath.Join(user, "rules", "style.md"), "user-rule")
	writeFile(t, filepath.Join(proj, "CLAUDE.md"), "proj-root")
	writeFile(t, filepath.Join(proj, ".claude", "CLAUDE.md"), "proj-dotclaude")
	writeFile(t, filepath.Join(proj, ".claude", "rules", "team.md"), "proj-rule")
	writeFile(t, filepath.Join(sub, "CLAUDE.md"), "proj-sub")
	writeFile(t, filepath.Join(proj, "CLAUDE.local.md"), "local-root")

	opts := ScopeOptions{CWD: sub, ManagedDir: managed, UserDir: user}
	files, err := DiscoverScopedClaudeFiles(opts)
	if err != nil {
		t.Fatal(err)
	}

	// Build path->scope index for assertions.
	got := map[string]Scope{}
	var order []string
	for _, f := range files {
		got[filepath.Base(filepath.Dir(f.Path))+"/"+filepath.Base(f.Path)] = f.Scope
		order = append(order, f.Path)
	}

	if s := got["managed/CLAUDE.md"]; s != ScopeManaged {
		t.Fatalf("managed CLAUDE.md scope = %q want managed", s)
	}
	if s := got["user/CLAUDE.md"]; s != ScopeUser {
		t.Fatalf("user CLAUDE.md scope = %q want user", s)
	}

	idx := func(suffix string) int {
		for i, p := range order {
			if filepath.Base(p) == suffix && containsDir(p, suffix == "CLAUDE.local.md") {
				return i
			}
		}
		return -1
	}
	_ = idx
	// Managed must come before User, User before any project file, project before local.
	pos := func(want string) int {
		for i, p := range order {
			if p == want {
				return i
			}
		}
		t.Fatalf("expected discovered file %s; got order %v", want, order)
		return -1
	}
	managedRoot := filepath.Join(managed, "CLAUDE.md")
	userRoot := filepath.Join(user, "CLAUDE.md")
	projRoot := filepath.Join(proj, "CLAUDE.md")
	projSub := filepath.Join(sub, "CLAUDE.md")
	localRoot := filepath.Join(proj, "CLAUDE.local.md")
	if !(pos(managedRoot) < pos(userRoot) &&
		pos(userRoot) < pos(projRoot) &&
		pos(projRoot) < pos(projSub) &&
		pos(projSub) < pos(localRoot)) {
		t.Fatalf("precedence order wrong: %v", order)
	}
}

func containsDir(string, bool) bool { return true } // placeholder helper; remove if unused
```
(If `containsDir`/`idx` go unused, delete them — they exist only to keep the assertion compiling during drafting. The load-bearing asserts are the `pos(...)` ordering checks.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -run TestDiscoverScopedClaudeFiles -v`
Expected: FAIL — `undefined: ScopeOptions` / `undefined: DiscoverScopedClaudeFiles`.

- [ ] **Step 3: Write minimal implementation**

In `internal/memory/types.go`, add `Scope` fields to `ClaudeFile` (currently in `claudemd.go:8`; move or extend). Add to the `ClaudeFile` struct:
```go
// add to existing ClaudeFile in claudemd.go
type ClaudeFile struct {
	Path  string
	Root  string
	Depth int
	Scope Scope
	Label string
}
```

Create `internal/memory/scopes.go`:
```go
package memory

import (
	"os"
	"path/filepath"
	"sort"
)

type Scope string

const (
	ScopeManaged Scope = "managed"
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopeLocal   Scope = "local"
)

// Display labels mirror CC (claudemd.ts:1170-1177).
const (
	labelProject = " (project instructions, checked into the codebase)"
	labelLocal   = " (user's private project instructions, not checked in)"
	labelUser    = " (user's private global instructions for all projects)"
	labelManaged = " (managed policy instructions for all projects)"
)

const localClaudeFilename = "CLAUDE.local.md"

// ScopeOptions injects every base directory so tests never read real paths.
type ScopeOptions struct {
	CWD        string
	ManagedDir string
	UserDir    string
}

// DiscoverScopedClaudeFiles returns CLAUDE.md sources lowest→highest precedence.
func DiscoverScopedClaudeFiles(opts ScopeOptions) ([]ClaudeFile, error) {
	if opts.CWD == "" {
		var err error
		if opts.CWD, err = os.Getwd(); err != nil {
			return nil, err
		}
	}
	cwd, err := filepath.Abs(opts.CWD)
	if err != nil {
		return nil, err
	}

	var out []ClaudeFile
	add := func(path string, scope Scope, label string) {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			out = append(out, ClaudeFile{Path: path, Root: filepath.Dir(path), Scope: scope, Label: label})
		}
	}
	addRules := func(dir string, scope Scope, label string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		var names []string
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			out = append(out, ClaudeFile{Path: filepath.Join(dir, n), Root: dir, Scope: scope, Label: label})
		}
	}

	// 1. Managed (lowest precedence).
	if opts.ManagedDir != "" {
		add(filepath.Join(opts.ManagedDir, DefaultClaudeMemoryFilename), ScopeManaged, labelManaged)
		addRules(filepath.Join(opts.ManagedDir, ".claude", "rules"), ScopeManaged, labelManaged)
	}
	// 2. User.
	if opts.UserDir != "" {
		add(filepath.Join(opts.UserDir, DefaultClaudeMemoryFilename), ScopeUser, labelUser)
		addRules(filepath.Join(opts.UserDir, "rules"), ScopeUser, labelUser)
	}

	// Directory chain root→cwd.
	dirs := ancestorDirsRootFirst(cwd)

	// 3. Project: each dir's CLAUDE.md, .claude/CLAUDE.md, .claude/rules/*.md.
	for i, dir := range dirs {
		add(filepath.Join(dir, DefaultClaudeMemoryFilename), ScopeProject, labelProject)
		add(filepath.Join(dir, ".claude", DefaultClaudeMemoryFilename), ScopeProject, labelProject)
		addRules(filepath.Join(dir, ".claude", "rules"), ScopeProject, labelProject)
		_ = i
	}
	// 4. Local: each dir's CLAUDE.local.md (highest precedence).
	for _, dir := range dirs {
		add(filepath.Join(dir, localClaudeFilename), ScopeLocal, labelLocal)
	}
	return out, nil
}

// ancestorDirsRootFirst returns dirs from filesystem root down to cwd.
func ancestorDirsRootFirst(cwd string) []string {
	var dirs []string
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}

// DefaultScopeOptions reads the real platform paths. Keep this the ONLY caller
// of the global path helpers so tests can inject ScopeOptions directly.
func DefaultScopeOptions(cwd string) ScopeOptions {
	return ScopeOptions{CWD: cwd, ManagedDir: defaultManagedDir(), UserDir: defaultUserDir()}
}
```

Add `internal/memory/scopes_paths.go` (isolates global-path reads so `scopes.go` stays pure/testable):
```go
package memory

import (
	"ccgo/internal/config"
	"ccgo/internal/platform"
)

func defaultManagedDir() string { return config.ManagedSettingsDir() }
func defaultUserDir() string    { return platform.ClaudeHomeDir() }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/memory/ -run TestDiscoverScopedClaudeFiles -v && go test ./internal/memory/ -v`
Expected: PASS, including pre-existing memory tests (the legacy `DiscoverClaudeFiles` is untouched).

- [ ] **Step 5: Commit**
```bash
git add internal/memory/scopes.go internal/memory/scopes_paths.go internal/memory/claudemd.go internal/memory/scopes_test.go
git commit -m "feat(memory): full CLAUDE.md scope hierarchy with precedence and labels"
```

---

## Task 2: @import resolution with cycle guard, depth cap, and safe paths

**Files:**
- Create: `internal/memory/import.go`
- Test: `internal/memory/import_test.go`

**Pre-flight verification:**
```bash
cd /Users/sqlrush/ccgo
go doc ./internal/platform ExpandPath        # confirm ~ expansion helper
grep -n "func ParseFrontmatter" internal/memory/frontmatter.go
```
And read CC's matcher to replicate byte-for-byte: `sed -n '455,540p' /Users/sqlrush/agent/claude-code/src/utils/claudemd.ts` — confirm the regex `/(?:^|\s)@((?:[^\s\\]|\\ )+)/g`, the valid-prefix set, `#fragment` stripping, `\ ` unescape, and `MAX_INCLUDE_DEPTH = 5`.

**Interfaces produced:**
- `type ImportOptions struct { BaseDir string; HomeDir string; MaxDepth int; AllowExternal bool; AllowedRoot string }`.
- `func extractImports(content string) []string` — pure; returns the raw import targets in order, skipping code spans/blocks.
- `func ResolveImports(doc Document, opts ImportOptions) ([]Document, error)` — returns imported docs **before** the host doc (CC order), de-duped, cycle-safe, depth-capped.

**Validation rules (fail closed):** resolve each target relative to the importing file's dir (or `HomeDir` for `~/`); reject empty, reject paths whose cleaned absolute form escapes `AllowedRoot` when `AllowExternal=false`; skip already-visited absolute paths; stop at `MaxDepth` (default 5).

- [ ] **Step 1: Write the failing test**

Create `internal/memory/import_test.go`:
```go
package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func docFor(t *testing.T, path, content string) Document {
	t.Helper()
	writeFile(t, path, content)
	return Document{Header: Header{Path: path, Filename: filepath.Base(path)}, Content: content, }
}

func TestExtractImports(t *testing.T) {
	content := "intro\n@./a.md and @~/b.md plus @/abs/c.md\n```\n@./inside-code.md\n```\nemail@example.com not an import\n"
	got := extractImports(content)
	want := []string{"./a.md", "~/b.md", "/abs/c.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("extractImports = %v want %v", got, want)
	}
}

func TestResolveImportsRecursiveAndCycle(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "CLAUDE.md")
	a := filepath.Join(root, "a.md")
	b := filepath.Join(root, "b.md")
	writeFile(t, a, "A body\n@./b.md\n")
	writeFile(t, b, "B body\n@./a.md\n") // cycle back to a
	doc := docFor(t, main, "Main body\n@./a.md\n")

	opts := ImportOptions{BaseDir: root, AllowedRoot: root, MaxDepth: 5}
	imported, err := ResolveImports(doc, opts)
	if err != nil {
		t.Fatalf("ResolveImports err: %v", err)
	}
	// a and b each appear exactly once; cycle did not loop forever.
	var paths []string
	for _, d := range imported {
		paths = append(paths, filepath.Base(d.Path))
	}
	joined := strings.Join(paths, ",")
	if strings.Count(joined, "a.md") != 1 || strings.Count(joined, "b.md") != 1 {
		t.Fatalf("expected a.md and b.md once each; got %v", paths)
	}
}

func TestResolveImportsBlocksTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.md")
	writeFile(t, outside, "secret")
	doc := docFor(t, filepath.Join(root, "CLAUDE.md"), "@"+outside+"\n")

	opts := ImportOptions{BaseDir: root, AllowedRoot: root, MaxDepth: 5, AllowExternal: false}
	imported, err := ResolveImports(doc, opts)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(imported) != 0 {
		t.Fatalf("expected traversal import to be skipped; got %v", imported)
	}
}

func TestResolveImportsDepthCap(t *testing.T) {
	root := t.TempDir()
	// chain c0 -> c1 -> ... -> c10
	for i := 0; i < 11; i++ {
		next := ""
		if i < 10 {
			next = "@./c" + itoa(i+1) + ".md\n"
		}
		writeFile(t, filepath.Join(root, "c"+itoa(i)+".md"), "level "+itoa(i)+"\n"+next)
	}
	doc := Document{Header: Header{Path: filepath.Join(root, "c0.md")}, Content: "@./c1.md\n"}
	opts := ImportOptions{BaseDir: root, AllowedRoot: root, MaxDepth: 5}
	imported, err := ResolveImports(doc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) > 5 {
		t.Fatalf("depth cap not honored: %d imported docs", len(imported))
	}
	_ = time.Now
}

func itoa(i int) string { return strings.TrimSpace(string(rune('0'+i))) } // single-digit helper; for i<10
```
(Note: `itoa` is a single-digit shim for the test; if a level index reaches double digits, replace with `strconv.Itoa`. Confirm `strconv` import if you switch.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -run 'TestExtractImports|TestResolveImports' -v`
Expected: FAIL — `undefined: extractImports` / `undefined: ResolveImports`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/memory/import.go`:
```go
package memory

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultMaxImportDepth = 5

// importPattern mirrors CC's claudemd.ts:459 — @ at start-of-line or after
// whitespace, capturing a path that may contain escaped spaces.
var importPattern = regexp.MustCompile(`(?:^|\s)@((?:[^\s\\]|\\ )+)`)

// fencePattern toggles fenced code blocks so imports inside them are ignored.
var fencePattern = regexp.MustCompile("^\\s*```")

type ImportOptions struct {
	BaseDir       string // dir of the importing file (relative-path root)
	HomeDir       string // expansion root for ~/ (empty => os.UserHomeDir)
	MaxDepth      int
	AllowExternal bool
	AllowedRoot   string // imports must stay within this root unless AllowExternal
}

// extractImports returns import targets in source order, skipping fenced code
// blocks and inline code spans. It does NOT resolve them.
func extractImports(content string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		if fencePattern.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		line = stripInlineCode(line)
		for _, m := range importPattern.FindAllStringSubmatch(line, -1) {
			target := strings.ReplaceAll(m[1], `\ `, " ")
			if i := strings.IndexByte(target, '#'); i >= 0 {
				target = target[:i]
			}
			if isImportTarget(target) {
				out = append(out, target)
			}
		}
	}
	return out
}

func stripInlineCode(line string) string {
	for {
		i := strings.IndexByte(line, '`')
		if i < 0 {
			return line
		}
		j := strings.IndexByte(line[i+1:], '`')
		if j < 0 {
			return line[:i]
		}
		line = line[:i] + " " + line[i+1+j+1:]
	}
}

func isImportTarget(p string) bool {
	if p == "" || p == "/" {
		return false
	}
	switch {
	case strings.HasPrefix(p, "./"), strings.HasPrefix(p, "~/"), strings.HasPrefix(p, "/"):
		return true
	}
	c := p[0]
	return c == '.' || c == '_' || c == '-' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// ResolveImports returns imported documents (recursively) ordered before the
// host doc, de-duped, cycle-safe, depth-capped, and path-validated.
func ResolveImports(doc Document, opts ImportOptions) ([]Document, error) {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxImportDepth
	}
	visited := map[string]bool{}
	if doc.Path != "" {
		if abs, err := filepath.Abs(doc.Path); err == nil {
			visited[abs] = true
		}
	}
	var out []Document
	err := resolveInto(doc.Content, opts, 0, visited, &out)
	return out, err
}

func resolveInto(content string, opts ImportOptions, depth int, visited map[string]bool, out *[]Document) error {
	if depth >= opts.MaxDepth {
		return nil
	}
	for _, target := range extractImports(content) {
		abs, ok := resolveImportPath(target, opts)
		if !ok || visited[abs] {
			continue
		}
		visited[abs] = true
		data, err := os.ReadFile(abs)
		if err != nil {
			continue // missing import: skip, do not fail the whole load
		}
		body := string(data)
		// Imports inside this file resolve relative to ITS directory.
		childOpts := opts
		childOpts.BaseDir = filepath.Dir(abs)
		if err := resolveInto(body, childOpts, depth+1, visited, out); err != nil {
			return err
		}
		*out = append(*out, Document{
			Header:  Header{Path: abs, Filename: filepath.Base(abs), Type: TypeProject},
			Content: body,
		})
	}
	return nil
}

func resolveImportPath(target string, opts ImportOptions) (string, bool) {
	var raw string
	switch {
	case strings.HasPrefix(target, "~/"):
		home := opts.HomeDir
		if home == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return "", false
			}
			home = h
		}
		raw = filepath.Join(home, target[2:])
	case filepath.IsAbs(target):
		raw = target
	default:
		raw = filepath.Join(opts.BaseDir, target)
	}
	abs, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		return "", false
	}
	if !opts.AllowExternal && opts.AllowedRoot != "" {
		root, err := filepath.Abs(opts.AllowedRoot)
		if err != nil {
			return "", false
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
	}
	return abs, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/memory/ -run 'TestExtractImports|TestResolveImports' -v && go vet ./internal/memory/`
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**
```bash
git add internal/memory/import.go internal/memory/import_test.go
git commit -m "feat(memory): @import resolution with cycle guard, depth cap, traversal defense"
```

---

## Task 3: Wire scopes + imports into a single scoped loader

**Files:**
- Modify: `internal/memory/claudemd.go` (add `LoadScopedClaudeContext`)
- Test: `internal/memory/claudemd_scoped_test.go`

**Interfaces produced:**
- `type LoadOptions struct { Scope ScopeOptions; Import ImportOptions }`.
- `func LoadScopedClaudeContext(opts LoadOptions) ([]Document, error)` — discovers scoped files (Task 1), reads each, expands its imports (Task 2, imported docs placed immediately before the host doc), returns the precedence-ordered list with `Header.Type`/`Header.Description` reflecting the scope label.

- [ ] **Step 1: Write the failing test**

Create `internal/memory/claudemd_scoped_test.go`:
```go
package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadScopedClaudeContextExpandsImports(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "user")
	proj := filepath.Join(root, "proj")
	writeFile(t, filepath.Join(user, "CLAUDE.md"), "user-global\n")
	writeFile(t, filepath.Join(proj, "shared.md"), "SHARED-CONTENT\n")
	writeFile(t, filepath.Join(proj, "CLAUDE.md"), "proj-root\n@./shared.md\n")

	opts := LoadOptions{
		Scope:  ScopeOptions{CWD: proj, UserDir: user},
		Import: ImportOptions{AllowedRoot: root, MaxDepth: 5},
	}
	docs, err := LoadScopedClaudeContext(opts)
	if err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, d := range docs {
		seq = append(seq, strings.TrimSpace(d.Content))
	}
	joined := strings.Join(seq, "|")
	// user before project; imported shared.md appears immediately before its host.
	if !strings.Contains(joined, "user-global") {
		t.Fatalf("missing user scope: %v", seq)
	}
	si, pi := indexOf(seq, "SHARED-CONTENT"), indexOf(seq, "proj-root")
	ui := indexOf(seq, "user-global")
	if !(ui < pi && si >= 0 && si < pi) {
		t.Fatalf("ordering wrong (user<proj, import<host): %v", seq)
	}
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -run TestLoadScopedClaudeContext -v`
Expected: FAIL — `undefined: LoadOptions` / `undefined: LoadScopedClaudeContext`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/memory/claudemd.go`:
```go
type LoadOptions struct {
	Scope  ScopeOptions
	Import ImportOptions
}

// LoadScopedClaudeContext discovers all scoped CLAUDE.md sources (precedence
// ordered) and expands @imports, placing imported docs before each host doc.
func LoadScopedClaudeContext(opts LoadOptions) ([]Document, error) {
	files, err := DiscoverScopedClaudeFiles(opts.Scope)
	if err != nil {
		return nil, err
	}
	var docs []Document
	for _, f := range files {
		info, err := os.Stat(f.Path)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		host := Document{
			Header: Header{
				Filename:    filepath.Base(f.Path),
				Path:        f.Path,
				Mtime:       info.ModTime(),
				Description: f.Label,
				Type:        scopeToType(f.Scope),
			},
			Content: string(data),
		}
		imp := opts.Import
		imp.BaseDir = filepath.Dir(f.Path)
		if imp.AllowedRoot == "" {
			imp.AllowedRoot = filepath.Dir(f.Path)
		}
		imported, err := ResolveImports(host, imp)
		if err != nil {
			return nil, err
		}
		docs = append(docs, imported...)
		docs = append(docs, host)
	}
	return docs, nil
}

func scopeToType(s Scope) Type {
	switch s {
	case ScopeUser, ScopeManaged:
		return TypeUser
	default:
		return TypeProject
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/memory/ -v && go build ./...`
Expected: PASS, build clean.

- [ ] **Step 5: Commit**
```bash
git add internal/memory/claudemd.go internal/memory/claudemd_scoped_test.go
git commit -m "feat(memory): scoped loader merging hierarchy with @import expansion"
```

---

## Task 4: Rewind snapshot format + content-addressed backup store + writer

**Files:**
- Create: `internal/rewind/snapshot.go`
- Create: `internal/rewind/backup_store.go`
- Create: `internal/rewind/writer.go`
- Test: `internal/rewind/snapshot_test.go`, `internal/rewind/writer_test.go`

**Pre-flight verification:**
```bash
cd /Users/sqlrush/ccgo
go doc ./internal/session TranscriptMessage      # confirm Type/UUID/Message/Content fields
go doc ./internal/session AppendTranscriptMessage
grep -n "file-history-snapshot" internal/session/transcript.go         # parser reads it
sed -n '/func parseSnapshotMessageID/,/^}/p' internal/session/transcript_metadata_fields.go  # accepted id keys
```
And confirm CC's snapshot shape: `sed -n '185,196p' /Users/sqlrush/agent/claude-code/src/types/logs.ts` and `sed -n '1088,1100p' /Users/sqlrush/agent/claude-code/src/utils/sessionStorage.ts`.

**Snapshot format (must round-trip through `session.LoadTranscript`'s `file-history-snapshot` parser, which keys by `messageId`):**
```jsonc
{
  "type": "file-history-snapshot",
  "messageId": "<uuid>",
  "isSnapshotUpdate": false,
  "snapshot": {
    "messageId": "<uuid>",
    "timestamp": "<RFC3339Nano>",
    "trackedFileBackups": {
      "/abs/path.go": { "backupFileName": "<sha>@v1", "version": 1, "backupTime": "<RFC3339Nano>" }
    }
  }
}
```

**Interfaces produced:**
- `type FileBackup struct { BackupFileName string \`json:"backupFileName"\`; Version int \`json:"version"\`; BackupTime string \`json:"backupTime"\` }`.
- `type Snapshot struct { MessageID contracts.ID \`json:"messageId"\`; Timestamp string \`json:"timestamp"\`; TrackedFileBackups map[string]FileBackup \`json:"trackedFileBackups"\` }`.
- `type snapshotLine struct { Type string; MessageID contracts.ID; IsSnapshotUpdate bool; Snapshot Snapshot }` (json tags `type`,`messageId`,`isSnapshotUpdate`,`snapshot`).
- `func SnapshotTranscriptMessage(snap Snapshot, isUpdate bool) session.TranscriptMessage` — builds the line via `Content` so the existing writer/parser handle it.
- `type Store struct { Dir string }`; `func NewStore(sessionDir string) Store`; `func (s Store) Capture(messageID contracts.ID, paths []string, now time.Time) (Snapshot, error)` — copies each path's bytes into `Dir/<sha256>@v<n>`, returns the snapshot.
- `type Writer struct { TranscriptPath string }`; `func (w Writer) Record(snap Snapshot, isUpdate bool) error` — appends via `session.AppendTranscriptMessage`.

- [ ] **Step 1: Write the failing test**

Create `internal/rewind/snapshot_test.go`:
```go
package rewind

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/session"
)

func TestCaptureAndSnapshotLineRoundTrips(t *testing.T) {
	work := t.TempDir()
	src := filepath.Join(work, "a.go")
	if err := os.WriteFile(src, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(work, ".snap"))
	snap, err := store.Capture("m1", []string{src}, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := snap.TrackedFileBackups[src]; !ok || b.BackupFileName == "" || b.Version != 1 {
		t.Fatalf("bad backup entry: %+v", snap.TrackedFileBackups)
	}
	// Backup file actually written with original bytes.
	bk := filepath.Join(store.Dir, snap.TrackedFileBackups[src].BackupFileName)
	if data, err := os.ReadFile(bk); err != nil || string(data) != "package a\n" {
		t.Fatalf("backup content = %q,%v", data, err)
	}

	// Build a transcript line and confirm it parses as file-history-snapshot.
	msg := SnapshotTranscriptMessage(snap, false)
	if msg.Type != "file-history-snapshot" {
		t.Fatalf("type = %q want file-history-snapshot", msg.Type)
	}
	encoded, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	tp := filepath.Join(work, "session.jsonl")
	if err := os.WriteFile(tp, append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := session.LoadTranscript(tp)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.FileHistorySnapshots) != 1 {
		t.Fatalf("parser saw %d snapshots want 1", len(tr.FileHistorySnapshots))
	}
	if _, ok := tr.FileHistoryByMessageID["m1"]; !ok {
		t.Fatalf("snapshot not keyed by messageId m1: %v", tr.FileHistoryByMessageID)
	}
}
```

Create `internal/rewind/writer_test.go`:
```go
package rewind

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/session"
)

func TestWriterAppendsParsableSnapshot(t *testing.T) {
	work := t.TempDir()
	src := filepath.Join(work, "f.txt")
	_ = os.WriteFile(src, []byte("hi"), 0o644)
	store := NewStore(filepath.Join(work, ".snap"))
	snap, err := store.Capture("mX", []string{src}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	tp := filepath.Join(work, "s.jsonl")
	w := Writer{TranscriptPath: tp}
	if err := w.Record(snap, false); err != nil {
		t.Fatal(err)
	}
	tr, err := session.LoadTranscript(tp)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.FileHistoryByMessageID["mX"]; !ok {
		t.Fatal("written snapshot not found by parser")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rewind/ -v`
Expected: FAIL — package/`NewStore`/`SnapshotTranscriptMessage`/`Writer` undefined.

**CRITICAL pre-impl check:** confirm how `session`'s parser reads the snapshot. The parser only stores the raw line into `FileHistorySnapshots` and indexes by `parseSnapshotMessageID` (top-level `messageId`/`uuid`/…). So the transcript line MUST carry the snapshot as a top-level field readable by the parser — verify the parser does NOT require the line to also be a `TranscriptMessage` "type" that the *message* switch handles. Re-read `sed -n '195,300p' internal/session/transcript.go` to confirm the `metadataType == "file-history-snapshot"` branch fires off the line's top-level `type` field (it does, via `normalizeTranscriptMetadataType`). The `TranscriptMessage` JSON tag for `Type` is `"type"` and for the snapshot payload we use `Content` (tag `content`) — but the parser keys `messageId` at top level, so ALSO set `TranscriptMessage.UUID` (tag `uuid`) to the messageId so `parseSnapshotMessageID`'s fallback `uuid` key resolves. If the parser needs the literal `messageId` key (not `uuid`), marshal a custom line struct in `SnapshotTranscriptMessage` instead of `TranscriptMessage`. Decide based on the grep, do not guess.

- [ ] **Step 3: Write minimal implementation**

Create `internal/rewind/snapshot.go`:
```go
package rewind

import (
	"encoding/json"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

const SnapshotType = "file-history-snapshot"

type FileBackup struct {
	BackupFileName string `json:"backupFileName"`
	Version        int    `json:"version"`
	BackupTime     string `json:"backupTime"`
}

type Snapshot struct {
	MessageID          contracts.ID          `json:"messageId"`
	Timestamp          string                `json:"timestamp"`
	TrackedFileBackups map[string]FileBackup `json:"trackedFileBackups"`
}

// snapshotLine is the exact JSONL shape (CC sessionStorage.ts:1090).
type snapshotLine struct {
	Type             string       `json:"type"`
	MessageID        contracts.ID `json:"messageId"`
	UUID             contracts.ID `json:"uuid"`
	IsSnapshotUpdate bool         `json:"isSnapshotUpdate"`
	Snapshot         Snapshot     `json:"snapshot"`
}

// SnapshotTranscriptMessage builds a TranscriptMessage whose marshaled form is
// the file-history-snapshot line the session parser already reads. We marshal
// the canonical line into Raw via Content so messageId stays top-level.
func SnapshotTranscriptMessage(snap Snapshot, isUpdate bool) session.TranscriptMessage {
	line := snapshotLine{
		Type:             SnapshotType,
		MessageID:        snap.MessageID,
		UUID:             snap.MessageID,
		IsSnapshotUpdate: isUpdate,
		Snapshot:         snap,
	}
	// Embed as Content so AppendTranscriptMessage emits these fields; Type/UUID
	// remain top-level for the parser's messageId resolution.
	payload, _ := json.Marshal(snap)
	return session.TranscriptMessage{
		Type:    SnapshotType,
		UUID:    snap.MessageID,
		Content: json.RawMessage(payload),
	}
}
```

> Implementer note: if the round-trip test shows `session.TranscriptMessage` cannot reproduce the exact `snapshot`/`isSnapshotUpdate` keys (because `TranscriptMessage` has no such fields), have `Writer.Record` marshal `snapshotLine` directly and append the raw bytes (open the file `O_APPEND`, write `json.Marshal(line)+"\n"`) instead of going through `AppendTranscriptMessage`. The parser only needs a valid JSON line whose top-level `type` normalizes to `file-history-snapshot` and that carries `messageId`. Choose the path the failing test dictates; both are acceptable. Keep `Writer.Record` ≤ 30 lines.

Create `internal/rewind/backup_store.go`:
```go
package rewind

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ccgo/internal/contracts"
)

type Store struct {
	Dir string
}

func NewStore(sessionDir string) Store {
	return Store{Dir: filepath.Join(sessionDir, "file-history")}
}

// Capture copies the current bytes of each path into a content-addressed backup
// and returns a Snapshot. Missing files are recorded with a nil backup name
// (deletion sentinel), matching CC's backupFileName: null.
func (s Store) Capture(messageID contracts.ID, paths []string, now time.Time) (Snapshot, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return Snapshot{}, fmt.Errorf("rewind: mkdir backup dir: %w", err)
	}
	snap := Snapshot{
		MessageID:          messageID,
		Timestamp:          now.UTC().Format(time.RFC3339Nano),
		TrackedFileBackups: map[string]FileBackup{},
	}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return Snapshot{}, fmt.Errorf("rewind: abs %q: %w", p, err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			snap.TrackedFileBackups[abs] = FileBackup{Version: 1, BackupTime: snap.Timestamp}
			continue
		}
		sum := sha256.Sum256(data)
		name := hex.EncodeToString(sum[:]) + "@v1"
		if err := os.WriteFile(filepath.Join(s.Dir, name), data, 0o600); err != nil {
			return Snapshot{}, fmt.Errorf("rewind: write backup: %w", err)
		}
		snap.TrackedFileBackups[abs] = FileBackup{BackupFileName: name, Version: 1, BackupTime: snap.Timestamp}
	}
	return snap, nil
}
```

Create `internal/rewind/writer.go`:
```go
package rewind

import (
	"ccgo/internal/session"
)

type Writer struct {
	TranscriptPath string
}

// Record appends the snapshot as a file-history-snapshot transcript line.
func (w Writer) Record(snap Snapshot, isUpdate bool) error {
	msg := SnapshotTranscriptMessage(snap, isUpdate)
	return session.AppendTranscriptMessage(w.TranscriptPath, msg)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rewind/ -v && go vet ./internal/rewind/`
Expected: PASS. If the round-trip fails on the snapshot payload, switch `Writer.Record` to marshal `snapshotLine` directly per the implementer note (and update `SnapshotTranscriptMessage`/`Writer` accordingly), then re-run.

- [ ] **Step 5: Commit**
```bash
git add internal/rewind/snapshot.go internal/rewind/backup_store.go internal/rewind/writer.go internal/rewind/snapshot_test.go internal/rewind/writer_test.go
git commit -m "feat(rewind): file-history snapshot format, backup store, and writer"
```

---

## Task 5: Rewind restore — apply a snapshot to the working tree

**Files:**
- Create: `internal/rewind/restore.go`
- Test: `internal/rewind/restore_test.go`

**Interfaces produced:**
- `func Restore(snap Snapshot, store Store) (changed []string, err error)` — for each tracked path: if `BackupFileName==""`, delete the file (it didn't exist at snapshot time); else rewrite the file with the backup bytes. Returns the list of changed paths. Validates each path is absolute; never writes outside a recorded path.
- `type RewindResult struct { Snapshot Snapshot; Changed []string; MessageID contracts.ID }`.
- `func Rewind(transcriptPath string, messageID contracts.ID, store Store) (RewindResult, error)` — loads the transcript, finds the snapshot indexed by `messageID` (`session.Transcript.FileHistoryByMessageID`), unmarshals it, and applies it. (The message-chain truncation point is returned via `MessageID` for the command layer in Task 6.)

- [ ] **Step 1: Write the failing test**

Create `internal/rewind/restore_test.go`:
```go
package rewind

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreRewritesAndDeletes(t *testing.T) {
	work := t.TempDir()
	keep := filepath.Join(work, "keep.txt")
	created := filepath.Join(work, "created.txt") // absent at snapshot time
	_ = os.WriteFile(keep, []byte("v1"), 0o644)

	store := NewStore(filepath.Join(work, ".snap"))
	snap, err := store.Capture("m1", []string{keep, created}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the tree after the snapshot.
	_ = os.WriteFile(keep, []byte("v2-modified"), 0o644)
	_ = os.WriteFile(created, []byte("new file"), 0o644)

	changed, err := Restore(snap, store)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(keep); string(data) != "v1" {
		t.Fatalf("keep.txt = %q want v1 (restored)", data)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("created.txt should be deleted on restore; stat err=%v", err)
	}
	if len(changed) != 2 {
		t.Fatalf("changed = %v want 2 entries", changed)
	}
}

func TestRewindFindsSnapshotByMessageID(t *testing.T) {
	work := t.TempDir()
	f := filepath.Join(work, "x.txt")
	_ = os.WriteFile(f, []byte("orig"), 0o644)
	store := NewStore(filepath.Join(work, ".snap"))
	snap, _ := store.Capture("mid-1", []string{f}, time.Now().UTC())
	tp := filepath.Join(work, "s.jsonl")
	if err := (Writer{TranscriptPath: tp}).Record(snap, false); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(f, []byte("changed"), 0o644)

	res, err := Rewind(tp, "mid-1", store)
	if err != nil {
		t.Fatal(err)
	}
	if res.MessageID != "mid-1" {
		t.Fatalf("MessageID = %q", res.MessageID)
	}
	if data, _ := os.ReadFile(f); string(data) != "orig" {
		t.Fatalf("file not restored: %q", data)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rewind/ -run 'TestRestore|TestRewind' -v`
Expected: FAIL — `undefined: Restore` / `undefined: Rewind`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/rewind/restore.go`:
```go
package rewind

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

// Restore applies a snapshot to disk. For each tracked path it either rewrites
// the file from its backup or deletes it (if the backup name is empty, meaning
// the file did not exist when the snapshot was taken).
func Restore(snap Snapshot, store Store) ([]string, error) {
	var changed []string
	for path, backup := range snap.TrackedFileBackups {
		if !filepath.IsAbs(path) {
			return changed, fmt.Errorf("rewind: refuse non-absolute restore path %q", path)
		}
		if backup.BackupFileName == "" {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return changed, fmt.Errorf("rewind: remove %q: %w", path, err)
			}
			changed = append(changed, path)
			continue
		}
		data, err := os.ReadFile(filepath.Join(store.Dir, backup.BackupFileName))
		if err != nil {
			return changed, fmt.Errorf("rewind: read backup for %q: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return changed, fmt.Errorf("rewind: mkdir for %q: %w", path, err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return changed, fmt.Errorf("rewind: write %q: %w", path, err)
		}
		changed = append(changed, path)
	}
	return changed, nil
}

type RewindResult struct {
	Snapshot  Snapshot
	Changed   []string
	MessageID contracts.ID
}

// Rewind loads the transcript, finds the snapshot for messageID, and applies it.
func Rewind(transcriptPath string, messageID contracts.ID, store Store) (RewindResult, error) {
	tr, err := session.LoadTranscript(transcriptPath)
	if err != nil {
		return RewindResult{}, err
	}
	raw, ok := tr.FileHistoryByMessageID[messageID]
	if !ok {
		return RewindResult{}, fmt.Errorf("rewind: no snapshot for message %q", messageID)
	}
	snap, err := decodeSnapshot(raw)
	if err != nil {
		return RewindResult{}, err
	}
	changed, err := Restore(snap, store)
	if err != nil {
		return RewindResult{}, err
	}
	return RewindResult{Snapshot: snap, Changed: changed, MessageID: messageID}, nil
}

// decodeSnapshot extracts the Snapshot from a stored file-history-snapshot line.
func decodeSnapshot(raw json.RawMessage) (Snapshot, error) {
	var line struct {
		Snapshot Snapshot `json:"snapshot"`
		Content  Snapshot `json:"content"`
	}
	if err := json.Unmarshal(raw, &line); err != nil {
		return Snapshot{}, fmt.Errorf("rewind: decode snapshot line: %w", err)
	}
	if line.Snapshot.MessageID != "" || len(line.Snapshot.TrackedFileBackups) > 0 {
		return line.Snapshot, nil
	}
	return line.Content, nil // Task 4 stored the payload under "content"
}
```

> The `decodeSnapshot` dual-field read tolerates either Task-4 representation (top-level `snapshot` per CC, or `content` if the implementer used `TranscriptMessage.Content`). Keep whichever the test exercises green; if only one path is real, drop the other branch.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rewind/ -v && go vet ./internal/rewind/`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/rewind/restore.go internal/rewind/restore_test.go
git commit -m "feat(rewind): restore a snapshot to disk and rewind by message id"
```

---

## Task 6: Cost persistence + restore-on-resume

**Files:**
- Create: `internal/costtrack/store.go`
- Test: `internal/costtrack/store_test.go`

**Pre-flight verification:**
```bash
cd /Users/sqlrush/ccgo
go doc ./internal/contracts Usage             # confirm CostUSD/InputTokens/OutputTokens fields
go doc ./internal/platform SanitizeProjectPath
sed -n '76,137p' /Users/sqlrush/agent/claude-code/src/cost-tracker.ts  # lastSessionId guard
```

**Interfaces produced:**
- `type ProjectCost struct { LastCost float64 \`json:"lastCost"\`; LastSessionID contracts.ID \`json:"lastSessionId"\`; LastTotalInputTokens int \`json:"lastTotalInputTokens"\`; LastTotalOutputTokens int \`json:"lastTotalOutputTokens"\` }` (mirrors CC `config.ts:76-105`, minimal subset).
- `type Options struct { ProjectsDir string; CWD string }` — `ProjectsDir` injectable (tests pass temp). The store path is `ProjectsDir/<SanitizeProjectPath(CWD)>/cost.json`.
- `func Save(opts Options, cost ProjectCost) error`.
- `func Restore(opts Options, sessionID contracts.ID) (ProjectCost, bool, error)` — returns `(cost, true, nil)` ONLY when the persisted `LastSessionID == sessionID` (CC's guard); otherwise `(_, false, nil)`.
- `func DefaultOptions(cwd string) Options` — fills `ProjectsDir = filepath.Join(platform.ClaudeHomeDir(), "projects")` (only place the global is read).

- [ ] **Step 1: Write the failing test**

Create `internal/costtrack/store_test.go`:
```go
package costtrack

import (
	"path/filepath"
	"testing"
)

func TestSaveRestoreSameSession(t *testing.T) {
	dir := t.TempDir()
	opts := Options{ProjectsDir: dir, CWD: "/home/u/proj"}
	want := ProjectCost{LastCost: 0.42, LastSessionID: "s1", LastTotalInputTokens: 10, LastTotalOutputTokens: 5}
	if err := Save(opts, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Restore(opts, "s1")
	if err != nil || !ok {
		t.Fatalf("Restore ok=%v err=%v", ok, err)
	}
	if got.LastCost != 0.42 || got.LastSessionID != "s1" {
		t.Fatalf("got %+v", got)
	}
}

func TestRestoreDifferentSessionMisses(t *testing.T) {
	dir := t.TempDir()
	opts := Options{ProjectsDir: dir, CWD: "/home/u/proj"}
	if err := Save(opts, ProjectCost{LastCost: 1.0, LastSessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	_, ok, err := Restore(opts, "s2")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("must not restore cost across a different session id")
	}
}

func TestRestoreNoFile(t *testing.T) {
	opts := Options{ProjectsDir: filepath.Join(t.TempDir(), "empty"), CWD: "/x"}
	_, ok, err := Restore(opts, "s1")
	if err != nil || ok {
		t.Fatalf("missing file should be (false,nil); ok=%v err=%v", ok, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/costtrack/ -v`
Expected: FAIL — package/`Save`/`Restore` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/costtrack/store.go`:
```go
package costtrack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

type ProjectCost struct {
	LastCost              float64      `json:"lastCost"`
	LastSessionID         contracts.ID `json:"lastSessionId"`
	LastTotalInputTokens  int          `json:"lastTotalInputTokens"`
	LastTotalOutputTokens int          `json:"lastTotalOutputTokens"`
}

type Options struct {
	ProjectsDir string
	CWD         string
}

func DefaultOptions(cwd string) Options {
	return Options{ProjectsDir: filepath.Join(platform.ClaudeHomeDir(), "projects"), CWD: cwd}
}

func costPath(opts Options) string {
	return filepath.Join(opts.ProjectsDir, platform.SanitizeProjectPath(opts.CWD), "cost.json")
}

func Save(opts Options, cost ProjectCost) error {
	path := costPath(opts)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("costtrack: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cost, "", "  ")
	if err != nil {
		return fmt.Errorf("costtrack: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("costtrack: write: %w", err)
	}
	return nil
}

// Restore returns the persisted cost only if it belongs to sessionID (CC's
// lastSessionId guard, cost-tracker.ts:87-137). Missing file => (_, false, nil).
func Restore(opts Options, sessionID contracts.ID) (ProjectCost, bool, error) {
	data, err := os.ReadFile(costPath(opts))
	if os.IsNotExist(err) {
		return ProjectCost{}, false, nil
	}
	if err != nil {
		return ProjectCost{}, false, fmt.Errorf("costtrack: read: %w", err)
	}
	var cost ProjectCost
	if err := json.Unmarshal(data, &cost); err != nil {
		return ProjectCost{}, false, fmt.Errorf("costtrack: parse: %w", err)
	}
	if cost.LastSessionID != sessionID {
		return ProjectCost{}, false, nil
	}
	return cost, true, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/costtrack/ -v && go vet ./internal/costtrack/`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/costtrack/store.go internal/costtrack/store_test.go
git commit -m "feat(costtrack): persist per-project cost and restore on same-session resume"
```

---

## Task 7: Post-compact file restoration builder

**Files:**
- Create: `internal/compact/postcompact.go`
- Test: `internal/compact/postcompact_test.go`

**Pre-flight verification:**
```bash
cd /Users/sqlrush/ccgo
go doc ./internal/contracts Message                 # confirm Type/Content fields for building messages
grep -n "func NewTextBlock\|func UserText\|MessageUser" internal/contracts/*.go internal/messages/*.go | head
sed -n '1415,1465p' /Users/sqlrush/agent/claude-code/src/services/compact/compact.ts  # 5 files, 50k budget
```

**Interfaces produced:**
- `type ReadFileEntry struct { Path string; Content string; Timestamp int64 }` (a recent-read snapshot the caller supplies; ccgo has no `readFileState` yet, so this is the injection point).
- `type PostCompactOptions struct { MaxFiles int; PreservedPaths map[string]bool; ApproxTokensPerChar float64; TokenBudget int; MaxTokensPerFile int }` with defaults `MaxFiles=5`, `TokenBudget=50000`, `MaxTokensPerFile=5000` (CC constants).
- `func BuildPostCompactAttachments(files []ReadFileEntry, opts PostCompactOptions) []contracts.Message` — sorts by `Timestamp` desc, skips `PreservedPaths`, caps file count + per-file + total token budget, returns user-role attachment messages (matching how ccgo represents file attachments — confirm via the grep above and reuse `messages.UserText`/`contracts.NewTextBlock`).

- [ ] **Step 1: Write the failing test**

Create `internal/compact/postcompact_test.go`:
```go
package compact

import "testing"

func TestBuildPostCompactAttachmentsRecentFirstAndSkipPreserved(t *testing.T) {
	files := []ReadFileEntry{
		{Path: "/a.go", Content: "AAA", Timestamp: 100},
		{Path: "/b.go", Content: "BBB", Timestamp: 300}, // most recent
		{Path: "/c.go", Content: "CCC", Timestamp: 200},
		{Path: "/preserved.go", Content: "PPP", Timestamp: 999},
	}
	opts := PostCompactOptions{
		MaxFiles:       2,
		PreservedPaths: map[string]bool{"/preserved.go": true},
	}
	msgs := BuildPostCompactAttachments(files, opts)
	if len(msgs) != 2 {
		t.Fatalf("got %d attachments want 2", len(msgs))
	}
	// preserved.go must be skipped even though it is the newest.
	body := messageText(t, msgs[0]) + messageText(t, msgs[1])
	if contains(body, "PPP") {
		t.Fatal("preserved file must not be re-attached")
	}
	// most recent non-preserved first: b.go then c.go.
	if !contains(messageText(t, msgs[0]), "BBB") {
		t.Fatalf("first attachment should be the newest (b.go); got %q", messageText(t, msgs[0]))
	}
}

func TestBuildPostCompactAttachmentsTokenBudget(t *testing.T) {
	big := make([]byte, 0, 40000)
	for i := 0; i < 40000; i++ {
		big = append(big, 'x')
	}
	files := []ReadFileEntry{
		{Path: "/1", Content: string(big), Timestamp: 3},
		{Path: "/2", Content: string(big), Timestamp: 2},
		{Path: "/3", Content: string(big), Timestamp: 1},
	}
	opts := PostCompactOptions{MaxFiles: 5, TokenBudget: 50000, MaxTokensPerFile: 50000, ApproxTokensPerChar: 1}
	msgs := BuildPostCompactAttachments(files, opts)
	// 40000 + 40000 = 80000 > 50000 budget => at most 1 fits.
	if len(msgs) > 1 {
		t.Fatalf("token budget exceeded: %d attachments", len(msgs))
	}
}
```

You MUST add the small test helpers `messageText`/`contains` (or reuse existing ones — check `grep -n "func messageText\|func contains" internal/compact/*_test.go`). If `contracts.Message` content extraction already has a helper (e.g. `messages.TextContent`), use it in `messageText` rather than re-deriving.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/compact/ -run TestBuildPostCompact -v`
Expected: FAIL — `undefined: ReadFileEntry` / `undefined: BuildPostCompactAttachments`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/compact/postcompact.go`:
```go
package compact

import (
	"fmt"
	"sort"

	"ccgo/internal/contracts"
	"ccgo/internal/messages"
)

const (
	defaultPostCompactMaxFiles    = 5
	defaultPostCompactTokenBudget = 50000
	defaultPostCompactPerFile     = 5000
)

type ReadFileEntry struct {
	Path      string
	Content   string
	Timestamp int64
}

type PostCompactOptions struct {
	MaxFiles            int
	PreservedPaths      map[string]bool
	ApproxTokensPerChar float64
	TokenBudget         int
	MaxTokensPerFile    int
}

// BuildPostCompactAttachments re-attaches the most recently read files after a
// compaction (CC compact.ts:1415-1464): newest first, skip preserved files,
// honor file-count, per-file, and total token budgets.
func BuildPostCompactAttachments(files []ReadFileEntry, opts PostCompactOptions) []contracts.Message {
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaultPostCompactMaxFiles
	}
	if opts.TokenBudget <= 0 {
		opts.TokenBudget = defaultPostCompactTokenBudget
	}
	if opts.MaxTokensPerFile <= 0 {
		opts.MaxTokensPerFile = defaultPostCompactPerFile
	}
	if opts.ApproxTokensPerChar <= 0 {
		opts.ApproxTokensPerChar = 0.25 // ~4 chars/token
	}

	sorted := append([]ReadFileEntry(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Timestamp > sorted[j].Timestamp })

	var msgs []contracts.Message
	used := 0
	for _, f := range sorted {
		if len(msgs) >= opts.MaxFiles {
			break
		}
		if opts.PreservedPaths[f.Path] {
			continue
		}
		tokens := int(float64(len(f.Content)) * opts.ApproxTokensPerChar)
		if tokens > opts.MaxTokensPerFile {
			continue
		}
		if used+tokens > opts.TokenBudget {
			continue
		}
		used += tokens
		body := fmt.Sprintf("Re-reading %s after compaction:\n%s", f.Path, f.Content)
		msgs = append(msgs, messages.UserText(body))
	}
	return msgs
}
```

> Confirm `messages.UserText(string) contracts.Message` exists (it is used by Phase 1's plan, `internal/messages`). If the attachment representation in ccgo is a distinct attachment subtype rather than a plain user message, build that instead — verify with `grep -rn "Attachment\|attachment" internal/messages/ internal/contracts/`. Keep the recency/budget logic regardless.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/compact/ -v && go vet ./internal/compact/`
Expected: PASS, including pre-existing compaction tests.

- [ ] **Step 5: Commit**
```bash
git add internal/compact/postcompact.go internal/compact/postcompact_test.go
git commit -m "feat(compact): post-compaction file re-attachment builder"
```

---

## Task 8: Wire prompt history + a /rewind command seam into the running path

**Files:**
- Create: `internal/repl/history_seam.go` (a tiny seam recording each submit to `history.jsonl`)
- Modify: `internal/repl/run.go` (call the seam on submit) — **verify it exists first**
- Test: `internal/repl/history_seam_test.go`
- Optionally: `internal/repl/rewind_command.go` + test (the in-loop entry point)

**CROSS-PHASE NOTE:** the full interactive `/rewind` UI (a picker over snapshots, confirmation dialog) belongs to **Phase 2 (interactive completeness)** / **Phase 6b (commands)**. This task lands the *seam and behavior* only: (a) every submitted prompt is appended to `~/.claude/history.jsonl` via the existing `session.BufferedHistoryWriter`/`AddToHistory`; (b) a plain `RewindToMessage(transcriptPath, messageID)` helper the future command/UI calls. Do NOT build dialogs here.

**Pre-flight verification (CRITICAL — confirm the seam target exists):**
```bash
cd /Users/sqlrush/ccgo
go doc ./internal/session AddToHistory          # (path, project, sessionID, HistoryEntry) (bool, error)
go doc ./internal/session HistoryEntry          # Display + PastedContents
go doc ./internal/session HistoryPath           # ~/.claude/history.jsonl
grep -n "func RunInteractive\|StartTurn\|loop.StartTurn" internal/repl/run.go   # confirm Phase 1 shape
grep -rn "AddToHistory\|BufferedHistoryWriter\|HistoryPath" cmd/ internal/repl/ internal/bootstrap/   # confirm STILL zero callers
echo "CLAUDE_CODE_SKIP_PROMPT_HISTORY env check (CC history.ts:414):"
```

**Interfaces produced:**
- `type HistoryRecorder struct { Path string; Project string; SessionID contracts.ID; Skip bool }`.
- `func NewHistoryRecorder(project string, sessionID contracts.ID) HistoryRecorder` — `Path = session.HistoryPath()`, `Skip = os.Getenv("CLAUDE_CODE_SKIP_PROMPT_HISTORY") == "true"` (CC parity).
- `func (r HistoryRecorder) Record(prompt string) error` — when not skipping, `session.AddToHistory(r.Path, r.Project, r.SessionID, session.HistoryEntry{Display: prompt})`.
- `func RewindToMessage(transcriptPath string, messageID contracts.ID, sessionDir string) (rewind.RewindResult, error)` — thin wrapper over `rewind.Rewind(transcriptPath, messageID, rewind.NewStore(sessionDir))`.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/history_seam_test.go`:
```go
package repl

import (
	"path/filepath"
	"testing"

	"ccgo/internal/session"
)

func TestHistoryRecorderAppends(t *testing.T) {
	dir := t.TempDir()
	rec := HistoryRecorder{
		Path:      filepath.Join(dir, "history.jsonl"),
		Project:   "/home/u/proj",
		SessionID: "s1",
	}
	if err := rec.Record("first prompt"); err != nil {
		t.Fatal(err)
	}
	if err := rec.Record("second prompt"); err != nil {
		t.Fatal(err)
	}
	w := &session.BufferedHistoryWriter{Path: rec.Path}
	entries, err := w.LoadHistory(10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected >=2 history entries, got %d", len(entries))
	}
}

func TestHistoryRecorderSkip(t *testing.T) {
	dir := t.TempDir()
	rec := HistoryRecorder{Path: filepath.Join(dir, "history.jsonl"), Skip: true}
	if err := rec.Record("ignored"); err != nil {
		t.Fatal(err)
	}
	if _, err := osStat(rec.Path); err == nil {
		t.Fatal("skip mode must not create the history file")
	}
}
```

Confirm the `BufferedHistoryWriter.LoadHistory` signature and `osStat` helper before relying on them:
```bash
go doc ./internal/session BufferedHistoryWriter
grep -n "func (w \*BufferedHistoryWriter) LoadHistory" internal/session/history.go
```
If `LoadHistory` needs a non-nil `PasteResolver`, pass a stub `func(string)(string,bool){return "",false}`. Add a tiny `osStat = os.Stat` alias in the test file or just use `os.Stat` directly (import `os`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestHistoryRecorder -v`
Expected: FAIL — `undefined: HistoryRecorder`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/history_seam.go`:
```go
package repl

import (
	"os"

	"ccgo/internal/contracts"
	"ccgo/internal/rewind"
	"ccgo/internal/session"
)

// HistoryRecorder appends submitted prompts to ~/.claude/history.jsonl.
type HistoryRecorder struct {
	Path      string
	Project   string
	SessionID contracts.ID
	Skip      bool
}

// NewHistoryRecorder mirrors CC: skip when CLAUDE_CODE_SKIP_PROMPT_HISTORY=true.
func NewHistoryRecorder(project string, sessionID contracts.ID) HistoryRecorder {
	return HistoryRecorder{
		Path:      session.HistoryPath(),
		Project:   project,
		SessionID: sessionID,
		Skip:      os.Getenv("CLAUDE_CODE_SKIP_PROMPT_HISTORY") == "true",
	}
}

func (r HistoryRecorder) Record(prompt string) error {
	if r.Skip || prompt == "" {
		return nil
	}
	_, err := session.AddToHistory(r.Path, r.Project, r.SessionID, session.HistoryEntry{Display: prompt})
	return err
}

// RewindToMessage restores the working tree to the snapshot for messageID.
// The interactive picker/confirmation UI is Phase 2 / Phase 6b; this is the seam.
func RewindToMessage(transcriptPath string, messageID contracts.ID, sessionDir string) (rewind.RewindResult, error) {
	return rewind.Rewind(transcriptPath, messageID, rewind.NewStore(sessionDir))
}
```

Then wire `HistoryRecorder.Record` into the submit path in `internal/repl/run.go`'s `StartTurn` closure (record the prompt right before launching the turn goroutine). Confirm the exact closure with the grep above and insert one call:
```go
// inside RunInteractive's StartTurn, before `go func(){...}`:
_ = recorder.Record(input) // best-effort; history failure must not break the turn
```
Construct `recorder := NewHistoryRecorder(base.CWD(), base.SessionID())` near the top of `RunInteractive` — confirm the runner exposes project/session via `grep -n "func (.*Runner) CWD\|SessionID\|Project" internal/conversation/*.go`; if not, thread them in from `cmd/claude/main.go` (where bootstrap state has them) and pass to `RunInteractive` as parameters. Do NOT fabricate accessor methods — verify or pass explicitly.

- [ ] **Step 4: Run tests + build the world**

Run:
```bash
go test ./internal/repl/ -run TestHistoryRecorder -v
go build ./... && go vet ./... && go test ./...
```
Expected: package tests PASS; full build/vet clean; full suite green (no regression in `--print` headless path).

- [ ] **Step 5: Commit**
```bash
git add internal/repl/history_seam.go internal/repl/run.go internal/repl/history_seam_test.go
git commit -m "feat(repl): record prompts to history.jsonl and expose a rewind seam"
```

---

## Self-Review

**Spec coverage (Phase 6c deliverables from roadmap §5):**
- Full CLAUDE.md scope hierarchy (Managed/User/project-walk/.claude/rules/*.local) with precedence + labels → Task 1. ✓
- @import resolution with cycle guard + depth cap + relative/`~`/traversal handling → Task 2. ✓
- Merged scoped loader (hierarchy + imports) → Task 3. ✓
- Rewind/checkpoint snapshot **writer** + format + backup store → Task 4. ✓ (parser already existed; writer is the gap)
- Rewind **restore** (apply snapshot, by message id) → Task 5. ✓
- Cost persistence + restore-on-resume (same-session guard) → Task 6. ✓
- Post-compact file restoration → Task 7. ✓
- `~/.claude/history.jsonl` wired + `/rewind` seam → Task 8. ✓

**Cross-phase dependencies (flagged):**
- Task 8's interactive `/rewind` *UI* (snapshot picker + confirm dialog) is **Phase 2 / Phase 6b**; this plan lands only the seam + behavior. The history recorder also needs the runner's project/session — verify accessors or thread from `cmd/claude/main.go` (Phase 1 wiring), do not invent methods.
- The snapshot **write trigger** (taking a snapshot before each file-mutating tool / each user turn) is the natural follow-up that the agent-loop owns; this plan delivers the writer/format/restore so the loop can call `rewind.Writer.Record` + `rewind.Store.Capture`. Wiring the trigger into the turn lifecycle can land here (extend Task 8) or alongside Phase 3 — flagged, not assumed.
- Cost persistence consumes `contracts.Usage.CostUSD`; populating it across a turn is the agent-loop's job (Phase 3). Task 6 only persists/restores whatever total it's handed.

**gap-audit vs. code discrepancies found:**
- gap-audit §4.G item 23 / §5 says "no `~/.claude/history.jsonl`". **FALSE** — `internal/session/history.go` already implements the full store (`HistoryPath`, `LogEntry` matching CC byte-for-byte, `AddToHistory`, `BufferedHistoryWriter`). The real gap is **zero callers** (no `cmd/`/`repl/`/`bootstrap/` wiring). Task 8 corrects the audit by *wiring*, not building.
- gap-audit item 21 ("rewind/checkpoint entirely absent (transcript parses snapshot lines but nobody writes them)") is **CONFIRMED** in code: `transcript.go:272-283` parses `file-history-snapshot`/`attribution-snapshot` into `FileHistorySnapshots`/`FileHistoryByMessageID`, and no writer emits them.
- gap-audit item 22 (CLAUDE.md only walks parent bare files) **CONFIRMED**: `claudemd.go:14-51` is a parent-walk over a single `CLAUDE.md` per dir; no scopes.
- gap-audit item 23 (@import not resolved) **CONFIRMED**: `LoadClaudeContext` reads files verbatim, no import expansion.
- Cost persistence **CONFIRMED absent**: `ResumeConversation` (`transcript_resume.go:9-18`) has no cost field; no `ProjectConfig`/`lastCost` anywhere in `internal/config`.
- Post-compact restoration **CONFIRMED absent**: no `PostCompact`/`readFileState`/`Attachment` in `internal/compact/`.

**Placeholder scan:** no `t.Skip`. The only intentional throwaway helpers are test-local shims (`itoa`/`containsDir`/`idx`) flagged for deletion if unused, and two "choose the path the test dictates" implementer notes in Tasks 4 and 5 (the snapshot line representation) — both are concrete, both green paths are spelled out, and the decode tolerates either. All production code is complete.

**Type-consistency verification points (flagged at point of use, must `go doc`/`grep` before writing):** `memory.ClaudeFile` fields (Task 1), `memory.Type` consts (Task 1/3), `config.ManagedSettingsDir`/`platform.ClaudeHomeDir` signatures (Task 1), CC `@import` regex + `MAX_INCLUDE_DEPTH` (Task 2), `session.TranscriptMessage` shape + `AppendTranscriptMessage` + `parseSnapshotMessageID` accepted keys + `file-history-snapshot` parser branch (Task 4), `session.Transcript.FileHistoryByMessageID` (Task 5), `contracts.Usage` fields + `platform.SanitizeProjectPath` (Task 6), `contracts.Message`/`messages.UserText`/attachment representation (Task 7), `session.AddToHistory`/`HistoryEntry`/`HistoryPath`/`BufferedHistoryWriter.LoadHistory` + `RunInteractive`/`StartTurn` shape + runner project/session accessors (Task 8).

**Immutability / errors / files:** all new functions return new values (no in-place mutation of shared structs; `Restore` writes to disk, not to shared memory). Every error is wrapped with `fmt.Errorf(... %w ...)` and surfaced; missing imports/missing-files are *skipped* deliberately (documented) rather than swallowed silently. Each new file is single-responsibility and well under 350 lines.
