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
	runwayProviderID          = "runway"
	runwayDefaultBaseURL      = "https://api.dev.runwayml.com"
	runwayDefaultVideoModel   = "gen4.5"
	runwayAPIVersion          = "2024-11-06"
	runwayDefaultTimeout      = 120 * time.Second
	runwayDefaultPollInterval = 2 * time.Second
	runwayMaxPollAttempts     = 120
	runwayMaxDurationSeconds  = 10
)

var (
	runwayTextAspectRatios = []string{"16:9", "9:16"}
	runwayEditAspectRatios = []string{"1:1", "16:9", "9:16", "3:4", "4:3", "21:9"}
	runwayTextModels       = map[string]struct{}{
		"gen4.5":      {},
		"veo3.1":      {},
		"veo3.1_fast": {},
		"veo3":        {},
	}
	runwayImageModels = map[string]struct{}{
		"gen4.5":      {},
		"gen4_turbo":  {},
		"gen3a_turbo": {},
		"veo3.1":      {},
		"veo3.1_fast": {},
		"veo3":        {},
	}
	runwayVideoModels = map[string]struct{}{
		"gen4_aleph": {},
	}
)

func init() {
	RegisterBuiltinProviderBuilder(runwayProviderID, func(entry model.ProviderEntry) (Provider, error) {
		return NewRunwayProvider(RunwayConfig{
			BaseURL: entry.BaseURL,
			APIKey:  entry.APIKey,
			Timeout: entry.Timeout,
			Headers: entry.Headers,
		})
	})
}

type RunwayConfig struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	Headers map[string]string
}

type RunwayProvider struct {
	config RunwayConfig
	client *http.Client
}

func NewRunwayProvider(cfg RunwayConfig) (*RunwayProvider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("mediagen/runway: api key is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = runwayDefaultBaseURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = runwayDefaultTimeout
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	return &RunwayProvider{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (p *RunwayProvider) ID() string    { return runwayProviderID }
func (p *RunwayProvider) Label() string { return "Runway" }

func (p *RunwayProvider) DefaultVideoModel() string { return runwayDefaultVideoModel }
func (p *RunwayProvider) VideoModels() []string {
	return []string{"gen4.5", "gen4_turbo", "gen4_aleph", "gen3a_turbo", "veo3.1", "veo3.1_fast", "veo3"}
}

func (p *RunwayProvider) VideoCapabilities() VideoCapabilities {
	return VideoCapabilities{
		MaxVideos:            1,
		MaxInputImages:       1,
		MaxInputVideos:       1,
		MaxDurationSeconds:   runwayMaxDurationSeconds,
		SupportsImageToVideo: true,
		SupportsVideoToVideo: true,
		SupportsAspectRatio:  true,
		AspectRatios:         cloneStrings(runwayEditAspectRatios),
	}
}

func (p *RunwayProvider) GenerateVideo(ctx context.Context, req VideoRequest) (*VideoResult, error) {
	endpoint, err := runwayEndpoint(req)
	if err != nil {
		return nil, err
	}
	model := firstNonEmpty(req.Model, runwayDefaultVideoModel)
	if err := validateRunwayModel(endpoint, model); err != nil {
		return nil, err
	}
	body, err := runwayCreateBody(req, model, endpoint)
	if err != nil {
		return nil, err
	}
	timeout := runwayTimeout(req.TimeoutMS)
	createCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var created runwayTaskCreateResponse
	if err := p.doJSON(createCtx, http.MethodPost, p.config.BaseURL+endpoint, body, &created); err != nil {
		return nil, err
	}
	taskID := strings.TrimSpace(created.ID)
	if taskID == "" {
		return nil, fmt.Errorf("mediagen/runway: video generation response missing task id")
	}

	completed, err := p.pollTask(ctx, taskID, timeout)
	if err != nil {
		return nil, err
	}
	if len(completed.Output) == 0 {
		return nil, fmt.Errorf("mediagen/runway: completed task missing output URLs")
	}
	videos := make([]GeneratedAsset, 0, len(completed.Output))
	for index, outputURL := range completed.Output {
		outputURL = strings.TrimSpace(outputURL)
		if outputURL == "" {
			continue
		}
		asset, err := p.downloadVideo(ctx, outputURL, index)
		if err != nil {
			return nil, err
		}
		videos = append(videos, asset)
	}
	if len(videos) == 0 {
		return nil, fmt.Errorf("mediagen/runway: completed task missing downloadable videos")
	}
	return &VideoResult{
		Videos: videos,
		Model:  model,
		Metadata: map[string]any{
			"task_id":  taskID,
			"status":   strings.TrimSpace(completed.Status),
			"endpoint": endpoint,
			"outputs":  append([]string(nil), completed.Output...),
		},
	}, nil
}

type runwayTaskCreateResponse struct {
	ID string `json:"id"`
}

type runwayTaskDetailResponse struct {
	ID      string            `json:"id"`
	Status  string            `json:"status"`
	Output  []string          `json:"output"`
	Failure any               `json:"failure"`
	Detail  *runwayFailureMsg `json:"detail"`
}

type runwayFailureMsg struct {
	Message string `json:"message"`
}

func (p *RunwayProvider) doJSON(ctx context.Context, method, url string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mediagen/runway: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mediagen/runway: create request: %w", err)
	}
	p.applyHeaders(req)
	return p.do(req, out)
}

func (p *RunwayProvider) do(req *http.Request, out any) error {
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("mediagen/runway: send request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("mediagen/runway: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return parseRunwayError(resp.StatusCode, body)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("mediagen/runway: decode response: %w", err)
	}
	return nil
}

func (p *RunwayProvider) pollTask(ctx context.Context, taskID string, timeout time.Duration) (*runwayTaskDetailResponse, error) {
	ticker := time.NewTicker(runwayDefaultPollInterval)
	defer ticker.Stop()
	deadline := time.Now().Add(timeout)
	for attempts := 0; attempts < runwayMaxPollAttempts; attempts++ {
		var detail runwayTaskDetailResponse
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.BaseURL+"/v1/tasks/"+taskID, nil)
		if err != nil {
			return nil, fmt.Errorf("mediagen/runway: create poll request: %w", err)
		}
		p.applyHeaders(req)
		if err := p.do(req, &detail); err != nil {
			return nil, err
		}
		switch strings.ToUpper(strings.TrimSpace(detail.Status)) {
		case "SUCCEEDED":
			return &detail, nil
		case "FAILED", "CANCELLED":
			return nil, fmt.Errorf("mediagen/runway: %s", runwayFailureMessage(detail))
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("mediagen/runway: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	return nil, fmt.Errorf("mediagen/runway: video generation timed out")
}

func (p *RunwayProvider) downloadVideo(ctx context.Context, url string, index int) (GeneratedAsset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/runway: create video download request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/runway: download generated video: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/runway: read generated video: %w", err)
	}
	if resp.StatusCode >= 400 {
		return GeneratedAsset{}, parseRunwayError(resp.StatusCode, body)
	}
	mimeType := firstNonEmpty(resp.Header.Get("Content-Type"), "video/mp4")
	return GeneratedAsset{
		Buffer:   body,
		MIMEType: mimeType,
		FileName: fmt.Sprintf("video-%d%s", index+1, falFileExtension(mimeType)),
	}, nil
}

func (p *RunwayProvider) applyHeaders(req *http.Request) {
	for key, value := range p.config.Headers {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Runway-Version", runwayAPIVersion)
}

func runwayEndpoint(req VideoRequest) (string, error) {
	imageCount := len(req.InputImages)
	videoCount := len(req.InputVideos)
	switch {
	case imageCount > 0 && videoCount > 0:
		return "", fmt.Errorf("mediagen/runway: image and video inputs cannot be mixed")
	case imageCount > 1 || videoCount > 1:
		return "", fmt.Errorf("mediagen/runway: supports at most one image or one video input")
	case videoCount > 0:
		return "/v1/video_to_video", nil
	case imageCount > 0:
		return "/v1/image_to_video", nil
	default:
		return "/v1/text_to_video", nil
	}
}

func validateRunwayModel(endpoint, model string) error {
	switch endpoint {
	case "/v1/text_to_video":
		if _, ok := runwayTextModels[model]; !ok {
			return fmt.Errorf("mediagen/runway: text-to-video does not support model %s", model)
		}
	case "/v1/image_to_video":
		if _, ok := runwayImageModels[model]; !ok {
			return fmt.Errorf("mediagen/runway: image-to-video does not support model %s", model)
		}
	case "/v1/video_to_video":
		if _, ok := runwayVideoModels[model]; !ok {
			return fmt.Errorf("mediagen/runway: video-to-video currently requires model gen4_aleph")
		}
	}
	return nil
}

func runwayCreateBody(req VideoRequest, model, endpoint string) (map[string]any, error) {
	body := map[string]any{
		"model":      model,
		"promptText": strings.TrimSpace(req.Prompt),
	}
	if endpoint != "/v1/video_to_video" {
		body["duration"] = runwayDuration(req.DurationSeconds)
	}
	if ratio, err := runwayRatio(req, endpoint); err != nil {
		return nil, err
	} else if ratio != "" {
		body["ratio"] = ratio
	}
	switch endpoint {
	case "/v1/image_to_video":
		body["promptImage"] = runwaySourceURI(req.InputImages[0], "image/png")
	case "/v1/video_to_video":
		body["videoUri"] = runwaySourceURI(req.InputVideos[0], "video/mp4")
	}
	return body, nil
}

func runwayRatio(req VideoRequest, endpoint string) (string, error) {
	if size := strings.TrimSpace(req.Size); size != "" {
		if endpoint == "/v1/text_to_video" && size != "1280:720" && size != "720:1280" {
			return "", fmt.Errorf("mediagen/runway: text-to-video supports only 16:9 or 9:16 ratios")
		}
		return size, nil
	}
	ratio := strings.TrimSpace(req.AspectRatio)
	switch ratio {
	case "":
		return "1280:720", nil
	case "16:9":
		return "1280:720", nil
	case "9:16":
		return "720:1280", nil
	case "1:1":
		return "960:960", nil
	case "3:4":
		return "832:1104", nil
	case "4:3":
		return "1104:832", nil
	case "21:9":
		return "1584:672", nil
	default:
		return "", fmt.Errorf("mediagen/runway: unsupported aspect ratio %s", ratio)
	}
}

func runwayDuration(duration int) int {
	if duration <= 0 {
		return 5
	}
	if duration < 2 {
		return 2
	}
	if duration > runwayMaxDurationSeconds {
		return runwayMaxDurationSeconds
	}
	return duration
}

func runwaySourceURI(asset InputAsset, fallbackMIME string) string {
	return dataURL(asset, fallbackMIME)
}

func runwayTimeout(timeoutMS int) time.Duration {
	if timeoutMS > 0 {
		return time.Duration(timeoutMS) * time.Millisecond
	}
	return runwayDefaultTimeout
}

func parseRunwayError(status int, body []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		for _, key := range []string{"message", "detail", "error"} {
			if msg := strings.TrimSpace(fmt.Sprint(payload[key])); msg != "" && msg != "<nil>" {
				return fmt.Errorf("mediagen/runway: status %d: %s", status, msg)
			}
		}
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("mediagen/runway: status %d: %s", status, message)
}

func runwayFailureMessage(detail runwayTaskDetailResponse) string {
	switch failure := detail.Failure.(type) {
	case string:
		if strings.TrimSpace(failure) != "" {
			return strings.TrimSpace(failure)
		}
	case map[string]any:
		if msg := strings.TrimSpace(fmt.Sprint(failure["message"])); msg != "" && msg != "<nil>" {
			return msg
		}
	}
	if detail.Detail != nil && strings.TrimSpace(detail.Detail.Message) != "" {
		return strings.TrimSpace(detail.Detail.Message)
	}
	return "request failed"
}
