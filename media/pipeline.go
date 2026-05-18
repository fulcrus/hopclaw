package media

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Pipeline constants
// ---------------------------------------------------------------------------

const (
	defaultImageConcurrency = 3
	defaultAudioConcurrency = 2
	defaultVideoConcurrency = 1
	defaultMaxFileSize      = 100 * 1024 * 1024 // 100 MiB
	defaultThumbnailSize    = ThumbnailMedium
)

// ---------------------------------------------------------------------------
// Pipeline
// ---------------------------------------------------------------------------

// Pipeline orchestrates media understanding across multiple attachments.
type Pipeline struct {
	registry    *Registry
	config      PipelineConfig
	ocrProvider OCRProvider
}

// NewPipeline creates a new media processing pipeline.
func NewPipeline(registry *Registry, config PipelineConfig) *Pipeline {
	if config.ImageConcurrency <= 0 {
		config.ImageConcurrency = defaultImageConcurrency
	}
	if config.AudioConcurrency <= 0 {
		config.AudioConcurrency = defaultAudioConcurrency
	}
	if config.VideoConcurrency <= 0 {
		config.VideoConcurrency = defaultVideoConcurrency
	}
	if config.MaxFileSizeBytes <= 0 {
		config.MaxFileSizeBytes = defaultMaxFileSize
	}
	if config.ThumbnailSize <= 0 {
		config.ThumbnailSize = defaultThumbnailSize
	}

	return &Pipeline{
		registry: registry,
		config:   config,
	}
}

// ---------------------------------------------------------------------------
// Pipeline result types
// ---------------------------------------------------------------------------

// PipelineResult holds the aggregated results from processing multiple attachments.
type PipelineResult struct {
	Outputs      []UnderstandingOutput `json:"outputs"`
	Thumbnails   []ThumbnailOutput     `json:"thumbnails,omitempty"`
	AppliedImage bool                  `json:"applied_image"`
	AppliedAudio bool                  `json:"applied_audio"`
	AppliedVideo bool                  `json:"applied_video"`
	AppliedOCR   bool                  `json:"applied_ocr"`
	Errors       []PipelineError       `json:"errors,omitempty"`
}

// ThumbnailOutput holds a generated thumbnail for an attachment.
type ThumbnailOutput struct {
	AttachmentIndex int    `json:"attachment_index"`
	Data            []byte `json:"-"`
	MIMEType        string `json:"mime_type"`
}

// PipelineError records a processing failure for a specific attachment.
type PipelineError struct {
	AttachmentIndex int    `json:"attachment_index"`
	Error           string `json:"error"`
}

// ---------------------------------------------------------------------------
// Process: batch processing
// ---------------------------------------------------------------------------

// Process handles multiple attachments concurrently, grouping them by kind
// and respecting per-kind concurrency limits.
func (p *Pipeline) Process(ctx context.Context, attachments []Attachment) (*PipelineResult, error) {
	if len(attachments) == 0 {
		return &PipelineResult{}, nil
	}

	// Group attachments by kind.
	var images, audios, videos []Attachment
	for i := range attachments {
		att := attachments[i]
		if att.Kind == "" && att.MIMEType != "" {
			att.Kind = DetectKind(att.MIMEType)
		}
		switch att.Kind {
		case KindImage:
			images = append(images, att)
		case KindAudio:
			audios = append(audios, att)
		case KindVideo:
			videos = append(videos, att)
		default:
			// Unknown kind: skip silently but note the error.
		}
	}

	result := &PipelineResult{}

	var mu sync.Mutex // guards result fields
	var wg sync.WaitGroup

	// Process each kind group with its own concurrency limit.
	if len(images) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outputs, errs := p.processGroup(ctx, images, p.config.ImageConcurrency)
			mu.Lock()
			result.Outputs = append(result.Outputs, outputs...)
			result.Errors = append(result.Errors, errs...)
			if len(outputs) > 0 {
				result.AppliedImage = true
			}
			mu.Unlock()
		}()
	}

	if len(audios) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outputs, errs := p.processGroup(ctx, audios, p.config.AudioConcurrency)
			mu.Lock()
			result.Outputs = append(result.Outputs, outputs...)
			result.Errors = append(result.Errors, errs...)
			if len(outputs) > 0 {
				result.AppliedAudio = true
			}
			mu.Unlock()
		}()
	}

	if len(videos) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outputs, errs := p.processGroup(ctx, videos, p.config.VideoConcurrency)
			mu.Lock()
			result.Outputs = append(result.Outputs, outputs...)
			result.Errors = append(result.Errors, errs...)
			if len(outputs) > 0 {
				result.AppliedVideo = true
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Post-processing: generate thumbnails for all attachments.
	if p.config.GenerateThumbnails {
		for i := range attachments {
			att := attachments[i]
			if len(att.Data) == 0 {
				continue
			}
			thumbData, err := GenerateThumbnail(att.Data, att.MIMEType, p.config.ThumbnailSize)
			if err != nil {
				continue // skip thumbnail generation errors
			}
			result.Thumbnails = append(result.Thumbnails, ThumbnailOutput{
				AttachmentIndex: att.Index,
				Data:            thumbData,
				MIMEType:        "image/jpeg",
			})
		}
	}

	return result, nil
}

// processGroup processes a homogeneous set of attachments with a concurrency
// semaphore. It returns collected outputs and errors.
func (p *Pipeline) processGroup(ctx context.Context, attachments []Attachment, concurrency int) ([]UnderstandingOutput, []PipelineError) {
	var (
		mu      sync.Mutex // guards outputs and errs
		outputs []UnderstandingOutput
		errs    []PipelineError
	)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range attachments {
		att := attachments[i]

		wg.Add(1)
		go func() {
			defer wg.Done()

			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			out, err := p.ProcessSingle(ctx, att)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, PipelineError{
					AttachmentIndex: att.Index,
					Error:           err.Error(),
				})
				return
			}
			outputs = append(outputs, *out)
		}()
	}

	wg.Wait()
	return outputs, errs
}

// ---------------------------------------------------------------------------
// ProcessSingle: single attachment processing
// ---------------------------------------------------------------------------

// ProcessSingle dispatches a single attachment to the appropriate provider
// based on its kind. It loads file data if Path is set and detects MIME
// type if not already set.
func (p *Pipeline) ProcessSingle(ctx context.Context, att Attachment) (*UnderstandingOutput, error) {
	// Load data from file if needed.
	if len(att.Data) == 0 && att.Path != "" {
		data, err := os.ReadFile(att.Path)
		if err != nil {
			return nil, fmt.Errorf("media: reading file %q: %w", att.Path, err)
		}
		if int64(len(data)) > p.config.MaxFileSizeBytes {
			return nil, fmt.Errorf("media: file %q exceeds maximum size of %d bytes", att.Path, p.config.MaxFileSizeBytes)
		}
		att.Data = data
	}

	// Detect MIME type if not set.
	if att.MIMEType == "" {
		if att.Path != "" {
			att.MIMEType = DetectMIMEType(att.Path)
		} else if len(att.Data) > 0 {
			att.MIMEType = DetectMIMETypeFromBytes(att.Data)
		}
	}

	// Detect kind if not set.
	if att.Kind == "" || att.Kind == KindUnknown {
		att.Kind = DetectKind(att.MIMEType)
	}

	start := time.Now()

	switch att.Kind {
	case KindImage:
		return p.processImage(ctx, att, start)
	case KindAudio:
		return p.processAudio(ctx, att, start)
	case KindVideo:
		return p.processVideo(ctx, att, start)
	default:
		return nil, fmt.Errorf("media: unsupported media kind %q", att.Kind)
	}
}

// SetOCRProvider sets the OCR provider for the pipeline.
func (p *Pipeline) SetOCRProvider(provider OCRProvider) {
	p.ocrProvider = provider
}

// processImage dispatches an image attachment to the first available image provider.
// If AutoResize is enabled, images are resized before sending to the provider.
func (p *Pipeline) processImage(ctx context.Context, att Attachment, start time.Time) (*UnderstandingOutput, error) {
	provider, err := p.registry.FindImageProvider("")
	if err != nil {
		return nil, err
	}

	data := att.Data
	mimeType := att.MIMEType

	// Pre-processing: auto-resize for vision providers.
	if p.config.AutoResize {
		resized, newMIME, resizeErr := ResizeForVision(data, mimeType)
		if resizeErr == nil {
			data = resized
			mimeType = newMIME
		}
		// On resize error, proceed with original data.
	}

	result, err := provider.DescribeImage(ctx, ImageRequest{
		Data:     data,
		MIMEType: mimeType,
	})
	if err != nil {
		return nil, err
	}

	return &UnderstandingOutput{
		Kind:            CapabilityImage,
		AttachmentIndex: att.Index,
		Text:            result.Text,
		Provider:        provider.ID(),
		Model:           result.Model,
		DurationMs:      time.Since(start).Milliseconds(),
	}, nil
}

// processAudio dispatches an audio attachment to the first available audio provider.
func (p *Pipeline) processAudio(ctx context.Context, att Attachment, start time.Time) (*UnderstandingOutput, error) {
	provider, err := p.registry.FindAudioProvider("")
	if err != nil {
		return nil, err
	}

	result, err := provider.TranscribeAudio(ctx, AudioRequest{
		Data:     att.Data,
		MIMEType: att.MIMEType,
	})
	if err != nil {
		return nil, err
	}

	return &UnderstandingOutput{
		Kind:            CapabilityAudio,
		AttachmentIndex: att.Index,
		Text:            result.Text,
		Provider:        provider.ID(),
		Model:           result.Model,
		DurationMs:      time.Since(start).Milliseconds(),
	}, nil
}

// processVideo dispatches a video attachment to the first available video provider.
func (p *Pipeline) processVideo(ctx context.Context, att Attachment, start time.Time) (*UnderstandingOutput, error) {
	provider, err := p.registry.FindVideoProvider("")
	if err != nil {
		return nil, err
	}

	result, err := provider.DescribeVideo(ctx, VideoRequest{
		Data:     att.Data,
		MIMEType: att.MIMEType,
	})
	if err != nil {
		return nil, err
	}

	return &UnderstandingOutput{
		Kind:            CapabilityVideo,
		AttachmentIndex: att.Index,
		Text:            result.Text,
		Provider:        provider.ID(),
		Model:           result.Model,
		DurationMs:      time.Since(start).Milliseconds(),
	}, nil
}

// ---------------------------------------------------------------------------
// ProcessOCR: OCR processing
// ---------------------------------------------------------------------------

// ProcessOCR performs OCR on an image attachment using the configured OCR
// provider. Returns an error if no OCR provider is set.
func (p *Pipeline) ProcessOCR(ctx context.Context, att Attachment, cfg OCRConfig) (*UnderstandingOutput, error) {
	if p.ocrProvider == nil {
		return nil, fmt.Errorf("media: no ocr provider configured")
	}

	// Load data from file if needed.
	if len(att.Data) == 0 && att.Path != "" {
		data, err := os.ReadFile(att.Path)
		if err != nil {
			return nil, fmt.Errorf("media: reading file %q: %w", att.Path, err)
		}
		att.Data = data
	}

	if att.MIMEType == "" {
		if att.Path != "" {
			att.MIMEType = DetectMIMEType(att.Path)
		} else if len(att.Data) > 0 {
			att.MIMEType = DetectMIMETypeFromBytes(att.Data)
		}
	}

	start := time.Now()

	result, err := PerformOCR(ctx, p.ocrProvider, att.Data, att.MIMEType, cfg)
	if err != nil {
		return nil, err
	}

	return &UnderstandingOutput{
		Kind:            CapabilityOCR,
		AttachmentIndex: att.Index,
		Text:            result.Text,
		Provider:        p.ocrProvider.ID(),
		DurationMs:      time.Since(start).Milliseconds(),
	}, nil
}
