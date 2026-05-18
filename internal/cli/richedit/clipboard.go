package richedit

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// PasteResult is the outcome of processing pasted content.
type PasteResult struct {
	IsImage   bool
	Text      string
	ImageData string
	MediaType string
	PathHint  bool
}

const clipboardReadTimeout = 2 * time.Second

var clipboardTextReader = readSystemClipboardText
var clipboardImageReader = readSystemClipboardImage
var clipboardCombinedOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// ProcessPaste analyzes pasted content and returns the appropriate result.
func ProcessPaste(content string) PasteResult {
	if content == "" {
		return PasteResult{}
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return PasteResult{Text: content}
	}

	// Check for data URI
	if strings.HasPrefix(trimmed, "data:image/") {
		parts := strings.SplitN(trimmed, ";base64,", 2)
		if len(parts) == 2 {
			mediaType := strings.TrimPrefix(parts[0], "data:")
			return PasteResult{
				IsImage:   true,
				ImageData: trimmed,
				MediaType: mediaType,
			}
		}
	}

	if looksLikePath(trimmed) {
		return PasteResult{Text: content, PathHint: true}
	}

	return PasteResult{Text: content}
}

// ReadClipboardPaste reads the current system clipboard and converts it into
// the same PasteResult shape used by bracketed paste.
func ReadClipboardPaste() (PasteResult, error) {
	imageData, mediaType, imageErr := clipboardImageReader()
	if imageErr == nil && strings.TrimSpace(imageData) != "" {
		return PasteResult{
			IsImage:   true,
			ImageData: imageData,
			MediaType: mediaType,
		}, nil
	}

	text, textErr := clipboardTextReader()
	if textErr == nil && text != "" {
		return ProcessPaste(text), nil
	}

	switch {
	case imageErr != nil && textErr != nil:
		return PasteResult{}, errors.Join(imageErr, textErr)
	case textErr != nil:
		return PasteResult{}, textErr
	case imageErr != nil:
		return PasteResult{}, imageErr
	default:
		return PasteResult{}, nil
	}
}

func looksLikePath(s string) bool {
	if strings.ContainsAny(s, "\n\r") {
		return false
	}
	s = strings.Trim(s, "\"'")
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, "/") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(s))
	return ext != ""
}

// readImageFile reads an image file and returns a data URI and media type.
func readImageFile(path string) (string, string, error) {
	path = strings.Trim(path, "\"'")
	// Expand ~ if present
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}

	mediaType := http.DetectContentType(data)
	if !strings.HasPrefix(mediaType, "image/") {
		mediaType = "application/octet-stream"
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	dataURI := "data:" + mediaType + ";base64," + encoded
	return dataURI, mediaType, nil
}

func readSystemClipboardText() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), clipboardReadTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "pbpaste")
	case "linux":
		switch {
		case os.Getenv("WAYLAND_DISPLAY") != "":
			cmd = exec.CommandContext(ctx, "wl-paste", "--no-newline")
		case lookPath("xclip") == nil:
			cmd = exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-o")
		case lookPath("xsel") == nil:
			cmd = exec.CommandContext(ctx, "xsel", "--clipboard", "--output")
		default:
			return "", errors.New("no clipboard text reader found")
		}
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Get-Clipboard -Raw")
	default:
		return "", errors.New("clipboard text read is unsupported on this platform")
	}

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func readSystemClipboardImage() (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), clipboardReadTimeout)
	defer cancel()

	switch runtime.GOOS {
	case "darwin":
		return readDarwinClipboardImage(ctx)
	case "linux":
		return readLinuxClipboardImage(ctx)
	case "windows":
		return readWindowsClipboardImage(ctx)
	default:
		return "", "", errors.New("clipboard image read is unsupported on this platform")
	}
}

func readDarwinClipboardImage(ctx context.Context) (string, string, error) {
	const script = `
ObjC.import('AppKit');
ObjC.import('Foundation');
var pb = $.NSPasteboard.generalPasteboard;
var data = pb.dataForType('public.png');
if (!data) {
  data = pb.dataForType('public.tiff');
  if (data) {
    var rep = $.NSBitmapImageRep.imageRepWithData(data);
    if (rep) {
      data = rep.representationUsingTypeProperties($.NSBitmapImageFileTypePNG, $.NSDictionary.dictionary());
    }
  }
}
if (data) {
  console.log(ObjC.unwrap(data.base64EncodedStringWithOptions(0)));
}
`
	out, err := clipboardCombinedOutput(ctx, "osascript", "-l", "JavaScript", "-e", script)
	if err != nil {
		return "", "", err
	}
	encoded := extractDarwinClipboardImageBase64(out)
	if encoded == "" {
		return "", "", nil
	}
	return "data:image/png;base64," + encoded, "image/png", nil
}

func readLinuxClipboardImage(ctx context.Context) (string, string, error) {
	if os.Getenv("WAYLAND_DISPLAY") != "" && lookPath("wl-paste") == nil {
		mime, err := linuxClipboardImageMime(ctx, "wl-paste", "--list-types")
		if err != nil || mime == "" {
			return "", "", err
		}
		out, err := exec.CommandContext(ctx, "wl-paste", "--no-newline", "--type", mime).Output()
		if err != nil {
			return "", "", err
		}
		if len(out) == 0 {
			return "", "", nil
		}
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(out), mime, nil
	}
	if lookPath("xclip") == nil {
		mime, err := linuxClipboardImageMime(ctx, "xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
		if err != nil || mime == "" {
			return "", "", err
		}
		out, err := exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-t", mime, "-o").Output()
		if err != nil {
			return "", "", err
		}
		if len(out) == 0 {
			return "", "", nil
		}
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(out), mime, nil
	}
	return "", "", nil
}

func readWindowsClipboardImage(ctx context.Context) (string, string, error) {
	const script = `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
if ([System.Windows.Forms.Clipboard]::ContainsImage()) {
  $img = [System.Windows.Forms.Clipboard]::GetImage()
  $ms = New-Object System.IO.MemoryStream
  $img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
  [Convert]::ToBase64String($ms.ToArray())
}
`
	out, err := exec.CommandContext(ctx, "powershell", "-Sta", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return "", "", err
	}
	encoded := strings.TrimSpace(string(out))
	if encoded == "" {
		return "", "", nil
	}
	return "data:image/png;base64," + encoded, "image/png", nil
}

func linuxClipboardImageMime(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		switch strings.TrimSpace(strings.ToLower(line)) {
		case "image/png":
			return "image/png", nil
		case "image/jpeg", "image/jpg":
			return "image/jpeg", nil
		}
	}
	return "", nil
}

func lookPath(name string) error {
	_, err := exec.LookPath(name)
	return err
}

func extractDarwinClipboardImageBase64(out []byte) string {
	lines := strings.FieldsFunc(string(out), func(r rune) bool {
		return r == '\r' || r == '\n'
	})
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if looksLikeBase64Line(line) {
			return line
		}
	}
	return ""
}

func looksLikeBase64Line(s string) bool {
	if len(s) < 16 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '+' || r == '/' || r == '=':
		default:
			return false
		}
	}
	return true
}
