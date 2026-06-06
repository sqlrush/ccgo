package tui

import (
	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

type Message struct {
	Role           Role
	Text           string
	ContentBlocks  []contracts.ContentBlock
	ImagePasteIDs  []int
	PastedContents map[int]session.PastedContent
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
	Active        bool
	Query         string
	Cursor        int
	Results       []string
	ResultEntries []session.HistoryEntry
	Focused       int
}

type KeyType string

const (
	KeyRune       KeyType = "rune"
	KeyEnter      KeyType = "enter"
	KeyShiftEnter KeyType = "shift+enter"
	KeyBackspace  KeyType = "backspace"
	KeyDelete     KeyType = "delete"
	KeyLeft       KeyType = "left"
	KeyRight      KeyType = "right"
	KeyCtrlLeft   KeyType = "ctrl+left"
	KeyCtrlRight  KeyType = "ctrl+right"
	KeyUp         KeyType = "up"
	KeyDown       KeyType = "down"
	KeyPageUp     KeyType = "pageup"
	KeyPageDown   KeyType = "pagedown"
	KeyHome       KeyType = "home"
	KeyEnd        KeyType = "end"
	KeyTab        KeyType = "tab"
	KeyShiftTab   KeyType = "shift+tab"
	KeyEsc        KeyType = "esc"
	KeyAltLeft    KeyType = "alt+left"
	KeyAltRight   KeyType = "alt+right"
	KeyAltB       KeyType = "alt+b"
	KeyAltD       KeyType = "alt+d"
	KeyAltF       KeyType = "alt+f"
	KeyAltY       KeyType = "alt+y"
	KeyAltBS      KeyType = "alt+backspace"
	KeyCtrlA      KeyType = "ctrl+a"
	KeyCtrlB      KeyType = "ctrl+b"
	KeyCtrlC      KeyType = "ctrl+c"
	KeyCtrlD      KeyType = "ctrl+d"
	KeyCtrlE      KeyType = "ctrl+e"
	KeyCtrlF      KeyType = "ctrl+f"
	KeyCtrlG      KeyType = "ctrl+g"
	KeyCtrlK      KeyType = "ctrl+k"
	KeyCtrlL      KeyType = "ctrl+l"
	KeyCtrlN      KeyType = "ctrl+n"
	KeyCtrlO      KeyType = "ctrl+o"
	KeyCtrlP      KeyType = "ctrl+p"
	KeyCtrlQ      KeyType = "ctrl+q"
	KeyCtrlR      KeyType = "ctrl+r"
	KeyCtrlS      KeyType = "ctrl+s"
	KeyCtrlT      KeyType = "ctrl+t"
	KeyCtrlU      KeyType = "ctrl+u"
	KeyCtrlV      KeyType = "ctrl+v"
	KeyCtrlW      KeyType = "ctrl+w"
	KeyCtrlX      KeyType = "ctrl+x"
	KeyCtrlY      KeyType = "ctrl+y"
	KeyCtrlZ      KeyType = "ctrl+z"
	KeyPaste      KeyType = "paste"
	KeyImageHint  KeyType = "image_hint"
	KeyMouse      KeyType = "mouse"
	KeyFocusIn    KeyType = "focus_in"
	KeyFocusOut   KeyType = "focus_out"
	KeyUnknown    KeyType = "unknown"
)

type Key struct {
	Type         KeyType
	Rune         rune
	Text         string
	Content      string
	MediaType    string
	Filename     string
	Dimensions   *session.ImageDimensions
	SourcePath   string
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

type PromptStash struct {
	Text           string
	Cursor         int
	PastedContents map[int]session.PastedContent
}
