package tui

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

type Message struct {
	Role Role
	Text string
}

type DialogKind string

const (
	DialogGeneric    DialogKind = ""
	DialogPermission DialogKind = "permission"
	DialogTask       DialogKind = "task"
)

type Dialog struct {
	Title   string
	Body    string
	Actions []string
	Focused int
	ID      string
	Kind    DialogKind
}

type Frame struct {
	Width      int
	Height     int
	Messages   []Message
	BodyLines  []string
	Status     string
	Prompt     PromptState
	Dialog     *Dialog
	ShowCursor bool
}

type KeyType string

const (
	KeyRune      KeyType = "rune"
	KeyEnter     KeyType = "enter"
	KeyBackspace KeyType = "backspace"
	KeyDelete    KeyType = "delete"
	KeyLeft      KeyType = "left"
	KeyRight     KeyType = "right"
	KeyUp        KeyType = "up"
	KeyDown      KeyType = "down"
	KeyPageUp    KeyType = "pageup"
	KeyPageDown  KeyType = "pagedown"
	KeyHome      KeyType = "home"
	KeyEnd       KeyType = "end"
	KeyTab       KeyType = "tab"
	KeyShiftTab  KeyType = "shift+tab"
	KeyEsc       KeyType = "esc"
	KeyCtrlA     KeyType = "ctrl+a"
	KeyCtrlE     KeyType = "ctrl+e"
	KeyCtrlC     KeyType = "ctrl+c"
	KeyCtrlR     KeyType = "ctrl+r"
	KeyUnknown   KeyType = "unknown"
)

type Key struct {
	Type KeyType
	Rune rune
}

type PromptResult struct {
	Submitted   string
	Cancelled   bool
	Interrupted bool
}
