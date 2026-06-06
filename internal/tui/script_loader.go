package tui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const maxInteractionScriptLineBytes = 50 * 1024 * 1024

// LoadInteractionScript reads an interaction script from a JSON array, wrapper object, or JSONL file.
func LoadInteractionScript(path string) ([]ScriptStep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load interaction script %q: %w", path, err)
	}
	return ParseInteractionScript(data)
}

// ParseInteractionScript parses an interaction script encoded as a JSON array, wrapper object, or newline-delimited JSON objects.
func ParseInteractionScript(data []byte) ([]ScriptStep, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	if data[0] == '[' {
		steps, err := parseInteractionScriptStepsValue(data)
		if err != nil {
			return nil, fmt.Errorf("parse interaction script array: %w", err)
		}
		return steps, nil
	}
	if data[0] == '{' {
		steps, ok, err := parseInteractionScriptObject(data)
		if ok || err != nil {
			return steps, err
		}
		if text, ok := interactionScriptProviderResponseText(data); ok {
			steps, err := ParseInteractionScript([]byte(text))
			if err != nil {
				return nil, fmt.Errorf("parse interaction script provider response: %w", err)
			}
			return steps, nil
		}
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

func parseInteractionScriptObject(data []byte) ([]ScriptStep, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, nil
	}
	for _, name := range []string{
		"steps",
		"script",
		"script_steps",
		"scriptSteps",
		"interaction_script",
		"interactionScript",
		"interaction_steps",
		"interactionSteps",
		"records",
		"recorded_steps",
		"recordedSteps",
		"events",
		"entries",
		"items",
		"included",
		"actions",
		"timeline",
		"tests",
		"test_cases",
		"testCases",
		"cases",
		"scenarios",
		"fixtures",
		"recordings",
		"runs",
		"operations",
		"commands",
		"plays",
		"collection",
		"collections",
		"list",
		"lists",
		"children",
		"values",
		"edges",
	} {
		value, ok := raw[name]
		if !ok {
			continue
		}
		steps, err := parseInteractionScriptStepsValue(value)
		if err != nil {
			return nil, true, fmt.Errorf("parse interaction script object %q: %w", name, err)
		}
		return steps, true, nil
	}
	for _, name := range []string{
		"data",
		"payload",
		"body",
		"result",
		"response",
		"resources",
		"nodes",
		"items",
		"included",
		"collection",
		"collections",
		"list",
		"lists",
		"children",
		"values",
		"edges",
	} {
		value, ok := raw[name]
		if !ok {
			continue
		}
		steps, ok, err := parseInteractionScriptOptionalStepsValue(value)
		if ok || err != nil {
			if err != nil {
				return nil, true, fmt.Errorf("parse interaction script object %q: %w", name, err)
			}
			return steps, true, nil
		}
	}
	for _, name := range []string{
		"scenario",
		"test",
		"case",
		"fixture",
		"interaction",
		"viewer",
		"node",
		"connection",
		"stepConnection",
		"stepsConnection",
		"interactionConnection",
		"recordingConnection",
		"data",
		"payload",
		"body",
		"result",
		"response",
		"resource",
		"attributes",
		"properties",
		"recording",
		"session",
		"run",
	} {
		value, ok := raw[name]
		if !ok {
			continue
		}
		steps, ok, err := parseInteractionScriptNestedObject(value)
		if ok || err != nil {
			if err != nil {
				return nil, true, fmt.Errorf("parse interaction script object %q: %w", name, err)
			}
			return steps, true, nil
		}
	}
	return nil, false, nil
}

func parseInteractionScriptStepsValue(value json.RawMessage) ([]ScriptStep, error) {
	value = bytes.TrimSpace(value)
	if len(value) > 0 && value[0] == '{' {
		steps, ok, err := parseInteractionScriptObject(value)
		if err != nil {
			return nil, err
		}
		if ok {
			return steps, nil
		}
	}
	if len(value) > 0 && value[0] == '[' {
		return parseInteractionScriptArrayValue(value)
	}
	var steps []ScriptStep
	if err := json.Unmarshal(value, &steps); err != nil {
		return nil, err
	}
	return steps, nil
}

func parseInteractionScriptArrayValue(value json.RawMessage) ([]ScriptStep, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err != nil {
		return nil, err
	}
	steps := make([]ScriptStep, 0, len(items))
	for index, item := range items {
		item = bytes.TrimSpace(item)
		if len(item) == 0 || bytes.Equal(item, []byte("null")) {
			continue
		}
		if item[0] == '{' && interactionScriptObjectHasContainer(item) {
			nested, ok, err := parseInteractionScriptObject(item)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", index, err)
			}
			if ok {
				steps = append(steps, nested...)
				continue
			}
		}
		var step ScriptStep
		if err := json.Unmarshal(item, &step); err != nil {
			return nil, fmt.Errorf("item %d: %w", index, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func interactionScriptObjectHasContainer(data []byte) bool {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return false
	}
	for _, name := range []string{
		"steps",
		"script",
		"script_steps",
		"scriptSteps",
		"interaction_script",
		"interactionScript",
		"interaction_steps",
		"interactionSteps",
		"records",
		"recorded_steps",
		"recordedSteps",
		"events",
		"entries",
		"items",
		"included",
		"actions",
		"timeline",
		"tests",
		"test_cases",
		"testCases",
		"cases",
		"scenarios",
		"fixtures",
		"recordings",
		"runs",
		"operations",
		"commands",
		"plays",
	} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) > 0 && (raw[0] == '{' || raw[0] == '[') {
			return true
		}
	}
	if scriptStepJSONHasDirectFields(fields) {
		return false
	}
	for _, name := range []string{
		"scenario",
		"test",
		"case",
		"fixture",
		"interaction",
		"viewer",
		"connection",
		"stepConnection",
		"stepsConnection",
		"interactionConnection",
		"recordingConnection",
		"recording",
		"session",
		"run",
	} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) > 0 && raw[0] == '{' {
			return true
		}
	}
	for _, name := range []string{"data", "payload", "body", "result", "response"} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) > 0 && raw[0] == '{' && interactionScriptObjectHasContainer(raw) {
			return true
		}
	}
	return false
}

func interactionScriptProviderResponseText(data []byte) (string, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return "", false
	}
	if scriptStepJSONHasDirectFields(object) {
		return "", false
	}
	for _, name := range []string{
		"choice",
		"choices",
		"output",
		"outputs",
		"candidate",
		"candidates",
		"generation",
		"generations",
		"completion",
		"completions",
		"response",
		"responses",
		"result",
		"results",
	} {
		value, ok := object[name]
		if !ok {
			continue
		}
		if text, ok := interactionScriptProviderTextFromRaw(value, 0, false); ok {
			if payload, ok := interactionScriptProviderJSONPayload(text); ok {
				return payload, true
			}
			return text, true
		}
	}
	return "", false
}

func interactionScriptProviderTextFromRaw(raw json.RawMessage, depth int, allowScalar bool) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || depth > 8 {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		return text, allowScalar && text != ""
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return "", false
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			part, ok := interactionScriptProviderTextFromRaw(item, depth+1, false)
			if ok {
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			return "", false
		}
		return strings.Join(parts, "\n"), true
	}
	if raw[0] != '{' {
		return "", false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", false
	}
	for _, name := range []string{"text", "content", "value", "output"} {
		value, ok := fields[name]
		if !ok {
			continue
		}
		if text, ok := interactionScriptProviderTextFromRaw(value, depth+1, true); ok {
			return text, true
		}
	}
	for _, name := range []string{"message", "delta", "part", "parts", "candidate", "choice", "generation", "result", "response"} {
		value, ok := fields[name]
		if !ok {
			continue
		}
		if text, ok := interactionScriptProviderTextFromRaw(value, depth+1, false); ok {
			return text, true
		}
	}
	return "", false
}

func interactionScriptProviderJSONPayload(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if interactionScriptProviderLooksJSON(text) {
		return text, true
	}
	start := strings.Index(text, "```")
	if start < 0 {
		return "", false
	}
	afterFence := text[start+3:]
	lineEnd := strings.IndexAny(afterFence, "\r\n")
	if lineEnd < 0 {
		return "", false
	}
	content := strings.TrimLeft(afterFence[lineEnd:], "\r\n")
	end := strings.Index(content, "```")
	if end >= 0 {
		content = content[:end]
	}
	content = strings.TrimSpace(content)
	if interactionScriptProviderLooksJSON(content) {
		return content, true
	}
	return "", false
}

func interactionScriptProviderLooksJSON(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")
}

func parseInteractionScriptOptionalStepsValue(value json.RawMessage) ([]ScriptStep, bool, error) {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil, false, nil
	}
	if value[0] == '[' {
		steps, err := parseInteractionScriptStepsValue(value)
		return steps, true, err
	}
	if value[0] == '{' {
		steps, ok, err := parseInteractionScriptObject(value)
		return steps, ok, err
	}
	return nil, false, nil
}

func parseInteractionScriptNestedObject(value json.RawMessage) ([]ScriptStep, bool, error) {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || value[0] != '{' {
		return nil, false, nil
	}
	return parseInteractionScriptObject(value)
}

func RunInteractionScriptFileChecked(screen *REPLScreen, path string) (ScriptResult, error) {
	steps, err := LoadInteractionScript(path)
	if err != nil {
		return ScriptResult{}, err
	}
	return RunInteractionScriptChecked(screen, steps)
}

func RunDialogRuntimeScriptFileChecked(screen *REPLScreen, runtime *DialogRuntime, baseStatus string, path string) (RuntimeScriptResult, error) {
	steps, err := LoadInteractionScript(path)
	if err != nil {
		return RuntimeScriptResult{}, err
	}
	return RunDialogRuntimeScriptChecked(screen, runtime, baseStatus, steps)
}

// RunInteractionScriptFileWithSnapshotCorpus runs a script file and compares captured snapshots against a corpus.
func RunInteractionScriptFileWithSnapshotCorpus(screen *REPLScreen, path string, corpus SnapshotCorpus, strict bool) (ScriptResult, SnapshotCorpusReport, error) {
	result, err := RunInteractionScriptFileChecked(screen, path)
	if err != nil {
		return result, SnapshotCorpusReport{}, err
	}
	report, err := compareScriptSnapshots(corpus, result.Snapshots, strict)
	return result, report, err
}

// RunDialogRuntimeScriptFileWithSnapshotCorpus runs a runtime script file and compares captured snapshots against a corpus.
func RunDialogRuntimeScriptFileWithSnapshotCorpus(screen *REPLScreen, runtime *DialogRuntime, baseStatus string, path string, corpus SnapshotCorpus, strict bool) (RuntimeScriptResult, SnapshotCorpusReport, error) {
	result, err := RunDialogRuntimeScriptFileChecked(screen, runtime, baseStatus, path)
	if err != nil {
		return result, SnapshotCorpusReport{}, err
	}
	report, err := compareScriptSnapshots(corpus, result.Snapshots, strict)
	return result, report, err
}

func compareScriptSnapshots(corpus SnapshotCorpus, snapshots []ANSISnapshot, strict bool) (SnapshotCorpusReport, error) {
	if strict {
		return corpus.CompareAllStrict(snapshots)
	}
	comparisons, err := corpus.CompareAll(snapshots)
	if err != nil {
		return SnapshotCorpusReport{}, err
	}
	return SnapshotCorpusReport{Comparisons: comparisons}, nil
}
