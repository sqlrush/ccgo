package tui

import "ccgo/internal/session"

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
	Width         int
	Height        int
	Messages      []Message
	BodyLines     []string
	Status        string
	Prompt        PromptState
	Dialog        *Dialog
	ReverseSearch *ReverseSearchState
	ShowCursor    bool
}

type ReverseSearchState struct {
	Active  bool
	Query   string
	Results []string
	Focused int
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
	KeyCtrlB     KeyType = "ctrl+b"
	KeyCtrlC     KeyType = "ctrl+c"
	KeyCtrlD     KeyType = "ctrl+d"
	KeyCtrlE     KeyType = "ctrl+e"
	KeyCtrlF     KeyType = "ctrl+f"
	KeyCtrlG     KeyType = "ctrl+g"
	KeyCtrlK     KeyType = "ctrl+k"
	KeyCtrlL     KeyType = "ctrl+l"
	KeyCtrlO     KeyType = "ctrl+o"
	KeyCtrlR     KeyType = "ctrl+r"
	KeyCtrlS     KeyType = "ctrl+s"
	KeyCtrlT     KeyType = "ctrl+t"
	KeyCtrlU     KeyType = "ctrl+u"
	KeyCtrlW     KeyType = "ctrl+w"
	KeyCtrlX     KeyType = "ctrl+x"
	KeyCtrlY     KeyType = "ctrl+y"
	KeyPaste     KeyType = "paste"
	KeyImageHint KeyType = "image_hint"
	KeyMouse     KeyType = "mouse"
	KeyFocusIn   KeyType = "focus_in"
	KeyFocusOut  KeyType = "focus_out"
	KeyUnknown   KeyType = "unknown"
)

type Key struct {
	Type         KeyType
	Rune         rune
	Text         string
	MouseButton  int
	MouseX       int
	MouseY       int
	MouseRelease bool
}

type PromptResult struct {
	Submitted      string
	Display        string
	PastedContents map[int]session.PastedContent
	Cancelled      bool
	Interrupted    bool
}
