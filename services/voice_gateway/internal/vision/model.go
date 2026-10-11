package vision

import "context"

// ModelRequest is the bounded, non-image payload passed to a vision model.
//
// The gateway never forwards raw pixels to the model client. The capture
// device performs on-device inference or uploads the frame through a separate
// privacy-reviewed channel; this request carries only the task type, the
// guardian prompt, and bounded frame dimensions.
type ModelRequest struct {
	TaskID      string
	DeviceID    string
	TaskType    TaskType
	Prompt      string
	FrameWidth  int
	FrameHeight int
	FrameBytes  int
}

// ModelResult is the bounded model output. Implementations must not return raw
// provider payloads, image bytes, or child-identifying content.
type ModelResult struct {
	Model   string
	Labels  []Label
	Summary string
}

// ModelClient performs one bounded vision inference. Implementations must be
// safe for concurrent use and must never log prompts, frames, or results.
type ModelClient interface {
	Analyze(ctx context.Context, request ModelRequest) (ModelResult, error)
}

// ModelClientFunc adapts a function to the ModelClient interface.
type ModelClientFunc func(ctx context.Context, request ModelRequest) (ModelResult, error)

// Analyze implements ModelClient.
func (fn ModelClientFunc) Analyze(
	ctx context.Context,
	request ModelRequest,
) (ModelResult, error) {
	return fn(ctx, request)
}
