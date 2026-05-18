// Package mcp implements the Model Context Protocol (MCP), a JSON-RPC 2.0
// based protocol for connecting AI assistants to external tool servers.
//
// HopClaw uses MCP in two roles:
//
//   - Client: connects to external MCP servers, discovers their tools, and
//     exposes those tools to the agent loop via the Bridge.
//   - Server: exposes HopClaw's own tools as an MCP endpoint so that
//     external callers can invoke them over stdio.
//
// The transport layer uses NDJSON (newline-delimited JSON) over stdio pipes.
// The Manager coordinates the lifecycle of multiple MCP server connections,
// and the Bridge converts MCP tools into HopClaw ToolManifest entries.
package mcp
