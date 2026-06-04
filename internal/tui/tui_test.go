package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
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

func TestPromptStateMultilineControlLineEditing(t *testing.T) {
	prompt := NewPromptState(nil)
	text := "one\ntwo three\nfour"
	prompt.Text = text
	prompt.Cursor = len([]rune("one\ntwo th"))
	prompt.Apply(ParseKey("\x01"))
	if prompt.Cursor != len([]rune("one\n")) {
		t.Fatalf("ctrl-a cursor = %d", prompt.Cursor)
	}

	prompt.Text = text
	prompt.Cursor = len([]rune("one\ntwo th"))
	prompt.Apply(ParseKey("\x05"))
	if prompt.Cursor != len([]rune("one\ntwo three")) {
		t.Fatalf("ctrl-e cursor = %d", prompt.Cursor)
	}

	prompt.Text = text
	prompt.Cursor = len([]rune("one\ntwo "))
	prompt.Apply(ParseKey("\x0b"))
	if prompt.Text != "one\ntwo \nfour" || prompt.Cursor != len([]rune("one\ntwo ")) {
		t.Fatalf("ctrl-k prompt = %#v", prompt)
	}

	prompt.Text = text
	prompt.Cursor = len([]rune("one\ntwo "))
	prompt.Apply(ParseKey("\x15"))
	if prompt.Text != "one\nthree\nfour" || prompt.Cursor != len([]rune("one\n")) {
		t.Fatalf("ctrl-u prompt = %#v", prompt)
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
	prompt.Apply(ParseKey("\x1b[1;5C"))
	if prompt.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("after ctrl-right prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1b[1;3D"))
	if prompt.Cursor != len([]rune("alpha ")) {
		t.Fatalf("after alt-left prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1b[1;3C"))
	if prompt.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("after alt-right prompt = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x1b[1;5D"))
	if prompt.Cursor != len([]rune("alpha ")) {
		t.Fatalf("after ctrl-left prompt = %#v", prompt)
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
	screen.ApplyKey(ParseKey("\x1b[1;5C"))
	if screen.ReverseSearch.Cursor != len([]rune("alpha beta gamma")) {
		t.Fatalf("reverse search ctrl-right = %#v", screen.ReverseSearch)
	}
	screen.ApplyKey(ParseKey("\x1b[1;3D"))
	if screen.ReverseSearch.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("reverse search alt-left = %#v", screen.ReverseSearch)
	}
	screen.ApplyKey(ParseKey("\x1b[1;3C"))
	if screen.ReverseSearch.Cursor != len([]rune("alpha beta gamma")) {
		t.Fatalf("reverse search alt-right = %#v", screen.ReverseSearch)
	}
	screen.ApplyKey(ParseKey("\x1b[1;5D"))
	if screen.ReverseSearch.Cursor != len([]rune("alpha beta ")) {
		t.Fatalf("reverse search ctrl-left = %#v", screen.ReverseSearch)
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

func TestReverseSearchCtrlPNNavigatesResults(t *testing.T) {
	screen := NewREPLScreen(40, 8, []string{"alpha one", "alpha two"})
	screen.ApplyKey(ParseKey("\x12"))
	for _, r := range "alpha" {
		screen.ApplyKey(Key{Type: KeyRune, Rune: r})
	}
	current, ok := screen.ReverseSearch.Current()
	if !ok || current != "alpha two" {
		t.Fatalf("initial reverse search current = %q ok=%v", current, ok)
	}
	screen.ApplyKey(ParseKey("\x0e"))
	current, ok = screen.ReverseSearch.Current()
	if !ok || current != "alpha one" {
		t.Fatalf("ctrl-n reverse search current = %q ok=%v", current, ok)
	}
	screen.ApplyKey(ParseKey("\x10"))
	current, ok = screen.ReverseSearch.Current()
	if !ok || current != "alpha two" {
		t.Fatalf("ctrl-p reverse search current = %q ok=%v", current, ok)
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

func TestPromptCtrlPNHistoryNavigationKeepsDraft(t *testing.T) {
	prompt := NewPromptState([]string{"one", "two"})
	prompt.Apply(ParseKey("d"))
	prompt.Apply(ParseKey("\x10"))
	if prompt.Text != "two" {
		t.Fatalf("ctrl-p history prev = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x10"))
	if prompt.Text != "one" {
		t.Fatalf("ctrl-p history prev again = %#v", prompt)
	}
	prompt.Apply(ParseKey("\x0e"))
	prompt.Apply(ParseKey("\x0e"))
	if prompt.Text != "d" {
		t.Fatalf("ctrl-n draft = %#v", prompt)
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

func TestPromptPasteCleansANSIAndWhitespace(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.Apply(ParseKey("\x1b[200~\x1b[31malpha\x1b[0m\tbeta\r\nomega\x1b[201~"))
	if prompt.Text != "alpha    beta\n\nomega" {
		t.Fatalf("prompt = %#v", prompt)
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

func TestREPLScreenInlinesShortPasteAtNormalHeight(t *testing.T) {
	screen := NewREPLScreen(40, 24, nil)
	screen.ApplyKey(ParseKey("\x1b[200~alpha\nbeta\x1b[201~"))
	if screen.Prompt.Text != "alpha\nbeta" || screen.Prompt.ExpandedText() != "alpha\nbeta" {
		t.Fatalf("prompt = %#v expanded=%q", screen.Prompt, screen.Prompt.ExpandedText())
	}
	if len(screen.Prompt.PastedContents) != 0 || screen.Prompt.NextPastedID != 1 {
		t.Fatalf("pasted contents = %#v next=%d", screen.Prompt.PastedContents, screen.Prompt.NextPastedID)
	}
}

func TestREPLScreenReferencesPasteAtSmallHeightOrLargePaste(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.ApplyKey(ParseKey("\x1b[200~short\x1b[201~"))
	if screen.Prompt.Text != "[Pasted text #1]" || screen.Prompt.ExpandedText() != "short" {
		t.Fatalf("small-height prompt = %#v expanded=%q", screen.Prompt, screen.Prompt.ExpandedText())
	}

	screen = NewREPLScreen(40, 24, nil)
	large := strings.Repeat("x", pasteReferenceThreshold+1)
	screen.ApplyKey(Key{Type: KeyPaste, Text: large})
	if screen.Prompt.Text != "[Pasted text #1]" || screen.Prompt.ExpandedText() != large {
		t.Fatalf("large prompt = %#v expanded=%q", screen.Prompt, screen.Prompt.ExpandedText())
	}
}

func TestREPLScreenResizeUpdatesPasteReferenceRows(t *testing.T) {
	screen := NewREPLScreen(40, 24, nil)
	screen.Resize(40, 8)
	screen.ApplyKey(ParseKey("\x1b[200~short\x1b[201~"))
	if screen.Prompt.Text != "[Pasted text #1]" || screen.Prompt.ExpandedText() != "short" {
		t.Fatalf("prompt = %#v expanded=%q", screen.Prompt, screen.Prompt.ExpandedText())
	}
}

func TestPromptPasteReferencesCanStoreImageHints(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.EnablePasteReferences()
	prompt.Apply(ParseKey("\x1b]1337;File=name=chart.png;type=image/png;inline=1:AAAA\a"))
	if prompt.Text != "[Image #1]" || prompt.ExpandedText() != "[Image #1]" {
		t.Fatalf("prompt = %#v expanded=%q", prompt, prompt.ExpandedText())
	}
	image := prompt.PastedContents[1]
	if image.Type != session.PastedContentImage || image.Filename != "chart.png" || image.MediaType != "image/png" || image.Content != "AAAA" {
		t.Fatalf("image content = %#v", image)
	}
	entry := prompt.HistoryEntry()
	if entry.Display != "[Image #1]" || entry.PastedContents[1].Filename != "chart.png" {
		t.Fatalf("history entry = %#v", entry)
	}
	result := prompt.Apply(ParseKey("\n"))
	if result.Submitted != "[Image #1]" || result.Display != "[Image #1]" || result.PastedContents[1].Type != session.PastedContentImage {
		t.Fatalf("result = %#v", result)
	}
}

func TestPromptPrunesOrphanImagePastedContent(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.EnablePasteReferences()
	prompt.Apply(ParseKey("\x1b]1337;File=name=chart.png;type=image/png;inline=1:AAAA\a"))
	prompt.Apply(ParseKey("\x15"))
	if prompt.Text != "" || len(prompt.PastedContents) != 0 || prompt.NextPastedID != 2 {
		t.Fatalf("after ctrl-u prompt = %#v", prompt)
	}

	prompt.Apply(ParseKey("\x1b]1337;File=name=next.png;type=image/png;inline=1:BBBB\a"))
	if prompt.Text != "[Image #2]" || len(prompt.PastedContents) != 1 || prompt.PastedContents[2].Content != "BBBB" {
		t.Fatalf("next image prompt = %#v", prompt)
	}

	result := prompt.Apply(ParseKey("\n"))
	if len(result.PastedContents) != 1 || result.PastedContents[2].Content != "BBBB" {
		t.Fatalf("result = %#v", result)
	}
}

func TestPromptImageHintLazySpacing(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.EnablePasteReferences()
	prompt.Apply(ParseKey("\x1b]1337;File=name=one.png;type=image/png;inline=1:AAAA\a"))
	prompt.Apply(ParseKey("x"))
	if prompt.Text != "[Image #1] x" {
		t.Fatalf("image then text = %#v", prompt)
	}

	prompt = NewPromptState(nil)
	prompt.EnablePasteReferences()
	prompt.Apply(ParseKey("\x1b]1337;File=name=one.png;type=image/png;inline=1:AAAA\a"))
	prompt.Apply(ParseKey(" "))
	prompt.Apply(ParseKey("x"))
	if prompt.Text != "[Image #1] x" {
		t.Fatalf("image then explicit space = %#v", prompt)
	}

	prompt = NewPromptState(nil)
	prompt.EnablePasteReferences()
	prompt.Apply(ParseKey("\x1b]1337;File=name=one.png;type=image/png;inline=1:AAAA\a"))
	prompt.Apply(ParseKey("\x1b]1337;File=name=two.png;type=image/png;inline=1:BBBB\a"))
	if prompt.Text != "[Image #1] [Image #2]" {
		t.Fatalf("consecutive images = %#v", prompt)
	}

	prompt = NewPromptState(nil)
	prompt.EnablePasteReferences()
	prompt.Apply(ParseKey("\x1b]1337;File=name=one.png;type=image/png;inline=1:AAAA\a"))
	prompt.Apply(ParseKey("\x1b[13;2u"))
	prompt.Apply(ParseKey("x"))
	if prompt.Text != "[Image #1]\nx" {
		t.Fatalf("image then shift-enter = %#v", prompt)
	}
}

func TestREPLScreenSeedsNextPastedIDFromMessages(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetMessages([]Message{
		{Role: RoleAssistant, Text: "[Image #99]", ImagePasteIDs: []int{99}},
		{Role: RoleUser, Text: "old [Pasted text #3]", ImagePasteIDs: []int{7}},
	})
	screen.ApplyKey(ParseKey("\x1b]1337;File=name=next.png;type=image/png;inline=1:BBBB\a"))
	if screen.Prompt.Text != "[Image #8]" || screen.Prompt.NextPastedID != 9 {
		t.Fatalf("prompt = %#v", screen.Prompt)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.AppendMessage(Message{Role: RoleUser, Text: "old [Image #4]"})
	screen.ApplyKey(ParseKey("\x1b]1337;File=name=next.png;type=image/png;inline=1:BBBB\a"))
	if screen.Prompt.Text != "[Image #5]" || screen.Prompt.NextPastedID != 6 {
		t.Fatalf("append-seeded prompt = %#v", screen.Prompt)
	}
}

func TestPromptImageHintWritesImageCacheWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	session.ClearStoredImagePaths()
	defer session.ClearStoredImagePaths()

	prompt := NewPromptState(nil)
	prompt.EnablePasteReferences()
	prompt.EnableImageCache("session-1")
	prompt.Apply(ParseKey("\x1b]1337;File=name=chart.png;type=image/png;inline=1:aW1hZ2U=\a"))

	path, ok := session.GetStoredImagePath(1)
	if !ok {
		t.Fatal("image path was not cached")
	}
	want := filepath.Join(dir, "image-cache", "session-1", "1.png")
	if path != want {
		t.Fatalf("image path = %q, want %q", path, want)
	}
	if image := prompt.PastedContents[1]; image.SourcePath != want {
		t.Fatalf("image source path = %q, want %q", image.SourcePath, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "image" {
		t.Fatalf("image data = %q", data)
	}
}

func TestREPLScreenImageCacheSessionAppliesToPrompt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	session.ClearStoredImagePaths()
	defer session.ClearStoredImagePaths()

	screen := NewREPLScreen(40, 8, nil)
	screen.EnableImageCache("session-1")
	screen.ApplyKey(ParseKey("\x1b]1337;File=name=diagram.webp;type=image/webp;inline=1:d2VicA==\a"))

	path, ok := session.GetStoredImagePath(1)
	if !ok {
		t.Fatal("image path was not cached")
	}
	want := filepath.Join(dir, "image-cache", "session-1", "1.webp")
	if path != want {
		t.Fatalf("image path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "webp" {
		t.Fatalf("image data = %q", data)
	}
	if screen.Prompt.Text != "[Image #1]" {
		t.Fatalf("prompt = %#v", screen.Prompt)
	}
}

func TestREPLScreenSubmitEventCarriesPastedContentsForMessages(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	session.ClearStoredImagePaths()
	defer session.ClearStoredImagePaths()

	screen := NewREPLScreen(40, 8, nil)
	screen.EnableImageCache("session-1")
	screen.ApplyKey(ParseKey("\x1b[200~alpha\nbeta\x1b[201~"))
	screen.ApplyKey(ParseKey("\x1b]1337;File=name=chart.png;type=image/png;inline=1:AAAA\a"))
	event := screen.ApplyKey(ParseKey("\n"))
	if event.Type != ScreenEventPromptSubmitted || event.Value != "alpha\nbeta[Image #2]" || event.Display != "[Pasted text #1 +1 lines][Image #2]" {
		t.Fatalf("event = %#v", event)
	}
	if len(event.PastedContents) != 2 || event.PastedContents[2].Type != session.PastedContentImage {
		t.Fatalf("event pasted contents = %#v", event.PastedContents)
	}
	messages := event.PromptMessages()
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].Content[0].Text != "alpha\nbeta[Image #2]" || messages[0].Content[1].Type != contracts.ContentImage {
		t.Fatalf("prompt message = %#v", messages[0])
	}
	if !messages[1].IsMeta || !strings.Contains(messages[1].Content[0].Text, filepath.Join("image-cache", "session-1", "2.png")) {
		t.Fatalf("metadata message = %#v", messages[1])
	}
}

func TestREPLScreenStashesAndRestoresPromptWithPastedContents(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.ApplyKey(ParseKey("\x1b[200~alpha\nbeta\x1b[201~"))
	screen.ApplyKey(ParseKey("\x1b]1337;File=name=Y2hhcnQucG5n;type=image/png;inline=1:AAAA\a"))

	stashed := screen.ApplyKey(ParseKey("\x13"))
	if stashed.Type != ScreenEventStashPrompt || stashed.Value != "alpha\nbeta[Image #2]" || stashed.Display != "[Pasted text #1 +1 lines][Image #2]" {
		t.Fatalf("stashed event = %#v", stashed)
	}
	if stashed.PastedContents[1].Content != "alpha\nbeta" || stashed.PastedContents[2].Filename != "chart.png" {
		t.Fatalf("stashed pasted contents = %#v", stashed.PastedContents)
	}
	if screen.Prompt.Text != "" || len(screen.Prompt.PastedContents) != 0 || screen.Prompt.NextPastedID != 1 || screen.StashedPrompt == nil {
		t.Fatalf("after stash prompt=%#v stash=%#v", screen.Prompt, screen.StashedPrompt)
	}

	restored := screen.ApplyKey(ParseKey("\x13"))
	if restored.Type != ScreenEventStashPrompt || restored.Value != "alpha\nbeta[Image #2]" || restored.Display != "[Pasted text #1 +1 lines][Image #2]" {
		t.Fatalf("restored event = %#v", restored)
	}
	if screen.Prompt.Text != "[Pasted text #1 +1 lines][Image #2]" || screen.Prompt.ExpandedText() != "alpha\nbeta[Image #2]" {
		t.Fatalf("restored prompt = %#v expanded=%q", screen.Prompt, screen.Prompt.ExpandedText())
	}
	if screen.Prompt.NextPastedID != 3 || screen.Prompt.PastedContents[2].Filename != "chart.png" || screen.StashedPrompt != nil {
		t.Fatalf("restored pasted contents = %#v next=%d stash=%#v", screen.Prompt.PastedContents, screen.Prompt.NextPastedID, screen.StashedPrompt)
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

func TestPromptHistoryRestoresPastedContentEntries(t *testing.T) {
	prompt := NewPromptStateFromEntries([]session.HistoryEntry{{
		Display: "use [Pasted text #1] [Image #2]",
		PastedContents: map[int]session.PastedContent{
			1: {ID: 1, Type: session.PastedContentText, Content: "expanded paste"},
			2: {ID: 2, Type: session.PastedContentImage, Filename: "chart.png", MediaType: "image/png"},
		},
	}})
	prompt.EnablePasteReferences()
	prompt.Apply(ParseKey("\x1b[A"))
	if prompt.Text != "use [Pasted text #1] [Image #2]" || prompt.ExpandedText() != "use expanded paste [Image #2]" || prompt.PastedContents[2].Filename != "chart.png" {
		t.Fatalf("history prompt = %#v expanded=%q", prompt, prompt.ExpandedText())
	}
	prompt.Apply(ParseKey("\x1b[B"))
	if prompt.Text != "" || prompt.ExpandedText() != "" || len(prompt.PastedContents) != 0 {
		t.Fatalf("draft prompt = %#v expanded=%q", prompt, prompt.ExpandedText())
	}

	prompt.Apply(ParseKey("\x1b[200~submitted\npaste\x1b[201~"))
	result := prompt.Apply(ParseKey("\n"))
	if result.Submitted != "submitted\npaste" {
		t.Fatalf("submitted = %#v", result)
	}
	prompt.Apply(ParseKey("\x1b[A"))
	if prompt.Text != "[Pasted text #1 +1 lines]" || prompt.ExpandedText() != "submitted\npaste" {
		t.Fatalf("submitted history = %#v expanded=%q", prompt, prompt.ExpandedText())
	}
}

func TestREPLScreenRestoresPastedHistoryEntries(t *testing.T) {
	screen := NewREPLScreenFromHistoryEntries(40, 8, []session.HistoryEntry{{
		Display: "run [Pasted text #1]",
		PastedContents: map[int]session.PastedContent{
			1: {ID: 1, Type: session.PastedContentText, Content: "expanded command"},
		},
	}})
	screen.ApplyKey(ParseKey("\x1b[A"))
	if screen.Prompt.Text != "run [Pasted text #1]" || screen.Prompt.ExpandedText() != "run expanded command" {
		t.Fatalf("prompt = %#v expanded=%q", screen.Prompt, screen.Prompt.ExpandedText())
	}
}

func TestReverseSearchRestoresPastedContentEntries(t *testing.T) {
	screen := NewREPLScreenFromHistoryEntries(40, 8, []session.HistoryEntry{
		{
			Display: "older [Pasted text #1]",
			PastedContents: map[int]session.PastedContent{
				1: {ID: 1, Type: session.PastedContentText, Content: "older paste"},
			},
		},
		{
			Display: "run [Pasted text #1] [Image #2]",
			PastedContents: map[int]session.PastedContent{
				1: {ID: 1, Type: session.PastedContentText, Content: "expanded command"},
				2: {ID: 2, Type: session.PastedContentImage, Filename: "chart.png", MediaType: "image/png", Content: "AAAA"},
			},
		},
	})

	screen.ApplyKey(ParseKey("\x12"))
	for _, seq := range []string{"r", "u", "n"} {
		screen.ApplyKey(ParseKey(seq))
	}
	selected := screen.ApplyKey(ParseKey("\n"))
	if selected.Type != ScreenEventReverseSelected || selected.Value != "run [Pasted text #1] [Image #2]" {
		t.Fatalf("selected = %#v", selected)
	}
	if screen.Prompt.Text != "run [Pasted text #1] [Image #2]" || screen.Prompt.ExpandedText() != "run expanded command [Image #2]" {
		t.Fatalf("prompt = %#v expanded=%q", screen.Prompt, screen.Prompt.ExpandedText())
	}
	if screen.Prompt.NextPastedID != 3 || screen.Prompt.PastedContents[2].Filename != "chart.png" {
		t.Fatalf("pasted contents = %#v next=%d", screen.Prompt.PastedContents, screen.Prompt.NextPastedID)
	}
	if selected.PastedContents[1].Content != "expanded command" || selected.PastedContents[2].Filename != "chart.png" {
		t.Fatalf("event pasted contents = %#v", selected.PastedContents)
	}

	submitted := screen.ApplyKey(ParseKey("\n"))
	if submitted.Type != ScreenEventPromptSubmitted || submitted.Value != "run expanded command [Image #2]" {
		t.Fatalf("submitted = %#v", submitted)
	}
	if submitted.Display != "run [Pasted text #1] [Image #2]" || submitted.PastedContents[2].Filename != "chart.png" {
		t.Fatalf("submitted pasted contents = %#v", submitted)
	}
}

func TestREPLScreenRestoreMessageAtRestoresPastedContent(t *testing.T) {
	screen := NewREPLScreen(50, 8, nil)
	screen.SetMessages([]Message{
		{Role: RoleAssistant, Text: "before"},
		{
			Role: RoleUser,
			Text: "retry [Pasted text #3] [Image #7]",
			ContentBlocks: []contracts.ContentBlock{
				contracts.NewBase64ImageBlock("image/jpeg", "BBBB"),
			},
			ImagePasteIDs: []int{7},
			PastedContents: map[int]session.PastedContent{
				3: {ID: 3, Type: session.PastedContentText, Content: "expanded retry"},
			},
		},
		{Role: RoleAssistant, Text: "after"},
	})

	if !screen.RestoreMessageAt(1) {
		t.Fatal("RestoreMessageAt returned false")
	}
	if len(screen.Messages) != 1 || screen.Messages[0].Text != "before" {
		t.Fatalf("messages after restore = %#v", screen.Messages)
	}
	if screen.Prompt.Text != "retry [Pasted text #3] [Image #7]" || screen.Prompt.ExpandedText() != "retry expanded retry [Image #7]" {
		t.Fatalf("prompt = %#v expanded=%q", screen.Prompt, screen.Prompt.ExpandedText())
	}
	if screen.Prompt.NextPastedID != 8 || screen.Prompt.PastedContents[7].Content != "BBBB" || screen.Prompt.PastedContents[7].MediaType != "image/jpeg" {
		t.Fatalf("pasted contents = %#v next=%d", screen.Prompt.PastedContents, screen.Prompt.NextPastedID)
	}
	submitted := screen.ApplyKey(ParseKey("\n"))
	if submitted.Type != ScreenEventPromptSubmitted || submitted.Value != "retry expanded retry [Image #7]" || submitted.PastedContents[7].Content != "BBBB" {
		t.Fatalf("submitted = %#v", submitted)
	}
}

func TestREPLScreenRestoreMessageAtBuildsDisplayFromBlocks(t *testing.T) {
	screen := NewREPLScreen(50, 8, nil)
	screen.SetMessages([]Message{{
		Role: RoleUser,
		ContentBlocks: []contracts.ContentBlock{
			contracts.NewTextBlock("look"),
			contracts.NewBase64ImageBlock("image/png", "AAAA"),
		},
		ImagePasteIDs: []int{5},
	}})

	if !screen.RestoreMessageAt(0) {
		t.Fatal("RestoreMessageAt returned false")
	}
	if screen.Prompt.Text != "look [Image #5]" || screen.Prompt.PastedContents[5].Content != "AAAA" || screen.Prompt.NextPastedID != 6 {
		t.Fatalf("prompt = %#v", screen.Prompt)
	}
}

func TestParseImageHintUsesGenericPlaceholder(t *testing.T) {
	key := ParseKey("\x1b]1337;File=inline=1:AAAA\a")
	if key.Type != KeyImageHint || key.Text != ImageHintPlaceholder {
		t.Fatalf("key = %#v", key)
	}
}

func TestParseImageHintAcceptsSTTerminatorAndBase64Name(t *testing.T) {
	key := ParseKey("\x1b]1337;File=name=Y2hhcnQucG5n;type=image/png;width=4000;height=2000;display_width=1000;display_height=500;sourcePath=/tmp/chart.png;inline=1:AAAA\x1b\\")
	if key.Type != KeyImageHint || key.Text != "[Image: chart.png]" || key.Filename != "chart.png" || key.MediaType != "image/png" || key.Content != "AAAA" || key.SourcePath != "/tmp/chart.png" {
		t.Fatalf("key = %#v", key)
	}
	if key.Dimensions == nil || key.Dimensions.OriginalWidth != 4000 || key.Dimensions.OriginalHeight != 2000 || key.Dimensions.DisplayWidth != 1000 || key.Dimensions.DisplayHeight != 500 {
		t.Fatalf("dimensions = %#v", key.Dimensions)
	}

	prompt := NewPromptState(nil)
	prompt.EnablePasteReferences()
	prompt.Apply(key)
	image := prompt.PastedContents[1]
	if prompt.Text != "[Image #1]" || image.Type != session.PastedContentImage || image.Filename != "chart.png" || image.Content != "AAAA" || image.SourcePath != "/tmp/chart.png" || image.Dimensions == nil || image.Dimensions.DisplayWidth != 1000 {
		t.Fatalf("prompt = %#v image=%#v", prompt, image)
	}

	rawName := ParseKey("\x1b]1337;File=name=AAAA;inline=1:data\a")
	if rawName.Type != KeyImageHint || rawName.Filename != "AAAA" || rawName.Text != "[Image: AAAA]" {
		t.Fatalf("raw name = %#v", rawName)
	}
}

func TestParseImageHintAcceptsMetadataAliases(t *testing.T) {
	key := ParseKey("\x1b]1337;File=fileName=diagram.webp;contentType=image/webp;sourceURL=file:///tmp/diagram.webp;originalWidth=2400;originalHeight=1200;displayWidth=600;displayHeight=300;inline=1:BBBB\a")
	if key.Type != KeyImageHint || key.Text != "[Image: diagram.webp]" || key.Filename != "diagram.webp" || key.MediaType != "image/webp" || key.Content != "BBBB" || key.SourcePath != "file:///tmp/diagram.webp" {
		t.Fatalf("key = %#v", key)
	}
	if key.Dimensions == nil || key.Dimensions.OriginalWidth != 2400 || key.Dimensions.OriginalHeight != 1200 || key.Dimensions.DisplayWidth != 600 || key.Dimensions.DisplayHeight != 300 {
		t.Fatalf("dimensions = %#v", key.Dimensions)
	}

	prompt := NewPromptState(nil)
	prompt.EnablePasteReferences()
	prompt.Apply(ParseKey("\x1b]1337;File=file_name=shot.png;mimeType=image/png;source=/tmp/shot.png;inline=1:CCCC\a"))
	image := prompt.PastedContents[1]
	if prompt.Text != "[Image #1]" || image.Filename != "shot.png" || image.MediaType != "image/png" || image.Content != "CCCC" || image.SourcePath != "/tmp/shot.png" {
		t.Fatalf("prompt = %#v image=%#v", prompt, image)
	}
}

func TestParseAlternateTerminalNavigationSequences(t *testing.T) {
	cases := []struct {
		seq  string
		want KeyType
	}{
		{seq: "\x1bOA", want: KeyUp},
		{seq: "\x1bOB", want: KeyDown},
		{seq: "\x1bOC", want: KeyRight},
		{seq: "\x1bOD", want: KeyLeft},
		{seq: "\x1b[1;5C", want: KeyCtrlRight},
		{seq: "\x1b[1;5D", want: KeyCtrlLeft},
		{seq: "\x1b[1;3C", want: KeyAltRight},
		{seq: "\x1b[1;3D", want: KeyAltLeft},
		{seq: "\x1b[1;9C", want: KeyAltRight},
		{seq: "\x1b[1;9D", want: KeyAltLeft},
		{seq: "\x1bOH", want: KeyHome},
		{seq: "\x1bOF", want: KeyEnd},
		{seq: "\x1b[7~", want: KeyHome},
		{seq: "\x1b[8~", want: KeyEnd},
		{seq: "\x1b[1;3H", want: KeyHome},
		{seq: "\x1b[1;3F", want: KeyEnd},
		{seq: "\x1b[1;5H", want: KeyHome},
		{seq: "\x1b[1;5F", want: KeyEnd},
		{seq: "\x1b[1;9H", want: KeyHome},
		{seq: "\x1b[1;9F", want: KeyEnd},
		{seq: "\x1b[[5~", want: KeyPageUp},
		{seq: "\x1b[[6~", want: KeyPageDown},
		{seq: "\x1b[5$", want: KeyPageUp},
		{seq: "\x1b[6$", want: KeyPageDown},
		{seq: "\x1b[5^", want: KeyPageUp},
		{seq: "\x1b[6^", want: KeyPageDown},
		{seq: "\x1b[3$", want: KeyDelete},
		{seq: "\x0e", want: KeyCtrlN},
		{seq: "\x10", want: KeyCtrlP},
		{seq: "\x1b[13;2u", want: KeyShiftEnter},
		{seq: "\x1b[13;2~", want: KeyShiftEnter},
		{seq: "\x1b[27;2;13~", want: KeyShiftEnter},
	}
	for _, tc := range cases {
		if key := ParseKey(tc.seq); key.Type != tc.want {
			t.Fatalf("ParseKey(%q) = %#v, want %q", tc.seq, key, tc.want)
		}
	}
}

func TestParseModifiedNavigationKeySequences(t *testing.T) {
	for _, modifier := range []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16"} {
		cases := []struct {
			seq  string
			want KeyType
		}{
			{seq: "\x1b[1;" + modifier + "H", want: KeyHome},
			{seq: "\x1b[1;" + modifier + "F", want: KeyEnd},
			{seq: "\x1b[3;" + modifier + "~", want: KeyDelete},
			{seq: "\x1b[5;" + modifier + "~", want: KeyPageUp},
			{seq: "\x1b[6;" + modifier + "~", want: KeyPageDown},
		}
		for _, tc := range cases {
			if key := ParseKey(tc.seq); key.Type != tc.want {
				t.Fatalf("ParseKey(%q) = %#v, want %q", tc.seq, key, tc.want)
			}
		}
	}
	arrowCases := []struct {
		seq  string
		want KeyType
	}{
		{seq: "\x1b[1;2D", want: KeyLeft},
		{seq: "\x1b[1;2C", want: KeyRight},
		{seq: "\x1b[1;2A", want: KeyUp},
		{seq: "\x1b[1;2B", want: KeyDown},
		{seq: "\x1b[1;4D", want: KeyAltLeft},
		{seq: "\x1b[1;4C", want: KeyAltRight},
		{seq: "\x1b[1;6D", want: KeyCtrlLeft},
		{seq: "\x1b[1;6C", want: KeyCtrlRight},
		{seq: "\x1b[1;7D", want: KeyCtrlLeft},
		{seq: "\x1b[1;8C", want: KeyCtrlRight},
		{seq: "\x1b[1;10D", want: KeyAltLeft},
		{seq: "\x1b[1;11C", want: KeyAltRight},
		{seq: "\x1b[1;13D", want: KeyCtrlLeft},
		{seq: "\x1b[1;16C", want: KeyCtrlRight},
	}
	for _, tc := range arrowCases {
		if key := ParseKey(tc.seq); key.Type != tc.want {
			t.Fatalf("ParseKey(%q) = %#v, want %q", tc.seq, key, tc.want)
		}
	}
}

func TestParseCSIuKeySequences(t *testing.T) {
	cases := []struct {
		seq  string
		want KeyType
		rune rune
	}{
		{seq: "\x1b[97u", want: KeyRune, rune: 'a'},
		{seq: "\x1b[97;1u", want: KeyRune, rune: 'a'},
		{seq: "\x1b[97:65;1:1u", want: KeyRune, rune: 'a'},
		{seq: "\x1b[32u", want: KeyRune, rune: ' '},
		{seq: "\x1b[13u", want: KeyEnter},
		{seq: "\x1b[13;1u", want: KeyEnter},
		{seq: "\x1b[9u", want: KeyTab},
		{seq: "\x1b[27u", want: KeyEsc},
		{seq: "\x1b[127u", want: KeyBackspace},
		{seq: "\x1b[97;5u", want: KeyCtrlA},
		{seq: "\x1b[65;5u", want: KeyCtrlA},
		{seq: "\x1b[97:65;5:1u", want: KeyCtrlA},
		{seq: "\x1b[117;6u", want: KeyCtrlU},
		{seq: "\x1b[104;5u", want: KeyBackspace},
		{seq: "\x1b[105;5u", want: KeyTab},
		{seq: "\x1b[106;5u", want: KeyEnter},
		{seq: "\x1b[109;5u", want: KeyEnter},
		{seq: "\x1b[91;5u", want: KeyEsc},
		{seq: "\x1b[63;5u", want: KeyBackspace},
		{seq: "\x1b[98;3u", want: KeyAltB},
		{seq: "\x1b[98:66;3:1u", want: KeyAltB},
		{seq: "\x1b[68;3u", want: KeyAltD},
		{seq: "\x1b[127;3u", want: KeyAltBS},
		{seq: "\x1b[98;9u", want: KeyAltB},
		{seq: "\x1b[98:66;10:1u", want: KeyAltB},
		{seq: "\x1b[127;9u", want: KeyAltBS},
		{seq: "\x1b[97;13u", want: KeyCtrlA},
		{seq: "\x1b[117;14u", want: KeyCtrlU},
		{seq: "\x1b[13;2u", want: KeyShiftEnter},
		{seq: "\x1b[13;2:1u", want: KeyShiftEnter},
		{seq: "\x1b[9;2u", want: KeyShiftTab},
		{seq: "\x1b[90;2u", want: KeyRune, rune: 'Z'},
		{seq: "\x1b[90:122;2:1u", want: KeyRune, rune: 'Z'},
		{seq: "\x1b[97;3u", want: KeyUnknown},
	}
	for _, tc := range cases {
		key := ParseKey(tc.seq)
		if key.Type != tc.want || key.Rune != tc.rune {
			t.Fatalf("ParseKey(%q) = %#v, want type=%q rune=%q", tc.seq, key, tc.want, tc.rune)
		}
	}
}

func TestParseMouseSequences(t *testing.T) {
	press := ParseKey("\x1b[<64;10;4M")
	if press.Type != KeyMouse || press.MouseButton != 64 || press.MouseX != 10 || press.MouseY != 4 || press.MouseRelease {
		t.Fatalf("press = %#v", press)
	}
	release := ParseKey("\x1b[<0;1;2m")
	if release.Type != KeyMouse || release.MouseButton != 0 || release.MouseX != 1 || release.MouseY != 2 || !release.MouseRelease {
		t.Fatalf("release = %#v", release)
	}
	legacyPress := ParseKey("\x1b[M !!")
	if legacyPress.Type != KeyMouse || legacyPress.MouseButton != 0 || legacyPress.MouseX != 1 || legacyPress.MouseY != 1 || legacyPress.MouseRelease {
		t.Fatalf("legacy press = %#v", legacyPress)
	}
	legacyRelease := ParseKey("\x1b[M#%&")
	if legacyRelease.Type != KeyMouse || legacyRelease.MouseButton != 3 || legacyRelease.MouseX != 5 || legacyRelease.MouseY != 6 || !legacyRelease.MouseRelease {
		t.Fatalf("legacy release = %#v", legacyRelease)
	}
	legacyWheel := ParseKey("\x1b[M`*$")
	if legacyWheel.Type != KeyMouse || legacyWheel.MouseButton != 64 || legacyWheel.MouseX != 10 || legacyWheel.MouseY != 4 || legacyWheel.MouseRelease {
		t.Fatalf("legacy wheel = %#v", legacyWheel)
	}
	urxvtPress := ParseKey("\x1b[32;7;8M")
	if urxvtPress.Type != KeyMouse || urxvtPress.MouseButton != 0 || urxvtPress.MouseX != 7 || urxvtPress.MouseY != 8 || urxvtPress.MouseRelease {
		t.Fatalf("urxvt press = %#v", urxvtPress)
	}
	urxvtRelease := ParseKey("\x1b[35;5;6M")
	if urxvtRelease.Type != KeyMouse || urxvtRelease.MouseButton != 3 || urxvtRelease.MouseX != 5 || urxvtRelease.MouseY != 6 || !urxvtRelease.MouseRelease {
		t.Fatalf("urxvt release = %#v", urxvtRelease)
	}
	urxvtWheel := ParseKey("\x1b[96;10;4M")
	if urxvtWheel.Type != KeyMouse || urxvtWheel.MouseButton != 64 || urxvtWheel.MouseX != 10 || urxvtWheel.MouseY != 4 || urxvtWheel.MouseRelease {
		t.Fatalf("urxvt wheel = %#v", urxvtWheel)
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

func TestRenderMessagesWrapsANSIByVisibleWidth(t *testing.T) {
	lines := RenderMessages([]Message{{
		Role: RoleAssistant,
		Text: "plain " + CSISequence(31, "m") + "red words" + CSISequence("m") + " tail",
	}}, 24)
	if len(lines) < 2 {
		t.Fatalf("lines = %#v", lines)
	}
	for _, line := range lines {
		if width := TerminalVisibleWidth(line); width > 24 {
			t.Fatalf("line width = %d line=%q lines=%#v", width, line, lines)
		}
	}
	output := strings.Join(lines, "\n")
	if !strings.Contains(output, "\x1b[0;31m") || !strings.Contains(output, "\x1b[0m") {
		t.Fatalf("styled output = %q", output)
	}
	visible := StripANSI(output)
	if !strings.Contains(visible, "assistant: plain red") || strings.Contains(visible, "\x1b") {
		t.Fatalf("visible = %q", visible)
	}
}

func TestRenderMessagesExpandsCSIRepeatByVisibleWidth(t *testing.T) {
	body := "ab" + CSISequence(5, "b") + " tail"
	lines := renderMessageBodyLines(body, 6)
	if len(lines) != 2 {
		t.Fatalf("lines = %#v", lines)
	}
	for _, line := range lines {
		if width := TerminalVisibleWidth(line); width > 6 {
			t.Fatalf("line width = %d line=%q lines=%#v", width, line, lines)
		}
	}
	if got := StripANSI(strings.Join(lines, "")); got != "abbbbbb tail" {
		t.Fatalf("visible = %q lines=%#v", got, lines)
	}

	trimmed := padOrTrim(body, 4)
	if got := StripANSI(trimmed); got != "abbb" || TerminalVisibleWidth(trimmed) != 4 {
		t.Fatalf("trimmed = %q visible=%q width=%d", trimmed, got, TerminalVisibleWidth(trimmed))
	}
}

func TestRenderMessagesWrapsWideGraphemesByVisibleWidth(t *testing.T) {
	lines := RenderMessages([]Message{{
		Role: RoleAssistant,
		Text: "界界界😀a",
	}}, 15)
	if len(lines) < 3 {
		t.Fatalf("lines = %#v", lines)
	}
	for _, line := range lines {
		if width := TerminalVisibleWidth(line); width > 15 {
			t.Fatalf("line width = %d line=%q lines=%#v", width, line, lines)
		}
	}
	visible := strings.Join(lines, "\n")
	if !strings.Contains(visible, "assistant: 界界") || !strings.Contains(visible, "          界😀") {
		t.Fatalf("wide wrapped lines = %#v", lines)
	}
}

func TestPadOrTrimUsesGraphemeVisibleWidth(t *testing.T) {
	padded := padOrTrim("界a", 4)
	if padded != "界a " || TerminalVisibleWidth(padded) != 4 {
		t.Fatalf("padded = %q width=%d", padded, TerminalVisibleWidth(padded))
	}
	trimmed := padOrTrim("界界a", 4)
	if trimmed != "界界" || TerminalVisibleWidth(trimmed) != 4 {
		t.Fatalf("trimmed = %q width=%d", trimmed, TerminalVisibleWidth(trimmed))
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
	if action := keymap.Resolve(ParseKey("\x1b[1;3D")); action != ActionMoveWordLeft {
		t.Fatalf("alt-left action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b[1;5D")); action != ActionMoveWordLeft {
		t.Fatalf("ctrl-left action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x06")); action != ActionMoveRight {
		t.Fatalf("ctrl-f action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1bf")); action != ActionMoveWordRight {
		t.Fatalf("alt-f action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b[1;3C")); action != ActionMoveWordRight {
		t.Fatalf("alt-right action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b[1;5C")); action != ActionMoveWordRight {
		t.Fatalf("ctrl-right action = %q", action)
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
	if action := keymap.Resolve(ParseKey("\x0e")); action != ActionHistoryNext {
		t.Fatalf("ctrl-n action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0f")); action != ActionToggleTranscript {
		t.Fatalf("ctrl-o action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x10")); action != ActionHistoryPrevious {
		t.Fatalf("ctrl-p action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b[13;2u")); action != ActionInsertNewline {
		t.Fatalf("shift-enter action = %q", action)
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
		{Key: "esc", Action: Action("none")},
		{Key: "focus-in", Action: ActionReverseSearch},
		{Key: "shift-enter", Action: Action("insert-newline")},
		{Key: "ctrl-n", Action: Action("history-prev")},
		{Key: "ctrl-o", Action: Action("toggleTranscript")},
		{Key: "ctrl-h", Action: ActionDeleteWordBack},
		{Key: "ctrl-i", Action: ActionPageDown},
		{Key: "ctrl-m", Action: ActionSubmitPrompt},
		{Key: "btab", Action: ActionFocusPrevious},
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
	if action := keymap.Resolve(ParseKey("\x1b[13;2u")); action != ActionInsertNewline {
		t.Fatalf("shift-enter action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0e")); action != ActionHistoryPrevious {
		t.Fatalf("ctrl-n alias action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0f")); action != ActionToggleTranscript {
		t.Fatalf("ctrl-o camelCase action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\b")); action != ActionDeleteWordBack {
		t.Fatalf("ctrl-h terminal alias action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\t")); action != ActionPageDown {
		t.Fatalf("ctrl-i terminal alias action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\n")); action != ActionSubmitPrompt {
		t.Fatalf("ctrl-m terminal alias action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b[Z")); action != ActionFocusPrevious {
		t.Fatalf("btab alias action = %q", action)
	}
	for _, tc := range []struct {
		name string
		want Action
	}{
		{name: "page-down", want: ActionPageDown},
		{name: "delete-word-fwd", want: ActionDeleteWordFwd},
		{name: "search-history", want: ActionReverseSearch},
		{name: "submitPrompt", want: ActionSubmitPrompt},
		{name: "insertNewline", want: ActionInsertNewline},
		{name: "cancelPrompt", want: ActionCancel},
		{name: "abort", want: ActionCancel},
		{name: "stopGeneration", want: ActionInterrupt},
		{name: "quit", want: ActionExit},
		{name: "clearScreen", want: ActionRedraw},
		{name: "showTranscript", want: ActionToggleTranscript},
		{name: "toggleTasks", want: ActionToggleTodos},
		{name: "openExternalEditor", want: ActionExternalEditor},
		{name: "stashInput", want: ActionStashPrompt},
		{name: "cancelAgents", want: ActionKillAgents},
		{name: "deleteWordBackward", want: ActionDeleteWordBack},
		{name: "deleteWordForward", want: ActionDeleteWordFwd},
		{name: "pasteKillRing", want: ActionYank},
		{name: "yankPrevious", want: ActionYankPop},
		{name: "cursorLeft", want: ActionMoveLeft},
		{name: "cursorRight", want: ActionMoveRight},
		{name: "wordBackward", want: ActionMoveWordLeft},
		{name: "wordForward", want: ActionMoveWordRight},
		{name: "lineStart", want: ActionMoveStart},
		{name: "lineEnd", want: ActionMoveEnd},
		{name: "deletePreviousChar", want: ActionDeleteBackward},
		{name: "deleteNextChar", want: ActionDeleteForward},
		{name: "deleteToBeginning", want: ActionDeleteToStart},
		{name: "killLine", want: ActionDeleteToEnd},
		{name: "backwardKillWord", want: ActionDeleteWordBack},
		{name: "killWord", want: ActionDeleteWordFwd},
		{name: "historyPrevious", want: ActionHistoryPrevious},
		{name: "historyNext", want: ActionHistoryNext},
		{name: "scrollLineUp", want: ActionScrollUp},
		{name: "scrollLineDown", want: ActionScrollDown},
		{name: "halfUp", want: ActionHalfPageUp},
		{name: "halfDown", want: ActionHalfPageDown},
		{name: "scrollToTop", want: ActionScrollToTop},
		{name: "scrollToBottom", want: ActionScrollToBottom},
		{name: "nextFocus", want: ActionFocusNext},
		{name: "focusPrev", want: ActionFocusPrevious},
		{name: "acceptSelection", want: ActionConfirmSelection},
		{name: "search", want: ActionReverseSearch},
		{name: "unbound", want: ActionNone},
	} {
		action, err := ParseActionName(tc.name)
		if err != nil || action != tc.want {
			t.Fatalf("ParseActionName(%q) = %q, %v", tc.name, action, err)
		}
	}
	for _, name := range []string{"paste", "image-hint", "mouse", "focus-out", "shift-enter", "shift+return", "shiftEnter", "shiftReturn", "shiftTab", "backtab", "back-tab", "backTab", "btab", "s-tab", "sTab", "s-enter", "sReturn", "page-up", "pgup", "pg-up", "prior", "page-down", "pgdn", "pg-dn", "pgdown", "pg-down", "next", "arrowLeft", "arrowRight", "arrowUp", "arrowDown", "alt-b", "alt-d", "alt-f", "alt-y", "alt-backspace", "alt-left", "alt-right", "alt-arrow-left", "alt-arrow-right", "altB", "metaD", "optionF", "altY", "altBackspace", "altLeft", "optionRight", "altArrowLeft", "metaArrowRight", "optionArrowLeft", "m-b", "mB", "a-d", "aD", "opt-f", "optF", "m-left", "m-arrow-left", "mArrowLeft", "a-right", "aArrowRight", "optRight", "optArrowRight", "meta-b", "meta-d", "meta-f", "meta-y", "meta-backspace", "meta-left", "meta-right", "option-arrow-left", "option-arrow-right", "ctrl-b", "ctrl-d", "ctrl-f", "ctrl-g", "ctrl-u", "ctrl-k", "ctrl-l", "ctrl-n", "ctrl-o", "ctrl-p", "ctrl-s", "ctrl-t", "ctrl-w", "ctrl-x", "ctrl-y", "ctrl-h", "ctrl-i", "ctrl-m", "control-h", "control-i", "control-m", "c-h", "c-i", "c-m", "c-[", "c-?", "ctrlH", "controlI", "ctrlM", "cH", "cI", "cM", "ctrl-left", "ctrl-right", "ctrl-arrow-left", "ctrl-arrow-right", "ctrlArrowLeft", "controlArrowRight", "c-left", "c-arrow-left", "c-right", "c-arrow-right", "ctrlA", "controlX", "ctrlLeft", "controlRight", "cA", "cLeft", "cArrowRight", "control-left", "control-right", "control-arrow-left", "control-arrow-right"} {
		if key, err := ParseKeyName(name); err != nil || key == KeyUnknown {
			t.Fatalf("ParseKeyName(%q) = %q, %v", name, key, err)
		}
	}
	if _, err := KeymapFromSpecs(DefaultKeymap(), []BindingSpec{{Key: "wat", Action: ActionCancel}}); err == nil {
		t.Fatal("expected unknown key error")
	}
	if _, err := KeymapFromSpecs(DefaultKeymap(), []BindingSpec{{Key: "enter", Action: Action("wat")}}); err == nil {
		t.Fatal("expected unknown action error")
	}
}

func TestKeymapFromSpecsAcceptsTerminalControlCharacterAliases(t *testing.T) {
	keymap, err := KeymapFromSpecs(DefaultKeymap(), []BindingSpec{
		{Key: "cJ", Action: ActionSubmitPrompt},
		{Key: "c-[", Action: ActionCancel},
		{Key: "c?", Action: ActionDeleteWordBack},
		{Key: "mB", Action: ActionMoveStart},
		{Key: "sTab", Action: ActionMoveEnd},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\n")); action != ActionSubmitPrompt {
		t.Fatalf("ctrl-j alias action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b")); action != ActionCancel {
		t.Fatalf("ctrl-[ alias action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x7f")); action != ActionDeleteWordBack {
		t.Fatalf("ctrl-? alias action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1bb")); action != ActionMoveStart {
		t.Fatalf("meta-b alias action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b[Z")); action != ActionMoveEnd {
		t.Fatalf("shift-tab alias action = %q", action)
	}
	for _, name := range []string{"ctrl-j", "control-j", "ctrlJ", "controlJ", "c-j", "cJ", "ctrl-[", "control-[", "ctrl[", "control[", "c-[", "c[", "ctrl-?", "control-?", "ctrl?", "control?", "c-?", "c?", "m-b", "mB", "s-tab", "sTab"} {
		if key, err := ParseKeyName(name); err != nil || key == KeyUnknown {
			t.Fatalf("ParseKeyName(%q) = %q, %v", name, key, err)
		}
	}
}

func TestParseKeyBindingSpecsAcceptsJSONShapes(t *testing.T) {
	specs, err := ParseKeyBindingSpecs([]byte(`[
		{"keys": "ctrl-r", "command": "pageDown"},
		{"key_sequence": "ctrl-x ctrl-k", "action_name": "none"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	keymap, err := KeymapFromSpecs(DefaultKeymap(), specs)
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\x12")); action != ActionPageDown {
		t.Fatalf("ctrl-r action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x18")); action != ActionNone {
		t.Fatalf("ctrl-x prefix action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0b")); action != ActionNone {
		t.Fatalf("removed ctrl-x ctrl-k action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0b")); action != ActionDeleteToEnd {
		t.Fatalf("ctrl-k after cleared chord action = %q", action)
	}

	specs, err = ParseKeyBindingSpecs([]byte(`{"esc":"none","focus-out":"reverseSearch"}`))
	if err != nil {
		t.Fatal(err)
	}
	keymap, err = KeymapFromSpecs(DefaultKeymap(), specs)
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\x1b")); action != ActionNone {
		t.Fatalf("esc map action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b[O")); action != ActionReverseSearch {
		t.Fatalf("focus-out map action = %q", action)
	}

	wrapper := []byte(`{"keybindings":[{"keySequence":"shiftEnter","actionName":"insertNewline"}]}`)
	specs, err = ParseKeyBindingSpecs(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].Key != "shiftEnter" || specs[0].Action != Action("insertNewline") {
		t.Fatalf("wrapper specs = %#v", specs)
	}
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, wrapper, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadKeyBindingSpecs(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Key != "shiftEnter" {
		t.Fatalf("loaded specs = %#v", loaded)
	}

	specs, err = ParseKeyBindingSpecs([]byte(`{
		"ctrl-r": {"commandName": "pageDown"},
		"ctrl-x ctrl-k": null,
		"ctrl-j": {"commandId": "submitPrompt"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	keymap, err = KeymapFromSpecs(DefaultKeymap(), specs)
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\x12")); action != ActionPageDown {
		t.Fatalf("object map ctrl-r action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x18")); action != ActionNone {
		t.Fatalf("object map ctrl-x prefix action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0b")); action != ActionNone {
		t.Fatalf("object map removed ctrl-x ctrl-k action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\n")); action != ActionSubmitPrompt {
		t.Fatalf("object map ctrl-j action = %q", action)
	}

	specs, err = ParseKeyBindingSpecs([]byte(`{
		"shortcuts": {
			"ctrl-[": false,
			"shift-enter": {"command_id": "insert-newline"},
			"ctrl-w": {"shortcutKey": "ctrl-w", "actionName": "deleteWordBackward"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	keymap, err = KeymapFromSpecs(DefaultKeymap(), specs)
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\x1b")); action != ActionNone {
		t.Fatalf("shortcut map ctrl-[ action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x1b[13;2u")); action != ActionInsertNewline {
		t.Fatalf("shortcut map shift-enter action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x17")); action != ActionDeleteWordBack {
		t.Fatalf("shortcutKey override action = %q", action)
	}
}

func TestParseKeyBindingSpecsAcceptsNestedWrappers(t *testing.T) {
	wrapper := []byte(`{
		"data": {
			"settings": {
				"keyboard": {
					"bindings": [
						{"shortcutKey": "ctrl-r", "commandName": "pageDown"},
						{"keys": ["ctrl-x", "ctrl-k"], "command": "none"}
					]
				}
			}
		}
	}`)
	specs, err := ParseKeyBindingSpecs(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 || specs[0].Key != "ctrl-r" || specs[0].Action != Action("pageDown") || specs[1].Key != "ctrl-x ctrl-k" || specs[1].Action != Action("none") {
		t.Fatalf("specs = %#v", specs)
	}
	keymap, err := KeymapFromSpecs(DefaultKeymap(), specs)
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\x12")); action != ActionPageDown {
		t.Fatalf("ctrl-r action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x18")); action != ActionNone {
		t.Fatalf("ctrl-x prefix action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0b")); action != ActionNone {
		t.Fatalf("ctrl-x ctrl-k action = %q", action)
	}

	path := filepath.Join(t.TempDir(), "nested-keybindings.json")
	if err := os.WriteFile(path, wrapper, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadKeyBindingSpecs(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != len(specs) || loaded[0].Key != specs[0].Key || loaded[1].Key != specs[1].Key {
		t.Fatalf("loaded specs = %#v", loaded)
	}
}

func TestParseKeyBindingSpecsAcceptsCollectionAliases(t *testing.T) {
	specs, err := ParseKeyBindingSpecs([]byte(`{
		"keymap": {
			"ctrl-r": "pageDown",
			"ctrl-x ctrl-k": false
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 || specs[0].Key != "ctrl-r" || specs[0].Action != Action("pageDown") || specs[1].Key != "ctrl-x ctrl-k" || specs[1].Action != ActionNone {
		t.Fatalf("keymap specs = %#v", specs)
	}
	keymap, err := KeymapFromSpecs(DefaultKeymap(), specs)
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\x12")); action != ActionPageDown {
		t.Fatalf("keymap ctrl-r action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x18")); action != ActionNone {
		t.Fatalf("keymap ctrl-x prefix action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0b")); action != ActionNone {
		t.Fatalf("keymap ctrl-x ctrl-k action = %q", action)
	}

	specs, err = ParseKeyBindingSpecs([]byte(`{
		"keymap": {
			"bindings": [
				{"shortcut": "shift-enter", "command": "insertNewline"}
			]
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].Key != "shift-enter" || specs[0].Action != Action("insertNewline") {
		t.Fatalf("wrapped keymap specs = %#v", specs)
	}

	specs, err = ParseKeyBindingSpecs([]byte(`{
		"preferences": {
			"keyboardShortcuts": {
				"ctrl-w": {"commandName": "deleteWordBackward"}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].Key != "ctrl-w" || specs[0].Action != Action("deleteWordBackward") {
		t.Fatalf("keyboardShortcuts specs = %#v", specs)
	}
}

func TestParseKeyBindingSpecsAcceptsArrayKeySequences(t *testing.T) {
	specs, err := ParseKeyBindingSpecs([]byte(`[
		{"keys": ["ctrl-x", "ctrl-k"], "command": "pageDown"},
		{"shortcut": ["ctrl-r"], "command": false},
		{"key": ["ctrl-x", "ctrl-y"], "action_name": "reverseSearch"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 || specs[0].Key != "ctrl-x ctrl-k" || specs[1].Key != "ctrl-r" || specs[1].Action != ActionNone || specs[2].Key != "ctrl-x ctrl-y" {
		t.Fatalf("specs = %#v", specs)
	}
	keymap, err := KeymapFromSpecs(DefaultKeymap(), specs)
	if err != nil {
		t.Fatal(err)
	}
	if action := keymap.Resolve(ParseKey("\x12")); action != ActionNone {
		t.Fatalf("ctrl-r unbound action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x18")); action != ActionNone {
		t.Fatalf("ctrl-x prefix action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x0b")); action != ActionPageDown {
		t.Fatalf("ctrl-x ctrl-k action = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x18")); action != ActionNone {
		t.Fatalf("ctrl-x prefix for second chord = %q", action)
	}
	if action := keymap.Resolve(ParseKey("\x19")); action != ActionReverseSearch {
		t.Fatalf("ctrl-x ctrl-y action = %q", action)
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

func TestRunInteractionScriptAcceptsReverseSearchAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"keys": ["ctrl-r", "d", "e", "p"],
			"expectReverseSearch": {
				"isActive": true,
				"search": "dep",
				"cursorIndex": 3,
				"currentResult": "deploy new",
				"matchCount": 2
			}
		},
		{
			"key": "\u001b",
			"expectReverseSearch": {"visible": false}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, []string{"deploy old", "test", "deploy new"})
	if _, err := RunInteractionScriptChecked(&screen, steps); err != nil {
		t.Fatal(err)
	}

	noMatchSteps, err := ParseInteractionScript([]byte(`[
		{
			"keys": ["ctrl-r", "z", "z", "z"],
			"expectReverseSearch": {"open": true, "term": "zzz", "noMatches": true}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	noMatchScreen := NewREPLScreen(40, 8, []string{"deploy old", "test"})
	if _, err := RunInteractionScriptChecked(&noMatchScreen, noMatchSteps); err != nil {
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

func TestREPLScreenShiftEnterInsertsMultilinePrompt(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.ApplyKey(ParseKey("a"))
	event := screen.ApplyKey(ParseKey("\x1b[13;2u"))
	if event.Type != ScreenEventNone {
		t.Fatalf("shift-enter event = %#v", event)
	}
	screen.ApplyKey(ParseKey("b"))
	if screen.Prompt.Text != "a\nb" || screen.Prompt.Cursor != len([]rune("a\nb")) {
		t.Fatalf("prompt = %#v", screen.Prompt)
	}
	event = screen.ApplyKey(ParseKey("\n"))
	if event.Type != ScreenEventPromptSubmitted || event.Value != "a\nb" {
		t.Fatalf("submit event = %#v", event)
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

	screen.Dialog = &Dialog{Title: "Permission", Body: "Allow?", Actions: []string{"Allow", "Deny"}, ID: "perm_3", Kind: DialogPermission}
	shiftClick := screen.ApplyKey(ParseKey("\x1b[<4;13;5M"))
	if shiftClick.Type != ScreenEventDialogAction || shiftClick.Value != "Deny" || shiftClick.DialogID != "perm_3" || shiftClick.DialogKind != DialogPermission {
		t.Fatalf("dialog shift mouse click = %#v", shiftClick)
	}
	if screen.Dialog != nil {
		t.Fatalf("dialog should close after shift mouse click")
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

func TestDialogRuntimeNormalizesPermissionActionAliases(t *testing.T) {
	runtime := NewDialogRuntime()
	runtime.RequestPermission(PermissionRequest{ID: "perm_reject", ToolName: "Write", Actions: []string{"Approve", "Reject"}})
	rejected := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Reject", DialogID: "perm_reject", DialogKind: DialogPermission})
	if !rejected.Found || rejected.Status != DialogResultDenied || rejected.Action != "Reject" {
		t.Fatalf("rejected = %#v", rejected)
	}

	runtime.RequestPermission(PermissionRequest{ID: "perm_lower", ToolName: "Edit", Actions: []string{"approve", "deny"}})
	denied := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "deny", DialogID: "perm_lower", DialogKind: DialogPermission})
	if !denied.Found || denied.Status != DialogResultDenied || denied.Action != "deny" {
		t.Fatalf("denied = %#v", denied)
	}

	runtime.RequestPermission(PermissionRequest{ID: "perm_approve", ToolName: "Read", Actions: []string{"Approve"}})
	approved := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Approve", DialogID: "perm_approve", DialogKind: DialogPermission})
	if !approved.Found || approved.Status != DialogResultAllowed || approved.Action != "Approve" {
		t.Fatalf("approved = %#v", approved)
	}

	runtime.RequestPermission(PermissionRequest{ID: "perm_cancel", ToolName: "Bash", Actions: []string{"Run", "Cancel"}})
	cancelled := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Cancel", DialogID: "perm_cancel", DialogKind: DialogPermission})
	if !cancelled.Found || cancelled.Status != DialogResultCancelled || cancelled.Action != "Cancel" {
		t.Fatalf("cancelled = %#v", cancelled)
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

func TestDialogRuntimeNormalizesTaskStateAliases(t *testing.T) {
	runtime := NewDialogRuntime()
	runtime.UpsertTask(TaskStatus{ID: "active", State: "in_progress", Progress: 120})
	runtime.UpsertTask(TaskStatus{ID: "queued", State: "queued"})
	runtime.UpsertTask(TaskStatus{ID: "success", State: "success"})
	runtime.UpsertTask(TaskStatus{ID: "error", State: "error"})
	runtime.UpsertTask(TaskStatus{ID: "canceled", State: "canceled"})

	if runtime.Tasks["active"].State != TaskRunning || runtime.Tasks["active"].Progress != 100 || runtime.Tasks["active"].Title != "active" {
		t.Fatalf("active task = %#v", runtime.Tasks["active"])
	}
	if runtime.Tasks["queued"].State != TaskPending || runtime.Tasks["success"].State != TaskCompleted || runtime.Tasks["error"].State != TaskFailed || runtime.Tasks["canceled"].State != TaskCancelled {
		t.Fatalf("tasks = %#v", runtime.Tasks)
	}
	if status := runtime.StatusLine(""); !strings.Contains(status, "running: 1") || !strings.Contains(status, "pending: 1") || !strings.Contains(status, "failed: 1") || !strings.Contains(status, "cancelled: 1") || !strings.Contains(status, "completed: 1") {
		t.Fatalf("status = %q", status)
	}

	dialog := TaskDialog([]TaskStatus{{ID: "done", State: "success"}})
	if !strings.Contains(dialog.Body, "done [completed]") {
		t.Fatalf("task dialog = %#v", dialog)
	}

	cancelled := runtime.CancelTasks("stop")
	if len(cancelled) != 2 || cancelled[0].ID != "active" || cancelled[1].ID != "queued" {
		t.Fatalf("cancelled = %#v", cancelled)
	}
	if runtime.Tasks["success"].State != TaskCompleted || runtime.Tasks["error"].State != TaskFailed || runtime.Tasks["canceled"].State != TaskCancelled {
		t.Fatalf("terminal tasks changed = %#v", runtime.Tasks)
	}
}

func TestDialogRuntimeCancelsCancelableTasksDeterministically(t *testing.T) {
	runtime := NewDialogRuntime()
	runtime.UpsertTask(TaskStatus{ID: "b", Title: "Build", State: TaskRunning, Detail: "compile", Progress: 25})
	runtime.UpsertTask(TaskStatus{ID: "a", Title: "Search", State: TaskPending})
	runtime.UpsertTask(TaskStatus{ID: "c", Title: "Done", State: TaskCompleted, Progress: 100})
	runtime.UpsertTask(TaskStatus{ID: "d", Title: "Failed", State: TaskFailed, Detail: "boom"})
	runtime.OpenTasksDialog()

	cancelled := runtime.CancelTasks("interrupted")
	if len(cancelled) != 2 || cancelled[0].ID != "a" || cancelled[1].ID != "b" {
		t.Fatalf("cancelled = %#v", cancelled)
	}
	for _, id := range []string{"a", "b"} {
		task := runtime.Tasks[id]
		if task.State != TaskCancelled || task.Detail != "interrupted" {
			t.Fatalf("task %s = %#v", id, task)
		}
	}
	if runtime.Tasks["c"].State != TaskCompleted || runtime.Tasks["d"].State != TaskFailed {
		t.Fatalf("terminal tasks should remain unchanged: %#v", runtime.Tasks)
	}
	if runtime.Active == nil || !strings.Contains(runtime.Active.Body, "Build [cancelled] 25% - interrupted") || !strings.Contains(runtime.Active.Body, "Search [cancelled] - interrupted") {
		t.Fatalf("active task dialog = %#v", runtime.Active)
	}
	if status := runtime.StatusLine("ready"); !strings.Contains(status, "failed: 1") || !strings.Contains(status, "cancelled: 2") || !strings.Contains(status, "completed: 1") {
		t.Fatalf("status = %q", status)
	}
	if got := runtime.CancelTasks("again"); len(got) != 0 {
		t.Fatalf("second cancel should not touch terminal tasks: %#v", got)
	}
}

func TestDialogRuntimeRefreshesActiveTaskDialog(t *testing.T) {
	runtime := NewDialogRuntime()
	screen := NewREPLScreen(48, 8, nil)
	runtime.StartTask("task_1", "Search", "starting")
	runtime.OpenTasksDialog()
	runtime.ApplyToScreen(&screen, "ready")
	if screen.Dialog == nil || !strings.Contains(screen.Dialog.Body, "Search [running] - starting") {
		t.Fatalf("task dialog before refresh = %#v", screen.Dialog)
	}
	runtime.UpdateTaskProgress("task_1", "halfway", 50)
	runtime.ApplyToScreen(&screen, "ready")
	if screen.Dialog == nil || !strings.Contains(screen.Dialog.Body, "Search [running] 50% - halfway") {
		t.Fatalf("task dialog after progress = %#v", screen.Dialog)
	}
	runtime.CompleteTask("task_1", "done")
	runtime.ApplyToScreen(&screen, "ready")
	if screen.Dialog == nil || !strings.Contains(screen.Dialog.Body, "Search [completed] 100% - done") {
		t.Fatalf("task dialog after complete = %#v", screen.Dialog)
	}
	runtime.RemoveTask("task_1")
	runtime.ApplyToScreen(&screen, "ready")
	if screen.Dialog == nil || !strings.Contains(screen.Dialog.Body, "No active tasks.") {
		t.Fatalf("task dialog after remove = %#v", screen.Dialog)
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

func TestDialogRuntimePromotesQueuedPermissionAfterResolve(t *testing.T) {
	runtime := NewDialogRuntime()
	screen := NewREPLScreen(40, 8, nil)
	runtime.RequestPermission(PermissionRequest{ID: "old", ToolName: "Write"})
	runtime.RequestPermission(PermissionRequest{ID: "new", ToolName: "Edit"})
	runtime.ApplyToScreen(&screen, "ready")
	if screen.Dialog == nil || screen.Dialog.ID != "new" {
		t.Fatalf("screen dialog before resolve = %#v", screen.Dialog)
	}
	result := runtime.ResolveScreenEvent(&screen, ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow", DialogID: "new", DialogKind: DialogPermission}, "ready")
	if !result.Found || result.ID != "new" || result.Status != DialogResultAllowed {
		t.Fatalf("result = %#v", result)
	}
	if screen.Dialog == nil || screen.Dialog.ID != "old" || screen.Dialog.Kind != DialogPermission {
		t.Fatalf("queued permission should be promoted to screen: %#v", screen.Dialog)
	}
	if !strings.Contains(screen.Status, "permissions: 1") {
		t.Fatalf("status after promotion = %q", screen.Status)
	}
	result = runtime.ResolveScreenEvent(&screen, ScreenEvent{Type: ScreenEventDialogAction, Value: "Deny", DialogID: "old", DialogKind: DialogPermission}, "ready")
	if !result.Found || result.ID != "old" || result.Status != DialogResultDenied {
		t.Fatalf("old result = %#v", result)
	}
	if screen.Dialog != nil || len(runtime.Permissions) != 0 {
		t.Fatalf("runtime after queue drain = dialog %#v permissions %#v", screen.Dialog, runtime.Permissions)
	}
}

func TestDialogRuntimeInteractionScriptResolvesPermissionFlow(t *testing.T) {
	runtime := NewDialogRuntime()
	screen := NewREPLScreen(42, 8, nil)
	runtime.StartTask("task_1", "Search", "running ripgrep")
	runtime.RequestPermission(PermissionRequest{ID: "perm_1", ToolName: "Bash", Path: "/tmp/project"})
	found := true
	stale := false
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
			ExpectDialogResult:   &DialogResultExpectation{ID: "perm_1", Kind: DialogPermission, Action: "Allow Session", Status: DialogResultAllowed, Found: &found, Stale: &stale},
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

func TestDialogRuntimeInteractionScriptAppliesRuntimeMutations(t *testing.T) {
	runtime := NewDialogRuntime()
	screen := NewREPLScreen(52, 9, nil)
	found := true
	taskCount := 1
	emptyTaskCount := 0
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", []ScriptStep{
		{
			RequestPermission:    &PermissionRequest{ID: "perm_1", ToolName: "Bash", Path: "/tmp/project"},
			UpsertTask:           &TaskStatus{ID: "task_1", Title: "Search", State: TaskRunning, Detail: "starting", Progress: 10},
			ExpectDialog:         &DialogExpectation{Active: true, ID: "perm_1", Kind: DialogPermission},
			ExpectTasks:          &TasksExpectation{Count: &taskCount, Contains: []TaskExpectation{{ID: "task_1", State: TaskRunning, Detail: "starting"}}},
			ExpectStatusContains: []string{"permissions: 1", "running: 1"},
		},
		{
			Key:                     "\n",
			ExpectEvent:             &ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow", DialogID: "perm_1", DialogKind: DialogPermission},
			ExpectDialogResult:      &DialogResultExpectation{ID: "perm_1", Kind: DialogPermission, Action: "Allow", Status: DialogResultAllowed, Found: &found},
			ExpectDialog:            &DialogExpectation{Active: false},
			ExpectStatusContains:    []string{"running: 1"},
			ExpectStatusNotContains: []string{"permissions:"},
		},
		{
			UpsertTask:      &TaskStatus{ID: "task_1", Title: "Search", State: TaskCompleted, Detail: "done", Progress: 100},
			OpenTasksDialog: true,
			ExpectDialog:    &DialogExpectation{Active: true, ID: "tasks", Kind: DialogTask},
			ExpectSnapshotContains: []string{
				"Search [completed] 100% - done",
			},
		},
		{
			RemoveTaskID: "task_1",
			ExpectDialog: &DialogExpectation{Active: true, ID: "tasks", Kind: DialogTask},
			ExpectTasks:  &TasksExpectation{Count: &emptyTaskCount},
			ExpectSnapshotContains: []string{
				"No active tasks.",
			},
			ExpectSnapshotNotContains: []string{
				"Search [completed]",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].Status != DialogResultAllowed {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
}

func TestDialogRuntimeInteractionScriptCancelsTasks(t *testing.T) {
	runtime := NewDialogRuntime()
	screen := NewREPLScreen(52, 9, nil)
	taskCount := 2
	cancelledProgress := 25
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", []ScriptStep{
		{
			UpsertTask: &TaskStatus{ID: "task_1", Title: "Search", State: TaskRunning, Detail: "running", Progress: 25},
		},
		{
			UpsertTask: &TaskStatus{ID: "task_2", Title: "Build", State: TaskPending},
		},
		{
			CancelAllTasks:    true,
			CancelTasksDetail: "interrupted",
			OpenTasksDialog:   true,
			ExpectDialog:      &DialogExpectation{Active: true, ID: "tasks", Kind: DialogTask},
			ExpectTasks: &TasksExpectation{
				Count: &taskCount,
				StateCounts: map[string]int{
					TaskCancelled: 2,
				},
				Contains: []TaskExpectation{
					{ID: "task_1", State: TaskCancelled, Detail: "interrupted", Progress: &cancelledProgress},
					{ID: "task_2", State: TaskCancelled, Detail: "interrupted"},
				},
			},
			ExpectStatusContains: []string{"cancelled: 2"},
			ExpectSnapshotContains: []string{
				"Search [cancelled] 25% - interrupted",
				"Build [cancelled] - interrupted",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 0 {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
}

func TestDialogRuntimeInteractionScriptCancelsPermissions(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"request_permission":{"id":"old","tool_name":"Write"}},
		{"request_permission":{"id":"new","tool_name":"Edit"}},
		{
			"cancel_permission_id":"old",
			"expect_dialog_results":{"id":"old","kind":"permission","status":"cancelled","found":true},
			"expect_dialog":{"active":true,"id":"new","kind":"permission"},
			"expect_status_contains":["permissions: 1"]
		},
		{
			"cancel_active_dialog":true,
			"expect_dialog_result":{"id":"new","kind":"permission","status":"cancelled","found":true},
			"expect_dialog":{"active":false},
			"expect_status_not_contains":["permissions:"]
		},
		{"request_permission":{"id":"b","tool_name":"Bash"}},
		{"request_permission":{"id":"a","tool_name":"Read"}},
		{
			"cancel_all_permissions":true,
			"expect_dialog_results":[
				{"id":"a","kind":"permission","status":"cancelled","found":true},
				{"id":"b","kind":"permission","status":"cancelled","found":true}
			],
			"expect_dialog":{"active":false},
			"expect_status_not_contains":["permissions:"]
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewDialogRuntime()
	screen := NewREPLScreen(52, 9, nil)
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 4 {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
	for index, want := range []string{"old", "new", "a", "b"} {
		if result.DialogResults[index].ID != want || result.DialogResults[index].Status != DialogResultCancelled {
			t.Fatalf("dialog result %d = %#v", index, result.DialogResults[index])
		}
	}
	if runtime.Active != nil || len(runtime.Permissions) != 0 {
		t.Fatalf("runtime after scripted cancellation = %#v", runtime)
	}
}

func TestDialogRuntimeInteractionScriptAcceptsMutationAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"permission": {"requestId": "perm_1", "tool": "Bash", "path": "/tmp/project", "choices": ["Allow", "Deny"]},
			"expectDialog": {"visible": true, "dialogId": "perm_1", "dialogKind": "permission"},
			"expectStatusContains": "permissions: 1"
		},
		{
			"cancelPermission": "perm_1",
			"expectDialogResult": {"dialogId": "perm_1", "resultStatus": "cancelled", "exists": true},
			"expectDialog": {"visible": false},
			"expectStatusNotContains": "permissions: 1"
		},
		{
			"taskStatus": {"taskId": "task_1", "taskTitle": "Build", "status": "running", "statusText": "go test", "progressPercent": 20}
		},
		{
			"showTasks": true,
			"expectDialog": {"visible": true, "dialogId": "tasks", "dialogKind": "task"},
			"expectTasks": {"taskCount": 1, "statusCounts": {"running": 1}, "contains": {"taskId": "task_1", "status": "running", "statusText": "go test", "progressPercent": 20}},
			"expectSnapshotContains": "Build [running] 20% - go test"
		},
		{
			"cancelTasks": true,
			"cancelReason": "interrupted",
			"expectTasks": {"taskCount": 1, "statusCounts": {"cancelled": 1}, "contains": {"taskId": "task_1", "status": "cancelled", "statusText": "interrupted", "progressPercent": 20}},
			"expectStatusContains": "cancelled: 1"
		},
		{
			"removeTask": "task_1",
			"expectTasks": {"taskCount": 0},
			"expectSnapshotContains": "No active tasks."
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(52, 9, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].ID != "perm_1" || result.DialogResults[0].Status != DialogResultCancelled {
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
	if _, ok := runtime.Permissions["new"]; ok {
		t.Fatalf("runtime after cancel = %#v", runtime)
	}
	if runtime.Active == nil || runtime.Active.ID != "old" {
		t.Fatalf("old permission should be promoted after active cancel: %#v", runtime.Active)
	}
	allowed := runtime.Resolve(ScreenEvent{Type: ScreenEventDialogAction, Value: "Allow", DialogID: "old", DialogKind: DialogPermission})
	if !allowed.Found || allowed.Status != DialogResultAllowed || runtime.Active != nil || len(runtime.Permissions) != 0 {
		t.Fatalf("runtime after promoted resolve = result %#v runtime %#v", allowed, runtime)
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

func TestREPLScreenMouseModifiersAndDragSelection(t *testing.T) {
	screen := NewREPLScreen(20, 6, nil)
	screen.SetMessages([]Message{
		{Role: RoleSystem, Text: "one"},
		{Role: RoleSystem, Text: "two"},
		{Role: RoleSystem, Text: "three"},
		{Role: RoleSystem, Text: "four"},
		{Role: RoleSystem, Text: "five"},
		{Role: RoleSystem, Text: "six"},
	})
	bottom := strings.Join(screen.Viewport.Visible(), "\n")
	screen.ApplyKey(ParseKey("\x1b[<68;4;4M"))
	scrolledUp := strings.Join(screen.Viewport.Visible(), "\n")
	if scrolledUp == bottom {
		t.Fatalf("modified mouse wheel up did not scroll: before=%q after=%q", bottom, scrolledUp)
	}
	screen.ApplyKey(ParseKey("\x1b[<69;4;4M"))
	if got := strings.Join(screen.Viewport.Visible(), "\n"); got != bottom {
		t.Fatalf("modified mouse wheel down mismatch: bottom=%q got=%q", bottom, got)
	}

	click := screen.ApplyKey(ParseKey("\x1b[<16;1;2M"))
	if click.Type != ScreenEventViewportSelected || screen.SelectedViewportLine < 0 {
		t.Fatalf("modified viewport click = %#v selected=%d", click, screen.SelectedViewportLine)
	}
	selected := screen.SelectedViewportLine
	drag := screen.ApplyKey(ParseKey("\x1b[<32;1;3M"))
	if drag.Type != ScreenEventViewportSelected || screen.SelectedViewportLine == selected {
		t.Fatalf("viewport drag = %#v selected=%d previous=%d", drag, screen.SelectedViewportLine, selected)
	}
	motion := screen.ApplyKey(ParseKey("\x1b[<35;1;3M"))
	if motion.Type != ScreenEventNone {
		t.Fatalf("buttonless motion should be ignored: %#v", motion)
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

func TestREPLScreenVimMatchingPairMotionsAndOperators(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "call(foo[bar])\nnext")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"g", "g"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("0"))
	screen.ApplyKey(ParseKey("%"))
	if screen.Prompt.Cursor != len([]rune("call(foo[bar]")) {
		t.Fatalf("cursor after %% = %d", screen.Prompt.Cursor)
	}
	screen.ApplyKey(ParseKey("%"))
	if screen.Prompt.Cursor != len([]rune("call")) {
		t.Fatalf("cursor after reverse %% = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"f", "[", "%"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("call(foo[bar")) {
		t.Fatalf("cursor after nested bracket %% = %d", screen.Prompt.Cursor)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "call(foo[bar])\nnext")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"g", "g"} {
		screen.ApplyKey(ParseKey(seq))
	}
	screen.ApplyKey(ParseKey("0"))
	for _, seq := range []string{"f", "(", "d", "%"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "call\nnext" || screen.Prompt.Cursor != len([]rune("call")) || screen.VimRegister != "(foo[bar])" {
		t.Fatalf("after d%% from open screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "call(foo) tail")
	screen.ApplyKey(ParseKey("\x1b"))
	screen.ApplyKey(ParseKey("0"))
	for _, seq := range []string{"f", ")", "d", "%"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "call tail" || screen.Prompt.Cursor != len([]rune("call")) || screen.VimRegister != "(foo)" {
		t.Fatalf("after d%% from close screen = %#v", screen)
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

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\n  two\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"k", "^"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("one\n  ")) {
		t.Fatalf("cursor after multiline ^ = %d", screen.Prompt.Cursor)
	}
	screen.ApplyKey(ParseKey("e"))
	for _, seq := range []string{"d", "^"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "one\n  o\nthree" || screen.Prompt.Cursor != len([]rune("one\n  ")) || screen.VimRegister != "tw" {
		t.Fatalf("after multiline d^ screen = %#v", screen)
	}
}

func TestREPLScreenVimLineLocalEndMotions(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\n  two\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"k", "$"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("one\n  two")) {
		t.Fatalf("cursor after multiline $ = %d", screen.Prompt.Cursor)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\n  two\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"k", "D"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "one\n\nthree" || screen.Prompt.Cursor != len([]rune("one\n")) || screen.VimRegister != "  two" {
		t.Fatalf("after multiline D screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\n  two\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"k", "A", "!"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.VimMode != VimInsert || screen.Prompt.Text != "one\n  two!\nthree" || screen.Prompt.Cursor != len([]rune("one\n  two!")) {
		t.Fatalf("after multiline A screen = %#v", screen)
	}
}

func TestREPLScreenVimLineLocalStartMotions(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\n  two\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"k", "$", "0"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("one\n")) {
		t.Fatalf("cursor after multiline 0 = %d", screen.Prompt.Cursor)
	}
	screen.ApplyKey(ParseKey("$"))
	for _, seq := range []string{"d", "0"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "one\n\nthree" || screen.Prompt.Cursor != len([]rune("one\n")) || screen.VimRegister != "  two" {
		t.Fatalf("after multiline d0 screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\n  two\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"k", "5", "|"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Cursor != len([]rune("one\n  tw")) {
		t.Fatalf("cursor after multiline 5| = %d", screen.Prompt.Cursor)
	}
	for _, seq := range []string{"$", "d", "3", "|"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "one\n  \nthree" || screen.Prompt.Cursor != len([]rune("one\n  ")) || screen.VimRegister != "two" {
		t.Fatalf("after multiline d3| screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\n  two\nthree")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"k", "I", "!"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.VimMode != VimInsert || screen.Prompt.Text != "one\n  !two\nthree" || screen.Prompt.Cursor != len([]rune("one\n  !")) {
		t.Fatalf("after multiline I screen = %#v", screen)
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

func TestREPLScreenVimBackwardEndOperatorMotions(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "alpha beta.gamma delta")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "w", "w", "d", "g", "e"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "alpha betgamma delta" || screen.Prompt.Cursor != len([]rune("alpha bet")) || screen.VimRegister != "a." {
		t.Fatalf("after dge screen = %#v", screen)
	}
	for _, seq := range []string{"w", "."} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "alpha betgammdelta" || screen.Prompt.Cursor != len([]rune("alpha betgamm")) || screen.VimRegister != "a " {
		t.Fatalf("after dge dot repeat screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "alpha beta.gamma delta")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "w", "w", "y", "g", "e"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "alpha beta.gamma delta" || screen.Prompt.Cursor != len([]rune("alpha bet")) || screen.VimRegister != "a." {
		t.Fatalf("after yge screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "foo.bar baz-qux tail")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"0", "W", "W", "c", "g", "E"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.VimMode != VimInsert || screen.Prompt.Text != "foo.bar baz-qutail" || screen.Prompt.Cursor != len([]rune("foo.bar baz-qu")) || screen.VimRegister != "x " {
		t.Fatalf("after cgE screen = %#v", screen)
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
	for _, seq := range []string{"g", "g", "y", "y", "p"} {
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
	for _, seq := range []string{"g", "g", "J"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.Prompt.Text != "one two\nthree" || screen.Prompt.Cursor != len([]rune("one")) {
		t.Fatalf("after J screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\ntwo")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"g", "g", "o"} {
		screen.ApplyKey(ParseKey(seq))
	}
	if screen.VimMode != VimInsert || screen.Prompt.Text != "one\n\ntwo" || screen.Prompt.Cursor != len([]rune("one\n")) {
		t.Fatalf("after o screen = %#v", screen)
	}

	screen = NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	typePromptText(&screen, "one\ntwo")
	screen.ApplyKey(ParseKey("\x1b"))
	for _, seq := range []string{"g", "g", ">", ">", "<", "<"} {
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
	options := TerminalModeOptions{MouseTracking: true, FocusEvents: true, BracketedPaste: true, ExtendedKeys: true}
	enter := lifecycle.EnterInteractive(options)
	if !lifecycle.AlternateScreen || !lifecycle.CursorHidden || !lifecycle.MouseTracking || !lifecycle.FocusEvents || !lifecycle.BracketedPaste || !lifecycle.ExtendedKeys {
		t.Fatalf("lifecycle after enter = %#v", lifecycle)
	}
	for _, want := range []string{EnterAlternateScreen, EnableMouseTracking, EnableFocusEvents, EnableBracketedPaste, EnableExtendedKeys} {
		if !strings.Contains(enter, want) {
			t.Fatalf("enter missing %q in %q", want, enter)
		}
	}
	if again := lifecycle.EnterInteractive(options); again != "" {
		t.Fatalf("second enter should be idempotent: %q", again)
	}
	reassert := lifecycle.ReassertTerminalModes(options)
	for _, want := range []string{EnableMouseTracking, EnableFocusEvents, EnableBracketedPaste, ReassertExtendedKeys} {
		if !strings.Contains(reassert, want) {
			t.Fatalf("reassert missing %q in %q", want, reassert)
		}
	}
	reassertInteractive := lifecycle.ReassertInteractive(options)
	for _, want := range []string{EnterAlternateScreen, ClearScreen, HomeCursor, HideCursor, EnableMouseTracking, EnableFocusEvents, EnableBracketedPaste, ReassertExtendedKeys} {
		if !strings.Contains(reassertInteractive, want) {
			t.Fatalf("interactive reassert missing %q in %q", want, reassertInteractive)
		}
	}
	exit := lifecycle.ExitInteractive()
	if lifecycle.AlternateScreen || lifecycle.CursorHidden || lifecycle.MouseTracking || lifecycle.FocusEvents || lifecycle.BracketedPaste || lifecycle.ExtendedKeys {
		t.Fatalf("lifecycle after exit = %#v", lifecycle)
	}
	for _, want := range []string{DisableMouseTracking, DisableFocusEvents, DisableBracketedPaste, DisableExtendedKeys, ShowCursor, ExitAlternateScreen} {
		if !strings.Contains(exit, want) {
			t.Fatalf("exit missing %q in %q", want, exit)
		}
	}
	if again := lifecycle.ExitInteractive(); again != "" {
		t.Fatalf("second exit should be idempotent: %q", again)
	}
	if reassert := lifecycle.ReassertInteractive(options); reassert != "" {
		t.Fatalf("inactive interactive reassert should be empty: %q", reassert)
	}
}

func TestScreenLifecycleReconcilesTerminalModes(t *testing.T) {
	var lifecycle ScreenLifecycle
	all := TerminalModeOptions{MouseTracking: true, FocusEvents: true, BracketedPaste: true, ExtendedKeys: true}
	_ = lifecycle.EnterInteractive(all)

	pasteOnly := TerminalModeOptions{BracketedPaste: true}
	update := lifecycle.EnterInteractive(pasteOnly)
	if !lifecycle.AlternateScreen || !lifecycle.CursorHidden || lifecycle.MouseTracking || lifecycle.FocusEvents || !lifecycle.BracketedPaste || lifecycle.ExtendedKeys {
		t.Fatalf("lifecycle after paste-only update = %#v", lifecycle)
	}
	for _, want := range []string{DisableMouseTracking, DisableFocusEvents, DisableExtendedKeys} {
		if !strings.Contains(update, want) {
			t.Fatalf("update missing %q in %q", want, update)
		}
	}
	for _, notWant := range []string{DisableBracketedPaste, EnableBracketedPaste, EnterAlternateScreen} {
		if strings.Contains(update, notWant) {
			t.Fatalf("update unexpectedly contains %q in %q", notWant, update)
		}
	}
	if again := lifecycle.EnterInteractive(pasteOnly); again != "" {
		t.Fatalf("same mode update should be idempotent: %q", again)
	}

	mouseAndPaste := TerminalModeOptions{MouseTracking: true, BracketedPaste: true}
	update = lifecycle.EnterInteractive(mouseAndPaste)
	if !lifecycle.MouseTracking || lifecycle.FocusEvents || !lifecycle.BracketedPaste || lifecycle.ExtendedKeys {
		t.Fatalf("lifecycle after mouse update = %#v", lifecycle)
	}
	if !strings.Contains(update, EnableMouseTracking) || strings.Contains(update, EnableBracketedPaste) {
		t.Fatalf("mouse update = %q", update)
	}
}

func TestClearTerminalSequences(t *testing.T) {
	if seq := ClearTerminalSequence(); seq != ClearScreen+ClearScrollback+HomeCursor {
		t.Fatalf("clear terminal = %q", seq)
	}
	if seq := ClearLegacyWindowsTerminalSequence(); seq != ClearScreen+LegacyWindowsHomeCursor {
		t.Fatalf("legacy clear terminal = %q", seq)
	}
}

func TestCSISequenceHelpers(t *testing.T) {
	if seq := CSISequence(); seq != CSIPrefix {
		t.Fatalf("csi prefix = %q", seq)
	}
	if !IsCSIParam('0') || !IsCSIParam('?') || IsCSIParam('/') || IsCSIParam('@') {
		t.Fatalf("CSI param range mismatch")
	}
	if !IsCSIIntermediate(' ') || !IsCSIIntermediate('/') || IsCSIIntermediate('0') {
		t.Fatalf("CSI intermediate range mismatch")
	}
	if !IsCSIFinal('@') || !IsCSIFinal('~') || IsCSIFinal('?') || IsCSIFinal(0x7f) {
		t.Fatalf("CSI final range mismatch")
	}
	if seq := CSISequence("H"); seq != HomeCursor {
		t.Fatalf("csi home = %q", seq)
	}
	if seq := CursorPosition(3, 4); seq != "\x1b[3;4H" {
		t.Fatalf("cursor position = %q", seq)
	}
	if seq := CursorMove(-2, 3); seq != "\x1b[2D\x1b[3B" {
		t.Fatalf("cursor move = %q", seq)
	}
	if seq := CursorUp(0) + CursorForward(0); seq != "" {
		t.Fatalf("zero cursor move = %q", seq)
	}
	if seq := EraseToStartOfLine() + EraseLineSequence() + EraseScreenSequence(); seq != "\x1b[1K"+EraseLine+ClearScreen {
		t.Fatalf("erase helpers = %q", seq)
	}
	if seq := EraseLinesSequence(3); seq != EraseLine+"\x1b[1A"+EraseLine+"\x1b[1A"+EraseLine+CursorLeft {
		t.Fatalf("erase lines = %q", seq)
	}
	if seq := EraseLinesSequence(0); seq != "" {
		t.Fatalf("zero erase lines = %q", seq)
	}
	if seq := ScrollUp(2) + ScrollDown(3) + SetScrollRegion(4, 10) + ResetScrollRegion; seq != "\x1b[2S\x1b[3T\x1b[4;10r\x1b[r" {
		t.Fatalf("scroll helpers = %q", seq)
	}
	if seq := ScrollUp(0) + ScrollDown(0); seq != "" {
		t.Fatalf("zero scroll = %q", seq)
	}
	if seq := SetCursorStyleSequence(CursorStyleBlock, false) + SetCursorStyleSequence(CursorStyleUnderline, true) + SetCursorStyleSequence(CursorStyleBar, false); seq != "\x1b[2 q\x1b[3 q\x1b[6 q" {
		t.Fatalf("cursor styles = %q", seq)
	}
	if seq := SetCursorStyleSequence(CursorStyle("unknown"), true); seq != "\x1b[0 q" {
		t.Fatalf("unknown cursor style = %q", seq)
	}
	if seq := PasteStart + PasteEnd + FocusInSequence + FocusOutSequence; seq != "\x1b[200~\x1b[201~\x1b[I\x1b[O" {
		t.Fatalf("input markers = %q", seq)
	}
	if key := ParseKey(FocusInSequence); key.Type != KeyFocusIn {
		t.Fatalf("focus in key = %#v", key)
	}
	if key := ParseKey(FocusOutSequence); key.Type != KeyFocusOut {
		t.Fatalf("focus out key = %#v", key)
	}
}

func TestParseCSISequenceActions(t *testing.T) {
	if action, ok := ParseCSISequence("x"); ok || action.Type != "" {
		t.Fatalf("non-csi parsed = %#v", action)
	}
	if action, ok := ParseCSISequence(CSIPrefix + "31"); !ok || action.Type != CSIActionUnknown || action.Sequence != CSIPrefix+"31" {
		t.Fatalf("incomplete csi action = %#v ok=%v", action, ok)
	}

	sgr, ok := ParseCSISequence(CSISequence(31, 1, "m"))
	if !ok || sgr.Type != CSIActionSGR || sgr.SGRParams != "31;1" {
		t.Fatalf("sgr action = %#v", sgr)
	}
	reset, ok := ParseCSISequence(CSISequence("!p"))
	if !ok || reset.Type != CSIActionReset {
		t.Fatalf("soft reset action = %#v ok=%v", reset, ok)
	}

	cursorCases := []struct {
		seq  string
		want CSICursorAction
	}{
		{seq: CSISequence("A"), want: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorUp, Count: 1}},
		{seq: CSISequence(0, "B"), want: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorDown, Count: 0}},
		{seq: CSISequence(2, "e"), want: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorDown, Count: 2}},
		{seq: CSISequence(4, "C"), want: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorForward, Count: 4}},
		{seq: CSISequence(6, "a"), want: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorForward, Count: 6}},
		{seq: CSISequence(5, "D"), want: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorBack, Count: 5}},
		{seq: CSISequence(7, "j"), want: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorBack, Count: 7}},
		{seq: CSISequence(3, "k"), want: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorUp, Count: 3}},
		{seq: CSISequence(2, "E"), want: CSICursorAction{Type: CSICursorActionNextLine, Count: 2}},
		{seq: CSISequence(3, "F"), want: CSICursorAction{Type: CSICursorActionPrevLine, Count: 3}},
		{seq: CSISequence(2, "I"), want: CSICursorAction{Type: CSICursorActionTab, Count: 2}},
		{seq: CSISequence(4, "Z"), want: CSICursorAction{Type: CSICursorActionBackTab, Count: 4}},
		{seq: CSISequence("g"), want: CSICursorAction{Type: CSICursorActionTabClear, Count: 0}},
		{seq: CSISequence(3, "g"), want: CSICursorAction{Type: CSICursorActionTabClear, Count: 3}},
		{seq: CSISequence(12, "G"), want: CSICursorAction{Type: CSICursorActionColumn, Column: 12}},
		{seq: CSISequence(14, "`"), want: CSICursorAction{Type: CSICursorActionColumn, Column: 14}},
		{seq: CSISequence("2:7H"), want: CSICursorAction{Type: CSICursorActionPosition, Row: 2, Column: 7}},
		{seq: CSISequence(9, "d"), want: CSICursorAction{Type: CSICursorActionRow, Row: 9}},
		{seq: CSISequence(4, 8, "f"), want: CSICursorAction{Type: CSICursorActionPosition, Row: 4, Column: 8}},
		{seq: CursorSave, want: CSICursorAction{Type: CSICursorActionSave}},
		{seq: CursorRestore, want: CSICursorAction{Type: CSICursorActionRestore}},
		{seq: CSISequence("?1048h"), want: CSICursorAction{Type: CSICursorActionSave}},
		{seq: CSISequence("?1048l"), want: CSICursorAction{Type: CSICursorActionRestore}},
		{seq: CSISequence("4 q"), want: CSICursorAction{Type: CSICursorActionStyle, Style: CursorStyleUnderline, Blinking: false}},
		{seq: CSISequence("99 q"), want: CSICursorAction{Type: CSICursorActionStyle, Style: CursorStyleBlock, Blinking: true}},
		{seq: ShowCursor, want: CSICursorAction{Type: CSICursorActionShow}},
		{seq: HideCursor, want: CSICursorAction{Type: CSICursorActionHide}},
	}
	for _, tc := range cursorCases {
		action, ok := ParseCSISequence(tc.seq)
		if !ok || action.Type != CSIActionCursor || !reflect.DeepEqual(action.Cursor, tc.want) {
			t.Fatalf("cursor action for %q = %#v, want %#v", tc.seq, action, tc.want)
		}
	}

	eraseCases := []struct {
		seq  string
		want CSIEraseAction
	}{
		{seq: CSISequence("J"), want: CSIEraseAction{Type: CSIEraseActionDisplay, Region: CSIEraseToEnd}},
		{seq: CSISequence(1, "J"), want: CSIEraseAction{Type: CSIEraseActionDisplay, Region: CSIEraseToStart}},
		{seq: CSISequence(2, "J"), want: CSIEraseAction{Type: CSIEraseActionDisplay, Region: CSIEraseAll}},
		{seq: CSISequence(3, "J"), want: CSIEraseAction{Type: CSIEraseActionDisplay, Region: CSIEraseScrollback}},
		{seq: CSISequence(9, "J"), want: CSIEraseAction{Type: CSIEraseActionDisplay, Region: CSIEraseToEnd}},
		{seq: CSISequence("?2J"), want: CSIEraseAction{Type: CSIEraseActionDisplay, Region: CSIEraseAll, Selective: true}},
		{seq: CSISequence("K"), want: CSIEraseAction{Type: CSIEraseActionLine, Region: CSIEraseToEnd}},
		{seq: CSISequence("?K"), want: CSIEraseAction{Type: CSIEraseActionLine, Region: CSIEraseToEnd, Selective: true}},
		{seq: CSISequence(2, "K"), want: CSIEraseAction{Type: CSIEraseActionLine, Region: CSIEraseAll}},
		{seq: CSISequence("N"), want: CSIEraseAction{Type: CSIEraseActionField, Region: CSIEraseToEnd}},
		{seq: CSISequence(1, "O"), want: CSIEraseAction{Type: CSIEraseActionArea, Region: CSIEraseToStart}},
		{seq: CSISequence(6, "X"), want: CSIEraseAction{Type: CSIEraseActionChars, Count: 6}},
	}
	for _, tc := range eraseCases {
		action, ok := ParseCSISequence(tc.seq)
		if !ok || action.Type != CSIActionErase || !reflect.DeepEqual(action.Erase, tc.want) {
			t.Fatalf("erase action for %q = %#v, want %#v", tc.seq, action, tc.want)
		}
	}

	editCases := []struct {
		seq  string
		want CSIEditAction
	}{
		{seq: CSISequence(2, "@"), want: CSIEditAction{Type: CSIEditActionInsertChars, Count: 2}},
		{seq: CSISequence(3, "P"), want: CSIEditAction{Type: CSIEditActionDeleteChars, Count: 3}},
		{seq: CSISequence(4, "L"), want: CSIEditAction{Type: CSIEditActionInsertLines, Count: 4}},
		{seq: CSISequence(5, "M"), want: CSIEditAction{Type: CSIEditActionDeleteLines, Count: 5}},
		{seq: CSISequence("b"), want: CSIEditAction{Type: CSIEditActionRepeatChars, Count: 1}},
		{seq: CSISequence(4, "b"), want: CSIEditAction{Type: CSIEditActionRepeatChars, Count: 4}},
		{seq: CSISequence("'}"), want: CSIEditAction{Type: CSIEditActionInsertCols, Count: 1}},
		{seq: CSISequence("3'}"), want: CSIEditAction{Type: CSIEditActionInsertCols, Count: 3}},
		{seq: CSISequence("2'~"), want: CSIEditAction{Type: CSIEditActionDeleteCols, Count: 2}},
	}
	for _, tc := range editCases {
		action, ok := ParseCSISequence(tc.seq)
		if !ok || action.Type != CSIActionEdit || action.Edit != tc.want {
			t.Fatalf("edit action for %q = %#v, want %#v", tc.seq, action, tc.want)
		}
	}

	reportCases := []struct {
		seq  string
		want CSIReportAction
	}{
		{seq: CSISequence("c"), want: CSIReportAction{Type: CSIReportActionDeviceAttrs}},
		{seq: CSISequence(">1c"), want: CSIReportAction{Type: CSIReportActionDeviceAttrs, Code: 1, PrivateMode: '>'}},
		{seq: CSISequence("=2c"), want: CSIReportAction{Type: CSIReportActionDeviceAttrs, Code: 2, PrivateMode: '='}},
		{seq: CSISequence("x"), want: CSIReportAction{Type: CSIReportActionTerminalParams}},
		{seq: CSISequence(1, "x"), want: CSIReportAction{Type: CSIReportActionTerminalParams, Code: 1}},
		{seq: CSISequence("?2x"), want: CSIReportAction{Type: CSIReportActionTerminalParams, Code: 2, PrivateMode: '?'}},
		{seq: CSISequence("t"), want: CSIReportAction{Type: CSIReportActionWindow}},
		{seq: CSISequence(14, "t"), want: CSIReportAction{Type: CSIReportActionWindow, Code: 14}},
		{seq: CSISequence("?18t"), want: CSIReportAction{Type: CSIReportActionWindow, Code: 18, PrivateMode: '?'}},
		{seq: CSISequence(5, "n"), want: CSIReportAction{Type: CSIReportActionDeviceStatus, Code: 5}},
		{seq: CSISequence(6, "n"), want: CSIReportAction{Type: CSIReportActionCursorPosition, Code: 6}},
		{seq: CSISequence("?25n"), want: CSIReportAction{Type: CSIReportActionUnknown, Code: 25, PrivateMode: '?'}},
		{seq: CSISequence("4$p"), want: CSIReportAction{Type: CSIReportActionModeRequest, Code: 4}},
		{seq: CSISequence("?25$p"), want: CSIReportAction{Type: CSIReportActionModeRequest, Code: 25, PrivateMode: '?'}},
		{seq: CSISequence("4;1$y"), want: CSIReportAction{Type: CSIReportActionModeStatus, Code: 4, Status: 1}},
		{seq: CSISequence("?25;2$y"), want: CSIReportAction{Type: CSIReportActionModeStatus, Code: 25, Status: 2, PrivateMode: '?'}},
	}
	for _, tc := range reportCases {
		action, ok := ParseCSISequence(tc.seq)
		if !ok || action.Type != CSIActionReport || action.Report != tc.want {
			t.Fatalf("report action for %q = %#v, want %#v", tc.seq, action, tc.want)
		}
	}

	scrollCases := []struct {
		seq  string
		want CSIScrollAction
	}{
		{seq: CSISequence(2, "S"), want: CSIScrollAction{Type: CSIScrollActionUp, Count: 2}},
		{seq: CSISequence(3, "T"), want: CSIScrollAction{Type: CSIScrollActionDown, Count: 3}},
		{seq: CSISequence("4 @"), want: CSIScrollAction{Type: CSIScrollActionLeft, Count: 4}},
		{seq: CSISequence("2 A"), want: CSIScrollAction{Type: CSIScrollActionRight, Count: 2}},
		{seq: CSISequence(4, 10, "r"), want: CSIScrollAction{Type: CSIScrollActionSetRegion, Top: 4, Bottom: 10}},
		{seq: ResetScrollRegion, want: CSIScrollAction{Type: CSIScrollActionSetRegion, Top: 1}},
		{seq: CSISequence(";10r"), want: CSIScrollAction{Type: CSIScrollActionSetRegion, Top: 1, Bottom: 10}},
		{seq: CSISequence(10, 80, "s"), want: CSIScrollAction{Type: CSIScrollActionSetHorizontalRegion, Left: 10, Right: 80}},
		{seq: CSISequence(";120s"), want: CSIScrollAction{Type: CSIScrollActionSetHorizontalRegion, Left: 1, Right: 120}},
	}
	for _, tc := range scrollCases {
		action, ok := ParseCSISequence(tc.seq)
		if !ok || action.Type != CSIActionScroll || !reflect.DeepEqual(action.Scroll, tc.want) {
			t.Fatalf("scroll action for %q = %#v, want %#v", tc.seq, action, tc.want)
		}
	}

	modeCases := []struct {
		seq  string
		want CSIModeAction
	}{
		{seq: "\x1b[4h", want: CSIModeAction{Type: CSIModeActionInsert, Enabled: true}},
		{seq: "\x1b[4l", want: CSIModeAction{Type: CSIModeActionInsert, Enabled: false}},
		{seq: "\x1b[20h", want: CSIModeAction{Type: CSIModeActionLineFeed, Enabled: true}},
		{seq: "\x1b[20l", want: CSIModeAction{Type: CSIModeActionLineFeed, Enabled: false}},
		{seq: "\x1b[?1h", want: CSIModeAction{Type: CSIModeActionApplicationCursor, Enabled: true}},
		{seq: "\x1b[?1l", want: CSIModeAction{Type: CSIModeActionApplicationCursor, Enabled: false}},
		{seq: "\x1b[?3h", want: CSIModeAction{Type: CSIModeActionColumn, Enabled: true}},
		{seq: "\x1b[?3l", want: CSIModeAction{Type: CSIModeActionColumn, Enabled: false}},
		{seq: "\x1b[?40h", want: CSIModeAction{Type: CSIModeActionAllowColumnSwitch, Enabled: true}},
		{seq: "\x1b[?40l", want: CSIModeAction{Type: CSIModeActionAllowColumnSwitch, Enabled: false}},
		{seq: "\x1b[?5h", want: CSIModeAction{Type: CSIModeActionReverseVideo, Enabled: true}},
		{seq: "\x1b[?5l", want: CSIModeAction{Type: CSIModeActionReverseVideo, Enabled: false}},
		{seq: "\x1b[?6h", want: CSIModeAction{Type: CSIModeActionOrigin, Enabled: true}},
		{seq: "\x1b[?6l", want: CSIModeAction{Type: CSIModeActionOrigin, Enabled: false}},
		{seq: "\x1b[?7h", want: CSIModeAction{Type: CSIModeActionAutoWrap, Enabled: true}},
		{seq: "\x1b[?7l", want: CSIModeAction{Type: CSIModeActionAutoWrap, Enabled: false}},
		{seq: "\x1b[?8h", want: CSIModeAction{Type: CSIModeActionAutoRepeat, Enabled: true}},
		{seq: "\x1b[?8l", want: CSIModeAction{Type: CSIModeActionAutoRepeat, Enabled: false}},
		{seq: "\x1b[?12h", want: CSIModeAction{Type: CSIModeActionCursorBlink, Enabled: true}},
		{seq: "\x1b[?12l", want: CSIModeAction{Type: CSIModeActionCursorBlink, Enabled: false}},
		{seq: "\x1b[?44h", want: CSIModeAction{Type: CSIModeActionMarginBell, Enabled: true}},
		{seq: "\x1b[?44l", want: CSIModeAction{Type: CSIModeActionMarginBell, Enabled: false}},
		{seq: "\x1b[?45h", want: CSIModeAction{Type: CSIModeActionReverseWrap, Enabled: true}},
		{seq: "\x1b[?45l", want: CSIModeAction{Type: CSIModeActionReverseWrap, Enabled: false}},
		{seq: "\x1b[?46h", want: CSIModeAction{Type: CSIModeActionLogging, Enabled: true}},
		{seq: "\x1b[?46l", want: CSIModeAction{Type: CSIModeActionLogging, Enabled: false}},
		{seq: "\x1b[?66h", want: CSIModeAction{Type: CSIModeActionApplicationKeypad, Enabled: true}},
		{seq: "\x1b[?66l", want: CSIModeAction{Type: CSIModeActionApplicationKeypad, Enabled: false}},
		{seq: "\x1b[?67h", want: CSIModeAction{Type: CSIModeActionBackarrowKey, Enabled: true}},
		{seq: "\x1b[?67l", want: CSIModeAction{Type: CSIModeActionBackarrowKey, Enabled: false}},
		{seq: "\x1b[?69h", want: CSIModeAction{Type: CSIModeActionLeftRightMargin, Enabled: true}},
		{seq: "\x1b[?69l", want: CSIModeAction{Type: CSIModeActionLeftRightMargin, Enabled: false}},
		{seq: "\x1b[?95h", want: CSIModeAction{Type: CSIModeActionNoClearOnColumn, Enabled: true}},
		{seq: "\x1b[?95l", want: CSIModeAction{Type: CSIModeActionNoClearOnColumn, Enabled: false}},
		{seq: EnterAlternateScreen, want: CSIModeAction{Type: CSIModeActionAlternateScreen, Enabled: true}},
		{seq: "\x1b[?47l", want: CSIModeAction{Type: CSIModeActionAlternateScreen, Enabled: false}},
		{seq: "\x1b[?1046h", want: CSIModeAction{Type: CSIModeActionAltScreenSwitch, Enabled: true}},
		{seq: "\x1b[?1046l", want: CSIModeAction{Type: CSIModeActionAltScreenSwitch, Enabled: false}},
		{seq: "\x1b[?1047h", want: CSIModeAction{Type: CSIModeActionAlternateScreen, Enabled: true}},
		{seq: "\x1b[?1047l", want: CSIModeAction{Type: CSIModeActionAlternateScreen, Enabled: false}},
		{seq: EnableBracketedPaste, want: CSIModeAction{Type: CSIModeActionBracketedPaste, Enabled: true}},
		{seq: "\x1b[?9h", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: true, MouseMode: CSIMouseTrackingX10}},
		{seq: "\x1b[?9l", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: false, MouseMode: CSIMouseTrackingOff}},
		{seq: "\x1b[?1000h", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: true, MouseMode: CSIMouseTrackingNormal}},
		{seq: "\x1b[?1001h", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: true, MouseMode: CSIMouseTrackingHighlight}},
		{seq: "\x1b[?1001l", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: false, MouseMode: CSIMouseTrackingOff}},
		{seq: "\x1b[?1002h", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: true, MouseMode: CSIMouseTrackingButton}},
		{seq: "\x1b[?1003l", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: false, MouseMode: CSIMouseTrackingOff}},
		{seq: "\x1b[?1005h", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: true, MouseMode: CSIMouseTrackingUTF8}},
		{seq: "\x1b[?1006h", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: true, MouseMode: CSIMouseTrackingSGR}},
		{seq: "\x1b[?1006l", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: false, MouseMode: CSIMouseTrackingOff}},
		{seq: "\x1b[?1007h", want: CSIModeAction{Type: CSIModeActionAlternateScroll, Enabled: true}},
		{seq: "\x1b[?1007l", want: CSIModeAction{Type: CSIModeActionAlternateScroll, Enabled: false}},
		{seq: "\x1b[?1015h", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: true, MouseMode: CSIMouseTrackingURXVT}},
		{seq: "\x1b[?1015l", want: CSIModeAction{Type: CSIModeActionMouseTracking, Enabled: false, MouseMode: CSIMouseTrackingOff}},
		{seq: EnableFocusEvents, want: CSIModeAction{Type: CSIModeActionFocusEvents, Enabled: true}},
		{seq: BeginSynchronizedOutput, want: CSIModeAction{Type: CSIModeActionSynchronized, Enabled: true}},
		{seq: EndSynchronizedOutput, want: CSIModeAction{Type: CSIModeActionSynchronized, Enabled: false}},
	}
	for _, tc := range modeCases {
		action, ok := ParseCSISequence(tc.seq)
		if !ok || action.Type != CSIActionMode || !reflect.DeepEqual(action.Mode, tc.want) {
			t.Fatalf("mode action for %q = %#v, want %#v", tc.seq, action, tc.want)
		}
	}

	multiMode, ok := ParseCSISequence(CSISequence("?1000;1006;2004h"))
	if !ok || multiMode.Type != CSIActionMode || len(multiMode.Modes) != 3 {
		t.Fatalf("multi private mode action = %#v ok=%v", multiMode, ok)
	}
	if multiMode.Mode != multiMode.Modes[0] || multiMode.Modes[0].MouseMode != CSIMouseTrackingNormal || multiMode.Modes[1].MouseMode != CSIMouseTrackingSGR || multiMode.Modes[2].Type != CSIModeActionBracketedPaste || !multiMode.Modes[2].Enabled {
		t.Fatalf("multi private modes = %#v", multiMode.Modes)
	}

	multiNormalMode, ok := ParseCSISequence(CSISequence("4;20l"))
	if !ok || multiNormalMode.Type != CSIActionMode || len(multiNormalMode.Modes) != 2 || multiNormalMode.Modes[0].Type != CSIModeActionInsert || multiNormalMode.Modes[0].Enabled || multiNormalMode.Modes[1].Type != CSIModeActionLineFeed || multiNormalMode.Modes[1].Enabled {
		t.Fatalf("multi normal modes = %#v ok=%v", multiNormalMode, ok)
	}

	mixedCursorMode, ok := ParseCSISequence(CSISequence("?25;1000h"))
	if !ok || mixedCursorMode.Type != CSIActionMode || len(mixedCursorMode.Modes) != 2 {
		t.Fatalf("mixed cursor/mode action = %#v ok=%v", mixedCursorMode, ok)
	}
	if mixedCursorMode.Modes[0].Type != CSIModeActionCursorVisible || !mixedCursorMode.Modes[0].Enabled || mixedCursorMode.Modes[1].Type != CSIModeActionMouseTracking || mixedCursorMode.Modes[1].MouseMode != CSIMouseTrackingNormal || !mixedCursorMode.Modes[1].Enabled {
		t.Fatalf("mixed cursor/mode list = %#v", mixedCursorMode.Modes)
	}

	parser := NewTerminalParser()
	actions := parser.Feed(CSISequence("?1000;1006;2004h"))
	if len(actions) != 1 || actions[0].Type != TerminalActionMode || len(actions[0].Modes) != 3 || actions[0].Mode != actions[0].Modes[0] {
		t.Fatalf("terminal parser multi mode actions = %#v", actions)
	}

	actions = parser.Feed(CSISequence("?25;1000h"))
	if len(actions) != 1 || actions[0].Type != TerminalActionMode || len(actions[0].Modes) != 2 || actions[0].Modes[0].Type != CSIModeActionCursorVisible || actions[0].Modes[1].MouseMode != CSIMouseTrackingNormal {
		t.Fatalf("terminal parser mixed cursor/mode actions = %#v", actions)
	}

	actions = parser.Feed(CSISequence("?K"))
	if len(actions) != 1 || actions[0].Type != TerminalActionErase || actions[0].Erase.Type != CSIEraseActionLine || !actions[0].Erase.Selective {
		t.Fatalf("terminal parser selective erase actions = %#v", actions)
	}

	actions = parser.Feed(CSISequence(2, "O"))
	if len(actions) != 1 || actions[0].Type != TerminalActionErase || actions[0].Erase.Type != CSIEraseActionArea || actions[0].Erase.Region != CSIEraseAll {
		t.Fatalf("terminal parser area erase actions = %#v", actions)
	}

	actions = parser.Feed(CSISequence("3'}") + CSISequence("2'~"))
	if len(actions) != 2 || actions[0].Type != TerminalActionEdit || actions[0].Edit.Type != CSIEditActionInsertCols || actions[0].Edit.Count != 3 || actions[1].Type != TerminalActionEdit || actions[1].Edit.Type != CSIEditActionDeleteCols || actions[1].Edit.Count != 2 {
		t.Fatalf("terminal parser column edit actions = %#v", actions)
	}

	actions = parser.Feed(CSISequence("?25$p"))
	if len(actions) != 1 || actions[0].Type != TerminalActionReport || actions[0].Report.Type != CSIReportActionModeRequest || actions[0].Report.Code != 25 || actions[0].Report.PrivateMode != '?' {
		t.Fatalf("terminal parser mode request actions = %#v", actions)
	}

	actions = parser.Feed(CSISequence("?25;2$y"))
	if len(actions) != 1 || actions[0].Type != TerminalActionReport || actions[0].Report.Type != CSIReportActionModeStatus || actions[0].Report.Code != 25 || actions[0].Report.Status != 2 || actions[0].Report.PrivateMode != '?' {
		t.Fatalf("terminal parser mode status actions = %#v", actions)
	}

	actions = parser.Feed(CSISequence(4, "j") + CSISequence(2, "k"))
	if len(actions) != 2 || actions[0].Type != TerminalActionCursor || actions[0].Cursor.Direction != CSICursorBack || actions[0].Cursor.Count != 4 || actions[1].Type != TerminalActionCursor || actions[1].Cursor.Direction != CSICursorUp || actions[1].Cursor.Count != 2 {
		t.Fatalf("terminal parser alternate backward cursor actions = %#v", actions)
	}

	actions = parser.Feed(CSISequence(5, 40, "s"))
	if len(actions) != 1 || actions[0].Type != TerminalActionScroll || actions[0].Scroll.Type != CSIScrollActionSetHorizontalRegion || actions[0].Scroll.Left != 5 || actions[0].Scroll.Right != 40 {
		t.Fatalf("terminal parser horizontal region actions = %#v", actions)
	}

	actions = parser.Feed(CSISequence("3 @") + CSISequence("2 A"))
	if len(actions) != 2 || actions[0].Type != TerminalActionScroll || actions[0].Scroll.Type != CSIScrollActionLeft || actions[0].Scroll.Count != 3 || actions[1].Type != TerminalActionScroll || actions[1].Scroll.Type != CSIScrollActionRight || actions[1].Scroll.Count != 2 {
		t.Fatalf("terminal parser horizontal scroll actions = %#v", actions)
	}
}

func TestParseESCSequenceActions(t *testing.T) {
	if !IsESCFinal('0') || !IsESCFinal('~') || IsESCFinal('/') || IsESCFinal(0x7f) {
		t.Fatalf("ESC final range mismatch")
	}
	if action, ok := ParseESCSequence("x"); ok || action.Type != "" {
		t.Fatalf("non-esc parsed = %#v", action)
	}
	if action, ok := ParseESCSequence(CSISequence("H")); ok || action.Type != "" {
		t.Fatalf("csi parsed as esc = %#v", action)
	}
	if action, ok := ParseESCContent(""); ok || action.Type != "" {
		t.Fatalf("empty esc content parsed = %#v", action)
	}
	if action, ok := ParseESCSequence("\x1b(B"); ok || action.Type != "" {
		t.Fatalf("charset selection should be ignored = %#v", action)
	}

	reset, ok := ParseESCSequence(ESCResetSequence)
	if !ok || reset.Type != ESCActionReset {
		t.Fatalf("reset action = %#v", reset)
	}

	cursorCases := []struct {
		seq  string
		want CSICursorAction
	}{
		{seq: ESCSaveCursor, want: CSICursorAction{Type: CSICursorActionSave}},
		{seq: ESCRestoreCursor, want: CSICursorAction{Type: CSICursorActionRestore}},
		{seq: ESCIndex, want: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorDown, Count: 1}},
		{seq: ESCReverseIndex, want: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorUp, Count: 1}},
		{seq: ESCNextLine, want: CSICursorAction{Type: CSICursorActionNextLine, Count: 1}},
		{seq: ESCTabSet, want: CSICursorAction{Type: CSICursorActionTabSet}},
	}
	for _, tc := range cursorCases {
		action, ok := ParseESCSequence(tc.seq)
		if !ok || action.Type != ESCActionCursor || !reflect.DeepEqual(action.Cursor, tc.want) {
			t.Fatalf("esc cursor action for %q = %#v, want %#v", tc.seq, action, tc.want)
		}
	}

	unknown, ok := ParseESCContent("]not-osc")
	if !ok || unknown.Type != ESCActionUnknown || unknown.Sequence != "\x1b]not-osc" {
		t.Fatalf("unknown esc action = %#v", unknown)
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

func TestCaptureANSISnapshotCanUseSynchronizedOutput(t *testing.T) {
	snapshot := CaptureANSISnapshotWithOptions("sync", 32, 6, Frame{
		Messages: []Message{{Role: RoleAssistant, Text: "hello"}},
		Status:   "ready",
		Prompt:   NewPromptState(nil),
	}, RenderOptions{SynchronizedOutput: true})
	if !strings.HasPrefix(snapshot.Output, BeginSynchronizedOutput) || !strings.HasSuffix(snapshot.Output, EndSynchronizedOutput) {
		t.Fatalf("output = %q", snapshot.Output)
	}
	if strings.Contains(snapshot.Text, "\x1b[") || !strings.Contains(snapshot.Text, "assistant: hello") {
		t.Fatalf("text = %q", snapshot.Text)
	}

	plain := RenderOnceWithOptions(32, 6, Frame{Status: "ready"}, RenderOptions{})
	if strings.Contains(plain, BeginSynchronizedOutput) || strings.Contains(plain, EndSynchronizedOutput) {
		t.Fatalf("plain output = %q", plain)
	}
}

func TestTerminalTitleSequenceStripsANSIControls(t *testing.T) {
	title := TerminalTitleSequence("Claude \x1b[31mCode\x1b[0m\x1b]0;hidden\x07")
	if title != OSCPrefix+OSCSetTitleAndIcon+";Claude Code"+OSCTerminator {
		t.Fatalf("title = %q", title)
	}
	if visible := StripANSI("a\x1b]0;hidden\x07b\x1bPignored\x1b\\c"); visible != "abc" {
		t.Fatalf("visible = %q", visible)
	}
	if unsafe := OSCSequence("0", "a\x07b\x1b\\c\x1bd"); unsafe != OSCPrefix+"0;abcd"+OSCTerminator {
		t.Fatalf("unsafe = %q", unsafe)
	}
}

func TestOSCSequenceCanUseStringTerminator(t *testing.T) {
	st := OSCSequenceWithStringTerminator(OSCSetTitleAndIcon, "Claude")
	wantST := OSCPrefix + OSCSetTitleAndIcon + ";Claude" + OSCStringTerminator
	if st != wantST {
		t.Fatalf("st = %q, want %q", st, wantST)
	}

	fallback := OSCSequenceWithTerminator("", OSCSetTitleAndIcon, "Claude")
	wantFallback := OSCPrefix + OSCSetTitleAndIcon + ";Claude" + OSCTerminator
	if fallback != wantFallback {
		t.Fatalf("fallback = %q, want %q", fallback, wantFallback)
	}
}

func TestParseOSCContent(t *testing.T) {
	title := ParseOSCContent("0;Claude")
	if title.Type != OSCActionTitle || title.Title.Type != "both" || title.Title.Title != "Claude" {
		t.Fatalf("title = %#v", title)
	}

	link := ParseOSCContent("8;id=f6ehdo;https://example.com/docs?x=1;y=2")
	if link.Type != OSCActionLink || link.Hyperlink.URL != "https://example.com/docs?x=1;y=2" || link.Hyperlink.Params["id"] != "f6ehdo" {
		t.Fatalf("link = %#v", link)
	}

	directory := ParseOSCContent("7;file://localhost/Users/sqlrush/project%20one")
	if directory.Type != OSCActionDirectory || !directory.Directory.Valid || directory.Directory.Scheme != "file" || directory.Directory.Host != "localhost" || directory.Directory.Path != "/Users/sqlrush/project one" {
		t.Fatalf("directory = %#v", directory)
	}

	tab := ParseOSCContent(`21337;status=Ready\; go;indicator=#000001`)
	if tab.Type != OSCActionTabStatus || tab.TabStatus.Status == nil || *tab.TabStatus.Status != "Ready; go" {
		t.Fatalf("tab = %#v", tab)
	}
	if tab.TabStatus.Indicator == nil || *tab.TabStatus.Indicator != (RGBColor{R: 0, G: 0, B: 1}) {
		t.Fatalf("tab indicator = %#v", tab.TabStatus.Indicator)
	}

	clipboard := ParseOSCContent("52;c;Y29weSBtZQ==")
	if clipboard.Type != OSCActionClipboard || !clipboard.Clipboard.Valid || clipboard.Clipboard.Selection != "c" || clipboard.Clipboard.Text != "copy me" {
		t.Fatalf("clipboard = %#v", clipboard)
	}

	progress := ParseOSCContent("9;4;1;101")
	if progress.Type != OSCActionProgress || progress.Progress.State != TerminalProgressRunning || progress.Progress.Percent != 100 {
		t.Fatalf("progress = %#v", progress)
	}

	iterm := ParseOSCContent("9;\n\nClaude:\nBuild complete")
	if iterm.Type != OSCActionNotification || iterm.Notification.Provider != "iterm2" || iterm.Notification.Title != "Claude" || iterm.Notification.Message != "Build complete" {
		t.Fatalf("iterm notification = %#v", iterm)
	}

	kitty := ParseOSCContent("99;i=7:p=body;Build complete")
	if kitty.Type != OSCActionNotification || kitty.Notification.Provider != "kitty" || kitty.Notification.ID != "7" || kitty.Notification.Part != "body" || kitty.Notification.Message != "Build complete" {
		t.Fatalf("kitty notification = %#v", kitty)
	}

	ghostty := ParseOSCContent("777;notify;Claude;Build complete")
	if ghostty.Type != OSCActionNotification || ghostty.Notification.Provider != "ghostty" || ghostty.Notification.Title != "Claude" || ghostty.Notification.Message != "Build complete" {
		t.Fatalf("ghostty notification = %#v", ghostty)
	}

	shellStart := ParseOSCContent("133;A")
	if shellStart.Type != OSCActionShell || shellStart.Shell.Marker != "promptStart" || shellStart.Shell.RawMarker != "A" || shellStart.Shell.HasExitCode {
		t.Fatalf("shell prompt start = %#v", shellStart)
	}
	shellEnd := ParseOSCContent("133;D;127")
	if shellEnd.Type != OSCActionShell || shellEnd.Shell.Marker != "commandEnd" || shellEnd.Shell.RawMarker != "D" || shellEnd.Shell.ExitCode != 127 || !shellEnd.Shell.HasExitCode {
		t.Fatalf("shell command end = %#v", shellEnd)
	}
	vsCodeShell := ParseOSCContent("633;C")
	if vsCodeShell.Type != OSCActionShell || vsCodeShell.Shell.Marker != "commandStart" || vsCodeShell.Shell.RawMarker != "C" {
		t.Fatalf("vscode shell command start = %#v", vsCodeShell)
	}
	vsCodeCommandLine := ParseOSCContent("633;E;go test ./...")
	if vsCodeCommandLine.Type != OSCActionShell || vsCodeCommandLine.Shell.Marker != "commandLine" || vsCodeCommandLine.Shell.RawMarker != "E" || vsCodeCommandLine.Shell.Value != "go test ./..." {
		t.Fatalf("vscode shell command line = %#v", vsCodeCommandLine)
	}
	vsCodeProperty := ParseOSCContent("633;P;Cwd=/tmp/ccgo;IsWindows=False;HasRichCommandDetection")
	if vsCodeProperty.Type != OSCActionShell || vsCodeProperty.Shell.Marker != "property" || vsCodeProperty.Shell.RawMarker != "P" || vsCodeProperty.Shell.Value != "Cwd=/tmp/ccgo;IsWindows=False;HasRichCommandDetection" {
		t.Fatalf("vscode shell property = %#v", vsCodeProperty)
	}
	if vsCodeProperty.Shell.Properties["Cwd"] != "/tmp/ccgo" || vsCodeProperty.Shell.Properties["IsWindows"] != "False" || vsCodeProperty.Shell.Properties["HasRichCommandDetection"] != "" {
		t.Fatalf("vscode shell properties = %#v", vsCodeProperty.Shell.Properties)
	}

	unknown := ParseOSCContent("999;noop")
	if unknown.Type != OSCActionUnknown || unknown.Sequence != OSCPrefix+"999;noop" {
		t.Fatalf("unknown = %#v", unknown)
	}
}

func TestParseOSCSequence(t *testing.T) {
	action, ok := ParseOSCSequence(TerminalTitleSequence("Claude"))
	if !ok || action.Type != OSCActionTitle || action.Title.Title != "Claude" {
		t.Fatalf("bel action = %#v ok=%v", action, ok)
	}

	stSequence := OSCSequenceWithStringTerminator(OSCSetTitleAndIcon, "Claude")
	stAction, ok := ParseOSCSequence(stSequence)
	if !ok || stAction.Type != OSCActionTitle || stAction.Title.Title != "Claude" {
		t.Fatalf("st action = %#v ok=%v", stAction, ok)
	}

	if action, ok := ParseOSCSequence(OSCPrefix + OSCSetTitleAndIcon + ";Claude"); ok || action.Type != "" {
		t.Fatalf("unterminated action = %#v ok=%v", action, ok)
	}
}

func TestParseOSCColor(t *testing.T) {
	hex, ok := ParseOSCColor("#5f87ff")
	if !ok || *hex != (RGBColor{R: 95, G: 135, B: 255}) {
		t.Fatalf("hex = %#v ok=%v", hex, ok)
	}

	short, ok := ParseOSCColor("rgb:f/0/8")
	if !ok || *short != (RGBColor{R: 255, G: 0, B: 136}) {
		t.Fatalf("short = %#v ok=%v", short, ok)
	}

	long, ok := ParseOSCColor("rgb:7fff/8000/ffff")
	if !ok || *long != (RGBColor{R: 127, G: 128, B: 255}) {
		t.Fatalf("long = %#v ok=%v", long, ok)
	}

	if invalid, ok := ParseOSCColor("rgb:fffff/0/0"); ok || invalid != nil {
		t.Fatalf("invalid = %#v ok=%v", invalid, ok)
	}
}

func TestTerminalHyperlinkSequence(t *testing.T) {
	url := "https://example.com/docs?x=1;y=2"
	link := TerminalHyperlinkSequence(url, nil)
	want := OSCPrefix + OSCHyperlink + ";id=f6ehdo;" + url + OSCTerminator
	if link != want {
		t.Fatalf("link = %q, want %q", link, want)
	}

	custom := TerminalHyperlinkSequence(url, map[string]string{"id": "custom", "rel": "noopener"})
	wantCustom := OSCPrefix + OSCHyperlink + ";id=custom:rel=noopener;" + url + OSCTerminator
	if custom != wantCustom {
		t.Fatalf("custom link = %q, want %q", custom, wantCustom)
	}

	end := EndTerminalHyperlinkSequence()
	wantEnd := OSCPrefix + OSCHyperlink + ";;" + OSCTerminator
	if end != wantEnd {
		t.Fatalf("end = %q, want %q", end, wantEnd)
	}

	visible := StripANSI(link + "docs" + end)
	if visible != "docs" {
		t.Fatalf("visible = %q", visible)
	}
}

func TestTerminalDirectorySequenceAndParser(t *testing.T) {
	sequence := TerminalDirectorySequence("file://localhost/tmp/workspace")
	want := OSCPrefix + OSCCurrentDirectory + ";file://localhost/tmp/workspace" + OSCTerminator
	if sequence != want {
		t.Fatalf("directory sequence = %q, want %q", sequence, want)
	}

	parser := NewTerminalParser()
	actions := parser.Feed("a" + sequence + "b")
	if len(actions) != 3 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[0].Type != TerminalActionText || actions[1].Type != TerminalActionDirectory || actions[2].Type != TerminalActionText {
		t.Fatalf("action types = %#v", actions)
	}
	if actions[1].OSC.Directory.URI != "file://localhost/tmp/workspace" || actions[1].OSC.Directory.Path != "/tmp/workspace" {
		t.Fatalf("directory action = %#v", actions[1])
	}
	if visible := StripANSI("a" + sequence + "b"); visible != "ab" {
		t.Fatalf("visible = %q", visible)
	}
}

func TestParseHyperlinkPayload(t *testing.T) {
	link := ParseHyperlinkPayload("id=f6ehdo:rel=noopener;https://example.com/docs?x=1;y=2")
	wantParams := map[string]string{"id": "f6ehdo", "rel": "noopener"}
	if link.End || link.URL != "https://example.com/docs?x=1;y=2" || !reflect.DeepEqual(link.Params, wantParams) {
		t.Fatalf("link = %#v", link)
	}

	end := ParseHyperlinkPayload(";")
	if !end.End || end.URL != "" || end.Params != nil {
		t.Fatalf("end = %#v", end)
	}
}

func TestTerminalClipboardSequence(t *testing.T) {
	seq := TerminalClipboardSequence("copy me")
	want := OSCPrefix + OSCClipboard + ";c;Y29weSBtZQ==" + OSCTerminator
	if seq != want {
		t.Fatalf("clipboard = %q, want %q", seq, want)
	}
}

func TestTerminalProgressSequence(t *testing.T) {
	running := TerminalProgressSequence(TerminalProgressRunning, 133)
	wantRunning := OSCPrefix + OSCITerm2 + ";" + ITerm2Progress + ";" + ITerm2ProgressSet + ";100" + OSCTerminator
	if running != wantRunning {
		t.Fatalf("running = %q, want %q", running, wantRunning)
	}

	failed := TerminalProgressSequence(TerminalProgressError, -5)
	wantFailed := OSCPrefix + OSCITerm2 + ";" + ITerm2Progress + ";" + ITerm2ProgressError + ";0" + OSCTerminator
	if failed != wantFailed {
		t.Fatalf("failed = %q, want %q", failed, wantFailed)
	}

	indeterminate := TerminalProgressSequence(TerminalProgressIndeterminate, 42)
	wantIndeterminate := OSCPrefix + OSCITerm2 + ";" + ITerm2Progress + ";" + ITerm2ProgressIndeterminate + ";" + OSCTerminator
	if indeterminate != wantIndeterminate {
		t.Fatalf("indeterminate = %q, want %q", indeterminate, wantIndeterminate)
	}

	clear := ClearTerminalProgressSequence()
	if completed := TerminalProgressSequence(TerminalProgressCompleted, 100); completed != clear {
		t.Fatalf("completed = %q, want %q", completed, clear)
	}
	wantClear := OSCPrefix + OSCITerm2 + ";" + ITerm2Progress + ";" + ITerm2ProgressClear + ";" + OSCTerminator
	if clear != wantClear {
		t.Fatalf("clear = %q, want %q", clear, wantClear)
	}

	if unknown := TerminalProgressSequence(TerminalProgressState("paused"), 50); unknown != "" {
		t.Fatalf("unknown = %q", unknown)
	}
}

func TestTerminalNotificationSequences(t *testing.T) {
	iterm := ITerm2NotificationSequence("Build complete", "Claude")
	wantITerm := OSCPrefix + OSCITerm2 + ";\n\nClaude:\nBuild complete" + OSCTerminator
	if iterm != wantITerm {
		t.Fatalf("iterm = %q, want %q", iterm, wantITerm)
	}

	kitty := KittyNotificationSequences("Build complete", "Claude", 7)
	wantKitty := []string{
		OSCPrefix + OSCKitty + ";i=7:d=0:p=title;Claude" + OSCTerminator,
		OSCPrefix + OSCKitty + ";i=7:p=body;Build complete" + OSCTerminator,
		OSCPrefix + OSCKitty + ";i=7:d=1:a=focus;" + OSCTerminator,
	}
	if !reflect.DeepEqual(kitty, wantKitty) {
		t.Fatalf("kitty = %#v, want %#v", kitty, wantKitty)
	}

	ghostty := GhosttyNotificationSequence("Build complete", "Claude")
	wantGhostty := OSCPrefix + OSCGhostty + ";notify;Claude;Build complete" + OSCTerminator
	if ghostty != wantGhostty {
		t.Fatalf("ghostty = %q, want %q", ghostty, wantGhostty)
	}

	if bell := TerminalBellSequence(); bell != OSCTerminator {
		t.Fatalf("bell = %q", bell)
	}
}

func TestTabStatusSequenceEscapesFields(t *testing.T) {
	status := "Working; path\\ok\x1b[31m hidden\x1b[0m"
	orange := RGBColor{R: 255, G: 149, B: 0}
	blue := RGBColor{R: 95, G: 135, B: 255}
	seq := TabStatusSequence(TabStatusFields{
		Indicator:   &orange,
		Status:      &status,
		StatusColor: &blue,
	})
	want := OSCPrefix + OSCTabStatus + ";indicator=#ff9500;status=Working\\; path\\\\ok hidden;status-color=#5f87ff" + OSCTerminator
	if seq != want {
		t.Fatalf("tab status = %q, want %q", seq, want)
	}

	clear := ClearTabStatusSequence()
	wantClear := OSCPrefix + OSCTabStatus + ";indicator=;status=;status-color=" + OSCTerminator
	if clear != wantClear {
		t.Fatalf("clear = %q, want %q", clear, wantClear)
	}

	reset := TabStatusSequence(TabStatusFields{ClearStatus: true})
	wantReset := OSCPrefix + OSCTabStatus + ";status=" + OSCTerminator
	if reset != wantReset {
		t.Fatalf("reset = %q, want %q", reset, wantReset)
	}
}

func TestParseTabStatusPayload(t *testing.T) {
	fields := ParseTabStatusPayload(`indicator=#ff9500;status=Working\; path\\ok;ignored=value;status-color=rgb:f/0/8`)
	if fields.Indicator == nil || *fields.Indicator != (RGBColor{R: 255, G: 149, B: 0}) || fields.ClearIndicator {
		t.Fatalf("indicator = %#v clear=%v", fields.Indicator, fields.ClearIndicator)
	}
	if fields.Status == nil || *fields.Status != `Working; path\ok` || fields.ClearStatus {
		t.Fatalf("status = %#v clear=%v", fields.Status, fields.ClearStatus)
	}
	if fields.StatusColor == nil || *fields.StatusColor != (RGBColor{R: 255, G: 0, B: 136}) || fields.ClearStatusColor {
		t.Fatalf("status color = %#v clear=%v", fields.StatusColor, fields.ClearStatusColor)
	}

	clear := ParseTabStatusPayload("indicator=;status;status-color=not-a-color")
	if clear.Indicator != nil || !clear.ClearIndicator {
		t.Fatalf("clear indicator = %#v clear=%v", clear.Indicator, clear.ClearIndicator)
	}
	if clear.Status != nil || !clear.ClearStatus {
		t.Fatalf("clear status = %#v clear=%v", clear.Status, clear.ClearStatus)
	}
	if clear.StatusColor != nil || !clear.ClearStatusColor {
		t.Fatalf("clear status color = %#v clear=%v", clear.StatusColor, clear.ClearStatusColor)
	}
}

func TestWrapForTerminalMultiplexer(t *testing.T) {
	seq := TerminalTitleSequence("Claude")
	tmux := WrapForTerminalMultiplexer(seq, "tmux")
	wantTmux := "\x1bPtmux;\x1b\x1b]0;Claude\x07\x1b\\"
	if tmux != wantTmux {
		t.Fatalf("tmux = %q, want %q", tmux, wantTmux)
	}

	screen := WrapForTerminalMultiplexer(seq, "screen")
	wantScreen := "\x1bP" + seq + OSCStringTerminator
	if screen != wantScreen {
		t.Fatalf("screen = %q, want %q", screen, wantScreen)
	}

	plain := WrapForTerminalMultiplexer(seq, "")
	if plain != seq {
		t.Fatalf("plain = %q, want %q", plain, seq)
	}
}

func TestRendererRendersMultilinePromptAndCursor(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.Text = "first\nsecond"
	prompt.Cursor = len([]rune("first\nsec"))
	output := RenderOnce(24, 6, Frame{
		Messages:   []Message{{Role: RoleAssistant, Text: "hello"}},
		Status:     "ready",
		Prompt:     prompt,
		ShowCursor: true,
	})
	visible := StripANSI(output)
	if !strings.Contains(visible, "> first") || !strings.Contains(visible, "  second") {
		t.Fatalf("visible prompt = %q", visible)
	}
	if !strings.Contains(output, "\x1b[6;6H") {
		t.Fatalf("cursor output = %q", output)
	}
}

func TestRendererWrapsLongPromptAndCursor(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.Text = "abcdefghijk"
	prompt.Cursor = len([]rune("abcdefghij"))
	output := RenderOnce(10, 5, Frame{
		Status:     "ready",
		Prompt:     prompt,
		ShowCursor: true,
	})
	visible := StripANSI(output)
	if !strings.Contains(visible, "> abcdefgh") || !strings.Contains(visible, "  ijk") {
		t.Fatalf("visible prompt = %q", visible)
	}
	if !strings.Contains(output, "\x1b[5;5H") {
		t.Fatalf("cursor output = %q", output)
	}
}

func TestRendererWrapsWidePromptAndCursorByVisibleWidth(t *testing.T) {
	prompt := NewPromptState(nil)
	prompt.Text = "界界界a"
	prompt.Cursor = len([]rune("界界"))
	output := RenderOnce(8, 4, Frame{
		Status:     "ready",
		Prompt:     prompt,
		ShowCursor: true,
	})
	visible := StripANSI(output)
	if !strings.Contains(visible, "> 界界界") || !strings.Contains(visible, "  a") {
		t.Fatalf("visible prompt = %q", visible)
	}
	if !strings.Contains(output, "\x1b[3;7H") {
		t.Fatalf("cursor output = %q", output)
	}
}

func TestRendererPositionsWideReverseSearchCursorByVisibleWidth(t *testing.T) {
	state := ReverseSearchState{
		Active:  true,
		Query:   "界界a",
		Cursor:  len([]rune("界界")),
		Results: []string{"界界abc"},
	}
	output := RenderOnce(40, 4, Frame{
		Status:        "ready",
		ReverseSearch: &state,
		ShowCursor:    true,
	})
	visible := StripANSI(output)
	if !strings.Contains(visible, "(reverse-i-search) `界界a': 界界abc") {
		t.Fatalf("visible reverse search = %q", visible)
	}
	if !strings.Contains(output, "\x1b[4;25H") {
		t.Fatalf("cursor output = %q", output)
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

func TestSnapshotCorpusComparesANSIOnlyBaselines(t *testing.T) {
	snapshot := CaptureANSISnapshot("ansi-only", 32, 6, Frame{
		Messages: []Message{{Role: RoleAssistant, Text: "ready"}},
		Prompt:   NewPromptState(nil),
	})
	corpus := SnapshotCorpus{Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(corpus.Dir, "ansi-only.ansi"), []byte(snapshot.Output), 0o644); err != nil {
		t.Fatal(err)
	}
	comparison, err := corpus.Compare(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.Match || comparison.Missing {
		t.Fatalf("comparison = %#v", comparison)
	}
	if err := os.WriteFile(filepath.Join(corpus.Dir, "stale-ansi.ansi"), []byte(snapshot.Output), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := corpus.CompareAllStrict([]ANSISnapshot{snapshot})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed() || len(report.Unexpected) != 1 || report.Unexpected[0] != "stale-ansi" {
		t.Fatalf("strict report = %#v", report)
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

	report, err := corpus.CompareAllStrict(result.Snapshots)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed() || len(report.Comparisons) != 2 {
		t.Fatalf("strict report = %#v", report)
	}
	if err := os.WriteFile(filepath.Join(corpus.Dir, "stale.txt"), []byte("old baseline"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = corpus.CompareAllStrict(result.Snapshots)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed() || len(report.Unexpected) != 1 || report.Unexpected[0] != "stale" {
		t.Fatalf("strict report with stale baseline = %#v", report)
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

func TestRunInteractionScriptAcceptsNamedKeys(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	text := "alpha beta gamma"
	cursorEnd := len([]rune(text))
	cursorGamma := len([]rune("alpha beta "))
	cursorBeta := len([]rune("alpha "))
	result, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{Keys: []string{"a", "l", "p", "h", "a", " ", "b", "e", "t", "a", " ", "g", "a", "m", "m", "a"}, ExpectPrompt: &PromptExpectation{Text: text, Cursor: &cursorEnd}},
		{Key: "c-left", ExpectPrompt: &PromptExpectation{Text: text, Cursor: &cursorGamma}},
		{Key: "m-left", ExpectPrompt: &PromptExpectation{Text: text, Cursor: &cursorBeta}},
		{Key: "a-right", ExpectPrompt: &PromptExpectation{Text: text, Cursor: &cursorGamma}},
		{Key: "c-right", ExpectPrompt: &PromptExpectation{Text: text, Cursor: &cursorEnd}},
		{Key: "enter", ExpectEvent: &ScreenEvent{Type: ScreenEventPromptSubmitted, Value: text}, ExpectPrompt: &PromptExpectation{Empty: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != text {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsStructuredMouse(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"dialog":{"title":"Permission","body":"Allow?","actions":["Allow","Deny"],"id":"perm_1","kind":"permission"}},
		{"mouse_event":{"button_code":0,"column":13,"row":5},"expect_event":{"type":"dialog_action","value":"Deny","dialog_id":"perm_1","dialog_kind":"permission"}},
		{"dialog":{"title":"Permission","body":"Allow?","actions":["Allow","Deny"],"id":"perm_2","kind":"permission"}},
		{"mouse":{"mouseButton":4,"x":13,"y":5,"released":false},"expect_event":{"type":"dialog_action","value":"Deny","dialogId":"perm_2","dialogKind":"permission"}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != ScreenEventDialogAction || result.Events[0].Value != "Deny" || result.Events[0].DialogID != "perm_1" ||
		result.Events[1].Type != ScreenEventDialogAction || result.Events[1].Value != "Deny" || result.Events[1].DialogID != "perm_2" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestScriptMouseAcceptsCoordinateAliases(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "page camel", raw: `{"button":0,"pageX":13,"pageY":5}`},
		{name: "offset snake", raw: `{"button":0,"offset_x":13,"offset_y":5}`},
		{name: "viewport camel", raw: `{"button":0,"viewportX":13,"viewportY":5}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mouse ScriptMouse
			if err := json.Unmarshal([]byte(tc.raw), &mouse); err != nil {
				t.Fatal(err)
			}
			if mouse.X != 13 || mouse.Y != 5 {
				t.Fatalf("mouse = %#v", mouse)
			}
		})
	}
}

func TestScriptBooleanAliasesAcceptNonStrictValues(t *testing.T) {
	var mouse ScriptMouse
	if err := json.Unmarshal([]byte(`{"button":0,"x":13,"y":5,"mouseUp":"yes"}`), &mouse); err != nil {
		t.Fatal(err)
	}
	if !mouse.Release {
		t.Fatalf("mouse release = %#v", mouse)
	}

	var release ScriptMouse
	release.Release = true
	if err := json.Unmarshal([]byte(`{"button":0,"x":13,"y":5,"release":0}`), &release); err != nil {
		t.Fatal(err)
	}
	if release.Release {
		t.Fatalf("direct release bool was not normalized: %#v", release)
	}

	var dialog DialogExpectation
	if err := json.Unmarshal([]byte(`{"visible":"1","dialogId":"perm_1"}`), &dialog); err != nil {
		t.Fatal(err)
	}
	if !dialog.Active || dialog.ID != "perm_1" {
		t.Fatalf("dialog = %#v", dialog)
	}

	var result DialogResultExpectation
	if err := json.Unmarshal([]byte(`{"exists":"on","isStale":"0"}`), &result); err != nil {
		t.Fatal(err)
	}
	if result.Found == nil || !*result.Found || result.Stale == nil || *result.Stale {
		t.Fatalf("dialog result = %#v", result)
	}

	var prompt PromptExpectation
	if err := json.Unmarshal([]byte(`{"empty":"true"}`), &prompt); err != nil {
		t.Fatal(err)
	}
	if !prompt.Empty {
		t.Fatalf("prompt = %#v", prompt)
	}

	var vim VimExpectation
	if err := json.Unmarshal([]byte(`{"vimEnabled":"y","registerLinewise":"0"}`), &vim); err != nil {
		t.Fatal(err)
	}
	if vim.Enabled == nil || !*vim.Enabled || vim.RegisterLinewise == nil || *vim.RegisterLinewise {
		t.Fatalf("vim = %#v", vim)
	}

	var search ReverseSearchExpectation
	if err := json.Unmarshal([]byte(`{"visible":"yes","empty":1}`), &search); err != nil {
		t.Fatal(err)
	}
	if !search.Active || !search.NoResults {
		t.Fatalf("reverse search = %#v", search)
	}

	var step ScriptStep
	if err := json.Unmarshal([]byte(`{"focus":"on","expectNoEvent":"1","expectFocused":"true","openTasksDialog":"yes","cancelAllTasks":1}`), &step); err != nil {
		t.Fatal(err)
	}
	if len(step.Keys) != 1 || step.Keys[0] != "focus-in" || !step.ExpectNoEvent || step.ExpectFocused == nil || !*step.ExpectFocused || !step.OpenTasksDialog || !step.CancelAllTasks {
		t.Fatalf("step = %#v", step)
	}
}

func TestRunInteractionScriptAcceptsMouseFieldAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"dialog":{"title":"Permission","body":"Allow?","actions":["Allow","Deny"],"id":"perm_1","kind":"permission"}},
		{"mouse_event":{"buttonMask":0,"clientX":13,"clientY":5,"isRelease":false},"expectEvent":{"event":"dialog_action","payload":"Deny","dialogId":"perm_1","dialogKind":"permission"}},
		{"dialog":{"title":"Permission","body":"Allow?","actions":["Allow","Deny"],"id":"perm_2","kind":"permission"}},
		{"mouse":{"btn":0,"mouse_x":13,"mouse_y":5,"mouseUp":true},"expectNoEvent":true,"expectDialog":{"visible":true,"dialogId":"perm_2"}},
		{"mouse":{"code":0,"screenX":13,"screenY":5,"releaseEvent":false},"expectEvent":{"type":"dialog_action","value":"Deny","dialog_id":"perm_2","dialog_kind":"permission"}},
		{"dialog":{"title":"Permission","body":"Allow?","actions":["Allow","Deny"],"id":"perm_3","kind":"permission"}},
		{"mouse_event":{"button":0,"pageX":13,"pageY":5,"released":false},"expectEvent":{"type":"dialog_action","value":"Deny","dialog_id":"perm_3","dialog_kind":"permission"}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 3 ||
		result.Events[0].Type != ScreenEventDialogAction || result.Events[0].Value != "Deny" || result.Events[0].DialogID != "perm_1" ||
		result.Events[1].Type != ScreenEventDialogAction || result.Events[1].Value != "Deny" || result.Events[1].DialogID != "perm_2" ||
		result.Events[2].Type != ScreenEventDialogAction || result.Events[2].Value != "Deny" || result.Events[2].DialogID != "perm_3" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptTypesTextField(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{Text: "run task", Key: "enter", ExpectEvent: &ScreenEvent{Type: ScreenEventPromptSubmitted, Value: "run task"}, ExpectPrompt: &PromptExpectation{Empty: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "run task" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsPasteAndImageFields(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	one := 1
	two := 2
	nextOne := 1
	nextTwo := 2
	nextThree := 3
	zero := 0
	result, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{Paste: "alpha\nbeta", ExpectPrompt: &PromptExpectation{
			Text:               "[Pasted text #1 +1 lines]",
			Expanded:           "alpha\nbeta",
			PastedContentCount: &one,
			NextPastedID:       &nextTwo,
			PastedContents: []PastedContentExpectation{{
				ID:      1,
				Type:    session.PastedContentText,
				Content: "alpha\nbeta",
			}},
		}},
		{Image: &ScriptImage{Filename: "chart.png", MediaType: "image/png", Content: "AAAA"}, ExpectPrompt: &PromptExpectation{
			Text:               "[Pasted text #1 +1 lines][Image #2]",
			Expanded:           "alpha\nbeta[Image #2]",
			PastedContentCount: &two,
			NextPastedID:       &nextThree,
			PastedContents: []PastedContentExpectation{{
				ID:        2,
				Type:      session.PastedContentImage,
				Content:   "AAAA",
				MediaType: "image/png",
				Filename:  "chart.png",
			}},
		}},
		{Key: "enter", ExpectEvent: &ScreenEvent{Type: ScreenEventPromptSubmitted, Value: "alpha\nbeta[Image #2]"}, ExpectPrompt: &PromptExpectation{
			Empty:              true,
			PastedContentCount: &zero,
			NextPastedID:       &nextOne,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "alpha\nbeta[Image #2]" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsImageFieldAliases(t *testing.T) {
	var steps []ScriptStep
	if err := json.Unmarshal([]byte(`[
		{
			"image": {"fileName": "chart.png", "mimeType": "image/png", "data": "AAAA", "sourcePath": "/tmp/chart.png", "dimensions": {"originalWidth": 4000, "originalHeight": 2000, "displayWidth": 1000, "displayHeight": 500}},
			"expect_prompt": {
				"text": "[Image #1]",
				"pastedContentCount": 1,
				"nextPastedID": 2,
				"pastedContents": {"id": 1, "type": "image", "mediaType": "image/png", "filename": "chart.png", "content": "AAAA", "sourcePath": "/tmp/chart.png", "dimensions": {"originalWidth": 4000, "originalHeight": 2000, "displayWidth": 1000, "displayHeight": 500}}
			}
		},
		{
			"image": {"name": "diagram.webp", "mime_type": "image/webp", "base64": "BBBB"},
			"expect_prompt": {
				"text": "[Image #1] [Image #2]",
				"pastedContentCount": 2,
				"nextPastedID": 3,
				"pastedContents": {"id": 2, "type": "image", "media_type": "image/webp", "filename": "diagram.webp", "content": "BBBB"}
			}
		}
	]`), &steps); err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	if _, err := RunInteractionScriptChecked(&screen, steps); err != nil {
		t.Fatal(err)
	}
}

func TestRunInteractionScriptAcceptsPastedContentAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"pasteText": "alpha\nbeta",
			"expectPrompt": {
				"pastedContents": {
					"pastedId": 1,
					"kind": "text",
					"value": "alpha\nbeta",
					"contains": "beta"
				}
			}
		},
		{
			"image": {"fileName": "chart.png", "mimeType": "image/png", "data": "AAAA"},
			"expectPrompt": {
				"pastedContents": {
					"pastedContentId": 2,
					"pastedType": "image",
					"contentType": "image/png",
					"fileName": "chart.png",
					"data": "AAAA"
				}
			}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	if _, err := RunInteractionScriptChecked(&screen, steps); err != nil {
		t.Fatal(err)
	}
}

func TestRunInteractionScriptAcceptsJSONFieldAliases(t *testing.T) {
	var steps []ScriptStep
	if err := json.Unmarshal([]byte(`[
		{
			"resize_width": 40,
			"resize_height": 8,
			"paste": "alpha\nbeta",
			"image": {"filename": "chart.png", "media_type": "image/png", "content": "AAAA"},
			"status_line": "json ready",
			"snapshot_name": "json-aliases",
			"expect_prompt": {
				"text": "[Pasted text #1 +1 lines][Image #2]",
				"expanded": "alpha\nbeta[Image #2]",
				"pasted_content_count": 2,
				"nextPastedID": 3,
				"pasted_contents": [
					{"id": 1, "type": "text", "content_contains": ["alpha", "beta"]},
					{"id": 2, "type": "image", "media_type": "image/png", "filename": "chart.png"}
				]
			},
			"expect_screen": {"width": 40, "height": 8},
			"expect_status_contains": ["json ready"],
			"expect_snapshot_contains": ["[Image #2]"],
			"expect_snapshot_not_contains": ["not present"]
		},
		{
			"key": "enter",
			"expect_event": {"type": "prompt_submitted", "value": "alpha\nbeta[Image #2]"},
			"expect_prompt": {"empty": true, "pastedContentCount": 0, "next_pasted_id": 1}
		}
	]`), &steps); err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(20, 4, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshots) != 1 || result.Snapshots[0].Name != "json-aliases" {
		t.Fatalf("snapshots = %#v", result.Snapshots)
	}
	if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "alpha\nbeta[Image #2]" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsPromptFieldAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"input": "abc",
			"expectPrompt": {"value": "abc", "cursorIndex": 3, "isEmpty": false}
		},
		{
			"paste_text": "one\ntwo",
			"expectPrompt": {
				"input": "abc[Pasted text #1 +1 lines]",
				"expandedText": "abcone\ntwo",
				"pastedContentCount": 1
			}
		},
		{
			"key": "enter",
			"expectPrompt": {"blank": true}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Value != "abcone\ntwo" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsStringContainsAliases(t *testing.T) {
	var steps []ScriptStep
	if err := json.Unmarshal([]byte(`[
		{
			"resizeWidth": 40,
			"resizeHeight": 8,
			"message": {"role": "system", "text": "alpha ready"},
			"pasteText": "alpha\nbeta",
			"snapshotName": "string-contains",
			"expect_status_contains": "ready",
			"expectStatusNotContains": "blocked",
			"expect_snapshot_contains": "[Pasted text #1 +1 lines]",
			"expectSnapshotNotContains": "missing marker",
			"expect_prompt": {
				"text": "[Pasted text #1 +1 lines]",
				"expanded": "alpha\nbeta",
				"pastedContents": {"id": 1, "type": "text", "contentContains": "beta"}
			},
			"expectViewport": {
				"visibleContains": "system: alpha ready",
				"visibleNotContains": "not visible"
			}
		}
	]`), &steps); err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(20, 4, nil)
	screen.Status = "ready"
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshots) != 1 || result.Snapshots[0].Name != "string-contains" {
		t.Fatalf("snapshots = %#v", result.Snapshots)
	}
}

func TestRunInteractionScriptAcceptsMessageContentAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"message": {"type": "assistant", "content": "content text"}, "snapshotName": "content", "expectSnapshotContains": "assistant: content text"},
		{"message": {"speaker": "system", "body": "body text"}, "snapshotName": "body", "expectSnapshotContains": "system: body text"},
		{"message": {"type": "user", "message": "message text"}, "snapshotName": "message", "expectSnapshotContains": "user: message text"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshots) != 3 {
		t.Fatalf("snapshots = %#v", result.Snapshots)
	}
}

func TestRunInteractionScriptAcceptsMessageListAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"messages": {"type": "system", "content": "single object"},
			"snapshot": "single",
			"expectSnapshotContains": "system: single object"
		},
		{
			"appendMessages": [
				{"speaker": "assistant", "body": "batched assistant"},
				{"type": "user", "message": "batched user"}
			],
			"snapshot": "batch",
			"expectSnapshotContains": ["assistant: batched assistant", "user: batched user"]
		},
		{
			"transcript_messages": [
				{"role": "tool", "text": "tool output"},
				{"role": "user", "text": "image history", "imagePasteIds": [9]},
				{"role": "user", "text": "single image history", "imagePasteId": 10},
				{
					"role": "user",
					"content": [
						{"type": "text", "text": "block user"},
						{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "AAAA"}}
					],
					"imagePasteIds": [11],
					"pasted_contents": {"12": {"id": 12, "type": "text", "content": "memo"}}
				},
				{
					"role": "user",
					"text": "attached [Pasted text #13] [Image #14]",
					"attachments": [
						{"pastedContentId": "13", "kind": "text", "value": "attached memo"},
						{"attachmentID": "14", "pastedType": "image", "data": "BBBB", "contentType": "image/png", "fileName": "attached.png"}
					],
					"pastedImageId": 14
				}
			],
			"snapshot": "transcript",
			"expectSnapshotContains": "tool: tool output",
			"expectPrompt": {"nextPastedID": 15}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(60, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshots) != 3 {
		t.Fatalf("snapshots = %#v", result.Snapshots)
	}
	if len(screen.Messages) != 8 {
		t.Fatalf("messages = %#v", screen.Messages)
	}
	if got := screen.Messages[4].ImagePasteIDs; len(got) != 1 || got[0] != 9 {
		t.Fatalf("image paste ids = %#v", got)
	}
	if got := screen.Messages[5].ImagePasteIDs; len(got) != 1 || got[0] != 10 {
		t.Fatalf("single image paste id = %#v", got)
	}
	if got := screen.Messages[6]; len(got.ContentBlocks) != 2 || got.ImagePasteIDs[0] != 11 || got.PastedContents[12].Content != "memo" {
		t.Fatalf("content block message = %#v", got)
	}
	if got := screen.Messages[7]; len(got.ImagePasteIDs) != 1 || got.ImagePasteIDs[0] != 14 || got.PastedContents[13].Content != "attached memo" || got.PastedContents[14].Filename != "attached.png" {
		t.Fatalf("attachment message = %#v", got)
	}
}

func TestRunInteractionScriptAcceptsInputFieldAliases(t *testing.T) {
	var steps []ScriptStep
	if err := json.Unmarshal([]byte(`[
		{"keys":"a","expect_prompt":{"text":"a"}},
		{"keys_text":"bc","expect_prompt":{"text":"abc"}},
		{"paste_text":"clip\nboard","expect_prompt":{"text":"abc[Pasted text #1 +1 lines]","expanded":"abcclip\nboard"}},
		{"inputText":" done","raw_key":"enter","expect_event":{"type":"prompt_submitted","value":"abcclip\nboard done"},"expect_prompt":{"empty":true}}
	]`), &steps); err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "abcclip\nboard done" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsKeysStringSequences(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"keys":"ship it","expect_prompt":{"text":"ship it","cursor":7}},
		{"keys":[" now","enter"],"expect_event":{"type":"prompt_submitted","value":"ship it now"},"expect_prompt":{"empty":true}},
		{"keys":"ctrl-x ctrl-k","expect_event":{"type":"kill_agents"}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "ship it now" ||
		result.Events[1].Type != ScreenEventKillAgents {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsPressFieldAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"press":"a","expectPrompt":{"text":"a"}},
		{"keyPress":"b","expectPrompt":{"text":"ab"}},
		{"shortcutKey":"enter","expectEvent":{"type":"prompt_submitted","value":"ab"},"expectPrompt":{"empty":true}},
		{"keyPresses":"ctrl-x ctrl-k","expectEvent":{"type":"kill_agents"}},
		{"shortcuts":["o","k","enter"],"expectEvent":{"type":"prompt_submitted","value":"ok"},"expectPrompt":{"empty":true}},
		{"presses":["y","enter"],"expectEvent":{"type":"prompt_submitted","value":"y"},"expectPrompt":{"empty":true}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 4 ||
		result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "ab" ||
		result.Events[1].Type != ScreenEventKillAgents ||
		result.Events[2].Type != ScreenEventPromptSubmitted || result.Events[2].Value != "ok" ||
		result.Events[3].Type != ScreenEventPromptSubmitted || result.Events[3].Value != "y" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsActionDiscriminatorAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"action":"type","value":"hi","expectPrompt":{"text":"hi"}},
		{"type":"press","payload":"enter","expectEvent":{"type":"prompt_submitted","value":"hi"},"expectPrompt":{"empty":true}},
		{"kind":"keys","data":["ctrl-x","ctrl-k"],"expectEvent":{"type":"kill_agents"}},
		{"name":"paste","payload":"clip","expectPrompt":{"text":"[Pasted text #1]","expandedText":"clip","pastedContentCount":1}},
		{"operation":"status","value":"busy","expectStatusContains":"busy"},
		{"action":"resize","value":[50,9],"expectScreen":{"columns":50,"rows":9}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "hi" ||
		result.Events[1].Type != ScreenEventKillAgents {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsCompactActionDiscriminatorAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"action":"typeText","value":"hi","expectPrompt":{"text":"hi"}},
		{"type":"keyPress","payload":"enter","expectEvent":{"type":"prompt_submitted","value":"hi"},"expectPrompt":{"empty":true}},
		{"kind":"keySequence","data":["ctrl-x","ctrl-k"],"expectEvent":{"type":"kill_agents"}},
		{"name":"pasteText","payload":"clip","expectPrompt":{"text":"[Pasted text #1]","expandedText":"clip","pastedContentCount":1}},
		{"operation":"setStatus","value":"busy","expectStatusContains":"busy"},
		{"action":"terminalSize","value":{"columns":50,"rows":9},"expectScreen":{"columns":50,"rows":9}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "hi" ||
		result.Events[1].Type != ScreenEventKillAgents {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsCompactEventActionAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"action":"focusOut","expectEvent":{"type":"focus_out"},"expectFocused":false},
		{"type":"focusIn","expectEvent":{"type":"focus_in"},"expectFocused":true},
		{"name":"pasteImage","payload":{"fileName":"chart.png","mimeType":"image/png","data":"AAAA"},"expectPrompt":{"text":"[Image #1]","pastedContentCount":1}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != ScreenEventFocusOut ||
		result.Events[1].Type != ScreenEventFocusIn {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestParseInteractionScriptAcceptsCompactMouseActionAlias(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"action":"mouseEvent","payload":{"buttonMask":0,"clientX":13,"clientY":5,"isRelease":false}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Mouse == nil || steps[0].Mouse.Button != 0 || steps[0].Mouse.X != 13 || steps[0].Mouse.Y != 5 || steps[0].Mouse.Release {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestRunDialogRuntimeScriptAcceptsRuntimeActionDiscriminatorAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"action":"requestPermission",
			"payload":{"permissionId":"perm_action","tool":"Bash","actions":"Allow"},
			"expectDialog":{"active":true,"id":"perm_action","kind":"permission"},
			"expectStatusContains":"permissions: 1"
		},
		{
			"action":"keyPress",
			"payload":"enter",
			"expectEvent":{"type":"dialog_action","value":"Allow","dialogId":"perm_action","dialogKind":"permission"},
			"expectDialogResult":{"id":"perm_action","status":"allowed","found":true}
		},
		{
			"type":"taskStatus",
			"payload":{"taskId":"task_action","name":"Build","status":"running","statusText":"go test","progressPercent":25},
			"expectTasks":{"count":1,"stateCounts":{"running":1},"contains":{"taskId":"task_action","status":"running","statusText":"go test","progressPercent":25}}
		},
		{
			"kind":"showTasks",
			"expectDialog":{"active":true,"id":"tasks","kind":"task"},
			"expectSnapshotContains":"Build [running] 25% - go test"
		},
		{
			"name":"cancelTasks",
			"payload":{"reason":"stopped"},
			"expectTasks":{"count":1,"stateCounts":{"cancelled":1},"contains":{"taskId":"task_action","status":"cancelled","statusText":"stopped","progressPercent":25}},
			"expectStatusContains":"cancelled: 1"
		},
		{
			"operation":"removeTask",
			"payload":{"taskId":"task_action"},
			"expectTasks":{"count":0}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(52, 9, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].ID != "perm_action" || result.DialogResults[0].Status != DialogResultAllowed {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
	if len(runtime.Tasks) != 0 {
		t.Fatalf("tasks = %#v", runtime.Tasks)
	}
}

func TestRunInteractionScriptAcceptsDialogActionDiscriminatorAlias(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"action":"showDialog",
			"payload":{"dialogId":"custom_action","dialogKind":"permission","heading":"Custom","content":"Pick one.","options":"Proceed"},
			"expectDialog":{"visible":true,"dialogId":"custom_action","dialogKind":"permission","heading":"Custom","content":"Pick one.","actions":"Proceed"}
		},
		{
			"action":"keyPress",
			"payload":"enter",
			"expectEvent":{"type":"dialog_action","value":"Proceed","dialogId":"custom_action","dialogKind":"permission"}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].DialogID != "custom_action" || result.Events[0].Value != "Proceed" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAppliesStepKeybindings(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"keybindings":{"keys":"ctrl-r","command":"submitPrompt"}},
		{"text":"send me","key":"ctrl-r","expect_event":{"type":"prompt_submitted","value":"send me"},"expect_prompt":{"empty":true}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "send me" {
		t.Fatalf("events = %#v", result.Events)
	}

	_, err = RunInteractionScriptChecked(&screen, []ScriptStep{{Keybindings: []BindingSpec{{Key: "wat", Action: ActionSubmitPrompt}}}})
	if err == nil || !strings.Contains(err.Error(), "script step 0 keybindings") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunInteractionScriptAcceptsKeybindingCollectionAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"keymap":{"ctrl-r":"submitPrompt"}},
		{"text":"first","key":"ctrl-r","expectEvent":{"type":"prompt_submitted","value":"first"},"expectPrompt":{"empty":true}},
		{"keyboardShortcuts":{"bindings":[{"shortcut":"ctrl-w","commandName":"submitPrompt"}]}},
		{"text":"second","key":"ctrl-w","expectEvent":{"type":"prompt_submitted","value":"second"},"expectPrompt":{"empty":true}},
		{"hotkeys":{"ctrl-x ctrl-k":false}},
		{"keys":"ctrl-x ctrl-k","expectNoEvent":true}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 ||
		result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "first" ||
		result.Events[1].Type != ScreenEventPromptSubmitted || result.Events[1].Value != "second" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsTerminalControlKeyAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"text":"az","key":"ctrl-h","expect_prompt":{"text":"a"}},
		{"text":"b","key":"control-m","expect_event":{"type":"prompt_submitted","value":"ab"},"expect_prompt":{"empty":true}},
		{"text":"line","key":"control-j","expect_event":{"type":"prompt_submitted","value":"line"},"expect_prompt":{"empty":true}},
		{"text":"cancel me","key":"ctrl-[","expect_event":{"type":"cancelled"},"expect_prompt":{"text":"cancel me"}},
		{"text":"x","key":"ctrl-?","expect_prompt":{"text":"cancel me"}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 3 ||
		result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "ab" ||
		result.Events[1].Type != ScreenEventPromptSubmitted || result.Events[1].Value != "line" ||
		result.Events[2].Type != ScreenEventCancelled {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunDialogRuntimeScriptAcceptsJSONFieldAliases(t *testing.T) {
	var steps []ScriptStep
	if err := json.Unmarshal([]byte(`[
		{
			"request_permission": {"id": "perm_1", "tool_name": "Bash", "path": "/tmp/a", "description": "Run command", "actions": ["Allow", "Deny"]},
			"expect_dialog": {"active": true, "id": "perm_1", "kind": "permission", "title": "Permission"},
			"expect_status_contains": ["permissions: 1"]
		},
		{
			"key": "enter",
			"expect_event": {"type": "dialog_action", "value": "Allow", "dialog_id": "perm_1", "dialog_kind": "permission"},
			"expect_dialog_result": {"id": "perm_1", "kind": "permission", "action": "Allow", "status": "allowed", "found": true},
			"expect_status_not_contains": ["permissions: 1"]
		},
		{
			"upsert_task": {"id": "task_1", "title": "Build", "state": "running", "detail": "go test", "progress": 40},
			"open_tasks_dialog": true,
			"snapshot_name": "tasks",
			"expect_dialog": {"active": true, "id": "tasks", "kind": "task"},
			"expect_tasks": {"count": 1, "state_counts": {"running": 1}, "contains": {"id": "task_1", "title": "Build", "state": "running", "detail": "go test", "progress": 40}},
			"expect_snapshot_contains": ["Build [running] 40% - go test"]
		},
		{
			"cancel_all_tasks": true,
			"cancel_tasks_detail": "stopped",
			"snapshot_name": "tasks-cancelled",
			"expect_tasks": {"count": 1, "state_counts": {"cancelled": 1}, "contains": [{"id": "task_1", "state": "cancelled", "detail": "stopped", "progress": 40}]},
			"expect_status_contains": ["cancelled: 1"],
			"expect_snapshot_contains": ["Build [cancelled] 40% - stopped"]
		}
	]`), &steps); err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(50, 9, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].Status != DialogResultAllowed {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
	if len(result.Snapshots) != 2 {
		t.Fatalf("snapshots = %#v", result.Snapshots)
	}
}

func TestRunDialogRuntimeScriptAcceptsPermissionRequestAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"request_permission": {
				"permissionId": "perm_alias",
				"tool": "Write",
				"filePath": "/tmp/a.txt",
				"prompt": "Need write access.",
				"actions": "Approve"
			},
			"expectDialog": {"active": true, "id": "perm_alias", "kind": "permission"},
			"expectSnapshotContains": ["Tool: Write", "Path: /tmp/a.txt", "Need write access.", "Approve"]
		},
		{
			"key": "enter",
			"expectEvent": {"type": "dialog_action", "value": "Approve", "dialogId": "perm_alias", "dialogKind": "permission"},
			"expectDialogResult": {"id": "perm_alias", "kind": "permission", "action": "Approve", "status": "allowed", "found": true}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(52, 9, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].ID != "perm_alias" || result.DialogResults[0].Status != DialogResultAllowed {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
	if len(runtime.Permissions) != 0 || runtime.Active != nil {
		t.Fatalf("runtime = %#v", runtime)
	}
}

func TestRunDialogRuntimeScriptAcceptsAdjacentPermissionPayloadAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"action": "requestPermission",
			"payload": {
				"requestID": 9001,
				"operation": "NotebookEdit",
				"resourcePath": "/tmp/notebook.ipynb",
				"body": "Need notebook edit.",
				"allowedActions": "Approve"
			},
			"expectDialog": {
				"active": true,
				"permissionID": 9001,
				"dialogKind": "permission",
				"actions": "Approve",
				"bodyContains": ["Tool: NotebookEdit", "Path: /tmp/notebook.ipynb", "Need notebook edit."]
			},
			"expectStatusContains": ["permissions: 1"]
		},
		{
			"key": "enter",
			"expectEvent": {"eventType": "dialog_action", "payload": "Approve", "permissionID": 9001, "dialogKind": "permission"},
			"expectDialogResult": {"requestID": 9001, "state": "approved", "found": true},
			"expectStatusNotContains": ["permissions: 1"]
		},
		{
			"action": "request-permission",
			"operationID": "cancel_me",
			"commandName": "Bash",
			"target": "/tmp/project",
			"reasonText": "Need shell.",
			"buttons": ["Allow", "Abort"],
			"expectDialog": {"active": true, "toolUseID": "cancel_me", "bodyContains": ["Tool: Bash", "Path: /tmp/project", "Need shell."]}
		},
		{
			"action": "cancel-permission",
			"toolUseID": "cancel_me",
			"expectDialogResult": {"operationID": "cancel_me", "state": "cancelled", "exists": true},
			"expectDialog": {"visible": false}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(58, 10, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 2 || result.DialogResults[0].ID != "9001" || result.DialogResults[0].Status != DialogResultAllowed || result.DialogResults[1].ID != "cancel_me" || result.DialogResults[1].Status != DialogResultCancelled {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
}

func TestRunDialogRuntimeScriptNormalizesPermissionActionAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"requestPermission": {
				"id": "perm_alias",
				"toolName": "Write",
				"actions": ["Approve", "Reject"]
			},
			"expectDialog": {"active": true, "id": "perm_alias", "kind": "permission", "focusedIndex": 0}
		},
		{
			"key": "tab",
			"expectDialog": {"active": true, "id": "perm_alias", "focusedIndex": 1}
		},
		{
			"key": "enter",
			"expectEvent": {"type": "dialog_action", "value": "Reject", "dialogId": "perm_alias", "dialogKind": "permission"},
			"expectDialogResult": {"id": "perm_alias", "kind": "permission", "action": "Reject", "status": "rejected", "found": true}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(52, 9, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].ID != "perm_alias" || result.DialogResults[0].Status != DialogResultDenied {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
}

func TestRunDialogRuntimeScriptChecksDialogDetails(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"request_permission": {"id": "perm_1", "tool_name": "Bash", "path": "/tmp/a", "description": "Run command", "actions": ["Allow", "Deny"]},
			"expect_dialog": {
				"active": true,
				"id": "perm_1",
				"kind": "permission",
				"title": "Permission",
				"body_contains": ["Tool: Bash", "Path: /tmp/a", "Run command"],
				"bodyNotContains": "No active tasks",
				"actions": ["Allow", "Deny"],
				"actionCount": 2,
				"actionContains": "Allow",
				"action_not_contains": "Allow Session",
				"focusedIndex": 0
			}
		},
		{
			"key": "tab",
			"expectDialog": {
				"active": true,
				"id": "perm_1",
				"actionContains": ["Deny"],
				"focused": 1
			}
		},
		{
			"key": "enter",
			"expectDialogResult": {"id": "perm_1", "kind": "permission", "action": "Deny", "status": "denied", "found": true},
			"expectDialog": {"active": false}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(52, 9, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].Status != DialogResultDenied {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
}

func TestRunDialogRuntimeScriptAcceptsDialogFieldAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"request_permission": {"permissionId": "perm_alias", "tool": "Bash", "prompt": "Need approval."},
			"expectDialog": {
				"isActive": true,
				"dialogId": "perm_alias",
				"dialogKind": "permission",
				"heading": "Permission",
				"content": "Tool: Bash\nNeed approval.",
				"actionContains": "Allow"
			}
		},
		{
			"key": "enter",
			"expectDialog": {"visible": false}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(52, 9, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].ID != "perm_alias" || result.DialogResults[0].Status != DialogResultAllowed {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
}

func TestRunInteractionScriptAcceptsDialogStepAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"dialog": {
				"dialogId": "custom",
				"dialogKind": "permission",
				"heading": "Custom",
				"content": "Choose carefully.",
				"options": "Proceed",
				"focusedIndex": 0
			},
			"expectDialog": {
				"visible": true,
				"dialogId": "custom",
				"dialogKind": "permission",
				"heading": "Custom",
				"content": "Choose carefully.",
				"actions": "Proceed",
				"focusedIndex": 0
			}
		},
		{
			"key": "enter",
			"expectEvent": {"type": "dialog_action", "value": "Proceed", "dialogId": "custom", "dialogKind": "permission"}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].DialogID != "custom" || result.Events[0].Value != "Proceed" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunDialogRuntimeScriptAcceptsCamelRuntimeAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`{"interactionScript":[
		{
			"baseStatus": "runtime ready",
			"requestPermission": {"id": "perm_1", "toolName": "Bash", "path": "/tmp/a"},
			"expectDialog": {"active": true, "id": "perm_1", "kind": "permission"},
			"expectStatusContains": ["runtime ready", "permissions: 1"]
		},
		{
			"key": "enter",
			"expectEvent": {"type": "dialog_action", "value": "Allow", "dialogId": "perm_1", "dialogKind": "permission"},
			"expectDialogResult": {"id": "perm_1", "status": "allowed", "found": true},
			"expectStatusContains": ["runtime ready"],
			"expectStatusNotContains": ["permissions: 1"]
		},
		{
			"upsertTask": {"taskId": "task_1", "name": "Build", "status": "running", "statusText": "go test", "progressPercent": 40},
			"openTasksDialog": true,
			"snapshotName": "tasks",
			"expectDialog": {"active": true, "id": "tasks", "kind": "task"},
			"expectTasks": {"count": 1, "stateCounts": {"running": 1}, "contains": {"taskId": "task_1", "taskTitle": "Build", "status": "running", "statusText": "go test", "progressPercent": 40}},
			"expectSnapshotContains": ["Build [running] 40% - go test"]
		},
		{
			"cancelAllTasks": true,
			"cancelTasksDetail": "stopped",
			"expectTasks": {"count": 1, "stateCounts": {"cancelled": 1}, "contains": [{"task_id": "task_1", "state": "cancelled", "detail": "stopped", "progress_percent": 40}]},
			"expectStatusContains": ["cancelled: 1"]
		},
		{
			"removeTaskId": "task_1",
			"expectTasks": {"count": 0}
		}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(50, 9, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].ID != "perm_1" || result.DialogResults[0].Status != DialogResultAllowed {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
	if len(runtime.Tasks) != 0 {
		t.Fatalf("tasks = %#v", runtime.Tasks)
	}
	if len(result.Snapshots) != 1 || result.Snapshots[0].Name != "tasks" {
		t.Fatalf("snapshots = %#v", result.Snapshots)
	}
}

func TestRunDialogRuntimeScriptAcceptsTaskExpectationAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"upsertTask": {"taskId": "task_1", "name": "Build", "status": "running", "statusText": "go test", "progressPercent": 40},
			"expectTasks": {
				"taskCount": 1,
				"statusCounts": {"running": 1},
				"contains": {"taskId": "task_1", "taskTitle": "Build", "status": "running", "statusText": "go test", "progressPercent": 40}
			}
		},
		{
			"cancelAllTasks": true,
			"cancelTasksDetail": "stopped",
			"expectTasks": {
				"total": 1,
				"countsByState": {"cancelled": 1},
				"contains": {"id": "task_1", "state": "cancelled", "detail": "stopped"}
			}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(50, 9, nil)
	runtime := NewDialogRuntime()
	if _, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps); err != nil {
		t.Fatal(err)
	}
}

func TestRunDialogRuntimeScriptNormalizesTaskStateAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"upsertTask": {"taskId": "task_1", "name": "Build", "status": "active", "statusText": "go test", "progressPercent": 40},
			"openTasksDialog": true,
			"snapshotName": "tasks",
			"expectTasks": {
				"taskCount": 1,
				"statusCounts": {"in_progress": 1},
				"contains": {"taskId": "task_1", "taskTitle": "Build", "status": "started", "statusText": "go test", "progressPercent": 40}
			},
			"expectStatusContains": ["running: 1"],
			"expectSnapshotContains": ["Build [running] 40% - go test"]
		},
		{
			"cancelAllTasks": true,
			"cancelTasksDetail": "stopped",
			"expectTasks": {
				"total": 1,
				"countsByState": {"canceled": 1},
				"contains": {"id": "task_1", "state": "canceled", "detail": "stopped"}
			},
			"expectStatusContains": ["cancelled: 1"]
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(50, 9, nil)
	runtime := NewDialogRuntime()
	if _, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps); err != nil {
		t.Fatal(err)
	}
}

func TestRunDialogRuntimeScriptAcceptsAdjacentTaskPayloadAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"upsertTask": {"jobId": 7001, "label": "Index repo", "phase": "processing", "message": "reading files", "percent": "33"},
			"openTasksDialog": true,
			"expectTasks": {
				"count": 1,
				"stateCounts": {"working": 1},
				"contains": {"taskID": "7001", "label": "Index repo", "phase": "active", "message": "reading files", "percent": 33}
			},
			"expectStatusContains": ["running: 1"],
			"expectSnapshotContains": ["Index repo [running] 33% - reading files"]
		},
		{
			"action": "task",
			"runID": "run_42",
			"displayName": "Write report",
			"taskState": "success",
			"currentStep": "done",
			"pct": "100",
			"expectTasks": {
				"count": 2,
				"stateCounts": {"running": 1, "done": 1},
				"contains": {"runId": "run_42", "displayName": "Write report", "taskState": "done", "currentStep": "done", "pct": 100}
			},
			"expectStatusContains": ["running: 1", "completed: 1"]
		},
		{
			"action": "remove-task",
			"jobID": 7001,
			"expectTasks": {
				"count": 1,
				"stateCounts": {"completed": 1},
				"contains": {"runID": "run_42", "status": "completed", "progress": 100}
			},
			"expectStatusNotContains": ["running: 1"]
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(60, 10, nil)
	runtime := NewDialogRuntime()
	if _, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps); err != nil {
		t.Fatal(err)
	}
}

func TestRunInteractionScriptAcceptsEventFieldAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"input": "go", "expectPrompt": {"text": "go"}},
		{"key": "enter", "expectEvents": {"event": "prompt_submitted", "text": "go"}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(30, 6, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "go" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptAcceptsExpectationWrappers(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"action": "typeText",
			"value": "go",
			"expect": {
				"prompt": {"text": "go"},
				"noEvent": "true"
			}
		},
		{
			"type": "press",
			"value": "enter",
			"assertions": {
				"event": {"type": "prompt_submitted", "value": "go"},
				"eventCount": 1,
				"totalEventCount": 1,
				"prompt": {"empty": "yes"}
			}
		},
		{
			"message": {"role": "assistant", "text": "ready"},
			"snapshot": "ready-state",
			"checks": {
				"snapshotContains": "assistant: ready",
				"screen": {"columns": 30, "rows": 6}
			}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(30, 6, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted || len(result.Snapshots) != 1 || result.Snapshots[0].Name != "ready-state" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunInteractionScriptAcceptsExpectationWrapperArrays(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"action": "typeText",
			"value": "go",
			"assertions": [
				{"type": "prompt", "value": {"text": "go"}},
				{"kind": "noEvent", "value": "true"}
			]
		},
		{
			"type": "press",
			"value": "enter",
			"expect": [
				{"target": "event", "value": {"type": "prompt_submitted", "value": "go"}},
				{"name": "eventCount", "value": 1},
				{"name": "totalEventCount", "value": 1},
				{"check": "prompt", "payload": {"empty": "yes"}}
			]
		},
		{
			"message": {"role": "assistant", "text": "ready"},
			"snapshot": "ready-array",
			"checks": [
				{"name": "snapshotContains", "value": "assistant: ready"},
				{"target": "screen", "value": {"columns": 30, "rows": 6}}
			]
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(30, 6, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted || result.Events[0].Value != "go" || len(result.Snapshots) != 1 || result.Snapshots[0].Name != "ready-array" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunDialogRuntimeScriptAcceptsEventAndResultAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"request_permission": {"id": "perm_1", "tool_name": "Bash"},
			"expectDialog": {"active": true, "id": "perm_1", "kind": "permission"}
		},
		{
			"key": "enter",
			"expectEvent": {"eventType": "dialog_action", "payload": "Allow", "dialogID": "perm_1", "dialogKind": "permission"},
			"expectDialogResult": {"dialogId": "perm_1", "dialogKind": "permission", "actionValue": "Allow", "resultStatus": "allowed", "exists": true, "isStale": false}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(42, 8, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].Status != DialogResultAllowed {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
}

func TestRunDialogRuntimeScriptChecksDialogResultCounts(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"request_permission": {"id": "perm_1", "tool_name": "Bash"},
			"expect_no_dialog_result": true,
			"expect_dialog_result_count": 0,
			"expectTotalDialogResultCount": 0,
			"expect_dialog": {"active": true, "id": "perm_1", "kind": "permission"}
		},
		{
			"key": "enter",
			"expectDialogResultCount": 1,
			"expect_total_dialog_result_count": 1,
			"expect_dialog_result": {"id": "perm_1", "status": "allowed", "found": true}
		},
		{
			"request_permission": {"id": "perm_2", "tool_name": "Read"},
			"expectNoDialogResult": true,
			"expect_total_dialog_result_count": 1
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptChecked(&screen, runtime, "ready", steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].ID != "perm_1" {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
}

func TestParseInteractionScriptAcceptsJSONArrayJSONLAndFile(t *testing.T) {
	arraySteps, err := ParseInteractionScript([]byte(`[
		{"text": "go", "expect_prompt": {"text": "go"}},
		{"key": "enter", "expect_event": {"type": "prompt_submitted", "value": "go"}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(30, 6, nil)
	result, err := RunInteractionScriptChecked(&screen, arraySteps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Value != "go" {
		t.Fatalf("array events = %#v", result.Events)
	}

	jsonl := []byte(`
{"text":"deploy","expect_prompt":{"text":"deploy"}}
{"key":"enter","expect_event":{"type":"prompt_submitted","value":"deploy"}}
`)
	jsonlSteps, err := ParseInteractionScript(jsonl)
	if err != nil {
		t.Fatal(err)
	}
	screen = NewREPLScreen(30, 6, nil)
	result, err = RunInteractionScriptChecked(&screen, jsonlSteps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Value != "deploy" {
		t.Fatalf("jsonl events = %#v", result.Events)
	}

	path := filepath.Join(t.TempDir(), "script.jsonl")
	if err := os.WriteFile(path, jsonl, 0o600); err != nil {
		t.Fatal(err)
	}
	fileSteps, err := LoadInteractionScript(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(fileSteps) != len(jsonlSteps) || fileSteps[0].Text != "deploy" {
		t.Fatalf("file steps = %#v", fileSteps)
	}
}

func TestParseInteractionScriptAcceptsLargeJSONLLine(t *testing.T) {
	largeText := strings.Repeat("x", 4*1024*1024+1024)
	data, err := json.Marshal(ScriptStep{Text: largeText})
	if err != nil {
		t.Fatal(err)
	}
	steps, err := ParseInteractionScript(append(data, '\n'))
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || len(steps[0].Text) != len(largeText) {
		t.Fatalf("steps = %d text bytes = %d", len(steps), len(steps[0].Text))
	}
}

func TestParseInteractionScriptAcceptsWrapperObjects(t *testing.T) {
	for name, script := range map[string]string{
		"steps":              `{"name":"basic","steps":[{"text":"go"},{"key":"enter","expect_event":{"type":"prompt_submitted","value":"go"}}]}`,
		"script":             `{"script":[{"text":"run"},{"key":"enter","expect_event":{"type":"prompt_submitted","value":"run"}}]}`,
		"scriptSteps":        `{"scriptSteps":[{"text":"lint"},{"key":"enter","expect_event":{"type":"prompt_submitted","value":"lint"}}]}`,
		"interaction_script": `{"interaction_script":[{"text":"ship"},{"key":"enter","expect_event":{"type":"prompt_submitted","value":"ship"}}]}`,
		"interactionScript":  `{"interactionScript":[{"text":"test"},{"key":"enter","expect_event":{"type":"prompt_submitted","value":"test"}}]}`,
		"interactionSteps":   `{"interactionSteps":[{"text":"build"},{"key":"enter","expect_event":{"type":"prompt_submitted","value":"build"}}]}`,
		"scenario":           `{"scenario":{"steps":[{"text":"review"},{"key":"enter","expect_event":{"type":"prompt_submitted","value":"review"}}]}}`,
		"fixture":            `{"fixture":{"script_steps":[{"text":"verify"},{"key":"enter","expect_event":{"type":"prompt_submitted","value":"verify"}}]}}`,
	} {
		steps, err := ParseInteractionScript([]byte(script))
		if err != nil {
			t.Fatalf("%s err = %v", name, err)
		}
		screen := NewREPLScreen(30, 6, nil)
		result, err := RunInteractionScriptChecked(&screen, steps)
		if err != nil {
			t.Fatalf("%s run err = %v", name, err)
		}
		if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted {
			t.Fatalf("%s events = %#v", name, result.Events)
		}
	}

	_, err := ParseInteractionScript([]byte(`{"steps":{"text":"not an array"}}`))
	if err == nil || !strings.Contains(err.Error(), `object "steps"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseInteractionScriptAcceptsStepRecordWrappers(t *testing.T) {
	for name, script := range map[string]string{
		"array": `[
			{"step":{"action":"type","value":"go"}},
			{"record":{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"go"}}}
		]`,
		"steps": `{"steps":[
			{"interactionStep":{"action":"type","value":"run"}},
			{"entry":{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"run"}}}
		]}`,
		"jsonl": `
{"event":{"action":"type","value":"ship"}}
{"item":{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"ship"}}}
`,
	} {
		t.Run(name, func(t *testing.T) {
			steps, err := ParseInteractionScript([]byte(script))
			if err != nil {
				t.Fatal(err)
			}
			screen := NewREPLScreen(30, 6, nil)
			result, err := RunInteractionScriptChecked(&screen, steps)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted {
				t.Fatalf("events = %#v", result.Events)
			}
		})
	}
}

func TestParseInteractionScriptAcceptsRecordArrayWrappers(t *testing.T) {
	for name, script := range map[string]string{
		"records": `{"records":[
			{"event":{"action":"type","value":"go"}},
			{"event":{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"go"}}}
		]}`,
		"events": `{"events":[
			{"step":{"action":"type","value":"run"}},
			{"step":{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"run"}}}
		]}`,
		"items": `{"items":[
			{"record":{"action":"type","value":"ship"}},
			{"record":{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"ship"}}}
		]}`,
		"timeline": `{"timeline":[
			{"action":"type","value":"test"},
			{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"test"}}
		]}`,
	} {
		t.Run(name, func(t *testing.T) {
			steps, err := ParseInteractionScript([]byte(script))
			if err != nil {
				t.Fatal(err)
			}
			screen := NewREPLScreen(30, 6, nil)
			result, err := RunInteractionScriptChecked(&screen, steps)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted {
				t.Fatalf("events = %#v", result.Events)
			}
		})
	}
}

func TestParseInteractionScriptAcceptsNestedRecordArrayWrappers(t *testing.T) {
	for name, script := range map[string]string{
		"data_records": `{"data":{"records":[
			{"step":{"action":"type","value":"go"}},
			{"step":{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"go"}}}
		]}}`,
		"payload_events": `{"payload":{"events":[
			{"record":{"action":"type","value":"run"}},
			{"record":{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"run"}}}
		]}}`,
		"result_timeline": `{"result":{"timeline":[
			{"action":"type","value":"ship"},
			{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"ship"}}
		]}}`,
		"resource_attributes": `{"id":"fixture_1","type":"interaction-script","attributes":{"steps":[
			{"action":"type","value":"plan"},
			{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"plan"}}
		]}}`,
		"resource_properties": `{"resource":{"id":"fixture_2","type":"interaction-script","properties":{"timeline":[
			{"action":"type","value":"audit"},
			{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"audit"}}
		]}}}`,
		"response_fixture": `{"response":{"fixture":{"steps":[
			{"action":"type","value":"test"},
			{"type":"press","value":"enter","expectEvent":{"type":"prompt_submitted","value":"test"}}
		]}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			steps, err := ParseInteractionScript([]byte(script))
			if err != nil {
				t.Fatal(err)
			}
			screen := NewREPLScreen(30, 6, nil)
			result, err := RunInteractionScriptChecked(&screen, steps)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Events) != 1 || result.Events[0].Type != ScreenEventPromptSubmitted {
				t.Fatalf("events = %#v", result.Events)
			}
		})
	}
}

func TestRunInteractionScriptFileCheckedLoadsAndRunsScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "script.json")
	script := []byte(`{"interactionScript":[
		{"message":{"role":"assistant","text":"ready"},"snapshot_name":"initial","expect_snapshot_contains":["assistant: ready"]},
		{"text":"go","key":"enter","expect_event":{"type":"prompt_submitted","value":"go"},"expect_prompt":{"empty":true}}
	]}`)
	if err := os.WriteFile(path, script, 0o600); err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptFileChecked(&screen, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Value != "go" {
		t.Fatalf("events = %#v", result.Events)
	}
	if len(result.Snapshots) != 1 || result.Snapshots[0].Name != "initial" {
		t.Fatalf("snapshots = %#v", result.Snapshots)
	}
}

func TestRunDialogRuntimeScriptFileCheckedLoadsAndRunsScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-script.json")
	script := []byte(`{"steps":[
		{"request_permission":{"id":"perm_1","tool_name":"Bash"},"expect_dialog":{"active":true,"id":"perm_1","kind":"permission"}},
		{"key":"enter","expect_dialog_result":{"id":"perm_1","status":"allowed","found":true}}
	]}`)
	if err := os.WriteFile(path, script, 0o600); err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	runtime := NewDialogRuntime()
	result, err := RunDialogRuntimeScriptFileChecked(&screen, runtime, "ready", path)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DialogResults) != 1 || result.DialogResults[0].ID != "perm_1" || result.DialogResults[0].Status != DialogResultAllowed {
		t.Fatalf("dialog results = %#v", result.DialogResults)
	}
}

func TestRunInteractionScriptFileWithSnapshotCorpus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.json")
	script := []byte(`{"steps":[
		{"message":{"role":"assistant","text":"ready"},"snapshot_name":"initial"},
		{"text":"go","snapshot_name":"typed"}
	]}`)
	if err := os.WriteFile(path, script, 0o600); err != nil {
		t.Fatal(err)
	}
	baselineScreen := NewREPLScreen(40, 8, nil)
	baseline, err := RunInteractionScriptFileChecked(&baselineScreen, path)
	if err != nil {
		t.Fatal(err)
	}
	corpus := SnapshotCorpus{Dir: filepath.Join(dir, "snapshots")}
	if err := corpus.WriteAll(baseline.Snapshots); err != nil {
		t.Fatal(err)
	}

	screen := NewREPLScreen(40, 8, nil)
	result, report, err := RunInteractionScriptFileWithSnapshotCorpus(&screen, path, corpus, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshots) != 2 || !report.Passed() || len(report.Comparisons) != 2 {
		t.Fatalf("result=%#v report=%#v", result.Snapshots, report)
	}

	if err := os.WriteFile(filepath.Join(corpus.Dir, "stale.txt"), []byte("old baseline"), 0o644); err != nil {
		t.Fatal(err)
	}
	screen = NewREPLScreen(40, 8, nil)
	_, report, err = RunInteractionScriptFileWithSnapshotCorpus(&screen, path, corpus, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed() || len(report.Unexpected) != 1 || report.Unexpected[0] != "stale" {
		t.Fatalf("strict report = %#v", report)
	}
}

func TestRunDialogRuntimeScriptFileWithSnapshotCorpus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime-script.json")
	script := []byte(`{"steps":[
		{"request_permission":{"id":"perm_1","tool_name":"Bash"},"snapshot_name":"permission","expect_dialog":{"active":true,"id":"perm_1","kind":"permission"}}
	]}`)
	if err := os.WriteFile(path, script, 0o600); err != nil {
		t.Fatal(err)
	}
	baselineScreen := NewREPLScreen(40, 8, nil)
	baselineRuntime := NewDialogRuntime()
	baseline, err := RunDialogRuntimeScriptFileChecked(&baselineScreen, baselineRuntime, "ready", path)
	if err != nil {
		t.Fatal(err)
	}
	corpus := SnapshotCorpus{Dir: filepath.Join(dir, "runtime-snapshots")}
	if err := corpus.WriteAll(baseline.Snapshots); err != nil {
		t.Fatal(err)
	}

	screen := NewREPLScreen(40, 8, nil)
	runtime := NewDialogRuntime()
	result, report, err := RunDialogRuntimeScriptFileWithSnapshotCorpus(&screen, runtime, "ready", path, corpus, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshots) != 1 || !report.Passed() || len(report.Comparisons) != 1 {
		t.Fatalf("result=%#v report=%#v", result.Snapshots, report)
	}
}

func TestParseInteractionScriptReportsJSONLLineNumber(t *testing.T) {
	_, err := ParseInteractionScript([]byte("{\"text\":\"ok\"}\n{bad}\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunInteractionScriptChecksEventSequences(t *testing.T) {
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, []ScriptStep{{
		Keys: []string{"a", "enter", "b", "enter"},
		ExpectEvents: []ScreenEvent{
			{Type: ScreenEventPromptSubmitted, Value: "a"},
			{Type: ScreenEventPromptSubmitted, Value: "b"},
		},
		ExpectPrompt: &PromptExpectation{Empty: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 || result.Events[0].Value != "a" || result.Events[1].Value != "b" {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestRunInteractionScriptChecksJSONEventCounts(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"text": "draft",
			"expect_no_event": true,
			"expect_event_count": 0,
			"expectTotalEventCount": 0,
			"expect_prompt": {"text": "draft"}
		},
		{
			"key": "enter",
			"expectEventCount": 1,
			"expect_total_event_count": 1,
			"expect_event": {"type": "prompt_submitted", "value": "draft"},
			"expect_prompt": {"empty": true}
		},
		{
			"keys": ["a", "enter"],
			"expect_event_count": 1,
			"expectTotalEventCount": 2,
			"expectEvents": {"type": "prompt_submitted", "value": "a"},
			"expect_prompt": {"empty": true}
		},
		{
			"keys": ["b", "enter", "c", "enter"],
			"expect_event_count": 2,
			"expectTotalEventCount": 4,
			"expect_events": [
				{"type": "prompt_submitted", "value": "b"},
				{"type": "prompt_submitted", "value": "c"}
			],
			"expect_prompt": {"empty": true}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 4 || result.Events[0].Value != "draft" || result.Events[3].Value != "c" {
		t.Fatalf("events = %#v", result.Events)
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

func TestRunInteractionScriptAcceptsVimAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{
			"input": "abc",
			"expectVim": {"vimEnabled": true, "modeName": "insert"}
		},
		{
			"keys": ["\u001b", "0", "y", "l"],
			"expectVim": {
				"isEnabled": true,
				"currentMode": "normal",
				"vimRegister": "a",
				"linewise": false
			}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(40, 8, nil)
	screen.SetVimEnabled(true)
	if _, err := RunInteractionScriptChecked(&screen, steps); err != nil {
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

func TestRunInteractionScriptAcceptsScreenViewportAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"expectScreen": {"columns": 22, "rows": 6}},
		{"message": {"role": "system", "text": "one"}},
		{"message": {"role": "system", "text": "two"}},
		{"message": {"role": "system", "text": "three"}},
		{"message": {"role": "system", "text": "four"}},
		{
			"message": {"role": "system", "text": "five"},
			"expectViewport": {"scrollOffset": 1, "visibleRows": 4, "visibleContains": "system: five"}
		},
		{
			"key": "\u001b[5~",
			"expectViewport": {"scroll_offset": 0, "lineCount": 4, "visibleNotContains": "system: five"}
		},
		{
			"resizeWidth": 30,
			"resizeHeight": 7,
			"expectScreen": {"screenWidth": 30, "screenHeight": 7}
		}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(22, 6, nil)
	if _, err := RunInteractionScriptChecked(&screen, steps); err != nil {
		t.Fatal(err)
	}
}

func TestRunInteractionScriptAcceptsStepStateAliases(t *testing.T) {
	steps, err := ParseInteractionScript([]byte(`[
		{"request": "label-only", "task": "label-only", "resize": {"columns": 42, "rows": 8}, "expectScreen": {"width": 42, "height": 8}},
		{"terminalSize": [44, 9], "expectScreen": {"width": 44, "height": 9}},
		{"blur": true, "expectEvent": {"type": "focus_out"}, "expectFocused": false},
		{"focus": true, "expectEvent": {"type": "focus_in"}, "expectFocused": true},
		{"message": {"role": "assistant", "text": "ready"}, "snapshot": "ready-state", "expectSnapshotContains": "assistant: ready"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	screen := NewREPLScreen(30, 6, nil)
	result, err := RunInteractionScriptChecked(&screen, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 || result.Events[0].Type != ScreenEventFocusOut || result.Events[1].Type != ScreenEventFocusIn {
		t.Fatalf("events = %#v", result.Events)
	}
	if len(result.Snapshots) != 1 || result.Snapshots[0].Name != "ready-state" {
		t.Fatalf("snapshots = %#v", result.Snapshots)
	}
}

func TestRunInteractionScriptChecksFocusState(t *testing.T) {
	screen := NewREPLScreen(30, 6, nil)
	focused := true
	blurred := false
	_, err := RunInteractionScriptChecked(&screen, []ScriptStep{
		{ExpectFocused: &focused, ExpectScreen: &ScreenExpectation{Width: 30, Height: 6}},
		{ResizeWidth: 42, ResizeHeight: 8, ExpectScreen: &ScreenExpectation{Width: 42, Height: 8}},
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
