<script setup>
import { useI18n } from '../i18n'

const { t, ta } = useI18n()
</script>

<template>
  <div class="page-shell features-page">
    <section class="page-intro">
      <div class="container">
        <div class="hero-grid">
          <div class="stack">
            <span class="eyebrow">{{ t('features.badge') }}</span>
            <h1 class="headline">{{ t('features.title') }}</h1>
            <p class="lede">{{ t('features.desc') }}</p>
          </div>
          <div class="context-panel">
            <span class="badge badge-accent">{{ t('features.contextEyebrow') }}</span>
            <p class="panel-desc">{{ t('features.contextDesc') }}</p>
            <div class="context-bands">
              <div
                v-for="(band, index) in ta('features.contextBands')"
                :key="band"
                class="context-band"
                :class="'tone-' + index"
              >
                {{ band }}
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <span class="eyebrow">{{ t('features.pillarsEyebrow') }}</span>
          <h2 class="section-title">{{ t('features.pillarsTitle') }}</h2>
          <p class="section-subtitle">{{ t('features.pillarsDesc') }}</p>
        </div>
        <div class="bento" style="grid-template-columns: repeat(4, 1fr)">
          <article v-for="item in ta('features.pillars')" :key="item.title" class="bento-cell">
            <h3>{{ item.title }}</h3>
            <p>{{ item.desc }}</p>
          </article>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <span class="eyebrow">{{ t('features.toolsEyebrow') }}</span>
          <h2 class="section-title">{{ t('features.toolsTitle') }}</h2>
          <p class="section-subtitle">{{ t('features.toolsDesc') }}</p>
        </div>
        <div class="bento" style="grid-template-columns: repeat(3, 1fr)">
          <article v-for="item in ta('features.toolFamilies')" :key="item.title" class="bento-cell">
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
            <span class="eyebrow">{{ t('features.securityEyebrow') }}</span>
            <h2 class="section-title">{{ t('features.securityTitle') }}</h2>
            <p class="section-subtitle">{{ t('features.securityDesc') }}</p>
          </div>
          <div class="grid-2">
            <article v-for="item in ta('features.securityCards')" :key="item.title" class="card callout-card">
              <h3>{{ item.title }}</h3>
              <p>{{ item.desc }}</p>
            </article>
          </div>
        </div>

        <div>
          <div class="section-head">
            <span class="eyebrow">{{ t('features.contextEyebrow') }}</span>
            <h2 class="section-title">{{ t('features.contextTitle') }}</h2>
            <p class="section-subtitle">{{ t('features.contextDesc') }}</p>
          </div>
          <ul class="check-list">
            <li v-for="note in ta('features.contextNotes')" :key="note">{{ note }}</li>
          </ul>
          <div class="context-bands compact">
            <div
              v-for="(band, index) in ta('features.contextBands')"
              :key="band"
              class="context-band"
              :class="'tone-' + index"
            >
              {{ band }}
            </div>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <span class="eyebrow">{{ t('features.surfaceEyebrow') }}</span>
          <h2 class="section-title">{{ t('features.surfaceTitle') }}</h2>
          <p class="section-subtitle">{{ t('features.surfaceDesc') }}</p>
        </div>
        <div class="table">
          <div class="table-row table-head surface-table">
            <span>{{ t('features.area') }}</span>
            <span>{{ t('features.surface') }}</span>
            <span>{{ t('features.whenToUse') }}</span>
          </div>
          <div v-for="row in ta('features.surfaceRows')" :key="row.path" class="table-row surface-table">
            <span class="surface-area">{{ row.area }}</span>
            <span class="mono surface-path">{{ row.path }}</span>
            <span>{{ row.desc }}</span>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.context-panel {
  display: grid;
  gap: 12px;
  align-content: start;
  padding: 24px;
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  background: var(--bg-subtle);
}

.panel-desc {
  margin: 0;
  color: var(--text-2);
  font-size: 0.92rem;
}

.context-bands {
  display: grid;
  gap: 6px;
}

.context-bands.compact {
  margin-top: 20px;
}

.context-band {
  padding: 10px 14px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  font-family: var(--font-mono);
  font-size: 0.78rem;
  color: var(--text-2);
}

.tone-0 { background: rgba(255, 255, 255, 0.02); }
.tone-1 { background: rgba(103, 232, 249, 0.04); }
.tone-2 { background: rgba(251, 191, 36, 0.04); }
.tone-3 { background: rgba(255, 122, 69, 0.04); }
.tone-4 { background: rgba(255, 255, 255, 0.04); }

.surface-table {
  grid-template-columns: 120px 240px 1fr;
}

.surface-area {
  font-weight: 600;
  color: var(--text);
}

.surface-path {
  font-size: 0.8rem;
  color: var(--cyan);
}

@media (max-width: 768px) {
  .surface-table { grid-template-columns: 1fr; }

  .bento[style*="repeat(4"] {
    grid-template-columns: 1fr !important;
  }

  .bento[style*="repeat(3"] {
    grid-template-columns: 1fr !important;
  }
}
</style>
