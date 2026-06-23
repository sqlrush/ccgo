package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/contracts"
)

// ForkResult is the result of a successful ForkSession call.
type ForkResult struct {
	// SessionID is the ID of the newly-created fork session.
	SessionID contracts.ID
	// Title is the effective custom title written to the fork transcript.
	Title string
	// TranscriptPath is the absolute path to the new fork transcript file.
	TranscriptPath string
}

// ForkSession creates a fork of the session identified by srcID rooted at root
// (the project working directory, same root used by TranscriptPath).
//
// Semantics mirror CC's branch.ts createFork (src/commands/branch/branch.ts):
//   - Reads all non-sidechain messages from the source transcript.
//   - Rewrites sessionId to the new fork ID in every message.
//   - Preserves all other metadata (timestamps, parentUuid, etc.).
//   - Appends a custom-title metadata record to the fork file.
//   - Returns an error when the source transcript is missing or empty.
//
// The fork title is derived as: "<baseName> (Branch)" where baseName is
// customTitle when non-empty, or the first user message text (≤100 runes),
// or "Branched conversation" as a fallback.
//
// SECURITY: the fork file is written 0o600 (owner-only), matching CC.
func ForkSession(srcID contracts.ID, root string, customTitle string) (ForkResult, error) {
	srcPath := TranscriptPath(root, srcID)

	data, err := os.ReadFile(srcPath)
	if err != nil {
		return ForkResult{}, fmt.Errorf("fork: read source transcript: %w", err)
	}
	if len(data) == 0 {
		return ForkResult{}, fmt.Errorf("fork: source transcript is empty — nothing to branch")
	}

	// Collect non-sidechain lines and track first user message text for title.
	type parsedLine struct {
		raw     json.RawMessage
		msgType string
		text    string // first text in user messages; used for title derivation
	}
	var lines []parsedLine
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 50*1024*1024)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var env struct {
			Type        string       `json:"type"`
			SessionID   contracts.ID `json:"sessionId"`
			IsSidechain bool         `json:"isSidechain"`
			Content     any          `json:"content"`
			Message     *struct {
				Content any `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			continue // tolerate malformed lines
		}
		if env.IsSidechain {
			continue
		}
		text := ""
		if env.Type == "user" {
			text = extractFirstText(env.Content)
			if text == "" && env.Message != nil {
				text = extractFirstText(env.Message.Content)
			}
		}
		lines = append(lines, parsedLine{
			raw:     append(json.RawMessage(nil), raw...),
			msgType: env.Type,
			text:    text,
		})
	}
	if err := scanner.Err(); err != nil {
		return ForkResult{}, fmt.Errorf("fork: scan source transcript: %w", err)
	}
	if len(lines) == 0 {
		return ForkResult{}, fmt.Errorf("fork: no messages to branch")
	}

	forkID := contracts.NewID()

	// Rewrite sessionId in all lines.
	rewritten := make([][]byte, 0, len(lines)+1)
	for _, l := range lines {
		out, err := rewriteSessionID(l.raw, srcID, forkID)
		if err != nil {
			return ForkResult{}, fmt.Errorf("fork: rewrite sessionId: %w", err)
		}
		rewritten = append(rewritten, out)
	}

	// Derive fork title.
	firstUserText := ""
	for _, l := range lines {
		if l.msgType == "user" && l.text != "" {
			firstUserText = l.text
			break
		}
	}
	baseName := strings.TrimSpace(customTitle)
	if baseName == "" {
		baseName = strings.TrimSpace(firstUserText)
	}
	if baseName == "" {
		baseName = "Branched conversation"
	}
	runes := []rune(baseName)
	if len(runes) > 100 {
		baseName = string(runes[:100])
	}
	title := baseName + " (Branch)"

	// Append custom-title metadata record.
	titleRecord := struct {
		Type        string       `json:"type"`
		SessionID   contracts.ID `json:"sessionId"`
		CustomTitle string       `json:"customTitle"`
	}{Type: "custom-title", SessionID: forkID, CustomTitle: title}
	titleLine, err := json.Marshal(titleRecord)
	if err != nil {
		return ForkResult{}, fmt.Errorf("fork: marshal custom-title: %w", err)
	}
	rewritten = append(rewritten, titleLine)

	// Write fork transcript.
	forkPath := TranscriptPath(root, forkID)
	if err := os.MkdirAll(filepath.Dir(forkPath), 0o700); err != nil {
		return ForkResult{}, fmt.Errorf("fork: create project dir: %w", err)
	}
	var out []byte
	for _, line := range rewritten {
		out = append(out, line...)
		out = append(out, '\n')
	}
	if err := os.WriteFile(forkPath, out, 0o600); err != nil {
		return ForkResult{}, fmt.Errorf("fork: write fork transcript: %w", err)
	}

	return ForkResult{
		SessionID:      forkID,
		Title:          title,
		TranscriptPath: forkPath,
	}, nil
}

// rewriteSessionID replaces the "sessionId" field value from oldID to newID
// in a raw JSON object. All other fields are preserved.
func rewriteSessionID(raw json.RawMessage, oldID, newID contracts.ID) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw, nil // pass through non-objects
	}
	if sid, ok := obj["sessionId"]; ok {
		var id contracts.ID
		if err := json.Unmarshal(sid, &id); err == nil && id == oldID {
			newSID, err := json.Marshal(newID)
			if err != nil {
				return nil, fmt.Errorf("marshal newID: %w", err)
			}
			obj["sessionId"] = newSID
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("marshal rewritten line: %w", err)
	}
	return out, nil
}

// extractFirstText returns the first text string found in a content field.
// content may be a plain string or []any of content blocks.
func extractFirstText(content any) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return strings.Join(strings.Fields(v), " ")
	case []any:
		for _, block := range v {
			m, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); t == "text" {
				if text, _ := m["text"].(string); text != "" {
					return strings.Join(strings.Fields(text), " ")
				}
			}
		}
	}
	return ""
}
