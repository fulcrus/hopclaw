package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// NewProvider tests
// ---------------------------------------------------------------------------

func TestNewProviderOpenAI(t *testing.T) {
	t.Parallel()

	p, err := NewProvider("openai", ProviderConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "openai" {
		t.Fatalf("expected provider name %q, got %q", "openai", p.Name())
	}
}

func TestNewProviderOpenAIMissingAPIKey(t *testing.T) {
	t.Parallel()

	_, err := NewProvider("openai", ProviderConfig{})
	if err == nil {
		t.Fatal("expected error for missing openai api key")
	}
	if !strings.Contains(err.Error(), "api key is required") {
		t.Fatalf("expected api key error, got: %v", err)
	}
}

func TestNewProviderLocal(t *testing.T) {
	t.Parallel()

	_, err := NewProvider("local", ProviderConfig{})
	// This will fail if whisper is not installed — that is expected.
	if err != nil && !errors.Is(err, ErrLocalWhisperNotAvailable) {
		t.Fatalf("expected ErrLocalWhisperNotAvailable or nil, got: %v", err)
	}
}

func TestNewProviderUnknown(t *testing.T) {
	t.Parallel()

	_, err := NewProvider("nonexistent", ProviderConfig{})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown stt provider") {
		t.Fatalf("expected unknown provider error, got: %v", err)
	}
}

func TestNewProviderCaseInsensitive(t *testing.T) {
	t.Parallel()

	p, err := NewProvider("OpenAI", ProviderConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "openai" {
		t.Fatalf("expected provider name %q, got %q", "openai", p.Name())
	}
}

// ---------------------------------------------------------------------------
// TranscribeRequest validation tests
// ---------------------------------------------------------------------------

func TestValidateRequestMissingAudio(t *testing.T) {
	t.Parallel()

	req := TranscribeRequest{
		Filename: "audio.wav",
	}
	err := req.Validate()
	if err == nil {
		t.Fatal("expected error for missing audio reader")
	}
	if !strings.Contains(err.Error(), "audio reader is required") {
		t.Fatalf("expected audio reader error, got: %v", err)
	}
}

func TestValidateRequestMissingFilename(t *testing.T) {
	t.Parallel()

	req := TranscribeRequest{
		Audio: strings.NewReader("data"),
	}
	err := req.Validate()
	if err == nil {
		t.Fatal("expected error for missing filename")
	}
	if !strings.Contains(err.Error(), "filename is required") {
		t.Fatalf("expected filename error, got: %v", err)
	}
}

func TestValidateRequestUnsupportedExtension(t *testing.T) {
	t.Parallel()

	req := TranscribeRequest{
		Audio:    strings.NewReader("data"),
		Filename: "audio.txt",
	}
	err := req.Validate()
	if err == nil {
		t.Fatal("expected error for unsupported extension")
	}
	if !strings.Contains(err.Error(), "unsupported file extension") {
		t.Fatalf("expected extension error, got: %v", err)
	}
}

func TestValidateRequestInvalidTemperature(t *testing.T) {
	t.Parallel()

	req := TranscribeRequest{
		Audio:       strings.NewReader("data"),
		Filename:    "audio.mp3",
		Temperature: 1.5,
	}
	err := req.Validate()
	if err == nil {
		t.Fatal("expected error for invalid temperature")
	}
	if !strings.Contains(err.Error(), "temperature must be between") {
		t.Fatalf("expected temperature error, got: %v", err)
	}
}

func TestValidateRequestInvalidFormat(t *testing.T) {
	t.Parallel()

	req := TranscribeRequest{
		Audio:    strings.NewReader("data"),
		Filename: "audio.mp3",
		Format:   "xml",
	}
	err := req.Validate()
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "unsupported response format") {
		t.Fatalf("expected format error, got: %v", err)
	}
}

func TestValidateRequestValidFormats(t *testing.T) {
	t.Parallel()

	validFormats := []string{"json", "text", "srt", "vtt", "verbose_json", ""}
	for _, format := range validFormats {
		req := TranscribeRequest{
			Audio:    strings.NewReader("data"),
			Filename: "audio.wav",
			Format:   format,
		}
		if err := req.Validate(); err != nil {
			t.Fatalf("unexpected error for format %q: %v", format, err)
		}
	}
}

func TestValidateRequestValidExtensions(t *testing.T) {
	t.Parallel()

	validExtensions := []string{
		"audio.flac", "audio.mp3", "audio.mp4", "audio.mpeg",
		"audio.mpga", "audio.m4a", "audio.ogg", "audio.wav", "audio.webm",
	}
	for _, filename := range validExtensions {
		req := TranscribeRequest{
			Audio:    strings.NewReader("data"),
			Filename: filename,
		}
		if err := req.Validate(); err != nil {
			t.Fatalf("unexpected error for filename %q: %v", filename, err)
		}
	}
}

// ---------------------------------------------------------------------------
// OpenAI provider tests (httptest)
// ---------------------------------------------------------------------------

func TestOpenAITranscribeVerboseJSON(t *testing.T) {
	t.Parallel()

	verboseResp := openAITranscriptionResponse{
		Text:     "Hello world",
		Language: "en",
		Duration: 2.5,
		Segments: []openAISegment{
			{Start: 0.0, End: 1.2, Text: "Hello"},
			{Start: 1.2, End: 2.5, Text: " world"},
		},
	}
	respBody, _ := json.Marshal(verboseResp)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and auth header.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		authHeader := r.Header.Get(authHeaderKey)
		if authHeader != authBearerPrefix+"test-key" {
			t.Errorf("expected auth header %q, got %q", authBearerPrefix+"test-key", authHeader)
		}

		// Verify content type is multipart.
		contentType := r.Header.Get(contentTypeHeaderKey)
		if !strings.HasPrefix(contentType, "multipart/form-data") {
			t.Errorf("expected multipart/form-data content type, got %q", contentType)
		}

		// Parse multipart form.
		if err := r.ParseMultipartForm(maxAudioSize); err != nil {
			t.Errorf("failed to parse multipart form: %v", err)
		}

		// Verify required fields.
		if r.FormValue(formFieldModel) != defaultWhisperModel {
			t.Errorf("expected model %q, got %q", defaultWhisperModel, r.FormValue(formFieldModel))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBody)
	}))
	defer server.Close()

	p, err := newOpenAIProvider(ProviderConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	result, err := p.Transcribe(context.Background(), TranscribeRequest{
		Audio:    strings.NewReader("fake-audio-data"),
		Filename: "test.wav",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "Hello world" {
		t.Fatalf("expected text %q, got %q", "Hello world", result.Text)
	}
	if result.Language != "en" {
		t.Fatalf("expected language %q, got %q", "en", result.Language)
	}
	expectedDuration := time.Duration(2.5 * float64(time.Second))
	if result.Duration != expectedDuration {
		t.Fatalf("expected duration %v, got %v", expectedDuration, result.Duration)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}
	if result.Segments[0].Text != "Hello" {
		t.Fatalf("expected first segment text %q, got %q", "Hello", result.Segments[0].Text)
	}
}

func TestOpenAITranscribePlainText(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello world in plain text"))
	}))
	defer server.Close()

	p, err := newOpenAIProvider(ProviderConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	result, err := p.Transcribe(context.Background(), TranscribeRequest{
		Audio:    strings.NewReader("fake-audio-data"),
		Filename: "test.mp3",
		Format:   "text",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "Hello world in plain text" {
		t.Fatalf("expected text %q, got %q", "Hello world in plain text", result.Text)
	}
}

func TestOpenAITranscribeSimpleJSON(t *testing.T) {
	t.Parallel()

	simpleResp := struct {
		Text string `json:"text"`
	}{Text: "simple json text"}
	respBody, _ := json.Marshal(simpleResp)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBody)
	}))
	defer server.Close()

	p, err := newOpenAIProvider(ProviderConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	result, err := p.Transcribe(context.Background(), TranscribeRequest{
		Audio:    strings.NewReader("fake-audio-data"),
		Filename: "test.wav",
		Format:   "json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "simple json text" {
		t.Fatalf("expected text %q, got %q", "simple json text", result.Text)
	}
}

func TestOpenAITranscribeAPIError(t *testing.T) {
	t.Parallel()

	apiErr := openAIErrorResponse{}
	apiErr.Error.Message = "invalid api key"
	apiErr.Error.Type = "invalid_request_error"
	respBody, _ := json.Marshal(apiErr)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write(respBody)
	}))
	defer server.Close()

	p, err := newOpenAIProvider(ProviderConfig{
		APIKey:  "bad-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	_, err = p.Transcribe(context.Background(), TranscribeRequest{
		Audio:    strings.NewReader("fake-audio-data"),
		Filename: "test.wav",
	})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("expected error containing 'invalid api key', got: %v", err)
	}
}

func TestOpenAITranscribeOptionalFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(maxAudioSize); err != nil {
			t.Errorf("failed to parse multipart form: %v", err)
		}

		// Verify optional fields were sent.
		if r.FormValue(formFieldLanguage) != "en" {
			t.Errorf("expected language %q, got %q", "en", r.FormValue(formFieldLanguage))
		}
		if r.FormValue(formFieldPrompt) != "context hint" {
			t.Errorf("expected prompt %q, got %q", "context hint", r.FormValue(formFieldPrompt))
		}
		if r.FormValue(formFieldFormat) != "text" {
			t.Errorf("expected format %q, got %q", "text", r.FormValue(formFieldFormat))
		}
		if r.FormValue(formFieldTemperature) != "0.50" {
			t.Errorf("expected temperature %q, got %q", "0.50", r.FormValue(formFieldTemperature))
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("transcribed text"))
	}))
	defer server.Close()

	p, err := newOpenAIProvider(ProviderConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	_, err = p.Transcribe(context.Background(), TranscribeRequest{
		Audio:       strings.NewReader("fake-audio-data"),
		Filename:    "test.mp3",
		Language:    "en",
		Prompt:      "context hint",
		Format:      "text",
		Temperature: 0.5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenAITranscribeValidationError(t *testing.T) {
	t.Parallel()

	p, err := newOpenAIProvider(ProviderConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	// Missing audio reader.
	_, err = p.Transcribe(context.Background(), TranscribeRequest{
		Filename: "test.wav",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestOpenAITranscribeContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow server.
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p, err := newOpenAIProvider(ProviderConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = p.Transcribe(ctx, TranscribeRequest{
		Audio:    strings.NewReader("fake-audio-data"),
		Filename: "test.wav",
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Local provider tests
// ---------------------------------------------------------------------------

func TestLocalProviderWhisperNotFound(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv, so this test runs sequentially.
	t.Setenv("PATH", t.TempDir())

	_, err := newLocalProvider(ProviderConfig{})
	if !errors.Is(err, ErrLocalWhisperNotAvailable) {
		t.Fatalf("expected ErrLocalWhisperNotAvailable, got: %v", err)
	}
}

func TestLocalParseOutput(t *testing.T) {
	t.Parallel()

	whisperJSON := localWhisperOutput{
		Text:     "Hello from local whisper",
		Language: "en",
		Segments: []localWhisperSegment{
			{Start: 0.0, End: 1.5, Text: "Hello"},
			{Start: 1.5, End: 3.0, Text: " from local whisper"},
		},
	}
	data, _ := json.Marshal(whisperJSON)

	result, err := parseLocalOutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "Hello from local whisper" {
		t.Fatalf("expected text %q, got %q", "Hello from local whisper", result.Text)
	}
	if result.Language != "en" {
		t.Fatalf("expected language %q, got %q", "en", result.Language)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}

	expectedEnd := time.Duration(1.5 * float64(time.Second))
	if result.Segments[0].End != expectedEnd {
		t.Fatalf("expected segment end %v, got %v", expectedEnd, result.Segments[0].End)
	}
}

func TestLocalParseOutputInvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parseLocalOutput([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parsing whisper output") {
		t.Fatalf("expected parsing error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// OpenAI provider configuration tests
// ---------------------------------------------------------------------------

func TestOpenAIProviderDefaults(t *testing.T) {
	t.Parallel()

	p, err := newOpenAIProvider(ProviderConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.baseURL != openAISTTEndpoint {
		t.Fatalf("expected base URL %q, got %q", openAISTTEndpoint, p.baseURL)
	}
	if p.model != defaultWhisperModel {
		t.Fatalf("expected model %q, got %q", defaultWhisperModel, p.model)
	}
	if p.httpClient.Timeout != defaultSTTTimeout {
		t.Fatalf("expected timeout %v, got %v", defaultSTTTimeout, p.httpClient.Timeout)
	}
}

func TestOpenAIProviderCustomConfig(t *testing.T) {
	t.Parallel()

	customTimeout := 60 * time.Second
	p, err := newOpenAIProvider(ProviderConfig{
		APIKey:  "test-key",
		BaseURL: "https://custom.api.example.com/v1/audio/transcriptions",
		Model:   "whisper-large-v3",
		Timeout: customTimeout,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.baseURL != "https://custom.api.example.com/v1/audio/transcriptions" {
		t.Fatalf("expected custom base URL, got %q", p.baseURL)
	}
	if p.model != "whisper-large-v3" {
		t.Fatalf("expected custom model, got %q", p.model)
	}
	if p.httpClient.Timeout != customTimeout {
		t.Fatalf("expected custom timeout, got %v", p.httpClient.Timeout)
	}
}

// ---------------------------------------------------------------------------
// Audio size limit test
// ---------------------------------------------------------------------------

func TestOpenAITranscribeAudioSizeLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This handler should not be reached if the limit works.
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("should not reach"))
	}))
	defer server.Close()

	p, err := newOpenAIProvider(ProviderConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	// Create audio data larger than maxAudioSize.
	oversizedData := make([]byte, maxAudioSize+1024)
	_, err = p.Transcribe(context.Background(), TranscribeRequest{
		Audio:    bytes.NewReader(oversizedData),
		Filename: "huge.wav",
	})
	if err == nil {
		t.Fatal("expected error for oversized audio")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected size limit error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Response parsing edge cases
// ---------------------------------------------------------------------------

func TestOpenAIParseResponseSRTFormat(t *testing.T) {
	t.Parallel()

	p := &openAIProvider{}
	srtContent := "1\n00:00:00,000 --> 00:00:02,000\nHello world\n"

	result, err := p.parseResponse("srt", []byte(srtContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != srtContent {
		t.Fatalf("expected raw SRT content, got %q", result.Text)
	}
}

func TestOpenAIParseResponseVTTFormat(t *testing.T) {
	t.Parallel()

	p := &openAIProvider{}
	vttContent := "WEBVTT\n\n00:00:00.000 --> 00:00:02.000\nHello world\n"

	result, err := p.parseResponse("vtt", []byte(vttContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != vttContent {
		t.Fatalf("expected raw VTT content, got %q", result.Text)
	}
}

func TestOpenAIParseResponseInvalidJSON(t *testing.T) {
	t.Parallel()

	p := &openAIProvider{}
	_, err := p.parseResponse("json", []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestOpenAIParseResponseUnsupportedFormat(t *testing.T) {
	t.Parallel()

	p := &openAIProvider{}
	_, err := p.parseResponse("xml", []byte("<xml/>"))
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

// ---------------------------------------------------------------------------
// Error parsing test
// ---------------------------------------------------------------------------

func TestParseOpenAIErrorStructured(t *testing.T) {
	t.Parallel()

	apiErr := openAIErrorResponse{}
	apiErr.Error.Message = "rate limit exceeded"
	apiErr.Error.Type = "rate_limit_error"
	body, _ := json.Marshal(apiErr)

	err := parseOpenAIError(http.StatusTooManyRequests, body)
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Fatalf("expected structured error message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected status code in error, got: %v", err)
	}
}

func TestParseOpenAIErrorUnstructured(t *testing.T) {
	t.Parallel()

	err := parseOpenAIError(http.StatusInternalServerError, []byte("internal server error"))
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected status code in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "internal server error") {
		t.Fatalf("expected raw body in error, got: %v", err)
	}
}

// Ensure unused imports do not cause compile errors.
var _ = io.Discard
