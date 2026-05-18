<script setup>
import TerminalBlock from '../components/TerminalBlock.vue'
import { useI18n } from '../i18n'

const { t, ta } = useI18n()

const ensureTerminal = [
  [{ text: '[run] missing skill -> github-issues', cls: 't-warn' }],
  [{ text: '> skill.ensure("github-issues")', cls: 't-cmd' }],
  [{ text: '[policy] install_policy=ask', cls: 't-out' }],
  [{ text: '[approval] install requested', cls: 't-warn' }],
  [{ text: '[resume] skill loaded from disk bundle', cls: 't-ok' }],
]

const compatibilitySnippet = `skills:
  auto_detect: true
  auto_refresh: true
  install_policy: ask
  dirs:
    - ./skills
    - ~/.openclaw/skills
    - ~/.openclaw/workspace/skills

# keep existing skill roots
# migrate without repackaging
# pick changes up without restarting`
</script>

<template>
  <div class="page-shell clawhub-page">
    <section class="page-intro">
      <div class="container">
        <div class="hero-grid">
          <div class="stack">
            <span class="eyebrow">{{ t('clawHub.badge') }}</span>
            <h1 class="headline">{{ t('clawHub.title') }}</h1>
            <p class="lede">{{ t('clawHub.desc') }}</p>
            <ul class="check-list">
              <li v-for="point in ta('clawHub.heroPoints')" :key="point">{{ point }}</li>
            </ul>
          </div>
          <div class="stack hero-side">
            <pre class="code-block"><code>{{ compatibilitySnippet }}</code></pre>
            <TerminalBlock title="migration flow" :lines="ensureTerminal" />
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <span class="eyebrow">{{ t('clawHub.pillarsEyebrow') }}</span>
          <h2 class="section-title">{{ t('clawHub.pillarsTitle') }}</h2>
          <p class="section-subtitle">{{ t('clawHub.pillarsDesc') }}</p>
        </div>
        <div class="bento" style="grid-template-columns: repeat(3, 1fr)">
          <article v-for="item in ta('clawHub.pillars')" :key="item.title" class="bento-cell">
            <h3>{{ item.title }}</h3>
            <p>{{ item.desc }}</p>
          </article>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <span class="eyebrow">{{ t('clawHub.groupsEyebrow') }}</span>
          <h2 class="section-title">{{ t('clawHub.groupsTitle') }}</h2>
          <p class="section-subtitle">{{ t('clawHub.groupsDesc') }}</p>
        </div>
        <div class="bento" style="grid-template-columns: repeat(2, 1fr)">
          <article v-for="item in ta('clawHub.groups')" :key="item.title" class="bento-cell">
            <h3>{{ item.title }}</h3>
            <p>{{ item.desc }}</p>
          </article>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container split">
        <div>
          <div class="section-head">
            <span class="eyebrow">{{ t('clawHub.policyEyebrow') }}</span>
            <h2 class="section-title">{{ t('clawHub.policyTitle') }}</h2>
            <p class="section-subtitle">{{ t('clawHub.policyDesc') }}</p>
          </div>
          <div class="grid-3">
            <article v-for="item in ta('clawHub.policies')" :key="item.title" class="card callout-card">
              <span class="badge badge-accent mono">{{ item.title }}</span>
              <p>{{ item.desc }}</p>
            </article>
          </div>
        </div>

        <div>
          <div class="section-head">
            <span class="eyebrow">{{ t('clawHub.authorEyebrow') }}</span>
            <h2 class="section-title">{{ t('clawHub.authorTitle') }}</h2>
            <p class="section-subtitle">{{ t('clawHub.authorDesc') }}</p>
          </div>
          <div class="author-steps">
            <article v-for="(step, index) in ta('clawHub.authorSteps')" :key="step.title" class="card author-step">
              <div class="author-index">0{{ index + 1 }}</div>
              <div class="stack-tight">
                <h3>{{ step.title }}</h3>
                <p>{{ step.desc }}</p>
              </div>
            </article>
          </div>
          <p class="note">{{ t('clawHub.note') }}</p>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.author-steps {
  display: grid;
  gap: 12px;
}

.author-step {
  display: grid;
  grid-template-columns: 48px 1fr;
  gap: 14px;
  align-items: start;
}

.author-step h3 {
  margin: 0;
  font-size: 1.05rem;
  color: var(--text);
}

.author-step p {
  margin: 0;
  color: var(--text-2);
  font-size: 0.92rem;
}

.author-index {
  display: grid;
  place-items: center;
  width: 48px;
  height: 48px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.02);
  color: var(--text);
  font-family: var(--font-display);
  font-size: 0.88rem;
  font-weight: 700;
}

.note {
  margin: 16px 0 0;
  color: var(--text-3);
  font-size: 0.88rem;
}

@media (max-width: 768px) {
  .author-step { grid-template-columns: 1fr; }

  .bento[style*="repeat(3"],
  .bento[style*="repeat(2"] {
    grid-template-columns: 1fr !important;
  }
}
</style>
