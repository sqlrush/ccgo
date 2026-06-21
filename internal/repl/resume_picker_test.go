package repl

import (
	"testing"

	"ccgo/internal/tui"
)

func sampleResumeEntries() []ResumeEntry {
	return []ResumeEntry{
		{ID: "s1", Summary: "fix bug", ProjectPath: "/a", ModifiedUnix: 200},
		{ID: "s2", Summary: "add feature", ProjectPath: "/b", ModifiedUnix: 100},
	}
}

func TestResumePickerEnterSelects(t *testing.T) {
	p := NewResumePicker(sampleResumeEntries())
	p.ApplyKey(tui.Key{Type: tui.KeyDown})
	res, _ := p.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "resume:s2" {
		t.Fatalf("submit = %q want resume:s2", res.Submit)
	}
}

func TestResumePickerEscDismisses(t *testing.T) {
	p := NewResumePicker(sampleResumeEntries())
	res, _ := p.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !res.Dismissed {
		t.Fatal("Esc should dismiss")
	}
}

func TestResumePickerRenderShowsSummaries(t *testing.T) {
	p := NewResumePicker(sampleResumeEntries())
	lines := p.Render(80, 24)
	joined := ""
	for _, l := range lines {
		joined += l
	}
	if !contains(joined, "fix bug") || !contains(joined, "add feature") {
		t.Fatalf("render missing summaries: %q", joined)
	}
}

// Additional edge case tests
func TestResumePickerUpAtTopNoOp(t *testing.T) {
	p := NewResumePicker(sampleResumeEntries())
	// cursor starts at 0; pressing up should be a no-op
	res, handled := p.ApplyKey(tui.Key{Type: tui.KeyUp})
	if !handled {
		t.Fatal("Up should be handled")
	}
	if res.Submit != "" || res.Dismissed {
		t.Fatalf("Up at top should return empty result, got Submit=%q Dismissed=%v", res.Submit, res.Dismissed)
	}
	// cursor should still be 0
	entry, ok := p.Selected()
	if !ok || entry.ID != "s1" {
		t.Fatalf("cursor should still be at s1, got %q", entry.ID)
	}
}

func TestResumePickerDownAtBottomNoOp(t *testing.T) {
	entries := sampleResumeEntries()
	p := NewResumePicker(entries)
	// move to the last entry
	p.ApplyKey(tui.Key{Type: tui.KeyDown})
	// now at bottom; pressing down should be a no-op
	res, handled := p.ApplyKey(tui.Key{Type: tui.KeyDown})
	if !handled {
		t.Fatal("Down should be handled")
	}
	if res.Submit != "" || res.Dismissed {
		t.Fatalf("Down at bottom should return empty result, got Submit=%q Dismissed=%v", res.Submit, res.Dismissed)
	}
	// cursor should still be at the last entry
	entry, ok := p.Selected()
	if !ok || entry.ID != "s2" {
		t.Fatalf("cursor should still be at s2, got %q", entry.ID)
	}
}

func TestResumePickerEnterOnEmptyListDismisses(t *testing.T) {
	p := NewResumePicker([]ResumeEntry{})
	res, handled := p.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if !res.Dismissed {
		t.Fatalf("Enter on empty list should dismiss, got Submit=%q", res.Submit)
	}
}

func TestResumePickerInitialCursorValid(t *testing.T) {
	entries := []ResumeEntry{
		{ID: "a", Summary: "first", ProjectPath: "/x", ModifiedUnix: 1},
	}
	p := NewResumePicker(entries)
	entry, ok := p.Selected()
	if !ok || entry.ID != "a" {
		t.Fatal("initial cursor should be valid")
	}
}

func TestResumePickerImmutability(t *testing.T) {
	entries := sampleResumeEntries()
	original := make([]ResumeEntry, len(entries))
	copy(original, entries)

	p := NewResumePicker(entries)
	p.ApplyKey(tui.Key{Type: tui.KeyDown})
	p.Render(80, 24)

	// Verify the input slice was not mutated
	for i, e := range entries {
		if e != original[i] {
			t.Fatalf("input entries were mutated at index %d", i)
		}
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && index(s, sub) >= 0 }
func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
