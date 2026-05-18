package usererror

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/fulcrus/hopclaw/agent"
	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
	"github.com/fulcrus/hopclaw/i18n"
)

const genericMessageKey = "user_error.generic"

type Classification struct {
	Code       apiresponse.ErrorCode
	MessageKey string
	HTTPStatus int
}

func Classify(err error) Classification {
	if err == nil {
		return Classification{}
	}
	if out, ok := classifyTyped(err); ok {
		return normalizeClassification(out)
	}
	return normalizeClassification(classifyText(err.Error()))
}

func Code(err error) apiresponse.ErrorCode {
	return Classify(err).Code
}

func HTTPStatus(err error, fallback int) int {
	if err == nil {
		return http.StatusOK
	}
	if status := Classify(err).HTTPStatus; status > 0 {
		return status
	}
	return fallback
}

// MessageKey classifies an error into a stable user-facing message key.
func MessageKey(err error) string {
	if err == nil {
		return ""
	}
	key := Classify(err).MessageKey
	if strings.TrimSpace(key) == "" {
		return genericMessageKey
	}
	return key
}

// MessageKeyText classifies raw internal error text into a stable message key.
func MessageKeyText(raw string) string {
	key := normalizeClassification(classifyText(raw)).MessageKey
	if strings.TrimSpace(key) == "" {
		return genericMessageKey
	}
	return key
}

// Humanize converts internal errors to user-friendly messages.
func Humanize(err error, locale string) string {
	key := MessageKey(err)
	if key == "" {
		return ""
	}
	return localized(resolveLocale(locale), key)
}

// HumanizeText converts raw internal error text to a user-friendly message.
func HumanizeText(raw string, locale string) string {
	loc := resolveLocale(locale)
	return localized(loc, MessageKeyText(raw))
}

// InferLocale picks a simple user-facing locale from free-form input text.
func InferLocale(text string) i18n.Locale {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return i18n.ZhCN
		}
	}
	return i18n.EN
}

func resolveLocale(raw string) i18n.Locale {
	resolved := i18n.ResolveConfiguredLocale(raw)
	if resolved == "" {
		return i18n.EN
	}
	return resolved
}

func localized(locale i18n.Locale, key string) string {
	msg := i18n.TCtx(i18n.WithLocale(context.Background(), locale), key)
	if strings.TrimSpace(msg) == "" || msg == key {
		return i18n.TCtx(i18n.WithLocale(context.Background(), i18n.EN), genericMessageKey)
	}
	return msg
}

func containsAny(raw string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(raw, needle) {
			return true
		}
	}
	return false
}

func classifyTyped(err error) (Classification, bool) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return Classification{
			Code:       apiresponse.ErrorCodeServiceUnavailable,
			MessageKey: "user_error.timeout",
			HTTPStatus: http.StatusServiceUnavailable,
		}, true
	case errors.Is(err, context.Canceled),
		errors.Is(err, agent.ErrRunCancelled):
		return Classification{
			Code:       apiresponse.ErrorCodeConflict,
			MessageKey: "user_error.cancelled",
			HTTPStatus: http.StatusConflict,
		}, true
	case errors.Is(err, agent.ErrRunRejected):
		return Classification{
			Code:       apiresponse.ErrorCodeConflict,
			MessageKey: "user_error.queue_busy",
			HTTPStatus: http.StatusConflict,
		}, true
	case errors.Is(err, agent.ErrTooManyToolRounds):
		return Classification{
			Code:       apiresponse.ErrorCodeInternalError,
			MessageKey: "user_error.too_complex",
			HTTPStatus: http.StatusInternalServerError,
		}, true
	case errors.Is(err, agent.ErrToolDenied):
		return Classification{
			Code:       apiresponse.ErrorCodeAuthorizationDenied,
			MessageKey: genericMessageKey,
			HTTPStatus: http.StatusForbidden,
		}, true
	case errors.Is(err, agent.ErrToolExecutorNil),
		errors.Is(err, agent.ErrModelClientNil),
		errors.Is(err, agent.ErrContextEngineNil):
		return Classification{
			Code:       apiresponse.ErrorCodeInternalError,
			MessageKey: "user_error.internal_configuration",
			HTTPStatus: http.StatusInternalServerError,
		}, true
	case errors.Is(err, agent.ErrApprovalStoreNil),
		errors.Is(err, agent.ErrArtifactStoreNil):
		return Classification{
			Code:       apiresponse.ErrorCodeServiceUnavailable,
			MessageKey: "user_error.unavailable",
			HTTPStatus: http.StatusServiceUnavailable,
		}, true
	default:
		return Classification{}, false
	}
}

func classifyText(raw string) Classification {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return Classification{
			Code:       apiresponse.ErrorCodeInternalError,
			MessageKey: genericMessageKey,
			HTTPStatus: http.StatusInternalServerError,
		}
	}
	switch {
	case containsAny(raw,
		"context deadline exceeded",
		"deadline exceeded",
		"timed out",
		"request timeout",
		"gateway timeout",
		"timeout after",
		"timeout awaiting",
		"connection timeout",
	):
		return Classification{
			Code:       apiresponse.ErrorCodeServiceUnavailable,
			MessageKey: "user_error.timeout",
			HTTPStatus: http.StatusServiceUnavailable,
		}
	case containsAny(raw,
		"rate limit",
		"rate limited",
		"too many requests",
		"resource exhausted",
		"429",
	):
		return Classification{
			Code:       apiresponse.ErrorCodeRateLimited,
			MessageKey: "user_error.rate_limited",
			HTTPStatus: http.StatusTooManyRequests,
		}
	case containsAny(raw,
		"service unavailable",
		"temporarily unavailable",
		"currently unavailable",
		"backend unavailable",
		"provider unavailable",
		"model unavailable",
		"tool unavailable",
		"upstream unavailable",
		"overloaded",
		"connection refused",
	):
		return Classification{
			Code:       apiresponse.ErrorCodeServiceUnavailable,
			MessageKey: "user_error.unavailable",
			HTTPStatus: http.StatusServiceUnavailable,
		}
	case containsAny(raw,
		"context canceled",
		"context cancelled",
		"run is canceled",
		"run is cancelled",
	):
		return Classification{
			Code:       apiresponse.ErrorCodeConflict,
			MessageKey: "user_error.cancelled",
			HTTPStatus: http.StatusConflict,
		}
	case strings.Contains(raw, "tool executor is required"):
		return Classification{
			Code:       apiresponse.ErrorCodeInternalError,
			MessageKey: "user_error.internal_configuration",
			HTTPStatus: http.StatusInternalServerError,
		}
	case strings.Contains(raw, "run rejected by queue mode"):
		return Classification{
			Code:       apiresponse.ErrorCodeConflict,
			MessageKey: "user_error.queue_busy",
			HTTPStatus: http.StatusConflict,
		}
	case strings.Contains(raw, "too many tool rounds"):
		return Classification{
			Code:       apiresponse.ErrorCodeInternalError,
			MessageKey: "user_error.too_complex",
			HTTPStatus: http.StatusInternalServerError,
		}
	case strings.Contains(raw, "not found"):
		return Classification{
			Code:       apiresponse.ErrorCodeNotFound,
			MessageKey: genericMessageKey,
			HTTPStatus: http.StatusNotFound,
		}
	case strings.Contains(raw, "already exists"),
		strings.Contains(raw, "already resolved"),
		strings.Contains(raw, "already "):
		return Classification{
			Code:       apiresponse.ErrorCodeConflict,
			MessageKey: genericMessageKey,
			HTTPStatus: http.StatusConflict,
		}
	case strings.Contains(raw, "forbidden"),
		strings.Contains(raw, "denied"):
		return Classification{
			Code:       apiresponse.ErrorCodeAuthorizationDenied,
			MessageKey: genericMessageKey,
			HTTPStatus: http.StatusForbidden,
		}
	case strings.Contains(raw, "required"),
		strings.Contains(raw, "invalid"):
		return Classification{
			Code:       apiresponse.ErrorCodeInvalidArgument,
			MessageKey: genericMessageKey,
			HTTPStatus: http.StatusBadRequest,
		}
	default:
		return Classification{
			Code:       apiresponse.ErrorCodeInternalError,
			MessageKey: genericMessageKey,
			HTTPStatus: http.StatusInternalServerError,
		}
	}
}

func normalizeClassification(in Classification) Classification {
	if in.Code == "" && in.HTTPStatus > 0 {
		in.Code = apiresponse.DefaultHTTPErrorCode(in.HTTPStatus)
	}
	if strings.TrimSpace(in.MessageKey) == "" {
		in.MessageKey = genericMessageKey
	}
	return in
}
