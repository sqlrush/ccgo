package integrations

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultVoiceCaptureDuration       = 5 * time.Second
	defaultVoiceCaptureMaxBytes       = 5 * 1024 * 1024
	defaultVoiceTranscriptionTimeout  = 30 * time.Second
	defaultVoiceTranscriptionMaxBytes = 256 * 1024
)

type VoiceCommandRunner func(ctx context.Context, command []string, maxBytes int64) ([]byte, bool, error)
type VoiceTranscriptionRunner func(ctx context.Context, command []string, audio []byte, maxBytes int64) (string, bool, error)

type VoiceCaptureOptions struct {
	Duration time.Duration
	MaxBytes int64
	Runner   VoiceCommandRunner
}

type VoiceTranscriptionOptions struct {
	Command  []string
	Timeout  time.Duration
	MaxBytes int64
	Runner   VoiceTranscriptionRunner
}

type VoiceCaptureResult struct {
	AdapterName  string   `json:"adapter_name,omitempty"`
	AdapterKind  string   `json:"adapter_kind,omitempty"`
	Command      []string `json:"command,omitempty"`
	SampleRateHz int      `json:"sample_rate_hz"`
	Channels     int      `json:"channels"`
	Encoding     string   `json:"encoding"`
	Bytes        int      `json:"bytes"`
	Truncated    bool     `json:"truncated,omitempty"`
	Skipped      bool     `json:"skipped,omitempty"`
	Detail       string   `json:"detail,omitempty"`
	Audio        []byte   `json:"-"`
}

type VoiceTranscriptionResult struct {
	Command    []string `json:"command,omitempty"`
	AudioBytes int      `json:"audio_bytes"`
	Transcript string   `json:"transcript,omitempty"`
	Truncated  bool     `json:"truncated,omitempty"`
	Skipped    bool     `json:"skipped,omitempty"`
	Detail     string   `json:"detail,omitempty"`
}

func CaptureVoiceAudio(ctx context.Context, plan VoiceCapturePlan, options VoiceCaptureOptions) (VoiceCaptureResult, error) {
	result := VoiceCaptureResult{
		AdapterName:  plan.Adapter.Name,
		AdapterKind:  plan.Adapter.Kind,
		Command:      append([]string(nil), plan.Adapter.Command...),
		SampleRateHz: plan.SampleRateHz,
		Channels:     plan.Channels,
		Encoding:     plan.Encoding,
	}
	if result.SampleRateHz <= 0 {
		result.SampleRateHz = 16000
	}
	if result.Channels <= 0 {
		result.Channels = 1
	}
	if result.Encoding == "" {
		result.Encoding = "pcm_s16le"
	}
	if !plan.Adapter.Available || len(plan.Adapter.Command) == 0 {
		result.Skipped = true
		result.Detail = "no executable audio capture adapter is available"
		return result, nil
	}
	runner := options.Runner
	if runner == nil {
		runner = DefaultVoiceCommandRunner
	}
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultVoiceCaptureMaxBytes
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		duration := options.Duration
		if duration <= 0 {
			duration = defaultVoiceCaptureDuration
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, duration)
		defer cancel()
	}
	audio, truncated, err := runner(ctx, plan.Adapter.Command, maxBytes)
	result.Audio = audio
	result.Bytes = len(audio)
	result.Truncated = truncated
	if err != nil {
		result.Detail = err.Error()
		return result, err
	}
	return result, nil
}

func DefaultVoiceCommandRunner(ctx context.Context, command []string, maxBytes int64) ([]byte, bool, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return nil, false, os.ErrInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if maxBytes <= 0 {
		maxBytes = defaultVoiceCaptureMaxBytes
	}
	stdout := &limitedBuffer{max: int(maxBytes)}
	stderr := &limitedBuffer{max: 64 * 1024}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil && stdout.Len() > 0 {
			return append([]byte(nil), stdout.Bytes()...), stdout.truncated, nil
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return append([]byte(nil), stdout.Bytes()...), stdout.truncated, fmt.Errorf("%w: %s", err, detail)
		}
		return append([]byte(nil), stdout.Bytes()...), stdout.truncated, err
	}
	return append([]byte(nil), stdout.Bytes()...), stdout.truncated, nil
}

func TranscribeVoiceAudio(ctx context.Context, audio []byte, options VoiceTranscriptionOptions) (VoiceTranscriptionResult, error) {
	result := VoiceTranscriptionResult{
		Command:    append([]string(nil), options.Command...),
		AudioBytes: len(audio),
	}
	if len(audio) == 0 {
		result.Skipped = true
		result.Detail = "no captured audio is available"
		return result, nil
	}
	if len(options.Command) == 0 || strings.TrimSpace(options.Command[0]) == "" {
		result.Skipped = true
		result.Detail = "voice transcription command is not configured"
		return result, nil
	}
	runner := options.Runner
	if runner == nil {
		runner = DefaultVoiceTranscriptionRunner
	}
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultVoiceTranscriptionMaxBytes
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		timeout := options.Timeout
		if timeout <= 0 {
			timeout = defaultVoiceTranscriptionTimeout
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	transcript, truncated, err := runner(ctx, options.Command, audio, maxBytes)
	result.Transcript = strings.TrimSpace(transcript)
	result.Truncated = truncated
	if err != nil {
		result.Detail = err.Error()
		return result, err
	}
	return result, nil
}

func DefaultVoiceTranscriptionRunner(ctx context.Context, command []string, audio []byte, maxBytes int64) (string, bool, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return "", false, os.ErrInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if maxBytes <= 0 {
		maxBytes = defaultVoiceTranscriptionMaxBytes
	}
	stdout := &limitedBuffer{max: int(maxBytes)}
	stderr := &limitedBuffer{max: 64 * 1024}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin = bytes.NewReader(audio)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return strings.TrimSpace(stdout.String()), stdout.truncated, fmt.Errorf("%w: %s", err, detail)
		}
		return strings.TrimSpace(stdout.String()), stdout.truncated, err
	}
	return strings.TrimSpace(stdout.String()), stdout.truncated, nil
}

func VoiceTranscriptionCommandFromEnv(env func(string) string) []string {
	if env == nil {
		env = os.Getenv
	}
	raw := strings.TrimSpace(env("CLAUDE_VOICE_TRANSCRIBE_COMMAND"))
	if raw == "" {
		return nil
	}
	return strings.Fields(raw)
}

type limitedBuffer struct {
	bytes.Buffer
	max       int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.max <= 0 {
		_, _ = b.Buffer.Write(p)
		return len(p), nil
	}
	remaining := b.max - b.Buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	_, _ = b.Buffer.Write(p)
	return len(p), nil
}
