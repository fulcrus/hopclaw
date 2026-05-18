package eventbus

import "testing"

func TestDeltaPayloadPreservesWhitespaceTokens(t *testing.T) {
	event := NewModelTextDeltaEvent("run-1", "sess-1", DeltaAttrs{Delta: " world"}, nil)

	payload, ok := event.ModelTextDeltaPayload()
	if !ok {
		t.Fatal("ModelTextDeltaPayload() ok = false")
	}
	if payload.Delta != " world" {
		t.Fatalf("payload.Delta = %q, want %q", payload.Delta, " world")
	}
}

func TestDeltaPayloadPreservesWhitespaceOnlyTokens(t *testing.T) {
	event := NewModelTextDeltaEvent("run-1", "sess-1", DeltaAttrs{Delta: " "}, nil)

	payload, ok := event.ModelTextDeltaPayload()
	if !ok {
		t.Fatal("ModelTextDeltaPayload() ok = false")
	}
	if payload.Delta != " " {
		t.Fatalf("payload.Delta = %q, want %q", payload.Delta, " ")
	}
}
