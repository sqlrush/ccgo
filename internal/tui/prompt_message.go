package tui

import (
	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

func (e ScreenEvent) PromptMessages() []contracts.Message {
	if e.Type != ScreenEventPromptSubmitted {
		return nil
	}
	display := e.Display
	if display == "" {
		display = e.Value
	}
	return session.PromptMessages(display, e.PastedContents)
}
