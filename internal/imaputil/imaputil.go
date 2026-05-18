package imaputil

import (
	"strconv"
	"strings"
)

var quoteReplacer = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// Quote escapes an IMAP string literal so it can be safely wrapped in quotes.
func Quote(value string) string {
	return `"` + quoteReplacer.Replace(value) + `"`
}

// ParseLiteralSize extracts the byte count from an IMAP literal marker like "{42}".
func ParseLiteralSize(line string) (int, bool) {
	start := strings.LastIndex(line, "{")
	end := strings.LastIndex(line, "}")
	if start < 0 || end <= start {
		return 0, false
	}
	size, err := strconv.Atoi(strings.TrimSpace(line[start+1 : end]))
	if err != nil || size < 0 {
		return 0, false
	}
	return size, true
}

// ParseHeaderBlock converts a raw RFC822-style header block into a lowercase key/value map.
func ParseHeaderBlock(raw string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		out[key] = value
	}
	return out
}
