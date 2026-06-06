package tui

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	OSCPrefix             = "\x1b]"
	OSCTerminator         = "\x07"
	OSCStringTerminator   = "\x1b\\"
	OSCSetTitleAndIcon    = "0"
	OSCPaletteColor       = "4"
	OSCCurrentDirectory   = "7"
	OSCHyperlink          = "8"
	OSCITerm2             = "9"
	OSCForegroundColor    = "10"
	OSCBackgroundColor    = "11"
	OSCCursorColor        = "12"
	OSCResetForeground    = "110"
	OSCResetBackground    = "111"
	OSCResetCursor        = "112"
	OSCResetPalette       = "104"
	OSCClipboard          = "52"
	OSCKitty              = "99"
	OSCShellIntegration   = "133"
	OSCVSShellIntegration = "633"
	OSCGhostty            = "777"
	OSCTabStatus          = "21337"

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
	OSCActionTitle        OSCActionType = "title"
	OSCActionDirectory    OSCActionType = "directory"
	OSCActionLink         OSCActionType = "link"
	OSCActionTabStatus    OSCActionType = "tabStatus"
	OSCActionClipboard    OSCActionType = "clipboard"
	OSCActionColor        OSCActionType = "color"
	OSCActionPalette      OSCActionType = "palette"
	OSCActionProgress     OSCActionType = "progress"
	OSCActionNotification OSCActionType = "notification"
	OSCActionShell        OSCActionType = "shellIntegration"
	OSCActionUnknown      OSCActionType = "unknown"
)

type TerminalTitleAction struct {
	Type  string
	Title string
	Name  string
}

type TerminalDirectoryAction struct {
	URI    string
	Scheme string
	Host   string
	Path   string
	Valid  bool
}

type TerminalClipboardAction struct {
	Selection string
	Text      string
	Base64    string
	Clear     bool
	Valid     bool
}

type TerminalProgressAction struct {
	State      TerminalProgressState
	Percent    int
	Clear      bool
	Known      bool
	RawCommand string
}

type TerminalOSCColorAction struct {
	Target string
	Color  *RGBColor
	Query  bool
	Reset  bool
	Raw    string
	Valid  bool
}

type TerminalOSCPaletteEntry struct {
	Index int
	Color *RGBColor
	Query bool
	Reset bool
	Raw   string
	Valid bool
}

type TerminalOSCPaletteAction struct {
	Entries  []TerminalOSCPaletteEntry
	ResetAll bool
	Raw      string
	Valid    bool
}

type TerminalNotificationAction struct {
	Provider string
	ID       string
	Part     string
	Action   string
	Title    string
	Message  string
}

type TerminalShellIntegrationAction struct {
	Marker      string
	RawMarker   string
	Value       string
	Properties  map[string]string
	ExitCode    int
	HasExitCode bool
}

type OSCAction struct {
	Type         OSCActionType
	Title        TerminalTitleAction
	Directory    TerminalDirectoryAction
	Hyperlink    TerminalHyperlink
	TabStatus    TabStatusFields
	Clipboard    TerminalClipboardAction
	Color        TerminalOSCColorAction
	Palette      TerminalOSCPaletteAction
	Progress     TerminalProgressAction
	Notification TerminalNotificationAction
	Shell        TerminalShellIntegrationAction
	Sequence     string
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
	case OSCPaletteColor:
		return OSCAction{Type: OSCActionPalette, Palette: ParseOSCPalettePayload(data)}
	case OSCCurrentDirectory:
		return OSCAction{Type: OSCActionDirectory, Directory: ParseDirectoryPayload(data)}
	case OSCHyperlink:
		return OSCAction{Type: OSCActionLink, Hyperlink: ParseHyperlinkPayload(data)}
	case OSCForegroundColor, OSCBackgroundColor, OSCCursorColor, OSCResetForeground, OSCResetBackground, OSCResetCursor:
		return OSCAction{Type: OSCActionColor, Color: ParseOSCColorPayload(strconv.Itoa(commandNumber), data)}
	case OSCResetPalette:
		return OSCAction{Type: OSCActionPalette, Palette: ParseOSCPaletteResetPayload(data)}
	case OSCClipboard:
		return OSCAction{Type: OSCActionClipboard, Clipboard: ParseClipboardPayload(data)}
	case OSCITerm2:
		if progress, ok := ParseITerm2ProgressPayload(data); ok {
			return OSCAction{Type: OSCActionProgress, Progress: progress}
		}
		if notification, ok := ParseITerm2NotificationPayload(data); ok {
			return OSCAction{Type: OSCActionNotification, Notification: notification}
		}
		return OSCAction{Type: OSCActionUnknown, Sequence: OSCPrefix + content}
	case OSCKitty:
		if notification, ok := ParseKittyNotificationPayload(data); ok {
			return OSCAction{Type: OSCActionNotification, Notification: notification}
		}
		return OSCAction{Type: OSCActionUnknown, Sequence: OSCPrefix + content}
	case OSCShellIntegration, OSCVSShellIntegration:
		if shell, ok := ParseShellIntegrationPayload(data); ok {
			return OSCAction{Type: OSCActionShell, Shell: shell}
		}
		return OSCAction{Type: OSCActionUnknown, Sequence: OSCPrefix + content}
	case OSCGhostty:
		if notification, ok := ParseGhosttyNotificationPayload(data); ok {
			return OSCAction{Type: OSCActionNotification, Notification: notification}
		}
		return OSCAction{Type: OSCActionUnknown, Sequence: OSCPrefix + content}
	case OSCTabStatus:
		return OSCAction{Type: OSCActionTabStatus, TabStatus: ParseTabStatusPayload(data)}
	default:
		return OSCAction{Type: OSCActionUnknown, Sequence: OSCPrefix + content}
	}
}

func TerminalDirectorySequence(uri string) string {
	return OSCSequence(OSCCurrentDirectory, uri)
}

func ParseDirectoryPayload(payload string) TerminalDirectoryAction {
	action := TerminalDirectoryAction{URI: payload}
	parsed, err := url.Parse(payload)
	if err != nil {
		return action
	}
	action.Valid = true
	action.Scheme = parsed.Scheme
	action.Host = parsed.Host
	action.Path = parsed.Path
	return action
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

func ParseClipboardPayload(payload string) TerminalClipboardAction {
	selection, encoded, ok := strings.Cut(payload, ";")
	if !ok {
		selection = payload
		encoded = ""
	}
	action := TerminalClipboardAction{Selection: selection, Base64: encoded}
	if encoded == "" {
		action.Clear = true
		action.Valid = true
		return action
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return action
	}
	action.Text = string(decoded)
	action.Valid = true
	return action
}

func ParseOSCColorPayload(command string, payload string) TerminalOSCColorAction {
	action := TerminalOSCColorAction{
		Target: oscColorTarget(command),
		Raw:    payload,
	}
	payload = strings.TrimSpace(payload)
	if oscColorResetCommand(command) {
		action.Reset = true
		action.Valid = payload == ""
		return action
	}
	if payload == "?" {
		action.Query = true
		action.Valid = true
		return action
	}
	if color, ok := ParseOSCColor(payload); ok {
		action.Color = color
		action.Valid = true
	}
	return action
}

func oscColorTarget(command string) string {
	switch command {
	case OSCForegroundColor:
		return "foreground"
	case OSCBackgroundColor:
		return "background"
	case OSCCursorColor:
		return "cursor"
	case OSCResetForeground:
		return "foreground"
	case OSCResetBackground:
		return "background"
	case OSCResetCursor:
		return "cursor"
	default:
		return command
	}
}

func oscColorResetCommand(command string) bool {
	switch command {
	case OSCResetForeground, OSCResetBackground, OSCResetCursor:
		return true
	default:
		return false
	}
}

func ParseOSCPalettePayload(payload string) TerminalOSCPaletteAction {
	action := TerminalOSCPaletteAction{
		Raw: payload,
	}
	parts := strings.Split(payload, ";")
	if len(parts) == 0 || len(parts)%2 != 0 {
		return action
	}
	entries := make([]TerminalOSCPaletteEntry, 0, len(parts)/2)
	for i := 0; i < len(parts); i += 2 {
		index, ok := parseOSCPaletteIndex(parts[i])
		if !ok {
			return action
		}
		spec := strings.TrimSpace(parts[i+1])
		entry := TerminalOSCPaletteEntry{
			Index: index,
			Raw:   parts[i+1],
		}
		switch {
		case spec == "?":
			entry.Query = true
			entry.Valid = true
		default:
			color, ok := ParseOSCColor(spec)
			if !ok {
				return action
			}
			entry.Color = color
			entry.Valid = true
		}
		entries = append(entries, entry)
	}
	action.Entries = entries
	action.Valid = len(entries) > 0
	return action
}

func ParseOSCPaletteResetPayload(payload string) TerminalOSCPaletteAction {
	action := TerminalOSCPaletteAction{
		Raw: payload,
	}
	payload = strings.TrimSpace(payload)
	if payload == "" {
		action.ResetAll = true
		action.Valid = true
		return action
	}
	parts := strings.Split(payload, ";")
	entries := make([]TerminalOSCPaletteEntry, 0, len(parts))
	for _, part := range parts {
		index, ok := parseOSCPaletteIndex(part)
		if !ok {
			return action
		}
		entries = append(entries, TerminalOSCPaletteEntry{
			Index: index,
			Reset: true,
			Raw:   part,
			Valid: true,
		})
	}
	action.Entries = entries
	action.Valid = len(entries) > 0
	return action
}

func parseOSCPaletteIndex(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	index, err := strconv.Atoi(value)
	if err != nil || index < 0 {
		return 0, false
	}
	return index, true
}

func ParseITerm2ProgressPayload(payload string) (TerminalProgressAction, bool) {
	parts := strings.Split(payload, ";")
	if len(parts) < 2 || parts[0] != ITerm2Progress {
		return TerminalProgressAction{}, false
	}
	progress := TerminalProgressAction{RawCommand: parts[1]}
	switch parts[1] {
	case ITerm2ProgressClear:
		progress.State = TerminalProgressCompleted
		progress.Clear = true
		progress.Known = true
	case ITerm2ProgressSet:
		progress.State = TerminalProgressRunning
		progress.Percent = parseOSCPercent(parts)
		progress.Known = true
	case ITerm2ProgressError:
		progress.State = TerminalProgressError
		progress.Percent = parseOSCPercent(parts)
		progress.Known = true
	case ITerm2ProgressIndeterminate:
		progress.State = TerminalProgressIndeterminate
		progress.Known = true
	default:
		return progress, true
	}
	return progress, true
}

func ParseITerm2NotificationPayload(payload string) (TerminalNotificationAction, bool) {
	if !strings.HasPrefix(payload, "\n\n") {
		return TerminalNotificationAction{}, false
	}
	text := strings.TrimPrefix(payload, "\n\n")
	notification := TerminalNotificationAction{Provider: "iterm2", Message: text}
	if title, message, ok := strings.Cut(text, ":\n"); ok {
		notification.Title = title
		notification.Message = message
	}
	return notification, true
}

func ParseKittyNotificationPayload(payload string) (TerminalNotificationAction, bool) {
	fieldsText, value, _ := strings.Cut(payload, ";")
	fields := parseColonFields(fieldsText)
	part := fields["p"]
	action := fields["a"]
	if part == "" && action == "" {
		return TerminalNotificationAction{}, false
	}
	notification := TerminalNotificationAction{
		Provider: "kitty",
		ID:       fields["i"],
		Part:     part,
		Action:   action,
	}
	switch part {
	case "title":
		notification.Title = value
	case "body":
		notification.Message = value
	}
	return notification, true
}

func ParseGhosttyNotificationPayload(payload string) (TerminalNotificationAction, bool) {
	parts := strings.SplitN(payload, ";", 3)
	if len(parts) < 2 || parts[0] != "notify" {
		return TerminalNotificationAction{}, false
	}
	notification := TerminalNotificationAction{Provider: "ghostty", Title: parts[1]}
	if len(parts) == 3 {
		notification.Message = parts[2]
	}
	return notification, true
}

func ParseShellIntegrationPayload(payload string) (TerminalShellIntegrationAction, bool) {
	marker, rest, _ := strings.Cut(payload, ";")
	action := TerminalShellIntegrationAction{RawMarker: marker}
	switch marker {
	case "A":
		action.Marker = "promptStart"
	case "B":
		action.Marker = "promptEnd"
	case "C":
		action.Marker = "commandStart"
	case "D":
		action.Marker = "commandEnd"
	case "E":
		action.Marker = "commandLine"
		action.Value = rest
	case "P":
		action.Marker = "property"
		action.Value = rest
		action.Properties = parseSemicolonFields(rest)
	default:
		return TerminalShellIntegrationAction{}, false
	}
	if marker == "D" && rest != "" {
		codeText, _, _ := strings.Cut(rest, ";")
		if code, err := strconv.Atoi(strings.TrimSpace(codeText)); err == nil {
			action.ExitCode = code
			action.HasExitCode = true
		}
	}
	return action, true
}

func parseSemicolonFields(value string) map[string]string {
	fields := map[string]string{}
	for _, part := range strings.Split(value, ";") {
		if part == "" {
			continue
		}
		key, fieldValue, ok := strings.Cut(part, "=")
		if !ok {
			fields[part] = ""
			continue
		}
		if key != "" {
			fields[key] = fieldValue
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func parseOSCPercent(parts []string) int {
	if len(parts) < 3 {
		return 0
	}
	percent, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil {
		return 0
	}
	return clampPercent(percent)
}

func parseColonFields(value string) map[string]string {
	fields := map[string]string{}
	for _, part := range strings.Split(value, ":") {
		key, fieldValue, ok := strings.Cut(part, "=")
		if ok && key != "" {
			fields[key] = fieldValue
		}
	}
	return fields
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
