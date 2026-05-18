package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"
)

// ---------------------------------------------------------------------------
// OpenAI Whisper STT constants
// ---------------------------------------------------------------------------

const (
	openAISTTEndpoint    = "https://api.openai.com/v1/audio/transcriptions"
	defaultWhisperModel  = "whisper-1"
	defaultSTTTimeout    = 120 * time.Second
	authHeaderKey        = "Authorization"
	authBearerPrefix     = "Bearer "
	contentTypeHeaderKey = "Content-Type"
	formFieldFile        = "file"
	formFieldModel       = "model"
	formFieldLanguage    = "language"
	formFieldPrompt      = "prompt"
	formFieldFormat      = "response_format"
	formFieldTemperature = "temperature"
)

// ---------------------------------------------------------------------------
// OpenAI Whisper STT provider
// ---------------------------------------------------------------------------

type openAIProvider struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

func newOpenAIProvider(cfg ProviderConfig) (*openAIProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai stt: api key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = openAISTTEndpoint
	}

	model := cfg.Model
	if model == "" {
		model = defaultWhisperModel
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultSTTTimeout
	}

	return &openAIProvider{
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (p *openAIProvider) Name() string { return providerOpenAI }

// ---------------------------------------------------------------------------
// OpenAI verbose JSON response types
// ---------------------------------------------------------------------------

// openAITranscriptionResponse is the verbose_json response from the Whisper API.
type openAITranscriptionResponse struct {
	Text     string          `json:"text"`
	Language string          `json:"language"`
	Duration float64         `json:"duration"`
	Segments []openAISegment `json:"segments"`
}

// openAISegment is a single segment from the Whisper API verbose response.
type openAISegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// openAIErrorResponse represents an error response from the OpenAI API.
type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ---------------------------------------------------------------------------
// Transcribe
// ---------------------------------------------------------------------------

func (p *openAIProvider) Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Read audio with a size limit to enforce maxAudioSize.
	limitedAudio := io.LimitReader(req.Audio, maxAudioSize+1)
	audioData, err := io.ReadAll(limitedAudio)
	if err != nil {
		return nil, fmt.Errorf("openai stt: reading audio data: %w", err)
	}
	if len(audioData) > maxAudioSize {
		return nil, fmt.Errorf("openai stt: audio exceeds maximum size of %d bytes", maxAudioSize)
	}

	// Build multipart form body.
	body, contentType, err := p.buildMultipartForm(req, audioData)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, body)
	if err != nil {
		return nil, fmt.Errorf("openai stt: creating request: %w", err)
	}
	httpReq.Header.Set(contentTypeHeaderKey, contentType)
	httpReq.Header.Set(authHeaderKey, authBearerPrefix+p.apiKey)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai stt: sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai stt: reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseOpenAIError(resp.StatusCode, respBody)
	}

	return p.parseResponse(req.Format, respBody)
}

// buildMultipartForm constructs the multipart/form-data body for the
// OpenAI transcription API request.
func (p *openAIProvider) buildMultipartForm(req TranscribeRequest, audioData []byte) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Write the audio file part.
	part, err := writer.CreateFormFile(formFieldFile, req.Filename)
	if err != nil {
		return nil, "", fmt.Errorf("openai stt: creating form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, "", fmt.Errorf("openai stt: writing audio data: %w", err)
	}

	// Write model field.
	if err := writer.WriteField(formFieldModel, p.model); err != nil {
		return nil, "", fmt.Errorf("openai stt: writing model field: %w", err)
	}

	// Write optional fields.
	if req.Language != "" {
		if err := writer.WriteField(formFieldLanguage, req.Language); err != nil {
			return nil, "", fmt.Errorf("openai stt: writing language field: %w", err)
		}
	}

	if req.Prompt != "" {
		if err := writer.WriteField(formFieldPrompt, req.Prompt); err != nil {
			return nil, "", fmt.Errorf("openai stt: writing prompt field: %w", err)
		}
	}

	format := req.Format
	if format == "" {
		format = defaultResponseFormat
	}
	if err := writer.WriteField(formFieldFormat, format); err != nil {
		return nil, "", fmt.Errorf("openai stt: writing format field: %w", err)
	}

	if req.Temperature > 0 {
		tempStr := strconv.FormatFloat(req.Temperature, 'f', 2, 64)
		if err := writer.WriteField(formFieldTemperature, tempStr); err != nil {
			return nil, "", fmt.Errorf("openai stt: writing temperature field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("openai stt: closing multipart writer: %w", err)
	}

	return &buf, writer.FormDataContentType(), nil
}

// ---------------------------------------------------------------------------
// Response parsing
// ---------------------------------------------------------------------------

func (p *openAIProvider) parseResponse(format string, body []byte) (*TranscribeResult, error) {
	if format == "" {
		format = defaultResponseFormat
	}

	switch format {
	case "text", "srt", "vtt":
		return &TranscribeResult{
			Text: string(body),
		}, nil

	case "json":
		var simple struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(body, &simple); err != nil {
			return nil, fmt.Errorf("openai stt: parsing json response: %w", err)
		}
		return &TranscribeResult{
			Text: simple.Text,
		}, nil

	case "verbose_json":
		var verbose openAITranscriptionResponse
		if err := json.Unmarshal(body, &verbose); err != nil {
			return nil, fmt.Errorf("openai stt: parsing verbose json response: %w", err)
		}
		return convertVerboseResponse(&verbose), nil

	default:
		return nil, fmt.Errorf("openai stt: unsupported response format %q", format)
	}
}

// convertVerboseResponse transforms an OpenAI verbose JSON response into
// the package-level TranscribeResult.
func convertVerboseResponse(resp *openAITranscriptionResponse) *TranscribeResult {
	segments := make([]Segment, len(resp.Segments))
	for i, s := range resp.Segments {
		segments[i] = Segment{
			Start: time.Duration(s.Start * float64(time.Second)),
			End:   time.Duration(s.End * float64(time.Second)),
			Text:  s.Text,
		}
	}

	return &TranscribeResult{
		Text:     resp.Text,
		Language: resp.Language,
		Duration: time.Duration(resp.Duration * float64(time.Second)),
		Segments: segments,
	}
}

// parseOpenAIError extracts a meaningful error from an OpenAI API error response.
func parseOpenAIError(statusCode int, body []byte) error {
	var apiErr openAIErrorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		return fmt.Errorf("openai stt: api error (status %d): %s", statusCode, apiErr.Error.Message)
	}
	return fmt.Errorf("openai stt: unexpected status %d: %s", statusCode, body)
}
