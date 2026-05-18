package server

import "testing"

func TestSecureTokenEqualMatchesEqualTokens(t *testing.T) {
	t.Parallel()

	if !secureTokenEqual("hopclaw-secret", "hopclaw-secret") {
		t.Fatal("secureTokenEqual() = false, want true for equal tokens")
	}
}

func TestSecureTokenEqualRejectsDifferentLengthTokens(t *testing.T) {
	t.Parallel()

	if secureTokenEqual("hopclaw-secret", "hopclaw") {
		t.Fatal("secureTokenEqual() = true, want false for different tokens")
	}
}
