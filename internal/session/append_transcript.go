package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"ccgo/internal/platform"
)

func AppendTranscriptMessage(path string, message TranscriptMessage) error {
	if message.Timestamp == "" {
		message.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := platform.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = f.Write(append(encoded, '\n'))
	return err
}
