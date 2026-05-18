<script setup>
import { ref } from 'vue'
import { useI18n } from '../i18n'

const props = defineProps({
  eyebrow: { type: String, required: true },
  title: { type: String, required: true },
  command: { type: String, required: true },
  note: { type: String, required: true },
})

const { t } = useI18n()
const copied = ref(false)

async function copyCommand() {
  try {
    await navigator.clipboard.writeText(props.command)
    copied.value = true
    window.setTimeout(() => {
      copied.value = false
    }, 1400)
  } catch {
    copied.value = false
  }
}
</script>

<template>
  <article class="card command-card">
    <div class="command-topline">
      <span class="command-eyebrow">{{ eyebrow }}</span>
      <button type="button" class="command-copy" :class="{ copied }" @click="copyCommand">
        {{ copied ? t('ui.copied') : t('ui.copy') }}
      </button>
    </div>
    <div class="stack-tight">
      <h3>{{ title }}</h3>
      <p>{{ note }}</p>
    </div>
    <pre class="code-block"><code>{{ command }}</code></pre>
  </article>
</template>

<style scoped>
.command-card {
  display: grid;
  gap: 14px;
}

.command-topline {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.command-eyebrow {
  display: inline-flex;
  padding: 4px 8px;
  border-radius: var(--r-sm);
  background: rgba(255, 255, 255, 0.04);
  color: var(--text-3);
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.command-card h3 {
  margin: 0;
  font-size: 1.05rem;
  color: var(--text);
}

.command-card p {
  margin: 0;
  color: var(--text-2);
}

.command-copy {
  display: inline-flex;
  align-items: center;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-3);
  font-size: 0.78rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 150ms ease;
}

.command-copy:hover {
  color: var(--text);
  border-color: var(--border-strong);
}

.command-copy.copied {
  color: var(--green);
  border-color: rgba(74, 222, 128, 0.2);
}
</style>
