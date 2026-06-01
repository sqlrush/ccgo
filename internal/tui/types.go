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

type Dialog struct {
	Title   string
	Body    string
	Actions []string
	Focused int
}

type Frame struct {
	Width      int
	Height     int
	Messages   []Message
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
	KeyHome      KeyType = "home"
	KeyEnd       KeyType = "end"
	KeyEsc       KeyType = "esc"
	KeyCtrlA     KeyType = "ctrl+a"
	KeyCtrlE     KeyType = "ctrl+e"
	KeyCtrlC     KeyType = "ctrl+c"
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
