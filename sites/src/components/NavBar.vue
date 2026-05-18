<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from '../i18n'

const route = useRoute()
const { t, locale, setLocale, localeOptions } = useI18n()

const menuOpen = ref(false)
const langOpen = ref(false)

const navLinks = [
  { to: '/use-cases', key: 'nav.useCases' },
  { to: '/features', key: 'nav.features' },
  { to: '/how-it-works', key: 'nav.runtime' },
  { to: '/docs', key: 'nav.docs' },
]

const currentLocale = computed(() => localeOptions.find((option) => option.code === locale.value) || localeOptions[0])

function switchLang(code) {
  setLocale(code)
  langOpen.value = false
}

function closePanels() {
  menuOpen.value = false
  langOpen.value = false
}

function onDocumentClick(event) {
  const target = event.target
  if (!(target instanceof Element)) return
  if (!target.closest('.nav-bar')) {
    langOpen.value = false
    menuOpen.value = false
  }
}

function onDocumentKeydown(event) {
  if (event.key === 'Escape' && (langOpen.value || menuOpen.value)) {
    closePanels()
  }
}

watch(
  () => route.fullPath,
  () => {
    closePanels()
  },
)

onMounted(() => {
  document.addEventListener('click', onDocumentClick)
  document.addEventListener('keydown', onDocumentKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocumentClick)
  document.removeEventListener('keydown', onDocumentKeydown)
})
</script>

<template>
  <header class="site-header">
    <div class="container">
      <nav class="nav-bar" :class="{ open: menuOpen }">
        <router-link to="/" class="brand" @click="closePanels">
          <img src="/hopclaw-mark.svg" alt="" class="brand-mark" />
          <div class="brand-copy">
            <span class="brand-name">HopClaw</span>
            <span class="brand-tagline">{{ t('nav.tagline') }}</span>
          </div>
        </router-link>

        <div class="nav-links" :class="{ open: menuOpen }">
          <router-link
            v-for="link in navLinks"
            :key="link.to"
            :to="link.to"
            class="nav-link"
            @click="closePanels"
          >
            {{ t(link.key) }}
          </router-link>
        </div>

        <div class="nav-end">
          <div class="lang-switcher">
            <button
              type="button"
              class="lang-btn"
              aria-haspopup="listbox"
              aria-controls="site-lang-menu"
              :aria-expanded="langOpen"
              @click.stop="langOpen = !langOpen"
            >
              {{ currentLocale.label }}
              <span class="lang-arrow" :class="{ open: langOpen }">&#x25BE;</span>
            </button>
            <div v-if="langOpen" id="site-lang-menu" role="listbox" class="lang-menu">
              <button
                v-for="option in localeOptions"
                :key="option.code"
                type="button"
                class="lang-option"
                :class="{ active: option.code === locale }"
                @click="switchLang(option.code)"
              >
                {{ option.label }}
              </button>
            </div>
          </div>

          <a href="https://github.com/fulcrus/hopclaw" target="_blank" rel="noopener" class="nav-github">
            {{ t('nav.github') }}
          </a>

          <router-link :to="{ path: '/', hash: '#quickstart' }" class="btn btn-primary nav-install" @click="closePanels">
            {{ t('nav.install') }}
          </router-link>

          <button type="button" class="menu-toggle" :aria-expanded="menuOpen" @click="menuOpen = !menuOpen">
            <span></span>
            <span></span>
          </button>
        </div>
      </nav>
    </div>
  </header>
</template>

<style scoped>
.site-header {
  position: sticky;
  top: 0;
  z-index: 1000;
  padding: 14px 0;
  background: rgba(245, 237, 226, 0.88);
  backdrop-filter: blur(14px);
  border-bottom: 1px solid var(--border);
}

.nav-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 48px;
}

.brand {
  display: inline-flex;
  align-items: center;
  gap: 12px;
  margin-right: 20px;
}

.brand-mark {
  width: 30px;
  height: 30px;
}

.brand-copy {
  display: grid;
  gap: 2px;
}

.brand-name {
  color: var(--text);
  font-family: var(--font-display);
  font-size: 1.08rem;
  font-weight: 700;
  letter-spacing: -0.03em;
}

.brand-tagline {
  color: var(--text-4);
  font-size: 0.72rem;
  line-height: 1.2;
}

.nav-links {
  display: flex;
  align-items: center;
  gap: 4px;
}

.nav-link {
  padding: 7px 12px;
  border-radius: var(--r-sm);
  color: var(--text-3);
  font-size: 0.88rem;
  font-weight: 600;
  transition: color 150ms ease, background 150ms ease;
}

.nav-link:hover {
  color: var(--text);
}

.nav-link.router-link-active {
  color: var(--text);
  background: rgba(181, 96, 53, 0.08);
}

.nav-end {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
}

.lang-switcher {
  position: relative;
}

.lang-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 34px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-3);
  font-size: 0.8rem;
  font-weight: 600;
  cursor: pointer;
  transition: color 150ms ease, border-color 150ms ease;
}

.lang-btn:hover {
  color: var(--text);
  border-color: var(--border-strong);
}

.lang-arrow {
  font-size: 0.6rem;
  transition: transform 150ms ease;
}

.lang-arrow.open {
  transform: rotate(180deg);
}

.lang-menu {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  min-width: 100px;
  padding: 4px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: rgba(255, 250, 244, 0.98);
  box-shadow: 0 12px 32px rgba(91, 63, 43, 0.12);
}

.lang-option {
  display: block;
  width: 100%;
  padding: 8px 10px;
  border: 0;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-3);
  font-size: 0.82rem;
  text-align: left;
  cursor: pointer;
  transition: color 150ms ease, background 150ms ease;
}

.lang-option:hover,
.lang-option.active {
  background: rgba(181, 96, 53, 0.08);
  color: var(--text);
}

.nav-github {
  display: inline-flex;
  align-items: center;
  height: 34px;
  padding: 0 12px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--text-2);
  font-size: 0.82rem;
  font-weight: 600;
  transition: color 150ms ease, border-color 150ms ease, background 150ms ease;
}

.nav-github:hover {
  color: var(--text);
  border-color: var(--border-strong);
  background: rgba(255, 250, 244, 0.92);
}

.nav-install {
  min-height: 34px;
  padding: 0 14px;
  font-size: 0.82rem;
}

.menu-toggle {
  display: none;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  width: 38px;
  height: 38px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: transparent;
  cursor: pointer;
}

.menu-toggle span {
  display: block;
  width: 16px;
  height: 2px;
  margin: 0 auto;
  background: var(--text);
}

@media (max-width: 980px) {
  .brand-tagline {
    display: none;
  }
}

@media (max-width: 860px) {
  .menu-toggle {
    display: inline-flex;
  }

  .nav-links {
    position: absolute;
    top: calc(100% + 12px);
    left: 24px;
    right: 24px;
    display: none;
    flex-direction: column;
    align-items: stretch;
    padding: 10px;
    border: 1px solid var(--border);
    border-radius: var(--r-lg);
    background: rgba(255, 250, 244, 0.98);
    box-shadow: 0 16px 40px rgba(91, 63, 43, 0.12);
  }

  .nav-links.open {
    display: flex;
  }

  .nav-link {
    padding: 10px 12px;
  }
}

@media (max-width: 640px) {
  .nav-github {
    display: none;
  }

  .nav-install {
    display: none;
  }
}
</style>
