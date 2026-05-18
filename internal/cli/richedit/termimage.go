package richedit

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
)

// ImageProtocol is the detected terminal image display protocol.
type ImageProtocol int

const (
	ProtocolNone   ImageProtocol = iota
	ProtocolKitty                // Kitty graphics protocol
	ProtocolITerm2               // iTerm2 inline images
	ProtocolSixel                // Sixel
)

// DetectImageProtocol checks terminal environment for image support.
func DetectImageProtocol() ImageProtocol {
	termProgram := os.Getenv("TERM_PROGRAM")
	termEnv := os.Getenv("TERM")

	switch {
	case strings.Contains(termEnv, "kitty"):
		return ProtocolKitty
	case termProgram == "WezTerm":
		return ProtocolKitty
	case termProgram == "iTerm.app":
		return ProtocolITerm2
	case strings.Contains(termProgram, "Ghostty"):
		return ProtocolKitty
	}

	return ProtocolNone
}

// RenderImage outputs an image to the terminal.
// Returns the number of terminal lines consumed.
func RenderImage(out io.Writer, protocol ImageProtocol, data []byte, mediaType string, maxCols, maxRows int) (int, error) {
	switch protocol {
	case ProtocolKitty:
		return renderKitty(out, data, maxCols, maxRows)
	case ProtocolITerm2:
		return renderITerm2(out, data, maxCols, maxRows)
	default:
		// No image rendering — show text placeholder
		fmt.Fprintf(out, "  [Image: %s, %d bytes]", mediaType, len(data))
		return 1, nil
	}
}

func renderKitty(out io.Writer, data []byte, cols, rows int) (int, error) {
	encoded := base64.StdEncoding.EncodeToString(data)
	// Kitty protocol: chunked transfer
	const chunkSize = 4096
	for i := 0; i < len(encoded); i += chunkSize {
		end := i + chunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		chunk := encoded[i:end]
		more := 1
		if end >= len(encoded) {
			more = 0
		}
		if i == 0 {
			fmt.Fprintf(out, "\033_Gf=100,a=T,c=%d,r=%d,m=%d;%s\033\\", cols, rows, more, chunk)
		} else {
			fmt.Fprintf(out, "\033_Gm=%d;%s\033\\", more, chunk)
		}
	}
	return rows, nil
}

func renderITerm2(out io.Writer, data []byte, cols, rows int) (int, error) {
	encoded := base64.StdEncoding.EncodeToString(data)
	fmt.Fprintf(out, "\033]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=1:%s\007",
		cols, rows, encoded)
	return rows, nil
}

// ImageInfoText returns a text description for when image rendering is not available.
func ImageInfoText(id int, mediaType string, dataSize int) string {
	kb := float64(dataSize) / 1024.0
	return fmt.Sprintf("[IMAGE#%d · %s · %.1f KB]", id, mediaType, kb)
}
