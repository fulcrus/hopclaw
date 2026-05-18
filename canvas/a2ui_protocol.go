package canvas

import (
	"encoding/json"
)

// ---------------------------------------------------------------------------
// A2UI WebSocket frame types
// ---------------------------------------------------------------------------

const (
	// FrameTypePush delivers new components to the client.
	FrameTypePush = "a2ui_push"

	// FrameTypeReset clears all components for a session.
	FrameTypeReset = "a2ui_reset"

	// FrameTypeAck acknowledges receipt from the client.
	FrameTypeAck = "a2ui_ack"

	// FrameTypeState delivers the full component state snapshot.
	FrameTypeState = "a2ui_state"
)

// Frame is the wire format for A2UI WebSocket messages.
type Frame struct {
	Type       string      `json:"type"`
	SessionID  string      `json:"session_id,omitempty"`
	Version    int64       `json:"version,omitempty"`
	Components []Component `json:"components,omitempty"`
}

// EncodeFrame marshals a frame to JSON bytes.
func EncodeFrame(f Frame) ([]byte, error) {
	return json.Marshal(f)
}

// DecodeFrame unmarshals a JSON frame.
func DecodeFrame(data []byte) (Frame, error) {
	var f Frame
	err := json.Unmarshal(data, &f)
	return f, err
}
