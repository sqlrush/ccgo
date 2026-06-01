package tui

import (
	"fmt"
	"strings"
)

type ScriptStep struct {
	Key                    string
	Message                *Message
	Dialog                 *Dialog
	ResizeWidth            int
	ResizeHeight           int
	SnapshotName           string
	ExpectEvent            *ScreenEvent
	ExpectSnapshotContains []string
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
			screen.Width = step.ResizeWidth
		}
		if step.ResizeHeight > 0 {
			screen.Height = step.ResizeHeight
		}
		if step.ResizeWidth > 0 || step.ResizeHeight > 0 {
			screen.rebuildViewport()
		}
		if step.Message != nil {
			screen.AppendMessage(*step.Message)
		}
		if step.Dialog != nil {
			dialog := *step.Dialog
			screen.Dialog = &dialog
		}
		if step.Key != "" {
			event = screen.ApplyKey(ParseKey(step.Key))
			if event.Type != ScreenEventNone {
				result.Events = append(result.Events, event)
			}
		}
		if step.ExpectEvent != nil {
			if err := compareEvent(index, event, *step.ExpectEvent); err != nil {
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
