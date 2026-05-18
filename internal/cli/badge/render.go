package badge

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"

	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

const kittyBadgeImageID = 1
const defaultBadgeRow = 2

// Renderer renders badges in the right margin with terminal image protocols.
type Renderer struct {
	out        io.Writer
	protocol   richedit.ImageProtocol
	color      color.RGBA
	size       int
	row        int
	lastCols   int
	lastRows   int
	lastTopRow int
	lastShown  bool
}

// NewRenderer constructs a badge renderer with validated color and size settings.
func NewRenderer(out io.Writer, protocol richedit.ImageProtocol, hexColor string, size int) (*Renderer, error) {
	if out == nil {
		out = io.Discard
	}
	renderer := &Renderer{
		out:      out,
		protocol: protocol,
		row:      defaultBadgeRow,
	}
	if err := renderer.SetAppearance(hexColor, size); err != nil {
		return nil, err
	}
	return renderer, nil
}

// SetAppearance updates the renderer tint and cell size.
func (r *Renderer) SetAppearance(hexColor string, size int) error {
	if size < minSizeCells || size > maxSizeCells {
		return fmt.Errorf("badge size must be between %d and %d cells", minSizeCells, maxSizeCells)
	}
	_, fill, err := normalizeHexColor(hexColor)
	if err != nil {
		return err
	}
	r.color = fill
	r.size = size
	return nil
}

// SetRow updates the absolute terminal row used for badge placement.
func (r *Renderer) SetRow(row int) {
	if row < 1 {
		row = 1
	}
	r.row = row
}

// Supported reports whether the configured protocol can render images.
func (r *Renderer) Supported() bool {
	switch r.protocol {
	case richedit.ProtocolKitty, richedit.ProtocolITerm2, richedit.ProtocolSixel:
		return true
	default:
		return false
	}
}

// Show renders a badge at the current cursor row, anchored to the right edge.
func (r *Renderer) Show(img *image.RGBA, termWidth int) error {
	if !r.Supported() || img == nil {
		return nil
	}

	if r.lastShown {
		if err := r.Clear(termWidth); err != nil {
			return err
		}
	}

	tinted := tintMonochromeImage(img, r.color)
	data, err := encodePNG(tinted)
	if err != nil {
		return err
	}

	col := rightAlignedColumn(termWidth, r.size)
	fmt.Fprint(r.out, "\033[s")
	fmt.Fprintf(r.out, "\033[%d;%dH", r.row, col)
	if err := r.renderImage(data, tinted); err != nil {
		fmt.Fprint(r.out, "\033[u")
		return err
	}
	fmt.Fprint(r.out, "\033[u")

	r.lastCols = r.size
	r.lastRows = r.size
	r.lastTopRow = r.row
	r.lastShown = true
	return nil
}

// Clear erases the previously rendered badge region.
func (r *Renderer) Clear(termWidth int) error {
	if !r.Supported() || !r.lastShown {
		return nil
	}

	col := rightAlignedColumn(termWidth, r.lastCols)
	fmt.Fprint(r.out, "\033[s")
	if r.protocol == richedit.ProtocolKitty {
		fmt.Fprintf(r.out, "\033_Ga=d,d=i,i=%d\033\\", kittyBadgeImageID)
	}

	spaces := strings.Repeat(" ", r.lastCols)
	for row := 0; row < r.lastRows; row++ {
		fmt.Fprintf(r.out, "\033[%d;%dH%s", r.lastTopRow+row, col, spaces)
	}
	fmt.Fprint(r.out, "\033[u")

	r.lastCols = 0
	r.lastRows = 0
	r.lastTopRow = 0
	r.lastShown = false
	return nil
}

func (r *Renderer) renderImage(data []byte, img *image.RGBA) error {
	switch r.protocol {
	case richedit.ProtocolKitty:
		return renderKittyBadge(r.out, data, r.size, r.size)
	case richedit.ProtocolITerm2:
		return renderITerm2Badge(r.out, data, r.size, r.size)
	case richedit.ProtocolSixel:
		scaled := scaleImageNearest(img, max(1, r.size*8), max(1, r.size*16))
		return renderSixelBadge(r.out, scaled)
	default:
		return nil
	}
}

func rightAlignedColumn(termWidth, widthCells int) int {
	if termWidth <= 0 || widthCells <= 0 {
		return 1
	}
	col := termWidth - widthCells + 1
	if col < 1 {
		return 1
	}
	return col
}

func encodePNG(img image.Image) ([]byte, error) {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		return nil, fmt.Errorf("encode badge image: %w", err)
	}
	return buffer.Bytes(), nil
}

func renderKittyBadge(out io.Writer, data []byte, cols, rows int) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	const chunkSize = 4096

	for i := 0; i < len(encoded); i += chunkSize {
		end := min(i+chunkSize, len(encoded))
		chunk := encoded[i:end]
		more := 1
		if end >= len(encoded) {
			more = 0
		}
		if i == 0 {
			fmt.Fprintf(out, "\033_Gf=100,a=T,i=%d,c=%d,r=%d,m=%d;%s\033\\", kittyBadgeImageID, cols, rows, more, chunk)
			continue
		}
		fmt.Fprintf(out, "\033_Gi=%d,m=%d;%s\033\\", kittyBadgeImageID, more, chunk)
	}
	return nil
}

func renderITerm2Badge(out io.Writer, data []byte, cols, rows int) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	_, err := fmt.Fprintf(out, "\033]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=1:%s\007", cols, rows, encoded)
	return err
}

func renderSixelBadge(out io.Writer, img *image.RGBA) error {
	bounds := img.Bounds()
	if bounds.Empty() {
		return nil
	}

	fill := img.RGBAAt(0, 0)
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y && !found; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := img.RGBAAt(x, y)
			if pixel.A == 0 {
				continue
			}
			fill = pixel
			found = true
			break
		}
	}

	if _, err := fmt.Fprintf(out, "\033Pq#1;2;%d;%d;%d", scaleSixelColor(fill.R), scaleSixelColor(fill.G), scaleSixelColor(fill.B)); err != nil {
		return err
	}
	for baseY := 0; baseY < bounds.Dy(); baseY += 6 {
		if _, err := io.WriteString(out, "#1"); err != nil {
			return err
		}
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			var bits byte
			for bit := 0; bit < 6; bit++ {
				y := baseY + bit
				if y >= bounds.Dy() {
					continue
				}
				pixel := img.RGBAAt(x, y)
				if pixel.A != 0 {
					bits |= 1 << bit
				}
			}
			if _, err := fmt.Fprintf(out, "%c", rune(63+bits)); err != nil {
				return err
			}
		}
		if baseY+6 < bounds.Dy() {
			if _, err := io.WriteString(out, "-"); err != nil {
				return err
			}
		}
	}
	_, err := io.WriteString(out, "\033\\")
	return err
}

func scaleSixelColor(component uint8) int {
	return int((uint16(component) * 100) / 255)
}
