package usererror

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/i18n"
	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
)

func TestMessageKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "timeout", err: context.DeadlineExceeded, want: "user_error.timeout"},
		{name: "cancelled", err: context.Canceled, want: "user_error.cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := MessageKey(tt.err); got != tt.want {
				t.Fatalf("MessageKey(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestClassifyUsesTypedErrorsFirst(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		err        error
		wantCode   apiresponse.ErrorCode
		wantKey    string
		wantStatus int
	}{
		{
			name:       "run rejected",
			err:        agent.ErrRunRejected,
			wantCode:   apiresponse.ErrorCodeConflict,
			wantKey:    "user_error.queue_busy",
			wantStatus: http.StatusConflict,
		},
		{
			name:       "tool denied",
			err:        agent.ErrToolDenied,
			wantCode:   apiresponse.ErrorCodeAuthorizationDenied,
			wantKey:    genericMessageKey,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "wrapped cancelled",
			err:        fmt.Errorf("stop: %w", agent.ErrRunCancelled),
			wantCode:   apiresponse.ErrorCodeConflict,
			wantKey:    "user_error.cancelled",
			wantStatus: http.StatusConflict,
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tt.err)
			if got.Code != tt.wantCode {
				t.Fatalf("Classify(%v).Code = %q, want %q", tt.err, got.Code, tt.wantCode)
			}
			if got.MessageKey != tt.wantKey {
				t.Fatalf("Classify(%v).MessageKey = %q, want %q", tt.err, got.MessageKey, tt.wantKey)
			}
			if got.HTTPStatus != tt.wantStatus {
				t.Fatalf("Classify(%v).HTTPStatus = %d, want %d", tt.err, got.HTTPStatus, tt.wantStatus)
			}
		})
	}
}

func TestHTTPStatusAndCodeFallbackFromText(t *testing.T) {
	t.Parallel()

	err := errors.New("invalid request payload")
	if got := Code(err); got != apiresponse.ErrorCodeInvalidArgument {
		t.Fatalf("Code(%v) = %q, want %q", err, got, apiresponse.ErrorCodeInvalidArgument)
	}
	if got := HTTPStatus(err, http.StatusInternalServerError); got != http.StatusBadRequest {
		t.Fatalf("HTTPStatus(%v) = %d, want %d", err, got, http.StatusBadRequest)
	}
}

func TestMessageKeyTextMatchesKnownErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "internal config", raw: "tool executor is required", want: "user_error.internal_configuration"},
		{name: "timeout variants", raw: "LLM request timed out", want: "user_error.timeout"},
		{name: "rate limited", raw: "429 too many requests", want: "user_error.rate_limited"},
		{name: "service unavailable", raw: "service unavailable", want: "user_error.unavailable"},
		{name: "queue busy", raw: "run rejected by queue mode", want: "user_error.queue_busy"},
		{name: "too complex", raw: "too many tool rounds", want: "user_error.too_complex"},
		{name: "fallback", raw: "unknown crash", want: "user_error.generic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := MessageKeyText(tt.raw); got != tt.want {
				t.Fatalf("MessageKeyText(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestHumanizeUsesLocalizedCatalogEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		locale i18n.Locale
		want   string
	}{
		{
			name:   "timeout english",
			err:    context.DeadlineExceeded,
			locale: i18n.EN,
			want:   "user_error.timeout",
		},
		{
			name:   "cancelled chinese",
			err:    context.Canceled,
			locale: i18n.ZhCN,
			want:   "user_error.cancelled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := i18n.TCtx(i18n.WithLocale(context.Background(), tt.locale), tt.want)
			if got := Humanize(tt.err, string(tt.locale)); got != want {
				t.Fatalf("Humanize(%v, %q) = %q, want %q", tt.err, tt.locale, got, want)
			}
		})
	}
}

func TestInferLocale(t *testing.T) {
	t.Parallel()

	if got := InferLocale("帮我修一下"); got != "zh-CN" {
		t.Fatalf("InferLocale(chinese) = %q", got)
	}
	if got := InferLocale("please fix it"); got != "en" {
		t.Fatalf("InferLocale(english) = %q", got)
	}
}
