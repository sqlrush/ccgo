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

const voiceCapturePlanFileName = "voice-capture-plan.json"

type VoiceCapturePlan struct {
	SessionID        contracts.ID `json:"session_id,omitempty"`
	WorkingDirectory string       `json:"working_directory,omitempty"`
	GeneratedAt      string       `json:"generated_at"`
	Adapter          Adapter      `json:"adapter"`
	SampleRateHz     int          `json:"sample_rate_hz"`
	Channels         int          `json:"channels"`
	Encoding         string       `json:"encoding"`
	Streaming        bool         `json:"streaming"`
}

func VoiceCapturePlanPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), voiceCapturePlanFileName)
}

func BuildVoiceCapturePlan(sessionID contracts.ID, cwd string, adapters []Adapter) VoiceCapturePlan {
	return VoiceCapturePlan{
		SessionID:        sessionID,
		WorkingDirectory: cwd,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		Adapter:          selectVoiceCaptureAdapter(adapters),
		SampleRateHz:     16000,
		Channels:         1,
		Encoding:         "pcm_s16le",
		Streaming:        true,
	}
}

func WriteVoiceCapturePlan(path string, plan VoiceCapturePlan) error {
	if path == "" {
		return os.ErrInvalid
	}
	if plan.GeneratedAt == "" {
		plan.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if plan.SampleRateHz <= 0 {
		plan.SampleRateHz = 16000
	}
	if plan.Channels <= 0 {
		plan.Channels = 1
	}
	if plan.Encoding == "" {
		plan.Encoding = "pcm_s16le"
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadVoiceCapturePlan(path string) (VoiceCapturePlan, error) {
	if path == "" {
		return VoiceCapturePlan{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return VoiceCapturePlan{}, nil
	}
	if err != nil {
		return VoiceCapturePlan{}, err
	}
	var plan VoiceCapturePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return VoiceCapturePlan{}, err
	}
	return plan, nil
}

func selectVoiceCaptureAdapter(adapters []Adapter) Adapter {
	for _, adapter := range adapters {
		if adapter.Kind == AdapterKindAudioCapture && adapter.Available {
			return adapter
		}
	}
	for _, adapter := range adapters {
		if adapter.Kind == AdapterKindAudioCapture {
			return adapter
		}
	}
	return Adapter{
		Name:      "audio-capture",
		Kind:      AdapterKindAudioCapture,
		Available: false,
		Detail:    "audio capture command not found in PATH",
	}
}
