package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type ResourceScope struct {
	PathPrefixes    []string            `json:"path_prefixes,omitempty"`
	Hosts           []string            `json:"hosts,omitempty"`
	CommandPrefixes []string            `json:"command_prefixes,omitempty"`
	Parameters      map[string][]string `json:"parameters,omitempty"`
	Summary         string              `json:"summary,omitempty"`
}

type GrantDecision struct {
	Granted       bool
	Denied        bool
	Scope         Scope
	ResourceScope ResourceScope
}

var resourceScopePathKeys = map[string]bool{
	"path": true, "paths": true, "source": true, "destination": true,
	"dir": true, "cwd": true, "file": true, "file_path": true,
	"source_path": true, "target_path": true, "output_path": true,
	"download_path": true,
}

var resourceScopeHostKeys = map[string]bool{
	"url": true, "urls": true, "endpoint": true, "base_url": true,
	"host": true, "hostname": true, "domain": true, "source_url": true,
}

var resourceScopeParameterKeys = map[string]bool{
	"method": true, "channel": true, "channel_id": true, "target_id": true,
	"action": true, "action_type": true, "model": true, "provider": true,
	"folder": true, "mailbox_folder": true, "calendar_id": true,
	"delivery_channel": true, "delivery_target": true,
}

func ResourceScopeFromToolCall(toolName string, input map[string]any) ResourceScope {
	var scope ResourceScope
	scope.collectToolSpecific(toolName, input)
	scope.collectValue("", input)
	return scope.Normalized()
}

func (s ResourceScope) Normalized() ResourceScope {
	out := s
	out.PathPrefixes = normalize.DedupeStrings(normalizeStrings(out.PathPrefixes))
	out.Hosts = normalize.DedupeStrings(normalizeStrings(out.Hosts))
	out.CommandPrefixes = normalize.DedupeStrings(normalizeStrings(out.CommandPrefixes))
	if len(out.Parameters) == 0 {
		out.Parameters = nil
	} else {
		params := make(map[string][]string, len(out.Parameters))
		for key, values := range out.Parameters {
			normalizedKey := strings.TrimSpace(strings.ToLower(key))
			if normalizedKey == "" {
				continue
			}
			normalizedValues := normalize.DedupeStrings(normalizeStrings(values))
			if len(normalizedValues) == 0 {
				continue
			}
			params[normalizedKey] = normalizedValues
		}
		if len(params) == 0 {
			out.Parameters = nil
		} else {
			out.Parameters = params
		}
	}
	out.Summary = strings.TrimSpace(out.Summary)
	if out.Summary == "" {
		out.Summary = out.summary()
	}
	return out
}

func (s ResourceScope) Empty() bool {
	normalized := s.Normalized()
	return len(normalized.PathPrefixes) == 0 &&
		len(normalized.Hosts) == 0 &&
		len(normalized.CommandPrefixes) == 0 &&
		len(normalized.Parameters) == 0
}

func (s ResourceScope) MatchesCall(toolName string, input map[string]any) bool {
	return s.Matches(ResourceScopeFromToolCall(toolName, input))
}

func (s ResourceScope) Matches(request ResourceScope) bool {
	granted := s.Normalized()
	request = request.Normalized()
	if granted.Empty() {
		return true
	}
	if len(granted.PathPrefixes) > 0 && !allRequestPathsWithin(granted.PathPrefixes, request.PathPrefixes) {
		return false
	}
	if len(granted.Hosts) > 0 && !allRequestValuesAllowed(granted.Hosts, request.Hosts, hostMatches) {
		return false
	}
	if len(granted.CommandPrefixes) > 0 && !allRequestValuesAllowed(granted.CommandPrefixes, request.CommandPrefixes, commandMatches) {
		return false
	}
	if len(granted.Parameters) > 0 {
		if len(request.Parameters) == 0 {
			return false
		}
		for key, allowed := range granted.Parameters {
			if !allRequestValuesAllowed(allowed, request.Parameters[key], exactMatch) {
				return false
			}
		}
	}
	return true
}

func (s ResourceScope) signature() string {
	normalized := s.Normalized()
	parts := make([]string, 0, 4+len(normalized.Parameters))
	if len(normalized.PathPrefixes) > 0 {
		parts = append(parts, "paths="+strings.Join(normalized.PathPrefixes, ","))
	}
	if len(normalized.Hosts) > 0 {
		parts = append(parts, "hosts="+strings.Join(normalized.Hosts, ","))
	}
	if len(normalized.CommandPrefixes) > 0 {
		parts = append(parts, "commands="+strings.Join(normalized.CommandPrefixes, ","))
	}
	if len(normalized.Parameters) > 0 {
		keys := make([]string, 0, len(normalized.Parameters))
		for key := range normalized.Parameters {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, "param:"+key+"="+strings.Join(normalized.Parameters[key], ","))
		}
	}
	return strings.Join(parts, "|")
}

func (s ResourceScope) summary() string {
	parts := make([]string, 0, 4)
	if len(s.PathPrefixes) > 0 {
		parts = append(parts, "paths="+strings.Join(limitScopeValues(s.PathPrefixes), ","))
	}
	if len(s.Hosts) > 0 {
		parts = append(parts, "hosts="+strings.Join(limitScopeValues(s.Hosts), ","))
	}
	if len(s.CommandPrefixes) > 0 {
		parts = append(parts, "commands="+strings.Join(limitScopeValues(s.CommandPrefixes), ","))
	}
	if len(s.Parameters) > 0 {
		keys := make([]string, 0, len(s.Parameters))
		for key := range s.Parameters {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		paramParts := make([]string, 0, len(keys))
		for _, key := range keys {
			paramParts = append(paramParts, key+"="+strings.Join(limitScopeValues(s.Parameters[key]), ","))
		}
		parts = append(parts, "params="+strings.Join(limitScopeValues(paramParts), ";"))
	}
	return strings.Join(parts, " | ")
}

func (s *ResourceScope) collectToolSpecific(toolName string, input map[string]any) {
	if s == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "exec.run", "proc.start":
		if prefix := commandPrefixFromInput(input); prefix != "" {
			s.CommandPrefixes = append(s.CommandPrefixes, prefix)
		}
	case "exec.shell":
		if command := strings.TrimSpace(fmt.Sprint(input["command"])); command != "" {
			s.CommandPrefixes = append(s.CommandPrefixes, command)
		}
	case "exec.script":
		interpreter := strings.TrimSpace(fmt.Sprint(input["interpreter"]))
		if interpreter == "" {
			interpreter = "/bin/sh"
		}
		s.CommandPrefixes = append(s.CommandPrefixes, interpreter)
		if script := strings.TrimSpace(fmt.Sprint(input["script"])); script != "" {
			if s.Parameters == nil {
				s.Parameters = make(map[string][]string)
			}
			digest := sha256.Sum256([]byte(script))
			s.Parameters["script_sha256"] = append(s.Parameters["script_sha256"], hex.EncodeToString(digest[:]))
		}
	case "proc.stop", "proc.logs", "proc.wait":
		appendScopeParameter(s, "process_id", fmt.Sprint(input["id"]))
	}
}

func commandPrefixFromInput(input map[string]any) string {
	command := strings.TrimSpace(fmt.Sprint(input["command"]))
	if command == "" {
		return ""
	}
	parts := []string{command}
	switch args := input["args"].(type) {
	case []string:
		for _, item := range args {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
	case []any:
		for _, item := range args {
			if trimmed := strings.TrimSpace(fmt.Sprint(item)); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
	}
	return strings.Join(parts, " ")
}

func appendScopeParameter(scope *ResourceScope, key, value string) {
	if scope == nil {
		return
	}
	key = strings.TrimSpace(strings.ToLower(key))
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	if scope.Parameters == nil {
		scope.Parameters = make(map[string][]string)
	}
	scope.Parameters[key] = append(scope.Parameters[key], value)
}

func (s *ResourceScope) collectValue(key string, value any) {
	if s == nil || value == nil {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		for childKey, childValue := range typed {
			s.collectValue(childKey, childValue)
		}
	case []any:
		for _, item := range typed {
			s.collectValue(key, item)
		}
	case []string:
		for _, item := range typed {
			s.collectScalar(key, item)
		}
	case string:
		s.collectScalar(key, typed)
	}
}

func (s *ResourceScope) collectScalar(key, raw string) {
	key = strings.TrimSpace(strings.ToLower(key))
	raw = strings.TrimSpace(raw)
	if key == "" || raw == "" {
		return
	}
	switch {
	case resourceScopePathKeys[key]:
		if normalizedPath := normalizeScopePath(raw); normalizedPath != "" {
			s.PathPrefixes = append(s.PathPrefixes, normalizedPath)
		}
	case resourceScopeHostKeys[key]:
		if host := normalizeScopeHost(raw); host != "" {
			s.Hosts = append(s.Hosts, host)
		}
	case resourceScopeParameterKeys[key]:
		if s.Parameters == nil {
			s.Parameters = make(map[string][]string)
		}
		s.Parameters[key] = append(s.Parameters[key], raw)
	}
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func normalizeScopePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	windowsStyle := looksWindowsStylePath(raw)
	raw = strings.ReplaceAll(raw, `\`, "/")
	cleaned := path.Clean(raw)
	if windowsStyle || looksWindowsStylePath(cleaned) {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

func normalizeScopeHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && strings.TrimSpace(parsed.Hostname()) != "" {
		return strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	}
	if host, _, err := net.SplitHostPort(raw); err == nil && strings.TrimSpace(host) != "" {
		return strings.ToLower(strings.TrimSpace(host))
	}
	return strings.ToLower(strings.Trim(strings.TrimSpace(raw), "."))
}

func allRequestPathsWithin(allowed, requested []string) bool {
	if len(allowed) == 0 {
		return true
	}
	if len(requested) == 0 {
		return false
	}
	for _, requestPath := range requested {
		matched := false
		for _, allowedPath := range allowed {
			if pathWithinScope(allowedPath, requestPath) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func pathWithinScope(allowed, requested string) bool {
	allowed = normalizeScopePath(allowed)
	requested = normalizeScopePath(requested)
	if allowed == "" || requested == "" {
		return false
	}
	if allowed == "." {
		return true
	}
	if requested == allowed {
		return true
	}
	if !strings.HasPrefix(requested, allowed) {
		return false
	}
	return len(requested) > len(allowed) && requested[len(allowed)] == '/'
}

func allRequestValuesAllowed(allowed, requested []string, match func(string, string) bool) bool {
	if len(allowed) == 0 {
		return true
	}
	if len(requested) == 0 {
		return false
	}
	for _, requestValue := range requested {
		matched := false
		for _, allowedValue := range allowed {
			if match(allowedValue, requestValue) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func hostMatches(allowed, requested string) bool {
	return strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(requested))
}

func commandMatches(allowed, requested string) bool {
	allowed = strings.TrimSpace(allowed)
	requested = strings.TrimSpace(requested)
	return allowed != "" && requested != "" && strings.HasPrefix(requested, allowed)
}

func exactMatch(allowed, requested string) bool {
	return strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(requested))
}

func limitScopeValues(values []string) []string {
	if len(values) <= 3 {
		return values
	}
	out := append([]string(nil), values[:3]...)
	out = append(out, fmt.Sprintf("+%d more", len(values)-3))
	return out
}

func looksWindowsStylePath(raw string) bool {
	if strings.Contains(raw, `\`) {
		return true
	}
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\`, "/"))
	if len(raw) >= 2 && isASCIILetter(raw[0]) && raw[1] == ':' {
		return true
	}
	return strings.HasPrefix(raw, "//")
}

func isASCIILetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}
