// Package mediagen provides pluggable media generation providers for HopClaw.
//
// The package intentionally stays outside the runtime kernel. It defines
// provider contracts and a lightweight registry that image/video/music
// generation tools can consume without coupling generation-specific API logic
// into core orchestration, approvals, or persistence layers.
package mediagen
