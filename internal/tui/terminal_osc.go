package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	OSCPrefix           = "\x1b]"
	OSCTerminator       = "\x07"
	OSCStringTerminator = "\x1b\\"
	OSCSetTitleAndIcon  = "0"
	OSCHyperlink        = "8"
	OSCTabStatus        = "21337"
)

type RGBColor struct {
	R int
	G int
	B int
}

type TabStatusFields struct {
	Indicator        *RGBColor
	ClearIndicator   bool
	Status           *string
	ClearStatus      bool
	StatusColor      *RGBColor
	ClearStatusColor bool
}

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

func ClearTerminalTitleSequence() string {
	return OSCSequence(OSCSetTitleAndIcon, "")
}

func TerminalHyperlinkSequence(url string, params map[string]string) string {
	if url == "" {
		return EndTerminalHyperlinkSequence()
	}
	merged := map[string]string{"id": osc8ID(url)}
	for key, value := range params {
		merged[key] = value
	}
	keys := make([]string, 0, len(merged)-1)
	for key := range merged {
		if key != "id" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	pairs := []string{"id=" + merged["id"]}
	for _, key := range keys {
		pairs = append(pairs, key+"="+merged[key])
	}
	return OSCSequence(OSCHyperlink, strings.Join(pairs, ":"), url)
}

func EndTerminalHyperlinkSequence() string {
	return OSCSequence(OSCHyperlink, "", "")
}

func TabStatusSequence(fields TabStatusFields) string {
	parts := []string{}
	if fields.Indicator != nil || fields.ClearIndicator {
		parts = append(parts, "indicator="+tabStatusColor(fields.Indicator))
	}
	if fields.Status != nil || fields.ClearStatus {
		status := ""
		if fields.Status != nil {
			status = escapeTabStatusText(*fields.Status)
		}
		parts = append(parts, "status="+status)
	}
	if fields.StatusColor != nil || fields.ClearStatusColor {
		parts = append(parts, "status-color="+tabStatusColor(fields.StatusColor))
	}
	return OSCSequence(OSCTabStatus, strings.Join(parts, ";"))
}

func ClearTabStatusSequence() string {
	return OSCSequence(OSCTabStatus, "indicator=;status=;status-color=")
}

func WrapForTerminalMultiplexer(sequence string, multiplexer string) string {
	switch strings.ToLower(strings.TrimSpace(multiplexer)) {
	case "tmux":
		return "\x1bPtmux;" + strings.ReplaceAll(sequence, "\x1b", "\x1b\x1b") + OSCStringTerminator
	case "screen":
		return "\x1bP" + sequence + OSCStringTerminator
	default:
		return sequence
	}
}

func sanitizeOSCPayload(value string) string {
	value = strings.ReplaceAll(value, OSCTerminator, "")
	value = strings.ReplaceAll(value, OSCStringTerminator, "")
	return strings.ReplaceAll(value, "\x1b", "")
}

func tabStatusColor(color *RGBColor) string {
	if color == nil {
		return ""
	}
	return fmt.Sprintf("#%02x%02x%02x", clampByte(color.R), clampByte(color.G), clampByte(color.B))
}

func clampByte(value int) int {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return value
}

func osc8ID(url string) string {
	var hash uint32
	for _, unit := range utf16.Encode([]rune(url)) {
		hash = hash*31 + uint32(unit)
	}
	return strconv.FormatUint(uint64(hash), 36)
}

func escapeTabStatusText(text string) string {
	text = sanitizeOSCPayload(StripANSI(text))
	text = strings.ReplaceAll(text, "\\", "\\\\")
	return strings.ReplaceAll(text, ";", "\\;")
}
