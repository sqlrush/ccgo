package anthropic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func ParseStream(r io.Reader, handle func(StreamEvent) error) error {
	if handle == nil {
		return fmt.Errorf("stream handler is nil")
	}
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var eventName string
	var data bytes.Buffer
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			if data.Len() > 0 {
				if err := dispatchEvent(eventName, data.Bytes(), handle); err != nil {
					return err
				}
				eventName = ""
				data.Reset()
			}
			continue
		}
		switch {
		case bytes.HasPrefix(line, []byte("event:")):
			eventName = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("event:"))))
		case bytes.HasPrefix(line, []byte("data:")):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.Write(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if data.Len() > 0 {
		return dispatchEvent(eventName, data.Bytes(), handle)
	}
	return nil
}

func dispatchEvent(eventName string, data []byte, handle func(StreamEvent) error) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
		return nil
	}
	var event StreamEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}
	if event.Event == "" {
		event.Event = eventName
	}
	if event.Type == "" {
		event.Type = event.Event
	}
	raw := append([]byte(nil), data...)
	event.Raw = raw
	if event.Message != nil {
		event.Message.Raw = raw
	}
	return handle(event)
}
