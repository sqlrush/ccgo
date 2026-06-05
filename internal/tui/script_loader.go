package tui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
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
		var steps []ScriptStep
		if err := json.Unmarshal(data, &steps); err != nil {
			return nil, fmt.Errorf("parse interaction script array: %w", err)
		}
		return steps, nil
	}
	if data[0] == '{' {
		steps, ok, err := parseInteractionScriptObject(data)
		if ok || err != nil {
			return steps, err
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
	var steps []ScriptStep
	if err := json.Unmarshal(value, &steps); err != nil {
		return nil, err
	}
	return steps, nil
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
