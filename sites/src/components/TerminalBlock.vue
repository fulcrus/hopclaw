<script setup>
defineProps({
  title: { type: String, default: 'terminal' },
  lines: { type: Array, required: true },
})
</script>

<template>
  <div class="terminal">
    <div class="terminal-header">
      <span class="terminal-dot red"></span>
      <span class="terminal-dot yellow"></span>
      <span class="terminal-dot green"></span>
      <span class="terminal-title">{{ title }}</span>
    </div>
    <div class="terminal-body">
      <div v-for="(line, lineIndex) in lines" :key="lineIndex" class="terminal-line">
        <span v-for="(segment, segmentIndex) in line" :key="segmentIndex" :class="segment.cls">{{ segment.text }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.terminal-title {
  margin-left: auto;
  color: var(--text-3);
  font-size: 0.72rem;
  font-family: var(--font-mono);
}
</style>
