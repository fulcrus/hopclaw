package richedit

import (
	"context"
	"errors"
	"testing"
)

func TestReadDarwinClipboardImageReadsConsoleLogOutput(t *testing.T) {
	original := clipboardCombinedOutput
	clipboardCombinedOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "osascript" {
			t.Fatalf("command = %q, want osascript", name)
		}
		return []byte("2026-04-08 10:00:00.000 osascript[1:2] warning\n" +
			"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jv2QAAAAASUVORK5CYII=\n"), nil
	}
	t.Cleanup(func() {
		clipboardCombinedOutput = original
	})

	dataURI, mediaType, err := readDarwinClipboardImage(context.Background())
	if err != nil {
		t.Fatalf("readDarwinClipboardImage() error = %v", err)
	}
	if mediaType != "image/png" {
		t.Fatalf("mediaType = %q, want image/png", mediaType)
	}
	const wantPrefix = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB"
	if len(dataURI) < len(wantPrefix) || dataURI[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("dataURI = %q, want prefix %q", dataURI, wantPrefix)
	}
}

func TestReadDarwinClipboardImagePropagatesCommandError(t *testing.T) {
	wantErr := errors.New("boom")
	original := clipboardCombinedOutput
	clipboardCombinedOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, wantErr
	}
	t.Cleanup(func() {
		clipboardCombinedOutput = original
	})

	_, _, err := readDarwinClipboardImage(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("readDarwinClipboardImage() error = %v, want %v", err, wantErr)
	}
}
