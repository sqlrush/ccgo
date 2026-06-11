package contracts

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMessageUnmarshalAcceptsStringContent(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"User","id":"m1","content":"hello"}`), &message); err != nil {
		t.Fatal(err)
	}
	if message.Type != MessageUser || message.ID != "m1" {
		t.Fatalf("message metadata = %#v", message)
	}
	if len(message.Content) != 1 || message.Content[0].Type != ContentText || message.Content[0].Text != "hello" {
		t.Fatalf("content = %#v", message.Content)
	}
}

func TestMessageUnmarshalAcceptsSingleContentBlock(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"messageType":"assistant","content":{"type":"text","text":"hello"}}`), &message); err != nil {
		t.Fatal(err)
	}
	if message.Type != MessageAssistant {
		t.Fatalf("message type = %q", message.Type)
	}
	if len(message.Content) != 1 || message.Content[0].Type != ContentText || message.Content[0].Text != "hello" {
		t.Fatalf("content = %#v", message.Content)
	}
}

func TestMessageUnmarshalAcceptsMixedStringContentArray(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"type":"assistant","content":["hello",{"type":"text","text":" world"}]}`), &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 2 ||
		message.Content[0].Type != ContentText || message.Content[0].Text != "hello" ||
		message.Content[1].Type != ContentText || message.Content[1].Text != " world" {
		t.Fatalf("content = %#v", message.Content)
	}
}

func TestMessageUnmarshalAcceptsTextBodyAliases(t *testing.T) {
	for name, raw := range map[string]string{
		"text":    `{"role":"assistant","text":"from text"}`,
		"body":    `{"role":"assistant","body":"from body"}`,
		"message": `{"role":"assistant","message":"from message"}`,
		"value":   `{"role":"assistant","value":"from value"}`,
		"output":  `{"role":"assistant","output":"from output"}`,
		"null":    `{"role":"assistant","content":null,"text":"from null fallback"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var message Message
			if err := json.Unmarshal([]byte(raw), &message); err != nil {
				t.Fatal(err)
			}
			if message.Type != MessageAssistant || len(message.Content) != 1 || message.Content[0].Text == "" {
				t.Fatalf("message = %#v", message)
			}
		})
	}
}

func TestMessageUnmarshalAcceptsTypeAliases(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want MessageType
	}{
		"assistant message": {raw: `{"type":"assistant_message","content":"hello"}`, want: MessageAssistant},
		"user camel":        {raw: `{"messageType":"userMessage","content":"hi"}`, want: MessageUser},
		"system event":      {raw: `{"message_type":"system-event","content":"rules"}`, want: MessageSystem},
		"attachment":        {raw: `{"role":"attachmentMessage","content":"file"}`, want: MessageAttachment},
		"progress update":   {raw: `{"type":"progress_update","content":"loading"}`, want: MessageProgress},
		"tombstone event":   {raw: `{"type":"tombstone_event","content":"removed"}`, want: MessageTombstone},
	} {
		t.Run(name, func(t *testing.T) {
			var message Message
			if err := json.Unmarshal([]byte(tc.raw), &message); err != nil {
				t.Fatal(err)
			}
			if message.Type != tc.want || len(message.Content) != 1 {
				t.Fatalf("message = %#v, want type %q", message, tc.want)
			}
		})
	}
}

func TestMessageUnmarshalAcceptsNormalizedFieldAliases(t *testing.T) {
	var message Message
	raw := `{"Message-Type":"assistant-message","Message-ID":101,"Message-UUID":"msg_101","Parent-Message-ID":100,"Session-ID":"sess_norm","Created At":"2026-01-01T00:00:01Z","Is-Meta":true,"Message Text":"done"}`
	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		t.Fatal(err)
	}
	if message.Type != MessageAssistant || message.ID != "101" || message.UUID != "msg_101" {
		t.Fatalf("normalized message IDs = %#v", message)
	}
	if message.ParentUUID == nil || *message.ParentUUID != "100" || message.SessionID != "sess_norm" || message.Timestamp != "2026-01-01T00:00:01Z" || !message.IsMeta {
		t.Fatalf("normalized message metadata = %#v", message)
	}
	if len(message.Content) != 1 || message.Content[0].Type != ContentText || message.Content[0].Text != "done" {
		t.Fatalf("normalized message content = %#v", message.Content)
	}
}

func TestSessionEntryUnmarshalAcceptsTypeAliases(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want MessageType
	}{
		"base type":     {raw: `{"type":"assistant_message","uuid":"a1"}`, want: MessageAssistant},
		"entry type":    {raw: `{"entryType":"userMessage","uuid":"u1"}`, want: MessageUser},
		"message type":  {raw: `{"message_type":"system-event","uuid":"s1"}`, want: MessageSystem},
		"progress type": {raw: `{"entry_type":"progress_update","uuid":"p1"}`, want: MessageProgress},
	} {
		t.Run(name, func(t *testing.T) {
			var entry SessionEntry
			if err := json.Unmarshal([]byte(tc.raw), &entry); err != nil {
				t.Fatal(err)
			}
			if entry.Type != tc.want {
				t.Fatalf("entry = %#v, want type %q", entry, tc.want)
			}
		})
	}
}

func TestSDKEventUnmarshalAcceptsTypeAliases(t *testing.T) {
	for name, tc := range map[string]struct {
		raw         string
		want        SDKEventType
		wantMessage bool
	}{
		"assistant message": {
			raw:         `{"type":"assistant_message","message":{"content":"hello"}}`,
			want:        SDKEventAssistant,
			wantMessage: true,
		},
		"user camel": {
			raw:         `{"eventType":"userMessage","payload":{"text":"hi"}}`,
			want:        SDKEventUser,
			wantMessage: true,
		},
		"system event": {
			raw:         `{"message_type":"system-event","body":{"text":"rules"}}`,
			want:        SDKEventSystem,
			wantMessage: true,
		},
		"result event": {
			raw:  `{"kind":"result_event","result":{"ok":true}}`,
			want: SDKEventResult,
		},
		"error event": {
			raw:  `{"name":"errorEvent","error":"boom"}`,
			want: SDKEventError,
		},
		"progress update": {
			raw:  `{"event":"progress_update","status":"working"}`,
			want: SDKEventStatus,
		},
		"assistant delta": {
			raw:         `{"eventType":"assistant_delta","payload":{"message":"partial"}}`,
			want:        SDKEventAssistant,
			wantMessage: true,
		},
		"human message": {
			raw:         `{"role":"humanMessage","body":{"text":"hello"}}`,
			want:        SDKEventUser,
			wantMessage: true,
		},
		"final result": {
			raw:  `{"name":"finalResult","result":{"summary":"done"}}`,
			want: SDKEventResult,
		},
		"response completed": {
			raw:  `{"kind":"response.completed","result":{"ok":true}}`,
			want: SDKEventResult,
		},
		"failure event": {
			raw:  `{"eventType":"failureEvent","error":"boom"}`,
			want: SDKEventError,
		},
		"status message": {
			raw:  `{"event":"statusMessage","status":"queued"}`,
			want: SDKEventStatus,
		},
	} {
		t.Run(name, func(t *testing.T) {
			var event SDKEvent
			if err := json.Unmarshal([]byte(tc.raw), &event); err != nil {
				t.Fatal(err)
			}
			if event.Type != tc.want {
				t.Fatalf("event type = %q, want %q", event.Type, tc.want)
			}
			if tc.wantMessage {
				if event.Message == nil || event.Message.Type != MessageType(tc.want) || len(event.Message.Content) != 1 {
					t.Fatalf("event message = %#v", event.Message)
				}
			}
		})
	}
}

func TestSDKEventUnmarshalAcceptsTimestampAliases(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want string
	}{
		"event time": {
			raw:  `{"type":"status","eventTime":"2026-01-01T00:00:01Z","status":"working"}`,
			want: "2026-01-01T00:00:01Z",
		},
		"occurred at": {
			raw:  `{"type":"status","occurred_at":"2026-01-01T00:00:02Z","status":"working"}`,
			want: "2026-01-01T00:00:02Z",
		},
		"numeric timestamp": {
			raw:  `{"type":"status","timestamp":1767225603000,"status":"working"}`,
			want: "1767225603000",
		},
		"numeric created at": {
			raw:  `{"type":"status","createdAt":1767225604,"status":"working"}`,
			want: "1767225604",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var event SDKEvent
			if err := json.Unmarshal([]byte(tc.raw), &event); err != nil {
				t.Fatal(err)
			}
			if event.Timestamp != tc.want {
				t.Fatalf("timestamp = %q, want %q", event.Timestamp, tc.want)
			}
		})
	}
}

func TestSDKEventUnmarshalAcceptsScalarMessagePayload(t *testing.T) {
	for name, tc := range map[string]struct {
		raw      string
		wantType MessageType
		wantText string
	}{
		"message string": {
			raw:      `{"type":"assistant","message":"hello from scalar"}`,
			wantType: MessageAssistant,
			wantText: "hello from scalar",
		},
		"message blocks": {
			raw:      `{"type":"user","message":[{"type":"text","text":"hello from block"}]}`,
			wantType: MessageUser,
			wantText: "hello from block",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var event SDKEvent
			if err := json.Unmarshal([]byte(tc.raw), &event); err != nil {
				t.Fatal(err)
			}
			if event.Message == nil || event.Message.Type != tc.wantType || len(event.Message.Content) != 1 || event.Message.Content[0].Text != tc.wantText {
				t.Fatalf("message = %#v", event.Message)
			}
		})
	}
}

func TestSDKEventUnmarshalAcceptsStatusErrorResultAliases(t *testing.T) {
	for name, tc := range map[string]struct {
		raw        string
		wantStatus string
		wantError  string
		wantResult any
	}{
		"status message": {
			raw:        `{"type":"status","statusMessage":"queued"}`,
			wantStatus: "queued",
		},
		"progress message": {
			raw:        `{"eventType":"progress","progress_message":"working"}`,
			wantStatus: "working",
		},
		"state message": {
			raw:        `{"type":"status","stateMessage":"tool queued"}`,
			wantStatus: "tool queued",
		},
		"update text": {
			raw:        `{"kind":"status_update","update_text":"still running"}`,
			wantStatus: "still running",
		},
		"status message text": {
			raw:        `{"type":"status","messageText":"status detail"}`,
			wantStatus: "status detail",
		},
		"error message": {
			raw:       `{"type":"error","errorMessage":"boom"}`,
			wantError: "boom",
		},
		"failure reason": {
			raw:       `{"eventType":"failureEvent","failure_reason":"denied"}`,
			wantError: "denied",
		},
		"failure message": {
			raw:       `{"eventType":"failureEvent","failureMessage":"permission denied"}`,
			wantError: "permission denied",
		},
		"exception diagnostic": {
			raw:       `{"type":"error","diagnostic_message":"stack collapsed"}`,
			wantError: "stack collapsed",
		},
		"error message text": {
			raw:       `{"type":"error","messageText":"error detail"}`,
			wantError: "error detail",
		},
		"result text": {
			raw:        `{"type":"result","outputText":"done"}`,
			wantResult: "done",
		},
		"summary text": {
			raw:        `{"type":"result","summaryText":"summarized done"}`,
			wantResult: "summarized done",
		},
		"final output": {
			raw:        `{"eventType":"finalResult","final_output":"final done"}`,
			wantResult: "final done",
		},
		"response text": {
			raw:        `{"type":"result","responseText":"response done"}`,
			wantResult: "response done",
		},
		"result object": {
			raw:        `{"eventType":"finalResult","response":{"ok":true}}`,
			wantResult: map[string]any{"ok": true},
		},
		"summary object": {
			raw:        `{"eventType":"finalResult","summary":{"tokens":7}}`,
			wantResult: map[string]any{"tokens": float64(7)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var event SDKEvent
			if err := json.Unmarshal([]byte(tc.raw), &event); err != nil {
				t.Fatal(err)
			}
			if event.Status != tc.wantStatus || event.Error != tc.wantError {
				t.Fatalf("event = %#v", event)
			}
			if tc.wantResult != nil && !reflect.DeepEqual(event.Result, tc.wantResult) {
				t.Fatalf("result = %#v, want %#v", event.Result, tc.wantResult)
			}
		})
	}
}

func TestContentBlockUnmarshalAcceptsTextAliases(t *testing.T) {
	for name, raw := range map[string]string{
		"body":         `{"type":"text","body":"from body"}`,
		"message":      `{"type":"text","message":"from message"}`,
		"value":        `{"type":"text","value":"from value"}`,
		"output":       `{"type":"text","output":"from output"}`,
		"contentText":  `{"type":"text","contentText":"from contentText"}`,
		"content_text": `{"type":"text","content_text":"from content_text"}`,
		"content":      `{"type":"text","content":"from content"}`,
		"output_text":  `{"type":"output_text","text":"from output_text"}`,
		"thinking":     `{"type":"thinking","content":"from thinking"}`,
		"default_type": `{"body":"from default"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var block ContentBlock
			if err := json.Unmarshal([]byte(raw), &block); err != nil {
				t.Fatal(err)
			}
			if block.Text == "" {
				t.Fatalf("block = %#v", block)
			}
			if name == "default_type" && block.Type != ContentText {
				t.Fatalf("default type = %q", block.Type)
			}
			if name == "thinking" && block.Type != ContentThinking {
				t.Fatalf("thinking type = %q", block.Type)
			}
		})
	}
}

func TestContentBlockUnmarshalAcceptsTypeAliases(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":[
		{"type":"toolUse","id":"toolu_1","name":"Read"},
		{"type":"tool-result","toolUseId":"toolu_1","content":"ok"},
		{"type":"cacheEdits","cacheReference":"cache_1"},
		{"type":"inputImage","mimeType":"image/png","base64":"AAAA"},
		{"type":"chain-of-thought","content":"reasoning"}
	]}`), &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 5 {
		t.Fatalf("content = %#v", message.Content)
	}
	if message.Content[0].Type != ContentToolUse || message.Content[0].ID != "toolu_1" {
		t.Fatalf("tool use = %#v", message.Content[0])
	}
	if message.Content[1].Type != ContentToolResult || message.Content[1].ToolUseID != "toolu_1" {
		t.Fatalf("tool result = %#v", message.Content[1])
	}
	if message.Content[2].Type != ContentCacheEdits || message.Content[2].CacheReference != "cache_1" {
		t.Fatalf("cache edits = %#v", message.Content[2])
	}
	source, ok := message.Content[3].Source.(ImageSource)
	if message.Content[3].Type != ContentImage || !ok || source.MediaType != "image/png" || source.Data != "AAAA" {
		t.Fatalf("image = %#v source=%#v", message.Content[3], message.Content[3].Source)
	}
	if message.Content[4].Type != ContentThinking || message.Content[4].Text != "reasoning" {
		t.Fatalf("thinking = %#v", message.Content[4])
	}
}

func TestMessageUnmarshalAcceptsContentBlockTextAliases(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":[{"type":"text","body":"hello"},{"type":"thinking","content":"reasoning"}]}`), &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 2 ||
		message.Content[0].Type != ContentText || message.Content[0].Text != "hello" ||
		message.Content[1].Type != ContentThinking || message.Content[1].Text != "reasoning" {
		t.Fatalf("content = %#v", message.Content)
	}
}

func TestImageSourceUnmarshalAcceptsAliases(t *testing.T) {
	var source ImageSource
	if err := json.Unmarshal([]byte(`{"kind":"base64","mimeType":"image/jpeg","base64":"AAAA"}`), &source); err != nil {
		t.Fatal(err)
	}
	if source.Type != "base64" || source.MediaType != "image/jpeg" || source.Data != "AAAA" {
		t.Fatalf("source = %#v", source)
	}
}

func TestContentBlockUnmarshalNormalizesImageSourceAliases(t *testing.T) {
	var block ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"image","source":{"kind":"base64","contentType":"image/webp","payload":"BBBB"}}`), &block); err != nil {
		t.Fatal(err)
	}
	source, ok := block.Source.(ImageSource)
	if !ok || source.Type != "base64" || source.MediaType != "image/webp" || source.Data != "BBBB" {
		t.Fatalf("source = %#v", block.Source)
	}
}

func TestContentBlockUnmarshalAcceptsTopLevelImageSourceAliases(t *testing.T) {
	var block ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"image","mimeType":"image/png","base64":"CCCC"}`), &block); err != nil {
		t.Fatal(err)
	}
	source, ok := block.Source.(ImageSource)
	if !ok || source.Type != "base64" || source.MediaType != "image/png" || source.Data != "CCCC" {
		t.Fatalf("source = %#v", block.Source)
	}
}

func TestMessageUnmarshalAcceptsImageContentBlockAliases(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"user","content":[{"type":"image","source":{"mimeType":"image/png","base64":"DDDD"}}]}`), &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 1 || message.Content[0].Type != ContentImage {
		t.Fatalf("content = %#v", message.Content)
	}
	source, ok := message.Content[0].Source.(ImageSource)
	if !ok || source.Type != "base64" || source.MediaType != "image/png" || source.Data != "DDDD" {
		t.Fatalf("source = %#v", message.Content[0].Source)
	}
}
