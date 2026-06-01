package messages

import (
	"ccgo/internal/contracts"
)

func NormalizeForAPI(in []contracts.Message) []contracts.APIMessage {
	out := make([]contracts.APIMessage, 0, len(in))
	for _, msg := range in {
		switch msg.Type {
		case contracts.MessageUser:
			out = append(out, contracts.APIMessage{Role: "user", Content: msg.Content})
		case contracts.MessageAssistant:
			out = append(out, contracts.APIMessage{Role: "assistant", Content: msg.Content})
		default:
			continue
		}
	}
	return out
}

func LinkParentChain(messages []contracts.Message) []contracts.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]contracts.Message, len(messages))
	copy(out, messages)
	var parent *contracts.ID
	for i := range out {
		if out[i].UUID == "" {
			out[i].UUID = contracts.NewID()
		}
		out[i].ParentUUID = parent
		id := out[i].UUID
		parent = &id
	}
	return out
}
