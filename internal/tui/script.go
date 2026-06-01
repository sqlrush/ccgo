package tui

type ScriptStep struct {
	Key          string
	Message      *Message
	Dialog       *Dialog
	ResizeWidth  int
	ResizeHeight int
	SnapshotName string
}

type ScriptResult struct {
	Events    []ScreenEvent
	Snapshots []ANSISnapshot
}

func RunInteractionScript(screen *REPLScreen, steps []ScriptStep) ScriptResult {
	var result ScriptResult
	for _, step := range steps {
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
			event := screen.ApplyKey(ParseKey(step.Key))
			if event.Type != ScreenEventNone {
				result.Events = append(result.Events, event)
			}
		}
		if step.SnapshotName != "" {
			result.Snapshots = append(result.Snapshots, CaptureANSISnapshot(step.SnapshotName, screen.Width, screen.Height, screen.Frame()))
		}
	}
	return result
}
