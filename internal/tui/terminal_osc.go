package tui

import (
	"encoding/base64"
	"fmt"
	"regexp"
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
	OSCITerm2           = "9"
	OSCClipboard        = "52"
	OSCKitty            = "99"
	OSCGhostty          = "777"
	OSCTabStatus        = "21337"

	ITerm2Progress              = "4"
	ITerm2ProgressClear         = "0"
	ITerm2ProgressSet           = "1"
	ITerm2ProgressError         = "2"
	ITerm2ProgressIndeterminate = "3"
)

type TerminalProgressState string

const (
	TerminalProgressRunning       TerminalProgressState = "running"
	TerminalProgressCompleted     TerminalProgressState = "completed"
	TerminalProgressError         TerminalProgressState = "error"
	TerminalProgressIndeterminate TerminalProgressState = "indeterminate"
)

var (
	oscHexColorPattern = regexp.MustCompile(`^#([0-9a-fA-F]{2})([0-9a-fA-F]{2})([0-9a-fA-F]{2})$`)
	oscRGBColorPattern = regexp.MustCompile(`^rgb:([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})$`)
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

type TerminalHyperlink struct {
	URL    string
	Params map[string]string
	End    bool
}

type OSCActionType string

const (
	OSCActionTitle     OSCActionType = "title"
	OSCActionLink      OSCActionType = "link"
	OSCActionTabStatus OSCActionType = "tabStatus"
	OSCActionUnknown   OSCActionType = "unknown"
)

type TerminalTitleAction struct {
	Type  string
	Title string
	Name  string
}

type OSCAction struct {
	Type      OSCActionType
	Title     TerminalTitleAction
	Hyperlink TerminalHyperlink
	TabStatus TabStatusFields
	Sequence  string
}

func OSCSequence(parts ...string) string {
	return OSCSequenceWithTerminator(OSCTerminator, parts...)
}

func OSCSequenceWithTerminator(terminator string, parts ...string) string {
	if terminator == "" {
		terminator = OSCTerminator
	}
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		clean = append(clean, sanitizeOSCPayload(part))
	}
	return OSCPrefix + strings.Join(clean, ";") + terminator
}

func OSCSequenceWithStringTerminator(parts ...string) string {
	return OSCSequenceWithTerminator(OSCStringTerminator, parts...)
}

func TerminalTitleSequence(title string) string {
	return OSCSequence(OSCSetTitleAndIcon, StripANSI(title))
}

func ClearTerminalTitleSequence() string {
	return OSCSequence(OSCSetTitleAndIcon, "")
}

func ParseOSCSequence(sequence string) (OSCAction, bool) {
	if !strings.HasPrefix(sequence, OSCPrefix) {
		return OSCAction{}, false
	}
	content := strings.TrimPrefix(sequence, OSCPrefix)
	switch {
	case strings.HasSuffix(content, OSCTerminator):
		content = strings.TrimSuffix(content, OSCTerminator)
	case strings.HasSuffix(content, OSCStringTerminator):
		content = strings.TrimSuffix(content, OSCStringTerminator)
	default:
		return OSCAction{}, false
	}
	return ParseOSCContent(content), true
}

func ParseOSCContent(content string) OSCAction {
	command, data, ok := strings.Cut(content, ";")
	if !ok {
		data = ""
	}
	commandNumber, err := strconv.Atoi(command)
	if err != nil {
		return OSCAction{Type: OSCActionUnknown, Sequence: OSCPrefix + content}
	}
	switch strconv.Itoa(commandNumber) {
	case OSCSetTitleAndIcon:
		return OSCAction{
			Type:  OSCActionTitle,
			Title: TerminalTitleAction{Type: "both", Title: data},
		}
	case "1":
		return OSCAction{
			Type:  OSCActionTitle,
			Title: TerminalTitleAction{Type: "iconName", Name: data},
		}
	case "2":
		return OSCAction{
			Type:  OSCActionTitle,
			Title: TerminalTitleAction{Type: "windowTitle", Title: data},
		}
	case OSCHyperlink:
		return OSCAction{Type: OSCActionLink, Hyperlink: ParseHyperlinkPayload(data)}
	case OSCTabStatus:
		return OSCAction{Type: OSCActionTabStatus, TabStatus: ParseTabStatusPayload(data)}
	default:
		return OSCAction{Type: OSCActionUnknown, Sequence: OSCPrefix + content}
	}
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

func ParseHyperlinkPayload(payload string) TerminalHyperlink {
	paramsText, url, ok := strings.Cut(payload, ";")
	if !ok {
		url = ""
	}
	if url == "" {
		return TerminalHyperlink{End: true}
	}
	params := map[string]string{}
	if paramsText != "" {
		for _, pair := range strings.Split(paramsText, ":") {
			key, value, ok := strings.Cut(pair, "=")
			if ok {
				params[key] = value
			}
		}
	}
	if len(params) == 0 {
		params = nil
	}
	return TerminalHyperlink{URL: url, Params: params}
}

func TerminalClipboardSequence(text string) string {
	return OSCSequence(OSCClipboard, "c", base64.StdEncoding.EncodeToString([]byte(text)))
}

func TerminalProgressSequence(state TerminalProgressState, percentage int) string {
	switch state {
	case "", TerminalProgressCompleted:
		return ClearTerminalProgressSequence()
	case TerminalProgressRunning:
		return OSCSequence(OSCITerm2, ITerm2Progress, ITerm2ProgressSet, strconv.Itoa(clampPercent(percentage)))
	case TerminalProgressError:
		return OSCSequence(OSCITerm2, ITerm2Progress, ITerm2ProgressError, strconv.Itoa(clampPercent(percentage)))
	case TerminalProgressIndeterminate:
		return OSCSequence(OSCITerm2, ITerm2Progress, ITerm2ProgressIndeterminate, "")
	default:
		return ""
	}
}

func ClearTerminalProgressSequence() string {
	return OSCSequence(OSCITerm2, ITerm2Progress, ITerm2ProgressClear, "")
}

func ITerm2NotificationSequence(message string, title string) string {
	display := message
	if title != "" {
		display = title + ":\n" + message
	}
	return OSCSequence(OSCITerm2, "\n\n"+display)
}

func KittyNotificationSequences(message string, title string, id int) []string {
	idValue := strconv.Itoa(id)
	return []string{
		OSCSequence(OSCKitty, "i="+idValue+":d=0:p=title", title),
		OSCSequence(OSCKitty, "i="+idValue+":p=body", message),
		OSCSequence(OSCKitty, "i="+idValue+":d=1:a=focus", ""),
	}
}

func GhosttyNotificationSequence(message string, title string) string {
	return OSCSequence(OSCGhostty, "notify", title, message)
}

func TerminalBellSequence() string {
	return OSCTerminator
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

func ParseTabStatusPayload(payload string) TabStatusFields {
	var fields TabStatusFields
	for _, pair := range splitTabStatusPairs(payload) {
		key, value := pair[0], pair[1]
		switch key {
		case "indicator":
			if color, ok := ParseOSCColor(value); ok {
				fields.Indicator = color
				fields.ClearIndicator = false
			} else {
				fields.Indicator = nil
				fields.ClearIndicator = true
			}
		case "status":
			if value == "" {
				fields.Status = nil
				fields.ClearStatus = true
			} else {
				fields.Status = &value
				fields.ClearStatus = false
			}
		case "status-color":
			if color, ok := ParseOSCColor(value); ok {
				fields.StatusColor = color
				fields.ClearStatusColor = false
			} else {
				fields.StatusColor = nil
				fields.ClearStatusColor = true
			}
		}
	}
	return fields
}

func splitTabStatusPairs(payload string) [][2]string {
	var pairs [][2]string
	var key strings.Builder
	var value strings.Builder
	inValue := false
	escaped := false
	for _, r := range payload {
		switch {
		case escaped:
			if inValue {
				value.WriteRune(r)
			} else {
				key.WriteRune(r)
			}
			escaped = false
		case r == '\\':
			escaped = true
		case r == ';':
			pairs = append(pairs, [2]string{key.String(), value.String()})
			key.Reset()
			value.Reset()
			inValue = false
		case r == '=' && !inValue:
			inValue = true
		case inValue:
			value.WriteRune(r)
		default:
			key.WriteRune(r)
		}
	}
	if key.Len() > 0 || inValue {
		pairs = append(pairs, [2]string{key.String(), value.String()})
	}
	return pairs
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

func ParseOSCColor(spec string) (*RGBColor, bool) {
	if match := oscHexColorPattern.FindStringSubmatch(spec); match != nil {
		color, err := parseHexColorParts(match[1], match[2], match[3])
		if err != nil {
			return nil, false
		}
		return &color, true
	}
	if match := oscRGBColorPattern.FindStringSubmatch(spec); match != nil {
		color, err := parseScaledRGBColorParts(match[1], match[2], match[3])
		if err != nil {
			return nil, false
		}
		return &color, true
	}
	return nil, false
}

func parseHexColorParts(red string, green string, blue string) (RGBColor, error) {
	r, err := strconv.ParseInt(red, 16, 0)
	if err != nil {
		return RGBColor{}, err
	}
	g, err := strconv.ParseInt(green, 16, 0)
	if err != nil {
		return RGBColor{}, err
	}
	b, err := strconv.ParseInt(blue, 16, 0)
	if err != nil {
		return RGBColor{}, err
	}
	return RGBColor{R: int(r), G: int(g), B: int(b)}, nil
}

func parseScaledRGBColorParts(red string, green string, blue string) (RGBColor, error) {
	r, err := scaleOSCColorPart(red)
	if err != nil {
		return RGBColor{}, err
	}
	g, err := scaleOSCColorPart(green)
	if err != nil {
		return RGBColor{}, err
	}
	b, err := scaleOSCColorPart(blue)
	if err != nil {
		return RGBColor{}, err
	}
	return RGBColor{R: r, G: g, B: b}, nil
}

func scaleOSCColorPart(part string) (int, error) {
	value, err := strconv.ParseInt(part, 16, 0)
	if err != nil {
		return 0, err
	}
	maxValue := (1 << (4 * len(part))) - 1
	return int((value*255 + int64(maxValue)/2) / int64(maxValue)), nil
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
