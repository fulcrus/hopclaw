<script setup>
import { computed, ref } from 'vue'
import { useI18n } from '../i18n'

const { t, ta } = useI18n()
const activeInstall = ref('unix')
const copiedKey = ref('')

const installOptions = computed(() => ta('home.installOptions'))
const currentInstall = computed(
  () => installOptions.value.find((item) => item.id === activeInstall.value) || installOptions.value[0],
)

const teamPathCommand = `cd deploy/enterprise
cp .env.example .env
cp config.enterprise.yaml.example config.enterprise.yaml
docker compose -f docker-compose.enterprise.yml up -d`

async function copyText(key, value) {
  try {
    await navigator.clipboard.writeText(value)
    copiedKey.value = key
    window.setTimeout(() => {
      if (copiedKey.value === key) {
        copiedKey.value = ''
      }
    }, 1400)
  } catch {
    copiedKey.value = ''
  }
}
</script>

<template>
  <div class="page-shell home-page">
    <section class="home-hero">
      <div class="container hero-shell">
        <div class="hero-copy">
          <span class="eyebrow">{{ t('home.badge') }}</span>
          <h1 class="headline">
            <span v-for="line in ta('home.titleLines')" :key="line">{{ line }}</span>
          </h1>
          <p class="lede hero-lede">{{ t('home.desc') }}</p>
          <ul class="check-list hero-points">
            <li v-for="point in ta('home.heroPoints')" :key="point">{{ point }}</li>
          </ul>
          <div class="hero-actions">
            <a href="#quickstart" class="btn btn-primary">{{ t('home.primaryCta') }}</a>
            <router-link to="/use-cases" class="btn btn-secondary">{{ t('home.secondaryCta') }}</router-link>
          </div>
        </div>

        <div id="quickstart" class="hero-panel">
          <span class="badge badge-primary">{{ t('home.installBadge') }}</span>
          <h2 class="hero-panel-title">{{ t('home.installTitle') }}</h2>
          <div class="install-tabs" role="tablist">
            <button
              v-for="item in installOptions"
              :key="item.id"
              type="button"
              class="install-tab"
              :class="{ active: item.id === activeInstall }"
              @click="activeInstall = item.id"
            >
              {{ item.label }}
            </button>
          </div>
          <p class="install-desc">{{ currentInstall?.desc }}</p>

          <div class="install-command">
            <pre class="install-code"><code>{{ currentInstall?.command }}</code></pre>
            <button
              type="button"
              class="install-copy"
              :class="{ copied: copiedKey === 'install' }"
              @click="copyText('install', currentInstall?.command || '')"
            >
              {{ copiedKey === 'install' ? t('ui.copied') : t('ui.copy') }}
            </button>
          </div>

          <div class="hero-step-list">
            <article v-for="item in ta('home.installChecks')" :key="item.title" class="hero-step">
              <span class="step-num">{{ item.step }}</span>
              <div class="stack-tight">
                <strong>{{ item.title }}</strong>
                <p>{{ item.desc }}</p>
              </div>
            </article>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head compact-head">
          <span class="eyebrow">{{ t('home.valueEyebrow') }}</span>
          <h2 class="section-title">{{ t('home.valueTitle') }}</h2>
          <p class="section-subtitle">{{ t('home.valueDesc') }}</p>
        </div>
        <div class="grid-3">
          <article v-for="card in ta('home.valueCards')" :key="card.title" class="card callout-card value-card">
            <span class="badge badge-accent">{{ card.badge }}</span>
            <h3>{{ card.title }}</h3>
            <p>{{ card.desc }}</p>
          </article>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head compact-head">
          <span class="eyebrow">{{ t('home.expandEyebrow') }}</span>
          <h2 class="section-title">{{ t('home.expandTitle') }}</h2>
          <p class="section-subtitle">{{ t('home.expandDesc') }}</p>
        </div>

        <div class="grid-2 path-grid">
          <article class="card path-card">
            <span class="badge badge-primary">{{ t('home.personalPath.badge') }}</span>
            <h3>{{ t('home.personalPath.title') }}</h3>
            <p>{{ t('home.personalPath.desc') }}</p>
            <ul class="check-list">
              <li v-for="point in ta('home.personalPath.points')" :key="point">{{ point }}</li>
            </ul>
          </article>

          <article class="card path-card">
            <span class="badge badge-orange">{{ t('home.teamPath.badge') }}</span>
            <h3>{{ t('home.teamPath.title') }}</h3>
            <p>{{ t('home.teamPath.desc') }}</p>
            <div class="code-card">
              <pre class="code-block"><code>{{ teamPathCommand }}</code></pre>
              <button
                type="button"
                class="install-copy inline-copy"
                :class="{ copied: copiedKey === 'team-path' }"
                @click="copyText('team-path', teamPathCommand)"
              >
                {{ copiedKey === 'team-path' ? t('ui.copied') : t('ui.copy') }}
              </button>
            </div>
            <ul class="check-list">
              <li v-for="point in ta('home.teamPath.points')" :key="point">{{ point }}</li>
            </ul>
          </article>
        </div>

        <div class="grid-3 trust-grid">
          <article v-for="card in ta('home.enterpriseCards')" :key="card.title" class="card callout-card trust-card">
            <span class="badge badge-accent">{{ card.badge }}</span>
            <h3>{{ card.title }}</h3>
            <p>{{ card.desc }}</p>
          </article>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="cta-box">
          <span class="eyebrow">{{ t('home.ctaEyebrow') }}</span>
          <h2 class="section-title">{{ t('home.ctaTitle') }}</h2>
          <p class="section-subtitle cta-subtitle">{{ t('home.ctaDesc') }}</p>
          <div class="hero-actions">
            <a href="#quickstart" class="btn btn-primary">{{ t('home.ctaPrimary') }}</a>
            <a href="https://github.com/fulcrus/hopclaw" target="_blank" rel="noreferrer" class="btn btn-ghost">
              {{ t('home.ctaSecondary') }}
            </a>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.home-hero {
  position: relative;
  padding: 110px 0 36px;
}

.home-hero::before {
  content: '';
  position: absolute;
  inset: 24px auto auto 50%;
  width: min(960px, 88vw);
  height: 420px;
  transform: translateX(-50%);
  border-radius: 999px;
  background: radial-gradient(circle at 50% 20%, rgba(197, 118, 68, 0.18), transparent 58%);
  filter: blur(18px);
  pointer-events: none;
  z-index: -1;
}

.hero-shell {
  display: grid;
  grid-template-columns: minmax(0, 1.1fr) minmax(320px, 0.9fr);
  gap: 40px;
  align-items: center;
}

.hero-copy {
  display: grid;
  gap: 20px;
}

.headline span {
  display: block;
}

.hero-lede {
  max-width: 620px;
}

.hero-points {
  max-width: 620px;
}

.hero-panel {
  display: grid;
  gap: 18px;
  padding: 28px;
  border: 1px solid var(--border);
  border-radius: 24px;
  background: rgba(255, 250, 244, 0.92);
  box-shadow: 0 24px 60px rgba(92, 61, 39, 0.12);
}

.hero-panel-title {
  margin: 0;
  color: var(--text);
  font-size: 1.25rem;
}

.install-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.install-tab {
  height: 34px;
  padding: 0 14px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: transparent;
  color: var(--text-3);
  font-size: 0.82rem;
  font-weight: 600;
  cursor: pointer;
  transition: border-color 150ms ease, color 150ms ease, background 150ms ease;
}

.install-tab:hover {
  color: var(--text);
  border-color: var(--border-strong);
}

.install-tab.active {
  background: var(--accent-dim);
  border-color: rgba(181, 96, 53, 0.2);
  color: var(--accent);
}

.install-desc {
  margin: 0;
  color: var(--text-2);
  font-size: 0.95rem;
}

.install-command,
.code-card {
  position: relative;
}

.install-code {
  margin: 0;
  padding: 18px 104px 18px 18px;
  border: 0;
  border-radius: 16px;
  background: #2b211b;
  color: #f8efe6;
  font-size: 0.84rem;
  line-height: 1.75;
  white-space: pre-wrap;
  word-break: break-word;
  overflow-x: auto;
}

.install-copy {
  position: absolute;
  top: 12px;
  right: 12px;
  height: 30px;
  padding: 0 12px;
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.06);
  color: #f8efe6;
  font-size: 0.78rem;
  font-weight: 600;
  cursor: pointer;
  transition: border-color 150ms ease, color 150ms ease, background 150ms ease;
}

.install-copy:hover {
  border-color: rgba(255, 255, 255, 0.28);
  background: rgba(255, 255, 255, 0.12);
}

.install-copy.copied {
  color: #f7f1e8;
  border-color: rgba(120, 178, 110, 0.55);
  background: rgba(120, 178, 110, 0.25);
}

.hero-step-list {
  display: grid;
  gap: 12px;
}

.hero-step {
  display: grid;
  grid-template-columns: 34px 1fr;
  gap: 12px;
  align-items: start;
}

.step-num {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  background: var(--accent-dim);
  color: var(--accent);
  font-family: var(--font-mono);
  font-size: 0.78rem;
  font-weight: 600;
}

.hero-step strong {
  color: var(--text);
  font-size: 0.96rem;
}

.hero-step p {
  margin: 0;
  color: var(--text-2);
  font-size: 0.88rem;
}

.compact-head {
  margin-bottom: 28px;
}

.value-card,
.trust-card,
.path-card {
  min-height: 220px;
}

.path-grid {
  gap: 16px;
  margin-bottom: 16px;
}

.path-card h3 {
  margin: 0;
  color: var(--text);
  font-size: 1.15rem;
}

.path-card p {
  margin: 0;
  color: var(--text-2);
}

.inline-copy {
  top: 14px;
}

.trust-grid {
  margin-top: 16px;
}

.cta-subtitle {
  max-width: 700px;
  text-align: center;
}

@media (max-width: 1024px) {
  .hero-shell {
    grid-template-columns: 1fr;
    gap: 28px;
  }
}

@media (max-width: 768px) {
  .home-hero {
    padding-top: 96px;
  }

  .hero-panel {
    padding: 22px;
  }
}

@media (max-width: 480px) {
  .install-code {
    padding-right: 18px;
  }

  .install-copy,
  .inline-copy {
    position: static;
    width: fit-content;
    color: var(--text);
    border-color: var(--border);
    background: var(--bg-subtle);
  }

  .install-command,
  .code-card {
    display: grid;
    gap: 12px;
  }
}
</style>
