package anthropic

import (
	"encoding/json"

	"ccgo/internal/contracts"
)

type StreamAccumulator struct {
	Response Response
	usage    contracts.Usage
	jsonBuf  map[int]string
}

func NewStreamAccumulator() *StreamAccumulator {
	return &StreamAccumulator{jsonBuf: map[int]string{}}
}

func (a *StreamAccumulator) Add(event StreamEvent) error {
	switch event.Type {
	case "message_start":
		if event.Message != nil {
			a.Response = *event.Message
			a.usage = event.Message.Usage
		}
	case "content_block_start":
		if event.ContentBlock != nil {
			a.ensureIndex(event.Index)
			a.Response.Content[event.Index] = *event.ContentBlock
		}
	case "content_block_delta":
		a.ensureIndex(event.Index)
		block := a.Response.Content[event.Index]
		switch event.Delta["type"] {
		case "text_delta":
			if text, ok := event.Delta["text"].(string); ok {
				block.Text += text
			}
		case "thinking_delta":
			if text, ok := event.Delta["thinking"].(string); ok {
				block.Text += text
			}
		case "signature_delta":
			if sig, ok := event.Delta["signature"].(string); ok {
				block.Signature = sig
			}
		case "input_json_delta":
			if partial, ok := event.Delta["partial_json"].(string); ok {
				a.jsonBuf[event.Index] += partial
			}
		}
		a.Response.Content[event.Index] = block
	case "content_block_stop":
		if partial := a.jsonBuf[event.Index]; partial != "" {
			a.ensureIndex(event.Index)
			block := a.Response.Content[event.Index]
			block.Input = json.RawMessage(partial)
			a.Response.Content[event.Index] = block
		}
	case "message_delta":
		if event.Delta != nil {
			if stop, ok := event.Delta["stop_reason"].(string); ok {
				a.Response.StopReason = stop
			}
			if stop, ok := event.Delta["stop_sequence"].(string); ok {
				a.Response.StopSequence = stop
			}
		}
		if event.Usage != nil {
			a.usage = UpdateUsage(a.usage, *event.Usage)
		}
	}
	return nil
}

func (a *StreamAccumulator) Finish() *Response {
	a.Response.Usage = a.usage
	if a.Response.Model != "" {
		a.Response.Usage = UsageWithCost(a.Response.Model, a.Response.Usage)
	}
	return &a.Response
}

func (a *StreamAccumulator) ensureIndex(index int) {
	for len(a.Response.Content) <= index {
		a.Response.Content = append(a.Response.Content, contracts.ContentBlock{})
	}
}
