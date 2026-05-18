import { ref, watch } from 'vue'
import en from './en.js'
import zh from './zh.js'
import ja from './ja.js'
import es from './es.js'

const messages = { en, zh, ja, es }
const STORAGE_KEY = 'hopclaw-lang'

function hasBrowserEnv() {
  return typeof window !== 'undefined' && typeof document !== 'undefined'
}

function detectLocale() {
  if (!hasBrowserEnv()) return 'en'

  const saved = window.localStorage.getItem(STORAGE_KEY)
  if (saved && messages[saved]) return saved
  const nav = window.navigator.language || ''
  if (nav.startsWith('zh')) return 'zh'
  if (nav.startsWith('ja')) return 'ja'
  if (nav.startsWith('es')) return 'es'
  return 'en'
}

export const locale = ref(detectLocale())

export const localeOptions = [
  { code: 'en', label: 'EN' },
  { code: 'zh', label: '中文' },
  { code: 'ja', label: '日本語' },
  { code: 'es', label: 'Español' },
]

if (hasBrowserEnv()) {
  document.documentElement.lang = locale.value
}

watch(locale, (code) => {
  if (hasBrowserEnv()) {
    document.documentElement.lang = code
  }
})

export function setLocale(code) {
  locale.value = code
  if (hasBrowserEnv()) {
    window.localStorage.setItem(STORAGE_KEY, code)
  }
}

function get(obj, path) {
  return path.split('.').reduce((o, k) => (o && o[k] !== undefined ? o[k] : null), obj)
}

export function useI18n() {
  function t(key) {
    return get(messages[locale.value], key) || get(messages.en, key) || key
  }

  function ta(key) {
    const val = get(messages[locale.value], key) || get(messages.en, key)
    return Array.isArray(val) ? val : []
  }

  return { t, ta, locale, setLocale, localeOptions }
}
