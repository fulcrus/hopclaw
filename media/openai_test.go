package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestOpenAIDescribeImage
// ---------------------------------------------------------------------------

func TestOpenAIDescribeImage(t *testing.T) {
	t.Parallel()

	chatResp := openaiChatResponse{
		Model: "gpt-4o-2024-08-06",
		Choices: []openaiChoice{
			{Message: struct {
				Content string `json:"content"`
			}{Content: "A fluffy orange cat sitting on a blue mat."}},
		},
	}
	respBody, _ := json.Marshal(chatResp)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method and auth.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		auth := r.Header.Get(openaiAuthHeader)
		if auth != openaiAuthPrefix+"test-key" {
			t.Errorf("expected auth %q, got %q", openaiAuthPrefix+"test-key", auth)
		}

		// Verify content type.
		ct := r.Header.Get(openaiContentType)
		if ct != "application/json" {
			t.Errorf("expected application/json, got %q", ct)
		}

		// Verify endpoint path.
		if r.URL.Path != openaiChatEndpoint {
			t.Errorf("expected path %q, got %q", openaiChatEndpoint, r.URL.Path)
		}

		// Decode and verify request body.
		var req openaiChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if req.Model != openaiDefaultVisionModel {
			t.Errorf("expected model %q, got %q", openaiDefaultVisionModel, req.Model)
		}
		if len(req.Messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(req.Messages))
		}
		if len(req.Messages[0].Content) != 2 {
			t.Errorf("expected 2 content parts, got %d", len(req.Messages[0].Content))
		}

		// Verify the text part contains the default prompt.
		textPart := req.Messages[0].Content[0]
		if textPart.Type != openaiPartTypeText {
			t.Errorf("expected text part type, got %q", textPart.Type)
		}
		if textPart.Text != openaiDefaultImagePrompt {
			t.Errorf("expected default prompt, got %q", textPart.Text)
		}

		// Verify the image part has a data URL.
		imgPart := req.Messages[0].Content[1]
		if imgPart.Type != openaiPartTypeImageURL {
			t.Errorf("expected image_url part type, got %q", imgPart.Type)
		}
		if imgPart.ImageURL == nil || !strings.HasPrefix(imgPart.ImageURL.URL, "data:image/jpeg;base64,") {
			t.Error("expected data URL with base64 image")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBody)
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	result, err := provider.DescribeImage(context.Background(), ImageRequest{
		Data:     []byte{0xFF, 0xD8, 0xFF, 0xE0},
		MIMEType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "A fluffy orange cat sitting on a blue mat." {
		t.Errorf("unexpected text: %q", result.Text)
	}
	if result.Model != "gpt-4o-2024-08-06" {
		t.Errorf("unexpected model: %q", result.Model)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIDescribeImageCustomPrompt
// ---------------------------------------------------------------------------

func TestOpenAIDescribeImageCustomPrompt(t *testing.T) {
	t.Parallel()

	chatResp := openaiChatResponse{
		Model: "gpt-4o",
		Choices: []openaiChoice{{Message: struct {
			Content string `json:"content"`
		}{Content: "3 people"}}},
	}
	respBody, _ := json.Marshal(chatResp)

	var receivedPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openaiChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) > 0 && len(req.Messages[0].Content) > 0 {
			receivedPrompt = req.Messages[0].Content[0].Text
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBody)
	}))
	defer server.Close()

	provider, _ := NewOpenAIProvider(OpenAIConfig{BaseURL: server.URL, APIKey: "key"})

	customPrompt := "How many people are in this image?"
	_, err := provider.DescribeImage(context.Background(), ImageRequest{
		Data:     []byte{0xFF, 0xD8, 0xFF},
		MIMEType: "image/jpeg",
		Prompt:   customPrompt,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedPrompt != customPrompt {
		t.Errorf("expected custom prompt %q, got %q", customPrompt, receivedPrompt)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIDescribeImageAPIError
// ---------------------------------------------------------------------------

func TestOpenAIDescribeImageAPIError(t *testing.T) {
	t.Parallel()

	apiErr := openaiErrorResponse{}
	apiErr.Error.Message = "invalid api key"
	apiErr.Error.Type = "authentication_error"
	respBody, _ := json.Marshal(apiErr)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write(respBody)
	}))
	defer server.Close()

	provider, _ := NewOpenAIProvider(OpenAIConfig{BaseURL: server.URL, APIKey: "bad-key"})

	_, err := provider.DescribeImage(context.Background(), ImageRequest{
		Data:     []byte{0xFF, 0xD8, 0xFF},
		MIMEType: "image/jpeg",
	})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("expected error containing 'invalid api key', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIDescribeImageEmptyData
// ---------------------------------------------------------------------------

func TestOpenAIDescribeImageEmptyData(t *testing.T) {
	t.Parallel()

	provider, _ := NewOpenAIProvider(OpenAIConfig{BaseURL: "http://localhost", APIKey: "key"})

	_, err := provider.DescribeImage(context.Background(), ImageRequest{})
	if err == nil {
		t.Fatal("expected error for empty image data")
	}
	if !strings.Contains(err.Error(), "image data is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAITranscribeAudio
// ---------------------------------------------------------------------------

func TestOpenAITranscribeAudio(t *testing.T) {
	t.Parallel()

	transcriptionResp := openaiTranscriptionResp{
		Text:     "Hello, world!",
		Language: "en",
	}
	respBody, _ := json.Marshal(transcriptionResp)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != openaiTranscriptionEndpoint {
			t.Errorf("expected path %q, got %q", openaiTranscriptionEndpoint, r.URL.Path)
		}

		auth := r.Header.Get(openaiAuthHeader)
		if auth != openaiAuthPrefix+"test-key" {
			t.Errorf("expected auth header, got %q", auth)
		}

		ct := r.Header.Get(openaiContentType)
		if !strings.HasPrefix(ct, "multipart/form-data") {
			t.Errorf("expected multipart/form-data, got %q", ct)
		}

		if err := r.ParseMultipartForm(openaiMaxAudioSize); err != nil {
			t.Errorf("failed to parse multipart form: %v", err)
		}
		if r.FormValue(openaiFormModel) != openaiDefaultWhisperModel {
			t.Errorf("expected model %q, got %q", openaiDefaultWhisperModel, r.FormValue(openaiFormModel))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBody)
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	result, err := provider.TranscribeAudio(context.Background(), AudioRequest{
		Data:     []byte("fake-audio-data"),
		MIMEType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != "Hello, world!" {
		t.Errorf("expected text %q, got %q", "Hello, world!", result.Text)
	}
	if result.Language != "en" {
		t.Errorf("expected language %q, got %q", "en", result.Language)
	}
	if result.Model != openaiDefaultWhisperModel {
		t.Errorf("expected model %q, got %q", openaiDefaultWhisperModel, result.Model)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAITranscribeAudioWithOptionalFields
// ---------------------------------------------------------------------------

func TestOpenAITranscribeAudioWithOptionalFields(t *testing.T) {
	t.Parallel()

	respBody, _ := json.Marshal(openaiTranscriptionResp{Text: "Hola mundo", Language: "es"})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(openaiMaxAudioSize); err != nil {
			t.Errorf("failed to parse form: %v", err)
		}
		if r.FormValue(openaiFormLanguage) != "es" {
			t.Errorf("expected language %q, got %q", "es", r.FormValue(openaiFormLanguage))
		}
		if r.FormValue(openaiFormPrompt) != "Spanish audio" {
			t.Errorf("expected prompt %q, got %q", "Spanish audio", r.FormValue(openaiFormPrompt))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBody)
	}))
	defer server.Close()

	provider, _ := NewOpenAIProvider(OpenAIConfig{BaseURL: server.URL, APIKey: "key"})

	_, err := provider.TranscribeAudio(context.Background(), AudioRequest{
		Data:     []byte("audio-data"),
		MIMEType: "audio/mp3",
		Language: "es",
		Prompt:   "Spanish audio",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAITranscribeAudioEmptyData
// ---------------------------------------------------------------------------

func TestOpenAITranscribeAudioEmptyData(t *testing.T) {
	t.Parallel()

	provider, _ := NewOpenAIProvider(OpenAIConfig{BaseURL: "http://localhost", APIKey: "key"})

	_, err := provider.TranscribeAudio(context.Background(), AudioRequest{})
	if err == nil {
		t.Fatal("expected error for empty audio data")
	}
	if !strings.Contains(err.Error(), "audio data is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAITranscribeAudioSizeLimit
// ---------------------------------------------------------------------------

func TestOpenAITranscribeAudioSizeLimit(t *testing.T) {
	t.Parallel()

	provider, _ := NewOpenAIProvider(OpenAIConfig{BaseURL: "http://localhost", APIKey: "key"})

	oversized := make([]byte, openaiMaxAudioSize+1)
	_, err := provider.TranscribeAudio(context.Background(), AudioRequest{
		Data:     oversized,
		MIMEType: "audio/wav",
	})
	if err == nil {
		t.Fatal("expected error for oversized audio")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProviderCapabilities
// ---------------------------------------------------------------------------

func TestOpenAIProviderCapabilities(t *testing.T) {
	t.Parallel()

	provider, err := NewOpenAIProvider(OpenAIConfig{
		BaseURL: "http://localhost",
		APIKey:  "key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if provider.ID() != openaiProviderID {
		t.Errorf("expected ID %q, got %q", openaiProviderID, provider.ID())
	}

	caps := provider.Capabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}

	hasImage, hasAudio := false, false
	for _, c := range caps {
		switch c {
		case CapabilityImage:
			hasImage = true
		case CapabilityAudio:
			hasAudio = true
		}
	}
	if !hasImage {
		t.Error("expected CapabilityImage")
	}
	if !hasAudio {
		t.Error("expected CapabilityAudio")
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProviderMissingAPIKey
// ---------------------------------------------------------------------------

func TestOpenAIProviderMissingAPIKey(t *testing.T) {
	t.Parallel()

	_, err := NewOpenAIProvider(OpenAIConfig{})
	if err == nil {
		t.Fatal("expected error for missing api key")
	}
	if !strings.Contains(err.Error(), "api key is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProviderDefaultConfig
// ---------------------------------------------------------------------------

func TestOpenAIProviderDefaultConfig(t *testing.T) {
	t.Parallel()

	provider, err := NewOpenAIProvider(OpenAIConfig{APIKey: "key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if provider.config.BaseURL != openaiDefaultBaseURL {
		t.Errorf("expected default base URL %q, got %q", openaiDefaultBaseURL, provider.config.BaseURL)
	}
	if provider.client.Timeout != openaiDefaultTimeout {
		t.Errorf("expected default timeout %v, got %v", openaiDefaultTimeout, provider.client.Timeout)
	}
}

// ---------------------------------------------------------------------------
// TestMimeToFilename
// ---------------------------------------------------------------------------

func TestMimeToFilename(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mime     string
		expected string
	}{
		{"audio/wav", "audio.wav"},
		{"audio/x-wav", "audio.wav"},
		{"audio/mpeg", "audio.mp3"},
		{"audio/mp3", "audio.mp3"},
		{"audio/ogg", "audio.ogg"},
		{"audio/flac", "audio.flac"},
		{"audio/mp4", "audio.m4a"},
		{"audio/webm", "audio.webm"},
		{"audio/aac", "audio.aac"},
		{"unknown/type", "audio.wav"},
	}

	for _, tc := range cases {
		got := mimeToFilename(tc.mime)
		if got != tc.expected {
			t.Errorf("mimeToFilename(%q) = %q, want %q", tc.mime, got, tc.expected)
		}
	}
}
