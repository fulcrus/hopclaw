package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

// ---------------------------------------------------------------------------
// Mock inner client for delegation tests
// ---------------------------------------------------------------------------

type oauthMockClient struct {
	mu        sync.Mutex
	chatCalls int
	resp      *agent.ModelResponse
	err       error
}

func (m *oauthMockClient) Chat(_ context.Context, _ agent.ChatRequest) (*agent.ModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chatCalls++
	return m.resp, m.err
}

// ---------------------------------------------------------------------------
// Token server helpers
// ---------------------------------------------------------------------------

// newTokenServer creates a test OAuth token server that issues tokens.
// grant specifies which grant_type to expect ("client_credentials" or "refresh_token").
func newTokenServer(t *testing.T, grant string, expiresIn int) *httptest.Server {
	t.Helper()
	var callCount atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/x-www-form-urlencoded") {
			t.Errorf("expected form-encoded content type, got %q", ct)
			http.Error(w, "bad content type", http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		gt := r.FormValue("grant_type")
		if gt != grant {
			t.Errorf("expected grant_type=%q, got %q", grant, gt)
			http.Error(w, "bad grant type", http.StatusBadRequest)
			return
		}

		n := callCount.Add(1)
		resp := oauthTokenResponse{
			AccessToken: fmt.Sprintf("tok-%d", n),
			TokenType:   "Bearer",
			ExpiresIn:   expiresIn,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

// ---------------------------------------------------------------------------
// TestOAuthClientCredentials
// ---------------------------------------------------------------------------

func TestOAuthClientCredentials(t *testing.T) {
	t.Parallel()

	tokenServer := newTokenServer(t, "client_credentials", 3600)
	defer tokenServer.Close()

	// Set up a mock model server.
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer tok-") {
			t.Errorf("expected Bearer token, got %q", auth)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer modelServer.Close()

	client, err := NewOAuthClient(OAuthConfig{
		TokenURL:     tokenServer.URL,
		ClientID:     "my-id",
		ClientSecret: "my-secret",
		ProviderConfig: ProviderEntry{
			API:     APIOpenAICompletions,
			BaseURL: modelServer.URL + "/v1",
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model: "test-model",
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: "hi",
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("Chat() content = %q, want %q", resp.Message.Content, "ok")
	}
}

// ---------------------------------------------------------------------------
// TestOAuthRefreshToken
// ---------------------------------------------------------------------------

func TestOAuthRefreshToken(t *testing.T) {
	t.Parallel()

	tokenServer := newTokenServer(t, "refresh_token", 3600)
	defer tokenServer.Close()

	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"refreshed\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer modelServer.Close()

	client, err := NewOAuthClient(OAuthConfig{
		TokenURL:     tokenServer.URL,
		RefreshToken: "my-refresh-token",
		ProviderConfig: ProviderEntry{
			API:     APIOpenAICompletions,
			BaseURL: modelServer.URL + "/v1",
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model: "test-model",
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: "hi",
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "refreshed" {
		t.Fatalf("Chat() content = %q, want %q", resp.Message.Content, "refreshed")
	}
}

// ---------------------------------------------------------------------------
// TestOAuthTokenRefresh
// ---------------------------------------------------------------------------

func TestOAuthTokenRefresh(t *testing.T) {
	t.Parallel()

	// Issue tokens with very short expiry so they expire between calls.
	var tokenCallCount atomic.Int64
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		n := tokenCallCount.Add(1)
		resp := oauthTokenResponse{
			AccessToken: fmt.Sprintf("tok-%d", n),
			TokenType:   "Bearer",
			ExpiresIn:   1, // 1 second — will be less than refresh margin
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer tokenServer.Close()

	var receivedTokens sync.Map
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		tok := strings.TrimPrefix(auth, "Bearer ")
		receivedTokens.Store(tok, true)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer modelServer.Close()

	client, err := NewOAuthClient(OAuthConfig{
		TokenURL:     tokenServer.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		ProviderConfig: ProviderEntry{
			API:     APIOpenAICompletions,
			BaseURL: modelServer.URL + "/v1",
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient() error = %v", err)
	}

	// First call — gets tok-1.
	if _, err := client.Chat(context.Background(), agent.ChatRequest{
		Model:    "test-model",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "a"}},
	}); err != nil {
		t.Fatalf("Chat() #1 error = %v", err)
	}

	// Second call — token has 1s expiry which is < 60s margin, so it should refresh.
	if _, err := client.Chat(context.Background(), agent.ChatRequest{
		Model:    "test-model",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "b"}},
	}); err != nil {
		t.Fatalf("Chat() #2 error = %v", err)
	}

	// Verify we got at least 2 different tokens.
	count := tokenCallCount.Load()
	if count < 2 {
		t.Fatalf("expected at least 2 token exchanges, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// TestOAuthChatDelegation
// ---------------------------------------------------------------------------

func TestOAuthChatDelegation(t *testing.T) {
	t.Parallel()

	tokenServer := newTokenServer(t, "client_credentials", 3600)
	defer tokenServer.Close()

	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"delegated\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer modelServer.Close()

	client, err := NewOAuthClient(OAuthConfig{
		TokenURL:     tokenServer.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		ProviderConfig: ProviderEntry{
			API:     APIOpenAICompletions,
			BaseURL: modelServer.URL + "/v1",
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model: "gpt-test",
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "delegated" {
		t.Fatalf("Chat() content = %q, want %q", resp.Message.Content, "delegated")
	}
}

// ---------------------------------------------------------------------------
// TestOAuthStreamDelegation
// ---------------------------------------------------------------------------

func TestOAuthStreamDelegation(t *testing.T) {
	t.Parallel()

	tokenServer := newTokenServer(t, "client_credentials", 3600)
	defer tokenServer.Close()

	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"streamed\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer modelServer.Close()

	client, err := NewOAuthClient(OAuthConfig{
		TokenURL:     tokenServer.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		ProviderConfig: ProviderEntry{
			API:     APIOpenAICompletions,
			BaseURL: modelServer.URL + "/v1",
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient() error = %v", err)
	}

	var mu sync.Mutex
	var deltas []string
	cb := &testStreamCallback{
		onTextDelta: func(_ context.Context, delta string) {
			mu.Lock()
			deltas = append(deltas, delta)
			mu.Unlock()
		},
	}

	resp, err := client.ChatStream(context.Background(), agent.ChatRequest{
		Model: "gpt-test",
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: "hello",
		}},
	}, cb)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if resp.Message.Content != "streamed" {
		t.Fatalf("ChatStream() content = %q, want %q", resp.Message.Content, "streamed")
	}
}

// testStreamCallback is a minimal StreamCallback for testing.
type testStreamCallback struct {
	onTextDelta     func(ctx context.Context, delta string)
	onToolCallStart func(ctx context.Context, toolCallID, toolName string)
	onToolCallDelta func(ctx context.Context, toolCallID, delta string)
	onComplete      func(ctx context.Context)
	onError         func(ctx context.Context, err error)
}

func (c *testStreamCallback) OnTextDelta(ctx context.Context, delta string) {
	if c.onTextDelta != nil {
		c.onTextDelta(ctx, delta)
	}
}
func (c *testStreamCallback) OnReasoningDelta(context.Context, string) {}
func (c *testStreamCallback) OnToolCallStart(ctx context.Context, toolCallID, toolName string) {
	if c.onToolCallStart != nil {
		c.onToolCallStart(ctx, toolCallID, toolName)
	}
}
func (c *testStreamCallback) OnToolCallDelta(ctx context.Context, toolCallID, delta string) {
	if c.onToolCallDelta != nil {
		c.onToolCallDelta(ctx, toolCallID, delta)
	}
}
func (c *testStreamCallback) OnComplete(ctx context.Context) {
	if c.onComplete != nil {
		c.onComplete(ctx)
	}
}
func (c *testStreamCallback) OnError(ctx context.Context, err error) {
	if c.onError != nil {
		c.onError(ctx, err)
	}
}

// ---------------------------------------------------------------------------
// TestOAuthStreamFallbackToChat
// ---------------------------------------------------------------------------

func TestOAuthStreamFallbackToChat(t *testing.T) {
	t.Parallel()

	client := &OAuthClient{
		accessToken: "cached-token",
		tokenExpiry: time.Now().Add(10 * time.Minute),
		inner: &oauthMockClient{
			resp: &agent.ModelResponse{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "fallback-ok",
				},
			},
		},
	}

	var mu sync.Mutex
	var deltas []string
	completed := false
	cb := &testStreamCallback{
		onTextDelta: func(_ context.Context, delta string) {
			mu.Lock()
			deltas = append(deltas, delta)
			mu.Unlock()
		},
		onComplete: func(context.Context) {
			mu.Lock()
			completed = true
			mu.Unlock()
		},
	}
	resp, err := client.ChatStream(context.Background(), agent.ChatRequest{
		Model: "gemini-test",
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: "hi",
		}},
	}, cb)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if resp.Message.Content != "fallback-ok" {
		t.Fatalf("ChatStream() content = %q, want %q", resp.Message.Content, "fallback-ok")
	}
	if len(deltas) != 1 || deltas[0] != "fallback-ok" {
		t.Fatalf("fallback deltas = %#v", deltas)
	}
	if !completed {
		t.Fatal("expected completion callback for fallback stream")
	}
}

// ---------------------------------------------------------------------------
// TestOAuthInvalidConfig
// ---------------------------------------------------------------------------

func TestOAuthInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  OAuthConfig
		want string
	}{
		{
			name: "missing token url",
			cfg: OAuthConfig{
				ClientID:     "id",
				ClientSecret: "secret",
			},
			want: "token url is required",
		},
		{
			name: "missing credentials",
			cfg: OAuthConfig{
				TokenURL: "https://auth.example.com/token",
			},
			want: "either client_id+client_secret or refresh_token is required",
		},
		{
			name: "only client id without secret",
			cfg: OAuthConfig{
				TokenURL: "https://auth.example.com/token",
				ClientID: "id",
			},
			want: "either client_id+client_secret or refresh_token is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewOAuthClient(tt.cfg)
			if err == nil {
				t.Fatal("NewOAuthClient() should return an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestOAuthTokenExchangeError
// ---------------------------------------------------------------------------

func TestOAuthTokenExchangeError(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(oauthErrorResponse{
			Error:            "invalid_grant",
			ErrorDescription: "the refresh token is expired",
		})
	}))
	defer tokenServer.Close()

	client, err := NewOAuthClient(OAuthConfig{
		TokenURL:     tokenServer.URL,
		RefreshToken: "bad-token",
		ProviderConfig: ProviderEntry{
			API: APIOpenAICompletions,
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient() error = %v", err)
	}

	_, err = client.Chat(context.Background(), agent.ChatRequest{
		Model:    "test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("Chat() should fail with token exchange error")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("error = %q, want to contain 'invalid_grant'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// TestOAuthTokenExchangeEmptyAccessToken
// ---------------------------------------------------------------------------

func TestOAuthTokenExchangeEmptyAccessToken(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token_type": "Bearer",
			"expires_in": 3600,
		})
	}))
	defer tokenServer.Close()

	client, err := NewOAuthClient(OAuthConfig{
		TokenURL:     tokenServer.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		ProviderConfig: ProviderEntry{
			API: APIOpenAICompletions,
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient() error = %v", err)
	}

	_, err = client.Chat(context.Background(), agent.ChatRequest{
		Model:    "test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("Chat() should fail when access_token is missing")
	}
	if !strings.Contains(err.Error(), "missing access_token") {
		t.Fatalf("error = %q, want to contain 'missing access_token'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// TestOAuthDefaultExpiry
// ---------------------------------------------------------------------------

func TestOAuthDefaultExpiry(t *testing.T) {
	t.Parallel()

	// Token server returns expires_in=0, so the client should use oauthDefaultExpiry.
	tokenServer := newTokenServer(t, "client_credentials", 0)
	defer tokenServer.Close()

	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer modelServer.Close()

	client, err := NewOAuthClient(OAuthConfig{
		TokenURL:     tokenServer.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		ProviderConfig: ProviderEntry{
			API:     APIOpenAICompletions,
			BaseURL: modelServer.URL + "/v1",
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient() error = %v", err)
	}

	// First call triggers token exchange.
	if _, err := client.Chat(context.Background(), agent.ChatRequest{
		Model:    "test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "a"}},
	}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	// Verify the token expiry was set to default (approximately oauthDefaultExpiry seconds from now).
	client.mu.Lock()
	expiry := client.tokenExpiry
	client.mu.Unlock()

	minExpected := time.Now().Add(time.Duration(oauthDefaultExpiry-10) * time.Second)
	if expiry.Before(minExpected) {
		t.Fatalf("token expiry %v is earlier than expected minimum %v", expiry, minExpected)
	}
}

// ---------------------------------------------------------------------------
// TestOAuthConcurrentAccess
// ---------------------------------------------------------------------------

func TestOAuthConcurrentAccess(t *testing.T) {
	t.Parallel()

	tokenServer := newTokenServer(t, "client_credentials", 3600)
	defer tokenServer.Close()

	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer modelServer.Close()

	client, err := NewOAuthClient(OAuthConfig{
		TokenURL:     tokenServer.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		ProviderConfig: ProviderEntry{
			API:     APIOpenAICompletions,
			BaseURL: modelServer.URL + "/v1",
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient() error = %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := client.Chat(context.Background(), agent.ChatRequest{
				Model:    "test",
				Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hi"}},
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Chat() error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestOAuthScopesIncluded
// ---------------------------------------------------------------------------

func TestOAuthScopesIncluded(t *testing.T) {
	t.Parallel()

	var receivedScope string
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		receivedScope = r.FormValue("scope")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(oauthTokenResponse{
			AccessToken: "tok-scoped",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		})
	}))
	defer tokenServer.Close()

	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer modelServer.Close()

	client, err := NewOAuthClient(OAuthConfig{
		TokenURL:     tokenServer.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		Scopes:       []string{"model.read", "model.write"},
		ProviderConfig: ProviderEntry{
			API:     APIOpenAICompletions,
			BaseURL: modelServer.URL + "/v1",
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient() error = %v", err)
	}

	if _, err := client.Chat(context.Background(), agent.ChatRequest{
		Model:    "test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if receivedScope != "model.read model.write" {
		t.Fatalf("scope = %q, want %q", receivedScope, "model.read model.write")
	}
}
