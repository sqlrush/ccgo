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
	KeyRune             KeyType = "rune"
	KeyEnter            KeyType = "enter"
	KeyShiftEnter       KeyType = "shift+enter"
	KeyBackspace        KeyType = "backspace"
	KeyDelete           KeyType = "delete"
	KeyLeft             KeyType = "left"
	KeyRight            KeyType = "right"
	KeyCtrlLeft         KeyType = "ctrl+left"
	KeyCtrlRight        KeyType = "ctrl+right"
	KeyUp               KeyType = "up"
	KeyDown             KeyType = "down"
	KeyPageUp           KeyType = "pageup"
	KeyPageDown         KeyType = "pagedown"
	KeyHome             KeyType = "home"
	KeyEnd              KeyType = "end"
	KeyTab              KeyType = "tab"
	KeyShiftTab         KeyType = "shift+tab"
	KeyEsc              KeyType = "esc"
	KeyF1               KeyType = "f1"
	KeyF2               KeyType = "f2"
	KeyF3               KeyType = "f3"
	KeyF4               KeyType = "f4"
	KeyF5               KeyType = "f5"
	KeyF6               KeyType = "f6"
	KeyF7               KeyType = "f7"
	KeyF8               KeyType = "f8"
	KeyF9               KeyType = "f9"
	KeyF10              KeyType = "f10"
	KeyF11              KeyType = "f11"
	KeyF12              KeyType = "f12"
	KeyF13              KeyType = "f13"
	KeyF14              KeyType = "f14"
	KeyF15              KeyType = "f15"
	KeyF16              KeyType = "f16"
	KeyF17              KeyType = "f17"
	KeyF18              KeyType = "f18"
	KeyF19              KeyType = "f19"
	KeyF20              KeyType = "f20"
	KeyAltLeft          KeyType = "alt+left"
	KeyAltRight         KeyType = "alt+right"
	KeyAltB             KeyType = "alt+b"
	KeyAltD             KeyType = "alt+d"
	KeyAltF             KeyType = "alt+f"
	KeyAltY             KeyType = "alt+y"
	KeyAltBS            KeyType = "alt+backspace"
	KeyCtrlA            KeyType = "ctrl+a"
	KeyCtrlB            KeyType = "ctrl+b"
	KeyCtrlC            KeyType = "ctrl+c"
	KeyCtrlD            KeyType = "ctrl+d"
	KeyCtrlE            KeyType = "ctrl+e"
	KeyCtrlF            KeyType = "ctrl+f"
	KeyCtrlG            KeyType = "ctrl+g"
	KeyCtrlK            KeyType = "ctrl+k"
	KeyCtrlL            KeyType = "ctrl+l"
	KeyCtrlN            KeyType = "ctrl+n"
	KeyCtrlO            KeyType = "ctrl+o"
	KeyCtrlP            KeyType = "ctrl+p"
	KeyCtrlQ            KeyType = "ctrl+q"
	KeyCtrlR            KeyType = "ctrl+r"
	KeyCtrlS            KeyType = "ctrl+s"
	KeyCtrlT            KeyType = "ctrl+t"
	KeyCtrlU            KeyType = "ctrl+u"
	KeyCtrlV            KeyType = "ctrl+v"
	KeyCtrlW            KeyType = "ctrl+w"
	KeyCtrlX            KeyType = "ctrl+x"
	KeyCtrlY            KeyType = "ctrl+y"
	KeyCtrlZ            KeyType = "ctrl+z"
	KeyCtrlSpace        KeyType = "ctrl+space"
	KeyCtrlBackslash    KeyType = "ctrl+\\"
	KeyCtrlRightBracket KeyType = "ctrl+]"
	KeyCtrlCaret        KeyType = "ctrl+^"
	KeyCtrlUnderscore   KeyType = "ctrl+_"
	KeyPaste            KeyType = "paste"
	KeyImageHint        KeyType = "image_hint"
	KeyMouse            KeyType = "mouse"
	KeyFocusIn          KeyType = "focus_in"
	KeyFocusOut         KeyType = "focus_out"
	KeyUnknown          KeyType = "unknown"
)

func functionKeyType(number int) (KeyType, bool) {
	switch number {
	case 1:
		return KeyF1, true
	case 2:
		return KeyF2, true
	case 3:
		return KeyF3, true
	case 4:
		return KeyF4, true
	case 5:
		return KeyF5, true
	case 6:
		return KeyF6, true
	case 7:
		return KeyF7, true
	case 8:
		return KeyF8, true
	case 9:
		return KeyF9, true
	case 10:
		return KeyF10, true
	case 11:
		return KeyF11, true
	case 12:
		return KeyF12, true
	case 13:
		return KeyF13, true
	case 14:
		return KeyF14, true
	case 15:
		return KeyF15, true
	case 16:
		return KeyF16, true
	case 17:
		return KeyF17, true
	case 18:
		return KeyF18, true
	case 19:
		return KeyF19, true
	case 20:
		return KeyF20, true
	default:
		return KeyUnknown, false
	}
}

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
