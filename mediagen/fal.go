package mediagen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/model"
)

const (
	falProviderID            = "fal"
	falDefaultBaseURL        = "https://fal.run"
	falDefaultQueueBaseURL   = "https://queue.fal.run"
	falDefaultImageModel     = "fal-ai/flux/dev"
	falDefaultImageEditPath  = "image-to-image"
	falDefaultVideoModel     = "fal-ai/minimax/video-01-live"
	falDefaultTimeout        = 120 * time.Second
	falDefaultPollInterval   = 2 * time.Second
	falDefaultOutputFormat   = "png"
	falDefaultImageMIME      = "image/png"
	falDefaultVideoMIME      = "video/mp4"
	falDefaultOperationLimit = 10 * time.Minute
)

var (
	falImageSizes        = []string{"1024x1024", "1024x1536", "1536x1024", "1024x1792", "1792x1024"}
	falImageAspectRatios = []string{"1:1", "4:3", "3:4", "16:9", "9:16"}
	falResolutions       = []string{"1K", "2K", "4K"}
)

func init() {
	RegisterBuiltinProviderBuilder(falProviderID, func(entry model.ProviderEntry) (Provider, error) {
		return NewFalProvider(FalConfig{
			BaseURL: entry.BaseURL,
			APIKey:  entry.APIKey,
			Timeout: entry.Timeout,
			Headers: entry.Headers,
		})
	})
}

type FalConfig struct {
	BaseURL      string
	QueueBaseURL string
	APIKey       string
	Timeout      time.Duration
	Headers      map[string]string
}

type FalProvider struct {
	config FalConfig
	client *http.Client
}

func NewFalProvider(cfg FalConfig) (*FalProvider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("mediagen/fal: api key is required")
	}
	cfg.BaseURL = strings.TrimRight(firstNonEmpty(cfg.BaseURL, falDefaultBaseURL), "/")
	cfg.QueueBaseURL = strings.TrimRight(firstNonEmpty(cfg.QueueBaseURL, resolveFalQueueBaseURL(cfg.BaseURL)), "/")
	if cfg.Timeout <= 0 {
		cfg.Timeout = falDefaultTimeout
	}
	return &FalProvider{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (p *FalProvider) ID() string    { return falProviderID }
func (p *FalProvider) Label() string { return "fal" }

func (p *FalProvider) DefaultImageModel() string { return falDefaultImageModel }
func (p *FalProvider) ImageModels() []string {
	return []string{falDefaultImageModel, falDefaultImageModel + "/" + falDefaultImageEditPath}
}

func (p *FalProvider) ImageCapabilities() ImageCapabilities {
	return ImageCapabilities{
		MaxCount:            4,
		MaxInputImages:      1,
		SupportsEdit:        true,
		SupportsSize:        true,
		SupportsAspectRatio: true,
		SupportsResolution:  true,
		Sizes:               cloneStrings(falImageSizes),
		AspectRatios:        cloneStrings(falImageAspectRatios),
		Resolutions:         cloneStrings(falResolutions),
	}
}

func (p *FalProvider) DefaultVideoModel() string { return falDefaultVideoModel }
func (p *FalProvider) VideoModels() []string {
	return []string{
		falDefaultVideoModel,
		"fal-ai/kling-video/v2.1/master/text-to-video",
		"fal-ai/wan/v2.2-a14b/text-to-video",
		"fal-ai/wan/v2.2-a14b/image-to-video",
	}
}

func (p *FalProvider) VideoCapabilities() VideoCapabilities {
	return VideoCapabilities{
		MaxVideos:            1,
		MaxInputImages:       1,
		MaxInputVideos:       0,
		SupportsImageToVideo: true,
		SupportsVideoToVideo: false,
		SupportsSize:         true,
		SupportsAspectRatio:  true,
		SupportsResolution:   true,
		Sizes:                cloneStrings(falImageSizes),
		AspectRatios:         cloneStrings(falImageAspectRatios),
		Resolutions:          cloneStrings(falResolutions),
	}
}

func (p *FalProvider) GenerateImage(ctx context.Context, req ImageRequest) (*ImageResult, error) {
	if len(req.InputImages) > 1 {
		return nil, fmt.Errorf("mediagen/fal: image generation supports at most one reference image")
	}
	ctx, cancel := withRequestTimeout(ctx, req.TimeoutMS, falDefaultOperationLimit)
	defer cancel()

	model := resolveFalImageModel(req.Model, len(req.InputImages) > 0)
	payload := map[string]any{
		"prompt":        strings.TrimSpace(req.Prompt),
		"num_images":    maxInt(req.Count, 1),
		"output_format": falDefaultOutputFormat,
	}
	if imageSize := resolveFalImageSize(req.Size, req.AspectRatio, req.Resolution); imageSize != nil {
		payload["image_size"] = imageSize
	}
	if len(req.InputImages) == 1 {
		payload["image_url"] = dataURL(req.InputImages[0], falDefaultImageMIME)
	}

	var response falImageResponse
	if err := p.doJSON(ctx, http.MethodPost, p.config.BaseURL+"/"+model, payload, &response); err != nil {
		return nil, err
	}
	images := make([]GeneratedAsset, 0, len(response.Images))
	for index, item := range response.Images {
		url := strings.TrimSpace(item.URL)
		if url == "" {
			continue
		}
		asset, err := p.downloadAsset(ctx, url, firstNonEmpty(item.ContentType, falDefaultImageMIME), fmt.Sprintf("image-%d", index+1))
		if err != nil {
			return nil, err
		}
		images = append(images, asset)
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("mediagen/fal: image generation response missing image outputs")
	}
	return &ImageResult{
		Images: images,
		Model:  model,
		Metadata: map[string]any{
			"prompt": response.Prompt,
		},
	}, nil
}

func (p *FalProvider) GenerateVideo(ctx context.Context, req VideoRequest) (*VideoResult, error) {
	if len(req.InputVideos) > 0 {
		return nil, fmt.Errorf("mediagen/fal: video reference inputs are not supported")
	}
	if len(req.InputImages) > 1 {
		return nil, fmt.Errorf("mediagen/fal: video generation supports at most one image reference")
	}
	ctx, cancel := withRequestTimeout(ctx, req.TimeoutMS, falDefaultOperationLimit)
	defer cancel()

	model := firstNonEmpty(req.Model, falDefaultVideoModel)
	payload := map[string]any{
		"prompt": strings.TrimSpace(req.Prompt),
	}
	if len(req.InputImages) == 1 {
		payload["image_url"] = dataURL(req.InputImages[0], falDefaultImageMIME)
	}
	if req.Size != "" {
		payload["size"] = req.Size
	}
	if req.AspectRatio != "" {
		payload["aspect_ratio"] = req.AspectRatio
	}
	if req.Resolution != "" {
		payload["resolution"] = req.Resolution
	}
	if req.DurationSeconds > 0 {
		payload["duration"] = req.DurationSeconds
	}

	var submitted falQueueResponse
	if err := p.doJSON(ctx, http.MethodPost, p.config.QueueBaseURL+"/"+model, payload, &submitted); err != nil {
		return nil, err
	}
	statusURL := strings.TrimSpace(submitted.StatusURL)
	responseURL := strings.TrimSpace(submitted.ResponseURL)
	if statusURL == "" || responseURL == "" {
		return nil, fmt.Errorf("mediagen/fal: queue response missing status or response URL")
	}

	resultPayload, err := p.pollQueue(ctx, statusURL, responseURL)
	if err != nil {
		return nil, err
	}
	videoURL := firstNonEmpty(
		valueOrEmpty(resultPayload.Video, func(v *falVideoAsset) string { return v.URL }),
		firstVideoURL(resultPayload.Videos),
	)
	if videoURL == "" {
		return nil, fmt.Errorf("mediagen/fal: video generation response missing output URL")
	}
	video, err := p.downloadAsset(ctx, videoURL, falDefaultVideoMIME, "video-1")
	if err != nil {
		return nil, err
	}
	return &VideoResult{
		Videos: []GeneratedAsset{video},
		Model:  model,
		Metadata: map[string]any{
			"request_id": submitted.RequestID,
			"prompt":     firstNonEmpty(resultPayload.Prompt, submitted.Prompt),
		},
	}, nil
}

type falImageResponse struct {
	Images []falImageAsset `json:"images"`
	Prompt string          `json:"prompt"`
}

type falImageAsset struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
}

type falVideoAsset struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
}

type falVideoResponse struct {
	Video  *falVideoAsset  `json:"video"`
	Videos []falVideoAsset `json:"videos"`
	Prompt string          `json:"prompt"`
}

type falQueueResponse struct {
	Status      string            `json:"status"`
	RequestID   string            `json:"request_id"`
	StatusURL   string            `json:"status_url"`
	ResponseURL string            `json:"response_url"`
	Prompt      string            `json:"prompt"`
	Detail      string            `json:"detail"`
	Error       *falErrorResponse `json:"error"`
	Response    *falVideoResponse `json:"response"`
}

type falErrorResponse struct {
	Message string `json:"message"`
}

func (p *FalProvider) doJSON(ctx context.Context, method, url string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mediagen/fal: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mediagen/fal: create request: %w", err)
	}
	p.applyHeaders(req)
	return p.do(req, out)
}

func (p *FalProvider) do(req *http.Request, out any) error {
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("mediagen/fal: send request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("mediagen/fal: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return parseFalError(resp.StatusCode, body)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("mediagen/fal: decode response: %w", err)
	}
	return nil
}

func (p *FalProvider) pollQueue(ctx context.Context, statusURL, responseURL string) (*falVideoResponse, error) {
	ticker := time.NewTicker(falDefaultPollInterval)
	defer ticker.Stop()
	for {
		var status falQueueResponse
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
		if err != nil {
			return nil, fmt.Errorf("mediagen/fal: create status request: %w", err)
		}
		p.applyHeaders(req)
		if err := p.do(req, &status); err != nil {
			return nil, err
		}
		switch strings.ToUpper(strings.TrimSpace(status.Status)) {
		case "COMPLETED":
			var response falVideoResponse
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, responseURL, nil)
			if err != nil {
				return nil, fmt.Errorf("mediagen/fal: create response request: %w", err)
			}
			p.applyHeaders(req)
			if err := p.do(req, &response); err != nil {
				return nil, err
			}
			if response.Video != nil || len(response.Videos) > 0 {
				return &response, nil
			}
			return nil, fmt.Errorf("mediagen/fal: completed response missing video payload")
		case "FAILED", "CANCELLED":
			return nil, fmt.Errorf("mediagen/fal: %s", firstNonEmpty(status.Detail, valueOrEmpty(status.Error, func(e *falErrorResponse) string { return e.Message }), "request failed"))
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("mediagen/fal: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (p *FalProvider) downloadAsset(ctx context.Context, url, fallbackMIME, baseName string) (GeneratedAsset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/fal: create download request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/fal: download generated asset: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/fal: read generated asset: %w", err)
	}
	if resp.StatusCode >= 400 {
		return GeneratedAsset{}, parseFalError(resp.StatusCode, body)
	}
	mimeType := firstNonEmpty(resp.Header.Get("Content-Type"), fallbackMIME)
	return GeneratedAsset{
		Buffer:   body,
		MIMEType: mimeType,
		FileName: baseName + falFileExtension(mimeType),
	}, nil
}

func (p *FalProvider) applyHeaders(req *http.Request) {
	for key, value := range p.config.Headers {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	req.Header.Set("Authorization", "Key "+p.config.APIKey)
	req.Header.Set("Content-Type", "application/json")
}

func resolveFalQueueBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return falDefaultQueueBaseURL
	}
	if strings.Contains(baseURL, "://fal.run") {
		return strings.Replace(baseURL, "://fal.run", "://queue.fal.run", 1)
	}
	return baseURL
}

func resolveFalImageModel(model string, hasInputImages bool) string {
	model = firstNonEmpty(model, falDefaultImageModel)
	if !hasInputImages {
		return model
	}
	if strings.HasSuffix(model, "/"+falDefaultImageEditPath) {
		return model
	}
	return model + "/" + falDefaultImageEditPath
}

func resolveFalImageSize(size, aspectRatio, resolution string) any {
	if parsed := parseSizeValue(size); parsed != nil {
		return map[string]int{
			"width":  int(parsed.width),
			"height": int(parsed.height),
		}
	}
	edge := falResolutionEdge(resolution)
	if aspectRatio != "" {
		if edge == 0 {
			edge = 1024
		}
		return falAspectRatioDimensions(aspectRatio, edge)
	}
	if edge > 0 {
		return map[string]int{"width": edge, "height": edge}
	}
	return nil
}

func falResolutionEdge(resolution string) int {
	switch strings.ToUpper(strings.TrimSpace(resolution)) {
	case "1K":
		return 1024
	case "2K":
		return 2048
	case "4K":
		return 4096
	default:
		return 0
	}
}

func falAspectRatioDimensions(aspectRatio string, edge int) map[string]int {
	parsed := parseAspectRatioValue(aspectRatio)
	if parsed == nil || edge <= 0 {
		return nil
	}
	widthRatio := parsed.width
	heightRatio := parsed.height
	if widthRatio >= heightRatio {
		return map[string]int{
			"width":  edge,
			"height": maxInt(int(float64(edge)*heightRatio/widthRatio), 1),
		}
	}
	return map[string]int{
		"width":  maxInt(int(float64(edge)*widthRatio/heightRatio), 1),
		"height": edge,
	}
}

func firstVideoURL(items []falVideoAsset) string {
	for _, item := range items {
		if strings.TrimSpace(item.URL) != "" {
			return strings.TrimSpace(item.URL)
		}
	}
	return ""
}

func parseFalError(status int, body []byte) error {
	var payload falQueueResponse
	if err := json.Unmarshal(body, &payload); err == nil {
		if msg := firstNonEmpty(payload.Detail, valueOrEmpty(payload.Error, func(e *falErrorResponse) string { return e.Message })); msg != "" {
			return fmt.Errorf("mediagen/fal: status %d: %s", status, msg)
		}
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("mediagen/fal: status %d: %s", status, message)
}

func falFileExtension(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "video/webm":
		return ".webm"
	case "video/mp4":
		return ".mp4"
	default:
		return ".png"
	}
}

func withRequestTimeout(ctx context.Context, timeoutMS int, fallback time.Duration) (context.Context, context.CancelFunc) {
	if timeoutMS > 0 {
		return context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	}
	return context.WithTimeout(ctx, fallback)
}

func maxInt(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}
