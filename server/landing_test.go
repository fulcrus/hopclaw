package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/i18n"
)

// ---------------------------------------------------------------------------
// resolveLandingLanguage
// ---------------------------------------------------------------------------

func TestResolveLandingLanguage_DefaultEnglish(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	lang := resolveLandingLanguage(req)
	if lang != "en" {
		t.Errorf("expected en, got %q", lang)
	}
}

func TestResolveLandingLanguage_QueryParamZH(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/?lang=zh-CN", nil)
	lang := resolveLandingLanguage(req)
	if lang != "zh-CN" {
		t.Errorf("expected zh-CN, got %q", lang)
	}
}

func TestResolveLandingLanguage_QueryParamZHShort(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/?lang=zh", nil)
	lang := resolveLandingLanguage(req)
	if lang != "zh-CN" {
		t.Errorf("expected zh-CN for lang=zh, got %q", lang)
	}
}

func TestResolveLandingLanguage_QueryParamEN(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/?lang=en", nil)
	lang := resolveLandingLanguage(req)
	if lang != "en" {
		t.Errorf("expected en, got %q", lang)
	}
}

func TestResolveLandingLanguage_AcceptHeaderZH(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	lang := resolveLandingLanguage(req)
	if lang != "zh-CN" {
		t.Errorf("expected zh-CN from Accept-Language, got %q", lang)
	}
}

func TestResolveLandingLanguage_AcceptHeaderEN(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	lang := resolveLandingLanguage(req)
	if lang != "en" {
		t.Errorf("expected en, got %q", lang)
	}
}

func TestResolveLandingLanguage_QueryOverridesHeader(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/?lang=en", nil)
	req.Header.Set("Accept-Language", "zh-CN")
	lang := resolveLandingLanguage(req)
	if lang != "en" {
		t.Errorf("expected query param to override header, got %q", lang)
	}
}

// ---------------------------------------------------------------------------
// landingContent
// ---------------------------------------------------------------------------

func TestLandingContent_English(t *testing.T) {
	t.Parallel()

	content := landingContent("en")
	ctx := i18n.WithLocale(context.Background(), i18n.EN)
	if content.Lang != "en" {
		t.Errorf("Lang = %q, want en", content.Lang)
	}
	if content.Title != i18n.TCtx(ctx, "landing.title") {
		t.Fatalf("Title = %q, want catalog-backed value", content.Title)
	}
	if content.Tagline != i18n.TCtx(ctx, "landing.tagline") {
		t.Fatalf("Tagline = %q, want catalog-backed value", content.Tagline)
	}
	if len(content.QuickCommands) == 0 {
		t.Error("expected non-empty QuickCommands")
	}
	if content.HealthValue != "GET /healthz" {
		t.Errorf("HealthValue = %q", content.HealthValue)
	}
}

func TestLandingContent_Chinese(t *testing.T) {
	t.Parallel()

	content := landingContent("zh-CN")
	ctx := i18n.WithLocale(context.Background(), i18n.ZhCN)
	enContent := landingContent("en")
	if content.Lang != "zh-CN" {
		t.Errorf("Lang = %q, want zh-CN", content.Lang)
	}
	if content.Title != i18n.TCtx(ctx, "landing.title") {
		t.Fatalf("Title = %q, want catalog-backed value", content.Title)
	}
	if content.Title == enContent.Title {
		t.Error("expected Chinese title to differ from English")
	}
	if content.WebTitle == enContent.WebTitle {
		t.Error("expected Chinese web card title to differ from English")
	}
}

func TestLandingContent_FallbackToEnglish(t *testing.T) {
	t.Parallel()

	content := landingContent("fr")
	if content.Lang != "en" {
		t.Errorf("expected fallback to en, got %q", content.Lang)
	}
}

// ---------------------------------------------------------------------------
// landingTemplate renders without error
// ---------------------------------------------------------------------------

func TestLandingTemplate_RendersEnglish(t *testing.T) {
	t.Parallel()

	content := landingContent("en")
	w := httptest.NewRecorder()
	if err := landingTemplate.Execute(w, content); err != nil {
		t.Fatalf("template execute: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `data-testid="landing-shell"`) {
		t.Fatal("expected rendered HTML to expose landing shell testid")
	}
	if !strings.Contains(body, `data-locale="en"`) {
		t.Error("expected structured locale marker to be en")
	}
}

func TestLandingTemplate_RendersChinese(t *testing.T) {
	t.Parallel()

	content := landingContent("zh-CN")
	w := httptest.NewRecorder()
	if err := landingTemplate.Execute(w, content); err != nil {
		t.Fatalf("template execute: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `data-locale="zh-CN"`) {
		t.Error("expected structured locale marker to be zh-CN")
	}
	if !strings.Contains(body, `data-testid="landing-title"`) {
		t.Fatal("expected rendered HTML to expose landing title testid")
	}
}
