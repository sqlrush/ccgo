package tui

import (
	"fmt"
	"strings"
)

type ScriptStep struct {
	Keys                   []string
	Key                    string
	Message                *Message
	Dialog                 *Dialog
	ResizeWidth            int
	ResizeHeight           int
	SnapshotName           string
	ExpectEvent            *ScreenEvent
	ExpectReverseSearch    *ReverseSearchExpectation
	ExpectSnapshotContains []string
}

type ReverseSearchExpectation struct {
	Active      bool
	Query       string
	Current     string
	ResultCount int
	NoResults   bool
}

type ScriptResult struct {
	Events    []ScreenEvent
	Snapshots []ANSISnapshot
}

func RunInteractionScript(screen *REPLScreen, steps []ScriptStep) ScriptResult {
	result, _ := RunInteractionScriptChecked(screen, steps)
	return result
}

func RunInteractionScriptChecked(screen *REPLScreen, steps []ScriptStep) (ScriptResult, error) {
	var result ScriptResult
	for index, step := range steps {
		var event ScreenEvent
		var snapshot ANSISnapshot
		if step.ResizeWidth > 0 {
			width := step.ResizeWidth
			height := screen.Height
			if step.ResizeHeight > 0 {
				height = step.ResizeHeight
			}
			screen.Resize(width, height)
		} else if step.ResizeHeight > 0 {
			screen.Resize(screen.Width, step.ResizeHeight)
		}
		if step.Message != nil {
			screen.AppendMessage(*step.Message)
		}
		if step.Dialog != nil {
			dialog := *step.Dialog
			screen.Dialog = &dialog
		}
		keys := step.Keys
		if step.Key != "" {
			keys = append(keys, step.Key)
		}
		for _, rawKey := range keys {
			event = screen.ApplyKey(ParseKey(rawKey))
			if event.Type != ScreenEventNone {
				result.Events = append(result.Events, event)
			}
		}
		if step.ExpectEvent != nil {
			if err := compareEvent(index, event, *step.ExpectEvent); err != nil {
				return result, err
			}
		}
		if step.ExpectReverseSearch != nil {
			if err := compareReverseSearch(index, screen.ReverseSearch, *step.ExpectReverseSearch); err != nil {
				return result, err
			}
		}
		if step.SnapshotName != "" {
			snapshot = CaptureANSISnapshot(step.SnapshotName, screen.Width, screen.Height, screen.Frame())
			result.Snapshots = append(result.Snapshots, snapshot)
		}
		if len(step.ExpectSnapshotContains) > 0 {
			if snapshot.Name == "" {
				snapshot = CaptureANSISnapshot(step.SnapshotName, screen.Width, screen.Height, screen.Frame())
			}
			for _, want := range step.ExpectSnapshotContains {
				if !strings.Contains(snapshot.Text, want) {
					return result, fmt.Errorf("script step %d snapshot missing %q in %q", index, want, snapshot.Text)
				}
			}
		}
	}
	return result, nil
}

func compareReverseSearch(index int, got ReverseSearchState, want ReverseSearchExpectation) error {
	if got.Active != want.Active {
		return fmt.Errorf("script step %d reverse active = %v, want %v", index, got.Active, want.Active)
	}
	if want.Query != "" && got.Query != want.Query {
		return fmt.Errorf("script step %d reverse query = %q, want %q", index, got.Query, want.Query)
	}
	if want.ResultCount > 0 && len(got.Results) != want.ResultCount {
		return fmt.Errorf("script step %d reverse result count = %d, want %d", index, len(got.Results), want.ResultCount)
	}
	if want.NoResults && len(got.Results) != 0 {
		return fmt.Errorf("script step %d reverse results = %#v, want none", index, got.Results)
	}
	if want.Current != "" {
		current, _ := got.Current()
		if current != want.Current {
			return fmt.Errorf("script step %d reverse current = %q, want %q", index, current, want.Current)
		}
	}
	return nil
}

func compareEvent(index int, got ScreenEvent, want ScreenEvent) error {
	if got.Type != want.Type {
		return fmt.Errorf("script step %d event type = %q, want %q", index, got.Type, want.Type)
	}
	if want.Value != "" && got.Value != want.Value {
		return fmt.Errorf("script step %d event value = %q, want %q", index, got.Value, want.Value)
	}
	if want.DialogID != "" && got.DialogID != want.DialogID {
		return fmt.Errorf("script step %d dialog id = %q, want %q", index, got.DialogID, want.DialogID)
	}
	if want.DialogKind != "" && got.DialogKind != want.DialogKind {
		return fmt.Errorf("script step %d dialog kind = %q, want %q", index, got.DialogKind, want.DialogKind)
	}
	return nil
}
