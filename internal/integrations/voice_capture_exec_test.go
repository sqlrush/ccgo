package integrations

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCaptureVoiceAudioRunsAdapterCommand(t *testing.T) {
	var gotCommand []string
	var gotMaxBytes int64
	plan := BuildVoiceCapturePlan("sess_voice", "/work", []Adapter{{
		Name:      "pw-record",
		Kind:      AdapterKindAudioCapture,
		Available: true,
		Command:   []string{"/usr/bin/pw-record", "--target", "default", "-"},
	}})
	result, err := CaptureVoiceAudio(context.Background(), plan, VoiceCaptureOptions{
		Duration: 10 * time.Millisecond,
		MaxBytes: 64,
		Runner: func(ctx context.Context, command []string, maxBytes int64) ([]byte, bool, error) {
			gotCommand = append([]string(nil), command...)
			gotMaxBytes = maxBytes
			return []byte{1, 2, 3, 4}, false, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AdapterName != "pw-record" || result.AdapterKind != AdapterKindAudioCapture || result.Bytes != 4 || result.Truncated {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Audio) != 4 || result.SampleRateHz != 16000 || result.Channels != 1 || result.Encoding != "pcm_s16le" {
		t.Fatalf("audio result = %#v", result)
	}
	if len(gotCommand) != 4 || gotCommand[0] != "/usr/bin/pw-record" || gotMaxBytes != 64 {
		t.Fatalf("command = %#v max=%d", gotCommand, gotMaxBytes)
	}
}

func TestCaptureVoiceAudioSkipsUnavailableAdapter(t *testing.T) {
	result, err := CaptureVoiceAudio(context.Background(), BuildVoiceCapturePlan("sess_voice", "/work", nil), VoiceCaptureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped || result.Bytes != 0 || result.Detail == "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCaptureVoiceAudioReturnsRunnerErrors(t *testing.T) {
	wantErr := errors.New("capture failed")
	plan := BuildVoiceCapturePlan("sess_voice", "/work", []Adapter{{
		Name:      "arecord",
		Kind:      AdapterKindAudioCapture,
		Available: true,
		Command:   []string{"/usr/bin/arecord", "-"},
	}})
	result, err := CaptureVoiceAudio(context.Background(), plan, VoiceCaptureOptions{
		Runner: func(context.Context, []string, int64) ([]byte, bool, error) {
			return []byte{1, 2}, true, wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if result.Bytes != 2 || !result.Truncated || result.Detail == "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestLimitedBufferTruncatesWithoutShortWrite(t *testing.T) {
	var buf limitedBuffer
	buf.max = 3
	n, err := buf.Write([]byte("abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 || buf.String() != "abc" || !buf.truncated {
		t.Fatalf("n=%d buf=%q truncated=%v", n, buf.String(), buf.truncated)
	}
}
