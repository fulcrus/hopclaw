<script setup>
import { computed } from 'vue'
import { useI18n } from '../i18n'

const { ta, t } = useI18n()

const cards = computed(() => ta('layers.cards'))
</script>

<template>
  <div class="layer-board">
    <div class="layer-board-head">
      <span class="badge badge-accent">{{ t('layers.badge') }}</span>
      <p class="mono">{{ t('layers.flowProbe') }}</p>
      <p class="mono">{{ t('layers.flowSkill') }}</p>
    </div>

    <div class="layer-stack">
      <article v-for="(card, index) in cards" :key="card.title" class="layer-row" :class="card.tone">
        <div class="layer-index">0{{ index + 1 }}</div>
        <div class="layer-copy">
          <div class="layer-title">
            <span class="layer-badge">{{ card.badge }}</span>
            <h3>{{ card.title }}</h3>
          </div>
          <p>{{ card.desc }}</p>
          <div class="layer-tags">
            <span v-for="tag in card.tags" :key="tag">{{ tag }}</span>
          </div>
        </div>
      </article>
    </div>
  </div>
</template>

<style scoped>
.layer-board {
  display: grid;
  gap: 16px;
  padding: 24px;
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  background: var(--bg-subtle);
}

.layer-board-head {
  display: grid;
  gap: 8px;
}

.layer-board-head p {
  margin: 0;
  color: var(--text-3);
  font-size: 0.82rem;
}

.layer-stack {
  display: grid;
  gap: 8px;
}

.layer-row {
  display: grid;
  grid-template-columns: 48px 1fr;
  gap: 14px;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: rgba(255, 255, 255, 0.01);
}

.layer-row.runtime { border-left: 3px solid var(--cyan); }
.layer-row.system { border-left: 3px solid var(--accent); }
.layer-row.skills { border-left: 3px solid var(--yellow); }

.layer-index {
  display: grid;
  place-items: center;
  width: 48px;
  height: 48px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  color: var(--text);
  font-family: var(--font-mono);
  font-size: 0.82rem;
}

.layer-copy {
  display: grid;
  gap: 8px;
}

.layer-title {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.layer-badge {
  display: inline-flex;
  padding: 3px 8px;
  border-radius: var(--r-sm);
  background: rgba(255, 255, 255, 0.04);
  color: var(--text-3);
  font-size: 0.7rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.layer-copy h3 {
  margin: 0;
  font-size: 1rem;
  color: var(--text);
}

.layer-copy p {
  margin: 0;
  color: var(--text-2);
  font-size: 0.88rem;
}

.layer-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.layer-tags span {
  display: inline-flex;
  padding: 4px 8px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--text-3);
  font-size: 0.72rem;
  font-family: var(--font-mono);
}

@media (max-width: 640px) {
  .layer-row { grid-template-columns: 1fr; }
}
</style>
