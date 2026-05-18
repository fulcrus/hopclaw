package media

import (
	"testing"
)

// ---------------------------------------------------------------------------
// DetectKind tests
// ---------------------------------------------------------------------------

func TestDetectKindImage(t *testing.T) {
	t.Parallel()

	imageTypes := []string{
		"image/jpeg", "image/png", "image/gif", "image/webp",
		"image/svg+xml", "image/bmp", "image/tiff", "image/heic",
		"image/heif", "image/avif",
	}
	for _, mime := range imageTypes {
		if kind := DetectKind(mime); kind != KindImage {
			t.Errorf("expected KindImage for %q, got %q", mime, kind)
		}
	}
}

func TestDetectKindAudio(t *testing.T) {
	t.Parallel()

	audioTypes := []string{
		"audio/mpeg", "audio/wav", "audio/ogg", "audio/flac",
		"audio/aac", "audio/mp4", "audio/webm", "audio/x-m4a",
		"audio/mp3",
	}
	for _, mime := range audioTypes {
		if kind := DetectKind(mime); kind != KindAudio {
			t.Errorf("expected KindAudio for %q, got %q", mime, kind)
		}
	}
}

func TestDetectKindVideo(t *testing.T) {
	t.Parallel()

	videoTypes := []string{
		"video/mp4", "video/webm", "video/quicktime",
		"video/x-msvideo", "video/x-matroska", "video/mpeg",
		"video/ogg",
	}
	for _, mime := range videoTypes {
		if kind := DetectKind(mime); kind != KindVideo {
			t.Errorf("expected KindVideo for %q, got %q", mime, kind)
		}
	}
}

func TestDetectKindUnknown(t *testing.T) {
	t.Parallel()

	unknownTypes := []string{
		"application/json", "text/plain", "application/pdf",
		"application/octet-stream", "",
	}
	for _, mime := range unknownTypes {
		if kind := DetectKind(mime); kind != KindUnknown {
			t.Errorf("expected KindUnknown for %q, got %q", mime, kind)
		}
	}
}

func TestDetectKindStripsParameters(t *testing.T) {
	t.Parallel()

	if kind := DetectKind("audio/wav; codecs=pcm"); kind != KindAudio {
		t.Errorf("expected KindAudio for MIME with params, got %q", kind)
	}
}

func TestDetectKindCaseInsensitive(t *testing.T) {
	t.Parallel()

	if kind := DetectKind("IMAGE/JPEG"); kind != KindImage {
		t.Errorf("expected KindImage for uppercase MIME, got %q", kind)
	}
}

// ---------------------------------------------------------------------------
// DetectMIMEType (extension-based) tests
// ---------------------------------------------------------------------------

func TestDetectMIMETypeExtensions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path     string
		expected string
	}{
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"image.png", "image/png"},
		{"animation.gif", "image/gif"},
		{"image.webp", "image/webp"},
		{"diagram.svg", "image/svg+xml"},
		{"picture.bmp", "image/bmp"},
		{"scan.tiff", "image/tiff"},
		{"scan.tif", "image/tiff"},
		{"photo.heic", "image/heic"},
		{"photo.avif", "image/avif"},
		{"song.mp3", "audio/mpeg"},
		{"voice.wav", "audio/wav"},
		{"podcast.ogg", "audio/ogg"},
		{"music.flac", "audio/flac"},
		{"ringtone.aac", "audio/aac"},
		{"voice.m4a", "audio/x-m4a"},
		{"clip.mp4", "video/mp4"},
		{"video.webm", "video/webm"},
		{"movie.mov", "video/quicktime"},
		{"old.avi", "video/x-msvideo"},
		{"film.mkv", "video/x-matroska"},
		{"tape.mpeg", "video/mpeg"},
		{"/path/to/Photo.JPG", "image/jpeg"},
	}

	for _, tc := range cases {
		got := DetectMIMEType(tc.path)
		if got != tc.expected {
			t.Errorf("DetectMIMEType(%q) = %q, want %q", tc.path, got, tc.expected)
		}
	}
}

func TestDetectMIMETypeUnknownExtension(t *testing.T) {
	t.Parallel()

	got := DetectMIMEType("data.xyz")
	if got != "application/octet-stream" {
		t.Errorf("expected application/octet-stream for unknown extension, got %q", got)
	}
}

func TestDetectMIMETypeNoExtension(t *testing.T) {
	t.Parallel()

	got := DetectMIMEType("README")
	if got != "application/octet-stream" {
		t.Errorf("expected application/octet-stream for no extension, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// DetectMIMETypeFromBytes (magic bytes) tests
// ---------------------------------------------------------------------------

func TestDetectMIMETypeFromBytesJPEG(t *testing.T) {
	t.Parallel()

	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	got := DetectMIMETypeFromBytes(data)
	if got != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesPNG(t *testing.T) {
	t.Parallel()

	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00}
	got := DetectMIMETypeFromBytes(data)
	if got != "image/png" {
		t.Errorf("expected image/png, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesGIF(t *testing.T) {
	t.Parallel()

	cases := [][]byte{
		{'G', 'I', 'F', '8', '7', 'a', 0x00, 0x00},
		{'G', 'I', 'F', '8', '9', 'a', 0x00, 0x00},
	}
	for _, data := range cases {
		got := DetectMIMETypeFromBytes(data)
		if got != "image/gif" {
			t.Errorf("expected image/gif, got %q", got)
		}
	}
}

func TestDetectMIMETypeFromBytesWEBP(t *testing.T) {
	t.Parallel()

	data := []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'}
	got := DetectMIMETypeFromBytes(data)
	if got != "image/webp" {
		t.Errorf("expected image/webp, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesBMP(t *testing.T) {
	t.Parallel()

	data := []byte{'B', 'M', 0x00, 0x00, 0x00, 0x00}
	got := DetectMIMETypeFromBytes(data)
	if got != "image/bmp" {
		t.Errorf("expected image/bmp, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesTIFF(t *testing.T) {
	t.Parallel()

	// Little-endian TIFF
	le := []byte{'I', 'I', 0x2A, 0x00, 0x08, 0x00}
	if got := DetectMIMETypeFromBytes(le); got != "image/tiff" {
		t.Errorf("expected image/tiff for LE, got %q", got)
	}

	// Big-endian TIFF
	be := []byte{'M', 'M', 0x00, 0x2A, 0x00, 0x00}
	if got := DetectMIMETypeFromBytes(be); got != "image/tiff" {
		t.Errorf("expected image/tiff for BE, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesMP3ID3(t *testing.T) {
	t.Parallel()

	data := []byte{'I', 'D', '3', 0x04, 0x00, 0x00}
	got := DetectMIMETypeFromBytes(data)
	if got != "audio/mpeg" {
		t.Errorf("expected audio/mpeg, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesMP3SyncWord(t *testing.T) {
	t.Parallel()

	data := []byte{0xFF, 0xFB, 0x90, 0x04}
	got := DetectMIMETypeFromBytes(data)
	if got != "audio/mpeg" {
		t.Errorf("expected audio/mpeg, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesWAV(t *testing.T) {
	t.Parallel()

	data := []byte{'R', 'I', 'F', 'F', 0x24, 0x00, 0x00, 0x00, 'W', 'A', 'V', 'E'}
	got := DetectMIMETypeFromBytes(data)
	if got != "audio/wav" {
		t.Errorf("expected audio/wav, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesOGG(t *testing.T) {
	t.Parallel()

	data := []byte{'O', 'g', 'g', 'S', 0x00, 0x02}
	got := DetectMIMETypeFromBytes(data)
	if got != "audio/ogg" {
		t.Errorf("expected audio/ogg, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesFLAC(t *testing.T) {
	t.Parallel()

	data := []byte{'f', 'L', 'a', 'C', 0x00, 0x00}
	got := DetectMIMETypeFromBytes(data)
	if got != "audio/flac" {
		t.Errorf("expected audio/flac, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesMP4(t *testing.T) {
	t.Parallel()

	// ftypisom brand
	data := []byte{0x00, 0x00, 0x00, 0x1C, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	got := DetectMIMETypeFromBytes(data)
	if got != "video/mp4" {
		t.Errorf("expected video/mp4, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesM4A(t *testing.T) {
	t.Parallel()

	data := []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}
	got := DetectMIMETypeFromBytes(data)
	if got != "audio/mp4" {
		t.Errorf("expected audio/mp4, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesQuickTime(t *testing.T) {
	t.Parallel()

	data := []byte{0x00, 0x00, 0x00, 0x14, 'f', 't', 'y', 'p', 'q', 't', ' ', ' '}
	got := DetectMIMETypeFromBytes(data)
	if got != "video/quicktime" {
		t.Errorf("expected video/quicktime, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesEmpty(t *testing.T) {
	t.Parallel()

	got := DetectMIMETypeFromBytes(nil)
	if got != "application/octet-stream" {
		t.Errorf("expected application/octet-stream for empty data, got %q", got)
	}
}

func TestDetectMIMETypeFromBytesTooShort(t *testing.T) {
	t.Parallel()

	got := DetectMIMETypeFromBytes([]byte{0x00, 0x01})
	// Falls through to http.DetectContentType
	if got == "" {
		t.Error("expected non-empty result for short data")
	}
}
