// Package acp implements HopClaw's Agent Client Protocol.
//
// ACP enables external clients (IDE extensions, mobile apps, desktop apps) to
// interact with the HopClaw agent over NDJSON (newline-delimited JSON) using
// JSON-RPC 2.0 framing. The ACP server runs as a subprocess that connects to
// the HopClaw gateway via HTTP.
//
// Naming note: HopClaw's "ACP" refers to the Agent CLIENT Protocol — the
// direction is client → agent (aligned with Zed's ACP for IDE integration).
// It is NOT related to IBM / Linux Foundation's Agent Communication Protocol,
// which targets agent ↔ agent interoperability across systems. The two share
// the "ACP" acronym but solve different problems and are not interchangeable.
// For agent-to-agent scenarios within HopClaw, see the delegation contract in
// package agent.
//
// Protocol overview:
//
//   - Transport: NDJSON over stdio (one JSON object per line)
//   - Framing: JSON-RPC 2.0 (request/response with integer IDs, notifications without)
//   - Lifecycle: initialize → new_session/load_session → prompt → stream events
//   - Permissions: tool executions may require client approval via permission requests
package acp
