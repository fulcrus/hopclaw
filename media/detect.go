package media

import (
	"net/http"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// MIME type classification maps
// ---------------------------------------------------------------------------

// imageMIMETypes lists known image MIME types.
var imageMIMETypes = map[string]bool{
	"image/jpeg":    true,
	"image/png":     true,
	"image/gif":     true,
	"image/webp":    true,
	"image/svg+xml": true,
	"image/bmp":     true,
	"image/tiff":    true,
	"image/heic":    true,
	"image/heif":    true,
	"image/avif":    true,
}

// audioMIMETypes lists known audio MIME types.
var audioMIMETypes = map[string]bool{
	"audio/mpeg":   true,
	"audio/wav":    true,
	"audio/ogg":    true,
	"audio/flac":   true,
	"audio/aac":    true,
	"audio/mp4":    true,
	"audio/webm":   true,
	"audio/x-m4a":  true,
	"audio/mp3":    true,
	"audio/x-wav":  true,
	"audio/x-flac": true,
}

// videoMIMETypes lists known video MIME types.
var videoMIMETypes = map[string]bool{
	"video/mp4":        true,
	"video/webm":       true,
	"video/quicktime":  true,
	"video/x-msvideo":  true,
	"video/x-matroska": true,
	"video/mpeg":       true,
	"video/ogg":        true,
	"video/x-flv":      true,
	"video/3gpp":       true,
	"video/x-ms-wmv":   true,
}

// extMIMEMap maps lowercase file extensions (with dot) to MIME types.
var extMIMEMap = map[string]string{
	// Images
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".bmp":  "image/bmp",
	".tiff": "image/tiff",
	".tif":  "image/tiff",
	".heic": "image/heic",
	".heif": "image/heif",
	".avif": "image/avif",
	// Audio
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".flac": "audio/flac",
	".aac":  "audio/aac",
	".m4a":  "audio/x-m4a",
	".wma":  "audio/x-ms-wma",
	".opus": "audio/ogg",
	// Video
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
	".mkv":  "video/x-matroska",
	".mpeg": "video/mpeg",
	".mpg":  "video/mpeg",
	".flv":  "video/x-flv",
	".wmv":  "video/x-ms-wmv",
	".3gp":  "video/3gpp",
}

// ---------------------------------------------------------------------------
// Magic bytes signatures
// ---------------------------------------------------------------------------

// magicBytesMinLen is the minimum number of bytes needed for magic byte detection.
const magicBytesMinLen = 12

// ---------------------------------------------------------------------------
// Detection functions
// ---------------------------------------------------------------------------

// DetectKind classifies a MIME type into image, audio, video, or unknown.
func DetectKind(mimeType string) MediaKind {
	// Normalize: strip parameters (e.g., "audio/wav; codecs=pcm").
	if idx := strings.IndexByte(mimeType, ';'); idx >= 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}
	mimeType = strings.ToLower(mimeType)

	if imageMIMETypes[mimeType] {
		return KindImage
	}
	if audioMIMETypes[mimeType] {
		return KindAudio
	}
	if videoMIMETypes[mimeType] {
		return KindVideo
	}
	return KindUnknown
}

// DetectMIMEType returns a MIME type based on the file extension of path.
// Returns "application/octet-stream" if the extension is not recognized.
func DetectMIMEType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if mime, ok := extMIMEMap[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}

// DetectMIMETypeFromBytes inspects magic bytes to determine the MIME type.
// It uses at most the first 512 bytes. Returns "application/octet-stream"
// if the type cannot be determined.
func DetectMIMETypeFromBytes(data []byte) string {
	if len(data) == 0 {
		return "application/octet-stream"
	}

	// Check well-known magic byte signatures first for precision.
	if mime := detectByMagicBytes(data); mime != "" {
		return mime
	}

	// Fallback to Go's built-in content detection (uses first 512 bytes).
	const sniffSize = 512
	sniffData := data
	if len(sniffData) > sniffSize {
		sniffData = sniffData[:sniffSize]
	}
	detected := http.DetectContentType(sniffData)

	// http.DetectContentType may return "application/octet-stream" or
	// "text/plain; charset=utf-8" for media it doesn't recognize.
	return detected
}

// detectByMagicBytes checks known file signatures and returns the MIME type.
// Returns "" if no signature matches.
func detectByMagicBytes(data []byte) string {
	if len(data) < 4 {
		return ""
	}

	// JPEG: 0xFF 0xD8 0xFF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}

	// PNG: 0x89 0x50 0x4E 0x47 0x0D 0x0A 0x1A 0x0A
	if len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A {
		return "image/png"
	}

	// GIF: "GIF87a" or "GIF89a"
	if len(data) >= 6 &&
		data[0] == 'G' && data[1] == 'I' && data[2] == 'F' &&
		data[3] == '8' && (data[4] == '7' || data[4] == '9') && data[5] == 'a' {
		return "image/gif"
	}

	// WEBP: "RIFF" + 4 bytes + "WEBP"
	if len(data) >= magicBytesMinLen &&
		data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P' {
		return "image/webp"
	}

	// BMP: "BM"
	if data[0] == 'B' && data[1] == 'M' {
		return "image/bmp"
	}

	// TIFF: "II" (little-endian) or "MM" (big-endian) + magic number 42
	if len(data) >= 4 {
		if (data[0] == 'I' && data[1] == 'I' && data[2] == 0x2A && data[3] == 0x00) ||
			(data[0] == 'M' && data[1] == 'M' && data[2] == 0x00 && data[3] == 0x2A) {
			return "image/tiff"
		}
	}

	// MP3: ID3 tag or MPEG sync word
	if data[0] == 'I' && data[1] == 'D' && data[2] == '3' {
		return "audio/mpeg"
	}
	if data[0] == 0xFF && (data[1]&0xE0) == 0xE0 {
		return "audio/mpeg"
	}

	// WAV: "RIFF" + 4 bytes + "WAVE"
	if len(data) >= magicBytesMinLen &&
		data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'A' && data[10] == 'V' && data[11] == 'E' {
		return "audio/wav"
	}

	// OGG: "OggS"
	if data[0] == 'O' && data[1] == 'g' && data[2] == 'g' && data[3] == 'S' {
		return "audio/ogg"
	}

	// FLAC: "fLaC"
	if data[0] == 'f' && data[1] == 'L' && data[2] == 'a' && data[3] == 'C' {
		return "audio/flac"
	}

	// MPEG-4 container (ftyp box): MP4, M4A, MOV, etc.
	if len(data) >= 8 &&
		data[4] == 'f' && data[5] == 't' && data[6] == 'y' && data[7] == 'p' {
		return detectFtyp(data)
	}

	return ""
}

// detectFtyp inspects the ftyp box of an MPEG-4 container to distinguish
// video/mp4, audio/mp4, and video/quicktime.
func detectFtyp(data []byte) string {
	if len(data) < magicBytesMinLen {
		return "video/mp4"
	}

	brand := string(data[8:12])
	switch {
	case brand == "M4A " || brand == "M4B ":
		return "audio/mp4"
	case brand == "qt  ":
		return "video/quicktime"
	default:
		return "video/mp4"
	}
}
