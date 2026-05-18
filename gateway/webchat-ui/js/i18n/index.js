// ---------------------------------------------------------------------------
// i18n — Index: imports all modules, merges EN/ZH dicts, exports t/setLang/getLang
// ---------------------------------------------------------------------------

import { reactive } from '../petite-vue.es.js';

import common from './common.js';
import chat from './chat.js';
import runs from './runs.js';
import approvals from './approvals.js';
import governance from './governance.js';
import knowledge from './knowledge.js';
import automation from './automation.js';
import settings from './settings.js';
import setup from './setup.js';

const LANG_KEY = 'hc_lang';

const BASE_I18N = {
  en: {
    ...common.en,
    ...chat.en,
    ...runs.en,
    ...approvals.en,
    ...governance.en,
    ...knowledge.en,
    ...automation.en,
    ...settings.en,
    ...setup.en,
  },
  zh: {
    ...common.zh,
    ...chat.zh,
    ...runs.zh,
    ...approvals.zh,
    ...governance.zh,
    ...knowledge.zh,
    ...automation.zh,
    ...settings.zh,
    ...setup.zh,
  },
  ja: {
    ...common.ja,
    ...chat.ja,
    ...runs.ja,
    ...approvals.ja,
    ...governance.ja,
    ...knowledge.ja,
    ...automation.ja,
    ...settings.ja,
    ...setup.ja,
  },
};

const REMOTE_I18N = {
  en: {},
  zh: {},
  ja: {},
};

function normalizeLang(lang) {
  const value = String(lang || '').trim().toLowerCase();
  if (value.startsWith('zh')) return 'zh';
  if (value.startsWith('ja')) return 'ja';
  return 'en';
}

function langToLocale(lang) {
  const normalized = normalizeLang(lang);
  if (normalized === 'zh') return 'zh-CN';
  if (normalized === 'ja') return 'ja-JP';
  return 'en';
}

const langState = reactive({
  current: normalizeLang(localStorage.getItem(LANG_KEY) || navigator.language || 'en'),
});

/**
 * Get a translated string by key.
 * @param {string} key - Translation key.
 * @returns {string}
 */
export function t(key) {
  const lang = normalizeLang(langState.current);
  const remote = REMOTE_I18N[lang] || REMOTE_I18N.en;
  const local = BASE_I18N[lang] || BASE_I18N.en;
  return remote[key] || local[key] || REMOTE_I18N.en[key] || BASE_I18N.en[key] || key;
}

/**
 * Set the current language.
 * @param {string} lang - Language code ('en' or 'zh').
 */
export function setLang(lang) {
  const normalized = normalizeLang(lang);
  langState.current = normalized;
  localStorage.setItem(LANG_KEY, normalized);
  document.documentElement.lang = langToLocale(normalized);
}

/**
 * Get the current language code.
 * @returns {string}
 */
export function getLang() {
  return normalizeLang(langState.current);
}

export function applyRemoteCatalog(payload) {
  const lang = normalizeLang(payload && (payload.lang || payload.locale));
  if (payload && payload.messages && typeof payload.messages === 'object') {
    REMOTE_I18N[lang] = {
      ...REMOTE_I18N[lang],
      ...payload.messages,
    };
  }
  if (payload && payload.locale) {
    document.documentElement.lang = String(payload.locale);
  }
  return lang;
}
