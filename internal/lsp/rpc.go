package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const defaultFrameLimit int64 = 8 << 20

var ErrFrameTooLarge = errors.New("lsp frame exceeds limit")

type StreamProcessResult struct {
	Messages            int            `json:"messages"`
	InitializeResponses int            `json:"initialize_responses,omitempty"`
	ServerCapabilities  map[string]any `json:"server_capabilities,omitempty"`
	DiagnosticsUpdates  int            `json:"diagnostics_updates"`
	LastSnapshot        []Diagnostic   `json:"last_snapshot,omitempty"`
}

func WriteFramedMessage(w io.Writer, payload []byte) error {
	if w == nil {
		return os.ErrInvalid
	}
	if payload == nil {
		payload = []byte("{}")
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func ReadFramedMessage(r *bufio.Reader, limit int64) ([]byte, error) {
	if r == nil {
		return nil, os.ErrInvalid
	}
	if limit <= 0 {
		limit = defaultFrameLimit
	}
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
				return nil, io.EOF
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || parsed < 0 {
				return nil, fmt.Errorf("invalid LSP Content-Length %q", strings.TrimSpace(value))
			}
			contentLength = parsed
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("LSP frame missing Content-Length header")
	}
	if int64(contentLength) > limit {
		return nil, ErrFrameTooLarge
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func ProcessDiagnosticsStream(ctx context.Context, reader io.Reader, snapshotPath string) (StreamProcessResult, error) {
	return ProcessDiagnosticsStreamLimit(ctx, reader, snapshotPath, defaultFrameLimit)
}

func ProcessDiagnosticsStreamLimit(ctx context.Context, reader io.Reader, snapshotPath string, limit int64) (StreamProcessResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if reader == nil || snapshotPath == "" {
		return StreamProcessResult{}, os.ErrInvalid
	}
	buffered, ok := reader.(*bufio.Reader)
	if !ok {
		buffered = bufio.NewReader(reader)
	}
	var result StreamProcessResult
	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		payload, err := ReadFramedMessage(buffered, limit)
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return result, err
		}
		result.Messages++
		capabilities, ok, err := initializeCapabilitiesPayload(payload)
		if err != nil {
			return result, err
		}
		if ok {
			result.InitializeResponses++
			result.ServerCapabilities = capabilities
			continue
		}
		updated, ok, err := applyPublishDiagnosticsPayload(snapshotPath, payload)
		if err != nil {
			return result, err
		}
		if !ok {
			continue
		}
		result.DiagnosticsUpdates++
		result.LastSnapshot = updated
	}
}

func applyPublishDiagnosticsPayload(snapshotPath string, payload []byte) ([]Diagnostic, bool, error) {
	var envelope struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, false, err
	}
	if envelope.Method != "textDocument/publishDiagnostics" {
		return nil, false, nil
	}
	updated, err := ApplyPublishDiagnosticsSnapshot(snapshotPath, payload)
	if err != nil {
		return nil, true, err
	}
	return updated, true, nil
}

func initializeCapabilitiesPayload(payload []byte) (map[string]any, bool, error) {
	var envelope struct {
		ID     any             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  any             `json:"error"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, false, err
	}
	if envelope.ID == nil || len(envelope.Result) == 0 || envelope.Error != nil {
		return nil, false, nil
	}
	var result struct {
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return nil, false, err
	}
	if len(result.Capabilities) == 0 {
		return nil, false, nil
	}
	return result.Capabilities, true, nil
}

func EncodeFramedJSON(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := WriteFramedMessage(&out, payload); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
