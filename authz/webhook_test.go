package authz

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebhookDeciderAllowsRequests(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer demo-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var req AuthorizationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if req.Resource != ResourceOperator || req.Action != ActionRead {
			t.Fatalf("request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(AuthorizationDecision{
			Allowed: true,
			Reason:  "allowed by corp authz",
		})
	}))
	defer server.Close()

	decider, err := NewWebhookDecider(WebhookConfig{
		URL: server.URL,
		Headers: map[string]string{
			"Authorization": "Bearer demo-token",
		},
	})
	if err != nil {
		t.Fatalf("NewWebhookDecider() error = %v", err)
	}

	decision, err := decider.Decide(context.Background(), AuthorizationRequest{
		Resource: ResourceOperator,
		Action:   ActionRead,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision = %+v", decision)
	}
	if decision.Source != "webhook" {
		t.Fatalf("Source = %q, want webhook", decision.Source)
	}
}

func TestWebhookDeciderSupportsWrappedDecisionAndHMAC(t *testing.T) {
	t.Parallel()

	secret := "demo-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestamp := strings.TrimSpace(r.Header.Get("X-HopClaw-Timestamp"))
		signature := strings.TrimSpace(r.Header.Get("X-HopClaw-Signature"))
		body, err := ioReadAll(r)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		expected := "sha256=" + computeTestHMAC(secret, timestamp, body)
		if signature != expected {
			t.Fatalf("signature = %q, want %q", signature, expected)
		}
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"decision": AuthorizationDecision{
				Allowed: false,
				Reason:  "corp policy denies access",
			},
		})
	}))
	defer server.Close()

	decider, err := NewWebhookDecider(WebhookConfig{
		URL:     server.URL,
		Secret:  secret,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewWebhookDecider() error = %v", err)
	}
	decision, err := decider.Decide(context.Background(), AuthorizationRequest{
		Resource: ResourceConfig,
		Action:   ActionWrite,
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Allowed {
		t.Fatalf("decision = %+v, want deny", decision)
	}
	if decision.Reason != "corp policy denies access" {
		t.Fatalf("Reason = %q", decision.Reason)
	}
}

func TestNewWebhookDeciderRejectsInvalidURL(t *testing.T) {
	t.Parallel()

	if _, err := NewWebhookDecider(WebhookConfig{URL: "/relative"}); err == nil {
		t.Fatal("expected invalid URL to fail")
	}
}

func ioReadAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func computeTestHMAC(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
