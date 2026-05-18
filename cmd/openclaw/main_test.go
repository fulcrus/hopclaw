package main

import (
	"bytes"
	"testing"
)

func TestPrintDeprecationNotice(t *testing.T) {
	var stderr bytes.Buffer
	printDeprecationNotice(&stderr)

	if got := stderr.String(); got != "Note: 'openclaw' binary is deprecated. Use 'hopclaw' instead.\n" {
		t.Fatalf("printDeprecationNotice() = %q", got)
	}
}
