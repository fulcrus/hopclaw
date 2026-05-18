package server

import (
	"context"
	"html/template"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/i18n"
	"github.com/fulcrus/hopclaw/logging"
)

type landingCopy struct {
	Lang            string
	Title           string
	Tagline         string
	ScopeTitle      string
	ScopeBody       string
	WebTitle        string
	WebBody         string
	InstallTitle    string
	InstallBody     string
	CompatTitle     string
	CompatBody      string
	APITitle        string
	APIBody         string
	QuickTitle      string
	QuickCommands   []string
	EnglishLabel    string
	ChineseLabel    string
	EnglishURL      string
	ChineseURL      string
	RepositoryLabel string
	RepositoryValue string
	HealthLabel     string
	HealthValue     string
	ToolsLabel      string
	ToolsValue      string
	ApprovalsLabel  string
	ApprovalsValue  string
}

var landingTemplate = template.Must(template.New("landing").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f2efe8;
      --panel: rgba(255,255,255,0.9);
      --ink: #1f1a17;
      --muted: #6c5d53;
      --line: rgba(31,26,23,0.12);
      --accent: #146356;
      --accent-2: #cf5c36;
      --shadow: 0 24px 70px rgba(31,26,23,0.12);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Iowan Old Style", "Palatino Linotype", "Noto Serif SC", serif;
      background:
        radial-gradient(circle at top left, rgba(20,99,86,0.20), transparent 28rem),
        radial-gradient(circle at bottom right, rgba(207,92,54,0.16), transparent 26rem),
        linear-gradient(180deg, #f8f4ed, var(--bg));
      color: var(--ink);
      min-height: 100vh;
    }
    .shell {
      max-width: 1100px;
      margin: 0 auto;
      padding: 32px 20px 48px;
    }
    .hero {
      display: grid;
      gap: 18px;
      padding: 28px;
      border: 1px solid var(--line);
      border-radius: 28px;
      background: linear-gradient(135deg, rgba(255,255,255,0.92), rgba(255,249,243,0.86));
      box-shadow: var(--shadow);
      overflow: hidden;
      position: relative;
    }
    .hero::after {
      content: "";
      position: absolute;
      inset: auto -40px -90px auto;
      width: 260px;
      height: 260px;
      background: radial-gradient(circle, rgba(20,99,86,0.18), transparent 68%);
      pointer-events: none;
    }
    .eyebrow {
      font: 600 12px/1.4 ui-monospace, "SFMono-Regular", Menlo, monospace;
      letter-spacing: 0.18em;
      text-transform: uppercase;
      color: var(--accent);
    }
    h1 {
      margin: 0;
      font-size: clamp(2.3rem, 5vw, 4.5rem);
      line-height: 0.94;
    }
    .tagline {
      margin: 0;
      max-width: 50rem;
      color: var(--muted);
      font-size: 1.05rem;
      line-height: 1.7;
    }
    .lang {
      display: flex;
      gap: 12px;
      flex-wrap: wrap;
      align-items: center;
      font: 600 0.95rem/1.4 "Avenir Next", "Helvetica Neue", sans-serif;
    }
    .lang a {
      color: var(--ink);
      text-decoration: none;
      border-bottom: 2px solid transparent;
      padding-bottom: 2px;
    }
    .lang a:hover { border-color: var(--accent); }
    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
      gap: 18px;
      margin-top: 20px;
    }
    .card {
      border: 1px solid var(--line);
      border-radius: 22px;
      background: var(--panel);
      box-shadow: var(--shadow);
      padding: 22px 20px;
    }
    .card h2 {
      margin: 0 0 10px;
      font-size: 1.1rem;
      font-family: "Avenir Next", "Helvetica Neue", sans-serif;
    }
    .card p {
      margin: 0;
      color: var(--muted);
      line-height: 1.7;
    }
    .facts {
      display: grid;
      gap: 10px;
      margin-top: 20px;
    }
    .fact {
      display: grid;
      grid-template-columns: 140px 1fr;
      gap: 12px;
      align-items: start;
      font-size: 0.96rem;
    }
    .fact strong {
      font-family: "Avenir Next", "Helvetica Neue", sans-serif;
    }
    code, pre {
      font-family: ui-monospace, "SFMono-Regular", Menlo, monospace;
    }
    pre {
      margin: 0;
      padding: 16px;
      background: #171412;
      color: #f6f1e8;
      border-radius: 18px;
      overflow-x: auto;
      line-height: 1.5;
      font-size: 0.9rem;
    }
    .commands {
      display: grid;
      gap: 12px;
      margin-top: 20px;
    }
    .muted {
      color: var(--muted);
    }
    @media (max-width: 720px) {
      .fact { grid-template-columns: 1fr; }
      .hero { padding: 22px 18px; }
      .card { padding: 18px 16px; }
    }
  </style>
</head>
<body>
  <main class="shell" data-testid="landing-shell" data-locale="{{.Lang}}">
    <section class="hero" data-testid="landing-hero">
      <div class="eyebrow">HopClaw Runtime</div>
      <div class="lang" data-testid="landing-language-switch">
        <span>{{.EnglishLabel}} / {{.ChineseLabel}}</span>
        <a href="{{.EnglishURL}}">{{.EnglishLabel}}</a>
        <a href="{{.ChineseURL}}">{{.ChineseLabel}}</a>
      </div>
      <h1 data-testid="landing-title">{{.Title}}</h1>
      <p class="tagline" data-testid="landing-tagline">{{.Tagline}}</p>
      <div class="facts" data-testid="landing-facts">
        <div class="fact" data-testid="landing-fact-repository"><strong>{{.RepositoryLabel}}</strong><span class="muted">{{.RepositoryValue}}</span></div>
        <div class="fact" data-testid="landing-fact-health"><strong>{{.HealthLabel}}</strong><span><code>{{.HealthValue}}</code></span></div>
        <div class="fact" data-testid="landing-fact-tools"><strong>{{.ToolsLabel}}</strong><span><code>{{.ToolsValue}}</code></span></div>
        <div class="fact" data-testid="landing-fact-approvals"><strong>{{.ApprovalsLabel}}</strong><span><code>{{.ApprovalsValue}}</code></span></div>
      </div>
    </section>
    <section class="grid" data-testid="landing-cards">
      <article class="card" data-testid="landing-card-scope">
        <h2>{{.ScopeTitle}}</h2>
        <p>{{.ScopeBody}}</p>
      </article>
      <article class="card" data-testid="landing-card-web">
        <h2>{{.WebTitle}}</h2>
        <p>{{.WebBody}}</p>
      </article>
      <article class="card" data-testid="landing-card-install">
        <h2>{{.InstallTitle}}</h2>
        <p>{{.InstallBody}}</p>
      </article>
      <article class="card" data-testid="landing-card-compat">
        <h2>{{.CompatTitle}}</h2>
        <p>{{.CompatBody}}</p>
      </article>
      <article class="card" data-testid="landing-card-api">
        <h2>{{.APITitle}}</h2>
        <p>{{.APIBody}}</p>
      </article>
    </section>
    <section class="card commands" data-testid="landing-commands">
      <h2>{{.QuickTitle}}</h2>
      {{range .QuickCommands}}
      <pre data-testid="landing-command">{{.}}</pre>
      {{end}}
    </section>
  </main>
</body>
</html>`))

func (s *Server) handleLandingPage(w http.ResponseWriter, r *http.Request) {
	data := landingContent(resolveLandingLanguage(r))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	logging.LogIfErr(r.Context(), landingTemplate.Execute(w, data), "execute landing template failed")
}

func resolveLandingLanguage(r *http.Request) string {
	if lang := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("lang"))); lang != "" {
		if strings.HasPrefix(lang, "zh") {
			return "zh-CN"
		}
		return "en"
	}
	accept := strings.ToLower(r.Header.Get("Accept-Language"))
	if strings.Contains(accept, "zh") {
		return "zh-CN"
	}
	return "en"
}

func landingContent(lang string) landingCopy {
	locale := landingLocale(lang)
	ctx := i18n.WithLocale(context.Background(), locale)
	return landingCopy{
		Lang:         string(locale),
		Title:        i18n.TCtx(ctx, "landing.title"),
		Tagline:      i18n.TCtx(ctx, "landing.tagline"),
		ScopeTitle:   i18n.TCtx(ctx, "landing.scope_title"),
		ScopeBody:    i18n.TCtx(ctx, "landing.scope_body"),
		WebTitle:     i18n.TCtx(ctx, "landing.web_title"),
		WebBody:      i18n.TCtx(ctx, "landing.web_body"),
		InstallTitle: i18n.TCtx(ctx, "landing.install_title"),
		InstallBody:  i18n.TCtx(ctx, "landing.install_body"),
		CompatTitle:  i18n.TCtx(ctx, "landing.compat_title"),
		CompatBody:   i18n.TCtx(ctx, "landing.compat_body"),
		APITitle:     i18n.TCtx(ctx, "landing.api_title"),
		APIBody:      i18n.TCtx(ctx, "landing.api_body"),
		QuickTitle:   i18n.TCtx(ctx, "landing.quick_title"),
		QuickCommands: []string{
			i18n.TCtx(ctx, "landing.quick_command_install"),
			i18n.TCtx(ctx, "landing.quick_command_binary"),
			i18n.TCtx(ctx, "landing.quick_command_tools"),
		},
		EnglishLabel:    i18n.TCtx(ctx, "landing.english_label"),
		ChineseLabel:    i18n.TCtx(ctx, "landing.chinese_label"),
		EnglishURL:      "/?lang=en",
		ChineseURL:      "/?lang=zh-CN",
		RepositoryLabel: i18n.TCtx(ctx, "landing.repository_label"),
		RepositoryValue: i18n.TCtx(ctx, "landing.repository_value"),
		HealthLabel:     i18n.TCtx(ctx, "landing.health_label"),
		HealthValue:     "GET /healthz",
		ToolsLabel:      i18n.TCtx(ctx, "landing.tools_label"),
		ToolsValue:      "GET /runtime/tools",
		ApprovalsLabel:  i18n.TCtx(ctx, "landing.approvals_label"),
		ApprovalsValue:  "GET /runtime/approvals?status=pending",
	}
}

func landingLocale(lang string) i18n.Locale {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh") {
		return i18n.ZhCN
	}
	return i18n.EN
}
