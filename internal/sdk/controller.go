package sdk

import "fmt"

// Controller dispatches inbound control requests to live session callbacks.
// Wire contract: CC bridgeMessaging.ts:362-371 (interrupt), :306-315 (set_model).
type Controller struct {
	interrupt func()
	setModel  func(string) error
}

// NewController wires the interrupt and set_model callbacks for a live session.
func NewController(interrupt func(), setModel func(string) error) *Controller {
	return &Controller{interrupt: interrupt, setModel: setModel}
}

// Handle dispatches one control request and returns the response to write back.
func (c *Controller) Handle(req ControlRequest) ControlResponse {
	switch req.Subtype() {
	case "interrupt":
		if c.interrupt != nil {
			c.interrupt()
		}
		return SuccessResponse(req.RequestID, nil)

	case "set_model":
		model, _ := req.Request["model"].(string)
		if c.setModel != nil {
			if err := c.setModel(model); err != nil {
				return ErrorResponse(req.RequestID, err.Error())
			}
		}
		return SuccessResponse(req.RequestID, map[string]any{"model": model})

	case "initialize":
		return SuccessResponse(req.RequestID, map[string]any{"capabilities": []string{"interrupt", "set_model", "can_use_tool"}})

	default:
		return ErrorResponse(req.RequestID, fmt.Sprintf("unsupported control subtype %q", req.Subtype()))
	}
}
