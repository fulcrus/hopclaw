package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock providers for testing
// ---------------------------------------------------------------------------

// mockImageProvider implements ImageProvider for testing.
type mockImageProvider struct {
	id    string
	err   error
	text  string
	model string
	delay time.Duration

	// callCount tracks the number of calls for concurrency testing.
	callCount atomic.Int64
}

func (m *mockImageProvider) ID() string                 { return m.id }
func (m *mockImageProvider) Capabilities() []Capability { return []Capability{CapabilityImage} }

func (m *mockImageProvider) DescribeImage(_ context.Context, _ ImageRequest) (*ImageResult, error) {
	m.callCount.Add(1)
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.err != nil {
		return nil, m.err
	}
	return &ImageResult{Text: m.text, Model: m.model}, nil
}

// mockAudioProvider implements AudioProvider for testing.
type mockAudioProvider struct {
	id    string
	err   error
	text  string
	model string

	callCount atomic.Int64
}

func (m *mockAudioProvider) ID() string                 { return m.id }
func (m *mockAudioProvider) Capabilities() []Capability { return []Capability{CapabilityAudio} }

func (m *mockAudioProvider) TranscribeAudio(_ context.Context, _ AudioRequest) (*AudioResult, error) {
	m.callCount.Add(1)
	if m.err != nil {
		return nil, m.err
	}
	return &AudioResult{Text: m.text, Model: m.model}, nil
}

// mockVideoProvider implements VideoProvider for testing.
type mockVideoProvider struct {
	id    string
	err   error
	text  string
	model string

	callCount atomic.Int64
}

func (m *mockVideoProvider) ID() string                 { return m.id }
func (m *mockVideoProvider) Capabilities() []Capability { return []Capability{CapabilityVideo} }

func (m *mockVideoProvider) DescribeVideo(_ context.Context, _ VideoRequest) (*VideoResult, error) {
	m.callCount.Add(1)
	if m.err != nil {
		return nil, m.err
	}
	return &VideoResult{Text: m.text, Model: m.model}, nil
}

// ---------------------------------------------------------------------------
// TestPipelineProcessImage
// ---------------------------------------------------------------------------

func TestPipelineProcessImage(t *testing.T) {
	t.Parallel()

	imgProvider := &mockImageProvider{
		id:    "test-image",
		text:  "a cat sitting on a mat",
		model: "test-vision-v1",
	}

	registry := NewRegistry()
	registry.Register(imgProvider)

	pipeline := NewPipeline(registry, PipelineConfig{})

	result, err := pipeline.Process(context.Background(), []Attachment{
		{
			Data:     []byte{0xFF, 0xD8, 0xFF, 0xE0}, // JPEG magic bytes
			MIMEType: "image/jpeg",
			Kind:     KindImage,
			Index:    0,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.AppliedImage {
		t.Error("expected AppliedImage to be true")
	}
	if len(result.Outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(result.Outputs))
	}
	if result.Outputs[0].Text != "a cat sitting on a mat" {
		t.Errorf("unexpected output text: %q", result.Outputs[0].Text)
	}
	if result.Outputs[0].Kind != CapabilityImage {
		t.Errorf("expected kind %q, got %q", CapabilityImage, result.Outputs[0].Kind)
	}
	if result.Outputs[0].Provider != "test-image" {
		t.Errorf("expected provider %q, got %q", "test-image", result.Outputs[0].Provider)
	}
}

// ---------------------------------------------------------------------------
// TestPipelineProcessMultiple
// ---------------------------------------------------------------------------

func TestPipelineProcessMultiple(t *testing.T) {
	t.Parallel()

	imgProvider := &mockImageProvider{id: "img", text: "image description", model: "v1"}
	audioProvider := &mockAudioProvider{id: "aud", text: "audio transcription", model: "w1"}
	vidProvider := &mockVideoProvider{id: "vid", text: "video description", model: "g1"}

	registry := NewRegistry()
	registry.Register(imgProvider)
	registry.Register(audioProvider)
	registry.Register(vidProvider)

	pipeline := NewPipeline(registry, PipelineConfig{})

	result, err := pipeline.Process(context.Background(), []Attachment{
		{Data: []byte{0xFF}, MIMEType: "image/jpeg", Kind: KindImage, Index: 0},
		{Data: []byte{0x00}, MIMEType: "audio/wav", Kind: KindAudio, Index: 1},
		{Data: []byte{0x00}, MIMEType: "video/mp4", Kind: KindVideo, Index: 2},
		{Data: []byte{0xFF}, MIMEType: "image/png", Kind: KindImage, Index: 3},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Outputs) != 4 {
		t.Fatalf("expected 4 outputs, got %d", len(result.Outputs))
	}
	if !result.AppliedImage {
		t.Error("expected AppliedImage to be true")
	}
	if !result.AppliedAudio {
		t.Error("expected AppliedAudio to be true")
	}
	if !result.AppliedVideo {
		t.Error("expected AppliedVideo to be true")
	}
}

// ---------------------------------------------------------------------------
// TestPipelineGrouping
// ---------------------------------------------------------------------------

func TestPipelineGrouping(t *testing.T) {
	t.Parallel()

	imgProvider := &mockImageProvider{id: "img", text: "img", model: "v1"}
	audioProvider := &mockAudioProvider{id: "aud", text: "aud", model: "w1"}

	registry := NewRegistry()
	registry.Register(imgProvider)
	registry.Register(audioProvider)

	pipeline := NewPipeline(registry, PipelineConfig{})

	attachments := []Attachment{
		{Data: []byte{0xFF}, MIMEType: "image/jpeg", Kind: KindImage, Index: 0},
		{Data: []byte{0xFF}, MIMEType: "image/png", Kind: KindImage, Index: 1},
		{Data: []byte{0x00}, MIMEType: "audio/wav", Kind: KindAudio, Index: 2},
	}

	result, err := pipeline.Process(context.Background(), attachments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if imgProvider.callCount.Load() != 2 {
		t.Errorf("expected 2 image provider calls, got %d", imgProvider.callCount.Load())
	}
	if audioProvider.callCount.Load() != 1 {
		t.Errorf("expected 1 audio provider call, got %d", audioProvider.callCount.Load())
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %d", len(result.Errors))
	}
}

// ---------------------------------------------------------------------------
// TestPipelineConcurrency
// ---------------------------------------------------------------------------

func TestPipelineConcurrency(t *testing.T) {
	t.Parallel()

	const concurrencyDelay = 50 * time.Millisecond
	const imageCount = 6
	const imageConcurrency = 2

	imgProvider := &mockImageProvider{
		id:    "img",
		text:  "described",
		model: "v1",
		delay: concurrencyDelay,
	}

	registry := NewRegistry()
	registry.Register(imgProvider)

	pipeline := NewPipeline(registry, PipelineConfig{
		ImageConcurrency: imageConcurrency,
	})

	var attachments []Attachment
	for i := range imageCount {
		attachments = append(attachments, Attachment{
			Data:     []byte{0xFF},
			MIMEType: "image/jpeg",
			Kind:     KindImage,
			Index:    i,
		})
	}

	start := time.Now()
	result, err := pipeline.Process(context.Background(), attachments)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Outputs) != imageCount {
		t.Errorf("expected %d outputs, got %d", imageCount, len(result.Outputs))
	}

	// With concurrency=2 and 6 items at 50ms each, minimum time is 3*50ms=150ms.
	// Without concurrency it would be 6*50ms=300ms.
	// Allow generous bounds for CI.
	minExpected := time.Duration(imageCount/imageConcurrency) * concurrencyDelay
	if elapsed < minExpected/2 {
		t.Errorf("processing finished too fast (%v), expected at least ~%v", elapsed, minExpected)
	}
}

// ---------------------------------------------------------------------------
// TestPipelineErrors
// ---------------------------------------------------------------------------

func TestPipelineErrors(t *testing.T) {
	t.Parallel()

	imgProvider := &mockImageProvider{
		id:  "img",
		err: fmt.Errorf("vision api unavailable"),
	}

	registry := NewRegistry()
	registry.Register(imgProvider)

	pipeline := NewPipeline(registry, PipelineConfig{})

	result, err := pipeline.Process(context.Background(), []Attachment{
		{Data: []byte{0xFF}, MIMEType: "image/jpeg", Kind: KindImage, Index: 0},
		{Data: []byte{0xFF}, MIMEType: "image/png", Kind: KindImage, Index: 1},
	})
	if err != nil {
		t.Fatalf("unexpected pipeline-level error: %v", err)
	}

	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(result.Errors))
	}
	if len(result.Outputs) != 0 {
		t.Errorf("expected 0 outputs, got %d", len(result.Outputs))
	}
	if result.AppliedImage {
		t.Error("expected AppliedImage to be false when all images failed")
	}
}

// ---------------------------------------------------------------------------
// TestPipelineProcessSingleWithFile
// ---------------------------------------------------------------------------

func TestPipelineProcessSingleWithFile(t *testing.T) {
	t.Parallel()

	imgProvider := &mockImageProvider{
		id:    "img",
		text:  "file-based image",
		model: "v1",
	}

	registry := NewRegistry()
	registry.Register(imgProvider)

	pipeline := NewPipeline(registry, PipelineConfig{})

	// Create a temp file with JPEG-like content.
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jpg")
	if err := os.WriteFile(path, []byte{0xFF, 0xD8, 0xFF, 0xE0}, 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	result, err := pipeline.ProcessSingle(context.Background(), Attachment{
		Path:  path,
		Kind:  KindImage,
		Index: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "file-based image" {
		t.Errorf("unexpected text: %q", result.Text)
	}
}

// ---------------------------------------------------------------------------
// TestPipelineProcessSingleAutoDetectMIME
// ---------------------------------------------------------------------------

func TestPipelineProcessSingleAutoDetectMIME(t *testing.T) {
	t.Parallel()

	imgProvider := &mockImageProvider{
		id:    "img",
		text:  "auto-detected",
		model: "v1",
	}

	registry := NewRegistry()
	registry.Register(imgProvider)

	pipeline := NewPipeline(registry, PipelineConfig{})

	// PNG magic bytes without explicit MIME or kind.
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	result, err := pipeline.ProcessSingle(context.Background(), Attachment{
		Data:  data,
		Index: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Kind != CapabilityImage {
		t.Errorf("expected kind %q, got %q", CapabilityImage, result.Kind)
	}
}

// ---------------------------------------------------------------------------
// TestPipelineEmptyAttachments
// ---------------------------------------------------------------------------

func TestPipelineEmptyAttachments(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	pipeline := NewPipeline(registry, PipelineConfig{})

	result, err := pipeline.Process(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Outputs) != 0 {
		t.Errorf("expected 0 outputs for empty attachments, got %d", len(result.Outputs))
	}
}

// ---------------------------------------------------------------------------
// TestPipelineNoProvider
// ---------------------------------------------------------------------------

func TestPipelineNoProvider(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	pipeline := NewPipeline(registry, PipelineConfig{})

	result, err := pipeline.Process(context.Background(), []Attachment{
		{Data: []byte{0xFF}, MIMEType: "image/jpeg", Kind: KindImage, Index: 0},
	})
	if err != nil {
		t.Fatalf("unexpected pipeline-level error: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error for missing provider, got %d", len(result.Errors))
	}
}

// ---------------------------------------------------------------------------
// TestPipelineFileSizeLimit
// ---------------------------------------------------------------------------

func TestPipelineFileSizeLimit(t *testing.T) {
	t.Parallel()

	imgProvider := &mockImageProvider{id: "img", text: "ok", model: "v1"}

	registry := NewRegistry()
	registry.Register(imgProvider)

	const maxSize = 100
	pipeline := NewPipeline(registry, PipelineConfig{
		MaxFileSizeBytes: maxSize,
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "large.jpg")
	largeData := make([]byte, maxSize+1)
	if err := os.WriteFile(path, largeData, 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	_, err := pipeline.ProcessSingle(context.Background(), Attachment{
		Path:  path,
		Kind:  KindImage,
		Index: 0,
	})
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

// ---------------------------------------------------------------------------
// TestPipelineDefaultConfig
// ---------------------------------------------------------------------------

func TestPipelineDefaultConfig(t *testing.T) {
	t.Parallel()

	pipeline := NewPipeline(NewRegistry(), PipelineConfig{})

	if pipeline.config.ImageConcurrency != defaultImageConcurrency {
		t.Errorf("expected default image concurrency %d, got %d",
			defaultImageConcurrency, pipeline.config.ImageConcurrency)
	}
	if pipeline.config.AudioConcurrency != defaultAudioConcurrency {
		t.Errorf("expected default audio concurrency %d, got %d",
			defaultAudioConcurrency, pipeline.config.AudioConcurrency)
	}
	if pipeline.config.VideoConcurrency != defaultVideoConcurrency {
		t.Errorf("expected default video concurrency %d, got %d",
			defaultVideoConcurrency, pipeline.config.VideoConcurrency)
	}
	if pipeline.config.MaxFileSizeBytes != defaultMaxFileSize {
		t.Errorf("expected default max file size %d, got %d",
			defaultMaxFileSize, pipeline.config.MaxFileSizeBytes)
	}
}

// ---------------------------------------------------------------------------
// TestPipelineKindAutoDetect
// ---------------------------------------------------------------------------

func TestPipelineKindAutoDetect(t *testing.T) {
	t.Parallel()

	audioProvider := &mockAudioProvider{id: "aud", text: "hello", model: "w1"}

	registry := NewRegistry()
	registry.Register(audioProvider)

	pipeline := NewPipeline(registry, PipelineConfig{})

	// Provide MIME type but no Kind - pipeline should auto-detect.
	result, err := pipeline.Process(context.Background(), []Attachment{
		{Data: []byte{0x00}, MIMEType: "audio/wav", Index: 0},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.AppliedAudio {
		t.Error("expected AppliedAudio to be true with auto-detected kind")
	}
}
