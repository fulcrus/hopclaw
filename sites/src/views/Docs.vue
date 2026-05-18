<script setup>
import { useI18n } from '../i18n'

const { t, ta } = useI18n()

const quickstart = `# macOS / Linux
curl -fsSL https://hopclaw.com/install.sh | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh
hopclaw

# Windows PowerShell
$env:HOPCLAW_INSTALL_RUN_ONBOARD='1'; irm https://hopclaw.com/install.ps1 | iex
hopclaw

hopclaw doctor
hopclaw dashboard --open`

const configExample = `runtime:
  profile: desktop
  audit:
    enabled: true

skills:
  auto_detect: true
  auto_refresh: true
  install_policy: ask

hosts:
  browser:
    enabled: true
    base_url: http://127.0.0.1:9223
    auth_token: \${HOPCLAW_BROWSER_TOKEN}`
</script>

<template>
  <div class="page-shell docs-page">
    <section class="page-intro">
      <div class="container">
        <div class="hero-grid">
          <div class="stack">
            <span class="eyebrow">{{ t('docs.badge') }}</span>
            <h1 class="headline">{{ t('docs.title') }}</h1>
            <p class="lede">{{ t('docs.desc') }}</p>
          </div>
          <div class="stack">
            <span class="badge badge-accent">{{ t('docs.quickStartTitle') }}</span>
            <p class="panel-note">{{ t('docs.quickStartDesc') }}</p>
            <pre class="code-block"><code>{{ quickstart }}</code></pre>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container split">
        <div>
          <div class="section-head">
            <span class="eyebrow">{{ t('docs.sourcesEyebrow') }}</span>
            <h2 class="section-title">{{ t('docs.sourcesTitle') }}</h2>
            <p class="section-subtitle">{{ t('docs.sourcesDesc') }}</p>
          </div>
          <div class="grid-2">
            <a
              v-for="item in ta('docs.sources')"
              :key="item.title"
              :href="item.href"
              target="_blank"
              rel="noreferrer"
              class="card callout-card source-card"
            >
              <h3>{{ item.title }}</h3>
              <p>{{ item.desc }}</p>
              <span class="source-cta">{{ item.cta }}</span>
            </a>
          </div>
        </div>

        <div>
          <div class="section-head">
            <span class="eyebrow">{{ t('docs.configEyebrow') }}</span>
            <h2 class="section-title">{{ t('docs.configTitle') }}</h2>
            <p class="section-subtitle">{{ t('docs.configDesc') }}</p>
          </div>
          <pre class="code-block"><code>{{ configExample }}</code></pre>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <span class="eyebrow">{{ t('docs.apiEyebrow') }}</span>
          <h2 class="section-title">{{ t('docs.apiTitle') }}</h2>
          <p class="section-subtitle">{{ t('docs.apiDesc') }}</p>
        </div>
        <div class="table">
          <div class="table-row table-head api-table">
            <span>{{ t('docs.method') }}</span>
            <span>{{ t('docs.endpoint') }}</span>
            <span>{{ t('docs.description') }}</span>
          </div>
          <div v-for="row in ta('docs.apiRows')" :key="row.path" class="table-row api-table">
            <span class="api-method mono">{{ row.method }}</span>
            <span class="api-path mono">{{ row.path }}</span>
            <span>{{ row.desc }}</span>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <span class="eyebrow">{{ t('docs.guidesEyebrow') }}</span>
          <h2 class="section-title">{{ t('docs.guidesTitle') }}</h2>
          <p class="section-subtitle">{{ t('docs.guidesDesc') }}</p>
        </div>
        <div class="bento" style="grid-template-columns: repeat(2, 1fr)">
          <a
            v-for="item in ta('docs.guides')"
            :key="item.title"
            :href="item.href"
            target="_blank"
            rel="noreferrer"
            class="bento-cell guide-link"
          >
            <h3>{{ item.title }}</h3>
            <p>{{ item.desc }}</p>
          </a>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.panel-note {
  margin: 0;
  color: var(--text-3);
  font-size: 0.9rem;
}

.source-card {
  transition: border-color 150ms ease;
}

.source-card:hover {
  border-color: var(--border-strong);
}

.source-cta {
  margin-top: 4px;
  color: var(--accent);
  font-size: 0.85rem;
  font-weight: 600;
}

.guide-link {
  transition: border-color 150ms ease, background 150ms ease, transform 150ms ease;
}

.guide-link:hover {
  transform: translateY(-2px);
  border-color: var(--border-strong);
  background: rgba(255, 255, 255, 0.05);
}

.api-table {
  grid-template-columns: 100px 240px 1fr;
}

.api-method {
  color: var(--cyan);
  font-weight: 600;
}

.api-path {
  font-size: 0.8rem;
}

@media (max-width: 768px) {
  .api-table { grid-template-columns: 1fr; }

  .bento[style*="repeat(2"] {
    grid-template-columns: 1fr !important;
  }
}
</style>
