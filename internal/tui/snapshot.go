package tui

import "strings"

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
	var out strings.Builder
	for i := 0; i < len(input); i++ {
		if input[i] != '\x1b' {
			out.WriteByte(input[i])
			continue
		}
		i++
		if i >= len(input) {
			break
		}
		if input[i] == '[' {
			for i+1 < len(input) {
				i++
				b := input[i]
				if b >= '@' && b <= '~' {
					break
				}
			}
			continue
		}
	}
	return out.String()
}
