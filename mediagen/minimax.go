package mediagen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/model"
)

const (
	minimaxProviderID        = "minimax"
	minimaxDefaultBaseURL    = "https://api.minimax.io"
	minimaxDefaultMusicModel = "music-2.5+"
	minimaxDefaultTimeout    = 120 * time.Second
)

func init() {
	RegisterBuiltinProviderBuilder(minimaxProviderID, func(entry model.ProviderEntry) (Provider, error) {
		return NewMinimaxMusicProvider(MinimaxConfig{
			BaseURL: entry.BaseURL,
			APIKey:  entry.APIKey,
			Timeout: entry.Timeout,
			Headers: entry.Headers,
		})
	})
}

type MinimaxConfig struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	Headers map[string]string
}

type MinimaxMusicProvider struct {
	config MinimaxConfig
	client *http.Client
}

func NewMinimaxMusicProvider(cfg MinimaxConfig) (*MinimaxMusicProvider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("mediagen/minimax: api key is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = minimaxDefaultBaseURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = minimaxDefaultTimeout
	}
	cfg.BaseURL = resolveMinimaxOrigin(cfg.BaseURL)
	return &MinimaxMusicProvider{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (p *MinimaxMusicProvider) ID() string    { return minimaxProviderID }
func (p *MinimaxMusicProvider) Label() string { return "MiniMax" }

func (p *MinimaxMusicProvider) DefaultMusicModel() string { return minimaxDefaultMusicModel }
func (p *MinimaxMusicProvider) MusicModels() []string {
	return []string{minimaxDefaultMusicModel, "music-2.5", "music-2.0"}
}

func (p *MinimaxMusicProvider) MusicCapabilities() MusicCapabilities {
	return MusicCapabilities{
		MaxTracks:            1,
		SupportsLyrics:       true,
		SupportsInstrumental: true,
		SupportsDuration:     true,
		SupportsFormat:       true,
		Formats:              []string{"mp3"},
	}
}

func (p *MinimaxMusicProvider) GenerateMusic(ctx context.Context, req MusicRequest) (*MusicResult, error) {
	if len(req.InputImages) > 0 {
		return nil, fmt.Errorf("mediagen/minimax: image references are not supported")
	}
	if req.Instrumental && strings.TrimSpace(req.Lyrics) != "" {
		return nil, fmt.Errorf("mediagen/minimax: lyrics cannot be combined with instrumental mode")
	}
	if format := strings.TrimSpace(req.Format); format != "" && !strings.EqualFold(format, "mp3") {
		return nil, fmt.Errorf("mediagen/minimax: only mp3 output is currently supported")
	}

	model := firstNonEmpty(req.Model, minimaxDefaultMusicModel)
	payload := map[string]any{
		"model":         model,
		"prompt":        buildMinimaxMusicPrompt(req.Prompt, req.DurationSeconds),
		"output_format": "url",
		"audio_setting": map[string]any{
			"sample_rate": 44100,
			"bitrate":     256000,
			"format":      "mp3",
		},
	}
	if req.Instrumental {
		payload["is_instrumental"] = true
	} else if lyrics := strings.TrimSpace(req.Lyrics); lyrics != "" {
		payload["lyrics"] = lyrics
	} else {
		payload["lyrics_optimizer"] = true
	}

	var resp minimaxMusicResponse
	if err := p.doJSON(ctx, "/v1/music_generation", payload, &resp); err != nil {
		return nil, err
	}
	if resp.BaseResp != nil && resp.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("mediagen/minimax: %s (%d)", firstNonEmpty(resp.BaseResp.StatusMsg, "request failed"), resp.BaseResp.StatusCode)
	}

	audioURL := firstNonEmpty(resp.AudioURL, valueOrEmpty(resp.Data, func(d *minimaxMusicData) string { return d.AudioURL }))
	audioValue := firstNonEmpty(resp.Audio, valueOrEmpty(resp.Data, func(d *minimaxMusicData) string { return d.Audio }))
	var track GeneratedAsset
	switch {
	case isRemoteURL(audioURL):
		downloaded, err := p.downloadAudio(ctx, audioURL)
		if err != nil {
			return nil, err
		}
		track = downloaded
	case strings.TrimSpace(audioValue) != "":
		decoded, err := decodePossibleBinary(audioValue)
		if err != nil {
			return nil, err
		}
		track = GeneratedAsset{
			Buffer:   decoded,
			MIMEType: "audio/mpeg",
			FileName: "track-1.mp3",
		}
	default:
		return nil, fmt.Errorf("mediagen/minimax: response missing audio output")
	}

	lyrics := decodePossibleText(firstNonEmpty(resp.Lyrics, valueOrEmpty(resp.Data, func(d *minimaxMusicData) string { return d.Lyrics })))
	metadata := map[string]any{
		"instrumental": req.Instrumental,
	}
	if taskID := strings.TrimSpace(resp.TaskID); taskID != "" {
		metadata["task_id"] = taskID
	}
	if audioURL != "" {
		metadata["audio_url"] = audioURL
	}
	if req.DurationSeconds > 0 {
		metadata["requested_duration_seconds"] = req.DurationSeconds
	}

	out := &MusicResult{
		Tracks:   []GeneratedAsset{track},
		Model:    model,
		Metadata: metadata,
	}
	if lyrics != "" {
		out.Lyrics = []string{lyrics}
	}
	return out, nil
}

type minimaxMusicResponse struct {
	TaskID   string            `json:"task_id"`
	Audio    string            `json:"audio"`
	AudioURL string            `json:"audio_url"`
	Lyrics   string            `json:"lyrics"`
	Data     *minimaxMusicData `json:"data"`
	BaseResp *minimaxBaseResp  `json:"base_resp"`
}

type minimaxMusicData struct {
	Audio    string `json:"audio"`
	AudioURL string `json:"audio_url"`
	Lyrics   string `json:"lyrics"`
}

type minimaxBaseResp struct {
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

type minimaxErrorResponse struct {
	BaseResp *minimaxBaseResp `json:"base_resp"`
}

func (p *MinimaxMusicProvider) doJSON(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mediagen/minimax: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mediagen/minimax: create request: %w", err)
	}
	for key, value := range p.config.Headers {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("mediagen/minimax: send request: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("mediagen/minimax: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return parseMinimaxError(resp.StatusCode, responseBody)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("mediagen/minimax: decode response: %w", err)
	}
	return nil
}

func (p *MinimaxMusicProvider) downloadAudio(ctx context.Context, rawURL string) (GeneratedAsset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/minimax: create audio download request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/minimax: download audio: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return GeneratedAsset{}, fmt.Errorf("mediagen/minimax: read downloaded audio: %w", err)
	}
	if resp.StatusCode >= 400 {
		return GeneratedAsset{}, fmt.Errorf("mediagen/minimax: audio download returned status %d", resp.StatusCode)
	}
	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = "audio/mpeg"
	}
	return GeneratedAsset{
		Buffer:   body,
		MIMEType: mimeType,
		FileName: "track-1.mp3",
	}, nil
}

func parseMinimaxError(status int, body []byte) error {
	var payload minimaxErrorResponse
	if err := json.Unmarshal(body, &payload); err == nil && payload.BaseResp != nil && strings.TrimSpace(payload.BaseResp.StatusMsg) != "" {
		return fmt.Errorf("mediagen/minimax: %s (%d)", strings.TrimSpace(payload.BaseResp.StatusMsg), payload.BaseResp.StatusCode)
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("mediagen/minimax: status %d: %s", status, message)
}

func resolveMinimaxOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return minimaxDefaultBaseURL
	}
	return parsed.Scheme + "://" + parsed.Host
}

func buildMinimaxMusicPrompt(prompt string, durationSeconds int) string {
	prompt = strings.TrimSpace(prompt)
	if durationSeconds <= 0 {
		return prompt
	}
	if prompt == "" {
		return fmt.Sprintf("Target duration: about %d seconds.", durationSeconds)
	}
	return prompt + "\n\nTarget duration: about " + fmt.Sprintf("%d", durationSeconds) + " seconds."
}

func decodePossibleBinary(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("mediagen/minimax: empty binary payload")
	}
	if decoded, err := hex.DecodeString(trimmed); err == nil {
		return decoded, nil
	}
	decoded, err := io.ReadAll(base64Decoder(trimmed))
	if err != nil {
		return nil, fmt.Errorf("mediagen/minimax: decode audio payload: %w", err)
	}
	return decoded, nil
}

func decodePossibleText(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if decoded, err := hex.DecodeString(trimmed); err == nil {
		return strings.TrimSpace(string(decoded))
	}
	return trimmed
}

func isRemoteURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func valueOrEmpty[T any](value *T, fn func(*T) string) string {
	if value == nil || fn == nil {
		return ""
	}
	return strings.TrimSpace(fn(value))
}

func base64Decoder(raw string) io.Reader {
	return base64.NewDecoder(base64.StdEncoding, strings.NewReader(raw))
}
