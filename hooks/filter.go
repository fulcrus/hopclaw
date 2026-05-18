package hooks

import (
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Filter operators
// ---------------------------------------------------------------------------

const (
	opEqual          = "=="
	opNotEqual       = "!="
	opGreaterThan    = ">"
	opLessThan       = "<"
	opGreaterOrEqual = ">="
	opLessOrEqual    = "<="
	opContains       = "contains"
)

// orderedOps lists comparison operators in precedence order (longest first)
// so that ">=" is matched before ">".
var orderedOps = []string{opGreaterOrEqual, opLessOrEqual, opNotEqual, opEqual, opGreaterThan, opLessThan, opContains}

// EvaluateFilter checks if a payload matches a filter expression.
// Supported expressions:
//
//	"tool_name == exec.run"  — equality check
//	"status != failed"       — inequality check
//	"tokens > 1000"          — numeric comparison
//	"model contains gpt"     — substring check
//	""                       — empty filter matches everything
func EvaluateFilter(filter string, payload map[string]any) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}

	field, op, value, err := parseFilter(filter)
	if err != nil {
		return false
	}

	actual, ok := payload[field]
	if !ok {
		return false
	}

	switch op {
	case opEqual:
		return fmt.Sprintf("%v", actual) == value
	case opNotEqual:
		return fmt.Sprintf("%v", actual) != value
	case opContains:
		return strings.Contains(fmt.Sprintf("%v", actual), value)
	case opGreaterThan, opLessThan, opGreaterOrEqual, opLessOrEqual:
		return compareNumeric(actual, op, value)
	default:
		return false
	}
}

// parseFilter splits a filter expression into (field, operator, value).
func parseFilter(expr string) (field, op, value string, err error) {
	for _, candidate := range orderedOps {
		idx := strings.Index(expr, " "+candidate+" ")
		if idx < 0 {
			continue
		}
		field = strings.TrimSpace(expr[:idx])
		value = strings.TrimSpace(expr[idx+len(candidate)+2:])
		if field == "" || value == "" {
			continue
		}
		return field, candidate, value, nil
	}
	return "", "", "", fmt.Errorf("unsupported filter expression: %s", expr)
}

// compareNumeric performs a numeric comparison between actual and value using op.
func compareNumeric(actual any, op, value string) bool {
	expected, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false
	}

	var actualFloat float64
	switch v := actual.(type) {
	case float64:
		actualFloat = v
	case float32:
		actualFloat = float64(v)
	case int:
		actualFloat = float64(v)
	case int64:
		actualFloat = float64(v)
	case string:
		parsed, parseErr := strconv.ParseFloat(v, 64)
		if parseErr != nil {
			return false
		}
		actualFloat = parsed
	default:
		return false
	}

	switch op {
	case opGreaterThan:
		return actualFloat > expected
	case opLessThan:
		return actualFloat < expected
	case opGreaterOrEqual:
		return actualFloat >= expected
	case opLessOrEqual:
		return actualFloat <= expected
	default:
		return false
	}
}
