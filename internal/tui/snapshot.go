package tui

type ANSISnapshot struct {
	Name   string
	Width  int
	Height int
	Output string
	Text   string
}

func CaptureANSISnapshot(name string, width int, height int, frame Frame) ANSISnapshot {
	return CaptureANSISnapshotWithOptions(name, width, height, frame, RenderOptions{})
}

func CaptureANSISnapshotWithOptions(name string, width int, height int, frame Frame, options RenderOptions) ANSISnapshot {
	output := RenderOnceWithOptions(width, height, frame, options)
	return ANSISnapshot{
		Name:   name,
		Width:  width,
		Height: height,
		Output: output,
		Text:   StripANSI(output),
	}
}

func StripANSI(input string) string {
	return TerminalVisibleText(input)
}
