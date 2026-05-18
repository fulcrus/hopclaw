package desktopd

import (
	"strings"
	"time"
)

type elementQuery struct {
	App           string
	BundleID      string
	Path          string
	Role          string
	Text          string
	Contains      string
	MatchIndex    int
	MaxDepth      int
	MaxResults    int
	Timeout       time.Duration
	ValueEquals   string
	ValueContains string
}

type desktopElement struct {
	Path        string
	Role        string
	Label       string
	Description string
	Value       string
	Position    []int
	Size        []int
	X           int
	Y           int
	Width       int
	Height      int
}

func parseElementQuery(params map[string]any, defaultDepth, defaultResults int) elementQuery {
	q := elementQuery{
		App:           stringParam(params, "app"),
		BundleID:      stringParam(params, "bundle_id"),
		Path:          stringParam(params, "path"),
		Role:          stringParam(params, "role"),
		Text:          stringParam(params, "text"),
		Contains:      stringParam(params, "contains"),
		MatchIndex:    intParam(params, "match_index"),
		MaxDepth:      intParam(params, "max_depth"),
		MaxResults:    intParam(params, "max_results"),
		ValueEquals:   stringParam(params, "value_equals"),
		ValueContains: stringParam(params, "value_contains"),
	}
	if q.Text == "" {
		q.Text = stringParam(params, "label")
	}
	if q.Contains == "" {
		q.Contains = stringParam(params, "label_contains")
	}
	if q.MaxResults <= 0 {
		q.MaxResults = intParam(params, "limit")
	}
	if q.MaxDepth <= 0 {
		q.MaxDepth = defaultDepth
	}
	if q.MaxResults <= 0 {
		q.MaxResults = defaultResults
	}
	if timeoutMS := intParam(params, "timeout_ms"); timeoutMS > 0 {
		q.Timeout = time.Duration(timeoutMS) * time.Millisecond
	}
	return q
}

func elementToMap(elem desktopElement) map[string]any {
	return map[string]any{
		"path":        elem.Path,
		"role":        elem.Role,
		"label":       elem.Label,
		"description": elem.Description,
		"value":       elem.Value,
		"position":    elem.Position,
		"size":        elem.Size,
		"x":           elem.X,
		"y":           elem.Y,
		"width":       elem.Width,
		"height":      elem.Height,
	}
}

func elementMatchesData(matches []desktopElement) map[string]any {
	items := make([]any, 0, len(matches))
	for _, match := range matches {
		items = append(items, elementToMap(match))
	}
	return map[string]any{
		"matches":     items,
		"match_count": len(matches),
		"elements":    items,
		"count":       len(matches),
	}
}

func selectElement(matches []desktopElement, matchIndex int) (desktopElement, bool) {
	if len(matches) == 0 {
		return desktopElement{}, false
	}
	if matchIndex < 0 || matchIndex >= len(matches) {
		matchIndex = 0
	}
	return matches[matchIndex], true
}

func elementValueVerified(elem desktopElement, q elementQuery) bool {
	if q.ValueEquals != "" && elem.Value != q.ValueEquals {
		return false
	}
	if q.ValueContains != "" && !containsFold(elem.Value, q.ValueContains) {
		return false
	}
	return true
}

func containsFold(s, substr string) bool {
	if substr == "" {
		return true
	}
	return containsLower(s, substr)
}

func containsLower(s, substr string) bool {
	return indexFold(s, substr) >= 0
}

func indexFold(s, substr string) int {
	ls := strings.ToLower(strings.TrimSpace(s))
	lsub := strings.ToLower(strings.TrimSpace(substr))
	return strings.Index(ls, lsub)
}

func cloneParamsWithValue(params map[string]any, value string) map[string]any {
	cloned := make(map[string]any, len(params)+1)
	for key, current := range params {
		cloned[key] = current
	}
	cloned["value"] = value
	return cloned
}
