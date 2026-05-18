// ---------------------------------------------------------------------------
// HopClaw Console - Petite Vue entry point
// ---------------------------------------------------------------------------

import { createApp, reactive } from './petite-vue.es.js';
import { api, setToken, getToken, showToast, consolePath } from './api.js';
import { t, setLang, getLang, applyRemoteCatalog } from './i18n/index.js';
import { ChatView, SessionList } from './views/chat.js';
import { SetupView } from './views/setup.js';
import { OverviewView } from './views/overview.js';
import { RunsView } from './views/runs.js';
import { ApprovalsView } from './views/approvals.js';
import { GovernanceView } from './views/governance.js';
import { KnowledgeView } from './views/knowledge.js';
import { AutomationView } from './views/automation.js';
import { SettingsView } from './views/settings.js';

const DEFAULT_ROUTE = '#/overview';
const CONNECTION_POLL_MS = 15000;
const GLOBAL_SSE_RECONNECT_MS = 5000;
const GLOBAL_SSE_RESPONSE_LIMIT = 256 * 1024;
const LANG_SEQUENCE = ['en', 'zh', 'ja'];
const THEME_STORAGE_KEY = 'hc_theme';
const GOVERNANCE_ALERT_CACHE_LIMIT = 64;

const ROUTE_ALIASES = {
  '#/chat': '#/assistant',
  '#/task-workspace': '#/assistant',
  '#/workspace': '#/assistant',
  '#/approval-center': '#/approvals',
  '#/operator': '#/governance',
  '#/operator-console': '#/governance',
  '#/channels': '#/settings/channels',
  '#/models': '#/settings/models',
  '#/sessions': '#/assistant',
  '#/artifacts': '#/knowledge',
  '#/memory': '#/knowledge',
  '#/agents': '#/automation/agents',
  '#/cron': '#/automation/schedules',
  '#/watch': '#/automation/watch',
  '#/hooks': '#/automation/hooks',
  '#/wakeup': '#/automation/schedules',
  '#/instances': '#/automation',
  '#/nodes': '#/settings/infrastructure',
  '#/devices': '#/settings/infrastructure',
  '#/helpers': '#/settings/infrastructure',
  '#/monitor': '#/overview',
  '#/usage': '#/settings/diagnostics',
  '#/logs': '#/settings/diagnostics',
  '#/audit': '#/settings/security',
};

const ICONS = {
  overview: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>',
  assistant: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>',
  runs: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polygon points="10,8 16,12 10,16 10,8"/></svg>',
  approvals: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M9 12l2 2 4-4"/></svg>',
  governance: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 3l7 4v5c0 5-3.5 8-7 9-3.5-1-7-4-7-9V7l7-4z"/><path d="M9 12h6"/><path d="M12 9v6"/></svg>',
  knowledge: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg>',
  automation: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2v4m0 12v4M4.93 4.93l2.83 2.83m8.48 8.48l2.83 2.83M2 12h4m12 0h4M4.93 19.07l2.83-2.83m8.48-8.48l2.83-2.83"/><circle cx="12" cy="12" r="3"/></svg>',
  settings: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>',
};

const NAV_ITEMS = [
  { id: 'overview', route: '#/overview', i18nKey: 'navOverview', icon: ICONS.overview, kickerKey: 'navOverview', descKey: 'overviewDesc' },
  { id: 'assistant', route: '#/assistant', i18nKey: 'navAssistant', icon: ICONS.assistant, kickerKey: 'shellPrimarySection', descKey: 'assistantDesc' },
  { id: 'runs', route: '#/runs', i18nKey: 'navRuns', icon: ICONS.runs, kickerKey: 'navRuns', descKey: 'runsDesc' },
  { id: 'approvals', route: '#/approvals', i18nKey: 'navApprovals', icon: ICONS.approvals, kickerKey: 'navApprovals', descKey: 'approvalsDesc' },
  { id: 'governance', route: '#/governance', i18nKey: 'navGovernance', icon: ICONS.governance, kickerKey: 'navGovernance', descKey: 'governanceDesc' },
  { id: 'knowledge', route: '#/knowledge', i18nKey: 'navKnowledge', icon: ICONS.knowledge, kickerKey: 'navKnowledge', descKey: 'knowledgeDesc' },
  { id: 'automation', route: '#/automation', i18nKey: 'navAutomation', icon: ICONS.automation, kickerKey: 'navAutomation', descKey: 'automationDesc' },
  { id: 'settings', route: '#/settings', i18nKey: 'navSettings', icon: ICONS.settings, kickerKey: 'navSettings', descKey: 'settingsDesc' },
];

const ROUTE_META = {
  '#/setup': {
    titleKey: 'setupTitle',
    kickerKey: 'setupTitle',
    descKey: 'setupDesc',
    descFallback: 'Finish the essentials, then move into the workspace without leaving the console.',
  },
};

function normalizeRoute(route) {
  const raw = route || DEFAULT_ROUTE;
  return ROUTE_ALIASES[raw] || raw;
}

function currentNavItem(route) {
  for (const item of NAV_ITEMS) {
    if (route === item.route || route.startsWith(item.route + '/')) return item;
  }
  return null;
}

function currentMeta(route) {
  const navItem = currentNavItem(route);
  if (navItem) return navItem;
  return ROUTE_META[route] || ROUTE_META['#/setup'];
}

function nextLanguage(current) {
  const currentIndex = LANG_SEQUENCE.indexOf(String(current || 'en'));
  if (currentIndex < 0) return LANG_SEQUENCE[0];
  return LANG_SEQUENCE[(currentIndex + 1) % LANG_SEQUENCE.length];
}

function languageToggleLabel(lang) {
  const nextLang = nextLanguage(lang);
  if (nextLang === 'zh') return '中文';
  if (nextLang === 'ja') return '日本語';
  return 'EN';
}

function normalizeTheme(theme) {
  return String(theme || '').trim().toLowerCase() === 'dark' ? 'dark' : 'light';
}

function readStoredTheme() {
  return normalizeTheme(localStorage.getItem(THEME_STORAGE_KEY) || 'light');
}

const initialTheme = readStoredTheme();
document.documentElement.setAttribute('data-theme', initialTheme);

const store = reactive({
  route: normalizeRoute(window.location.hash || DEFAULT_ROUTE),
  lang: getLang(),
  langVersion: 0,
  theme: initialTheme,
  connectionStatus: 'connecting',
  setupConfigured: true,
  sidebarOpen: false,
  sidebarCollapsed: localStorage.getItem('hc_sidebar_collapsed') === 'true',
  threadsOpen: false,
  sessionKey: localStorage.getItem('hc_session_key') || 'webchat',
  sessionList: [],
});

window._hcStore = store;

let connectionProbeTimer = null;
let connectionProbeRunning = null;
let consoleEventStream = null;
let consoleEventReconnectTimer = null;
let consoleEventGeneration = 0;
let consoleEventBuffer = '';
const governanceAlertCache = [];
const governanceAlertSeen = new Set();

function applyThemePreference(theme) {
  const next = normalizeTheme(theme);
  store.theme = next;
  document.documentElement.setAttribute('data-theme', next);
  localStorage.setItem(THEME_STORAGE_KEY, next);
  return next;
}

function applyDocumentTitle() {
  const meta = currentMeta(store.route);
  const title = meta && meta.i18nKey ? t(meta.i18nKey) : meta && meta.titleKey ? t(meta.titleKey) : t('appTitle');
  document.title = title + ' · HopClaw Console';
}

function applyBootstrapQueryState() {
  const url = new URL(window.location.href);
  const token = (url.searchParams.get('token') || '').trim();
  const session = (url.searchParams.get('session') || '').trim();
  const lang = (url.searchParams.get('lang') || '').trim();

  if (token) {
    setToken(token);
    url.searchParams.delete('token');
  }
  if (session) {
    store.sessionKey = session;
    localStorage.setItem('hc_session_key', session);
  }
  if (lang) {
    setLang(lang);
    store.lang = getLang();
  }

  if (window.location.href !== url.href) {
    window.history.replaceState({}, '', url.toString());
  }
}

async function loadRemoteCatalog(lang) {
  try {
    const query = new URLSearchParams({ lang: String(lang || store.lang || 'en') }).toString();
    const payload = await api.get(consolePath('/api/i18n') + (query ? '?' + query : ''), { background: true });
    applyRemoteCatalog(payload);
    store.lang = getLang();
    store.langVersion++;
    applyDocumentTitle();
  } catch (_) {
    // Catalog bootstrap is best-effort. Local dictionaries remain available.
  }
}

async function bootstrapConsoleConfig() {
  try {
    const params = new URLSearchParams();
    const currentSession = (store.sessionKey || '').trim();
    if (currentSession) params.set('session', currentSession);
    params.set('lang', store.lang);
    const query = params.toString();
    const cfg = await api.get(consolePath('/api/config') + (query ? '?' + query : ''), { background: true });
    if (cfg && typeof cfg === 'object') {
      if (cfg.session_key) {
        store.sessionKey = cfg.session_key;
        localStorage.setItem('hc_session_key', cfg.session_key);
      }
      if (!getToken() && cfg.auth_token) {
        setToken(cfg.auth_token);
      }
      setLang(cfg.locale || cfg.lang || store.lang);
      store.lang = getLang();
      await loadRemoteCatalog(cfg.locale || cfg.lang || store.lang);
    }
  } catch (err) {
    // Config bootstrap is best-effort; the console still works with local defaults.
    if (err && (err.status === 401 || err.status === 403)) {
      store.connectionStatus = 'auth_required';
    }
  }
}

function syncRouteFromHash() {
  const nextRoute = normalizeRoute(window.location.hash || DEFAULT_ROUTE);
  if (window.location.hash !== nextRoute) {
    window.location.hash = nextRoute;
    return;
  }
  store.route = nextRoute;
  applyDocumentTitle();
}

async function checkSetupStatus() {
  try {
    const data = await api.get('/operator/setup/status', { background: true });
    if (data && data.configured === false) {
      store.setupConfigured = false;
      if (!window.location.hash) {
        window.location.hash = '#/setup';
        store.route = '#/setup';
        applyDocumentTitle();
        return;
      }
    } else {
      store.setupConfigured = true;
    }
  } catch (_) {
    // If the setup endpoint is unavailable, keep the console reachable.
  }

  if (!window.location.hash) {
    window.location.hash = DEFAULT_ROUTE;
  }
  syncRouteFromHash();
}

async function probeConsoleConnection() {
  if (connectionProbeRunning) return connectionProbeRunning;
  connectionProbeRunning = (async () => {
    try {
      await api.get('/operator/status', { background: true });
      store.connectionStatus = 'connected';
    } catch (err) {
      if (err && (err.status === 401 || err.status === 403)) {
        store.connectionStatus = 'auth_required';
      } else if (err && err.status === 0) {
        store.connectionStatus = 'network_error';
      } else {
        store.connectionStatus = 'server_error';
      }
    } finally {
      connectionProbeRunning = null;
    }
  })();
  return connectionProbeRunning;
}

function startConsoleConnectionProbe() {
  if (connectionProbeTimer) clearInterval(connectionProbeTimer);
  probeConsoleConnection();
  connectionProbeTimer = window.setInterval(() => {
    probeConsoleConnection();
  }, CONNECTION_POLL_MS);
}

function currentConsoleRouteIsGovernance() {
  return store.route === '#/governance' || store.route.startsWith('#/governance/');
}

function governanceEventAttrs(event) {
  if (!event || typeof event !== 'object' || !event.attrs || typeof event.attrs !== 'object') {
    return {};
  }
  return event.attrs;
}

function isGovernanceDeadLetterEvent(event) {
  if (!event || typeof event !== 'object') return false;
  const eventType = String(event.type || '').trim();
  if (eventType === 'governance.dead_letter' || eventType === 'governance.delivery.dead_lettered') {
    return true;
  }
  const attrs = governanceEventAttrs(event);
  return String(attrs.delivery_status || '').trim() === 'dead_letter';
}

function governanceDeadLetterAlertKey(event) {
  const attrs = governanceEventAttrs(event);
  const parts = [
    String(event && event.id || '').trim(),
    String(attrs.delivery_id || '').trim(),
    String(attrs.event_id || '').trim(),
    String(attrs.adapter_name || '').trim(),
    String(attrs.summary || '').trim(),
  ].filter(Boolean);
  if (parts.length === 0) return '';
  return parts.join('|');
}

function rememberGovernanceDeadLetterAlert(key) {
  if (!key || governanceAlertSeen.has(key)) return;
  governanceAlertSeen.add(key);
  governanceAlertCache.push(key);
  if (governanceAlertCache.length <= GOVERNANCE_ALERT_CACHE_LIMIT) return;
  const oldest = governanceAlertCache.shift();
  if (oldest) governanceAlertSeen.delete(oldest);
}

function governanceDeadLetterToastMessage(event) {
  const attrs = governanceEventAttrs(event);
  const adapter = String(attrs.adapter_name || '').trim();
  const summary = String(attrs.summary || '').trim();
  if (summary && adapter) return summary + ' (' + adapter + ')';
  if (summary) return summary;
  if (adapter) {
    return (t('governanceDeadLetterToastAdapter') || 'Delivery failed on') + ' ' + adapter + '. ' +
      (t('governanceDeadLetterToastReview') || 'Review it in Governance.');
  }
  return t('governanceDeadLetterToast') || 'A delivery was sent to dead letter. Review it in Governance.';
}

function handleConsoleEvent(event) {
  if (!isGovernanceDeadLetterEvent(event)) return;
  if (currentConsoleRouteIsGovernance()) return;
  const alertKey = governanceDeadLetterAlertKey(event);
  if (!alertKey || governanceAlertSeen.has(alertKey)) return;
  rememberGovernanceDeadLetterAlert(alertKey);
  showToast(governanceDeadLetterToastMessage(event), 'warning');
}

function parseConsoleEventChunk(chunk) {
  consoleEventBuffer += chunk;
  const lines = consoleEventBuffer.split('\n');
  consoleEventBuffer = lines.pop() || '';
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.indexOf('data: ') !== 0) continue;
    try {
      handleConsoleEvent(JSON.parse(trimmed.substring(6)));
    } catch (_) {
      // Ignore malformed SSE frames from advisory event stream.
    }
  }
}

function stopConsoleEventStream() {
  if (consoleEventReconnectTimer) {
    clearTimeout(consoleEventReconnectTimer);
    consoleEventReconnectTimer = null;
  }
  if (consoleEventStream) {
    try { consoleEventStream.abort(); } catch (_) {}
    consoleEventStream = null;
  }
}

function scheduleConsoleEventReconnect(generation) {
  if (consoleEventReconnectTimer || generation !== consoleEventGeneration) return;
  consoleEventReconnectTimer = window.setTimeout(() => {
    consoleEventReconnectTimer = null;
    if (generation === consoleEventGeneration) startConsoleEventStream();
  }, GLOBAL_SSE_RECONNECT_MS);
}

function startConsoleEventStream() {
  const generation = ++consoleEventGeneration;
  stopConsoleEventStream();
  consoleEventBuffer = '';

  const xhr = new XMLHttpRequest();
  let lastIndex = 0;
  xhr.open('GET', consolePath('/sse'), true);

  const token = getToken();
  if (token) xhr.setRequestHeader('Authorization', 'Bearer ' + token);
  xhr.setRequestHeader('Accept', 'text/event-stream');
  xhr.setRequestHeader('Cache-Control', 'no-cache');

  consoleEventStream = xhr;

  xhr.onreadystatechange = function () {
    if (generation !== consoleEventGeneration || consoleEventStream !== xhr) {
      try { xhr.abort(); } catch (_) {}
      return;
    }
    if (xhr.readyState === XMLHttpRequest.HEADERS_RECEIVED && xhr.status !== 200) {
      consoleEventStream = null;
      try { xhr.abort(); } catch (_) {}
      scheduleConsoleEventReconnect(generation);
      return;
    }
    if (xhr.readyState >= 3) {
      const chunk = xhr.responseText.substring(lastIndex);
      lastIndex = xhr.responseText.length;
      parseConsoleEventChunk(chunk);
      if (xhr.responseText.length > GLOBAL_SSE_RESPONSE_LIMIT) {
        consoleEventBuffer = '';
        consoleEventStream = null;
        try { xhr.abort(); } catch (_) {}
        scheduleConsoleEventReconnect(generation);
        return;
      }
    }
    if (xhr.readyState === 4 && generation === consoleEventGeneration && consoleEventStream === xhr) {
      consoleEventStream = null;
      scheduleConsoleEventReconnect(generation);
    }
  };

  xhr.onerror = function () {
    if (generation !== consoleEventGeneration || consoleEventStream !== xhr) return;
    consoleEventStream = null;
    scheduleConsoleEventReconnect(generation);
  };

  xhr.send();
}

function App() {
  return {
    get route() { return store.route; },
    get lang() { return store.lang; },
    get theme() { return store.theme; },
    get langToggleLabel() { return languageToggleLabel(store.lang); },
    get langVersion() { return store.langVersion; },
    get sidebarOpen() { return store.sidebarOpen; },
    get sidebarCollapsed() { return store.sidebarCollapsed; },
    get threadsOpen() { return store.threadsOpen; },
    set threadsOpen(v) { store.threadsOpen = v; },
    get hasToken() { return Boolean(getToken()); },
    get storeSessionKey() {
      return store.sessionKey || localStorage.getItem('hc_session_key') || 'webchat';
    },
    get connectionDotClass() {
      return store.connectionStatus === 'connected' ? 'ok'
        : store.connectionStatus === 'connecting' ? ''
        : store.connectionStatus === 'server_error' ? 'warn'
        : 'err';
    },
    get showSetupNotice() {
      return !store.setupConfigured && store.route !== '#/setup';
    },
    get connectionMetaLabel() {
      if (store.connectionStatus === 'auth_required') return t('shellAuthRequired');
      if (store.connectionStatus === 'network_error') return t('shellNetworkIssue');
      if (store.connectionStatus === 'server_error') return t('shellServerIssue');
      return getToken() ? t('shellAuthTokenReady') : t('shellAuthTokenMissing');
    },
    get connectionMetaTone() {
      return store.connectionStatus === 'connected' ? 'ok'
        : store.connectionStatus === 'server_error' ? 'warn'
        : store.connectionStatus === 'connecting' ? ''
        : 'err';
    },
    get connectionLabel() {
      return store.connectionStatus === 'connected' ? t('connected')
        : store.connectionStatus === 'auth_required' ? t('authRequired')
        : store.connectionStatus === 'network_error' ? t('networkIssue')
        : store.connectionStatus === 'server_error' ? t('serverIssue')
        : t('connecting');
    },
    get topbarKicker() {
      const meta = currentMeta(store.route);
      return meta && meta.kickerKey ? t(meta.kickerKey) : t('appTitle');
    },
    get topbarTitle() {
      const meta = currentMeta(store.route);
      if (meta && meta.i18nKey) return t(meta.i18nKey);
      if (meta && meta.titleKey) return t(meta.titleKey);
      return t('navAssistant');
    },
    get topbarDesc() {
      const meta = currentMeta(store.route);
      if (!meta || !meta.descKey) return t('assistantDesc');
      const text = t(meta.descKey);
      if (text && text !== meta.descKey) return text;
      return meta.descFallback || t('assistantDesc');
    },

    navItems: NAV_ITEMS,
    ICONS,
    t,

    isNavActive(item) {
      return store.route === item.route || store.route.startsWith(item.route + '/');
    },

    navigate(hash) {
      window.location.hash = normalizeRoute(hash);
    },

    openSidebar() {
      store.sidebarOpen = true;
    },

    closeSidebar() {
      store.sidebarOpen = false;
    },

    toggleSidebarCollapse() {
      store.sidebarCollapsed = !store.sidebarCollapsed;
      localStorage.setItem('hc_sidebar_collapsed', store.sidebarCollapsed);
    },

    async toggleLang() {
      const nextLang = nextLanguage(store.lang);
      setLang(nextLang);
      store.lang = getLang();
      await loadRemoteCatalog(nextLang);
      applyDocumentTitle();
    },

    copyCode(btn) {
      const pre = btn.parentElement;
      const code = pre ? pre.querySelector('code') : null;
      if (!code) return;
      navigator.clipboard.writeText(code.textContent).then(() => {
        btn.textContent = t('copied');
        setTimeout(() => {
          btn.textContent = t('copyCode');
        }, 1500);
      });
    },

    queueReview(route) {
      this.navigate(route);
      this.closeSidebar();
    },

    async init() {
      document.documentElement.setAttribute('data-theme', store.theme);
      setLang(store.lang);
      store.lang = getLang();
      store.connectionStatus = 'connecting';

      applyBootstrapQueryState();
      await loadRemoteCatalog(store.lang);
      await bootstrapConsoleConfig();
      syncRouteFromHash();
      window.addEventListener('hashchange', syncRouteFromHash);
      window.addEventListener('focus', probeConsoleConnection);
      window.addEventListener('online', probeConsoleConnection);
      window.addEventListener('offline', () => {
        store.connectionStatus = 'network_error';
      });
      document.addEventListener('visibilitychange', () => {
        if (!document.hidden) probeConsoleConnection();
      });
      document.addEventListener('keydown', (event) => {
        if (event.key === 'Escape' && store.sidebarOpen) {
          store.sidebarOpen = false;
        }
      });
      startConsoleConnectionProbe();
      startConsoleEventStream();

      await checkSetupStatus();
    },
  };
}

window.HC = {
  copyCode(btn) {
    const pre = btn.parentElement;
    const code = pre ? pre.querySelector('code') : null;
    if (!code) return;
    navigator.clipboard.writeText(code.textContent).then(() => {
      btn.textContent = t('copied');
      setTimeout(() => {
        btn.textContent = t('copyCode');
      }, 1500);
    });
  },
  store,
  setTheme(theme) {
    return applyThemePreference(theme);
  },
  showToast,
  t,
};

createApp({
  App,
  ChatView,
  SessionList,
  SetupView,
  OverviewView,
  RunsView,
  ApprovalsView,
  GovernanceView,
  KnowledgeView,
  AutomationView,
  SettingsView,
}).mount('#app');
