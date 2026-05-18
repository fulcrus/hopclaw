<script setup>
import TerminalBlock from '../components/TerminalBlock.vue'
import { useI18n } from '../i18n'

const { t, ta } = useI18n()

const terminals = [
  [
    [{ text: '[desktop] open Browser and Notes', cls: 't-out' }],
    [{ text: '> browser.navigate("https://calendar.google.com")', cls: 't-hl' }],
    [{ text: '> desktop.screenshot()', cls: 't-hl' }],
    [{ text: '> desktop.clipboard_write(summary)', cls: 't-hl' }],
    [{ text: '[done] morning brief prepared', cls: 't-ok' }],
  ],
  [
    [{ text: '[slack] /deploy api to production', cls: 't-out' }],
    [{ text: '> git.status()', cls: 't-hl' }],
    [{ text: '> exec.run("kubectl set image ...")', cls: 't-hl' }],
    [{ text: '[approval] waiting for operator', cls: 't-warn' }],
    [{ text: '[resume] rollout continues', cls: 't-ok' }],
  ],
  [
    [{ text: 'POST /runtime/runs', cls: 't-cmd' }],
    [{ text: '{"session_key":"daily-report","content":"summarize new issues"}', cls: 't-out' }],
    [{ text: '202 Accepted', cls: 't-ok' }],
    [{ text: 'GET /runtime/runs/7d1a', cls: 't-cmd' }],
    [{ text: '[status] completed, artifact_ref=artifact://local/9b4', cls: 't-hl' }],
  ],
  [
    [{ text: '[run] missing capability: github.issue.search', cls: 't-warn' }],
    [{ text: '> skill.ensure("github-issues")', cls: 't-cmd' }],
    [{ text: '[policy] install_policy=ask', cls: 't-out' }],
    [{ text: '[approval] skill install requested', cls: 't-warn' }],
    [{ text: '[resume] skill available, continue', cls: 't-ok' }],
  ],
]
</script>

<template>
  <div class="page-shell use-cases-page">
    <section class="page-intro">
      <div class="container">
        <div class="hero-grid">
          <div class="stack">
            <span class="eyebrow">{{ t('useCases.badge') }}</span>
            <h1 class="headline">{{ t('useCases.title') }}</h1>
            <p class="lede">{{ t('useCases.desc') }}</p>
          </div>
          <TerminalBlock title="scenario 01" :lines="terminals[0]" />
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="cases-list">
          <div
            v-for="(item, index) in ta('useCases.cases')"
            :key="item.title"
            class="case-row"
            :class="{ reverse: index % 2 === 1 }"
          >
            <div class="case-copy card">
              <div class="case-topline">
                <span class="case-index">0{{ index + 1 }}</span>
                <span class="eyebrow">{{ item.eyebrow }}</span>
              </div>
              <h2 class="case-title">{{ item.title }}</h2>
              <p class="case-desc">{{ item.desc }}</p>
              <ul class="check-list">
                <li v-for="point in item.outcomes" :key="point">{{ point }}</li>
              </ul>
            </div>
            <TerminalBlock :title="`scenario 0${index + 1}`" :lines="terminals[index]" />
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <span class="eyebrow">{{ t('useCases.modesEyebrow') }}</span>
          <h2 class="section-title">{{ t('useCases.modesTitle') }}</h2>
          <p class="section-subtitle">{{ t('useCases.modesDesc') }}</p>
        </div>
        <div class="bento" style="grid-template-columns: repeat(3, 1fr)">
          <article v-for="mode in ta('useCases.modes')" :key="mode.title" class="bento-cell">
            <h3>{{ mode.title }}</h3>
            <p>{{ mode.desc }}</p>
          </article>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.cases-list {
  display: grid;
  gap: 24px;
}

.case-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  align-items: stretch;
}

.case-row.reverse {
  direction: rtl;
}

.case-row.reverse > * {
  direction: ltr;
}

.case-copy {
  display: grid;
  gap: 14px;
  align-content: start;
}

.case-topline {
  display: flex;
  align-items: center;
  gap: 10px;
}

.case-index {
  display: inline-grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.02);
  color: var(--text);
  font-family: var(--font-display);
  font-size: 0.82rem;
  font-weight: 700;
}

.case-title {
  margin: 0;
  font-family: var(--font-display);
  font-size: clamp(1.4rem, 2.5vw, 2rem);
  font-weight: 700;
  color: var(--text);
  letter-spacing: -0.02em;
  line-height: 1.15;
}

.case-desc {
  margin: 0;
  color: var(--text-2);
}

@media (max-width: 860px) {
  .case-row,
  .case-row.reverse {
    direction: ltr;
    grid-template-columns: 1fr;
  }

  .bento[style*="repeat(3"] {
    grid-template-columns: 1fr !important;
  }
}
</style>
