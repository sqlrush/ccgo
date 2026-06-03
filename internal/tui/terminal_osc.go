package tui

import "strings"

const (
	OSCPrefix           = "\x1b]"
	OSCTerminator       = "\x07"
	OSCStringTerminator = "\x1b\\"
	OSCSetTitleAndIcon  = "0"
)

func OSCSequence(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		clean = append(clean, sanitizeOSCPayload(part))
	}
	return OSCPrefix + strings.Join(clean, ";") + OSCTerminator
}

func TerminalTitleSequence(title string) string {
	return OSCSequence(OSCSetTitleAndIcon, StripANSI(title))
}

func sanitizeOSCPayload(value string) string {
	value = strings.ReplaceAll(value, OSCTerminator, "")
	value = strings.ReplaceAll(value, OSCStringTerminator, "")
	return strings.ReplaceAll(value, "\x1b", "")
}
