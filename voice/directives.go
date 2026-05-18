package voice

import (
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// TTS directive parsing
// ---------------------------------------------------------------------------

// directivePattern matches [[tts:voice_name]] markers in model output.
var directivePattern = regexp.MustCompile(`\[\[tts:([^\]]+)\]\]`)

// Directive represents a TTS directive extracted from model output.
type Directive struct {
	Voice string `json:"voice"`
}

// ParseDirectives scans text for [[tts:voice_name]] patterns, strips them
// from the output, and returns the cleaned text along with the directives.
func ParseDirectives(text string) (string, []Directive) {
	matches := directivePattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return text, nil
	}

	directives := make([]Directive, 0, len(matches))
	for _, m := range matches {
		directives = append(directives, Directive{
			Voice: strings.TrimSpace(m[1]),
		})
	}

	cleanText := directivePattern.ReplaceAllString(text, "")
	cleanText = strings.TrimSpace(cleanText)

	return cleanText, directives
}
