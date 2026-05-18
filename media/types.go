package media

import "context"

// ---------------------------------------------------------------------------
// Capability and MediaKind enumerations
// ---------------------------------------------------------------------------

// Capability describes a kind of media understanding a provider supports.
type Capability string

const (
	// CapabilityImage indicates image description support.
	CapabilityImage Capability = "image"
	// CapabilityAudio indicates audio transcription support.
	CapabilityAudio Capability = "audio"
	// CapabilityVideo indicates video analysis support.
	CapabilityVideo Capability = "video"
	// CapabilityOCR indicates optical character recognition support.
	CapabilityOCR Capability = "ocr"
)

// MediaKind classifies the content type of a media file.
type MediaKind string

const (
	// KindImage represents an image file.
	KindImage MediaKind = "image"
	// KindAudio represents an audio file.
	KindAudio MediaKind = "audio"
	// KindVideo represents a video file.
	KindVideo MediaKind = "video"
	// KindUnknown represents an unrecognized media type.
	KindUnknown MediaKind = "unknown"
)

// ---------------------------------------------------------------------------
// Attachment
// ---------------------------------------------------------------------------

// Attachment represents a media file to be processed by the pipeline.
type Attachment struct {
	Path     string    `json:"path,omitempty"`
	URL      string    `json:"url,omitempty"`
	MIMEType string    `json:"mime_type,omitempty"`
	Data     []byte    `json:"-"` // raw bytes, not serialized
	Kind     MediaKind `json:"kind"`
	Index    int       `json:"index"`
}

// ---------------------------------------------------------------------------
// Understanding output
// ---------------------------------------------------------------------------

// UnderstandingOutput is the result of processing one attachment.
type UnderstandingOutput struct {
	Kind            Capability `json:"kind"`
	AttachmentIndex int        `json:"attachment_index"`
	Text            string     `json:"text"`
	Provider        string     `json:"provider"`
	Model           string     `json:"model,omitempty"`
	DurationMs      int64      `json:"duration_ms"`
}

// ---------------------------------------------------------------------------
// Provider interfaces
// ---------------------------------------------------------------------------

// Provider is the base interface for media understanding backends.
type Provider interface {
	// ID returns the provider identifier (e.g. "openai", "google").
	ID() string
	// Capabilities returns the list of media capabilities this provider supports.
	Capabilities() []Capability
}

// ImageProvider can describe images.
type ImageProvider interface {
	Provider
	DescribeImage(ctx context.Context, req ImageRequest) (*ImageResult, error)
}

// AudioProvider can transcribe audio.
type AudioProvider interface {
	Provider
	TranscribeAudio(ctx context.Context, req AudioRequest) (*AudioResult, error)
}

// VideoProvider can describe videos.
type VideoProvider interface {
	Provider
	DescribeVideo(ctx context.Context, req VideoRequest) (*VideoResult, error)
}

// ---------------------------------------------------------------------------
// Request and result types
// ---------------------------------------------------------------------------

// ImageRequest holds the input for image description.
type ImageRequest struct {
	Data     []byte // image bytes
	MIMEType string // e.g., "image/jpeg"
	Prompt   string // optional custom prompt
}

// ImageResult holds the output from image description.
type ImageResult struct {
	Text  string
	Model string
}

// AudioRequest holds the input for audio transcription.
type AudioRequest struct {
	Data     []byte // audio bytes
	MIMEType string // e.g., "audio/wav"
	Language string // optional language hint (e.g., "en")
	Prompt   string // optional context
}

// AudioResult holds the output from audio transcription.
type AudioResult struct {
	Text     string
	Language string
	Model    string
}

// VideoRequest holds the input for video analysis.
type VideoRequest struct {
	Data     []byte // video bytes
	MIMEType string // e.g., "video/mp4"
	Prompt   string // optional custom prompt
}

// VideoResult holds the output from video analysis.
type VideoResult struct {
	Text  string
	Model string
}

// ---------------------------------------------------------------------------
// Pipeline configuration
// ---------------------------------------------------------------------------

// PipelineConfig controls the media understanding pipeline behaviour.
type PipelineConfig struct {
	ImageConcurrency int   `json:"image_concurrency" yaml:"image_concurrency"`
	AudioConcurrency int   `json:"audio_concurrency" yaml:"audio_concurrency"`
	VideoConcurrency int   `json:"video_concurrency" yaml:"video_concurrency"`
	MaxFileSizeBytes int64 `json:"max_file_size_bytes" yaml:"max_file_size_bytes"`
	EchoTranscripts  bool  `json:"echo_transcripts" yaml:"echo_transcripts"`

	// AutoResize enables automatic image resizing before sending to vision providers.
	AutoResize bool `json:"auto_resize" yaml:"auto_resize"`
	// GenerateThumbnails enables thumbnail generation for processed media.
	GenerateThumbnails bool `json:"generate_thumbnails" yaml:"generate_thumbnails"`
	// ThumbnailSize is the pixel size for generated thumbnails.
	ThumbnailSize int `json:"thumbnail_size" yaml:"thumbnail_size"`
}
