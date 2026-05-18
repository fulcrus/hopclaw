package repl

import "strings"

const (
	dsmlFunctionCallsStart = "<｜DSML｜function_calls>"
	dsmlFunctionCallsEnd   = "</｜DSML｜function_calls>"
)

// StreamTextFilter filters DSML function-call blocks from streamed text.
type StreamTextFilter = streamTextFilter

type streamTextFilter struct {
	carry              string
	skippingDSML       bool
	trimNextLineBreak  bool
	lastVisibleNewline bool
}

func (f *streamTextFilter) Reset() {
	f.carry = ""
	f.skippingDSML = false
	f.trimNextLineBreak = false
	f.lastVisibleNewline = false
}

func (f *streamTextFilter) Filter(delta string) string {
	if delta == "" {
		return ""
	}
	data := f.carry + delta
	f.carry = ""

	var out strings.Builder
	for len(data) > 0 {
		if f.skippingDSML {
			end := strings.Index(data, dsmlFunctionCallsEnd)
			if end < 0 {
				f.carry = trailingPartialToken(data, dsmlFunctionCallsEnd)
				return out.String()
			}
			data = data[end+len(dsmlFunctionCallsEnd):]
			f.skippingDSML = false
			if f.trimNextLineBreak && strings.HasPrefix(data, "\n") {
				data = data[1:]
			}
			f.trimNextLineBreak = false
			continue
		}

		start := strings.Index(data, dsmlFunctionCallsStart)
		if start < 0 {
			keep := partialTokenSuffixLen(data, dsmlFunctionCallsStart)
			visible := data[:len(data)-keep]
			out.WriteString(visible)
			if visible != "" {
				f.lastVisibleNewline = visible[len(visible)-1] == '\n'
			}
			f.carry = data[len(data)-keep:]
			return out.String()
		}

		visible := data[:start]
		out.WriteString(visible)
		if visible != "" {
			f.lastVisibleNewline = visible[len(visible)-1] == '\n'
		}
		f.trimNextLineBreak = f.lastVisibleNewline
		f.skippingDSML = true
		data = data[start+len(dsmlFunctionCallsStart):]
	}
	return out.String()
}

func (f *streamTextFilter) Flush() string {
	if f.skippingDSML {
		f.Reset()
		return ""
	}
	visible := f.carry
	f.carry = ""
	if visible != "" {
		f.lastVisibleNewline = visible[len(visible)-1] == '\n'
	}
	return visible
}

func trailingPartialToken(data string, token string) string {
	keep := partialTokenSuffixLen(data, token)
	if keep == 0 {
		return ""
	}
	return data[len(data)-keep:]
}

func partialTokenSuffixLen(data string, token string) int {
	limit := len(token) - 1
	if limit > len(data) {
		limit = len(data)
	}
	for size := limit; size > 0; size-- {
		if strings.HasPrefix(token, data[len(data)-size:]) {
			return size
		}
	}
	return 0
}
