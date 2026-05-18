package mediagen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/model"
)

const (
	openAIProviderID         = "openai"
	openAIDefaultBaseURL     = "https://api.openai.com/v1"
	openAIDefaultImageModel  = "gpt-image-1"
	openAIDefaultVideoModel  = "sora-2"
	openAIDefaultImageSize   = "1024x1024"
	openAIDefaultImageMIME   = "image/png"
	openAIDefaultVideoMIME   = "video/mp4"
	openAIDefaultTimeout     = 120 * time.Second
	openAIVideoPollInterval  = 2500 * time.Millisecond
	openAIVideoPollAttempts  = 120
	openAIMaxInputImages     = 5
	openAIMaxInputVideos     = 1
	openAIImageResponseField = "b64_json"
)

var (
	openAIImageSizes  = []string{"1024x1024", "1024x1536", "1536x1024", "1024x1792", "1792x1024"}
	openAIVideoSizes  = []string{"720x1280", "1280x720", "1024x1792", "1792x1024"}
	openAIVideoLimits = []int{4, 8, 12}
)

func init() {
	RegisterBuiltinProviderBuilder(openAIProviderID, func(entry model.ProviderEntry) (Provider, error) {
		return NewOpenAIProvider(OpenAIConfig{
			BaseURL: entry.BaseURL,
			APIKey:  entry.APIKey,
			Timeout: entry.Timeout,
			Headers: entry.Headers,
		})
	})
}

type OpenAIConfig struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	Headers map[string]string
}

type OpenAIProvider struct {
	config OpenAIConfig
	client *http.Client
}

func NewOpenAIProvider(cfg OpenAIConfig) (*OpenAIProvider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("mediagen/openai: api key is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = openAIDefaultBaseURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = openAIDefaultTimeout
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	return &OpenAIProvider{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (p *OpenAIProvider) ID() string    { return openAIProviderID }
func (p *OpenAIProvider) Label() string { return "OpenAI" }

func (p *OpenAIProvider) DefaultImageModel() string { return openAIDefaultImageModel }
func (p *OpenAIProvider) ImageModels() []string {
	return []string{openAIDefaultImageModel}
}

func (p *OpenAIProvider) ImageCapabilities() ImageCapabilities {
	return ImageCapabilities{
		MaxCount:            4,
		MaxInputImages:      openAIMaxInputImages,
		SupportsEdit:        true,
		SupportsSize:        true,
		SupportsAspectRatio: false,
		SupportsResolution:  false,
		Sizes:               cloneStrings(openAIImageSizes),
	}
}

func (p *OpenAIProvider) DefaultVideoModel() string { return openAIDefaultVideoModel }
func (p *OpenAIProvider) VideoModels() []string {
	return []string{openAIDefaultVideoModel, "sora-2-pro"}
}

func (p *OpenAIProvider) VideoCapabilities() VideoCapabilities {
	return VideoCapabilities{
		MaxVideos:            1,
		MaxInputImages:       1,
		MaxInputVideos:       1,
		MaxDurationSeconds:   openAIVideoLimits[len(openAIVideoLimits)-1],
		SupportedDurations:   cloneInts(openAIVideoLimits),
		SupportsImageToVideo: true,
		SupportsVideoToVideo: true,
		SupportsSize:         true,
		SupportsAspectRatio:  true,
		SupportsResolution:   true,
		SupportsAudio:        false,
		Sizes:                cloneStrings(openAIVideoSizes),
		AspectRatios:         []string{"9:16", "16:9", "4:7", "7:4"},
		Resolutions:          []string{"1080P"},
	}
}

func (p *OpenAIProvider) GenerateImage(ctx context.Context, req ImageRequest) (*ImageResult, error) {
	model := firstNonEmpty(req.Model, openAIDefaultImageModel)
	count := req.Count
	if count <= 0 {
		count = 1
	}
	if count > 4 {
		return nil, fmt.Errorf("mediagen/openai: count %d exceeds maximum 4", count)
	}
	if len(req.InputImages) > openAIMaxInputImages {
		return nil, fmt.Errorf("mediagen/openai: input_images exceeds maximum %d", openAIMaxInputImages)
	}
	size := resolveOpenAIImageSize(req.Size, req.AspectRatio)
	if size == "" {
		size = openAIDefaultImageSize
	}

	payload := map[string]any{
		"model":           model,
		"prompt":          strings.TrimSpace(req.Prompt),
		"n":               count,
		"size":            size,
		"response_format": openAIImageResponseField,
	}
	endpoint := "/images/generations"
	if len(req.InputImages) > 0 {
		endpoint = "/images/edits"
		images := make([]map[string]any, 0, len(req.InputImages))
		for _, asset := range req.InputImages {
			images = append(images, map[string]any{
				"image_url": dataURL(asset, openAIDefaultImageMIME),
			})
		}
		payload["images"] = images
	}

	var resp openAIImageResponse
	if err := p.doJSON(ctx, http.MethodPost, endpoint, payload, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("mediagen/openai: image generation response missing data")
	}

	images := make([]GeneratedAsset, 0, len(resp.Data))
	revised := make([]string, 0, len(resp.Data))
	for index, item := range resp.Data {
		if strings.TrimSpace(item.B64JSON) == "" {
			continue
		}
		body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(item.B64JSON))
		if err != nil {
			return nil, fmt.Errorf("mediagen/openai: decode generated image %d: %w", index, err)
		}
		images = append(images, GeneratedAsset{
			Buffer:   body,
			MIMEType: openAIDefaultImageMIME,
			FileName: fmt.Sprintf("image-%d.png", index+1),
		})
		if prompt := strings.TrimSpace(item.RevisedPrompt); prompt != "" {
			revised = append(revised, prompt)
		}
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("mediagen/openai: image generation response missing b64_json payloads")
	}

	return &ImageResult{
		Images:         images,
		Model:          firstNonEmpty(resp.Model, model),
		RevisedPrompts: revised,
		Metadata: map[string]any{
			"size":  size,
			"count": len(images),
			"edit":  len(req.InputImages) > 0,
		},
	}, nil
}

func (p *OpenAIProvider) GenerateVideo(ctx context.Context, req VideoRequest) (*VideoResult, error) {
	if len(req.InputImages) > 1 {
		return nil, fmt.Errorf("mediagen/openai: input_images supports at most 1 item")
	}
	if len(req.InputVideos) > openAIMaxInputVideos {
		return nil, fmt.Errorf("mediagen/openai: input_videos supports at most %d item", openAIMaxInputVideos)
	}
	if len(req.InputImages) > 0 && len(req.InputVideos) > 0 {
		return nil, fmt.Errorf("mediagen/openai: image and video references cannot be mixed")
	}

	model := firstNonEmpty(req.Model, openAIDefaultVideoModel)
	seconds := resolveOpenAIVideoDuration(req.DurationSeconds)
	size := resolveOpenAIVideoSize(req.Size, req.AspectRatio, req.Resolution)

	var submitted openAIVideoResponse
	switch {
	case len(req.InputImages) == 1:
		payload := map[string]any{
			"prompt": strings.TrimSpace(req.Prompt),
			"model":  model,
			"input_reference": map[string]any{
				"image_url": dataURL(req.InputImages[0], openAIDefaultImageMIME),
			},
		}
		if seconds != "" {
			payload["seconds"] = seconds
		}
		if size != "" {
			payload["size"] = size
		}
		if err := p.doJSON(ctx, http.MethodPost, "/videos", payload, &submitted); err != nil {
			return nil, err
		}
	case len(req.InputVideos) == 1:
		fields := map[string]string{
			"prompt": strings.TrimSpace(req.Prompt),
			"model":  model,
		}
		if seconds != "" {
			fields["seconds"] = seconds
		}
		if size != "" {
			fields["size"] = size
		}
		if err := p.doMultipart(ctx, "/videos", fields, req.InputVideos[0], &submitted); err != nil {
			return nil, err
		}
	default:
		payload := map[string]any{
			"prompt": strings.TrimSpace(req.Prompt),
			"model":  model,
		}
		if seconds != "" {
			payload["seconds"] = seconds
		}
		if size != "" {
			payload["size"] = size
		}
		if err := p.doJSON(ctx, http.MethodPost, "/videos", payload, &submitted); err != nil {
			return nil, err
		}
	}

	videoID := strings.TrimSpace(submitted.ID)
	if videoID == "" {
		return nil, fmt.Errorf("mediagen/openai: video generation response missing id")
	}
	completed, err := p.pollVideo(ctx, videoID)
	if err != nil {
		return nil, err
	}
	asset, err := p.downloadVideo(ctx, videoID)
	if err != nil {
		return nil, err
	}
	return &VideoResult{
		Videos: []GeneratedAsset{asset},
		Model:  firstNonEmpty(strings.TrimSpace(completed.Model), strings.TrimSpace(submitted.Model), model),
		Metadata: map[string]any{
			"video_id": videoID,
			"status":   firstNonEmpty(strings.TrimSpace(completed.Status), strings.TrimSpace(submitted.Status)),
			"seconds":  firstNonEmpty(strings.TrimSpace(completed.Seconds), strings.TrimSpace(submitted.Seconds), seconds),
			"size":     firstNonEmpty(strings.TrimSpace(completed.Size), strings.TrimSpace(submitted.Size), size),
		},
	}, nil
}

type openAIImageResponse struct {
	Data []struct {
		B64JSON       string `json:"b64_json"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
	Model string `json:"model"`
}

type openAIVideoResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Prompt  string `json:"prompt"`
	Seconds string `json:"seconds"`
	Size    string `json:"size"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type openAIErrorResponse struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p *OpenAIProvider) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mediagen/openai: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.config.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mediagen/openai: create request: %w", err)
	}
	p.applyHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	return p.do(req, out)
}

func (p *OpenAIProvider) doMultipart(ctx context.Context, path string, fields map[string]string, asset InputAsset, out any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if err := writer.WriteField(key, value); err != nil {
			return fmt.Errorf("mediagen/openai: write multipart field %q: %w", key, err)
		}
	}
	fileName := normalizeAssetFileName(asset.FileName, "reference"+fileExtForMIME(asset.MIMEType))
	part, err := writer.CreateFormFile("input_reference", fileName)
	if err != nil {
		return fmt.Errorf("mediagen/openai: create multipart file: %w", err)
	}
	if _, err := part.Write(asset.Buffer); err != nil {
		return fmt.Errorf("mediagen/openai: write multipart file: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("mediagen/openai: close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.BaseURL+path, &body)
	if err != nil {
		return fmt.Errorf("mediagen/openai: create multipart request: %w", err)
	}
	p.applyHeaders(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return p.do(req, out)
}

func (p *OpenAIProvider) do(req *http.Request, out any) error {
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("mediagen/openai: send request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("mediagen/openai: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return parseOpenAIError(resp.StatusCode, body)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("mediagen/openai: decode response: %w", err)
	}
	return nil
}

func (p *OpenAIProvider) pollVideo(ctx context.Context, videoID string) (*openAIVideoResponse, error) {
	for attempt := 0; attempt < openAIVideoPollAttempts; attempt++ {
		var payload openAIVideoResponse
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.BaseURL+"/videos/"+videoID, nil)
		if err != nil {
			return nil, fmt.Errorf("mediagen/openai: create poll request: %w", err)
		}
		p.applyHeaders(req)
		if err := p.do(req, &payload); err != nil {
			return nil, err
		}
		switch strings.TrimSpace(payload.Status) {
		case "completed":
			return &payload, nil
		case "failed":
			if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
				return nil, fmt.Errorf("mediagen/openai: %s", strings.TrimSpace(payload.Error.Message))
			}
			return nil, fmt.Errorf("mediagen/openai: video generation failed")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(openAIVideoPollInterval):
		}
	}
	return nil, fmt.Errorf("mediagen/openai: video generation timed out")
}

func (p *OpenAIProvider) downloadVideo(ctx context.Context, videoID string) (GeneratedAsset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.BaseURL+"/videos/"+videoID+"/content?variant=video", nil)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/openai: create download request: %w", err)
	}
	p.applyHeaders(req)
	req.Header.Set("Accept", "application/binary")
	resp, err := p.client.Do(req)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/openai: download generated video: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/openai: read generated video: %w", err)
	}
	if resp.StatusCode >= 400 {
		return GeneratedAsset{}, parseOpenAIError(resp.StatusCode, body)
	}
	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = openAIDefaultVideoMIME
	}
	return GeneratedAsset{
		Buffer:   body,
		MIMEType: mimeType,
		FileName: "video-1" + fileExtForMIME(mimeType),
	}, nil
}

func (p *OpenAIProvider) applyHeaders(req *http.Request) {
	for key, value := range p.config.Headers {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
}

func parseOpenAIError(status int, body []byte) error {
	var payload openAIErrorResponse
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return fmt.Errorf("mediagen/openai: status %d: %s", status, strings.TrimSpace(payload.Error.Message))
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("mediagen/openai: status %d: %s", status, message)
}

func resolveOpenAIImageSize(size, aspectRatio string) string {
	if strings.TrimSpace(size) != "" {
		return strings.TrimSpace(size)
	}
	switch strings.TrimSpace(aspectRatio) {
	case "2:3":
		return "1024x1536"
	case "3:2":
		return "1536x1024"
	case "9:16":
		return "1024x1792"
	case "16:9":
		return "1792x1024"
	default:
		return ""
	}
}

func resolveOpenAIVideoDuration(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	rounded := seconds
	best := openAIVideoLimits[0]
	for _, candidate := range openAIVideoLimits {
		if absInt(candidate-rounded) < absInt(best-rounded) {
			best = candidate
		}
	}
	return fmt.Sprintf("%d", best)
}

func resolveOpenAIVideoSize(size, aspectRatio, resolution string) string {
	if strings.TrimSpace(size) != "" {
		return strings.TrimSpace(size)
	}
	switch strings.TrimSpace(aspectRatio) {
	case "9:16":
		return "720x1280"
	case "16:9":
		return "1280x720"
	case "4:7":
		return "1024x1792"
	case "7:4":
		return "1792x1024"
	}
	if strings.EqualFold(strings.TrimSpace(resolution), "1080P") {
		return "1792x1024"
	}
	return ""
}

func dataURL(asset InputAsset, fallbackMIME string) string {
	mimeType := strings.TrimSpace(asset.MIMEType)
	if mimeType == "" {
		mimeType = fallbackMIME
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(asset.Buffer)
}

func fileExtForMIME(mimeType string) string {
	switch {
	case strings.Contains(strings.ToLower(mimeType), "jpeg"):
		return ".jpg"
	case strings.Contains(strings.ToLower(mimeType), "png"):
		return ".png"
	case strings.Contains(strings.ToLower(mimeType), "webp"):
		return ".webp"
	case strings.Contains(strings.ToLower(mimeType), "mp4"):
		return ".mp4"
	case strings.Contains(strings.ToLower(mimeType), "webm"):
		return ".webm"
	case strings.Contains(strings.ToLower(mimeType), "mpeg"):
		return ".mp3"
	default:
		ext := strings.TrimSpace(filepath.Ext(mimeType))
		if ext != "" {
			return ext
		}
		return ".bin"
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
