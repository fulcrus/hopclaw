package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
	"github.com/fulcrus/hopclaw/internal/usererror"
	"github.com/fulcrus/hopclaw/logging"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Upgrader
// ---------------------------------------------------------------------------

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  wsReadBufferSize,
	WriteBufferSize: wsWriteBufferSize,
	CheckOrigin:     sameOriginWebSocketRequest,
}

func sameOriginWebSocketRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := neturl.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	originHost, originPort := canonicalWSHostPort(parsed.Host, parsed.Scheme)
	requestHost, requestPort := canonicalWSHostPort(r.Host, requestScheme(r))
	return strings.EqualFold(originHost, requestHost) && originPort == requestPort
}

func canonicalWSHostPort(hostport, scheme string) (string, string) {
	hostport = strings.TrimSpace(hostport)
	host := hostport
	port := ""
	if parsedHost, parsedPort, err := net.SplitHostPort(hostport); err == nil {
		host, port = parsedHost, parsedPort
	} else if strings.Count(hostport, ":") > 1 && strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		host = strings.Trim(hostport, "[]")
	}
	host = strings.Trim(host, "[]")
	if port == "" {
		switch strings.ToLower(strings.TrimSpace(scheme)) {
		case "https", "wss":
			port = "443"
		default:
			port = "80"
		}
	}
	return strings.ToLower(host), port
}

func requestScheme(r *http.Request) string {
	if r == nil {
		return "http"
	}
	if r.TLS != nil {
		return "https"
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return "https"
	}
	return "http"
}

// ---------------------------------------------------------------------------
// Available RPC methods
// ---------------------------------------------------------------------------

var wsMethods = wsMethodStrings(wsSupportedMethods)

// Available event types.
var wsEvents = []string{
	"tick", "run.submitted", "run.started", "run.completed", "run.failed",
	"model.text_delta", "tool.executed", "approval.requested", "approval.resolved",
}

// ---------------------------------------------------------------------------
// nonce generation
// ---------------------------------------------------------------------------

const wsNonceBytes = 16

func generateNonce() (string, error) {
	buf := make([]byte, wsNonceBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("ws: failed to generate nonce: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// HandleWebSocket is the HTTP handler for WebSocket upgrade.
// Canonical route: GET /runtime/ws
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.wsHub == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("websocket not available"))
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an HTTP error response.
		log.Warn("websocket upgrade failed", "error", err)
		return
	}

	// Generate a unique connection ID.
	connID, err := generateNonce()
	if err != nil {
		log.Warn("websocket nonce generation failed", "error", err)
		logging.DebugIfErr(conn.Close(), "close websocket connection failed")
		return
	}

	wsc := newWSConn(context.WithoutCancel(r.Context()), connID, conn, s)
	wsc.authScope = requestAuthScopeFromHeaders(r)

	// Run the handshake synchronously before starting the pumps.
	if !s.doHandshake(wsc) {
		logging.DebugIfErr(conn.Close(), "close websocket connection failed")
		return
	}

	// Register with the hub.
	if err := s.wsHub.Register(wsc); err != nil {
		log.Warn("websocket registration failed", "error", err)
		logging.LogIfErr(wsc.context(), wsc.sendResponse(ResponseFrame{
			Type: frameTypeResponse,
			OK:   false,
			Error: &WSError{
				Code:    WSErrRateLimited,
				Message: "max connections reached",
			},
		}), "send websocket response failed")
		logging.DebugIfErr(conn.Close(), "close websocket connection failed")
		return
	}

	// Start read/write pumps in separate goroutines.
	go wsc.writePump()
	go wsc.readPump()
}

// doHandshake runs the WebSocket handshake protocol:
//  1. Send challenge event with nonce
//  2. Wait for "connect" request (within handshake timeout)
//  3. Validate ConnectParams (protocol version, auth)
//  4. Send HelloOK response
//
// Returns true on success.
func (s *Server) doHandshake(wsc *WSConn) bool {
	// Restrict reads to pre-auth payload size during handshake.
	wsc.conn.SetReadLimit(int64(wsMaxPreAuthPayloadBytes))

	// 1. Send challenge event.
	nonce, err := generateNonce()
	if err != nil {
		log.Warn("websocket challenge nonce failed", "error", err)
		return false
	}
	challenge := EventFrame{
		Type:  frameTypeEvent,
		Event: "challenge",
		Payload: map[string]any{
			"nonce":        nonce,
			"min_protocol": wsProtocolVersion,
			"max_protocol": wsProtocolVersion,
		},
	}
	data, err := json.Marshal(challenge)
	if err != nil {
		return false
	}
	if err := wsc.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
		return false
	}
	if err := wsc.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return false
	}

	// 2. Wait for "connect" request within handshake timeout.
	if err := wsc.conn.SetReadDeadline(time.Now().Add(wsHandshakeTimeout)); err != nil {
		return false
	}
	_, msg, err := wsc.conn.ReadMessage()
	if err != nil {
		log.Warn("websocket handshake read failed", "error", err)
		return false
	}

	var frame RequestFrame
	if err := json.Unmarshal(msg, &frame); err != nil || frame.Type != frameTypeRequest || frame.Method != WSMethodConnect {
		s.sendHandshakeError(wsc, frame.ID, WSErrInvalidRequest, "expected connect request")
		return false
	}

	var params ConnectParams
	if frame.Params != nil {
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			s.sendHandshakeError(wsc, frame.ID, WSErrInvalidRequest, "invalid connect params")
			return false
		}
	}

	// 3. Validate protocol version.
	if params.MaxProtocol < wsProtocolVersion || params.MinProtocol > wsProtocolVersion {
		s.sendHandshakeError(wsc, frame.ID, WSErrInvalidRequest,
			fmt.Sprintf("unsupported protocol version range [%d, %d], server supports %d",
				params.MinProtocol, params.MaxProtocol, wsProtocolVersion))
		return false
	}

	// 4. Validate auth if the server has a token configured.
	token := strings.TrimSpace(s.config.AuthToken)
	if token != "" {
		if params.Auth == nil || !secureTokenEqual(strings.TrimSpace(params.Auth.Token), token) {
			s.sendHandshakeError(wsc, frame.ID, WSErrUnauthorized, "invalid or missing auth token")
			return false
		}
	}

	// Populate connection metadata.
	wsc.clientInfo = params.Client
	wsc.role = params.Role
	wsc.scopes = params.Scopes
	wsc.authScope = mergeRequestAuthScope(wsc.authScope, params.Scopes)
	wsc.authed = true

	// Lift the read limit now that the client is authenticated.
	wsc.conn.SetReadLimit(int64(wsMaxPayloadBytes))

	// 5. Send HelloOK.
	hello := HelloOK{
		Type:     "hello-ok",
		Protocol: wsProtocolVersion,
		Server: HelloServer{
			Version: "hopclaw/1.0",
			ConnID:  wsc.id,
		},
		Features: HelloFeatures{
			Methods: wsMethods,
			Events:  wsEvents,
		},
		Policy: HelloPolicy{
			MaxPayload:     wsMaxPayloadBytes,
			MaxBuffered:    wsMaxBufferedBytes,
			TickIntervalMs: wsTickInterval.Milliseconds(),
		},
	}
	helloPayload, err := json.Marshal(hello)
	if err != nil {
		return false
	}
	resp := ResponseFrame{
		Type:    frameTypeResponse,
		ID:      frame.ID,
		OK:      true,
		Payload: helloPayload,
	}
	respData, err := json.Marshal(resp)
	if err != nil {
		return false
	}
	if err := wsc.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
		return false
	}
	if err := wsc.conn.WriteMessage(websocket.TextMessage, respData); err != nil {
		return false
	}

	// Clear the read deadline so the read pump uses pong-based deadlines.
	if err := wsc.conn.SetReadDeadline(time.Time{}); err != nil {
		return false
	}
	return true
}

// sendHandshakeError sends an error response during the handshake phase.
func (s *Server) sendHandshakeError(wsc *WSConn, reqID, code, message string) {
	resp := ResponseFrame{
		Type: frameTypeResponse,
		ID:   reqID,
		OK:   false,
		Error: &WSError{
			Code:    code,
			Message: message,
		},
	}
	data, _ := json.Marshal(resp)
	logging.DebugIfErr(wsc.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)), "set websocket write deadline failed")
	logging.LogIfErr(wsc.context(), wsc.conn.WriteMessage(websocket.TextMessage, data), "websocket write message failed")
}

// ---------------------------------------------------------------------------
// RPC method dispatch
// ---------------------------------------------------------------------------

// dispatchMethod handles RPC methods from authenticated clients.
func (s *Server) dispatchMethod(ctx context.Context, conn *WSConn, method WSMethod, params json.RawMessage) (any, *WSError) {
	scope := wsConnectionAuthScope(conn)
	switch method {
	case WSMethodPing:
		return map[string]string{"pong": "ok"}, nil

	case WSMethodStatus:
		return s.wsStatus(), nil

	case WSMethodRunsSubmit:
		return s.wsRunsSubmit(ctx, scope, params)

	case WSMethodInteractionsSubmit:
		return s.wsInteractionsSubmit(ctx, scope, params)

	case WSMethodRunsList:
		return s.wsRunsList(ctx, scope, params)

	case WSMethodRunsCancel:
		return s.wsRunsCancel(ctx, scope, params)

	case WSMethodSessionsList:
		return s.wsSessionsList(ctx, scope)

	case WSMethodSessionsGet:
		return s.wsSessionsGet(ctx, scope, params)

	case WSMethodApprovalsList:
		return s.wsApprovalsList(ctx, scope, params)

	case WSMethodApprovalsResolve:
		return s.wsApprovalsResolve(ctx, scope, params)

	case WSMethodEventsList:
		return s.wsEventsList(params)

	default:
		return nil, &WSError{
			Code:    WSErrNotFound,
			Message: fmt.Sprintf("unknown method %q", method),
		}
	}
}

// ---------------------------------------------------------------------------
// RPC method implementations
// ---------------------------------------------------------------------------

type wsStatusResponse struct {
	OK          bool `json:"ok"`
	Connections int  `json:"connections"`
}

func (s *Server) wsStatus() wsStatusResponse {
	conns := 0
	if s.wsHub != nil {
		conns = s.wsHub.ConnectionCount()
	}
	return wsStatusResponse{OK: true, Connections: conns}
}

func wsConnectionAuthScope(conn *WSConn) requestAuthScope {
	if conn == nil {
		return requestAuthScope{}
	}
	return conn.authScope
}

type wsRunsSubmitParams struct {
	SessionKey    string                       `json:"session_key"`
	Content       string                       `json:"content"`
	ContentBlocks []contextengine.ContentBlock `json:"content_blocks,omitempty"`
	Images        []string                     `json:"images,omitempty"`
	Model         string                       `json:"model,omitempty"`
	AutomationID  string                       `json:"automation_id,omitempty"`
	Metadata      map[string]any               `json:"metadata,omitempty"`
	Execute       *bool                        `json:"execute,omitempty"`
}

func (s *Server) wsRunsSubmit(ctx context.Context, scope requestAuthScope, params json.RawMessage) (any, *WSError) {
	var p wsRunsSubmitParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "invalid params"}
	}
	if strings.TrimSpace(p.SessionKey) == "" || (strings.TrimSpace(p.Content) == "" && len(p.ContentBlocks) == 0 && len(p.Images) == 0) {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "session_key and content, content_blocks, or images are required"}
	}
	resolvedAutomationID, err := scope.resolveAutomationID(p.AutomationID)
	if err != nil {
		return nil, &WSError{Code: WSErrUnauthorized, Message: err.Error()}
	}
	run, err := s.runtime.Submit(ctx, runtimesvc.SubmitRequest{
		SessionKey:    p.SessionKey,
		Content:       p.Content,
		ContentBlocks: append([]contextengine.ContentBlock(nil), p.ContentBlocks...),
		Images:        append([]string(nil), p.Images...),
		Model:         p.Model,
		AutomationID:  resolvedAutomationID,
		Metadata:      p.Metadata,
		Execute:       p.Execute,
	})
	if err != nil {
		return nil, mapToWSError(err)
	}
	return run, nil
}

type wsInteractionsSubmitParams struct {
	SessionKey         string                         `json:"session_key"`
	Content            string                         `json:"content"`
	Input              string                         `json:"input,omitempty"`
	ContentBlocks      []contextengine.ContentBlock   `json:"content_blocks,omitempty"`
	Images             []string                       `json:"images,omitempty"`
	Model              string                         `json:"model,omitempty"`
	AutomationID       string                         `json:"automation_id,omitempty"`
	Metadata           map[string]any                 `json:"metadata,omitempty"`
	StructuredCommand  *runtimesvc.StructuredCommand  `json:"structured_command,omitempty"`
	StructuredApproval *runtimesvc.StructuredApproval `json:"structured_approval,omitempty"`
}

func (s *Server) wsInteractionsSubmit(ctx context.Context, scope requestAuthScope, params json.RawMessage) (any, *WSError) {
	var p wsInteractionsSubmitParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "invalid params"}
	}
	if strings.TrimSpace(p.Content) == "" && strings.TrimSpace(p.Input) != "" {
		p.Content = p.Input
	}
	if strings.TrimSpace(p.SessionKey) == "" || (strings.TrimSpace(p.Content) == "" && len(p.ContentBlocks) == 0 && len(p.Images) == 0 && p.StructuredCommand == nil && p.StructuredApproval == nil) {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "session_key and content, content_blocks, images, or structured action are required"}
	}
	resolvedAutomationID, err := scope.resolveAutomationID(p.AutomationID)
	if err != nil {
		return nil, &WSError{Code: WSErrUnauthorized, Message: err.Error()}
	}
	result, err := s.runtime.Interact(ctx, runtimesvc.InteractionRequest{
		SessionKey:         p.SessionKey,
		Content:            p.Content,
		ContentBlocks:      append([]contextengine.ContentBlock(nil), p.ContentBlocks...),
		Images:             append([]string(nil), p.Images...),
		Model:              p.Model,
		AutomationID:       resolvedAutomationID,
		Metadata:           p.Metadata,
		StructuredCommand:  p.StructuredCommand,
		StructuredApproval: p.StructuredApproval,
	})
	if err != nil {
		return nil, mapToWSError(err)
	}
	return interactResponse{
		InteractionResult: result,
		Message:           runtimesvc.RenderDirectInteractionReply(result, effectiveInteractReplyContent(p.Content, p.ContentBlocks)),
	}, nil
}

type wsRunsListParams struct {
	SessionID string `json:"session_id,omitempty"`
	Status    string `json:"status,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Include   string `json:"include,omitempty"`
}

func (s *Server) wsRunsList(ctx context.Context, scope requestAuthScope, params json.RawMessage) (any, *WSError) {
	var p wsRunsListParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &WSError{Code: WSErrInvalidRequest, Message: "invalid params"}
		}
	}
	items, err := s.runtime.ListRunViews(ctx, agent.RunListFilter{
		SessionID: p.SessionID,
		Status:    agent.RunStatus(p.Status),
		Scope:     scope.scopeFilter(),
		Limit:     p.Limit,
	}, runtimesvc.RunListViewOptions{
		IncludeVerification: queryIncludes(p.Include, "verification"),
	})
	if err != nil {
		return nil, mapToWSError(err)
	}
	return countedListResponse{Items: items, Count: len(items)}, nil
}

type wsRunsCancelParams struct {
	ID string `json:"id"`
}

func (s *Server) wsRunsCancel(ctx context.Context, scope requestAuthScope, params json.RawMessage) (any, *WSError) {
	var p wsRunsCancelParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "invalid params"}
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "id is required"}
	}
	if _, err := s.runtime.GetRunScoped(ctx, p.ID, scope.scopeFilter()); err != nil {
		return nil, mapToWSError(err)
	}
	run, err := s.runtime.CancelRun(ctx, p.ID)
	if err != nil {
		return nil, mapToWSError(err)
	}
	return run, nil
}

func (s *Server) wsSessionsList(ctx context.Context, scope requestAuthScope) (any, *WSError) {
	sessions, err := s.runtime.ListSessionsFiltered(ctx, agent.SessionListFilter{
		Scope: scope.scopeFilter(),
	})
	if err != nil {
		return nil, mapToWSError(err)
	}
	return countedListResponse{Items: sessions, Count: len(sessions)}, nil
}

type wsSessionsGetParams struct {
	ID string `json:"id"`
}

func (s *Server) wsSessionsGet(ctx context.Context, scope requestAuthScope, params json.RawMessage) (any, *WSError) {
	var p wsSessionsGetParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "invalid params"}
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "id is required"}
	}
	sess, err := s.runtime.GetSessionScoped(ctx, p.ID, scope.scopeFilter())
	if err != nil {
		return nil, mapToWSError(err)
	}
	return sess, nil
}

type wsApprovalsListParams struct {
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

func (s *Server) wsApprovalsList(ctx context.Context, scope requestAuthScope, params json.RawMessage) (any, *WSError) {
	var p wsApprovalsListParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &WSError{Code: WSErrInvalidRequest, Message: "invalid params"}
		}
	}
	if p.Limit < 0 || p.Offset < 0 {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "limit and offset must be non-negative"}
	}
	limit := p.Limit
	if limit == 0 {
		limit = approvalListDefaultLimit
	}
	if limit > approvalListMaxLimit {
		limit = approvalListMaxLimit
	}
	items, err := s.runtime.ListApprovalViewsFiltered(ctx, approval.ListFilter{
		Status: approval.Status(p.Status),
		Limit:  limit,
		Offset: p.Offset,
	}, scope.scopeFilter())
	if err != nil {
		return nil, mapToWSError(err)
	}
	return listResponse{Items: items, Count: len(items)}, nil
}

type wsApprovalsResolveParams struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	ResolvedBy string `json:"resolved_by,omitempty"`
	Note       string `json:"note,omitempty"`
}

func (s *Server) wsApprovalsResolve(ctx context.Context, scope requestAuthScope, params json.RawMessage) (any, *WSError) {
	var p wsApprovalsResolveParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "invalid params"}
	}
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Status) == "" {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "id and status are required"}
	}
	ticket, err := s.runtime.ResolveApprovalViewScoped(ctx, p.ID, scope.scopeFilter(), approval.Resolution{
		Status:     approval.Status(p.Status),
		ResolvedBy: p.ResolvedBy,
		Note:       p.Note,
	})
	if err != nil {
		return nil, mapToWSError(err)
	}
	return ticket, nil
}

type wsEventsListParams struct {
	Limit     int    `json:"limit,omitempty"`
	Since     string `json:"since,omitempty"`
	Type      string `json:"type,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

type wsEventCursorResponse struct {
	Events       []runtimesvc.EventView `json:"events"`
	CursorStatus eventbus.CursorStatus  `json:"cursor_status"`
	NextCursor   string                 `json:"next_cursor,omitempty"`
}

func (s *Server) wsEventsList(params json.RawMessage) (any, *WSError) {
	var p wsEventsListParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &WSError{Code: WSErrInvalidRequest, Message: "invalid params"}
		}
	}
	if p.Limit < 0 {
		return nil, &WSError{Code: WSErrInvalidRequest, Message: "limit must be non-negative"}
	}
	limit := s.config.MaxEventResults
	if p.Limit > 0 {
		limit = p.Limit
	}
	filter := runtimesvc.EventFilter{
		Type:      eventbus.EventType(strings.TrimSpace(p.Type)),
		RunID:     strings.TrimSpace(p.RunID),
		SessionID: strings.TrimSpace(p.SessionID),
	}
	if strings.TrimSpace(p.Since) != "" {
		result := s.runtime.EventsSinceFiltered(p.Since, filter, limit)
		return wsEventCursorResponse{
			Events:       runtimesvc.ProjectEventViews(result.Events),
			CursorStatus: result.Status,
			NextCursor:   result.NextCursor,
		}, nil
	}
	items := s.runtime.EventSnapshotFiltered(filter, limit)
	return listResponse{Items: runtimesvc.ProjectEventViews(items)}, nil
}

// ---------------------------------------------------------------------------
// Error mapping
// ---------------------------------------------------------------------------

func mapToWSError(err error) *WSError {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if err == runtimesvc.ErrRateLimited {
		return &WSError{Code: WSErrRateLimited, Message: msg}
	}
	switch usererror.Code(err) {
	case apiresponse.ErrorCodeNotFound:
		return &WSError{Code: WSErrNotFound, Message: msg}
	case apiresponse.ErrorCodeRateLimited:
		return &WSError{Code: WSErrRateLimited, Message: msg}
	case apiresponse.ErrorCodeInvalidArgument,
		apiresponse.ErrorCodeConflict,
		apiresponse.ErrorCodeAuthorizationDenied:
		return &WSError{Code: WSErrInvalidRequest, Message: msg}
	default:
		return &WSError{Code: WSErrInternal, Message: msg}
	}
}
