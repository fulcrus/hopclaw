package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("acp")

const (
	// protocolVersion is the ACP protocol version negotiated during handshake.
	protocolVersion = "2024-11-05"
	// serverName identifies this server implementation.
	serverName = "hopclaw"
	// serverVersion is the current version of the ACP server.
	serverVersion = "0.1.0"
	// promptTimeout is the maximum duration for a single prompt execution.
	promptTimeout = 10 * time.Minute
)

// ---------------------------------------------------------------------------
// GatewayClient interface
// ---------------------------------------------------------------------------

// GatewayClient abstracts the HopClaw gateway operations needed by the ACP
// server. Implementations connect to the gateway via HTTP or in-process.
type GatewayClient interface {
	SubmitRun(ctx context.Context, sessionKey, message string, images []string) (runID string, events <-chan RunEvent, err error)
	CancelRun(ctx context.Context, sessionKey, runID string) error
	ListSessions(ctx context.Context, limit, offset int) ([]SessionInfo, error)
	ResolveSession(ctx context.Context, key string) (*SessionInfo, error)
	ResetSession(ctx context.Context, key string) error
}

type PromptGatewayClient interface {
	SubmitRunWithOptions(ctx context.Context, sessionKey, message string, images []string, options PromptOptions) (runID string, events <-chan RunEvent, err error)
}

type ApprovalGatewayClient interface {
	ResolveApproval(ctx context.Context, requestID string, resolution approval.Resolution) error
}

// RunEvent represents a single streaming event from the gateway.
type RunEvent struct {
	Type          string // "text_delta", "tool_start", "tool_delta", "tool_end", "complete", "error"
	Text          string
	ToolName      string
	ToolInput     string
	ToolOutput    string
	Usage         *UsageInfo
	ModelFailover *ModelFailoverInfo
	Error         string
	StopReason    StopReason
	Permission    *PermissionRequest
	Runless       bool
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// Server implements the ACP agent-side protocol. It reads JSON-RPC requests
// from a Transport, dispatches them to handler methods, and streams
// notifications back to the client.
type Server struct {
	gateway           GatewayClient
	transport         *Transport
	sessions          *SessionStore
	permissionHandler *PermissionHandler
	promptLaunchMu    sync.Mutex
	promptLaunches    map[string]chan bool
	nextID            atomic.Int64
	done              chan struct{}

	// config
	defaultSessionKey string
	defaultCWD        string
	prefixCWD         bool
	commandProvider   func(context.Context) ([]Command, error)
}

// ServerConfig holds configuration for a new ACP Server.
type ServerConfig struct {
	DefaultSessionKey string
	DefaultCWD        string
	PrefixCWD         bool
	CommandProvider   func(context.Context) ([]Command, error)
}

// NewServer creates an ACP Server backed by the given GatewayClient.
func NewServer(gateway GatewayClient, cfg ServerConfig) *Server {
	return &Server{
		gateway:           gateway,
		sessions:          NewSessionStore(),
		promptLaunches:    make(map[string]chan bool),
		done:              make(chan struct{}),
		defaultSessionKey: cfg.DefaultSessionKey,
		defaultCWD:        cfg.DefaultCWD,
		prefixCWD:         cfg.PrefixCWD,
		commandProvider:   cfg.CommandProvider,
	}
}

// Serve runs the main read-dispatch loop over the provided stdio streams.
// It blocks until ctx is cancelled, the reader returns io.EOF, or an
// unrecoverable error occurs.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	s.transport = NewTransport(r, w)
	s.permissionHandler = NewPermissionHandler(s.transport)
	defer s.transport.Close()
	defer s.sessions.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := s.transport.receiveLine()
		if err != nil {
			// EOF means the client disconnected normally.
			if isEOF(err) {
				return nil
			}
			return fmt.Errorf("acp: receive error: %w", err)
		}

		msg, perr := decodeJSONRPCMessage(line)
		if perr != nil {
			s.sendProtocolError(protocolResponseID(msg), perr)
			continue
		}
		if perr := validateInboundRequest(msg); perr != nil {
			s.sendProtocolError(protocolResponseID(msg), perr)
			continue
		}

		s.dispatch(ctx, msg)
	}
}

// dispatch routes an incoming message to the appropriate handler.
func (s *Server) dispatch(ctx context.Context, msg *JSONRPCMessage) {
	if !msg.HasID {
		s.handleNotification(ctx, msg)
		return
	}

	var reply *JSONRPCMessage
	switch msg.Method {
	case "initialize":
		reply = s.handleInitialize(ctx, msg.ID, msg.Params)
	case "acp/newSession":
		reply = s.handleNewSession(ctx, msg.ID, msg.Params)
	case "acp/loadSession":
		reply = s.handleLoadSession(ctx, msg.ID, msg.Params)
	case "acp/prompt":
		reply = s.handlePrompt(ctx, msg.ID, msg.Params)
	case "acp/cancel":
		reply = s.handleCancel(ctx, msg.ID, msg.Params)
	case "acp/listSessions":
		reply = s.handleListSessions(ctx, msg.ID, msg.Params)
	case "acp/setMode":
		reply = s.handleSetMode(ctx, msg.ID, msg.Params)
	case "acp/setConfigOption":
		reply = s.handleSetConfigOption(ctx, msg.ID, msg.Params)
	case "acp/permissionResponse":
		reply = s.handlePermissionResponse(ctx, msg.ID, msg.Params)
	default:
		reply = newProtocolErrorResponse(msg.ID, newMethodNotFoundError(msg.Method))
	}

	if reply != nil {
		sendErr := s.transport.Send(reply)
		if sendErr != nil {
			log.Warn("acp: failed to send response", "error", sendErr)
		}
		if msg.Method == "acp/prompt" {
			s.releasePromptLaunch(msg.ID, sendErr == nil && reply.Error == nil)
		}
	}

	switch msg.Method {
	case "initialize", "acp/newSession", "acp/loadSession":
		if reply != nil && reply.Error != nil {
			return
		}
		s.sendCommandsUpdateIfConfigured(ctx)
	}
}

func (s *Server) handleNotification(ctx context.Context, msg *JSONRPCMessage) {
	switch msg.Method {
	case "acp/permissionResponse":
		resp, perr := s.decodePermissionResponse(msg.Params)
		if perr != nil {
			log.Warn("acp: rejected malformed permission response notification", "error", perr.Error())
			return
		}
		if perr := s.applyPermissionResponse(ctx, resp); perr != nil {
			log.Warn("acp: failed to apply permission response notification", "error", perr.Error())
		}
	default:
		log.Warn("acp: ignoring unsupported notification", "method", msg.Method)
	}
}

// ---------------------------------------------------------------------------
// Request handlers
// ---------------------------------------------------------------------------

func (s *Server) handleInitialize(_ context.Context, id any, params json.RawMessage) *JSONRPCMessage {
	var p InitializeParams
	if perr := strictDecodeParams(params, &p); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}
	capabilities := s.supportedCapabilities()
	if perr := p.Validate(capabilities); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}

	result := InitializeResult{
		ProtocolVersion: protocolVersion,
		ServerInfo: Implementation{
			Name:    serverName,
			Version: serverVersion,
		},
		Capabilities: capabilities,
	}
	return newResultResponse(id, result)
}

func (s *Server) handleNewSession(_ context.Context, id any, params json.RawMessage) *JSONRPCMessage {
	var p NewSessionParams
	if perr := strictDecodeParams(params, &p); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}

	sessionID := p.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("acp-%d", s.nextID.Add(1))
	}

	gatewayKey := p.SessionKey
	if gatewayKey == "" {
		gatewayKey = s.defaultSessionKey
	}
	if gatewayKey == "" {
		gatewayKey = sessionID
	}

	sess := s.sessions.GetOrCreate(sessionID, gatewayKey, s.defaultCWD)
	info := SessionInfo{
		SessionID:  sess.ID,
		SessionKey: sess.GatewayKey,
		Status:     SessionIdle,
		CreatedAt:  sess.CreatedAt,
	}
	return newResultResponse(id, info)
}

func (s *Server) handleLoadSession(ctx context.Context, id any, params json.RawMessage) *JSONRPCMessage {
	var p LoadSessionParams
	if perr := strictDecodeParams(params, &p); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}
	if perr := p.Validate(); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}

	resolved, err := s.gateway.ResolveSession(ctx, p.SessionKey)
	if err != nil {
		return newProtocolErrorResponse(id, newInternalProtocolError("failed to resolve session", err))
	}

	sessionID := p.SessionID
	if sessionID == "" {
		sessionID = resolved.SessionID
	}

	sess := s.sessions.GetOrCreate(sessionID, resolved.SessionKey, s.defaultCWD)
	info := SessionInfo{
		SessionID:  sess.ID,
		SessionKey: sess.GatewayKey,
		Status:     resolved.Status,
		CreatedAt:  sess.CreatedAt,
	}
	return newResultResponse(id, info)
}

func (s *Server) handlePrompt(ctx context.Context, id any, params json.RawMessage) *JSONRPCMessage {
	var p PromptParams
	if perr := strictDecodeParams(params, &p); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}
	if perr := p.Validate(); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}

	sess, ok := s.sessions.Get(p.SessionID)
	if !ok {
		return newProtocolErrorResponse(id, newSessionNotFoundError(p.SessionID))
	}
	s.sessions.Touch(p.SessionID)

	promptCtx, cancel := context.WithTimeout(ctx, promptTimeout)
	start := s.registerPromptLaunch(id)
	go func() {
		allowed, ok := <-start
		if !ok || !allowed {
			cancel()
			return
		}
		s.streamRun(promptCtx, cancel, sess, p)
	}()

	// Acknowledge the prompt submission.
	return newResultResponse(id, map[string]string{
		"session_id": p.SessionID,
		"status":     "accepted",
	})
}

func (s *Server) registerPromptLaunch(id any) chan bool {
	ch := make(chan bool, 1)
	key := promptLaunchKey(id)
	if key == "" {
		ch <- true
		return ch
	}
	s.promptLaunchMu.Lock()
	if existing := s.promptLaunches[key]; existing != nil {
		delete(s.promptLaunches, key)
		existing <- false
		close(existing)
	}
	s.promptLaunches[key] = ch
	s.promptLaunchMu.Unlock()
	return ch
}

func (s *Server) releasePromptLaunch(id any, allowed bool) {
	key := promptLaunchKey(id)
	if key == "" {
		return
	}
	s.promptLaunchMu.Lock()
	ch := s.promptLaunches[key]
	delete(s.promptLaunches, key)
	s.promptLaunchMu.Unlock()
	if ch == nil {
		return
	}
	ch <- allowed
	close(ch)
}

func promptLaunchKey(id any) string {
	if id == nil {
		return ""
	}
	data, err := json.Marshal(id)
	if err != nil {
		return strings.TrimSpace(fmt.Sprintf("%T:%v", id, id))
	}
	return strings.TrimSpace(string(data))
}

// streamRun submits a run to the gateway and relays events as session update
// notifications.
func (s *Server) streamRun(ctx context.Context, cancel context.CancelFunc, sess *session, p PromptParams) {
	defer cancel()

	options := PromptOptions{
		Model:              p.Model,
		ContentBlocks:      append([]contextengine.ContentBlock(nil), p.ContentBlocks...),
		StructuredCommand:  cloneStructuredCommand(p.StructuredCommand),
		StructuredApproval: cloneStructuredApproval(p.StructuredApproval),
	}

	var (
		runID  string
		events <-chan RunEvent
		err    error
	)
	if withOptions, ok := s.gateway.(PromptGatewayClient); ok {
		runID, events, err = withOptions.SubmitRunWithOptions(ctx, sess.GatewayKey, p.Message, p.Images, options)
	} else {
		runID, events, err = s.gateway.SubmitRun(ctx, sess.GatewayKey, p.Message, p.Images)
	}
	if err != nil {
		logging.LogIfErr(ctx, s.sendSessionUpdate(SessionUpdateNotification{
			SessionID: p.SessionID,
			Status:    SessionError,
			Error:     err.Error(),
		}), "send session update failed")
		return
	}

	s.sessions.SetActiveRun(p.SessionID, runID, cancel)
	defer s.sessions.ClearActiveRunIfMatch(p.SessionID, runID)
	s.sendCommandsUpdateIfConfigured(ctx)

	lastStatus := SessionIdle
	var pendingUsage *UsageInfo

	for event := range events {
		update := SessionUpdateNotification{
			SessionID: p.SessionID,
			RunID:     runID,
			Runless:   event.Runless,
		}

		switch event.Type {
		case "text_delta":
			update.Status = SessionStreaming
			update.TextDelta = event.Text
		case "tool_start":
			update.Status = SessionToolUse
			update.ToolName = event.ToolName
			update.ToolInput = event.ToolInput
		case "tool_delta":
			update.Status = SessionToolUse
			update.ToolName = event.ToolName
			update.ToolOutput = event.ToolOutput
		case "tool_end":
			update.Status = SessionStreaming
			update.ToolName = event.ToolName
			update.ToolOutput = event.ToolOutput
		case "permission_request":
			if event.Permission == nil {
				continue
			}
			if err := s.sendPermissionRequest(*event.Permission); err != nil {
				log.Warn("acp: failed to send permission request", "error", err)
			}
			continue
		case "usage":
			pendingUsage = cloneUsageInfo(event.Usage)
			continue
		case "model_failover":
			update.Status = SessionStreaming
			update.ModelFailover = cloneModelFailoverInfo(event.ModelFailover)
		case "complete":
			update.Status = SessionCompleted
			update.StopReason = event.StopReason
			if event.Usage != nil {
				pendingUsage = cloneUsageInfo(event.Usage)
			}
			update.Usage = cloneUsageInfo(pendingUsage)
		case "error":
			update.Status = SessionError
			update.Error = event.Error
		default:
			continue
		}

		if err := s.sendSessionUpdate(update); err != nil {
			log.Warn("acp: failed to send session update", "error", err)
			return
		}
		if update.Status != lastStatus {
			lastStatus = update.Status
			s.sendCommandsUpdateIfConfigured(ctx)
		}
	}
}

func cloneStructuredCommand(cmd *StructuredCommand) *StructuredCommand {
	if cmd == nil {
		return nil
	}
	cloned := *cmd
	cloned.Kind = strings.TrimSpace(cloned.Kind)
	cloned.RunID = strings.TrimSpace(cloned.RunID)
	return &cloned
}

func cloneStructuredApproval(approval *StructuredApproval) *StructuredApproval {
	if approval == nil {
		return nil
	}
	cloned := *approval
	cloned.Action = strings.TrimSpace(cloned.Action)
	return &cloned
}

func cloneUsageInfo(info *UsageInfo) *UsageInfo {
	if info == nil {
		return nil
	}
	cloned := *info
	return &cloned
}

func cloneModelFailoverInfo(info *ModelFailoverInfo) *ModelFailoverInfo {
	if info == nil {
		return nil
	}
	cloned := *info
	return &cloned
}

func (s *Server) handleCancel(ctx context.Context, id any, params json.RawMessage) *JSONRPCMessage {
	var p CancelParams
	if perr := strictDecodeParams(params, &p); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}
	if perr := p.Validate(); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}

	// streamRun registers the active run via SetActiveRun *after* the
	// gateway's SubmitRunWithOptions returns; a Cancel that arrives in
	// that small window would otherwise be silently dropped (no active
	// run, nothing to cancel). Briefly wait for the active run to appear
	// before snapshotting so the in-flight prompt is actually cancelable.
	deadline := time.Now().Add(500 * time.Millisecond)
	var (
		gatewayKey   string
		activeRunID  string
		cancelFn     context.CancelFunc
		exists       bool
	)
	for {
		gatewayKey, activeRunID, cancelFn, exists = s.sessions.SnapshotActive(p.SessionID)
		if !exists {
			return newProtocolErrorResponse(id, newSessionNotFoundError(p.SessionID))
		}
		if activeRunID != "" || cancelFn != nil || !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return newProtocolErrorResponse(id, newInternalProtocolError("cancel context done", ctx.Err()))
		case <-time.After(2 * time.Millisecond):
		}
	}

	// Issue the gateway cancel outside the store lock so the in-flight run
	// goroutine has a chance to observe the cancellation and drain its event
	// stream before we tear down its context. Then cancel the run context
	// and clear the slot. ClearActiveRunIfMatch is a no-op if streamRun's
	// own deferred cleanup ran first, which keeps this safe under races.
	if activeRunID != "" {
		if err := s.gateway.CancelRun(ctx, gatewayKey, activeRunID); err != nil {
			return newProtocolErrorResponse(id, newInternalProtocolError("failed to cancel run", err))
		}
	}
	if cancelFn != nil {
		cancelFn()
	}
	s.sessions.ClearActiveRunIfMatch(p.SessionID, activeRunID)

	return newResultResponse(id, map[string]string{
		"session_id": p.SessionID,
		"status":     "cancelled",
	})
}

func (s *Server) handleListSessions(ctx context.Context, id any, params json.RawMessage) *JSONRPCMessage {
	var p ListSessionsParams
	if perr := strictDecodeParams(params, &p); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}
	if perr := p.Validate(); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}

	// Try the gateway first for persisted sessions.
	sessions, err := s.gateway.ListSessions(ctx, p.Limit, p.Offset)
	if err != nil {
		// Fall back to local store.
		sessions = s.sessions.List(p.Limit, p.Offset)
	}

	return newResultResponse(id, map[string]any{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

func (s *Server) handleSetMode(_ context.Context, id any, params json.RawMessage) *JSONRPCMessage {
	var p SetSessionModeParams
	if perr := strictDecodeParams(params, &p); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}
	if perr := p.Validate(); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}

	if _, ok := s.sessions.Get(p.SessionID); !ok {
		return newProtocolErrorResponse(id, newSessionNotFoundError(p.SessionID))
	}
	s.sessions.Touch(p.SessionID)
	s.sessions.SetConfigOption(p.SessionID, ConfigOptionKey("mode"), p.Mode)

	return newResultResponse(id, map[string]string{
		"session_id": p.SessionID,
		"mode":       p.Mode,
	})
}

func (s *Server) handleSetConfigOption(_ context.Context, id any, params json.RawMessage) *JSONRPCMessage {
	var p SetConfigOptionParams
	if perr := strictDecodeParams(params, &p); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}
	if perr := p.Validate(); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}

	if _, ok := s.sessions.Get(p.SessionID); !ok {
		return newProtocolErrorResponse(id, newSessionNotFoundError(p.SessionID))
	}
	s.sessions.Touch(p.SessionID)
	s.sessions.SetConfigOption(p.SessionID, p.Key, p.Value)

	return newResultResponse(id, map[string]string{
		"session_id": p.SessionID,
		"key":        string(p.Key),
		"value":      p.Value,
	})
}

func (s *Server) handlePermissionResponse(ctx context.Context, id any, params json.RawMessage) *JSONRPCMessage {
	resp, perr := s.decodePermissionResponse(params)
	if perr != nil {
		return newProtocolErrorResponse(id, perr)
	}
	if perr := s.applyPermissionResponse(ctx, resp); perr != nil {
		return newProtocolErrorResponse(id, perr)
	}
	return newResultResponse(id, map[string]string{
		"request_id": resp.RequestID,
		"status":     "resolved",
	})
}

func (s *Server) decodePermissionResponse(params json.RawMessage) (*PermissionResponse, *protocolMessageError) {
	type permissionResponsePayload struct {
		RequestID string `json:"request_id"`
		Approved  *bool  `json:"approved"`
		Scope     string `json:"scope,omitempty"`
		Reason    string `json:"reason,omitempty"`
	}

	var payload permissionResponsePayload
	if perr := strictDecodeParams(params, &payload); perr != nil {
		return nil, perr
	}
	if strings.TrimSpace(payload.RequestID) == "" {
		return nil, newInvalidParamsError("request_id is required", map[string]any{"field": "request_id"}, nil)
	}
	if payload.Approved == nil {
		return nil, newInvalidParamsError("approved is required", map[string]any{"field": "approved"}, nil)
	}
	resp := &PermissionResponse{
		RequestID: payload.RequestID,
		Approved:  *payload.Approved,
		Scope:     payload.Scope,
		Reason:    payload.Reason,
	}
	if perr := resp.Validate(); perr != nil {
		return nil, perr
	}
	return resp, nil
}

func (s *Server) applyPermissionResponse(ctx context.Context, resp *PermissionResponse) *protocolMessageError {
	if resp == nil {
		return newInvalidParamsError("permission response is required", nil, nil)
	}
	resolver, ok := s.gateway.(ApprovalGatewayClient)
	if !ok {
		return newInternalProtocolError("gateway does not support approval resolution", nil)
	}
	resolution := approval.Resolution{
		ResolvedBy: "acp",
		Note:       strings.TrimSpace(resp.Reason),
		Scope:      approval.Scope(strings.TrimSpace(resp.Scope)),
	}
	if resp.Approved {
		resolution.Status = approval.StatusApproved
		if strings.TrimSpace(resolution.Note) == "" {
			resolution.Note = "approved via acp"
		}
	} else {
		resolution.Status = approval.StatusDenied
		if strings.TrimSpace(resolution.Note) == "" {
			resolution.Note = "denied via acp"
		}
	}
	if err := resolver.ResolveApproval(ctx, resp.RequestID, resolution); err != nil {
		return newInternalProtocolError("failed to resolve approval", err)
	}
	if s.permissionHandler != nil {
		s.permissionHandler.HandleResponse(*resp)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Notifications (server -> client)
// ---------------------------------------------------------------------------

func (s *Server) sendSessionUpdate(update SessionUpdateNotification) error {
	return sendNotification(s.transport, "acp/sessionUpdate", update)
}

func (s *Server) sendCommandsUpdate(commands []Command) error {
	return sendNotification(s.transport, "acp/commandsUpdate", map[string]any{
		"commands": commands,
	})
}

func (s *Server) sendCommandsUpdateIfConfigured(ctx context.Context) {
	if s.commandProvider == nil {
		return
	}
	if err := s.sendCommandsUpdate(s.availableCommands(ctx)); err != nil {
		log.Warn("acp: failed to send commands update", "error", err)
	}
}

func (s *Server) sendPermissionRequest(req PermissionRequest) error {
	return sendNotification(s.transport, "acp/permissionRequest", req)
}

// availableCommands returns the current set of slash commands.
func (s *Server) availableCommands(ctx context.Context) []Command {
	commands := append([]Command(nil), defaultCommands()...)
	if s.commandProvider != nil {
		if commands, err := s.commandProvider(ctx); err == nil && len(commands) > 0 {
			seen := make(map[string]struct{}, len(commands)+len(defaultCommands()))
			out := make([]Command, 0, len(defaultCommands())+len(commands))
			for _, command := range commands {
				name := strings.TrimPrefix(strings.TrimSpace(command.Name), "/")
				if name == "" {
					continue
				}
				command.Name = name
				seen[name] = struct{}{}
				out = append(out, command)
			}
			for _, command := range defaultCommands() {
				if _, ok := seen[command.Name]; ok {
					continue
				}
				out = append(out, command)
			}
			return out
		}
	}
	return commands
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newResultResponse builds a JSON-RPC success response.
func newResultResponse(id any, result any) *JSONRPCMessage {
	data, err := json.Marshal(result)
	if err != nil {
		return newProtocolErrorResponse(id, newInternalProtocolError("failed to marshal result", err))
	}
	return &JSONRPCMessage{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		HasID:   true,
		Result:  data,
	}
}

// newErrorResponse builds a JSON-RPC error response.
func newErrorResponse(id any, code int, message string) *JSONRPCMessage {
	return newErrorResponseWithData(id, code, message, nil)
}

func newErrorResponseWithData(id any, code int, message string, data any) *JSONRPCMessage {
	return &JSONRPCMessage{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		HasID:   true,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

func newProtocolErrorResponse(id any, err *protocolMessageError) *JSONRPCMessage {
	if err == nil {
		return newErrorResponse(id, errCodeInternal, "internal error")
	}
	return newErrorResponseWithData(id, err.code, err.message, err.data)
}

func (s *Server) sendProtocolError(id any, err *protocolMessageError) {
	if s.transport == nil || err == nil {
		return
	}
	if sendErr := s.transport.Send(newProtocolErrorResponse(id, err)); sendErr != nil {
		log.Warn("acp: failed to send protocol error", "error", sendErr)
	}
}

func (s *Server) supportedCapabilities() map[string]any {
	return negotiatedCapabilities(s.commandProvider != nil)
}

// sendNotification writes a JSON-RPC notification (no ID) through the
// transport.
func sendNotification(t *Transport, method string, params any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("acp: failed to marshal notification params: %w", err)
	}
	return t.Send(&JSONRPCMessage{
		JSONRPC: jsonrpcVersion,
		HasID:   false,
		Method:  method,
		Params:  data,
	})
}

func protocolResponseID(msg *JSONRPCMessage) any {
	if msg == nil || !msg.HasID || !validJSONRPCID(msg.ID) {
		return nil
	}
	return msg.ID
}

// isEOF returns true if the error chain contains io.EOF.
func isEOF(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		(err != nil && err.Error() == "acp: failed to read message: EOF")
}
