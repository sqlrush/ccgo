package tui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

const maxInteractionScriptLineBytes = 4 * 1024 * 1024

// LoadInteractionScript reads an interaction script from a JSON array or JSONL file.
func LoadInteractionScript(path string) ([]ScriptStep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load interaction script %q: %w", path, err)
	}
	return ParseInteractionScript(data)
}

// ParseInteractionScript parses an interaction script encoded as a JSON array or newline-delimited JSON objects.
func ParseInteractionScript(data []byte) ([]ScriptStep, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	if data[0] == '[' {
		var steps []ScriptStep
		if err := json.Unmarshal(data, &steps); err != nil {
			return nil, fmt.Errorf("parse interaction script array: %w", err)
		}
		return steps, nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), maxInteractionScriptLineBytes)
	var steps []ScriptStep
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var step ScriptStep
		if err := json.Unmarshal(line, &step); err != nil {
			return nil, fmt.Errorf("parse interaction script line %d: %w", lineNumber, err)
		}
		steps = append(steps, step)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse interaction script line %d: %w", lineNumber+1, err)
	}
	return steps, nil
}
