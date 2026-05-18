<script setup>
import TerminalBlock from '../components/TerminalBlock.vue'
import { useI18n } from '../i18n'

const { t, ta } = useI18n()

const bootSequence = [
  [{ text: '$ ', cls: 't-prompt' }, { text: 'hopclaw serve --config ./local.yaml', cls: 't-cmd' }],
  [{ text: '[boot] runtime profile -> desktop', cls: 't-ok' }],
  [{ text: '[boot] register built-ins and layer2 groups', cls: 't-ok' }],
  [{ text: '[host] Browser Helper -> unavailable', cls: 't-out' }],
  [{ text: '[host] Desktop Helper -> unavailable', cls: 't-out' }],
  [{ text: '[api]  /runtime/runs /runtime/tools /runtime/approvals', cls: 't-hl' }],
  [{ text: '[ui]   operator console served from /dashboard/', cls: 't-hl' }],
  [{ text: '[ready] hopclaw listening on 127.0.0.1:16280', cls: 't-ok' }],
]
</script>

<template>
  <div class="page-shell runtime-page">
    <section class="page-intro">
      <div class="container">
        <div class="hero-grid">
          <div class="stack">
            <span class="eyebrow">{{ t('runtime.badge') }}</span>
            <h1 class="headline">{{ t('runtime.title') }}</h1>
            <p class="lede">{{ t('runtime.desc') }}</p>
            <ul class="check-list">
              <li v-for="point in ta('runtime.bootPoints')" :key="point">{{ point }}</li>
            </ul>
          </div>
          <TerminalBlock title="service boot" :lines="bootSequence" />
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <span class="eyebrow">{{ t('runtime.mapEyebrow') }}</span>
          <h2 class="section-title">{{ t('runtime.mapTitle') }}</h2>
          <p class="section-subtitle">{{ t('runtime.mapDesc') }}</p>
        </div>
        <div class="bento" style="grid-template-columns: repeat(4, 1fr)">
          <article v-for="card in ta('runtime.mapCards')" :key="card.title" class="bento-cell">
            <h3>{{ card.title }}</h3>
            <p>{{ card.desc }}</p>
          </article>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <span class="eyebrow">{{ t('runtime.flowEyebrow') }}</span>
          <h2 class="section-title">{{ t('runtime.flowTitle') }}</h2>
          <p class="section-subtitle">{{ t('runtime.flowDesc') }}</p>
        </div>
        <div class="flow-list">
          <article v-for="item in ta('runtime.flowSteps')" :key="item.step" class="flow-item card">
            <div class="flow-step">{{ item.step }}</div>
            <div class="stack-tight">
              <h3>{{ item.title }}</h3>
              <p>{{ item.desc }}</p>
            </div>
          </article>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container split">
        <div>
          <div class="section-head">
            <span class="eyebrow">{{ t('runtime.profilesEyebrow') }}</span>
            <h2 class="section-title">{{ t('runtime.profilesTitle') }}</h2>
            <p class="section-subtitle">{{ t('runtime.profilesDesc') }}</p>
          </div>
          <div class="grid-3">
            <article v-for="profile in ta('runtime.profiles')" :key="profile.name" class="card callout-card">
              <span class="badge badge-accent mono">{{ profile.name }}</span>
              <p>{{ profile.desc }}</p>
            </article>
          </div>
        </div>

        <div>
          <div class="section-head">
            <span class="eyebrow">{{ t('runtime.boundaryEyebrow') }}</span>
            <h2 class="section-title">{{ t('runtime.boundaryTitle') }}</h2>
            <p class="section-subtitle">{{ t('runtime.boundaryDesc') }}</p>
          </div>
          <div class="grid-2">
            <article class="card callout-card">
              <span class="badge badge-green">{{ t('runtime.shippedTitle') }}</span>
              <ul class="check-list">
                <li v-for="item in ta('runtime.shipped')" :key="item">{{ item }}</li>
              </ul>
            </article>
            <article class="card callout-card">
              <span class="badge badge-orange">{{ t('runtime.notShippedTitle') }}</span>
              <ul class="check-list">
                <li v-for="item in ta('runtime.notShipped')" :key="item">{{ item }}</li>
              </ul>
            </article>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.flow-list {
  display: grid;
  gap: 12px;
}

.flow-item {
  display: grid;
  grid-template-columns: 64px 1fr;
  gap: 16px;
  align-items: start;
}

.flow-item h3 {
  margin: 0;
  font-size: 1.05rem;
  color: var(--text);
}

.flow-item p {
  margin: 0;
  color: var(--text-2);
  font-size: 0.92rem;
}

.flow-step {
  display: grid;
  place-items: center;
  width: 64px;
  height: 64px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.02);
  color: var(--text);
  font-family: var(--font-display);
  font-size: 1rem;
  font-weight: 700;
}

@media (max-width: 768px) {
  .flow-item {
    grid-template-columns: 1fr;
  }

  .bento[style*="repeat(4"] {
    grid-template-columns: 1fr !important;
  }
}
</style>
