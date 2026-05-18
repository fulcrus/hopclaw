package gateway

import "encoding/json"

const (
	operatorWebSocketPath = "/operator/ws"
	devicePairClaimPath   = "/device/pair/claim"
)

type operatorWSRequestFrame struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type operatorWSResponseFrame struct {
	Type    string                `json:"type"`
	ID      string                `json:"id"`
	OK      bool                  `json:"ok"`
	Payload json.RawMessage       `json:"payload,omitempty"`
	Error   *operatorWSFrameError `json:"error,omitempty"`
}

type operatorWSFrameError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
