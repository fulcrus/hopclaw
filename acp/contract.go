package acp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type ACPErrorData struct {
	Code    string         `json:"code"`
	Details map[string]any `json:"details,omitempty"`
}

const (
	acpErrorParse                      = "acp.parse_error"
	acpErrorInvalidRequest             = "acp.invalid_request"
	acpErrorMethodNotFound             = "acp.method_not_found"
	acpErrorInvalidParams              = "acp.invalid_params"
	acpErrorInternal                   = "acp.internal_error"
	acpErrorProtocolVersionUnsupported = "acp.protocol_version_unsupported"
	acpErrorCapabilityUnsupported      = "acp.capability_unsupported"
	acpErrorSessionNotFound            = "acp.session_not_found"
)

type protocolMessageError struct {
	code    int
	message string
	data    *ACPErrorData
	err     error
}

func (e *protocolMessageError) Error() string {
	if e == nil {
		return ""
	}
	if e.err == nil {
		return e.message
	}
	if e.message == "" {
		return e.err.Error()
	}
	return fmt.Sprintf("%s: %v", e.message, e.err)
}

func newProtocolError(code int, message, acpCode string, details map[string]any) *protocolMessageError {
	var data *ACPErrorData
	if strings.TrimSpace(acpCode) != "" || len(details) > 0 {
		data = &ACPErrorData{
			Code:    strings.TrimSpace(acpCode),
			Details: cloneDetails(details),
		}
	}
	return &protocolMessageError{
		code:    code,
		message: strings.TrimSpace(message),
		data:    data,
	}
}

func newParseError(message string, details map[string]any, err error) *protocolMessageError {
	perr := newProtocolError(errCodeParse, message, acpErrorParse, details)
	perr.err = err
	return perr
}

func newInvalidRequestError(message string, details map[string]any, err error) *protocolMessageError {
	perr := newProtocolError(errCodeInvalidRequest, message, acpErrorInvalidRequest, details)
	perr.err = err
	return perr
}

func newInvalidParamsError(message string, details map[string]any, err error) *protocolMessageError {
	perr := newProtocolError(errCodeInvalidParams, message, acpErrorInvalidParams, details)
	perr.err = err
	return perr
}

func newMethodNotFoundError(method string) *protocolMessageError {
	return newProtocolError(
		errCodeMethodNotFound,
		fmt.Sprintf("unknown method: %s", strings.TrimSpace(method)),
		acpErrorMethodNotFound,
		map[string]any{"method": strings.TrimSpace(method)},
	)
}

func newInternalProtocolError(message string, err error) *protocolMessageError {
	perr := newProtocolError(errCodeInternal, message, acpErrorInternal, nil)
	perr.err = err
	return perr
}

func newProtocolVersionUnsupportedError(version string) *protocolMessageError {
	return newProtocolError(
		errCodeInvalidParams,
		fmt.Sprintf("unsupported protocol_version %q", strings.TrimSpace(version)),
		acpErrorProtocolVersionUnsupported,
		map[string]any{
			"protocol_version":   strings.TrimSpace(version),
			"supported_versions": []string{protocolVersion},
		},
	)
}

func newCapabilityUnsupportedError(path string) *protocolMessageError {
	return newProtocolError(
		errCodeInvalidParams,
		fmt.Sprintf("unsupported required capability %q", strings.TrimSpace(path)),
		acpErrorCapabilityUnsupported,
		map[string]any{"capability": strings.TrimSpace(path)},
	)
}

func newSessionNotFoundError(sessionID string) *protocolMessageError {
	return newProtocolError(
		errCodeInvalidParams,
		fmt.Sprintf("session %s not found", strings.TrimSpace(sessionID)),
		acpErrorSessionNotFound,
		map[string]any{"session_id": strings.TrimSpace(sessionID)},
	)
}

func strictDecodeJSON(data []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("unexpected trailing JSON content")
	}
	return nil
}

func strictDecodeParams(raw json.RawMessage, dest any) *protocolMessageError {
	payload := raw
	if len(bytes.TrimSpace(payload)) == 0 {
		payload = []byte("{}")
	}
	if err := strictDecodeJSON(payload, dest); err != nil {
		return newInvalidParamsError("invalid params", decodeErrorDetails(err), err)
	}
	return nil
}

func decodeJSONRPCMessage(line []byte) (*JSONRPCMessage, *protocolMessageError) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil, newInvalidRequestError("request body must not be empty", nil, nil)
	}
	if !json.Valid(trimmed) {
		return nil, newParseError("invalid JSON", nil, nil)
	}
	if trimmed[0] != '{' {
		return nil, newInvalidRequestError("request must be a JSON object", nil, nil)
	}

	var fields map[string]json.RawMessage
	if err := strictDecodeJSON(trimmed, &fields); err != nil {
		return nil, newInvalidRequestError("invalid JSON-RPC message", decodeErrorDetails(err), err)
	}

	msg := &JSONRPCMessage{}
	var unknownField string
	for key, raw := range fields {
		switch key {
		case "jsonrpc":
			if err := strictDecodeJSON(raw, &msg.JSONRPC); err != nil {
				return msg, newInvalidRequestError("invalid JSON-RPC message", decodeErrorDetails(err), err)
			}
		case "id":
			id, err := decodeJSONRPCID(raw)
			msg.HasID = true
			msg.ID = id
			if err != nil {
				return msg, newInvalidRequestError("invalid JSON-RPC message", decodeErrorDetails(err), err)
			}
		case "method":
			if err := strictDecodeJSON(raw, &msg.Method); err != nil {
				return msg, newInvalidRequestError("invalid JSON-RPC message", decodeErrorDetails(err), err)
			}
		case "params":
			msg.Params = append(json.RawMessage(nil), raw...)
		case "result":
			msg.Result = append(json.RawMessage(nil), raw...)
		case "error":
			var rpcErr JSONRPCError
			if err := strictDecodeJSON(raw, &rpcErr); err != nil {
				return msg, newInvalidRequestError("invalid JSON-RPC message", decodeErrorDetails(err), err)
			}
			msg.Error = &rpcErr
		default:
			if strings.TrimSpace(unknownField) == "" {
				unknownField = key
			}
		}
	}
	if strings.TrimSpace(unknownField) != "" {
		return msg, newInvalidRequestError("invalid JSON-RPC message", map[string]any{
			"field":  strings.TrimSpace(unknownField),
			"reason": fmt.Sprintf("json: unknown field %q", strings.TrimSpace(unknownField)),
		}, nil)
	}
	if strings.TrimSpace(msg.JSONRPC) != jsonrpcVersion {
		return msg, newInvalidRequestError(`jsonrpc must equal "2.0"`, map[string]any{"jsonrpc": msg.JSONRPC}, nil)
	}
	return msg, nil
}

func validateInboundRequest(msg *JSONRPCMessage) *protocolMessageError {
	if msg == nil {
		return newInvalidRequestError("request is required", nil, nil)
	}
	if strings.TrimSpace(msg.Method) == "" {
		return newInvalidRequestError("method is required", nil, nil)
	}
	if len(bytes.TrimSpace(msg.Result)) > 0 || msg.Error != nil {
		return newInvalidRequestError("requests must not include result or error", nil, nil)
	}
	if !validJSONRPCID(msg.ID) {
		return newInvalidRequestError("id must be a string, number, or null", nil, nil)
	}
	return nil
}

func validJSONRPCID(id any) bool {
	switch id.(type) {
	case nil, string, float64, int, int32, int64, uint, uint32, uint64, json.Number:
		return true
	default:
		return false
	}
}

func decodeJSONRPCID(raw json.RawMessage) (any, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var id any
	if err := decoder.Decode(&id); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, fmt.Errorf("unexpected trailing JSON content")
	}
	return id, nil
}

func negotiatedCapabilities(commandsUpdate bool) map[string]any {
	return map[string]any{
		"streaming":   true,
		"permissions": true,
		"commands":    true,
		"prompt": map[string]any{
			"message":             true,
			"images":              true,
			"content_blocks":      true,
			"structured_command":  true,
			"structured_approval": true,
			"model":               true,
		},
		"sessions": map[string]any{
			"new":               true,
			"load":              true,
			"list":              true,
			"cancel":            true,
			"set_mode":          true,
			"set_config_option": true,
		},
		"notifications": map[string]any{
			"session_update":     true,
			"permission_request": true,
			"commands_update":    commandsUpdate,
		},
		"protocol_versions": []string{protocolVersion},
	}
}

func unsupportedRequestedCapabilities(requested, supported map[string]any) []string {
	if len(requested) == 0 {
		return nil
	}
	required := make([]string, 0, len(requested))
	collectRequiredCapabilityPaths("", requested, &required)
	if len(required) == 0 {
		return nil
	}
	sort.Strings(required)
	out := make([]string, 0, len(required))
	for _, path := range required {
		ok, known := capabilityAtPath(supported, strings.Split(path, "."))
		if known && ok {
			continue
		}
		out = append(out, path)
	}
	return out
}

func invalidCapabilityShape(prefix string, value any) (string, string) {
	switch typed := value.(type) {
	case bool:
		return "", ""
	case map[string]any:
		for key, child := range typed {
			key = strings.TrimSpace(key)
			if key == "" {
				if prefix == "" {
					return "capabilities", "empty key"
				}
				return prefix, "empty key"
			}
			next := "capabilities." + key
			if prefix != "" {
				next = prefix + "." + key
			}
			if badPath, reason := invalidCapabilityShape(next, child); badPath != "" {
				return badPath, reason
			}
		}
		return "", ""
	case nil:
		if prefix == "" {
			return "capabilities", "null"
		}
		return prefix, "null"
	default:
		if prefix == "" {
			return "capabilities", fmt.Sprintf("%T", value)
		}
		return prefix, fmt.Sprintf("%T", value)
	}
}

func collectRequiredCapabilityPaths(prefix string, value any, out *[]string) {
	switch typed := value.(type) {
	case bool:
		if typed && strings.TrimSpace(prefix) != "" {
			*out = append(*out, prefix)
		}
	case map[string]any:
		for key, child := range typed {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			collectRequiredCapabilityPaths(next, child, out)
		}
	}
}

func capabilityAtPath(source map[string]any, path []string) (bool, bool) {
	if len(path) == 0 {
		return false, false
	}
	var current any = source
	for _, segment := range path {
		node, ok := current.(map[string]any)
		if !ok {
			return false, false
		}
		value, ok := node[segment]
		if !ok {
			return false, false
		}
		current = value
	}
	enabled, ok := current.(bool)
	return enabled, ok
}

func decodeErrorDetails(err error) map[string]any {
	if err == nil {
		return nil
	}
	details := map[string]any{"reason": err.Error()}
	if field := unknownFieldName(err.Error()); field != "" {
		details["field"] = field
	}
	return details
}

func unknownFieldName(message string) string {
	const prefix = "json: unknown field "
	index := strings.Index(message, prefix)
	if index < 0 {
		return ""
	}
	field := strings.TrimPrefix(message[index:], prefix)
	field = strings.TrimSpace(field)
	field = strings.Trim(field, `"`)
	return field
}

func cloneDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	out := make(map[string]any, len(details))
	for key, value := range details {
		out[key] = value
	}
	return out
}
