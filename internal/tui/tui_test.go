package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccgo/internal/session"
)

func typePromptText(screen *REPLScreen, text string) {
	for _, r := range text {
		screen.ApplyKey(Key{Type: KeyRune, Rune: r})
	}
}

func TestPromptStateEditsAndSubmits(t *testing.T) {
	prompt := NewPromptState([]string{"old command"})
	for _, seq := range []string{"h", "i", "\x1b[D", "!"} {
		prompt.Apply(ParseKey(seq))
	}
	if prompt.Text != "h!i" || prompt.Cursor != 2 {
		t.Fatalf("prompt = %#v", prompt)
	}
	result := prompt.Apply(ParseKey("\n"))
	if result.Submitted != "h!i" {
		t.Fatalf("result = %#v", result)
	}
	if prompt.Text != "" || prompt.Cursor != 0 {
		t.Fatalf("prompt after submit = %#v", prompt)
	}
}

func TestPromptStateControlLineEditing(t *testing.T) {
	prompt := NewPromptState(nil)
	typePromptText := func(text string) {
		for _, r := range text {
			prompt.Apply(Key{Type: KeyRune, Rune: r})
		}
	}
	typePromptText("alpha beta gamma")
	prompt.Apply(ParseKey("\x17"))
	if prompt.Text != "alpha beta " || prompt.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("after ctrl-w prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x02"))
	if prompt.Cursor != len([]rune("alpha beta")) {
		t.Fatalf("after ctrl-b prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x06"))
	if prompt.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("after ctrl-f prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x01"))
	prompt.Apply(ParseKey("\x0b"))
	if prompt.Text != "" || prompt.Cursor != 0 {
		t.Fatalf("after ctrl-k prompt = %#v", prompt)
	}
	typePromptText("alpha beta")
	prompt.Apply(ParseKey("\x01"))
	for i := 0; i < len([]rune("alpha ")); i++ {
		prompt.Apply(ParseKey("\x06"))
	}
	prompt.Apply(ParseKey("\x04"))
	if prompt.Text != "alpha eta" || prompt.Cursor != len([]rune("alpha ")) {
		t.Fatalf("after ctrl-d prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x0b"))
	if prompt.Text != "alpha " || prompt.Cursor != len([]rune("alpha ")) {
		t.Fatalf("after ctrl-k after ctrl-d prompt = %#v", prompt)
	}
	typePromptText("beta")
	prompt.Apply(ParseKey("\x1b[D"))
	prompt.Apply(ParseKey("\x15"))
	if prompt.Text != "a" || prompt.Cursor != 0 {
		t.Fatalf("after ctrl-u prompt = %#v", prompt)
	}
}

func TestPromptStateKillRingYank(t *testing.T) {
	resetSharedKillRingForTesting()
	defer resetSharedKillRingForTesting()

	prompt := NewPromptState(nil)
	typePromptText := func(text string) {
		for _, r := range text {
			prompt.Apply(Key{Type: KeyRune, Rune: r})
		}
	}

	typePromptText("alpha beta gamma")
	prompt.Apply(ParseKey("\x17"))
	prompt.Apply(ParseKey("\x17"))
	if prompt.Text != "alpha " || prompt.Cursor != len([]rune("alpha ")) {
		t.Fatalf("after consecutive ctrl-w prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x19"))
	if prompt.Text != "alpha beta gamma" || prompt.Cursor != len([]rune("alpha beta gamma")) {
		t.Fatalf("after ctrl-y prompt = %#v", prompt)
	}

	prompt.Apply(ParseKey("\x15"))
	if prompt.Text != "" || prompt.Cursor != 0 {
		t.Fatalf("after ctrl-u prompt = %#v", prompt)
	}
	typePromptText("new ")
	prompt.Apply(ParseKey("\x19"))
	if prompt.Text != "new alpha beta gamma" || prompt.Cursor != len([]rune("new alpha beta gamma")) {
		t.Fatalf("after ctrl-y following ctrl-u prompt = %#v", prompt)
	}

	prompt.Apply(ParseKey("\x01"))
	for i := 0; i < len([]rune("new ")); i++ {
		prompt.Apply(ParseKey("\x06"))
	}
	prompt.Apply(ParseKey("\x0b"))
	if prompt.Text != "new " || prompt.Cursor != len([]rune("new ")) {
		t.Fatalf("after ctrl-k prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x19"))
	if prompt.Text != "new alpha beta gamma" || prompt.Cursor != len([]rune("new alpha beta gamma")) {
		t.Fatalf("after ctrl-y following ctrl-k prompt = %#v", prompt)
	}
}

func TestPromptStateYankPopCyclesAndResets(t *testing.T) {
	resetSharedKillRingForTesting()
	defer resetSharedKillRingForTesting()

	prompt := NewPromptState(nil)
	sharedKillRing.ring = []string{"beta", "gamma", "delta"}

	prompt.Apply(ParseKey("\x19"))
	if prompt.Text != "beta" || prompt.Cursor != len([]rune("beta")) {
		t.Fatalf("after ctrl-y prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1by"))
	if prompt.Text != "gamma" || prompt.Cursor != len([]rune("gamma")) {
		t.Fatalf("after first alt-y prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1by"))
	if prompt.Text != "delta" || prompt.Cursor != len([]rune("delta")) {
		t.Fatalf("after second alt-y prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1by"))
	if prompt.Text != "beta" || prompt.Cursor != len([]rune("beta")) {
		t.Fatalf("after wrapped alt-y prompt = %#v", prompt)
	}

	prompt.Apply(ParseKey("!"))
	prompt.Apply(ParseKey("\x1by"))
	if prompt.Text != "beta!" || prompt.Cursor != len([]rune("beta!")) {
		t.Fatalf("alt-y after non-yank key should be ignored: %#v", prompt)
	}
}

func TestPromptStateAltWordEditing(t *testing.T) {
	resetSharedKillRingForTesting()
	defer resetSharedKillRingForTesting()

	prompt := NewPromptState(nil)
	for _, r := range "alpha beta gamma" {
		prompt.Apply(Key{Type: KeyRune, Rune: r})
	}
	prompt.Apply(ParseKey("\x1bb"))
	if prompt.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("after alt-b prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1bb"))
	if prompt.Cursor != len([]rune("alpha ")) {
		t.Fatalf("after second alt-b prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1bf"))
	if prompt.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("after alt-f prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1bd"))
	if prompt.Text != "alpha beta " || prompt.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("after alt-d prompt = %#v", prompt)
	}
	for _, r := range "delta" {
		prompt.Apply(Key{Type: KeyRune, Rune: r})
	}
	prompt.Apply(ParseKey("\x1b\x7f"))
	if prompt.Text != "alpha beta " || prompt.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("after alt-backspace prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x19"))
	if prompt.Text != "alpha beta delta" {
		t.Fatalf("after ctrl-y from alt-backspace prompt = %#v", prompt)
	}
}

func TestPromptAndReverseSearchShareKillRing(t *testing.T) {
	resetSharedKillRingForTesting()
	defer resetSharedKillRingForTesting()

	prompt := NewPromptState(nil)
	for _, r := range "shared term" {
		prompt.Apply(Key{Type: KeyRune, Rune: r})
	}
	prompt.Apply(ParseKey("\x17"))
	if prompt.Text != "shared " {
		t.Fatalf("prompt after ctrl-w = %#v", prompt)
	}

	screen := NewREPLScreen(40, 8, []string{"find shared term", "find other"})
	screen.ApplyKey(ParseKey("\x12"))
	screen.ApplyKey(ParseKey("\x19"))
	if screen.ReverseSearch.Query != "term" || screen.ReverseSearch.Cursor != len([]rune("term")) {
		t.Fatalf("reverse search after shared yank = %#v", screen.ReverseSearch)
	}

	screen.ApplyKey(ParseKey("\x15"))
	if screen.ReverseSearch.Query != "" || screen.ReverseSearch.Cursor != 0 {
		t.Fatalf("reverse search after ctrl-u = %#v", screen.ReverseSearch)
	}
	prompt = NewPromptState(nil)
	prompt.Apply(ParseKey("\x19"))
	if prompt.Text != "term" || prompt.Cursor != len([]rune("term")) {
		t.Fatalf("prompt after reverse-search kill yank = %#v", prompt)
	}
}

func TestReverseSearchCursorEditingAndYankPop(t *testing.T) {
	resetSharedKillRingForTesting()
	defer resetSharedKillRingForTesting()

	screen := NewREPLScreen(40, 8, []string{"alpha beta", "alpha gamma", "alpha delta"})
	screen.ApplyKey(ParseKey("\x12"))
	for _, r := range "alphabet" {
		screen.ApplyKey(Key{Type: KeyRune, Rune: r})
	}
	for i := 0; i < len([]rune("bet")); i++ {
		screen.ApplyKey(ParseKey("\x1b[D"))
	}
	screen.ApplyKey(ParseKey(" "))
	if screen.ReverseSearch.Query != "alpha bet" || screen.ReverseSearch.Cursor != len([]rune("alpha ")) {
		t.Fatalf("reverse search cursor insert = %#v", screen.ReverseSearch)
	}
	screen.ApplyKey(ParseKey("\x0b"))
	if screen.ReverseSearch.Query != "alpha " {
		t.Fatalf("reverse search ctrl-k = %#v", screen.ReverseSearch)
	}
	sharedKillRing.ring = []string{"beta", "gamma", "delta"}
	screen.ApplyKey(ParseKey("\x19"))
	if screen.ReverseSearch.Query != "alpha beta" {
		t.Fatalf("reverse search ctrl-y = %#v", screen.ReverseSearch)
	}
	screen.ApplyKey(ParseKey("\x1by"))
	if screen.ReverseSearch.Query != "alpha gamma" || screen.ReverseSearch.Cursor != len([]rune("alpha gamma")) {
		t.Fatalf("reverse search alt-y = %#v", screen.ReverseSearch)
	}
}

func TestReverseSearchAltWordEditing(t *testing.T) {
	resetSharedKillRingForTesting()
	defer resetSharedKillRingForTesting()

	screen := NewREPLScreen(40, 8, []string{"alpha beta", "alpha gamma", "alpha delta"})
	screen.ApplyKey(ParseKey("\x12"))
	for _, r := range "alpha beta gamma" {
		screen.ApplyKey(Key{Type: KeyRune, Rune: r})
	}
	screen.ApplyKey(ParseKey("\x1bb"))
	if screen.ReverseSearch.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("reverse search alt-b = %#v", screen.ReverseSearch)
	}
	screen.ApplyKey(ParseKey("\x1bd"))
	if screen.ReverseSearch.Query != "alpha beta " || screen.ReverseSearch.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("reverse search alt-d = %#v", screen.ReverseSearch)
	}
	screen.ApplyKey(ParseKey("\x1b\x7f"))
	if screen.ReverseSearch.Query != "alpha " || screen.ReverseSearch.Cursor != len([]rune("alpha ")) {
		t.Fatalf("reverse search alt-backspace = %#v", screen.ReverseSearch)
	}
	screen.ApplyKey(ParseKey("\x19"))
	if screen.ReverseSearch.Query != "alpha beta " || screen.ReverseSearch.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("reverse search ctrl-y from alt-backspace = %#v", screen.ReverseSearch)
	}
	screen.ApplyKey(ParseKey("\x1bb"))
	screen.ApplyKey(ParseKey("\x1bf"))
	if screen.ReverseSearch.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("reverse search alt-f = %#v", screen.ReverseSearch)
	}
}

func TestPromptHistoryNavigationKeepsDraft(t *testing.T) {
	prompt := NewPromptState([]string{"one", "two"})
	prompt.Apply(ParseKey("d"))
	prompt.Apply(ParseKey("\x1b[A"))
	if prompt.Text != "two" {
		t.Fatalf("history prev = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1b[A"))
	if prompt.Text != "one" {
		t.Fatalf("history prev again = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1b[B"))
	prompt.Apply(ParseKey("\x1b[B"))
	if prompt.Text != "d" {
		t.Fatalf("draft = %#v", prompt)
	}
}

func TestPromptHandlesBracketedPasteAndImageHints(t *testing.T) {
	prompt := NewPromptState(nil)
	paste := ParseKey("\x1b[200~hello\nworld\x1b[201~")
	if paste.Type != KeyPaste || paste.Text != "hello\nworld" {
		t.Fatalf("paste key = %#v", paste)
	}
	prompt.Apply(paste)
	if prompt.Text != "hello\nworld" || prompt.Cursor != len([]rune("hello\nworld")) {
		t.Fatalf("prompt after paste = %#v", prompt)
	}
	image := ParseKey("\x1b]1337;File=name=chart.png;inline=1:AAAA\a")
	if image.Type != KeyImageHint || image.Text != "[Image: chart.png]" {
		t.Fatalf("image key = %#v", image)
	}
	prompt.Apply(image)
	if !strings.Contains(prompt.Text, "[Image: chart.png]") {
		t.Fatalf("prompt after image = %#v", prompt)
	}
}

func TestPromptPasteReferencesCanStoreAndExpandPastedContent(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.EnablePasteReferences()
	prompt.Apply(ParseKey("\x1b[200~hello\nworld\x1b[201~"))
	if prompt.Text != "[Pasted text #1 +1 lines]" || prompt.ExpandedText() != "hello\nworld" {
		t.Fatalf("prompt = %#v expanded=%q", prompt, prompt.ExpandedText())
	}
	entry := prompt.HistoryEntry()
	if entry.Display != "[Pasted text #1 +1 lines]" || entry.PastedContents[1].Content != "hello\nworld" {
		t.Fatalf("history entry = %#v", entry)
	}
	result := prompt.Apply(ParseKey("\n"))
	if result.Submitted != "hello\nworld" || result.Display != "[Pasted text #1 +1 lines]" || result.PastedContents[1].Type != session.PastedContentText {
		t.Fatalf("result = %#v", result)
	}
	if len(prompt.PastedContents) != 0 || prompt.NextPastedID != 1 {
		t.Fatalf("pasted contents should reset: %#v next=%d", prompt.PastedContents, prompt.NextPastedID)
	}
}

func TestPromptPasteReferencesSurviveDraftHistoryNavigation(t *testing.T) {
	prompt := NewPromptState([]string{"old"})
	prompt.EnablePasteReferences()
	prompt.Apply(ParseKey("\x1b[200~draft\npaste\x1b[201~"))
	prompt.Apply(ParseKey("\x1b[A"))
	if prompt.Text != "old" || prompt.ExpandedText() != "old" {
		t.Fatalf("history entry = %#v expanded=%q", prompt, prompt.ExpandedText())
	}
	prompt.Apply(ParseKey("\x1b[B"))
	if prompt.Text != "[Pasted text #1 +1 lines]" || prompt.ExpandedText() != "draft\npaste" {
		t.Fatalf("draft = %#v expanded=%q", prompt, prompt.ExpandedText())
	}
}

func TestParseImageHintUsesGenericPlaceholder(t *testing.T) {
	key := ParseKey("\x1b]1337;File=inline=1:AAAA\a")
	if key.Type != KeyImageHint || key.Text != ImageHintPlaceholder {
		t.Fatalf("key = %#v", key)
	}
}

func TestParseSGRMouse(t *testing.T) {
	press := ParseKey("\x1b[<64;10;4M")
	if press.Type != KeyMouse || press.MouseButton != 64 || press.MouseX != 10 || press.MouseY != 4 || press.MouseRelease {
		t.Fatalf("press = %#v", press)
	}
	release := ParseKey("\x1b[<0;1;2m")
	if release.Type != KeyMouse || release.MouseButton != 0 || release.MouseX != 1 || release.MouseY != 2 || !release.MouseRelease {
		t.Fatalf("release = %#v", release)
	}
}

func TestParseFocusEvents(t *testing.T) {
	if key := ParseKey("\x1b[I"); key.Type != KeyFocusIn {
		t.Fatalf("focus in = %#v", key)
	}
	if key := ParseKey("\x1b[O"); key.Type != KeyFocusOut {
		t.Fatalf("focus out = %#v", key)
	}
}

func TestRendererIncludesStatusPromptAndDialog(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.Text = "hello"
	prompt.Cursor = 5
	output := RenderOnce(32, 8, Frame{
		Messages: []Message{
			{Role: RoleUser, Text: "Please edit the file"},
			{Role: RoleAssistant, Text: "I will inspect it first"},
		},
		Status: "sonnet | 12%",
		Prompt: prompt,
		Dialog: &Dialog{
			Title:   "Permission",
			Body:    "Allow Edit on /tmp/a.txt?",
			Actions: []string{"Allow", "Deny"},
			Focused: 0,
		},
		ShowCursor: true,
	})
	if !strings.Contains(output, "\x1b[2J") || !strings.Contains(output, "Permission") || !strings.Contains(output, "[Allow]") {
		t.Fatalf("output = %q", output)
	}
	if !strings.Contains(output, "\x1b[8;8H") {
		t.Fatalf("cursor position missing: %q", output)
	}
}

func TestRenderMessagesWrapsWithRolePrefix(t *testing.T) {
	lines := RenderMessages([]Message{{Role: RoleAssistant, Text: "alpha beta gamma"}}, 18)
	if len(lines) < 2 {
		t.Fatalf("lines = %#v", lines)
	}
	if !strings.HasPrefix(lines[0], "assistant:") || strings.HasPrefix(strings.TrimLeft(lines[1], " "), "assistant:") {
		t.Fatalf("wrapped lines = %#v", lines)
	}
}

func TestViewportScrollsAndClamps(t *testing.T) {
	v := NewViewport([]string{"1", "2", "3", "4", "5"}, 3)
	if got := strings.Join(v.Visible(), ","); got != "3,4,5" {
		t.Fatalf("bottom visible = %s", got)
	}
	v.Scroll(-2)
	if got := strings.Join(v.Visible(), ","); got != "1,2,3" {
		t.Fatalf("scrolled visible = %s", got)
	}
	v.Page(10)
	if got := strings.Join(v.Visible(), ","); got != "3,4,5" {
		t.Fatalf("paged visible = %s", got)
	}
	v.HalfPage(-1)
	if got := strings.Join(v.Visible(), ","); got != "2,3,4" {
		t.Fatalf("half-paged visible = %s", got)
	}
	v.ScrollToTop()
	if got := strings.Join(v.Visible(), ","); got != "1,2,3" {
		t.Fatalf("top visible = %s", got)
	}
	v.ScrollToBottom()
	if got := strings.Join(v.Visible(), ","); got != "3,4,5" {
		t.Fatalf("bottom visible after top = %s", got)
	}
}

func TestSelectionMovesAndRendersFocus(t *testing.T) {
	s := NewSelection([]string{"one", "two", "three", "four"})
	s.Move(2)
	current, ok := s.Current()
	if !ok || current != "three" {
		t.Fatalf("current = %q ok=%v", current, ok)
	}
	lines := s.Render(12, 3)
	if len(lines) != 3 || !strings.HasPrefix(lines[1], "> three") {
		t.Fatalf("lines = %#v", lines)
	}
	s.Move(100)
	current, _ = s.Current()
	if current != "four" {
		t.Fatalf("clamped current = %q", current)
	}
}

func TestKeymapResolvesDefaultActions(t *testing.T) {
	keymap := DefaultKeymap()
	if action := keymap.Resolve(ParseKey("\x12")); action != ActionReverseSearch {
		t.Fatalf("ctrl-r action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x02")); action != ActionMoveLeft {
		t.Fatalf("ctrl-b action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1bb")); action != ActionMoveWordLeft {
		t.Fatalf("alt-b action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x06")); action != ActionMoveRight {
		t.Fatalf("ctrl-f action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1bf")); action != ActionMoveWordRight {
		t.Fatalf("alt-f action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x07")); action != ActionExternalEditor {
		t.Fatalf("ctrl-g action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x04")); action != ActionExit {
		t.Fatalf("ctrl-d action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x15")); action != ActionDeleteToStart {
		t.Fatalf("ctrl-u action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0b")); action != ActionDeleteToEnd {
		t.Fatalf("ctrl-k action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0c")); action != ActionRedraw {
		t.Fatalf("ctrl-l action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0f")); action != ActionToggleTranscript {
		t.Fatalf("ctrl-o action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x13")); action != ActionStashPrompt {
		t.Fatalf("ctrl-s action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x14")); action != ActionToggleTodos {
		t.Fatalf("ctrl-t action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x17")); action != ActionDeleteWordBack {
		t.Fatalf("ctrl-w action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b\x7f")); action != ActionDeleteWordBack {
		t.Fatalf("alt-backspace action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1bd")); action != ActionDeleteWordFwd {
		t.Fatalf("alt-d action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x19")); action != ActionYank {
		t.Fatalf("ctrl-y action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1by")); action != ActionYankPop {
		t.Fatalf("alt-y action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("x")); action != ActionInsertRune {
		t.Fatalf("rune action = %q", action)
	}

	keymap = DefaultKeymap()
	if action := keymap.Resolve(ParseKey("\x18")); action != ActionNone {
		t.Fatalf("ctrl-x prefix action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x05")); action != ActionExternalEditor {
		t.Fatalf("ctrl-x ctrl-e action = %q", action)
	}
	keymap = DefaultKeymap()
	if action := keymap.Resolve(ParseKey("\x18")); action != ActionNone {
		t.Fatalf("ctrl-x prefix before kill action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0b")); action != ActionKillAgents {
		t.Fatalf("ctrl-x ctrl-k action = %q", action)
	}
}

func TestKeymapFromSpecsOverridesAndRemovesBindings(t *testing.T) {
	keymap, err := KeymapFromSpecs(DefaultKeymap(), []BindingSpec{
		{Key: "ctrl-r", Action: ActionPageUp},
		{Key: "esc", Action: ActionNone},
		{Key: "focus-in", Action: ActionReverseSearch},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\x12")); action != ActionPageUp {
		t.Fatalf("ctrl-r action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b")); action != ActionNone {
		t.Fatalf("esc action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b[I")); action != ActionReverseSearch {
		t.Fatalf("focus-in action = %q", action)
	}
	for _, name := range []string{"paste", "image-hint", "mouse", "focus-out", "alt-b", "alt-d", "alt-f", "alt-y", "alt-backspace", "meta-b", "meta-d", "meta-f", "meta-y", "meta-backspace", "ctrl-b", "ctrl-d", "ctrl-f", "ctrl-g", "ctrl-u", "ctrl-k", "ctrl-l", "ctrl-o", "ctrl-s", "ctrl-t", "ctrl-w", "ctrl-x", "ctrl-y"} {
		if key, err := ParseKeyName(name); err != nil || key == KeyUnknown {
			t.Fatalf("ParseKeyName(%q) = %q, %v", name, key, err)
		}
	}
	if _, err := KeymapFromSpecs(DefaultKeymap(), []BindingSpec{{Key: "wat", Action: ActionCancel}}); err == nil {
		t.Fatal("expected unknown key error")
	}
}

func TestKeymapResolvesChordBindings(t *testing.T) {
	keymap, err := KeymapFromSpecs(DefaultKeymap(), []BindingSpec{
		{Key: "ctrl-r enter", Action: ActionPageDown},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\x12")); action != ActionNone {
		t.Fatalf("first chord action = %q", action)
	}
	if len(keymap.PendingChord) != 1 || keymap.PendingChord[0] != KeyCtrlR {
		t.Fatalf("pending chord = %#v", keymap.PendingChord)
	}
	if action := keymap.Resolve(ParseKey("\n")); action != ActionPageDown {
		t.Fatalf("second chord action = %q", action)
	}
	if len(keymap.PendingChord) != 0 {
		t.Fatalf("pending chord after exact = %#v", keymap.PendingChord)
	}

	keymap, err = KeymapFromSpecs(DefaultKeymap(), []BindingSpec{
		{Key: "ctrl-r enter", Action: ActionPageDown},
		{Key: "ctrl-r enter", Action: ActionNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\x12")); action != ActionReverseSearch {
		t.Fatalf("single key should win after chord removal: %q", action)
	}
}

func TestReverseSearchFiltersNewestFirstAndSelects(t *testing.T) {
	results := FilterHistoryForReverseSearch([]string{"deploy old", "test", "deploy new", "deploy old"}, "deploy", 10)
	if strings.Join(results, ",") != "deploy old,deploy new" {
		t.Fatalf("results = %#v", results)
	}
	screen := NewREPLScreen(40, 8, []string{"deploy old", "test", "deploy new"})
	event := screen.ApplyKey(ParseKey("\x12"))
	if event.Type != ScreenEventReverseSearch || !screen.ReverseSearch.Active {
		t.Fatalf("event = %#v state=%#v", event, screen.ReverseSearch)
	}
	for _, seq := range []string{"d", "e", "p"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.ReverseSearch.Query != "dep" || len(screen.ReverseSearch.Results) != 2 || screen.ReverseSearch.Results[0] != "deploy new" {
		t.Fatalf("reverse state = %#v", screen.ReverseSearch)
	}
	output := screen.Render()
	if !strings.Contains(output, "(reverse-i-search) `dep': deploy new") {
		t.Fatalf("reverse search render = %q", output)
	}
	selected := screen.ApplyKey(ParseKey("\n"))
	if selected.Type != ScreenEventReverseSelected || selected.Value != "deploy new" || screen.Prompt.Text != "deploy new" || screen.ReverseSearch.Active {
		t.Fatalf("selected = %#v prompt=%#v state=%#v", selected, screen.Prompt, screen.ReverseSearch)
	}

	scriptScreen := NewREPLScreen(40, 8, []string{"deploy old", "test", "deploy new"})
	cursor := len([]rune("dep"))
	_, err := RunInteractionScriptChecked(&scriptScreen, []ScriptStep{
		{
			Keys: []string{"\x12", "d", "e", "p"},
			ExpectReverseSearch: &ReverseSearchExpectation{
				Active:      true,
				Query:       "dep",
				Cursor:      &cursor,
				Current:     "deploy new",
				ResultCount: 2,
			},
			ExpectSnapshotContains: []string{"(reverse-i-search) `dep': deploy new"},
		},
		{
			Key:         "\n",
			ExpectEvent: &ScreenEvent{Type: ScreenEventReverseSelected, Value: "deploy new"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	scriptCursorScreen := NewREPLScreen(40, 8, []string{"alpha beta"})
	searchCursor := len([]rune("alpha "))
	_, err = RunInteractionScriptChecked(&scriptCursorScreen, []ScriptStep{
		{
			Keys: []string{"\x12", "a", "l", "p", "h", "a", "b", "e", "t", "a", "\x1b[D", "\x1b[D", "\x1b[D", "\x1b[D", " "},
			ExpectReverseSearch: &ReverseSearchExpectation{
				Active: true,
				Query:  "alpha beta",
				Cursor: &searchCursor,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	emptyScreen := NewREPLScreen(40, 8, []string{"deploy old", "test"})
	_, err = RunInteractionScriptChecked(&emptyScreen, []ScriptStep{
		{
			Keys: []string{"\x12", "z", "z", "z"},
			ExpectReverseSearch: &ReverseSearchExpectation{
				Active:    true,
				Query:     "zzz",
				NoResults: true,
			},
			ExpectSnapshotContains: []string{"(reverse-i-search) `zzz': no matches"},
		},
		{
			Key:                 "\x1b",
			ExpectEvent:         &ScreenEvent{Type: ScreenEventCancelled},
			ExpectReverseSearch: &ReverseSearchExpectation{Active: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPermissionAndTaskDialogs(t *testing.T) {
	permission := PermissionDialog(PermissionRequest{
		ID:          "perm_1",
		ToolName:    "Edit",
		Path:        "/tmp/a.txt",
		Description: "Modify file contents.",
	})
	if permission.Title != "Permission" || permission.ID != "perm_1" || permission.Kind != DialogPermission || !strings.Contains(permission.Body, "Tool: Edit") || len(permission.Actions) != 3 {
		t.Fatalf("permission = %#v", permission)
	}
	tasks := TaskDialog([]TaskStatus{{ID: "task_1", Title: "Search", State: "running", Detail: "grep", Progress: 42}})
	if tasks.Title != "Tasks" || tasks.Kind != DialogTask || !strings.Contains(tasks.Body, "Search [running] 42% - grep") {
		t.Fatalf("tasks = %#v", tasks)
	}
}

func TestREPLScreenSubmitsPromptAndRendersMessages(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.Status = "ready"
	screen.AppendMessage(Message{Role: RoleAssistant, Text: "hello from assistant"})
	for _, seq := range []string{"r", "u", "n"} {
		event := screen.ApplyKey(ParseKey(seq))
		if event.Type != ScreenEventNone {
			t.Fatalf("unexpected event = %#v", event)
		}
	}
	event := screen.ApplyKey(ParseKey("\n"))
	if event.Type != ScreenEventPromptSubmitted || event.Value != "run" {
		t.Fatalf("submit event = %#v", event)
	}
	output := screen.Render()
	if !strings.Contains(output, "assistant: hello from assistant") || !strings.Contains(output, "ready") {
		t.Fatalf("output = %q", output)
	}
}

func TestREPLScreenSubmitsExpandedPasteReferences(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.ApplyKey(ParseKey("\x1b[200~alpha\nbeta\x1b[201~"))
	if screen.Prompt.Text != "[Pasted text #1 +1 lines]" {
		t.Fatalf("prompt = %#v", screen.Prompt)
	}
	event := screen.ApplyKey(ParseKey("\n"))
	if event.Type != ScreenEventPromptSubmitted || event.Value != "alpha\nbeta" {
		t.Fatalf("event = %#v", event)
	}
}

func TestREPLScreenDialogFocusAndConfirm(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.Dialog = &Dialog{Title: "Permission", Body: "Allow?", Actions: []string{"Allow", "Deny"}, ID: "perm_1", Kind: DialogPermission}
	screen.ApplyKey(ParseKey("x"))
	if screen.Prompt.Text != "" {
		t.Fatalf("dialog input should not edit prompt: %#v", screen.Prompt)
	}
	screen.ApplyKey(ParseKey("\t"))
	if screen.Dialog.Focused != 1 {
		t.Fatalf("focused = %d", screen.Dialog.Focused)
	}
	event := screen.ApplyKey(ParseKey("\n"))
	if event.Type != ScreenEventDialogAction || event.Value != "Deny" || event.DialogID != "perm_1" || event.DialogKind != DialogPermission {
		t.Fatalf("dialog event = %#v", event)
	}
	if screen.Dialog != nil {
		t.Fatalf("dialog should close")
	}

	screen.Dialog = &Dialog{Title: "Permission", Body: "Allow?", Actions: []string{"Allow", "Deny"}, ID: "perm_2", Kind: DialogPermission}
	click := screen.ApplyKey(ParseKey("\x1b[<0;13;5M"))
	if click.Type != ScreenEventDialogAction || click.Value != "Deny" || click.DialogID != "perm_2" || click.DialogKind != DialogPermission {
		t.Fatalf("dialog mouse click = %#v", click)
	}
	if screen.Dialog != nil {
		t.Fatalf("dialog should close after mouse click")
	}
}

func TestREPLScreenRedrawEvent(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	event := screen.ApplyKey(ParseKey("\x0c"))
	if event.Type != ScreenEventRedraw {
		t.Fatalf("redraw event = %#v", event)
	}

	screen.Dialog = &Dialog{Title: "Permission", Body: "Allow?", Actions: []string{"Allow"}}
	event = screen.ApplyKey(ParseKey("\x0c"))
	if event.Type != ScreenEventRedraw || screen.Dialog == nil {
		t.Fatalf("dialog redraw event = %#v dialog=%#v", event, screen.Dialog)
	}
}

func TestREPLScreenGlobalToggleEvents(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	for _, tc := range []struct {
		seq  string
		want ScreenEventType
	}{
		{seq: "\x0f", want: ScreenEventToggleTranscript},
		{seq: "\x14", want: ScreenEventToggleTodos},
	} {
		event := screen.ApplyKey(ParseKey(tc.seq))
		if event.Type != tc.want {
			t.Fatalf("event for %q = %#v, want %s", tc.seq, event, tc.want)
		}
	}
}

func TestREPLScreenChatControlEvents(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	for _, tc := range []struct {
		seqs []string
		want ScreenEventType
	}{
		{seqs: []string{"\x07"}, want: ScreenEventExternalEditor},
		{seqs: []string{"\x13"}, want: ScreenEventStashPrompt},
		{seqs: []string{"\x18", "\x05"}, want: ScreenEventExternalEditor},
		{seqs: []string{"\x18", "\x0b"}, want: ScreenEventKillAgents},
	} {
		var event ScreenEvent
		for _, seq := range tc.seqs {
			event = screen.ApplyKey(ParseKey(seq))
		}
		if event.Type != tc.want {
			t.Fatalf("event for %#v = %#v, want %s", tc.seqs, event, tc.want)
		}
	}
}

func TestREPLScreenCtrlDDeletesForwardOrDoublePressExits(t *testing.T) {
	now := time.Unix(100, 0)
	screen := NewREPLScreen(40, 8, nil)
	screen.Now = func() time.Time { return now }
	typePromptText(&screen, "abc")
	screen.ApplyKey(ParseKey("\x1b[D"))
	event := screen.ApplyKey(ParseKey("\x04"))
	if event.Type != ScreenEventNone || screen.Prompt.Text != "ab" || screen.Prompt.Cursor != 2 {
		t.Fatalf("ctrl-d delete event=%#v prompt=%#v", event, screen.Prompt)
	}

	screen.Prompt.Text = ""
	screen.Prompt.Cursor = 0
	event = screen.ApplyKey(ParseKey("\x04"))
	if event.Type != ScreenEventExitPending || event.Value != "Ctrl-D" {
		t.Fatalf("first ctrl-d = %#v", event)
	}
	now = now.Add(DoublePressTimeout)
	event = screen.ApplyKey(ParseKey("\x04"))
	if event.Type != ScreenEventExit {
		t.Fatalf("second ctrl-d = %#v", event)
	}

	now = now.Add(DoublePressTimeout + time.Millisecond)
	event = screen.ApplyKey(ParseKey("\x04"))
	if event.Type != ScreenEventExitPending {
		t.Fatalf("expired first ctrl-d = %#v", event)
	}
	now = now.Add(DoublePressTimeout + time.Millisecond)
	event = screen.ApplyKey(ParseKey("\x04"))
	if event.Type != ScreenEventExitPending {
		t.Fatalf("expired second ctrl-d should re-arm = %#v", event)
	}
}

func TestREPLScreenCtrlCInterruptThenDoublePressExits(t *testing.T) {
	now := time.Unix(200, 0)
	screen := NewREPLScreen(40, 8, nil)
	screen.Now = func() time.Time { return now }
	event := screen.ApplyKey(ParseKey("\x03"))
	if event.Type != ScreenEventInterrupted {
		t.Fatalf("first ctrl-c = %#v", event)
	}
	now = now.Add(DoublePressTimeout)
	event = screen.ApplyKey(ParseKey("\x03"))
	if event.Type != ScreenEventExit {
		t.Fatalf("second ctrl-c = %#v", event)
	}

	screen.Dialog = &Dialog{Title: "Permission", Body: "Allow?", Actions: []string{"Allow"}}
	now = now.Add(DoublePressTimeout + time.Millisecond)
	event = screen.ApplyKey(ParseKey("\x03"))
	if event.Type != ScreenEventExitPending || event.Value != "Ctrl-C" || screen.Dialog == nil {
		t.Fatalf("dialog first ctrl-c = %#v dialog=%#v", event, screen.Dialog)
	}
	now = now.Add(time.Millisecond)
	event = screen.ApplyKey(ParseKey("\x03"))
	if event.Type != ScreenEventExit {
		t.Fatalf("dialog second ctrl-c = %#v", event)
	}
}

func TestDialogRuntimeResolvesPermissionAndTasks(t *testing.T) {
	runtime := NewDialogRuntime()
	dialog := runtime.RequestPermission(PermissionRequest{ID: "perm_1", ToolName: "Write"})
	if runtime.Active == nil || dialog.ID != "perm_1" || len(runtime.Permissions) != 1 {
		t.Fatalf("runtime = %#v dialog = %#v", runtime, dialog)
	}
	result := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow Session", DialogID: "perm_1", DialogKind: DialogPermission})
	if !result.Found || result.Status != DialogResultAllowed || result.Action != "Allow Session" {
		t.Fatalf("result = %#v", result)
	}
	if len(runtime.Permissions) != 0 || runtime.Active != nil {
		t.Fatalf("runtime after resolve = %#v", runtime)
	}
	missing := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow", DialogID: "perm_1", DialogKind: DialogPermission})
	if missing.Found {
		t.Fatalf("stale permission event should be ignored: %#v", missing)
	}

	runtime.UpsertTask(TaskStatus{ID: "b", Title: "Done", State: "completed"})
	runtime.UpsertTask(TaskStatus{ID: "a", Title: "Run", State: "running"})
	tasks := runtime.SortedTasks()
	if len(tasks) != 2 || tasks[0].ID != "a" {
		t.Fatalf("tasks = %#v", tasks)
	}
	taskDialog := runtime.OpenTasksDialog()
	if taskDialog.Kind != DialogTask || !strings.Contains(taskDialog.Body, "Run [running]") {
		t.Fatalf("task dialog = %#v", taskDialog)
	}
	closed := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Close", DialogID: "tasks", DialogKind: DialogTask})
	if !closed.Found || closed.Status != DialogResultClosed {
		t.Fatalf("closed = %#v", closed)
	}
	status := runtime.StatusLine("ready")
	if !strings.Contains(status, "running: 1") || !strings.Contains(status, "completed: 1") {
		t.Fatalf("status = %q", status)
	}
	if got := NewDialogRuntime().StatusLine(""); got != "ready" {
		t.Fatalf("empty status = %q", got)
	}
	runtime.RequestPermission(PermissionRequest{ID: "perm_status", ToolName: "Edit"})
	runtime.UpsertTask(TaskStatus{ID: "pending", State: TaskPending})
	status = runtime.StatusLine("")
	if !strings.Contains(status, "dialog: permission") || !strings.Contains(status, "permissions: 1") || !strings.Contains(status, "pending: 1") {
		t.Fatalf("permission status = %q", status)
	}
}

func TestDialogRuntimeTaskLifecycle(t *testing.T) {
	runtime := NewDialogRuntime()
	running := runtime.StartTask("task_1", "Search", "starting")
	if running.State != TaskRunning || running.Progress != 0 {
		t.Fatalf("running = %#v", running)
	}
	progress := runtime.UpdateTaskProgress("task_1", "halfway", 50)
	if progress.State != TaskRunning || progress.Detail != "halfway" || progress.Progress != 50 {
		t.Fatalf("progress = %#v", progress)
	}
	done := runtime.CompleteTask("task_1", "done")
	if done.State != TaskCompleted || done.Progress != 100 {
		t.Fatalf("done = %#v", done)
	}
	failed := runtime.FailTask("task_2", "boom")
	if failed.State != TaskFailed || failed.Title != "task_2" {
		t.Fatalf("failed = %#v", failed)
	}
	cancelled := runtime.CancelTask("task_3", "stopped")
	if cancelled.State != TaskCancelled || cancelled.Detail != "stopped" {
		t.Fatalf("cancelled = %#v", cancelled)
	}
}

func TestDialogRuntimeAppliesToScreenAndResolvesEvents(t *testing.T) {
	runtime := NewDialogRuntime()
	screen := NewREPLScreen(40, 8, nil)
	runtime.StartTask("task_1", "Search", "running ripgrep")
	runtime.RequestPermission(PermissionRequest{ID: "perm_1", ToolName: "Bash", Path: "/tmp/project"})
	runtime.ApplyToScreen(&screen, "ready")
	if screen.Dialog == nil || screen.Dialog.ID != "perm_1" || screen.Dialog.Kind != DialogPermission {
		t.Fatalf("screen dialog = %#v", screen.Dialog)
	}
	if !strings.Contains(screen.Status, "ready") || !strings.Contains(screen.Status, "permissions: 1") || !strings.Contains(screen.Status, "running: 1") {
		t.Fatalf("status = %q", screen.Status)
	}
	screen.ApplyKey(ParseKey("\t"))
	event := screen.ApplyKey(ParseKey("\n"))
	result := runtime.ResolveScreenEvent(&screen, event, "ready")
	if !result.Found || result.Status != DialogResultAllowed || result.Action != "Allow Session" {
		t.Fatalf("result = %#v", result)
	}
	if screen.Dialog != nil {
		t.Fatalf("dialog should be cleared: %#v", screen.Dialog)
	}
	if strings.Contains(screen.Status, "permissions:") || !strings.Contains(screen.Status, "running: 1") {
		t.Fatalf("status after resolve = %q", screen.Status)
	}
	runtime.CompleteTask("task_1", "done")
	runtime.ApplyToScreen(&screen, "ready")
	if !strings.Contains(screen.Status, "completed: 1") {
		t.Fatalf("completed status = %q", screen.Status)
	}
}

func TestDialogRuntimeInteractionScriptResolvesPermissionFlow(t *testing.T) {
	runtime := NewDialogRuntime()
	screen := NewREPLScreen(42, 8, nil)
	runtime.StartTask("task_1", "Search", "running ripgrep")
	runtime.RequestPermission(PermissionRequest{ID: "perm_1", ToolName: "Bash", Path: "/tmp/project"})
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", []ScriptStep{
		{
			ExpectDialog:         &DialogExpectation{Active: true, ID: "perm_1", Kind: DialogPermission, Title: "Permission"},
			ExpectStatusContains: []string{"ready", "permissions: 1", "running: 1"},
			ExpectSnapshotContains: []string{
				"Tool: Bash",
			},
		},
		{
			Keys:                 []string{"\t", "\n"},
			ExpectEvent:          &ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow Session", DialogID: "perm_1", DialogKind: DialogPermission},
			ExpectDialog:         &DialogExpectation{Active: false},
			ExpectStatusContains: []string{"ready", "running: 1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].Status != DialogResultAllowed {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
	if len(result.Events) != 1 || result.Events[0].Value != "Allow Session" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestDialogRuntimeInteractionScriptChecksTasks(t *testing.T) {
	runtime := NewDialogRuntime()
	screen := NewREPLScreen(52, 9, nil)
	runtime.StartTask("task_1", "Search", "starting")
	runtime.UpdateTaskProgress("task_1", "halfway", 50)
	runtime.CompleteTask("task_2", "summarized")
	count := 2
	progress := 50
	completed := 100
	taskChecks := &TasksExpectation{
		Count: &count,
		StateCounts: map[string]int{
			TaskRunning:   1,
			TaskCompleted: 1,
		},
		Contains: []TaskExpectation{
			{ID: "task_1", Title: "Search", State: TaskRunning, Detail: "halfway", Progress: &progress},
			{ID: "task_2", Title: "task_2", State: TaskCompleted, Detail: "summarized", Progress: &completed},
		},
	}
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", []ScriptStep{
		{
			ExpectTasks:          taskChecks,
			ExpectStatusContains: []string{"ready", "running: 1", "completed: 1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 0 {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
	runtime.OpenTasksDialog()
	result, err = RunDialogRuntimeScriptChecked(&screen, runtime, "ready", []ScriptStep{
		{
			ExpectDialog: &DialogExpectation{Active: true, ID: "tasks", Kind: DialogTask, Title: "Tasks"},
			ExpectTasks:  taskChecks,
			ExpectSnapshotContains: []string{
				"Search [running] 50% - halfway",
				"task_2 [completed] 100% - summarized",
			},
		},
		{
			Key:                  "\n",
			ExpectEvent:          &ScreenEvent{Type: ScreenEventDialogAction, Value: "Close", DialogID: "tasks", DialogKind: DialogTask},
			ExpectDialog:         &DialogExpectation{Active: false},
			ExpectTasks:          taskChecks,
			ExpectStatusContains: []string{"ready", "running: 1", "completed: 1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].Status != DialogResultClosed {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
}

func TestDialogRuntimeIgnoresStalePermissionEventsAndCancelsActive(t *testing.T) {
	runtime := NewDialogRuntime()
	runtime.RequestPermission(PermissionRequest{ID: "old", ToolName: "Write"})
	runtime.RequestPermission(PermissionRequest{ID: "new", ToolName: "Edit"})
	stale := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow", DialogID: "old", DialogKind: DialogPermission})
	if !stale.Stale || stale.Found {
		t.Fatalf("stale = %#v", stale)
	}
	if _, ok := runtime.Permissions["old"]; !ok {
		t.Fatalf("old permission should remain pending until explicitly replaced or cancelled: %#v", runtime.Permissions)
	}
	cancelled := runtime.CancelActive()
	if !cancelled.Found || cancelled.Status != DialogResultCancelled || cancelled.ID != "new" {
		t.Fatalf("cancelled = %#v", cancelled)
	}
	if _, ok := runtime.Permissions["new"]; ok || runtime.Active != nil {
		t.Fatalf("runtime after cancel = %#v", runtime)
	}
}

func TestDialogRuntimeCancelsPendingPermissionByID(t *testing.T) {
	runtime := NewDialogRuntime()
	runtime.RequestPermission(PermissionRequest{ID: "old", ToolName: "Write"})
	runtime.RequestPermission(PermissionRequest{ID: "new", ToolName: "Edit"})
	cancelledOld := runtime.CancelPermission("old")
	if !cancelledOld.Found || cancelledOld.Status != DialogResultCancelled || cancelledOld.ID != "old" {
		t.Fatalf("cancelled old = %#v", cancelledOld)
	}
	if _, ok := runtime.Permissions["old"]; ok {
		t.Fatalf("old permission should be removed: %#v", runtime.Permissions)
	}
	if runtime.Active == nil || runtime.Active.ID != "new" {
		t.Fatalf("active permission should remain new: %#v", runtime.Active)
	}
	missing := runtime.CancelPermission("old")
	if missing.Found || missing.Status != DialogResultNone {
		t.Fatalf("missing cancel = %#v", missing)
	}
	cancelledActive := runtime.CancelPermission("")
	if !cancelledActive.Found || cancelledActive.Status != DialogResultCancelled || cancelledActive.ID != "new" {
		t.Fatalf("cancelled active = %#v", cancelledActive)
	}
	if runtime.Active != nil || len(runtime.Permissions) != 0 {
		t.Fatalf("runtime after active cancel = %#v", runtime)
	}
}

func TestDialogRuntimeCancelsAllPermissionsDeterministically(t *testing.T) {
	runtime := NewDialogRuntime()
	runtime.RequestPermission(PermissionRequest{ID: "b", ToolName: "Write"})
	runtime.RequestPermission(PermissionRequest{ID: "a", ToolName: "Edit"})
	results := runtime.CancelPermissions()
	if len(results) != 2 || results[0].ID != "a" || results[1].ID != "b" {
		t.Fatalf("results = %#v", results)
	}
	for _, result := range results {
		if !result.Found || result.Status != DialogResultCancelled || result.Kind != DialogPermission {
			t.Fatalf("result = %#v", result)
		}
	}
	if runtime.Active != nil || len(runtime.Permissions) != 0 {
		t.Fatalf("runtime after cancel all = %#v", runtime)
	}
	if got := runtime.CancelPermissions(); got != nil {
		t.Fatalf("empty cancel all = %#v", got)
	}
}

func TestREPLScreenViewportScrolls(t *testing.T) {
	screen := NewREPLScreen(20, 6, nil)
	screen.SetMessages([]Message{
		{Role: RoleSystem, Text: "one"},
		{Role: RoleSystem, Text: "two"},
		{Role: RoleSystem, Text: "three"},
		{Role: RoleSystem, Text: "four"},
		{Role: RoleSystem, Text: "five"},
	})
	before := strings.Join(screen.Viewport.Visible(), "\n")
	screen.ApplyKey(ParseKey("\x1b[5~"))
	after := strings.Join(screen.Viewport.Visible(), "\n")
	if before == after || !strings.Contains(after, "one") {
		t.Fatalf("before=%q after=%q", before, after)
	}
	screen.ApplyKey(ParseKey("\x1b[<65;4;4M"))
	scrolledDown := strings.Join(screen.Viewport.Visible(), "\n")
	if scrolledDown == after {
		t.Fatalf("mouse wheel down did not scroll: before=%q after=%q", after, scrolledDown)
	}
	screen.ApplyKey(ParseKey("\x1b[<64;4;4M"))
	scrolledUp := strings.Join(screen.Viewport.Visible(), "\n")
	if scrolledUp != after {
		t.Fatalf("mouse wheel up mismatch: after=%q scrolledUp=%q", after, scrolledUp)
	}
	click := screen.ApplyKey(ParseKey("\x1b[<0;1;2M"))
	if click.Type != ScreenEventViewportSelected || !strings.Contains(click.Value, "system:") || screen.SelectedViewportLine < 0 {
		t.Fatalf("viewport click = %#v selected=%d", click, screen.SelectedViewportLine)
	}
	statusClick := screen.ApplyKey(ParseKey("\x1b[<0;1;5M"))
	if statusClick.Type != ScreenEventNone {
		t.Fatalf("status click should not select viewport: %#v", statusClick)
	}
}

func TestREPLScreenConfiguredViewportScrollActions(t *testing.T) {
	keymap, err := KeymapFromSpecs(DefaultKeymap(), []BindingSpec{
		{Key: "ctrl-u", Action: ActionHalfPageUp},
		{Key: "ctrl-k", Action: ActionHalfPageDown},
		{Key: "ctrl-b", Action: ActionScrollToTop},
		{Key: "ctrl-f", Action: ActionScrollToBottom},
	})
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(20, 6, nil)
	screen.Keymap = keymap
	screen.SetMessages([]Message{
		{Role: RoleSystem, Text: "one"},
		{Role: RoleSystem, Text: "two"},
		{Role: RoleSystem, Text: "three"},
		{Role: RoleSystem, Text: "four"},
		{Role: RoleSystem, Text: "five"},
		{Role: RoleSystem, Text: "six"},
		{Role: RoleSystem, Text: "seven"},
		{Role: RoleSystem, Text: "eight"},
	})

	bottom := maxViewportOffset(screen.Viewport)
	if screen.Viewport.Offset != bottom {
		t.Fatalf("initial offset = %d, want bottom %d", screen.Viewport.Offset, bottom)
	}
	screen.ApplyKey(ParseKey("\x15"))
	if screen.Viewport.Offset != bottom-screen.Viewport.Height/2 {
		t.Fatalf("half-page up offset = %d", screen.Viewport.Offset)
	}
	screen.ApplyKey(ParseKey("\x02"))
	if screen.Viewport.Offset != 0 {
		t.Fatalf("scroll to top offset = %d", screen.Viewport.Offset)
	}
	screen.ApplyKey(ParseKey("\x0b"))
	if screen.Viewport.Offset != screen.Viewport.Height/2 {
		t.Fatalf("half-page down offset = %d", screen.Viewport.Offset)
	}
	screen.ApplyKey(ParseKey("\x06"))
	if screen.Viewport.Offset != bottom {
		t.Fatalf("scroll to bottom offset = %d, want %d", screen.Viewport.Offset, bottom)
	}
}

func TestREPLScreenFocusAndResizePreservesScroll(t *testing.T) {
	screen := NewREPLScreen(24, 6, nil)
	screen.SetMessages([]Message{
		{Role: RoleSystem, Text: "one"},
		{Role: RoleSystem, Text: "two"},
		{Role: RoleSystem, Text: "three"},
		{Role: RoleSystem, Text: "four"},
		{Role: RoleSystem, Text: "five"},
		{Role: RoleSystem, Text: "six"},
		{Role: RoleSystem, Text: "seven"},
		{Role: RoleSystem, Text: "eight"},
	})
	screen.ApplyKey(ParseKey("\x1b[5~"))
	before := strings.Join(screen.Viewport.Visible(), "\n")
	screen.Resize(24, 7)
	after := strings.Join(screen.Viewport.Visible(), "\n")
	if !strings.Contains(before, "system: one") || !strings.Contains(after, "system: one") {
		t.Fatalf("resize should preserve scrolled position: before=%q after=%q", before, after)
	}
	focusOut := screen.ApplyKey(ParseKey("\x1b[O"))
	if focusOut.Type != ScreenEventFocusOut || screen.Focused {
		t.Fatalf("focus out = %#v focused=%v", focusOut, screen.Focused)
	}
	focusIn := screen.ApplyKey(ParseKey("\x1b[I"))
	if focusIn.Type != ScreenEventFocusIn || !screen.Focused {
		t.Fatalf("focus in = %#v focused=%v", focusIn, screen.Focused)
	}
	screen.Viewport.ScrollToBottom()
	screen.Resize(24, 5)
	bottom := strings.Join(screen.Viewport.Visible(), "\n")
	if !strings.Contains(bottom, "system: eight") {
		t.Fatalf("bottom resize = %q", bottom)
	}
}

func TestREPLScreenVimNormalModeEditsPrompt(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"a", "b", "c"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	if screen.VimMode != VimNormal || screen.Prompt.Text != "abc" {
		t.Fatalf("screen = %#v", screen)
	}
	screen.ApplyKey(ParseKey("h"))
	screen.ApplyKey(ParseKey("x"))
	screen.ApplyKey(ParseKey("i"))
	screen.ApplyKey(ParseKey("Z"))
	if screen.VimMode != VimInsert || screen.Prompt.Text != "abZ" {
		t.Fatalf("screen = %#v", screen)
	}
}

func TestREPLScreenVimWordAndDeleteActions(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"a", "l", "p", "h", "a", " ", "b", "e", "t", "a", " ", "g", "a", "m", "m", "a"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(ParseKey("0"))
	screen.ApplyKey(ParseKey("w"))
	if screen.Prompt.Cursor != len([]rune("alpha ")) {
		t.Fatalf("cursor after w = %d", screen.Prompt.Cursor)
	}
	screen.ApplyKey(ParseKey("d"))
	screen.ApplyKey(ParseKey("w"))
	if screen.Prompt.Text != "alpha gamma" || screen.Prompt.Cursor != len([]rune("alpha ")) {
		t.Fatalf("after dw prompt = %#v", screen.Prompt)
	}
	screen.ApplyKey(ParseKey("C"))
	if screen.VimMode != VimInsert || screen.Prompt.Text != "alpha " {
		t.Fatalf("after C screen = %#v", screen)
	}
	for _, seq := range []string{"d", "e", "l", "t", "a"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(ParseKey("d"))
	screen.ApplyKey(ParseKey("d"))
	if screen.Prompt.Text != "" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after dd prompt = %#v", screen.Prompt)
	}
}

func TestREPLScreenVimCountsRepeatMotionsAndOperators(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"o", "n", "e", " ", "t", "w", "o", " ", "t", "h", "r", "e", "e", " ", "f", "o", "u", "r"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "3", "w"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("one two three ")) {
		t.Fatalf("cursor after 3w = %d", screen.Prompt.Cursor)
	}
	screen.ApplyKey(ParseKey("0"))
	for _, seq := range []string{"2", "x"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "e two three four" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after 2x prompt = %#v", screen.Prompt)
	}
	for _, seq := range []string{"d", "2", "w"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "three four" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after d2w prompt = %#v", screen.Prompt)
	}
	for _, seq := range []string{"2", "d", "w"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after 2dw prompt = %#v", screen.Prompt)
	}
}

func TestREPLScreenVimReplaceAndUndo(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"a", "b", "c", "d", "e", "f"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(ParseKey("0"))
	for _, seq := range []string{"2", "r", "Z"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "ZZcdef" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after replace prompt = %#v", screen.Prompt)
	}
	screen.ApplyKey(ParseKey("u"))
	if screen.Prompt.Text != "abcdef" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after undo replace prompt = %#v", screen.Prompt)
	}
	for _, seq := range []string{"3", "x"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "def" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after 3x prompt = %#v", screen.Prompt)
	}
	screen.ApplyKey(ParseKey("u"))
	if screen.Prompt.Text != "abcdef" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after undo delete prompt = %#v", screen.Prompt)
	}
}

func TestREPLScreenVimFindTillMotionsAndOperators(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"a", "l", "p", "h", "a", ",", "b", "e", "t", "a", ",", "g", "a", "m", "m", "a"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(ParseKey("0"))
	for _, seq := range []string{"f", ","} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("alpha")) {
		t.Fatalf("cursor after f, = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"t", "g"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("alpha,beta")) {
		t.Fatalf("cursor after tg = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"F", ","} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("alpha")) {
		t.Fatalf("cursor after F, = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"d", "f", ","} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "alphagamma" || screen.Prompt.Cursor != len([]rune("alpha")) {
		t.Fatalf("after df, prompt = %#v", screen.Prompt)
	}
	for _, seq := range []string{"c", "t", "m"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.VimMode != VimInsert || screen.Prompt.Text != "alphamma" || screen.Prompt.Cursor != len([]rune("alpha")) {
		t.Fatalf("after ctm screen = %#v", screen)
	}
}

func TestREPLScreenVimRepeatsFindTillMotions(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"a", ":", "b", ":", "c", ":", "d"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(ParseKey("0"))
	for _, seq := range []string{"f", ":", ";"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("a:b")) {
		t.Fatalf("cursor after ; = %d", screen.Prompt.Cursor)
	}
	screen.ApplyKey(ParseKey(","))
	if screen.Prompt.Cursor != len([]rune("a")) {
		t.Fatalf("cursor after , = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"2", ";"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("a:b:c")) {
		t.Fatalf("cursor after 2; = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"d", ","} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "a:bd" || screen.Prompt.Cursor != len([]rune("a:b")) {
		t.Fatalf("after d, prompt = %#v", screen.Prompt)
	}
}

func TestREPLScreenVimWORDMotionsAndFirstNonBlank(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{" ", " ", "f", "o", "o", ".", "b", "a", "r", " ", "b", "a", "z", "-", "q", "u", "x"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(ParseKey("^"))
	if screen.Prompt.Cursor != 2 {
		t.Fatalf("cursor after ^ = %d", screen.Prompt.Cursor)
	}
	screen.ApplyKey(ParseKey("W"))
	if screen.Prompt.Cursor != len([]rune("  foo.bar ")) {
		t.Fatalf("cursor after W = %d", screen.Prompt.Cursor)
	}
	screen.ApplyKey(ParseKey("E"))
	if screen.Prompt.Cursor != len([]rune("  foo.bar baz-qux"))-1 {
		t.Fatalf("cursor after E = %d", screen.Prompt.Cursor)
	}
	screen.ApplyKey(ParseKey("B"))
	if screen.Prompt.Cursor != len([]rune("  foo.bar ")) {
		t.Fatalf("cursor after B = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"d", "B"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "  baz-qux" || screen.Prompt.Cursor != 2 {
		t.Fatalf("after dB prompt = %#v", screen.Prompt)
	}
}

func TestREPLScreenVimBackwardEndMotions(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "alpha beta.gamma delta")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"$", "g", "e"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("alpha beta.gamma delta"))-1 {
		t.Fatalf("cursor after ge = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"g", "e"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("alpha beta.gamma"))-1 {
		t.Fatalf("cursor after second ge = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"g", "e"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("alpha beta"))-1 {
		t.Fatalf("cursor after third ge = %d", screen.Prompt.Cursor)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "foo.bar baz-qux tail")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"$", "g", "E"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("foo.bar baz-qux tail"))-1 {
		t.Fatalf("cursor after gE = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"2", "g", "E"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("foo.bar"))-1 {
		t.Fatalf("cursor after 2gE = %d", screen.Prompt.Cursor)
	}
}

func TestREPLScreenVimNormalModeSpecialKeys(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"a", "b", "c", "d"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(Key{Type: KeyLeft})
	screen.ApplyKey(Key{Type: KeyBackspace})
	screen.ApplyKey(Key{Type: KeyDelete})
	if screen.Prompt.Text != "abd" || screen.Prompt.Cursor != 2 {
		t.Fatalf("prompt after special keys = %#v", screen.Prompt)
	}
}

func TestREPLScreenVimWordTextObjects(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"a", "l", "p", "h", "a", " ", "b", "e", "t", "a", " ", "g", "a", "m", "m", "a"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(ParseKey("0"))
	screen.ApplyKey(ParseKey("w"))
	for _, seq := range []string{"d", "i", "w"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "alpha  gamma" || screen.Prompt.Cursor != len([]rune("alpha ")) {
		t.Fatalf("after diw prompt = %#v", screen.Prompt)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"a", "l", "p", "h", "a", " ", "b", "e", "t", "a", " ", "g", "a", "m", "m", "a"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(ParseKey("0"))
	screen.ApplyKey(ParseKey("w"))
	for _, seq := range []string{"d", "a", "w"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "alpha gamma" || screen.Prompt.Cursor != len([]rune("alpha ")) {
		t.Fatalf("after daw prompt = %#v", screen.Prompt)
	}
}

func TestREPLScreenVimWORDTextObjects(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"f", "o", "o", ".", "b", "a", "r", " ", "b", "a", "z"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(ParseKey("0"))
	for _, seq := range []string{"c", "i", "W"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.VimMode != VimInsert || screen.Prompt.Text != " baz" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after ciW screen = %#v", screen)
	}
}

func TestREPLScreenVimBracketTextObjects(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "call(alpha, beta) end")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "f", "(", "d", "i", "("} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "call() end" || screen.Prompt.Cursor != len([]rune("call(")) {
		t.Fatalf("after di( prompt = %#v", screen.Prompt)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "call(alpha, beta) end")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "f", "(", "d", "a", ")"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "call end" || screen.Prompt.Cursor != len([]rune("call")) {
		t.Fatalf("after da) prompt = %#v", screen.Prompt)
	}
}

func TestREPLScreenVimQuoteTextObjects(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, `say "hello world" now`)
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "f", "\"", "c", "i", "\""} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.VimMode != VimInsert || screen.Prompt.Text != `say "" now` || screen.Prompt.Cursor != len([]rune(`say "`)) {
		t.Fatalf("after ci\" screen = %#v", screen)
	}
}

func TestREPLScreenVimBraceTextObjectAliases(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "cfg {inner} tail")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "f", "{", "c", "i", "B"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.VimMode != VimInsert || screen.Prompt.Text != "cfg {} tail" || screen.Prompt.Cursor != len([]rune("cfg {")) {
		t.Fatalf("after ciB screen = %#v", screen)
	}
}

func TestREPLScreenVimYankPasteRegister(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "abc")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "y", "l", "$", "2", "p"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "abcaa" || screen.VimRegister != "a" || screen.Prompt.Cursor != len([]rune("abcaa"))-1 {
		t.Fatalf("after yl 2p screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "alpha beta")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "d", "w", "P"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "alpha beta" || screen.VimRegister != "alpha " {
		t.Fatalf("after dw P screen = %#v", screen)
	}
}

func TestREPLScreenVimLinewiseYankPaste(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\ntwo")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "y", "y", "p"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "one\none\ntwo" || screen.VimRegister != "one\n" || !screen.VimRegisterLinewise || screen.Prompt.Cursor != len([]rune("one\n")) {
		t.Fatalf("after yy p screen = %#v", screen)
	}
}

func TestREPLScreenVimGLineNavigationAndOperators(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\ntwo\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(ParseKey("G"))
	if screen.Prompt.Cursor != len([]rune("one\ntwo\n")) {
		t.Fatalf("cursor after G = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"g", "g"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != 0 {
		t.Fatalf("cursor after gg = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"2", "g", "g"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("one\n")) {
		t.Fatalf("cursor after 2gg = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"d", "G"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "one" || screen.VimRegister != "two\nthree\n" {
		t.Fatalf("after dG screen = %#v", screen)
	}
}

func TestREPLScreenVimJKLineMotionsAndOperators(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\ntwo\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"g", "g", "j", "j", "k"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("one\n")) {
		t.Fatalf("cursor after jjk = %d", screen.Prompt.Cursor)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\ntwo\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"g", "g", "d", "j"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "three" || screen.VimRegister != "one\ntwo\n" {
		t.Fatalf("after dj screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\ntwo\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"G", "d", "k"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "one" || screen.VimRegister != "two\nthree\n" {
		t.Fatalf("after dk screen = %#v", screen)
	}
}

func TestREPLScreenVimEditCommands(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "abC")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "2", "~"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "ABC" || screen.Prompt.Cursor != 2 {
		t.Fatalf("after 2~ screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\n  two\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "J"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "one two\nthree" || screen.Prompt.Cursor != len([]rune("one")) {
		t.Fatalf("after J screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\ntwo")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "o"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.VimMode != VimInsert || screen.Prompt.Text != "one\n\ntwo" || screen.Prompt.Cursor != len([]rune("one\n")) {
		t.Fatalf("after o screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\ntwo")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", ">", ">", "<", "<"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "one\ntwo" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after >> << screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "abcd")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "2", "s"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.VimMode != VimInsert || screen.Prompt.Text != "cd" || screen.Prompt.Cursor != 0 || screen.VimRegister != "ab" || screen.VimRegisterLinewise {
		t.Fatalf("after 2s screen = %#v", screen)
	}
	typePromptText(&screen, "X")
	screen.ApplyKey(ParseKey("\x1b"))
	if screen.Prompt.Text != "Xcd" {
		t.Fatalf("after 2s insert screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\ntwo\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"G", "S"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.VimMode != VimInsert || screen.Prompt.Text != "one\ntwo\n" || screen.Prompt.Cursor != len([]rune("one\ntwo\n")) || screen.VimRegister != "three\n" || !screen.VimRegisterLinewise {
		t.Fatalf("after S screen = %#v", screen)
	}
}

func TestREPLScreenVimDotRepeatsChanges(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "abcd")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "x", "."} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "cd" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after x. screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "abcd")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "r", "x", "l", "."} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "xxcd" || screen.Prompt.Cursor != 1 {
		t.Fatalf("after r/l/. screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "alpha beta gamma")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "d", "w", "."} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "gamma" || screen.Prompt.Cursor != 0 {
		t.Fatalf("after dw. screen = %#v", screen)
	}
}

func TestREPLScreenVimDotRepeatsInsert(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	for _, seq := range []string{"a", "b", "\x1b", "$", "."} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "abab" || screen.Prompt.Cursor != len([]rune("abab")) {
		t.Fatalf("after insert dot repeat screen = %#v", screen)
	}
}

func TestScreenLifecycleAlternateScreenSequences(t *testing.T) {
	var lifecycle ScreenLifecycle
	enter := lifecycle.EnterAlternate()
	if !lifecycle.AlternateScreen || !lifecycle.CursorHidden {
		t.Fatalf("lifecycle after enter = %#v", lifecycle)
	}
	if !strings.Contains(enter, EnterAlternateScreen) || !strings.Contains(enter, HideCursor) {
		t.Fatalf("enter = %q", enter)
	}
	if again := lifecycle.EnterAlternate(); again != "" {
		t.Fatalf("second enter should be idempotent: %q", again)
	}
	exit := lifecycle.ExitAlternate()
	if lifecycle.AlternateScreen || lifecycle.CursorHidden {
		t.Fatalf("lifecycle after exit = %#v", lifecycle)
	}
	if !strings.Contains(exit, ShowCursor) || !strings.Contains(exit, ExitAlternateScreen) {
		t.Fatalf("exit = %q", exit)
	}
	if again := lifecycle.ExitAlternate(); again != "" {
		t.Fatalf("second exit should be idempotent: %q", again)
	}
	lifecycle.EnterAlternate()
	reset := lifecycle.Reset()
	if lifecycle.AlternateScreen || lifecycle.CursorHidden || !strings.Contains(reset, ExitAlternateScreen) {
		t.Fatalf("reset = %q lifecycle=%#v", reset, lifecycle)
	}
}

func TestScreenLifecycleInteractiveTerminalModes(t *testing.T) {
	var lifecycle ScreenLifecycle
	options := TerminalModeOptions{MouseTracking: true, FocusEvents: true, BracketedPaste: true}
	enter := lifecycle.EnterInteractive(options)
	if !lifecycle.AlternateScreen || !lifecycle.CursorHidden || !lifecycle.MouseTracking || !lifecycle.FocusEvents || !lifecycle.BracketedPaste {
		t.Fatalf("lifecycle after enter = %#v", lifecycle)
	}
	for _, want := range []string{EnterAlternateScreen, EnableMouseTracking, EnableFocusEvents, EnableBracketedPaste} {
		if !strings.Contains(enter, want) {
			t.Fatalf("enter missing %q in %q", want, enter)
		}
	}
	if again := lifecycle.EnterInteractive(options); again != "" {
		t.Fatalf("second enter should be idempotent: %q", again)
	}
	reassert := lifecycle.ReassertTerminalModes(options)
	for _, want := range []string{EnableMouseTracking, EnableFocusEvents, EnableBracketedPaste} {
		if !strings.Contains(reassert, want) {
			t.Fatalf("reassert missing %q in %q", want, reassert)
		}
	}
	exit := lifecycle.ExitInteractive()
	if lifecycle.AlternateScreen || lifecycle.CursorHidden || lifecycle.MouseTracking || lifecycle.FocusEvents || lifecycle.BracketedPaste {
		t.Fatalf("lifecycle after exit = %#v", lifecycle)
	}
	for _, want := range []string{DisableMouseTracking, DisableFocusEvents, DisableBracketedPaste, ShowCursor, ExitAlternateScreen} {
		if !strings.Contains(exit, want) {
			t.Fatalf("exit missing %q in %q", want, exit)
		}
	}
	if again := lifecycle.ExitInteractive(); again != "" {
		t.Fatalf("second exit should be idempotent: %q", again)
	}
}

func TestCaptureANSISnapshotPreservesOutputAndVisibleText(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.Text = "run"
	prompt.Cursor = 3
	snapshot := CaptureANSISnapshot("main", 32, 6, Frame{
		Messages:   []Message{{Role: RoleAssistant, Text: "hello"}},
		Status:     "ready",
		Prompt:     prompt,
		ShowCursor: true,
	})
	if snapshot.Name != "main" || snapshot.Width != 32 || snapshot.Height != 6 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if !strings.Contains(snapshot.Output, HomeCursor) || !strings.Contains(snapshot.Output, ClearScreen) {
		t.Fatalf("output = %q", snapshot.Output)
	}
	if strings.Contains(snapshot.Text, "\x1b[") || !strings.Contains(snapshot.Text, "assistant: hello") || !strings.Contains(snapshot.Text, "> run") {
		t.Fatalf("text = %q", snapshot.Text)
	}
}

func TestSnapshotCorpusWritesAndComparesVisibleText(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.Text = "run"
	snapshot := CaptureANSISnapshot("main:view", 32, 6, Frame{
		Messages:   []Message{{Role: RoleAssistant, Text: "hello"}},
		Status:     "ready",
		Prompt:     prompt,
		ShowCursor: true,
	})
	corpus := SnapshotCorpus{Dir: t.TempDir()}
	if err := corpus.Write(snapshot); err != nil {
		t.Fatal(err)
	}
	text, err := corpus.LoadText("main:view")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "assistant: hello") {
		t.Fatalf("text = %q", text)
	}
	comparison, err := corpus.Compare(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.Match || comparison.ExpectedText != comparison.ActualText {
		t.Fatalf("comparison = %#v", comparison)
	}
	changed := snapshot
	changed.Text = strings.ReplaceAll(changed.Text, "hello", "bye")
	comparison, err = corpus.Compare(changed)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Match || !strings.Contains(comparison.ExpectedText, "hello") || !strings.Contains(comparison.ActualText, "bye") {
		t.Fatalf("changed comparison = %#v", comparison)
	}
	if comparison.FirstDiffLine == 0 || !strings.Contains(comparison.ExpectedDiff, "hello") || !strings.Contains(comparison.ActualDiff, "bye") {
		t.Fatalf("changed comparison diff = %#v", comparison)
	}
	missing := changed
	missing.Name = "missing:view"
	comparison, err = corpus.Compare(missing)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.Missing || comparison.Match || comparison.Name != "missing:view" || !strings.Contains(comparison.ActualText, "bye") {
		t.Fatalf("missing comparison = %#v", comparison)
	}
	if _, err := os.Stat(filepath.Join(corpus.Dir, "main_view.ansi")); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotCorpusWritesAndComparesScriptSnapshots(t *testing.T) {
	screen := NewREPLScreen(30, 6, nil)
	result, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{Message: &Message{Role: RoleAssistant, Text: "ready"}, SnapshotName: "initial"},
		{Keys: []string{"o", "k"}, SnapshotName: "typed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshots) != 2 {
		t.Fatalf("snapshots = %#v", result.Snapshots)
	}
	corpus := SnapshotCorpus{Dir: t.TempDir()}
	if err := corpus.WriteAll(result.Snapshots); err != nil {
		t.Fatal(err)
	}
	comparisons, err := corpus.CompareAll(result.Snapshots)
	if err != nil {
		t.Fatal(err)
	}
	if len(comparisons) != 2 {
		t.Fatalf("comparisons = %#v", comparisons)
	}
	for _, comparison := range comparisons {
		if !comparison.Match || comparison.Missing {
			t.Fatalf("comparison = %#v", comparison)
		}
	}
	changed := append([]ANSISnapshot(nil), result.Snapshots...)
	changed[1].Text = strings.ReplaceAll(changed[1].Text, "> ok", "> no")
	comparisons, err = corpus.CompareAll(changed)
	if err != nil {
		t.Fatal(err)
	}
	if !comparisons[0].Match || comparisons[1].Match || comparisons[1].FirstDiffLine == 0 {
		t.Fatalf("changed comparisons = %#v", comparisons)
	}
}

func TestRunInteractionScriptCapturesEventsAndSnapshots(t *testing.T) {
	screen := NewREPLScreen(30, 6, nil)
	permission := PermissionDialog(PermissionRequest{ID: "perm_1", ToolName: "Edit"})
	cursor := 3
	result, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{Message: &Message{Role: RoleAssistant, Text: "ready"}, SnapshotName: "initial", ExpectSnapshotContains: []string{"assistant: ready"}},
		{Keys: []string{"r", "u", "n"}, ExpectPrompt: &PromptExpectation{Text: "run", Cursor: &cursor}},
		{Key: "\n", SnapshotName: "submitted", ExpectEvent: &ScreenEvent{Type: ScreenEventPromptSubmitted, Value: "run"}, ExpectPrompt: &PromptExpectation{Empty: true}},
		{Dialog: &permission},
		{Key: "\t"},
		{Key: "\n", SnapshotName: "permission", ExpectEvent: &ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow Session", DialogID: "perm_1", DialogKind: DialogPermission}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("events = %#v", result.Events)
	}
	if result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "run" {
		t.Fatalf("submit event = %#v", result.Events[0])
	}
	if result.Events[1].Type != ScreenEventDialogAction || result.Events[1].DialogID != "perm_1" || result.Events[1].Value != "Allow Session" {
		t.Fatalf("dialog event = %#v", result.Events[1])
	}
	if len(result.Snapshots) != 3 || !strings.Contains(result.Snapshots[0].Text, "assistant: ready") {
		t.Fatalf("snapshots = %#v", result.Snapshots)
	}
}

func TestRunInteractionScriptChecksPromptExpandedPaste(t *testing.T) {
	screen := NewREPLScreen(30, 6, nil)
	_, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{
			Key:          "\x1b[200~alpha\nbeta\x1b[201~",
			ExpectPrompt: &PromptExpectation{Text: "[Pasted text #1 +1 lines]", Expanded: "alpha\nbeta"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunInteractionScriptChecksVimState(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	enabled := true
	linewise := false
	cursor := len([]rune("abc"))
	_, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{
			Keys:         []string{"a", "b", "c"},
			ExpectPrompt: &PromptExpectation{Text: "abc", Cursor: &cursor},
			ExpectVim:    &VimExpectation{Enabled: &enabled, Mode: VimInsert},
		},
		{
			Keys:      []string{"\x1b", "0", "y", "l"},
			ExpectVim: &VimExpectation{Enabled: &enabled, Mode: VimNormal, Register: "a", RegisterLinewise: &linewise},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunInteractionScriptChecksViewport(t *testing.T) {
	screen := NewREPLScreen(22, 6, nil)
	_, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{Message: &Message{Role: RoleSystem, Text: "one"}},
		{Message: &Message{Role: RoleSystem, Text: "two"}},
		{Message: &Message{Role: RoleSystem, Text: "three"}},
		{Message: &Message{Role: RoleSystem, Text: "four"}},
		{Message: &Message{Role: RoleSystem, Text: "five"}, ExpectViewport: &ViewportExpectation{VisibleContains: []string{"system: five"}, VisibleNotContains: []string{"system: one"}}},
		{Key: "\x1b[5~", ExpectViewport: &ViewportExpectation{VisibleContains: []string{"system: one"}, VisibleLineCount: 4}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunInteractionScriptChecksFocusState(t *testing.T) {
	screen := NewREPLScreen(30, 6, nil)
	focused := true
	blurred := false
	_, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{ExpectFocused: &focused},
		{Key: "\x1b[O", ExpectEvent: &ScreenEvent{Type: ScreenEventFocusOut}, ExpectFocused: &blurred},
		{Key: "\x1b[I", ExpectEvent: &ScreenEvent{Type: ScreenEventFocusIn}, ExpectFocused: &focused},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunInteractionScriptCheckedFailsOnExpectationMismatch(t *testing.T) {
	screen := NewREPLScreen(30, 6, nil)
	_, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{Key: "\n", ExpectEvent: &ScreenEvent{Type: ScreenEventDialogAction}},
	})
	if err == nil || !strings.Contains(err.Error(), "event type") {
		t.Fatalf("err = %v", err)
	}
}
