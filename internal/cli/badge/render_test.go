package badge

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

func TestRendererSupported(t *testing.T) {
	tests := []struct {
		name     string
		protocol richedit.ImageProtocol
		want     bool
	}{
		{name: "none", protocol: richedit.ProtocolNone, want: false},
		{name: "kitty", protocol: richedit.ProtocolKitty, want: true},
		{name: "iterm2", protocol: richedit.ProtocolITerm2, want: true},
		{name: "sixel", protocol: richedit.ProtocolSixel, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer, err := NewRenderer(&strings.Builder{}, tt.protocol, "#00ff88", 4)
			if err != nil {
				t.Fatalf("NewRenderer() error = %v", err)
			}
			if got := renderer.Supported(); got != tt.want {
				t.Fatalf("Supported() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRendererShowKitty(t *testing.T) {
	var out strings.Builder
	renderer, err := NewRenderer(&out, richedit.ProtocolKitty, "#ff6600", 4)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}

	if err := renderer.Show(monochromeTestImage(), 80); err != nil {
		t.Fatalf("Show() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "\033[s") || !strings.Contains(got, "\033[u") {
		t.Fatalf("Show() output missing save/restore cursor: %q", got)
	}
	if !strings.Contains(got, "\033[2;77H") {
		t.Fatalf("Show() output missing right-edge position: %q", got)
	}
	if !strings.Contains(got, "\033_Gf=100,a=T,i=1,c=4,r=4,m=") {
		t.Fatalf("Show() output missing kitty image payload: %q", got)
	}
}

func TestRendererClearKitty(t *testing.T) {
	var out strings.Builder
	renderer, err := NewRenderer(&out, richedit.ProtocolKitty, "#00ff88", 4)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	if err := renderer.Show(monochromeTestImage(), 80); err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	out.Reset()

	if err := renderer.Clear(80); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "\033_Ga=d,d=i,i=1\033\\") {
		t.Fatalf("Clear() output missing kitty delete sequence: %q", got)
	}
	for _, want := range []string{"\033[2;77H    ", "\033[3;77H    ", "\033[4;77H    ", "\033[5;77H    "} {
		if !strings.Contains(got, want) {
			t.Fatalf("Clear() output missing row clear %q: %q", want, got)
		}
	}
	if strings.Count(got, "    ") != 4 {
		t.Fatalf("Clear() should blank 4 rows, output = %q", got)
	}
}

func TestRendererShowUnsupported(t *testing.T) {
	var out strings.Builder
	renderer, err := NewRenderer(&out, richedit.ProtocolNone, "#00ff88", 4)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	if err := renderer.Show(monochromeTestImage(), 80); err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("Show() wrote output for unsupported protocol: %q", out.String())
	}
}

func TestRendererShowSixel(t *testing.T) {
	var out strings.Builder
	renderer, err := NewRenderer(&out, richedit.ProtocolSixel, "#00ff88", 2)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}

	if err := renderer.Show(monochromeTestImage(), 40); err != nil {
		t.Fatalf("Show() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "\033Pq#1;2;0;100;53") {
		t.Fatalf("Show() output missing sixel header: %q", got)
	}
	if !strings.Contains(got, "\033\\") {
		t.Fatalf("Show() output missing sixel terminator: %q", got)
	}
}

func TestMonochromeTinting(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	src.SetRGBA(1, 0, color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff})

	tinted := tintMonochromeImage(src, color.RGBA{R: 0x20, G: 0x80, B: 0xe0, A: 0xff})
	bright := tinted.RGBAAt(0, 0)
	dim := tinted.RGBAAt(1, 0)

	if bright.R != 0x20 || bright.G != 0x80 || bright.B != 0xe0 || bright.A != 0xff {
		t.Fatalf("bright pixel = %#v, want full tint", bright)
	}
	if dim.R >= bright.R || dim.G >= bright.G || dim.B >= bright.B || dim.A != 0xff {
		t.Fatalf("dim pixel = %#v, want lower-brightness tint than %#v", dim, bright)
	}
}

func monochromeTestImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if x < 2 || y < 2 {
				continue
			}
			img.SetRGBA(x, y, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		}
	}
	return img
}
