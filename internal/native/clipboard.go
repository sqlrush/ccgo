package native

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const clipboardFileName = "native-clipboard.json"

type ClipboardState struct {
	SessionID contracts.ID    `json:"session_id,omitempty"`
	UpdatedAt string          `json:"updated_at"`
	Items     []ClipboardItem `json:"items,omitempty"`
}

type ClipboardItem struct {
	Selection string `json:"selection"`
	Text      string `json:"text"`
	UpdatedAt string `json:"updated_at"`
}

func SessionClipboardPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), clipboardFileName)
}

func EnsureClipboardState(path string, sessionID contracts.ID) error {
	if path == "" {
		return os.ErrInvalid
	}
	state, err := LoadClipboard(path)
	if err != nil {
		return err
	}
	if state.UpdatedAt != "" {
		return nil
	}
	return WriteClipboard(path, ClipboardState{SessionID: sessionID})
}

func WriteClipboardText(path string, sessionID contracts.ID, selection string, text string) (ClipboardState, error) {
	state, err := LoadClipboard(path)
	if err != nil {
		return ClipboardState{}, err
	}
	if state.SessionID == "" {
		state.SessionID = sessionID
	}
	selection = normalizeClipboardSelection(selection)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	item := ClipboardItem{Selection: selection, Text: text, UpdatedAt: now}
	replaced := false
	for i, existing := range state.Items {
		if normalizeClipboardSelection(existing.Selection) == selection {
			state.Items[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		state.Items = append(state.Items, item)
	}
	state.UpdatedAt = now
	if err := WriteClipboard(path, state); err != nil {
		return ClipboardState{}, err
	}
	return state, nil
}

func ReadClipboardText(path string, selection string) (string, bool, error) {
	state, err := LoadClipboard(path)
	if err != nil {
		return "", false, err
	}
	selection = normalizeClipboardSelection(selection)
	for _, item := range state.Items {
		if normalizeClipboardSelection(item.Selection) == selection {
			return item.Text, true, nil
		}
	}
	return "", false, nil
}

func WriteClipboard(path string, state ClipboardState) error {
	if path == "" {
		return os.ErrInvalid
	}
	if state.UpdatedAt == "" {
		state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	for i := range state.Items {
		state.Items[i].Selection = normalizeClipboardSelection(state.Items[i].Selection)
		if state.Items[i].UpdatedAt == "" {
			state.Items[i].UpdatedAt = state.UpdatedAt
		}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o600)
}

func LoadClipboard(path string) (ClipboardState, error) {
	if path == "" {
		return ClipboardState{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ClipboardState{}, nil
	}
	if err != nil {
		return ClipboardState{}, err
	}
	var state ClipboardState
	if err := json.Unmarshal(data, &state); err != nil {
		return ClipboardState{}, err
	}
	for i := range state.Items {
		state.Items[i].Selection = normalizeClipboardSelection(state.Items[i].Selection)
	}
	return state, nil
}

func normalizeClipboardSelection(selection string) string {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return "clipboard"
	}
	return selection
}
