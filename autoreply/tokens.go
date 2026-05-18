package autoreply

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Token constants
// ---------------------------------------------------------------------------

const (
	// SilentReplyToken indicates the engine matched a rule but the caller
	// should suppress any visible response.
	SilentReplyToken = "NO_REPLY"

	// HeartbeatOKToken signals that a heartbeat check found nothing to report.
	HeartbeatOKToken = "HEARTBEAT_OK"

	// defaultHeartbeatAckMaxChars is the maximum length for heartbeat
	// acknowledgment text. Responses shorter than this in heartbeat mode
	// are treated as insignificant and suppressed.
	defaultHeartbeatAckMaxChars = 300
)

// ---------------------------------------------------------------------------
// Heartbeat strip types
// ---------------------------------------------------------------------------

// HeartbeatStripMode controls how remaining text is evaluated after token
// removal.
type HeartbeatStripMode string

const (
	// HeartbeatStripMessage is the default mode: remaining text is returned
	// as-is after stripping the token.
	HeartbeatStripMessage HeartbeatStripMode = "message"

	// HeartbeatStripHeartbeat suppresses remaining text that is shorter than
	// maxAckChars, treating it as a trivial acknowledgment.
	HeartbeatStripHeartbeat HeartbeatStripMode = "heartbeat"
)

// HeartbeatStripResult holds the outcome of stripping a HEARTBEAT_OK token.
type HeartbeatStripResult struct {
	// ShouldSkip is true when the entire message should be suppressed.
	ShouldSkip bool
	// Text is the remaining content after token removal.
	Text string
	// DidStrip is true when the token was found and removed.
	DidStrip bool
}

// ---------------------------------------------------------------------------
// Silent-reply helpers
// ---------------------------------------------------------------------------

// IsSilentReply reports whether text is the silent-reply token. Leading and
// trailing whitespace is tolerated (the reference uses /^\s*NO_REPLY\s*$/).
func IsSilentReply(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return trimmed == SilentReplyToken
}

// IsSilentReplyPrefix detects partial/streaming fragments of the silent token.
// It returns true for ALL-CAPS prefixes of NO_REPLY that are at least 2
// characters long (e.g., "NO", "NO_", "NO_RE"), guarding against natural
// language text like "No problem" by rejecting mixed-case input.
func IsSilentReplyPrefix(text string) bool {
	trimmed := strings.TrimLeft(text, " \t\n\r")
	if trimmed == "" {
		return false
	}
	// Guard: only match if input is entirely uppercase.
	if trimmed != strings.ToUpper(trimmed) {
		return false
	}
	if len(trimmed) < 2 {
		return false
	}
	// Must contain only ASCII uppercase letters and underscores.
	for _, r := range trimmed {
		if !((r >= 'A' && r <= 'Z') || r == '_') {
			return false
		}
	}
	tokenUpper := strings.ToUpper(SilentReplyToken)
	if !strings.HasPrefix(tokenUpper, trimmed) {
		return false
	}
	// An underscore proves it is part of a TOKEN_PATTERN.
	if strings.ContainsRune(trimmed, '_') {
		return true
	}
	// For NO_REPLY specifically, allow bare "NO" as a common stream fragment.
	return trimmed == "NO"
}

// silentTrailingRe matches NO_REPLY at the end of text, optionally preceded
// by whitespace or asterisks.
var silentTrailingRe = regexp.MustCompile(
	`(?:^|\s+|\*+)` + regexp.QuoteMeta(SilentReplyToken) + `\s*$`,
)

// StripSilentToken removes a trailing NO_REPLY token from mixed-content text.
// Returns the remaining text with the token removed (trimmed). If the result
// is empty the entire message should be treated as silent.
func StripSilentToken(text string) string {
	return strings.TrimSpace(silentTrailingRe.ReplaceAllString(text, ""))
}

// ---------------------------------------------------------------------------
// Heartbeat token helpers
// ---------------------------------------------------------------------------

// htmlTagRe matches HTML tags for markup stripping.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// heartbeatEndRe matches HeartbeatOKToken followed by up to 4 non-word
// characters at the end of the string.
var heartbeatEndRe = regexp.MustCompile(
	regexp.QuoteMeta(HeartbeatOKToken) + `\W{0,4}$`,
)

// multiSpaceRe matches runs of whitespace for collapsing.
var multiSpaceRe = regexp.MustCompile(`\s+`)

// stripMarkup removes HTML tags, &nbsp; entities, and leading/trailing
// markdown-style wrappers (* ` ~ _).
func stripMarkup(text string) string {
	text = htmlTagRe.ReplaceAllString(text, " ")
	text = strings.NewReplacer("&nbsp;", " ", "&NBSP;", " ").Replace(text)
	text = strings.TrimLeftFunc(text, isMarkdownWrapper)
	text = strings.TrimRightFunc(text, isMarkdownWrapper)
	return text
}

func isMarkdownWrapper(r rune) bool {
	return r == '*' || r == '`' || r == '~' || r == '_'
}

// isWordChar matches [0-9A-Za-z_], consistent with Go regexp \w.
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// containsWordChar reports whether s contains at least one word character.
func containsWordChar(s string) bool {
	for _, r := range s {
		if isWordChar(r) {
			return true
		}
	}
	return false
}

func collapseWhitespace(s string) string {
	return strings.TrimSpace(multiSpaceRe.ReplaceAllString(s, " "))
}

type tokenEdgeResult struct {
	text     string
	didStrip bool
}

// stripTokenAtEdges removes the HEARTBEAT_OK token from the beginning and/or
// end of text. Up to 4 trailing non-word characters the model may append
// (e.g. ".", "!!!", "---") are also removed.
func stripTokenAtEdges(raw string) tokenEdgeResult {
	text := strings.TrimSpace(raw)
	if text == "" {
		return tokenEdgeResult{}
	}
	if !strings.Contains(text, HeartbeatOKToken) {
		return tokenEdgeResult{text: text}
	}

	didStrip := false
	changed := true
	for changed {
		changed = false
		text = strings.TrimSpace(text)

		// Strip at start.
		if strings.HasPrefix(text, HeartbeatOKToken) {
			text = strings.TrimSpace(text[len(HeartbeatOKToken):])
			didStrip = true
			changed = true
			continue
		}

		// Strip at end (with up to 4 trailing non-word chars). Only the text
		// before the token is kept; trailing punctuation the model appended
		// after the token (e.g. ".", "!!!") is discarded.
		if heartbeatEndRe.MatchString(text) {
			idx := strings.LastIndex(text, HeartbeatOKToken)
			text = strings.TrimRight(text[:idx], " \t\n\r")
			didStrip = true
			changed = true
		}
	}

	return tokenEdgeResult{text: collapseWhitespace(text), didStrip: didStrip}
}

// StripHeartbeatToken removes the HEARTBEAT_OK token from text, handling
// HTML tags, markdown wrappers, and trailing punctuation. Returns a
// structured result.
//
// When mode is HeartbeatStripHeartbeat, remaining text shorter than
// maxAckChars is treated as an insignificant acknowledgment and suppressed.
// Pass maxAckChars <= 0 to use the default (300).
func StripHeartbeatToken(text string, mode HeartbeatStripMode, maxAckChars int) HeartbeatStripResult {
	if text == "" {
		return HeartbeatStripResult{ShouldSkip: true}
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return HeartbeatStripResult{ShouldSkip: true}
	}

	if maxAckChars <= 0 {
		maxAckChars = defaultHeartbeatAckMaxChars
	}

	// Normalize markup so wrapped tokens (e.g. <b>HEARTBEAT_OK</b>,
	// **HEARTBEAT_OK**) are still detected.
	normalized := stripMarkup(trimmed)
	hasToken := strings.Contains(trimmed, HeartbeatOKToken) ||
		strings.Contains(normalized, HeartbeatOKToken)
	if !hasToken {
		return HeartbeatStripResult{Text: trimmed}
	}

	origResult := stripTokenAtEdges(trimmed)
	normResult := stripTokenAtEdges(normalized)

	// Prefer original-text result when it succeeded and has meaningful
	// remaining text. Fall back to normalized when the original's remainder
	// is only markup/punctuation artifacts (e.g. "****" from "**HEARTBEAT_OK**").
	picked := origResult
	if !(origResult.didStrip && origResult.text != "" && containsWordChar(origResult.text)) {
		picked = normResult
	}
	if !picked.didStrip {
		return HeartbeatStripResult{Text: trimmed}
	}
	if picked.text == "" || !containsWordChar(picked.text) {
		return HeartbeatStripResult{ShouldSkip: true, DidStrip: true}
	}

	rest := strings.TrimSpace(picked.text)
	if mode == HeartbeatStripHeartbeat && len(rest) <= maxAckChars {
		return HeartbeatStripResult{ShouldSkip: true, DidStrip: true}
	}

	return HeartbeatStripResult{Text: rest, DidStrip: true}
}
