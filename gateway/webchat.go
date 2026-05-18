package gateway

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/i18n"
)

const (
	defaultConsoleSessionKey = "webchat"
	defaultConsoleLang       = "en"
)

// ---------------------------------------------------------------------------
// Static file serving
// ---------------------------------------------------------------------------

//go:embed webchat-ui
var webChatUIFS embed.FS

func (g *Gateway) consoleUIHandler() http.Handler {
	sub, err := fs.Sub(webChatUIFS, "webchat-ui")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "webchat ui unavailable", http.StatusInternalServerError)
		})
	}
	return http.FileServer(http.FS(sub))
}

// ---------------------------------------------------------------------------
// Config API
// ---------------------------------------------------------------------------

type webChatConfig struct {
	SessionKey string `json:"session_key"`
	AuthToken  string `json:"auth_token,omitempty"`
	Lang       string `json:"lang"`
	Locale     string `json:"locale,omitempty"`
}

type webChatCatalog struct {
	Lang     string            `json:"lang"`
	Locale   string            `json:"locale"`
	Messages map[string]string `json:"messages"`
}

func (g *Gateway) handleWebChatConfig(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	sessionKey := strings.TrimSpace(query.Get("session"))
	if sessionKey == "" {
		sessionKey = defaultConsoleSessionKey
	}
	locale := g.resolveConsoleLocale(query.Get("lang"))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(webChatConfig{
		SessionKey: sessionKey,
		Lang:       consoleLang(locale),
		Locale:     string(locale),
	})
}

func (g *Gateway) handleWebChatCatalog(w http.ResponseWriter, r *http.Request) {
	locale := g.resolveConsoleLocale(r.URL.Query().Get("lang"))
	i18n.Global().EnsureLoaded(locale)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(webChatCatalog{
		Lang:     consoleLang(locale),
		Locale:   string(locale),
		Messages: i18n.Global().Messages(locale),
	})
}

func (g *Gateway) resolveConsoleLocale(raw string) i18n.Locale {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		return i18n.ResolveConfiguredLocale(trimmed)
	}
	if g != nil && g.effectiveCfg != nil {
		if trimmed := strings.TrimSpace(g.effectiveCfg.Current().Locale); trimmed != "" {
			return i18n.ResolveConfiguredLocale(trimmed)
		}
	}
	return i18n.ResolveConfiguredLocale(defaultConsoleLang)
}

func consoleLang(locale i18n.Locale) string {
	switch locale {
	case i18n.ZhCN, i18n.ZhTW:
		return "zh"
	case i18n.JaJP:
		return "ja"
	default:
		return "en"
	}
}
