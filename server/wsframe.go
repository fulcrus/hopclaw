package server

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Frame types for the WebSocket protocol
// ---------------------------------------------------------------------------
//
// Three frame kinds:
//   - request  (client->server RPC call)
//   - response (server->client RPC reply)
//   - event    (server->client push)

const (
	frameTypeRequest  = "req"
	frameTypeResponse = "res"
	frameTypeEvent    = "event"

	// Protocol version negotiated during handshake.
	wsProtocolVersion = 1
)

// ---------------------------------------------------------------------------
// RPC methods and client modes
// ---------------------------------------------------------------------------

type WSMethod string

const (
	WSMethodConnect            WSMethod = "connect"
	WSMethodPing               WSMethod = "ping"
	WSMethodStatus             WSMethod = "status"
	WSMethodRunsSubmit         WSMethod = "runs.submit"
	WSMethodInteractionsSubmit WSMethod = "interactions.submit"
	WSMethodRunsList           WSMethod = "runs.list"
	WSMethodRunsCancel         WSMethod = "runs.cancel"
	WSMethodSessionsList       WSMethod = "sessions.list"
	WSMethodSessionsGet        WSMethod = "sessions.get"
	WSMethodApprovalsList      WSMethod = "approvals.list"
	WSMethodApprovalsResolve   WSMethod = "approvals.resolve"
	WSMethodEventsList         WSMethod = "events.list"
)

func (m WSMethod) String() string {
	return string(m)
}

var wsSupportedMethods = []WSMethod{
	WSMethodRunsSubmit,
	WSMethodInteractionsSubmit,
	WSMethodRunsList,
	WSMethodRunsCancel,
	WSMethodSessionsList,
	WSMethodSessionsGet,
	WSMethodApprovalsList,
	WSMethodApprovalsResolve,
	WSMethodEventsList,
	WSMethodStatus,
	WSMethodPing,
}

func wsMethodStrings(methods []WSMethod) []string {
	out := make([]string, 0, len(methods))
	for _, method := range methods {
		if method == "" {
			continue
		}
		out = append(out, method.String())
	}
	return out
}

type WSClientMode string

const (
	WSClientModeUnknown  WSClientMode = ""
	WSClientModeBackend  WSClientMode = "backend"
	WSClientModeFrontend WSClientMode = "frontend"
	WSClientModeProbe    WSClientMode = "probe"
)

func (m WSClientMode) String() string {
	return string(m)
}

// ---------------------------------------------------------------------------
// Payload limits
// ---------------------------------------------------------------------------

const (
	wsMaxPayloadBytes        = 25 * 1024 * 1024 // 25 MB post-auth
	wsMaxPreAuthPayloadBytes = 64 * 1024        // 64 KB before auth
	wsMaxBufferedBytes       = 50 * 1024 * 1024 // 50 MB per-connection send buffer
)

// ---------------------------------------------------------------------------
// Timing
// ---------------------------------------------------------------------------

const (
	wsTickInterval     = 30 * time.Second
	wsHandshakeTimeout = 5 * time.Second
	wsPongWait         = 60 * time.Second
	wsPingInterval     = 45 * time.Second // must be < pongWait
	wsWriteWait        = 10 * time.Second
)

// ---------------------------------------------------------------------------
// Request / Response / Event frames
// ---------------------------------------------------------------------------

// RequestFrame is a client->server RPC call.
type RequestFrame struct {
	Type   string          `json:"type"` // always "req"
	ID     string          `json:"id"`
	Method WSMethod        `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// ResponseFrame is a server->client RPC response.
type ResponseFrame struct {
	Type    string          `json:"type"` // always "res"
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *WSError        `json:"error,omitempty"`
}

// EventFrame is a server->client push event.
type EventFrame struct {
	Type    string `json:"type"` // always "event"
	Event   string `json:"event"`
	Payload any    `json:"payload,omitempty"`
	Seq     int64  `json:"seq,omitempty"`
}

// WSError is a structured error in a response frame.
type WSError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// ---------------------------------------------------------------------------
// Error codes
// ---------------------------------------------------------------------------

const (
	WSErrInvalidRequest = "invalid_request"
	WSErrNotFound       = "not_found"
	WSErrUnauthorized   = "unauthorized"
	WSErrRateLimited    = "rate_limited"
	WSErrInternal       = "internal_error"
)

// ---------------------------------------------------------------------------
// Handshake types
// ---------------------------------------------------------------------------

// ConnectParams sent by client during handshake.
type ConnectParams struct {
	MinProtocol int               `json:"min_protocol"`
	MaxProtocol int               `json:"max_protocol"`
	Client      ConnectClientInfo `json:"client"`
	Auth        *ConnectAuth      `json:"auth,omitempty"`
	Role        string            `json:"role,omitempty"`
	Scopes      []string          `json:"scopes,omitempty"`
}

// ConnectClientInfo describes the connecting client.
type ConnectClientInfo struct {
	ID          string       `json:"id"`
	DisplayName string       `json:"display_name,omitempty"`
	Version     string       `json:"version,omitempty"`
	Platform    string       `json:"platform,omitempty"`
	Mode        WSClientMode `json:"mode,omitempty"`
}

// ConnectAuth carries authentication credentials.
type ConnectAuth struct {
	Token    string `json:"token,omitempty"`
	Password string `json:"password,omitempty"`
}

// HelloOK is the successful handshake response.
type HelloOK struct {
	Type     string        `json:"type"` // "hello-ok"
	Protocol int           `json:"protocol"`
	Server   HelloServer   `json:"server"`
	Features HelloFeatures `json:"features"`
	Policy   HelloPolicy   `json:"policy"`
}

// HelloServer identifies the server in the handshake response.
type HelloServer struct {
	Version string `json:"version"`
	ConnID  string `json:"conn_id"`
}

// HelloFeatures lists the methods and events supported by the server.
type HelloFeatures struct {
	Methods []string `json:"methods"`
	Events  []string `json:"events"`
}

// HelloPolicy communicates server-side limits to the client.
type HelloPolicy struct {
	MaxPayload     int   `json:"max_payload"`
	MaxBuffered    int   `json:"max_buffered"`
	TickIntervalMs int64 `json:"tick_interval_ms"`
}
