package repl

// G26: OVL-08 GlobalSearchOverlay wiring tests.
// Verifies:
//   - ActionGlobalSearch keybinding resolves in default keymap.
//   - ScreenEventGlobalSearch is emitted when screen.ApplyKey sees Ctrl+\.
//   - handleGlobalSearchSubmit inserts @<file> into prompt.
//   - Loop opens GlobalSearchOverlay on ScreenEventGlobalSearch when searchRoot set.
//   - SetSearchRoot / SetCWD seeds searchRoot correctly.

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

func TestActionGlobalSearchInDefaultKeymap(t *testing.T) {
	km := tui.DefaultKeymap()
	action := km.Resolve(tui.Key{Type: tui.KeyCtrlBackslash})
	if action != tui.ActionGlobalSearch {
		t.Fatalf("Ctrl+\\ should resolve to ActionGlobalSearch, got %q", action)
	}
}

func TestScreenApplyKeyCtrlBackslashEmitsGlobalSearch(t *testing.T) {
	s := tui.REPLScreen{Width: 80, Height: 24}
	event := s.ApplyKey(tui.Key{Type: tui.KeyCtrlBackslash})
	if event.Type != tui.ScreenEventGlobalSearch {
		t.Fatalf("Ctrl+\\ should emit ScreenEventGlobalSearch, got %q", event.Type)
	}
}

func TestHandleGlobalSearchSubmitInsertsAtMention(t *testing.T) {
	tests := []struct {
		name    string
		prompt  string
		submit  string
		want    string
		handled bool
	}{
		{
			name:    "empty prompt",
			prompt:  "",
			submit:  "globalsearch:internal/foo/bar.go:42",
			want:    "@internal/foo/bar.go ",
			handled: true,
		},
		{
			name:    "non-empty prompt",
			prompt:  "look at",
			submit:  "globalsearch:cmd/claude/main.go:100",
			want:    "look at @cmd/claude/main.go ",
			handled: true,
		},
		{
			name:    "non-globalsearch submit",
			prompt:  "unchanged",
			submit:  "quickopen:foo.go",
			want:    "unchanged",
			handled: false,
		},
		{
			name:    "file without line",
			prompt:  "",
			submit:  "globalsearch:foo.go",
			want:    "@foo.go ",
			handled: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.prompt
			got := handleGlobalSearchSubmit(&p, tc.submit)
			if got != tc.handled {
				t.Fatalf("handled = %v want %v", got, tc.handled)
			}
			if p != tc.want {
				t.Fatalf("prompt = %q want %q", p, tc.want)
			}
		})
	}
}

func TestSetCWDSeedsSearchRoot(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetCWD("/workspace/project")
	if l.searchRoot != "/workspace/project" {
		t.Fatalf("searchRoot = %q want /workspace/project", l.searchRoot)
	}
}

func TestSetSearchRootOverridesCWD(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetCWD("/workspace")
	l.SetSearchRoot("/workspace/sub")
	if l.searchRoot != "/workspace/sub" {
		t.Fatalf("searchRoot = %q want /workspace/sub after SetSearchRoot", l.searchRoot)
	}
}

func TestSetCWDDoesNotOverrideExplicitSearchRoot(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetSearchRoot("/explicit")
	l.SetCWD("/other")
	// SetCWD should not override an already-set searchRoot.
	// (The SetCWD implementation only seeds searchRoot when it is still empty.)
	if l.searchRoot != "/explicit" {
		t.Fatalf("searchRoot = %q want /explicit (SetCWD should not override explicit SetSearchRoot)", l.searchRoot)
	}
}

func TestLoopOpensGlobalSearchOverlayOnCtrlBackslash(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetSearchRoot("/workspace")
	// Simulate the full handleKey path via the exported test helper.
	// Use the same pattern as OVL-07 HistorySearch tests.
	exit := l.handleKey(tui.Key{Type: tui.KeyCtrlBackslash})
	if exit {
		t.Fatal("Ctrl+\\ should not trigger loop exit")
	}
	if l.activeOverlay == nil {
		t.Fatal("activeOverlay should be set after Ctrl+\\ with searchRoot set")
	}
	_, ok := l.activeOverlay.(*GlobalSearchOverlay)
	if !ok {
		t.Fatalf("activeOverlay should be *GlobalSearchOverlay, got %T", l.activeOverlay)
	}
}

func TestLoopDoesNotOpenGlobalSearchWithoutSearchRoot(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	// searchRoot is empty — overlay must NOT open.
	l.handleKey(tui.Key{Type: tui.KeyCtrlBackslash})
	if l.activeOverlay != nil {
		t.Fatalf("activeOverlay should remain nil when searchRoot is empty, got %T", l.activeOverlay)
	}
}

func TestGlobalSearchOverlaySubmitRoutedCorrectly(t *testing.T) {
	// Verify that handleOverlaySubmit routes "globalsearch:…" and inserts path.
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.screen.Prompt.Text = ""
	handled := l.handleOverlaySubmit("globalsearch:internal/foo.go:10")
	if !handled {
		t.Fatal("handleOverlaySubmit should handle globalsearch: prefix")
	}
	if !strings.Contains(l.screen.Prompt.Text, "@internal/foo.go") {
		t.Fatalf("prompt should contain @internal/foo.go, got %q", l.screen.Prompt.Text)
	}
}
