package imaputil

import "testing"

func TestQuoteEscapesBackslashesAndQuotes(t *testing.T) {
	t.Parallel()

	if got := Quote(`mailbox\"name`); got != `"mailbox\\\"name"` {
		t.Fatalf("Quote() = %q", got)
	}
}

func TestParseLiteralSize(t *testing.T) {
	t.Parallel()

	if got, ok := ParseLiteralSize(`* 1 FETCH (BODY[] {42}`); !ok || got != 42 {
		t.Fatalf("ParseLiteralSize() = (%d, %v), want (42, true)", got, ok)
	}
	if _, ok := ParseLiteralSize(`* 1 FETCH`); ok {
		t.Fatal("ParseLiteralSize() unexpectedly parsed invalid literal")
	}
}

func TestParseHeaderBlock(t *testing.T) {
	t.Parallel()

	headers := ParseHeaderBlock("Subject: Hello\r\nFrom: Test User\r\n\r\n")
	if headers["subject"] != "Hello" {
		t.Fatalf("subject = %q", headers["subject"])
	}
	if headers["from"] != "Test User" {
		t.Fatalf("from = %q", headers["from"])
	}
}
