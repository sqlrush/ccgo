package integrations

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const computerUseDriverPlanFileName = "computer-use-driver-plan.json"

type ComputerUseDriverPlan struct {
	SessionID            contracts.ID `json:"session_id,omitempty"`
	WorkingDirectory     string       `json:"working_directory,omitempty"`
	GeneratedAt          string       `json:"generated_at"`
	ScreenCaptureAdapter Adapter      `json:"screen_capture_adapter"`
	InputControlAdapter  Adapter      `json:"input_control_adapter"`
	ScreenshotFormat     string       `json:"screenshot_format"`
	CoordinateSystem     string       `json:"coordinate_system"`
	ExecutionMode        string       `json:"execution_mode"`
}

func ComputerUseDriverPlanPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), computerUseDriverPlanFileName)
}

func BuildComputerUseDriverPlan(sessionID contracts.ID, cwd string, adapters []Adapter) ComputerUseDriverPlan {
	return ComputerUseDriverPlan{
		SessionID:            sessionID,
		WorkingDirectory:     cwd,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339Nano),
		ScreenCaptureAdapter: selectComputerUseAdapter(adapters, AdapterKindScreenCapture),
		InputControlAdapter:  selectComputerUseAdapter(adapters, AdapterKindInputControl),
		ScreenshotFormat:     "png",
		CoordinateSystem:     "screen_pixels",
		ExecutionMode:        "planned",
	}
}

func WriteComputerUseDriverPlan(path string, plan ComputerUseDriverPlan) error {
	if path == "" {
		return os.ErrInvalid
	}
	if plan.GeneratedAt == "" {
		plan.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if plan.ScreenshotFormat == "" {
		plan.ScreenshotFormat = "png"
	}
	if plan.CoordinateSystem == "" {
		plan.CoordinateSystem = "screen_pixels"
	}
	if plan.ExecutionMode == "" {
		plan.ExecutionMode = "planned"
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadComputerUseDriverPlan(path string) (ComputerUseDriverPlan, error) {
	if path == "" {
		return ComputerUseDriverPlan{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ComputerUseDriverPlan{}, nil
	}
	if err != nil {
		return ComputerUseDriverPlan{}, err
	}
	var plan ComputerUseDriverPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return ComputerUseDriverPlan{}, err
	}
	return plan, nil
}

func selectComputerUseAdapter(adapters []Adapter, kind string) Adapter {
	for _, adapter := range adapters {
		if adapter.Kind == kind && adapter.Available {
			return adapter
		}
	}
	for _, adapter := range adapters {
		if adapter.Kind == kind {
			return adapter
		}
	}
	return Adapter{
		Name:      kind,
		Kind:      kind,
		Available: false,
		Detail:    kind + " command not found in PATH",
	}
}
