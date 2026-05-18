package hooks

import "testing"

// ---------------------------------------------------------------------------
// Test filter: empty filter matches everything
// ---------------------------------------------------------------------------

func TestEvaluateFilterEmpty(t *testing.T) {
	payload := map[string]any{"tool_name": "exec.run"}
	if !EvaluateFilter("", payload) {
		t.Error("empty filter should match everything")
	}
	if !EvaluateFilter("   ", payload) {
		t.Error("whitespace-only filter should match everything")
	}
}

// ---------------------------------------------------------------------------
// Test filter: equality
// ---------------------------------------------------------------------------

func TestEvaluateFilterEquality(t *testing.T) {
	payload := map[string]any{"tool_name": "exec.run", "status": "completed"}

	if !EvaluateFilter("tool_name == exec.run", payload) {
		t.Error("expected tool_name == exec.run to match")
	}
	if EvaluateFilter("tool_name == read", payload) {
		t.Error("expected tool_name == read to not match")
	}
	if !EvaluateFilter("status == completed", payload) {
		t.Error("expected status == completed to match")
	}
}

// ---------------------------------------------------------------------------
// Test filter: inequality
// ---------------------------------------------------------------------------

func TestEvaluateFilterInequality(t *testing.T) {
	payload := map[string]any{"status": "failed"}

	if !EvaluateFilter("status != completed", payload) {
		t.Error("expected status != completed to match")
	}
	if EvaluateFilter("status != failed", payload) {
		t.Error("expected status != failed to not match")
	}
}

// ---------------------------------------------------------------------------
// Test filter: numeric comparisons
// ---------------------------------------------------------------------------

func TestEvaluateFilterNumeric(t *testing.T) {
	payload := map[string]any{"tokens": float64(1500), "score": 42}

	if !EvaluateFilter("tokens > 1000", payload) {
		t.Error("expected tokens > 1000 to match")
	}
	if EvaluateFilter("tokens > 2000", payload) {
		t.Error("expected tokens > 2000 to not match")
	}
	if !EvaluateFilter("tokens < 2000", payload) {
		t.Error("expected tokens < 2000 to match")
	}
	if !EvaluateFilter("tokens >= 1500", payload) {
		t.Error("expected tokens >= 1500 to match")
	}
	if EvaluateFilter("tokens >= 1501", payload) {
		t.Error("expected tokens >= 1501 to not match")
	}
	if !EvaluateFilter("tokens <= 1500", payload) {
		t.Error("expected tokens <= 1500 to match")
	}
	if EvaluateFilter("tokens <= 1499", payload) {
		t.Error("expected tokens <= 1499 to not match")
	}

	// int types
	if !EvaluateFilter("score > 40", payload) {
		t.Error("expected score > 40 to match (int payload)")
	}
}

// ---------------------------------------------------------------------------
// Test filter: contains
// ---------------------------------------------------------------------------

func TestEvaluateFilterContains(t *testing.T) {
	payload := map[string]any{"model": "gpt-4-turbo"}

	if !EvaluateFilter("model contains gpt", payload) {
		t.Error("expected model contains gpt to match")
	}
	if !EvaluateFilter("model contains turbo", payload) {
		t.Error("expected model contains turbo to match")
	}
	if EvaluateFilter("model contains claude", payload) {
		t.Error("expected model contains claude to not match")
	}
}

// ---------------------------------------------------------------------------
// Test filter: missing field returns false
// ---------------------------------------------------------------------------

func TestEvaluateFilterMissingField(t *testing.T) {
	payload := map[string]any{"status": "ok"}

	if EvaluateFilter("nonexistent == value", payload) {
		t.Error("expected missing field to not match")
	}
}

// ---------------------------------------------------------------------------
// Test filter: invalid expression returns false
// ---------------------------------------------------------------------------

func TestEvaluateFilterInvalidExpression(t *testing.T) {
	payload := map[string]any{"status": "ok"}

	if EvaluateFilter("invalid expression here without operator", payload) {
		t.Error("expected invalid expression to not match")
	}
}

// ---------------------------------------------------------------------------
// Test filter: numeric comparison with string payload
// ---------------------------------------------------------------------------

func TestEvaluateFilterNumericStringPayload(t *testing.T) {
	payload := map[string]any{"count": "100"}

	if !EvaluateFilter("count > 50", payload) {
		t.Error("expected string numeric comparison to work")
	}
	if EvaluateFilter("count > 200", payload) {
		t.Error("expected string numeric comparison to not match")
	}
}

// ---------------------------------------------------------------------------
// Test filter: non-numeric value in numeric comparison
// ---------------------------------------------------------------------------

func TestEvaluateFilterNonNumericComparison(t *testing.T) {
	payload := map[string]any{"name": "not-a-number"}

	if EvaluateFilter("name > 50", payload) {
		t.Error("expected non-numeric value to not match numeric comparison")
	}
}
