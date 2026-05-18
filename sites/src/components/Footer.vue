<script setup>
import { useI18n } from '../i18n'

const { t } = useI18n()
</script>

<template>
  <footer class="site-footer">
    <div class="container">
      <div class="footer-grid">
        <div class="footer-brand">
          <div class="footer-logo">
            <img src="/hopclaw-mark.svg" alt="HopClaw" />
            <span>HopClaw</span>
          </div>
          <p class="footer-desc">{{ t('footer.blurb') }}</p>
          <p class="footer-license">{{ t('footer.license') }}</p>
        </div>

        <div class="footer-cols">
          <div class="footer-col">
            <h4>{{ t('footer.product') }}</h4>
            <router-link :to="{ path: '/', hash: '#quickstart' }">{{ t('nav.install') }}</router-link>
            <router-link to="/use-cases">{{ t('nav.useCases') }}</router-link>
            <router-link to="/features">{{ t('nav.features') }}</router-link>
            <router-link to="/how-it-works">{{ t('nav.runtime') }}</router-link>
          </div>

          <div class="footer-col">
            <h4>{{ t('footer.resources') }}</h4>
            <router-link to="/docs">{{ t('nav.docs') }}</router-link>
            <a href="https://github.com/fulcrus/hopclaw/blob/main/docs/getting-started/quickstart.md" target="_blank" rel="noreferrer">{{ t('footer.quickstart') }}</a>
            <a href="https://github.com/fulcrus/hopclaw/blob/main/docs/reference/cli.md" target="_blank" rel="noreferrer">{{ t('footer.cli') }}</a>
          </div>

          <div class="footer-col">
            <h4>{{ t('footer.repo') }}</h4>
            <a href="https://github.com/fulcrus/hopclaw" target="_blank" rel="noreferrer">GitHub</a>
            <a href="https://github.com/fulcrus/hopclaw/issues" target="_blank" rel="noreferrer">{{ t('footer.issues') }}</a>
            <a href="https://hopclaw.com/releases/" target="_blank" rel="noreferrer">{{ t('footer.releases') }}</a>
            <a href="https://github.com/fulcrus/hopclaw/issues/new?template=bug_report.yml" target="_blank" rel="noreferrer">{{ t('footer.bugTemplate') }}</a>
            <a href="https://github.com/fulcrus/hopclaw/blob/main/CONTRIBUTING.md" target="_blank" rel="noreferrer">{{ t('footer.contributing') }}</a>
          </div>
        </div>
      </div>

      <div class="footer-bottom">
        <p>{{ t('footer.copyright') }}</p>
      </div>
    </div>
  </footer>
</template>

<style scoped>
.site-footer {
  padding: 44px 0;
  border-top: 1px solid var(--border);
  background: rgba(249, 240, 231, 0.92);
}

.footer-grid {
  display: grid;
  grid-template-columns: 1.15fr 1fr;
  gap: 48px;
}

.footer-brand {
  display: grid;
  gap: 12px;
  align-content: start;
}

.footer-logo {
  display: flex;
  align-items: center;
  gap: 10px;
}

.footer-logo img {
  width: 28px;
  height: 28px;
}

.footer-logo span {
  color: var(--text);
  font-family: var(--font-display);
  font-size: 1.1rem;
  font-weight: 700;
  letter-spacing: -0.03em;
}

.footer-desc {
  margin: 0;
  max-width: 460px;
  color: var(--text-2);
  font-size: 0.9rem;
  line-height: 1.7;
}

.footer-license {
  margin: 0;
  color: var(--text-4);
  font-size: 0.8rem;
}

.footer-cols {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 24px;
}

.footer-col {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.footer-col h4 {
  margin: 0 0 4px;
  color: var(--text-4);
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.footer-col a {
  color: var(--text-3);
  font-size: 0.88rem;
  transition: color 150ms ease;
}

.footer-col a:hover {
  color: var(--text);
}

.footer-bottom {
  padding-top: 24px;
  margin-top: 32px;
  border-top: 1px solid var(--border);
}

.footer-bottom p {
  margin: 0;
  color: var(--text-4);
  font-size: 0.78rem;
}

@media (max-width: 860px) {
  .footer-grid {
    grid-template-columns: 1fr;
    gap: 32px;
  }
}

@media (max-width: 560px) {
  .footer-cols {
    grid-template-columns: 1fr;
  }
}
</style>
