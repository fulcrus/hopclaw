// Package nodes provides multi-device node registration and command dispatch.
package nodes

import (
	"time"
)

// NodeSession represents a connected device node.
type NodeSession struct {
	NodeID          string    `json:"node_id"`
	Platform        string    `json:"platform"` // "iOS", "Android", "macOS", "Windows", "Linux"
	Version         string    `json:"version"`
	DeviceFamily    string    `json:"device_family"`    // "iPhone", "iPad", "Mac", "PC"
	ModelIdentifier string    `json:"model_identifier"` // "iPhone15,2"
	RemoteIP        string    `json:"remote_ip"`
	Capabilities    []string  `json:"capabilities"`
	Commands        []string  `json:"commands"`
	ConnectedAt     time.Time `json:"connected_at"`
	LastSeenAt      time.Time `json:"last_seen_at"`
}

// NodeInvokeRequest sends a command to a node.
type NodeInvokeRequest struct {
	NodeID  string         `json:"node_id"`
	Command string         `json:"command"`
	Params  map[string]any `json:"params,omitempty"`
	Timeout time.Duration  `json:"timeout,omitempty"`
}

// NodeInvokeResponse is the response from a node command.
type NodeInvokeResponse struct {
	OK    bool           `json:"ok"`
	Data  map[string]any `json:"data,omitempty"`
	Error string         `json:"error,omitempty"`
}
