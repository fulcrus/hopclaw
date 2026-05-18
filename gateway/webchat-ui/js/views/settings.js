// ---------------------------------------------------------------------------
// Settings View - Petite Vue component
// ---------------------------------------------------------------------------

import { api, showToast } from '../api.js';
import { t } from '../i18n/index.js';
import { artifactPreviewPath, safeExternalURL } from '../linking.js';
import { buildSettingsBrowserSection } from './settings/browser.js';
import { buildSettingsChannelsSection } from './settings/channels.js';
import { buildSettingsCoreReadinessSection } from './settings/core-readiness.js';
import { buildSettingsDiagnosticsSection } from './settings/diagnostics.js';
import { buildSettingsInfrastructureSection } from './settings/infrastructure.js';
import { buildSettingsModelsSection } from './settings/models.js';
import { buildSettingsPluginsSection } from './settings/plugins.js';
import { buildSettingsSecurityConfigSection } from './settings/security-config.js';
import { buildSettingsSkillsSection } from './settings/skills.js';
import {
  defaultProviderAPI,
  defaultProviderFieldValues,
  effectiveProviderAPIOptions,
  effectiveProviderAPISchema,
  normalizeProviderAPI,
  providerCapabilityBadges,
  providerFieldDisplayValue,
  providerFieldEmptyPayload,
  providerFieldIsTextarea as isProviderFieldTextarea,
  providerFieldPayloadValue,
  providerFieldRequiresExplicitMutation,
  providerFieldRows as providerFieldRowCount,
} from '../provider-api-forms.js';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const TABS = [
  {
    id: 'models',
    i18nKey: 'settingsModels',
    groupKey: 'settingsGroupCoreSetup',
    badgeKey: 'settingsBadgeEssential',
    summaryKey: 'settingsSummaryModels',
    group: 'Core Setup',
    badge: 'Essential',
    summary: 'Connect and validate the model providers that power HopClaw.',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><circle cx="12" cy="12" r="3"/><path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4M4.22 19.78l2.83-2.83M16.95 7.05l2.83-2.83"/></svg>',
  },
  {
    id: 'channels',
    i18nKey: 'settingsChannels',
    groupKey: 'settingsGroupCoreSetup',
    badgeKey: 'settingsBadgeEssential',
    summaryKey: 'settingsSummaryChannels',
    group: 'Core Setup',
    badge: 'Essential',
    summary: 'Connect the work channels where HopClaw receives requests and delivers outcomes.',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M4 11a9 9 0 0 1 9 9"/><path d="M4 4a16 16 0 0 1 16 16"/><circle cx="5" cy="19" r="1"/></svg>',
  },
  {
    id: 'skills',
    i18nKey: 'settingsSkills',
    groupKey: 'settingsGroupWorkspaceCapabilities',
    badgeKey: 'settingsBadgeRecommended',
    summaryKey: 'settingsSummarySkills',
    group: 'Workspace Capabilities',
    badge: 'Recommended',
    summary: 'Inspect, install, validate, and test skills and tool contracts in one place.',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>',
  },
  {
    id: 'plugins',
    i18nKey: 'settingsPlugins',
    groupKey: 'settingsGroupAdvanced',
    badgeKey: 'settingsBadgeAdvanced',
    summaryKey: 'settingsSummaryPlugins',
    group: 'Advanced',
    badge: 'Advanced',
    summary: 'Manage external plugin sources and extension packages.',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M14 7V4a2 2 0 0 0-4 0v3"/><path d="M5 10h14v9a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2v-9z"/><path d="M10 10V7a2 2 0 1 1 4 0v3"/></svg>',
  },
  {
    id: 'browser',
    i18nKey: 'settingsBrowser',
    groupKey: 'settingsGroupWorkspaceCapabilities',
    badgeKey: 'settingsBadgeOptional',
    summaryKey: 'settingsSummaryBrowser',
    group: 'Workspace Capabilities',
    badge: 'Optional',
    summary: 'Configure browser hosts, profiles, and operator-side browsing helpers.',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><rect x="2" y="4" width="20" height="16" rx="2"/><path d="M2 8h20"/><path d="M6 6h.01M10 6h.01"/></svg>',
  },
  {
    id: 'security',
    i18nKey: 'settingsSecurity',
    groupKey: 'settingsGroupWorkspaceCapabilities',
    badgeKey: 'settingsBadgeRecommended',
    summaryKey: 'settingsSummarySecurity',
    group: 'Workspace Capabilities',
    badge: 'Recommended',
    summary: 'Tune execution policy, approvals, network boundaries, and tool risk controls.',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>',
  },
  {
    id: 'config',
    i18nKey: 'settingsConfig',
    groupKey: 'settingsGroupAdvanced',
    badgeKey: 'settingsBadgeAdvanced',
    summaryKey: 'settingsSummaryConfig',
    group: 'Advanced',
    badge: 'Advanced',
    summary: 'Inspect and edit full JSON configuration sections directly.',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>',
  },
  {
    id: 'infrastructure',
    i18nKey: 'settingsInfrastructure',
    groupKey: 'settingsGroupCoreSetup',
    badgeKey: 'settingsBadgeEssential',
    summaryKey: 'settingsSummaryInfrastructure',
    group: 'Core Setup',
    badge: 'Essential',
    summary: 'Verify helpers, capability hosts, nodes, devices, and pairing records.',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M5 8h14"/><path d="M5 16h14"/><path d="M8 5v14"/><path d="M16 5v14"/></svg>',
  },
  {
    id: 'diagnostics',
    i18nKey: 'settingsDiagnostics',
    groupKey: 'settingsGroupAdvanced',
    badgeKey: 'settingsBadgeAdvanced',
    summaryKey: 'settingsSummaryDiagnostics',
    group: 'Advanced',
    badge: 'Advanced',
    summary: 'Review health, usage, logs, audit output, and operational diagnostics.',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>',
  },
];

const DEFAULT_PAGE_SIZE = 20;
const PREVIEW_TEXT_LIMIT = 220;

const MULTILINE_CHANNEL_FIELD_TYPES = new Set(['string_list']);
const CHANNEL_TYPE_ALIASES = {
  nextcloud: 'nextcloud_talk',
  nextcloudtalk: 'nextcloud_talk',
  synologychat: 'synology_chat',
};

// ---------------------------------------------------------------------------
// Exec Approval Mode Options
// ---------------------------------------------------------------------------

const EXEC_APPROVAL_OPTIONS = ['deny', 'allowlist', 'approve', 'full'];

const LAYER2_FEATURES = ['git', 'media', 'container', 'packages', 'search', 'speech', 'email', 'session'];
const HEALTHY_CAPABILITY_STATES = ['healthy', 'ok', 'ready', 'green'];
const HEALTHY_MODULE_STATES = ['healthy', 'ok', 'ready', 'green'];
const CONNECTED_CHANNEL_STATES = ['connected'];
const WARNING_CHANNEL_STATES = ['startup_grace', 'stale_socket'];
const ACTIVE_HELPER_STATES = ['running', 'ready', 'healthy', 'ok'];
const WARNING_HELPER_STATES = ['starting', 'degraded'];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function fmtTime(iso) {
  try {
    return iso ? new Date(iso).toLocaleString() : '-';
  } catch (_) {
    return iso || '-';
  }
}

function formatStatusLabel(value) {
  if (!value) return '-';
  return String(value).replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function formatFileSize(bytes) {
  const size = Number(bytes);
  if (!Number.isFinite(size) || size <= 0) return '';
  const units = ['B', 'KB', 'MB', 'GB'];
  let index = 0;
  let current = size;
  while (current >= 1024 && index < units.length - 1) {
    current /= 1024;
    index++;
  }
  return current.toFixed(index === 0 ? 0 : 1) + ' ' + units[index];
}

function prettyData(value) {
  if (value == null || value === '') return '';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch (_) {
    return String(value);
  }
}

function truncatePreview(text) {
  const value = String(text || '').trim();
  if (value.length <= PREVIEW_TEXT_LIMIT) return value;
  return value.slice(0, PREVIEW_TEXT_LIMIT) + '…';
}

function paginate(items, page, pageSize) {
  const size = Number(pageSize);
  const start = (page - 1) * size;
  return items.slice(start, start + size);
}

function capabilityHealth(value) {
  if (!value) return '';
  if (typeof value === 'string') return value.toLowerCase();
  if (typeof value === 'object') return String(value.status || value.Status || '').toLowerCase();
  return String(value).toLowerCase();
}

function moduleHealth(value) {
  if (!value) return '';
  if (typeof value === 'string') return value.toLowerCase();
  if (typeof value === 'object') return String(value.status || value.Status || '').toLowerCase();
  return String(value).toLowerCase();
}

function totalPages(items, pageSize) {
  return Math.max(1, Math.ceil(items.length / Number(pageSize)));
}

function mergeSectionDescriptors(target, ...sections) {
  for (const section of sections) {
    if (!section) continue;
    Object.defineProperties(target, Object.getOwnPropertyDescriptors(section));
  }
  return target;
}

function settledValue(result) {
  return result && result.status === 'fulfilled' ? result.value : null;
}

function normalizeChannelTypeKey(type) {
  const normalized = String(type || '').trim().toLowerCase();
  if (!normalized) return '';
  return CHANNEL_TYPE_ALIASES[normalized] || normalized;
}

function normalizeChannelFieldType(field) {
  const type = String(field && field.type || '').trim().toLowerCase();
  switch (type) {
    case 'bool':
    case 'string_list':
    case 'text':
      return type;
    default:
      return field && field.secret === true ? 'password' : 'text';
  }
}

function normalizeConsoleTheme(theme) {
  return String(theme || '').trim().toLowerCase() === 'dark' ? 'dark' : 'light';
}

function splitChannelFieldLines(raw) {
  return String(raw || '')
    .split(/[\r\n,]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function normalizeManagedHelperName(name) {
  return String(name || '').trim().toLowerCase();
}

function helperDisplayName(name) {
  switch (normalizeManagedHelperName(name)) {
    case 'browser':
      return t('helpersBrowser') || 'Browser Helper';
    case 'desktop':
      return t('helpersDesktop') || 'Desktop Helper';
    default:
      return formatStatusLabel(name || 'helper');
  }
}

function helperStatusText(status) {
  switch (String(status || '').trim().toLowerCase()) {
    case 'running':
    case 'ready':
    case 'healthy':
    case 'ok':
      return t('helpersStatusRunning') || 'Running';
    case 'stopped':
      return t('helpersStatusStopped') || 'Stopped';
    case 'unavailable':
      return t('helpersStatusUnavailable') || 'Unavailable';
    case '':
    case 'unknown':
      return t('helpersStatusUnknown') || 'Unknown';
    default:
      return formatStatusLabel(status || 'unknown');
  }
}

function normalizeHelperState(name, state) {
  const source = state && typeof state === 'object' ? state : {};
  const lastUseAt = String(source.last_use_at || '').trim();
  const sessionCount = Number(source.session_count);
  const idleTimeoutSec = Number(source.idle_timeout_sec);
  const view = {
    ...source,
    name: normalizeManagedHelperName(name || source.name),
    status: String(source.status || '').trim().toLowerCase() || 'unknown',
    session_count: Number.isFinite(sessionCount) ? sessionCount : 0,
    last_use_at: lastUseAt.startsWith('0001-01-01') ? '' : lastUseAt,
    idle_timeout_sec: Number.isFinite(idleTimeoutSec) ? idleTimeoutSec : 0,
  };

  return view;
}

function normalizeHelperList(payload) {
  if (Array.isArray(payload)) {
    return payload.map(item => normalizeHelperState(item && item.name, item)).filter(item => item.name);
  }
  if (payload && Array.isArray(payload.helpers) && payload.helpers.length > 0) {
    return payload.helpers.map(item => normalizeHelperState(item && item.name, item)).filter(item => item.name);
  }

  const items = [];
  const add = (name, state) => {
    if (!state || typeof state !== 'object') return;
    items.push(normalizeHelperState(name, state));
  };
  if (payload && typeof payload === 'object') {
    add('browser', payload.browser);
    add('desktop', payload.desktop);
  }
  return items;
}

function formatHelperIdleTimeout(seconds) {
  const total = Number(seconds);
  if (!Number.isFinite(total) || total <= 0) return t('disabled') || 'Disabled';

  const parts = [];
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = total % 60;
  if (hours > 0) parts.push(hours + 'h');
  if (minutes > 0) parts.push(minutes + 'm');
  if (secs > 0 || parts.length === 0) parts.push(secs + 's');
  return parts.join(' ');
}

function setupCatalogChannelProfile(setupCatalog, type) {
  if (!setupCatalog || !Array.isArray(setupCatalog.channels)) return null;
  const normalized = normalizeChannelTypeKey(type);
  return setupCatalog.channels.find((item) => normalizeChannelTypeKey(item && item.id) === normalized) || null;
}

function channelFieldFromCatalog(field) {
  const key = (field && (field.config_key || field.id)) || '';
  if (!key) return null;
  const view = {
    key,
    label: (field && field.label) || key,
    type: normalizeChannelFieldType(field),
    required: !!(field && field.required),
    placeholder: (field && (field.placeholder || field.default_value || field.description)) || '',
  };
}

function effectiveChannelSchema(type, setupCatalog) {
  const normalized = normalizeChannelTypeKey(type);
  const profile = setupCatalogChannelProfile(setupCatalog, normalized);
  if (!profile) {
    return { label: normalized || type, fields: [] };
  }

  const view = {
    label: profile.display_name || normalized || type,
    fields: (Array.isArray(profile.operator_fields) && profile.operator_fields.length > 0
      ? profile.operator_fields
      : (Array.isArray(profile.fields) ? profile.fields : [])
    ).map(channelFieldFromCatalog).filter(Boolean),
  };

  return view;
}

function effectiveChannelTypeOptions(setupCatalog) {
  const out = [];
  const seen = new Set();
  const add = (value) => {
    const normalized = normalizeChannelTypeKey(value);
    if (!normalized || seen.has(normalized)) return;
    seen.add(normalized);
    out.push(normalized);
  };

  const catalogChannels = setupCatalog && Array.isArray(setupCatalog.channels) ? setupCatalog.channels : [];
  for (const item of catalogChannels) {
    add(item && item.id);
  }
  return out;
}

function channelFieldDisplayValue(field, value) {
  const type = normalizeChannelFieldType(field);
  if (type === 'string_list') {
    if (Array.isArray(value)) return value.join('\n');
    return String(value || '');
  }
  if (type === 'bool') return value === true || value === 'true';
  return value == null ? '' : String(value);
}

function channelFieldPayloadValue(field, rawValue) {
  const type = normalizeChannelFieldType(field);
  if (type === 'string_list') {
    const items = Array.isArray(rawValue)
      ? rawValue.map((item) => String(item || '').trim()).filter(Boolean)
      : splitChannelFieldLines(rawValue);
    return items.length > 0 ? items : undefined;
  }
  if (type === 'bool') {
    return rawValue === true || rawValue === 'true';
  }
  const value = String(rawValue == null ? '' : rawValue).trim();
  return value ? value : undefined;
}

function channelFieldRowCount(field) {
  return MULTILINE_CHANNEL_FIELD_TYPES.has(normalizeChannelFieldType(field)) ? 4 : 0;
}

function defaultChannelFormConfig(type, setupCatalog) {
  const schema = effectiveChannelSchema(type, setupCatalog);
  const config = {};
  const fields = schema && Array.isArray(schema.fields) ? schema.fields : [];
  for (const field of fields) {
    if (!field || !field.key) continue;
    if (field.type === 'bool') {
      config[field.key] = false;
    }
  }
  return config;
}

// ---------------------------------------------------------------------------
// SettingsView Component
// ---------------------------------------------------------------------------

export function SettingsView() {
  const store = window._hcStore;
  let boundRouteSync = null;

  function getInitialTab() {
    const hash = store.route || window.location.hash || '';
    const sub = hash.replace('#/settings', '').replace(/^\//, '') || 'models';
    return TABS.find((tb) => tb.id === sub) ? sub : 'models';
  }

  // -------------------------------------------------------------------------
  // Template
  // -------------------------------------------------------------------------

  const $template = `
    <div class="hc-settings-layout hc-settings-no-tabs">
      <div class="hc-settings-content">
        <div class="hc-page-intro">
          <div class="hc-page-intro-copy">
            <div class="hc-page-intro-title">{{ t('settingsTitle') }}</div>
            <div class="hc-page-intro-text">
              {{ t('settingsIntroText') }}
            </div>
          </div>
          <div class="hc-nav-chip-list">
            <a class="hc-nav-chip" href="#/setup">{{ t('settingsOpenSetup') }}</a>
            <a class="hc-nav-chip" href="#/overview">{{ t('settingsReviewReadiness') }}</a>
          </div>
        </div>

        <div class="hc-card hc-settings-theme-card" data-testid="settings-theme-toggle">
          <div>
            <div class="hc-settings-section-title">{{ t('settingsAppearanceTitle') || 'Appearance' }}</div>
            <div class="hc-settings-section-desc" style="margin-bottom:0">
              {{ t('settingsAppearanceDesc') || 'Choose how the console should render in this browser. The preference is saved locally.' }}
            </div>
          </div>
          <div class="hc-tabs hc-settings-theme-options">
            <button
              v-for="option in themeOptions"
              :key="'theme-option-' + option"
              type="button"
              class="hc-tab"
              :class="{ active: selectedTheme === option }"
              :data-testid="'settings-theme-' + option"
              @click="setTheme(option)"
            >
              {{ themeOptionLabel(option) }}
            </button>
          </div>
        </div>

        <div class="hc-card hc-settings-nav-shell">
          <div class="hc-settings-nav-groups">
            <div v-for="group in settingsGroups()" :key="group.name" class="hc-settings-nav-group">
              <div class="hc-settings-nav-group-title">{{ group.i18nName || group.name }}</div>
              <div class="hc-settings-nav-group-tabs">
                <button
                  v-for="tab in group.items"
                  :key="tab.id"
                  :data-testid="'settings-tab-' + tab.id"
                  class="hc-tab hc-settings-topnav-tab"
                  :class="{ active: activeTab === tab.id }"
                  @click="switchTab(tab.id)"
                >
                  <span v-html="tab.icon"></span>
                  <span>{{ t(tab.i18nKey) }}</span>
                  <span class="hc-badge hc-badge-gray">{{ tab.badgeKey ? (t(tab.badgeKey) || tab.badge) : tab.badge }}</span>
                </button>
              </div>
            </div>
          </div>

          <div class="hc-settings-context-grid">
            <div v-if="activeTabMeta()" class="hc-settings-context-card">
              <div class="hc-settings-context-kicker">{{ activeTabMeta().groupKey ? (t(activeTabMeta().groupKey) || activeTabMeta().group) : activeTabMeta().group }}</div>
              <div class="hc-settings-context-title">{{ t(activeTabMeta().i18nKey) }}</div>
              <div class="hc-settings-context-copy">{{ activeTabMeta().summaryKey ? (t(activeTabMeta().summaryKey) || activeTabMeta().summary) : activeTabMeta().summary }}</div>
              <div class="hc-result-chip-row">
                <span class="hc-result-chip">{{ activeTabMeta().badgeKey ? (t(activeTabMeta().badgeKey) || activeTabMeta().badge) : activeTabMeta().badge }}</span>
              </div>
            </div>

            <div class="hc-settings-context-card hc-settings-readiness-card">
              <div class="hc-settings-context-kicker">{{ t('settingsCoreSetup') }}</div>
              <div class="hc-settings-context-title">{{ t('settingsRealReadiness') }}</div>
              <div class="hc-settings-context-copy">
                {{ coreReadinessHeadline() }}
              </div>
              <div class="hc-settings-readiness-score">
                <div class="hc-monitor-card-value">{{ coreReadinessWarnings.length > 0 ? '?' : coreReadinessCompleted() }}/{{ coreReadinessItems().length || 4 }}</div>
                <div class="hc-monitor-card-sub">{{ coreReadinessSummary() }}</div>
              </div>
              <div class="hc-readiness-list hc-settings-readiness-list">
                <a v-for="item in coreReadinessItems()" :key="item.key" class="hc-readiness-item" :href="item.href">
                  <div class="hc-readiness-main">
                    <span class="hc-readiness-icon" :class="item.ok ? 'ok' : (item.warn ? 'warn' : 'todo')">{{ item.ok ? '✓' : (item.warn ? '!' : '•') }}</span>
                    <div>
                      <div class="hc-readiness-title">{{ item.title }}</div>
                      <div class="hc-readiness-desc">{{ item.desc }}</div>
                    </div>
                  </div>
                  <span class="hc-readiness-cta">{{ item.cta }}</span>
                </a>
              </div>
              <div v-if="coreReadinessWarnings.length > 0" class="hc-empty" style="margin-top:12px;text-align:left;padding:12px">
                <div v-for="warning in coreReadinessWarnings" :key="'core-warning-' + warning" style="color:var(--warning);font-size:0.84rem;line-height:1.5">{{ warning }}</div>
              </div>
            </div>
          </div>
        </div>

        <!-- =================================================================
             Models Tab
             ================================================================= -->
        <div v-if="activeTab === 'models'" class="hc-settings-section">
          <div class="hc-settings-section-title">{{ t('settingsModelsTitle') }}</div>
          <div class="hc-settings-section-desc">{{ t('settingsModelsDesc') }}</div>
          <div style="margin-bottom:16px">
            <button class="hc-btn hc-btn-primary" data-testid="settings-models-toggle-form" @click="showAddForm = !showAddForm; editingProvider = null; validationResult = null; testResult = null; resetModelForm()">
              {{ showAddForm ? t('cancel') : '+ ' + t('settingsAddProvider') }}
            </button>
          </div>

          <div v-if="showAddForm || editingProvider" data-testid="settings-model-form" style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:16px">
            <div v-if="!editingProvider && providerPresetOptions().length > 0" class="hc-form-group" style="margin-bottom:12px">
              <label class="hc-form-label">{{ t('settingsProviderPreset') || 'Provider Preset' }}</label>
              <select class="hc-form-select" v-model="modelPresetID" @change="onModelPresetChange()">
                <option value="">{{ t('settingsSelectPreset') || '-- Select preset --' }}</option>
                <option v-for="preset in providerPresetOptions()" :key="preset.id" :value="preset.id">{{ preset.display_name || preset.id }}</option>
              </select>
              <div v-if="currentProviderPresetBadges().length" class="hc-result-chip-row" style="margin-top:8px">
                <span v-for="badge in currentProviderPresetBadges()" :key="'preset-cap-' + badge" class="hc-result-chip">{{ badge }}</span>
              </div>
            </div>
            <div class="hc-form-group" style="margin-bottom:12px">
              <label class="hc-form-label">{{ t('settingsProviderFormName') || 'Name' }}</label>
              <input class="hc-form-input" data-testid="settings-model-field-name" v-model="formName" :disabled="!!editingProvider" placeholder="e.g. openai" />
            </div>
            <div class="hc-form-group" style="margin-bottom:12px">
              <label class="hc-form-label">{{ t('settingsApiType') || 'API Type' }}</label>
              <select class="hc-form-select" data-testid="settings-model-field-api" v-model="formApi" @change="onModelApiTypeChange()">
                <option v-for="opt in apiTypeOptions" :key="opt" :value="opt">{{ opt }}</option>
              </select>
              <div v-if="currentProviderAPIBadges().length" class="hc-result-chip-row" style="margin-top:8px">
                <span v-for="badge in currentProviderAPIBadges()" :key="'api-cap-' + badge" class="hc-result-chip">{{ badge }}</span>
              </div>
            </div>

            <div v-for="field in currentProviderBasicFields" :key="field.key" class="hc-form-group" style="margin-bottom:12px">
                <label class="hc-form-label">{{ field.label }}{{ field.required ? ' *' : '' }}</label>
                <textarea
                  v-if="providerFieldIsTextarea(field)"
                  class="hc-form-input"
                  :data-testid="'settings-model-field-' + field.key"
                  :rows="providerFieldRows(field)"
                  :value="formProviderFieldValue(field)"
                  @input="setModelProviderFieldValue(field, $event.target.value)"
                  :placeholder="providerFieldPlaceholder(field)"
                  spellcheck="false"
                  style="resize:vertical;font-family:var(--font-mono)"
                ></textarea>
                <input
                  v-else
                  class="hc-form-input"
                  :data-testid="'settings-model-field-' + field.key"
                  :type="providerFieldInputType(field)"
                  :value="formProviderFieldValue(field)"
                  @input="setModelProviderFieldValue(field, $event.target.value)"
                  :placeholder="providerFieldPlaceholder(field)" />
                <label v-if="showProviderFieldClear(field)" class="hc-checkbox-label" style="margin-top:8px">
                  <input type="checkbox" :checked="providerFieldClearRequested(field)" @change="setProviderFieldClear(field, $event.target.checked)" />
                  <span>{{ providerFieldClearLabel(field) }}</span>
                </label>
                <div v-if="field.description" style="margin-top:4px;font-size:0.78rem;color:var(--ink3)">{{ field.description }}</div>
                <div v-if="field.key === 'api_key' && providerAPIKeyEnvHint()" style="margin-top:4px;font-size:0.78rem;color:var(--ink3)">{{ providerAPIKeyEnvHint() }}</div>
                <div v-if="providerFieldRetentionHint(field)" style="margin-top:4px;font-size:0.78rem;color:var(--ink3)">{{ providerFieldRetentionHint(field) }}</div>
            </div>

            <details v-if="currentProviderAdvancedFields.length" :open="providerAdvancedFieldsExpanded()" style="margin:4px 0 16px;border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:12px;background:var(--surface)">
              <summary style="cursor:pointer;font-weight:600;color:var(--ink2)">{{ t('settingsAdvancedConnectionOptions') || 'Advanced Connection Options' }}</summary>
              <div style="margin-top:12px">
                <div v-for="field in currentProviderAdvancedFields" :key="'advanced-' + field.key" class="hc-form-group" style="margin-bottom:12px">
                    <label class="hc-form-label">{{ field.label }}{{ field.required ? ' *' : '' }}</label>
                    <textarea
                      v-if="providerFieldIsTextarea(field)"
                      class="hc-form-input"
                      :data-testid="'settings-model-field-' + field.key"
                      :rows="providerFieldRows(field)"
                      :value="formProviderFieldValue(field)"
                      @input="setModelProviderFieldValue(field, $event.target.value)"
                      :placeholder="providerFieldPlaceholder(field)"
                      spellcheck="false"
                      style="resize:vertical;font-family:var(--font-mono)"
                    ></textarea>
                    <input
                      v-else
                      class="hc-form-input"
                      :data-testid="'settings-model-field-' + field.key"
                      :type="providerFieldInputType(field)"
                      :value="formProviderFieldValue(field)"
                      @input="setModelProviderFieldValue(field, $event.target.value)"
                      :placeholder="providerFieldPlaceholder(field)" />
                    <label v-if="showProviderFieldClear(field)" class="hc-checkbox-label" style="margin-top:8px">
                      <input type="checkbox" :checked="providerFieldClearRequested(field)" @change="setProviderFieldClear(field, $event.target.checked)" />
                      <span>{{ providerFieldClearLabel(field) }}</span>
                    </label>
                    <div v-if="field.description" style="margin-top:4px;font-size:0.78rem;color:var(--ink3)">{{ field.description }}</div>
                    <div v-if="field.key === 'api_key' && providerAPIKeyEnvHint()" style="margin-top:4px;font-size:0.78rem;color:var(--ink3)">{{ providerAPIKeyEnvHint() }}</div>
                    <div v-if="providerFieldRetentionHint(field)" style="margin-top:4px;font-size:0.78rem;color:var(--ink3)">{{ providerFieldRetentionHint(field) }}</div>
                </div>
              </div>
            </details>

            <div style="display:flex;gap:8px;flex-wrap:wrap">
              <button class="hc-btn hc-btn-primary" data-testid="settings-model-save" @click="saveProvider()">{{ t('save') }}</button>
              <button class="hc-btn hc-btn-secondary" data-testid="settings-model-validate" @click="validateForm()">{{ t('settingsValidateProvider') || 'Validate Provider' }}</button>
              <button class="hc-btn hc-btn-secondary" data-testid="settings-model-test" @click="testForm()">{{ t('settingsTestMessage') || 'Test Message' }}</button>
            </div>
          </div>

          <div v-if="validationResult" data-testid="settings-model-validation-result" :class="validationResult.valid ? 'hc-settings-validation valid' : 'hc-settings-validation invalid'" style="margin-bottom:12px">
            <span v-if="validationResult.valid">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><polyline points="20 6 9 17 4 12"/></svg>
            </span>
            <span v-else>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </span>
            {{ validationResult.message || (validationResult.valid ? (t('settingsValid') || 'Valid') : (t('settingsInvalid') || 'Invalid')) }}
          </div>

          <div v-if="testResult" data-testid="settings-model-test-result" :class="'hc-settings-validation' + (testResult.ok !== false ? ' valid' : ' invalid')" style="margin-bottom:12px">
            <div v-if="testResult.reply || testResult.content" style="margin-bottom:4px">{{ testResult.reply || testResult.content }}</div>
            <span v-if="testResult.latency_ms" style="font-size:0.78rem;color:var(--ink3)">{{ t('settingsLatency') || 'Latency' }}: {{ testResult.latency_ms }}ms</span>
            <span v-if="testResult.tokens" style="font-size:0.78rem;color:var(--ink3)"> {{ t('settingsTokens') || 'Tokens' }}: {{ testResult.tokens }}</span>
          </div>

          <div class="hc-settings-providers">
            <div v-if="modelsError" class="hc-empty" style="text-align:center">
              <div style="margin-bottom:8px">{{ t('loadError') }}</div>
              <div style="font-size:0.84rem;color:var(--ink3);margin-bottom:12px">{{ modelsError }}</div>
              <button class="hc-btn hc-btn-primary hc-btn-sm" @click="loadModels()">{{ t('retryLoad') }}</button>
            </div>
            <div v-else-if="modelsData.length === 0" class="hc-empty">{{ t('noData') }}</div>
            <div v-else>
              <div v-for="prov in modelsData" :key="prov.name" class="hc-settings-provider" :data-testid="'settings-model-row-' + prov.name">
                <div class="hc-settings-provider-icon">{{ (prov.name || '?').charAt(0).toUpperCase() }}</div>
                <div class="hc-settings-provider-info">
                  <div class="hc-settings-provider-name">{{ prov.name || '-' }}</div>
                  <div class="hc-settings-provider-model">
                    {{ prov.api || prov.api_type || 'openai' }}{{ prov.base_url ? ' | ' + prov.base_url : '' }}{{ prov.default_model ? ' | ' + prov.default_model : '' }}
                  </div>
                  <div v-if="providerFeatureBadges(prov).length" class="hc-result-chip-row" style="margin-top:6px">
                    <span v-for="badge in providerFeatureBadges(prov)" :key="(prov.name || 'provider') + '-cap-' + badge" class="hc-result-chip">{{ badge }}</span>
                  </div>
                  <span v-if="prov.has_key" class="hc-badge hc-badge-green" style="margin-top:4px;display:inline-block">{{ t('settingsKeyConfigured') || 'Key configured' }}</span>
                  <span v-if="prov.mutable === false" class="hc-badge hc-badge-gray" style="margin-top:4px;display:inline-block">{{ t('settingsReadOnlyItem') || 'Read-only effective item' }}</span>
                </div>
                <div v-if="prov.mutable !== false" class="hc-settings-provider-actions">
                  <button class="hc-btn hc-btn-sm hc-btn-secondary" @click="editProvider(prov)">{{ t('edit') }}</button>
                  <button class="hc-btn hc-btn-sm hc-btn-danger" @click="deleteProvider(prov.name)">{{ t('delete') }}</button>
                </div>
              </div>
            </div>
          </div>

          <div class="hc-settings-section" style="margin-top:24px">
            <div class="hc-settings-section-title" data-testid="settings-runtime-capability-packs">{{ t('settingsRuntimeCapabilityPacks') || 'Runtime Capability Packs' }}</div>
            <div class="hc-settings-section-desc">{{ t('settingsRuntimeCapabilityPacksDesc') || 'Live inventory from the extension registry. Builtin packs and plugin packs are shown together with source, delivery mode, and health.' }}</div>

            <div v-if="pluginsModules.length > 0" class="hc-result-chip-row" style="margin-bottom:12px">
              <span class="hc-result-chip">{{ pluginsModules.length }} total</span>
              <span v-for="chip in pluginModuleSummaryChips()" :key="'model-plugin-module-chip-' + chip" class="hc-result-chip">{{ chip }}</span>
            </div>

            <div v-if="pluginsModulesUnavailable" class="hc-empty" style="text-align:left">
              Capability pack inventory unavailable. The operator could not load runtime module metadata.
            </div>
            <div v-else-if="pluginsModules.length === 0" class="hc-empty">{{ t('settingsNoCapPacks') }}</div>
            <div v-else class="hc-table-wrap"><table class="hc-table">
              <thead><tr>
                <th>{{ t('settingsCapPackName') }}</th>
                <th>{{ t('settingsCapPackSource') }}</th>
                <th>{{ t('settingsCapPackHealth') }}</th>
                <th>{{ t('settingsCapPackDelivery') }}</th>
                <th>{{ t('settingsCapPackContributions') }}</th>
                <th>{{ t('settingsCapPackDetails') }}</th>
              </tr></thead>
              <tbody>
                <tr v-for="module in pagedPluginModules" :key="'models-' + pluginModuleKey(module)" :data-testid="'settings-capability-pack-row-' + pluginModuleKey(module)">
                  <td>
                    <strong>{{ pluginModuleName(module) }}</strong>
                    <div style="font-size:0.78rem;color:var(--ink3)">{{ pluginModuleKey(module) }}</div>
                  </td>
                  <td>
                    <span class="hc-badge" :class="pluginModuleSourceBadge(pluginModuleSource(module))">{{ pluginModuleSource(module) }}</span>
                  </td>
                  <td>
                    <span class="hc-badge" :class="resultBadge(pluginModuleHealthStatus(module))">{{ pluginModuleHealthStatus(module) }}</span>
                    <div v-if="pluginModuleHealthSummary(module)" style="font-size:0.78rem;color:var(--ink3);margin-top:4px">{{ pluginModuleHealthSummary(module) }}</div>
                  </td>
                  <td>{{ pluginModuleDelivery(module) || '-' }}</td>
                  <td>
                    <div v-if="pluginModuleContributionBadges(module).length" class="hc-result-chip-row">
                      <span v-for="badge in pluginModuleContributionBadges(module)" :key="'models-' + pluginModuleKey(module) + '-badge-' + badge" class="hc-result-chip">{{ badge }}</span>
                    </div>
                    <div v-if="pluginModuleContributionPreview(module)" style="font-size:0.78rem;color:var(--ink3);margin-top:4px">{{ pluginModuleContributionPreview(module) }}</div>
                    <div v-else-if="pluginModuleContributionTotal(module) === 0" style="font-size:0.78rem;color:var(--ink3)">{{ t('settingsCapPackNone') }}</div>
                  </td>
                  <td>
                    <div>{{ pluginModuleDescription(module) || '-' }}</div>
                    <div v-if="pluginModuleMetaSummary(module)" style="font-size:0.78rem;color:var(--ink3);margin-top:4px">{{ pluginModuleMetaSummary(module) }}</div>
                  </td>
                </tr>
              </tbody>
            </table>
            <div v-if="pluginModulesTotalPages > 1" class="hc-pagination">
              <span class="hc-pagination-info">{{ (pluginModulesPage-1)*Number(pluginModulesPageSize)+1 }}-{{ Math.min(pluginModulesPage*Number(pluginModulesPageSize), pluginsModules.length) }} {{ t('of') }} {{ pluginsModules.length }}</span>
              <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="pluginModulesPage<=1" @click="pluginModulesPage--">&#8249;</button>
              <span class="hc-pagination-pages">{{ pluginModulesPage }} / {{ pluginModulesTotalPages }}</span>
              <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="pluginModulesPage>=pluginModulesTotalPages" @click="pluginModulesPage++">&#8250;</button>
              <select class="hc-form-select hc-pagination-size" v-model="pluginModulesPageSize" @change="pluginModulesPage=1">
                <option value="20">20</option>
                <option value="50">50</option>
                <option value="100">100</option>
              </select>
            </div>
            </div>
          </div>
        </div>

        <!-- =================================================================
             Channels Tab
             ================================================================= -->
        <div v-if="activeTab === 'channels'" class="hc-settings-section">
          <div class="hc-settings-section-title">{{ t('settingsChannelsTitle') }}</div>
          <div class="hc-settings-section-desc">{{ t('settingsChannelsDesc') }}</div>

          <div style="margin-bottom:16px">
            <button class="hc-btn hc-btn-primary" data-testid="settings-channels-toggle-form" @click="toggleChannelForm()">
              {{ showChannelForm ? t('cancel') : t('settingsAddChannelBtn') || '+ Add Channel' }}
            </button>
          </div>

          <!-- Channel Add/Edit Form -->
          <div v-if="showChannelForm || editingChannel" data-testid="settings-channel-form" style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:16px">
            <div class="hc-form-group" style="margin-bottom:12px">
              <label class="hc-form-label">{{ t('settingsChannelNameLabel') || 'Channel Name' }} *</label>
              <input class="hc-form-input" data-testid="settings-channel-field-name" v-model="chFormName" :disabled="!!editingChannel" placeholder="e.g. slack" />
              <div v-if="!editingChannel" style="margin-top:6px;color:var(--text-secondary);font-size:12px">
                {{ t('settingsChannelNameHint') || 'For file-backed config, keep the name equal to the canonical channel id from setup catalog, usually the same as the selected type.' }}
              </div>
            </div>
            <div class="hc-form-group" style="margin-bottom:12px">
              <label class="hc-form-label">{{ t('settingsChannelTypeLabel') || 'Channel Type' }} *</label>
              <select class="hc-form-select" data-testid="settings-channel-field-type" v-model="chFormType" :disabled="!!editingChannel" @change="onChannelTypeChange()">
                <option value="">{{ t('settingsSelectType') || '-- Select type --' }}</option>
                <option v-for="ct in channelTypeOptions" :key="ct" :value="ct">{{ channelSchemaLabel(ct) }}</option>
              </select>
            </div>

            <div v-if="chFormType && currentChannelFields.length > 0">
              <div v-for="field in currentChannelFields" :key="field.key" class="hc-form-group" style="margin-bottom:12px">
                <label class="hc-form-label">{{ field.label }}{{ field.required ? ' *' : '' }}</label>
                <label v-if="field.type === 'bool'" style="display:flex;align-items:center;gap:8px;cursor:pointer">
                  <input type="checkbox"
                    :checked="chFormConfig[field.key] === true || chFormConfig[field.key] === 'true'"
                    @change="chFormConfig[field.key] = $event.target.checked" />
                  {{ field.label }}
                </label>
                <textarea v-else-if="field.type === 'string_list'" class="hc-form-input"
                  :data-testid="'settings-channel-field-' + field.key"
                  :rows="channelFieldRows(field)"
                  :value="channelFieldValue(field)"
                  @input="chFormConfig[field.key] = $event.target.value"
                  :placeholder="field.placeholder || ''"
                  spellcheck="false"
                  style="resize:vertical;font-family:var(--font-mono)"></textarea>
                <input v-else class="hc-form-input"
                  :data-testid="'settings-channel-field-' + field.key"
                  :type="field.type === 'password' ? 'password' : 'text'"
                  :value="channelFieldValue(field)"
                  @input="chFormConfig[field.key] = $event.target.value"
                  :placeholder="field.placeholder || ''" />
              </div>
            </div>

            <div class="hc-form-group" style="margin-bottom:12px">
              <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
                <input type="checkbox" :checked="chFormEnabled" @change="chFormEnabled = $event.target.checked" />
                {{ t('enabled') || 'Enabled' }}
              </label>
            </div>

            <div style="display:flex;gap:8px;flex-wrap:wrap">
              <button class="hc-btn hc-btn-primary" data-testid="settings-channel-save" @click="saveChannel()">{{ t('save') }}</button>
              <button class="hc-btn hc-btn-secondary" @click="cancelChannelForm()">{{ t('cancel') }}</button>
            </div>
          </div>

          <!-- Channel List -->
          <div v-if="channelsError" class="hc-empty" style="text-align:center">
            <div style="margin-bottom:8px">{{ t('loadError') }}</div>
            <div style="font-size:0.84rem;color:var(--ink3);margin-bottom:12px">{{ channelsError }}</div>
            <button class="hc-btn hc-btn-primary hc-btn-sm" @click="loadChannels()">{{ t('retryLoad') }}</button>
          </div>
          <div v-else-if="channelsList.length === 0 && channelsHealth.length === 0" class="hc-empty">{{ t('noData') }}</div>
          <div v-else class="hc-table-wrap"><table class="hc-table">
            <thead><tr>
              <th>{{ t('name') }}</th>
              <th>{{ t('type') }}</th>
              <th>{{ t('enabled') }}</th>
              <th>{{ t('settingsHealthColumn') || 'Health' }}</th>
              <th>{{ t('settingsPoliciesColumn') || 'Policies' }}</th>
              <th>{{ t('settingsFeaturesColumn') || 'Features' }}</th>
              <th>{{ t('settingsSourceColumn') || 'Source' }}</th>
              <th>{{ t('actions') }}</th>
            </tr></thead>
            <tbody>
              <tr v-for="ch in mergedChannels" :key="ch.name" :data-testid="'settings-channel-row-' + ch.name">
                <td><strong>{{ ch.name || '-' }}</strong></td>
                <td>{{ ch.type || '-' }}</td>
                <td>
                  <span class="hc-badge" :class="ch.enabled !== false ? 'hc-badge-green' : 'hc-badge-gray'">
                    {{ ch.enabled !== false ? t('yes') : t('no') }}
                  </span>
                </td>
                <td>
                  <span style="display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:6px;vertical-align:middle" :style="channelDotStyle(ch)"></span>
                  {{ ch.health_status || ch.status || ch.health || 'unknown' }}
                </td>
                <td style="font-size:0.8rem;min-width:180px">
                  <div>{{ formatChannelPolicies(ch) }}</div>
                </td>
                <td style="min-width:260px">
                  <div style="display:flex;gap:6px;flex-wrap:wrap">
                    <span v-for="tag in channelFeatureBadges(ch)" :key="tag" class="hc-badge hc-badge-gray">{{ tag }}</span>
                  </div>
                </td>
                <td>
                  <span class="hc-badge" :class="ch.source === 'yaml' ? 'hc-badge-gray' : 'hc-badge-blue'">{{ ch.source || 'api' }}</span>
                </td>
                <td style="white-space:nowrap">
                  <button class="hc-btn hc-btn-sm hc-btn-secondary" @click="editChannel(ch)">{{ t('edit') }}</button>
                  <button class="hc-btn hc-btn-sm hc-btn-secondary" @click="validateChannel(ch.name)">{{ t('settingsValidateChannel') || 'Validate' }}</button>
                  <button class="hc-btn hc-btn-sm hc-btn-secondary" @click="testChannel(ch.name)">{{ t('settingsTestChannel') || 'Test' }}</button>
                  <button class="hc-btn hc-btn-sm hc-btn-danger" @click="deleteChannel(ch.name)">{{ t('delete') }}</button>
                </td>
              </tr>
            </tbody>
          </table></div>

          <div v-if="channelsThreadBindings.length > 0" style="margin-top:16px">
            <div class="hc-settings-section-title" style="font-size:0.95rem">{{ t('settingsThreadBindings') || 'Thread Bindings' }}</div>
            <div class="hc-table-wrap"><table class="hc-table">
              <thead><tr><th>{{ t('settingsChannelLabel') || 'Channel' }}</th><th>{{ t('settingsThreadColumn') || 'Thread' }}</th><th>{{ t('settingsSessionColumn') || 'Session' }}</th><th>{{ t('actions') }}</th></tr></thead>
              <tbody>
                <tr v-for="item in channelsThreadBindings" :key="item.channel + ':' + item.thread_id">
                  <td>{{ item.channel }}</td>
                  <td style="font-family:var(--font-mono);font-size:0.82rem">{{ item.thread_id }}</td>
                  <td style="font-family:var(--font-mono);font-size:0.82rem">{{ item.session_key }}</td>
                  <td><button class="hc-btn hc-btn-sm hc-btn-danger" @click="deleteThreadBinding(item)">{{ t('delete') }}</button></td>
                </tr>
              </tbody>
            </table></div>
          </div>
        </div>

        <!-- =================================================================
             Skills Tab
             ================================================================= -->
        <div v-if="activeTab === 'skills'" class="hc-settings-section hc-skills-page">
          <div class="hc-card hc-skills-hero">
            <div class="hc-page-section-head">
              <div class="hc-page-section-copy">
                <div class="hc-settings-section-title">{{ t('settingsSkillsTitle') }}</div>
                <div class="hc-settings-section-desc">{{ t('settingsSkillsDesc') }}</div>
              </div>
              <div class="hc-page-section-actions">
                <button class="hc-btn hc-btn-secondary" @click="refreshSkillWorkspace()">{{ t('settingsRefreshLibrary') || 'Refresh library' }}</button>
                <button class="hc-btn hc-btn-secondary" @click="preflightSkills()">{{ t('settingsWorkspacePreflight') || 'Workspace preflight' }}</button>
              </div>
            </div>

            <div class="hc-skills-toolbar">
              <input class="hc-form-input" v-model="skillInstallSource" placeholder="install by skill id or local source" />
              <button class="hc-btn hc-btn-primary" @click="installSkill()">{{ t('settingsInstallSkill') }}</button>
              <input class="hc-form-input" v-model="skillQuery" placeholder="search catalog, tools, or use cases" @keydown.enter.prevent="loadSkillCatalog()" />
              <button class="hc-btn hc-btn-secondary" @click="loadSkillCatalog()">{{ t('search') }}</button>
            </div>

            <div class="hc-skills-stats">
              <div v-for="metric in skillMetrics()" :key="metric.label" class="hc-skills-stat">
                <div class="hc-skills-stat-label">{{ metric.label }}</div>
                <div class="hc-skills-stat-value">{{ metric.value }}</div>
                <div class="hc-skills-stat-note">{{ metric.note }}</div>
              </div>
            </div>
          </div>

          <div class="hc-skills-layout">
            <div class="hc-card hc-skills-sidebar">
              <div class="hc-skills-sidebar-head">
                <div>
                  <div class="hc-settings-section-title" style="font-size:0.95rem">{{ t('settingsSkillLibrary') || 'Skill library' }}</div>
                  <div class="hc-settings-section-desc">{{ t('settingsSkillLibraryDesc') || 'Installed skills, catalog candidates, readiness, and risk all live in one place.' }}</div>
                </div>
                <div class="hc-tabs hc-skills-filter-tabs">
                  <button class="hc-tab" :class="{ active: skillsLibraryMode === 'all' }" @click="skillsLibraryMode = 'all'">{{ t('settingsSkillFilterAll') || 'All' }}</button>
                  <button class="hc-tab" :class="{ active: skillsLibraryMode === 'installed' }" @click="skillsLibraryMode = 'installed'">{{ t('settingsSkillFilterInstalled') || 'Installed' }}</button>
                  <button class="hc-tab" :class="{ active: skillsLibraryMode === 'catalog' }" @click="skillsLibraryMode = 'catalog'">{{ t('settingsSkillFilterCatalog') || 'Catalog' }}</button>
                </div>
              </div>

              <div v-if="showInstalledSkills()" class="hc-skills-list-section">
                <div class="hc-skills-list-head">
                  <span>{{ t('settingsSkillInstalledHeading') || 'Installed' }}</span>
                  <span class="hc-result-chip">{{ skillsData.length }}</span>
                </div>
                <div v-if="skillsError" class="hc-empty hc-empty-compact">
                  <div style="margin-bottom:8px">{{ t('loadError') }}</div>
                  <div style="font-size:0.84rem;color:var(--ink3);margin-bottom:12px">{{ skillsError }}</div>
                  <button class="hc-btn hc-btn-primary hc-btn-sm" @click="loadSkills()">{{ t('retryLoad') || 'Retry' }}</button>
                </div>
                <div v-else-if="skillsData.length === 0" class="hc-empty hc-empty-compact">{{ t('settingsSkillNoInstalled') || 'No installed skills yet. Install one from the catalog to see readiness, tools, and risk labels here.' }}</div>
                <div v-else class="hc-skills-card-list">
                  <div
                    v-for="skill in pagedSkills"
                    :key="'installed-' + (skill.id || skill.name)"
                    class="hc-skill-card"
                    :class="{ active: skillIsSelected('installed', skill.id || skill.name) }"
                    @click="inspectSkill(skill.id || skill.name, 'installed')"
                  >
                    <div class="hc-skill-card-head">
                      <div>
                        <div class="hc-skill-card-title">{{ skill.name || skill.id || '-' }}</div>
                        <div class="hc-skill-card-subtitle">{{ skillCardSubtitle(skill) }}</div>
                      </div>
                      <span class="hc-badge" :class="skillStatusBadge(skill)">{{ formatStatusLabel(skill.status || (skill.ready ? 'ready' : 'unknown')) }}</span>
                    </div>
                    <div class="hc-result-chip-row">
                      <span class="hc-result-chip" :class="skillRiskChip(skill)">{{ skillRiskLabel(skill) }}</span>
                      <span class="hc-result-chip" :class="skillInstallabilityChip(skill)">{{ skillInstallabilityText(skill) }}</span>
                      <span v-if="skill.version" class="hc-result-chip">{{ skill.version }}</span>
                      <span v-if="skill.trust" class="hc-result-chip">{{ skill.trust }}</span>
                    </div>
                    <div v-if="skill.tools && skill.tools.length" class="hc-skill-card-tools">
                      <span v-for="tool in skill.tools.slice(0, 4)" :key="'tool-' + tool" class="hc-badge hc-badge-gray">{{ tool }}</span>
                    </div>
                    <div class="hc-skill-card-footer">
                      <span class="hc-result-deliverable-meta">{{ skillDetailMeta(skill) }}</span>
                      <div class="hc-skill-card-actions">
                        <button class="hc-btn hc-btn-sm hc-btn-secondary" @click.stop="inspectSkill(skill.id || skill.name, 'installed')">{{ t('settingsSkillInspect') }}</button>
                        <button class="hc-btn hc-btn-sm hc-btn-danger" @click.stop="deleteSkill(skill.id || skill.name)">{{ t('delete') }}</button>
                      </div>
                    </div>
                  </div>
                </div>
                <div v-if="skillsTotalPages > 1" class="hc-pagination">
                  <span class="hc-pagination-info">{{ (skillsPage-1)*Number(skillsPageSize)+1 }}-{{ Math.min(skillsPage*Number(skillsPageSize), skillsData.length) }} {{ t('of') }} {{ skillsData.length }}</span>
                  <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="skillsPage<=1" @click="skillsPage--">&#8249;</button>
                  <span class="hc-pagination-pages">{{ skillsPage }} / {{ skillsTotalPages }}</span>
                  <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="skillsPage>=skillsTotalPages" @click="skillsPage++">&#8250;</button>
                  <select class="hc-form-select hc-pagination-size" v-model="skillsPageSize" @change="skillsPage=1">
                    <option value="20">20</option>
                    <option value="50">50</option>
                    <option value="100">100</option>
                  </select>
                </div>
              </div>

              <div v-if="showCatalogSkills()" class="hc-skills-list-section">
                <div class="hc-skills-list-head">
                  <span>{{ t('settingsSkillCatalogHeading') || 'Catalog' }}</span>
                  <span class="hc-result-chip">{{ skillsCatalog.length }}</span>
                </div>
                <div v-if="skillsCatalogError" class="hc-empty hc-empty-compact">
                  <div style="margin-bottom:8px">{{ t('loadError') }}</div>
                  <div style="font-size:0.84rem;color:var(--ink3);margin-bottom:12px">{{ skillsCatalogError }}</div>
                  <button class="hc-btn hc-btn-primary hc-btn-sm" @click="loadSkillCatalog()">{{ t('retryLoad') || 'Retry' }}</button>
                </div>
                <div v-else-if="skillsCatalog.length === 0" class="hc-empty hc-empty-compact">{{ t('settingsSkillNoCatalog') || 'No catalog matches yet. Try searching by platform, use case, or tool family.' }}</div>
                <div v-else class="hc-skills-card-list">
                  <div
                    v-for="item in skillsCatalog"
                    :key="'catalog-' + item.id"
                    class="hc-skill-card"
                    :class="{ active: skillIsSelected('catalog', item.id) }"
                    @click="inspectSkill(item.id, 'catalog')"
                  >
                    <div class="hc-skill-card-head">
                      <div>
                        <div class="hc-skill-card-title">{{ item.name || item.id }}</div>
                        <div class="hc-skill-card-subtitle">{{ skillCardSubtitle(item) }}</div>
                      </div>
                      <span class="hc-badge" :class="item.installed ? 'hc-badge-green' : skillStatusBadge(item)">{{ item.installed ? 'Installed' : formatStatusLabel(item.installability && item.installability.label || 'catalog') }}</span>
                    </div>
                    <div class="hc-result-chip-row">
                      <span class="hc-result-chip" :class="skillRiskChip(item)">{{ skillRiskLabel(item) }}</span>
                      <span class="hc-result-chip" :class="skillInstallabilityChip(item)">{{ skillInstallabilityText(item) }}</span>
                      <span v-if="item.version" class="hc-result-chip">{{ item.version }}</span>
                    </div>
                    <div v-if="item.tools && item.tools.length" class="hc-skill-card-tools">
                      <span v-for="tool in item.tools.slice(0, 4)" :key="'catalog-tool-' + tool" class="hc-badge hc-badge-gray">{{ tool }}</span>
                    </div>
                    <div class="hc-skill-card-footer">
                      <span class="hc-result-deliverable-meta">{{ skillDetailMeta(item) }}</span>
                      <div class="hc-skill-card-actions">
                        <button class="hc-btn hc-btn-sm hc-btn-secondary" @click.stop="inspectSkill(item.id, 'catalog')">{{ t('settingsSkillInspect') }}</button>
                        <button v-if="!item.installed" class="hc-btn hc-btn-sm hc-btn-primary" @click.stop="installSkill(item.id)">{{ t('settingsInstallSkill') }}</button>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <div class="hc-skills-main">
              <div class="hc-card hc-skills-detail-card">
                <div class="hc-result-panel">
                  <div class="hc-result-panel-header">
                    <div class="hc-result-panel-copy">
                      <div class="hc-result-panel-title">{{ skillInspectPanel().title }}</div>
                      <div v-if="skillInspectPanel().summary" class="hc-result-panel-summary">{{ skillInspectPanel().summary }}</div>
                    </div>
                    <div class="hc-result-panel-badges">
                      <span v-if="skillInspectLoading" class="hc-badge hc-badge-blue hc-badge-pulse">{{ t('settingsSkillLoading') }}</span>
                      <span v-else-if="skillInspectScope" class="hc-badge hc-badge-gray">{{ skillInspectScope === 'catalog' ? 'Catalog detail' : 'Installed detail' }}</span>
                      <span v-if="skillInspectPanel().status" class="hc-badge" :class="resultBadge(skillInspectPanel().status)">{{ formatStatusLabel(skillInspectPanel().status) }}</span>
                    </div>
                  </div>

                  <div v-if="skillInspectError" class="hc-run-detail-error"><strong>Error:</strong> {{ skillInspectError }}</div>
                  <div v-else-if="!skillInspectLoading && !skillInspectPanel().hasContent" class="hc-empty" style="margin:0">{{ t('settingsSkillEmptyInspect') }}</div>
                  <div v-else>
                    <div class="hc-skills-detail-actions" v-if="skillInspectDetail">
                      <button v-if="skillInspectScope === 'catalog' && !skillInspectDetail.installed" class="hc-btn hc-btn-primary" @click="installSkill(skillInspectDetail.skill_id || skillInspectName)">{{ t('settingsSkillInstallThis') }}</button>
                      <button v-if="skillInspectScope === 'installed'" class="hc-btn hc-btn-danger" @click="deleteSkill(skillInspectDetail.skill_id || skillInspectName)">{{ t('delete') }}</button>
                      <a v-if="skillInspectDetail.homepage" class="hc-btn hc-btn-secondary" :href="resultActionHref({ kind: 'open_url', target: skillInspectDetail.homepage })" target="_blank" rel="noopener noreferrer">{{ t('settingsSkillOpenHomepage') }}</a>
                    </div>

                    <div v-if="skillInspectDetail" class="hc-skills-detail-metrics">
                      <div v-for="metric in skillDetailMetrics(skillInspectDetail)" :key="metric.label" class="hc-skills-detail-metric">
                        <div class="hc-skills-detail-metric-label">{{ metric.label }}</div>
                        <div class="hc-skills-detail-metric-value">{{ metric.value }}</div>
                        <div class="hc-skills-detail-metric-note">{{ metric.note }}</div>
                      </div>
                    </div>

                    <div v-if="skillInspectDetail && skillInspectDetail.risk && skillInspectDetail.risk.tags && skillInspectDetail.risk.tags.length" class="hc-result-section">
                      <div class="hc-result-section-head">
                        <div class="hc-result-block-title">{{ t('settingsSkillRiskLabels') }}</div>
                        <div class="hc-result-deliverable-meta">{{ skillInspectDetail.risk.level || 'low' }}</div>
                      </div>
                      <div class="hc-result-chip-row">
                        <span v-for="tag in skillInspectDetail.risk.tags" :key="'risk-' + tag" class="hc-result-chip" :class="skillRiskChip(skillInspectDetail)">{{ formatStatusLabel(tag) }}</span>
                      </div>
                    </div>

                    <div v-if="skillInspectDetail && skillInspectDetail.tools && skillInspectDetail.tools.length" class="hc-result-section">
                      <div class="hc-result-section-head">
                        <div class="hc-result-block-title">{{ t('settingsSkillExportedTools') }}</div>
                        <div class="hc-result-deliverable-meta">{{ skillInspectDetail.tools.length }} item(s)</div>
                      </div>
                      <div class="hc-result-deliverable-grid">
                        <div v-for="tool in skillInspectDetail.tools" :key="'skill-tool-' + tool.name" class="hc-result-deliverable-card">
                          <div class="hc-result-deliverable-head">
                            <div>
                              <div class="hc-result-deliverable-name">{{ tool.name || 'tool' }}</div>
                              <div class="hc-result-deliverable-sub">{{ tool.side_effect_class || 'tool' }}</div>
                            </div>
                            <span class="hc-badge" :class="tool.requires_approval ? 'hc-badge-orange' : 'hc-badge-green'">{{ tool.requires_approval ? 'Approval' : 'Direct' }}</span>
                          </div>
                          <div v-if="tool.description" class="hc-result-deliverable-preview">{{ truncatePreview(tool.description) }}</div>
                          <div class="hc-result-chip-row">
                            <span v-if="tool.execution_key" class="hc-result-chip">{{ tool.execution_key }}</span>
                            <span v-if="tool.timeout" class="hc-result-chip">{{ tool.timeout }}</span>
                          </div>
                        </div>
                      </div>
                    </div>

                    <div v-if="skillInspectDetail && skillInspectDetail.checks && skillInspectDetail.checks.length" class="hc-result-section">
                      <div class="hc-result-section-head">
                        <div class="hc-result-block-title">{{ t('settingsSkillDependencyReadiness') }}</div>
                        <div class="hc-result-deliverable-meta">{{ skillInspectDetail.checks.length }} check(s)</div>
                      </div>
                      <div class="hc-result-deliverable-grid">
                        <div v-for="check in skillInspectDetail.checks" :key="'skill-check-' + (check.kind || '') + '-' + (check.name || '')" class="hc-result-deliverable-card">
                          <div class="hc-result-deliverable-head">
                            <div>
                              <div class="hc-result-deliverable-name">{{ check.name || formatStatusLabel(check.kind || 'check') }}</div>
                              <div class="hc-result-deliverable-sub">{{ check.kind || 'check' }}</div>
                            </div>
                            <span class="hc-badge" :class="dependencyBadge(check)">{{ formatStatusLabel(check.status || (check.present ? 'satisfied' : 'missing')) }}</span>
                          </div>
                          <div v-if="check.message || check.hint" class="hc-result-deliverable-preview">{{ truncatePreview(check.message || check.hint) }}</div>
                          <div v-if="check.path || check.source" class="hc-result-deliverable-meta">{{ check.path || check.source }}</div>
                        </div>
                      </div>
                    </div>

                    <div v-if="skillInspectPanel().actions.length" class="hc-result-section">
                      <div class="hc-result-section-head">
                        <div class="hc-result-block-title">{{ t("settingsSkillActions") }}</div>
                        <div class="hc-result-deliverable-meta">{{ skillInspectPanel().actions.length }} item(s)</div>
                      </div>
                      <div class="hc-result-action-grid">
                        <div v-for="(action, idx) in skillInspectPanel().actions" :key="'sia-'+idx" class="hc-result-action-card">
                          <div class="hc-result-action-head">
                            <div>
                              <div class="hc-result-deliverable-name">{{ resultActionTitle(action) }}</div>
                              <div class="hc-result-deliverable-sub">{{ action.kind || 'action' }}</div>
                            </div>
                          </div>
                          <div v-if="resultActionDescription(action)" class="hc-result-action-body">{{ resultActionDescription(action) }}</div>
                          <div class="hc-result-deliverable-actions">
                            <a v-if="resultActionHref(action)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="resultActionHref(action)" target="_blank" rel="noopener noreferrer">{{ t('settingsSkillOpen') }}</a>
                            <span v-else-if="action.target" class="hc-result-deliverable-uri">{{ action.target }}</span>
                          </div>
                        </div>
                      </div>
                    </div>

                    <div v-if="skillInspectPanel().blocks.length" class="hc-result-section">
                      <div class="hc-result-section-head">
                        <div class="hc-result-block-title">{{ t("settingsSkillBlocks") }}</div>
                        <div class="hc-result-deliverable-meta">{{ skillInspectPanel().blocks.length }} item(s)</div>
                      </div>
                      <div class="hc-result-block-list">
                        <div v-for="(block, idx) in skillInspectPanel().blocks" :key="'sib-'+idx" class="hc-result-block">
                          <div class="hc-result-block-title">{{ block.title || formatStatusLabel(block.kind || 'summary') }}</div>
                          <div v-if="resultBlockText(block)" class="hc-result-block-content">{{ resultBlockText(block) }}</div>
                          <pre v-if="resultBlockData(block)" class="hc-run-detail-tool-args">{{ resultBlockData(block) }}</pre>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <div class="hc-card hc-skills-tooltest-card">
                <div class="hc-settings-section-title" style="font-size:0.95rem">{{ t('settingsToolTest') || 'Tool Test' }}</div>
                <div class="hc-settings-section-desc">{{ t('settingsToolTestDesc') || 'Validate the exact Blocks / Artifacts / Actions contract a tool returns before you publish or demo it.' }}</div>
                <div class="hc-settings-tooltest-form">
                  <div class="hc-form-group">
                    <label class="hc-form-label">{{ t('settingsToolName') }}</label>
                    <input class="hc-form-input" v-model="toolTestTool" placeholder="e.g. fs.list" @keydown.enter.prevent="runToolTest()" />
                  </div>
                  <div class="hc-form-group">
                    <label class="hc-form-label">{{ t('settingsToolSessionKey') }}</label>
                    <input class="hc-form-input" v-model="toolTestSessionKey" placeholder="session key for contextual tool resolution" @keydown.enter.prevent="runToolTest()" />
                  </div>
                  <div class="hc-form-group hc-settings-tooltest-input">
                    <label class="hc-form-label">JSON Input</label>
                    <textarea class="hc-form-input" v-model="toolTestInput" spellcheck="false"></textarea>
                  </div>
                  <div style="display:flex;gap:8px;flex-wrap:wrap">
                    <button class="hc-btn hc-btn-primary" @click="runToolTest()" :disabled="toolTestLoading">{{ toolTestLoading ? 'Running...' : 'Run tool test' }}</button>
                    <button class="hc-btn hc-btn-secondary" @click="resetToolTest()">Reset</button>
                  </div>
                </div>

                <div class="hc-result-panel" style="margin-top:16px">
                  <div class="hc-result-panel-header">
                    <div class="hc-result-panel-copy">
                      <div class="hc-result-panel-title">{{ toolTestPanel().title }}</div>
                      <div v-if="toolTestPanel().summary" class="hc-result-panel-summary">{{ toolTestPanel().summary }}</div>
                    </div>
                    <div class="hc-result-panel-badges">
                      <span v-if="toolTestLoading" class="hc-badge hc-badge-blue hc-badge-pulse">{{ t('settingsToolRunning') }}</span>
                      <span v-else-if="toolTestPanel().status" class="hc-badge" :class="resultBadge(toolTestPanel().status)">{{ formatStatusLabel(toolTestPanel().status) }}</span>
                    </div>
                  </div>
                  <div v-if="toolTestError" class="hc-run-detail-error"><strong>Error:</strong> {{ toolTestError }}</div>
                  <div v-else-if="!toolTestLoading && !toolTestPanel().hasContent" class="hc-empty" style="margin:0">{{ t('settingsToolEmptyTest') }}</div>
                  <div v-else>
                    <div v-if="toolTestPanel().actions.length" class="hc-result-section">
                      <div class="hc-result-section-head">
                        <div class="hc-result-block-title">{{ t("settingsSkillActions") }}</div>
                        <div class="hc-result-deliverable-meta">{{ toolTestPanel().actions.length }} item(s)</div>
                      </div>
                      <div class="hc-result-action-grid">
                        <div v-for="(action, idx) in toolTestPanel().actions" :key="'tta-'+idx" class="hc-result-action-card">
                          <div class="hc-result-action-head">
                            <div>
                              <div class="hc-result-deliverable-name">{{ resultActionTitle(action) }}</div>
                              <div class="hc-result-deliverable-sub">{{ action.kind || 'action' }}</div>
                            </div>
                          </div>
                          <div v-if="resultActionDescription(action)" class="hc-result-action-body">{{ resultActionDescription(action) }}</div>
                          <div class="hc-result-deliverable-actions">
                            <a v-if="resultActionHref(action)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="resultActionHref(action)" target="_blank" rel="noopener noreferrer">{{ t('settingsSkillOpen') }}</a>
                            <span v-else-if="action.target" class="hc-result-deliverable-uri">{{ action.target }}</span>
                          </div>
                        </div>
                      </div>
                    </div>
                    <div v-if="toolTestPanel().blocks.length" class="hc-result-section">
                      <div class="hc-result-section-head">
                        <div class="hc-result-block-title">{{ t("settingsSkillBlocks") }}</div>
                        <div class="hc-result-deliverable-meta">{{ toolTestPanel().blocks.length }} item(s)</div>
                      </div>
                      <div class="hc-result-block-list">
                        <div v-for="(block, idx) in toolTestPanel().blocks" :key="'ttb-'+idx" class="hc-result-block">
                          <div class="hc-result-block-title">{{ block.title || formatStatusLabel(block.kind || 'summary') }}</div>
                          <div v-if="resultBlockText(block)" class="hc-result-block-content">{{ resultBlockText(block) }}</div>
                          <pre v-if="resultBlockData(block)" class="hc-run-detail-tool-args">{{ resultBlockData(block) }}</pre>
                        </div>
                      </div>
                    </div>
                    <div v-if="toolTestPanel().artifacts.length" class="hc-result-section">
                      <div class="hc-result-section-head">
                        <div class="hc-result-block-title">{{ t("settingsToolArtifacts") }}</div>
                        <div class="hc-result-deliverable-meta">{{ toolTestPanel().artifacts.length }} item(s)</div>
                      </div>
                      <div class="hc-result-deliverable-grid">
                        <div v-for="(item, idx) in toolTestPanel().artifacts" :key="'ttd-'+idx" class="hc-result-deliverable-card">
                          <div class="hc-result-deliverable-head">
                            <div>
                              <div class="hc-result-deliverable-name">{{ item.name || item.label || item.kind || 'artifact' }}</div>
                              <div class="hc-result-deliverable-sub">{{ item.kind || 'artifact' }}</div>
                            </div>
                            <span v-if="item.size_bytes" class="hc-result-deliverable-meta">{{ formatFileSize(item.size_bytes) }}</span>
                          </div>
                          <div v-if="item.content_type" class="hc-result-deliverable-meta">{{ item.content_type }}</div>
                          <div v-if="artifactPreviewText(item)" class="hc-result-deliverable-preview">{{ truncatePreview(artifactPreviewText(item)) }}</div>
                          <div class="hc-result-deliverable-actions">
                            <a v-if="artifactPreviewHref(item)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="artifactPreviewHref(item)" target="_blank" rel="noopener noreferrer">{{ t('settingsToolOpenPreview') }}</a>
                            <span v-else-if="item.uri" class="hc-result-deliverable-uri">{{ item.uri }}</span>
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- =================================================================
             Plugins Tab
             ================================================================= -->
        <div v-if="activeTab === 'plugins'" class="hc-settings-section">
          <div class="hc-settings-section-title">{{ t('settingsPluginsTitle') }}</div>
          <div class="hc-settings-section-desc">{{ t('settingsPluginsDesc') }}</div>

          <div style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:16px;display:grid;grid-template-columns:minmax(0,2fr) auto auto;gap:8px;align-items:center">
            <input class="hc-form-input" v-model="pluginSource" placeholder="registry name / github url / local path" />
            <button class="hc-btn hc-btn-primary" @click="installPlugin()">{{ t('settingsInstallPlugin') }}</button>
            <button class="hc-btn hc-btn-secondary" @click="loadPlugins()">{{ t('refresh') }}</button>
          </div>

          <div class="hc-settings-section" style="margin-top:24px">
            <div class="hc-settings-section-title" data-testid="settings-installed-plugins">{{ t('settingsInstalledPlugins') || 'Installed Plugins' }}</div>
            <div class="hc-settings-section-desc">{{ t('settingsInstalledPluginsDesc') || 'Manage manifest plugins that were added from a registry, GitHub URL, or local path.' }}</div>

            <div v-if="pluginsError" class="hc-empty" style="text-align:center">
              <div style="margin-bottom:8px">{{ t('loadError') }}</div>
              <div style="font-size:0.84rem;color:var(--ink3);margin-bottom:12px">{{ pluginsError }}</div>
              <button class="hc-btn hc-btn-primary hc-btn-sm" @click="loadPlugins()">{{ t('retryLoad') }}</button>
            </div>
            <div v-else-if="pluginsData.length === 0" class="hc-empty">{{ t('noData') }}</div>
            <div v-else class="hc-table-wrap"><table class="hc-table">
              <thead><tr>
                <th>{{ t('name') }}</th>
                <th>{{ t('settingsSkillVersion') }}</th>
                <th>{{ t('description') }}</th>
                <th>{{ t('actions') }}</th>
              </tr></thead>
              <tbody>
                <tr v-for="plugin in pagedPlugins" :key="plugin.name" :data-testid="'settings-plugin-row-' + plugin.name">
                  <td>
                    <strong>{{ plugin.name || '-' }}</strong>
                    <div style="font-size:0.78rem;color:var(--ink3)">{{ plugin.dir || '' }}</div>
                  </td>
                  <td>{{ plugin.version || '-' }}</td>
                  <td>
                    <div>{{ plugin.description || '-' }}</div>
                    <div v-if="pluginComponentSummary(plugin)" style="font-size:0.78rem;color:var(--ink3);margin-top:4px">{{ pluginComponentSummary(plugin) }}</div>
                    <div v-if="pluginRuntimeModuleSummary(plugin)" style="font-size:0.78rem;color:var(--ink3);margin-top:4px">{{ pluginRuntimeModuleSummary(plugin) }}</div>
                    <div v-if="pluginRuntimeModuleHealthSummary(plugin)" style="font-size:0.78rem;color:var(--ink3);margin-top:4px">{{ pluginRuntimeModuleHealthSummary(plugin) }}</div>
                  </td>
                  <td>
                    <button class="hc-btn hc-btn-sm hc-btn-danger" @click="deletePlugin(plugin.name)">{{ t('delete') }}</button>
                  </td>
                </tr>
              </tbody>
            </table>
            <div v-if="pluginsTotalPages > 1" class="hc-pagination">
              <span class="hc-pagination-info">{{ (pluginsPage-1)*Number(pluginsPageSize)+1 }}-{{ Math.min(pluginsPage*Number(pluginsPageSize), pluginsData.length) }} {{ t('of') }} {{ pluginsData.length }}</span>
              <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="pluginsPage<=1" @click="pluginsPage--">&#8249;</button>
              <span class="hc-pagination-pages">{{ pluginsPage }} / {{ pluginsTotalPages }}</span>
              <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="pluginsPage>=pluginsTotalPages" @click="pluginsPage++">&#8250;</button>
              <select class="hc-form-select hc-pagination-size" v-model="pluginsPageSize" @change="pluginsPage=1">
                <option value="20">20</option>
                <option value="50">50</option>
                <option value="100">100</option>
              </select>
            </div>
            </div>
          </div>

          <div class="hc-settings-section" style="margin-top:24px">
            <div class="hc-settings-section-title" data-testid="settings-runtime-capability-packs">{{ t('settingsRuntimeCapabilityPacks') || 'Runtime Capability Packs' }}</div>
            <div class="hc-settings-section-desc">{{ t('settingsRuntimeCapabilityPacksDesc') || 'Live inventory from the extension registry. Builtin packs and plugin packs are shown together with source, delivery mode, and health.' }}</div>

            <div v-if="pluginsModules.length > 0" class="hc-result-chip-row" style="margin-bottom:12px">
              <span class="hc-result-chip">{{ pluginsModules.length }} total</span>
              <span v-for="chip in pluginModuleSummaryChips()" :key="'plugin-module-chip-' + chip" class="hc-result-chip">{{ chip }}</span>
            </div>

            <div v-if="pluginsModulesUnavailable" class="hc-empty" style="text-align:left">
              Capability pack inventory unavailable. The operator could not load runtime module metadata.
            </div>
            <div v-else-if="pluginsModules.length === 0" class="hc-empty">{{ t('settingsNoCapPacks') }}</div>
            <div v-else class="hc-table-wrap"><table class="hc-table">
              <thead><tr>
                <th>{{ t('settingsCapPackName') }}</th>
                <th>{{ t('settingsCapPackSource') }}</th>
                <th>{{ t('settingsCapPackHealth') }}</th>
                <th>{{ t('settingsCapPackDelivery') }}</th>
                <th>{{ t('settingsCapPackContributions') }}</th>
                <th>{{ t('settingsCapPackDetails') }}</th>
              </tr></thead>
              <tbody>
                <tr v-for="module in pagedPluginModules" :key="pluginModuleKey(module)" :data-testid="'settings-capability-pack-row-' + pluginModuleKey(module)">
                  <td>
                    <strong>{{ pluginModuleName(module) }}</strong>
                    <div style="font-size:0.78rem;color:var(--ink3)">{{ pluginModuleKey(module) }}</div>
                  </td>
                  <td>
                    <span class="hc-badge" :class="pluginModuleSourceBadge(pluginModuleSource(module))">{{ pluginModuleSource(module) }}</span>
                  </td>
                  <td>
                    <span class="hc-badge" :class="resultBadge(pluginModuleHealthStatus(module))">{{ pluginModuleHealthStatus(module) }}</span>
                    <div v-if="pluginModuleHealthSummary(module)" style="font-size:0.78rem;color:var(--ink3);margin-top:4px">{{ pluginModuleHealthSummary(module) }}</div>
                  </td>
                  <td>{{ pluginModuleDelivery(module) || '-' }}</td>
                  <td>
                    <div v-if="pluginModuleContributionBadges(module).length" class="hc-result-chip-row">
                      <span v-for="badge in pluginModuleContributionBadges(module)" :key="pluginModuleKey(module) + '-badge-' + badge" class="hc-result-chip">{{ badge }}</span>
                    </div>
                    <div v-if="pluginModuleContributionPreview(module)" style="font-size:0.78rem;color:var(--ink3);margin-top:4px">{{ pluginModuleContributionPreview(module) }}</div>
                    <div v-else-if="pluginModuleContributionTotal(module) === 0" style="font-size:0.78rem;color:var(--ink3)">{{ t('settingsCapPackNone') }}</div>
                  </td>
                  <td>
                    <div>{{ pluginModuleDescription(module) || '-' }}</div>
                    <div v-if="pluginModuleMetaSummary(module)" style="font-size:0.78rem;color:var(--ink3);margin-top:4px">{{ pluginModuleMetaSummary(module) }}</div>
                  </td>
                </tr>
              </tbody>
            </table>
            <div v-if="pluginModulesTotalPages > 1" class="hc-pagination">
              <span class="hc-pagination-info">{{ (pluginModulesPage-1)*Number(pluginModulesPageSize)+1 }}-{{ Math.min(pluginModulesPage*Number(pluginModulesPageSize), pluginsModules.length) }} {{ t('of') }} {{ pluginsModules.length }}</span>
              <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="pluginModulesPage<=1" @click="pluginModulesPage--">&#8249;</button>
              <span class="hc-pagination-pages">{{ pluginModulesPage }} / {{ pluginModulesTotalPages }}</span>
              <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="pluginModulesPage>=pluginModulesTotalPages" @click="pluginModulesPage++">&#8250;</button>
              <select class="hc-form-select hc-pagination-size" v-model="pluginModulesPageSize" @change="pluginModulesPage=1">
                <option value="20">20</option>
                <option value="50">50</option>
                <option value="100">100</option>
              </select>
            </div>
            </div>
          </div>
        </div>

        <!-- =================================================================
             Browser Tab
             ================================================================= -->
        <div v-if="activeTab === 'browser'" class="hc-settings-section">
          <div class="hc-settings-section-title">{{ t('settingsBrowserTitle') }}</div>
          <div class="hc-settings-section-desc">{{ t('settingsBrowserDesc') }}</div>

          <div style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:16px">
            <h4 style="margin:0 0 12px;font-size:0.9rem;color:var(--ink)">{{ t('settingsBrowserHelper') || 'Browser Helper' }}</h4>
            <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px">
              <label class="hc-form-label" style="display:flex;align-items:center;gap:8px">
                <input type="checkbox" v-model="browserHostForm.enabled" />
                <span>{{ t('settingsBrowserEnabled') }}</span>
              </label>
              <label class="hc-form-label" style="display:flex;align-items:center;gap:8px">
                <input type="checkbox" v-model="browserHostForm.headless" />
                <span>{{ t('settingsBrowserHeadless') }}</span>
              </label>
              <label class="hc-form-label" style="display:flex;align-items:center;gap:8px">
                <input type="checkbox" v-model="browserHostForm.no_sandbox" />
                <span>{{ t('settingsBrowserNoSandbox') }}</span>
              </label>
            </div>
            <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:12px;margin-top:12px">
              <div class="hc-form-group">
                <label class="hc-form-label">{{ t('settingsBrowserBaseUrl') }}</label>
                <input class="hc-form-input" v-model="browserHostForm.base_url" placeholder="http://127.0.0.1:9223" />
              </div>
              <div class="hc-form-group">
                <label class="hc-form-label">{{ t('settingsBrowserChromePath') }}</label>
                <input class="hc-form-input" v-model="browserHostForm.chrome_path" placeholder="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" />
              </div>
              <div class="hc-form-group">
                <label class="hc-form-label">{{ t('settingsBrowserAuthToken') }}</label>
                <input class="hc-form-input" v-model="browserHostForm.auth_token" placeholder="optional token" />
              </div>
              <div class="hc-form-group">
                <label class="hc-form-label">{{ t('settingsBrowserIdleTimeout') }}</label>
                <input class="hc-form-input" v-model="browserHostForm.idle_timeout" placeholder="90s" />
              </div>
            </div>
            <div style="display:flex;gap:8px;flex-wrap:wrap;margin-top:12px">
              <button class="hc-btn hc-btn-primary" @click="saveBrowserHostSettings()">{{ t('settingsBrowserSaveHelper') }}</button>
              <button class="hc-btn hc-btn-secondary" @click="loadBrowserHostSettings()">{{ t('settingsBrowserReloadHelper') }}</button>
            </div>
          </div>

          <div style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:16px">
            <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:8px;align-items:center">
              <input class="hc-form-input" v-model="browserProfileName" placeholder="profile name" />
              <input class="hc-form-input" v-model="browserProfileColor" placeholder="#4A90D9" />
              <input class="hc-form-input" v-model="browserProfileCDP" placeholder="optional remote CDP URL" />
            </div>
            <div style="display:flex;gap:8px;flex-wrap:wrap;margin-top:12px">
              <button class="hc-btn hc-btn-primary" @click="createBrowserProfile()">{{ t('create') }}</button>
              <button class="hc-btn hc-btn-secondary" @click="loadBrowserProfiles()">{{ t('refresh') }}</button>
            </div>
          </div>

          <div v-if="browserError" class="hc-empty" style="text-align:center">
            <div style="margin-bottom:8px">{{ t('loadError') }}</div>
            <div style="font-size:0.84rem;color:var(--ink3);margin-bottom:12px">{{ browserError }}</div>
            <button class="hc-btn hc-btn-primary hc-btn-sm" @click="loadBrowserProfiles()">{{ t('retryLoad') }}</button>
          </div>
          <div v-else-if="browserProfiles.length === 0" class="hc-empty">{{ t('noData') }}</div>
          <div v-else class="hc-table-wrap"><table class="hc-table">
            <thead><tr>
              <th>{{ t('name') }}</th>
              <th>{{ t('type') }}</th>
              <th>{{ t('settingsProfileColor') }}</th>
              <th>{{ t('settingsProfileCreated') }}</th>
              <th>{{ t('actions') }}</th>
            </tr></thead>
            <tbody>
              <tr v-for="profile in pagedBrowserProfiles" :key="profile.name">
                <td>
                  <strong>{{ profile.name }}</strong>
                  <div style="font-size:0.78rem;color:var(--ink3)">{{ profile.cdp_url || '' }}</div>
                </td>
                <td>{{ profile.driver || '-' }}</td>
                <td><span :style="'display:inline-block;width:10px;height:10px;border-radius:50%;background:' + (profile.color || '#999') + ';margin-right:8px'"></span>{{ profile.color || '-' }}</td>
                <td>{{ fmtTime(profile.created_at) }}</td>
                <td>
                  <button class="hc-btn hc-btn-sm hc-btn-danger" @click="deleteBrowserProfile(profile.name)">{{ t('delete') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
          <div v-if="browserProfilesTotalPages > 1" class="hc-pagination">
            <span class="hc-pagination-info">{{ (browserProfilesPage-1)*Number(browserProfilesPageSize)+1 }}-{{ Math.min(browserProfilesPage*Number(browserProfilesPageSize), browserProfiles.length) }} {{ t('of') }} {{ browserProfiles.length }}</span>
            <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="browserProfilesPage<=1" @click="browserProfilesPage--">&#8249;</button>
            <span class="hc-pagination-pages">{{ browserProfilesPage }} / {{ browserProfilesTotalPages }}</span>
            <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="browserProfilesPage>=browserProfilesTotalPages" @click="browserProfilesPage++">&#8250;</button>
            <select class="hc-form-select hc-pagination-size" v-model="browserProfilesPageSize" @change="browserProfilesPage=1">
              <option value="20">20</option>
              <option value="50">50</option>
              <option value="100">100</option>
            </select>
          </div>
          </div>
        </div>

        <!-- =================================================================
             Security Tab
             ================================================================= -->
        <div v-if="activeTab === 'security'" class="hc-settings-section">
          <div class="hc-settings-section-title">{{ t('settingsSecurityTitle') }}</div>
          <div class="hc-settings-section-desc">{{ t('settingsSecurityDesc') }}</div>

          <!-- (a) Exec Approval Mode -->
          <div style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:12px">
            <h4 style="margin:0 0 12px;font-size:0.9rem;color:var(--ink)">{{ t('settingsExecApprovalMode') || 'Exec Approval Mode' }}</h4>
            <div class="hc-form-group" style="margin-bottom:12px">
              <label class="hc-form-label">{{ t('settingsSecurityExecApproval') }}</label>
              <select class="hc-form-select" data-testid="settings-security-field-exec-mode" v-model="secForm.exec_mode">
                <option v-for="opt in execApprovalOptions" :key="opt" :value="opt">{{ opt }}</option>
              </select>
            </div>

            <div v-if="secForm.exec_mode === 'allowlist'">
              <div class="hc-form-group" style="margin-bottom:8px">
                <label class="hc-form-label">{{ t('settingsSecurityAllowedRoots') }}</label>
                <div v-for="(pat, idx) in secForm.allowlist" :key="idx" style="display:flex;gap:6px;margin-bottom:4px;align-items:center">
                  <input class="hc-form-input" :value="pat" @input="secForm.allowlist[idx] = $event.target.value" style="flex:1" />
                  <button class="hc-btn hc-btn-sm hc-btn-danger" @click="secForm.allowlist.splice(idx, 1)">x</button>
                </div>
                <button class="hc-btn hc-btn-sm hc-btn-secondary" @click="secForm.allowlist.push('')" style="margin-top:4px">+ Add pattern</button>
              </div>
            </div>

            <div v-if="secForm.exec_mode === 'approve'">
              <div class="hc-form-group" style="margin-bottom:8px">
                <label class="hc-form-label">{{ t('settingsSecurityApprovalTimeout') }}</label>
                <input class="hc-form-input" data-testid="settings-security-field-approval-timeout" type="number" v-model="secForm.approval_timeout" placeholder="300" style="max-width:200px" />
              </div>
              <div class="hc-form-group" style="margin-bottom:8px">
                <label class="hc-form-label">{{ t('settingsSecurityGracePeriod') }}</label>
                <input class="hc-form-input" data-testid="settings-security-field-grace-period" type="number" v-model="secForm.grace_period" placeholder="60" style="max-width:200px" />
              </div>
            </div>

            <button class="hc-btn hc-btn-primary hc-btn-sm" data-testid="settings-security-save" @click="saveSecurityConfig()" style="margin-top:8px">{{ t('settingsSecuritySaveSecurity') }}</button>
          </div>

          <!-- (b) Network Constraints -->
          <div style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:12px">
            <h4 style="margin:0 0 12px;font-size:0.9rem;color:var(--ink)">{{ t('settingsNetworkConstraints') || 'Network Constraints' }}</h4>
            <div style="display:flex;gap:16px;margin-bottom:12px;flex-wrap:wrap">
              <label style="display:flex;align-items:center;gap:6px;cursor:pointer">
                <input type="checkbox" :checked="secForm.allow_private" @change="secForm.allow_private = $event.target.checked" />
                Allow Private Networks
              </label>
              <label style="display:flex;align-items:center;gap:6px;cursor:pointer">
                <input type="checkbox" :checked="secForm.allow_local" @change="secForm.allow_local = $event.target.checked" />
                Allow Localhost
              </label>
            </div>
            <div class="hc-form-group" style="margin-bottom:8px">
              <label class="hc-form-label">{{ t('settingsSecurityDenyHosts') }}</label>
              <div v-for="(host, idx) in secForm.deny_hosts" :key="idx" style="display:flex;gap:6px;margin-bottom:4px;align-items:center">
                <input class="hc-form-input" :value="host" @input="secForm.deny_hosts[idx] = $event.target.value" style="flex:1" />
                <button class="hc-btn hc-btn-sm hc-btn-danger" @click="secForm.deny_hosts.splice(idx, 1)">x</button>
              </div>
              <button class="hc-btn hc-btn-sm hc-btn-secondary" @click="secForm.deny_hosts.push('')" style="margin-top:4px">+ Add host</button>
            </div>
            <button class="hc-btn hc-btn-primary hc-btn-sm" @click="saveSecurityConfig()" style="margin-top:8px">{{ t('settingsSecuritySaveSecurity') }}</button>
          </div>

          <!-- (c) Filesystem Constraints -->
          <div style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:12px">
            <h4 style="margin:0 0 12px;font-size:0.9rem;color:var(--ink)">{{ t('settingsFilesystemConstraints') || 'Filesystem Constraints' }}</h4>

            <div class="hc-form-group" style="margin-bottom:8px">
              <label class="hc-form-label">{{ t('settingsSecurityAllowedRoots') }}</label>
              <div v-for="(root, idx) in secForm.allowed_roots" :key="idx" style="display:flex;gap:6px;margin-bottom:4px;align-items:center">
                <input class="hc-form-input" :value="root" @input="secForm.allowed_roots[idx] = $event.target.value" style="flex:1" />
                <button class="hc-btn hc-btn-sm hc-btn-danger" @click="secForm.allowed_roots.splice(idx, 1)">x</button>
              </div>
              <button class="hc-btn hc-btn-sm hc-btn-secondary" @click="secForm.allowed_roots.push('')" style="margin-top:4px">+ Add root</button>
            </div>

            <div class="hc-form-group" style="margin-bottom:8px">
              <label class="hc-form-label">{{ t('settingsSecurityDenyPatterns') }}</label>
              <div v-for="(pat, idx) in secForm.deny_patterns" :key="idx" style="display:flex;gap:6px;margin-bottom:4px;align-items:center">
                <input class="hc-form-input" :value="pat" @input="secForm.deny_patterns[idx] = $event.target.value" style="flex:1" />
                <button class="hc-btn hc-btn-sm hc-btn-danger" @click="secForm.deny_patterns.splice(idx, 1)">x</button>
              </div>
              <button class="hc-btn hc-btn-sm hc-btn-secondary" @click="secForm.deny_patterns.push('')" style="margin-top:4px">+ Add pattern</button>
            </div>

            <div class="hc-form-group" style="margin-bottom:8px">
              <label class="hc-form-label">{{ t('settingsSecuritySkipDirs') }}</label>
              <div v-for="(dir, idx) in secForm.skip_dirs" :key="idx" style="display:flex;gap:6px;margin-bottom:4px;align-items:center">
                <input class="hc-form-input" :value="dir" @input="secForm.skip_dirs[idx] = $event.target.value" style="flex:1" />
                <button class="hc-btn hc-btn-sm hc-btn-danger" @click="secForm.skip_dirs.splice(idx, 1)">x</button>
              </div>
              <button class="hc-btn hc-btn-sm hc-btn-secondary" @click="secForm.skip_dirs.push('')" style="margin-top:4px">+ Add directory</button>
            </div>

            <button class="hc-btn hc-btn-primary hc-btn-sm" @click="saveSecurityConfig()" style="margin-top:8px">{{ t('settingsSecuritySaveSecurity') }}</button>
          </div>

          <!-- (d) Layer2 Feature Toggles -->
          <div style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:12px">
            <h4 style="margin:0 0 12px;font-size:0.9rem;color:var(--ink)">{{ t('settingsLayer2FeatureToggles') || 'Layer2 Feature Toggles' }}</h4>
            <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(140px,1fr));gap:8px">
              <label v-for="feat in layer2Features" :key="feat" style="display:flex;align-items:center;gap:6px;cursor:pointer">
                <input type="checkbox"
                  :checked="secForm.layer2[feat] === true"
                  @change="secForm.layer2[feat] = $event.target.checked" />
                {{ feat }}
              </label>
            </div>
            <button class="hc-btn hc-btn-primary hc-btn-sm" @click="saveToolsConfig()" style="margin-top:12px">{{ t('settingsSecuritySaveTools') }}</button>
          </div>

          <!-- (e) Security Patterns -->
          <div style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:12px">
            <h4 style="margin:0 0 12px;font-size:0.9rem;color:var(--ink)">{{ t('settingsSecurityPatterns') || 'Security Patterns' }}</h4>
            <div class="hc-table-wrap" v-if="secForm.patterns.length > 0"><table class="hc-table">
              <thead><tr>
                <th>{{ t('settingsCapPackName') }}</th>
                <th>{{ t('settingsSecurityDenyPatterns') }}</th>
                <th>{{ t('severity') }}</th>
                <th>{{ t('kind') }}</th>
                <th></th>
              </tr></thead>
              <tbody>
                <tr v-for="(pat, idx) in secForm.patterns" :key="idx">
                  <td><input class="hc-form-input" :value="pat.name" @input="pat.name = $event.target.value" style="min-width:80px" /></td>
                  <td><input class="hc-form-input" :value="pat.pattern" @input="pat.pattern = $event.target.value" style="min-width:120px" /></td>
                  <td><input class="hc-form-input" :value="pat.severity" @input="pat.severity = $event.target.value" style="min-width:60px" /></td>
                  <td><input class="hc-form-input" :value="pat.category" @input="pat.category = $event.target.value" style="min-width:80px" /></td>
                  <td><button class="hc-btn hc-btn-sm hc-btn-danger" @click="secForm.patterns.splice(idx, 1)">x</button></td>
                </tr>
              </tbody>
            </table></div>
            <div v-else style="color:var(--ink3);font-size:0.84rem;margin-bottom:8px">{{ t('settingsSecurityDenyPatterns') }}</div>
            <button class="hc-btn hc-btn-sm hc-btn-secondary" @click="secForm.patterns.push({ name: '', pattern: '', severity: 'medium', category: '' })" style="margin-top:8px">+ Add pattern</button>
            <button class="hc-btn hc-btn-primary hc-btn-sm" @click="saveSecurityConfig()" style="margin-top:8px;margin-left:8px">{{ t('settingsSecuritySaveSecurity') }}</button>
          </div>

          <!-- (f) Dangerous Tools -->
          <div style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:12px">
            <h4 style="margin:0 0 12px;font-size:0.9rem;color:var(--ink)">{{ t('settingsDangerousTools') || 'Dangerous Tools' }}</h4>
            <div v-for="(tool, idx) in secForm.dangerous_tools" :key="idx" style="display:flex;gap:6px;margin-bottom:4px;align-items:center">
              <input class="hc-form-input" :value="tool" @input="secForm.dangerous_tools[idx] = $event.target.value" style="flex:1" />
              <button class="hc-btn hc-btn-sm hc-btn-danger" @click="secForm.dangerous_tools.splice(idx, 1)">x</button>
            </div>
            <div v-if="secForm.dangerous_tools.length === 0" style="color:var(--ink3);font-size:0.84rem;margin-bottom:8px">{{ t('settingsSecurityDangerousTools') }}</div>
            <button class="hc-btn hc-btn-sm hc-btn-secondary" @click="secForm.dangerous_tools.push('')" style="margin-top:4px">+ Add tool</button>
            <button class="hc-btn hc-btn-primary hc-btn-sm" @click="saveToolsConfig()" style="margin-top:8px;margin-left:8px">{{ t('settingsSecuritySaveTools') }}</button>
          </div>

          <!-- (g) Sandbox Toggle -->
          <div style="background:var(--surface-alt);border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:16px;margin-bottom:12px">
            <h4 style="margin:0 0 12px;font-size:0.9rem;color:var(--ink)">{{ t('settingsSandbox') || 'Sandbox' }}</h4>
            <label style="display:flex;align-items:center;gap:8px;cursor:pointer;margin-bottom:12px">
              <input type="checkbox" :checked="secForm.sandbox" @change="secForm.sandbox = $event.target.checked" />
              <span>{{ t('settingsSecuritySandboxMode') }}</span>
              <span class="hc-badge" :class="secForm.sandbox ? 'hc-badge-green' : 'hc-badge-gray'" style="margin-left:8px">
                {{ secForm.sandbox ? (t('enabled') || 'Enabled') : (t('disabled') || 'Disabled') }}
              </span>
            </label>
            <button class="hc-btn hc-btn-primary hc-btn-sm" @click="saveSecurityConfig()">{{ t('settingsSecuritySaveSecurity') }}</button>
          </div>
        </div>

        <!-- =================================================================
             Config Tab
             ================================================================= -->
        <div v-if="activeTab === 'config'" class="hc-settings-section">
          <div class="hc-settings-section-title">{{ t('settingsConfigTitle') || 'Configuration' }}</div>
          <div class="hc-settings-section-desc">{{ t('settingsConfigDesc') || 'View and manage the full agent configuration as JSON.' }}</div>
          <textarea class="hc-config-editor" v-model="configText" spellcheck="false"></textarea>
          <div style="display:flex;gap:8px;margin-top:12px;flex-wrap:wrap">
            <button class="hc-btn hc-btn-secondary" @click="previewConfig()">[Preview Changes]</button>
            <select class="hc-form-select" v-model="configSection" style="font-size:0.84rem;padding:6px 10px">
              <option value="">-- Save Section --</option>
              <option v-for="s in configSections" :key="s" :value="s">{{ s }}</option>
            </select>
            <button class="hc-btn hc-btn-primary" @click="saveConfigSection()">[Save Section]</button>
          </div>
        </div>

        <!-- =================================================================
             Infrastructure Tab
             ================================================================= -->
        <div v-if="activeTab === 'infrastructure'">
          <div v-if="infraWarnings.length > 0 && !infraLoading" class="hc-empty" style="margin-bottom:16px;text-align:left;padding:12px">
            <div v-for="warning in infraWarnings" :key="'infra-warning-' + warning" style="color:var(--warning);font-size:0.84rem;line-height:1.5">{{ warning }}</div>
          </div>
          <!-- Nodes -->
          <div class="hc-settings-section">
            <div class="hc-settings-section-title">{{ t('navNodes') || 'Nodes' }}</div>
            <div v-if="infraLoading" class="hc-loading">{{ t('loading') }}</div>
            <div v-else-if="infraNodesUnavailable" data-testid="settings-infra-nodes-unavailable" class="hc-empty" style="text-align:left;margin:8px 0 0">{{ t('settingsInfraNodesWarning') || 'Nodes inventory unavailable' }}</div>
            <div v-else-if="infraNodes.length === 0" style="color:var(--ink3);padding:8px 0">{{ t('noData') || 'No nodes registered' }}</div>
            <table v-else class="hc-table" style="margin-top:8px">
              <thead><tr><th>ID</th><th>{{ t('name') || 'Name' }}</th><th>{{ t('type') || 'Type' }}</th><th>{{ t('status') || 'Status' }}</th><th>{{ t('navHelpers') || 'Helpers' }}</th></tr></thead>
              <tbody>
                <tr v-for="n in infraNodes" :key="n.id || n.node_id">
                  <td style="font-family:var(--font-mono);font-size:0.82rem">{{ (n.id || n.node_id || '').substring(0, 12) }}</td>
                  <td>{{ n.name || '-' }}</td>
                  <td>{{ n.platform || n.type || '-' }}</td>
                  <td><span class="hc-status-dot" :class="n.status === 'online' || n.status === 'healthy' ? 'ok' : 'err'" style="display:inline-block;width:8px;height:8px;border-radius:50%"></span> {{ n.status || 'unknown' }}</td>
                  <td>{{ (n.helpers || []).length }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Devices -->
          <div class="hc-settings-section" style="margin-top:24px">
            <div class="hc-settings-section-title">{{ t('navDevices') || 'Devices' }}</div>
            <div v-if="infraDevicesUnavailable && !infraLoading" class="hc-empty" style="text-align:left;margin:8px 0 0">{{ t('settingsInfraDevicesWarning') || 'Devices inventory unavailable' }}</div>
            <div v-else-if="infraDevices.length === 0 && !infraLoading" style="color:var(--ink3);padding:8px 0">{{ t('noData') || 'No devices paired' }}</div>
            <table v-else-if="infraDevices.length > 0" class="hc-table" style="margin-top:8px">
              <thead><tr><th>{{ t('name') || 'Name' }}</th><th>{{ t('type') || 'Type' }}</th><th>{{ t('status') || 'Status' }}</th><th>Trusted</th></tr></thead>
              <tbody>
                <tr v-for="d in infraDevices" :key="d.id || d.device_id">
                  <td>{{ d.name || d.device_id || '-' }}</td>
                  <td>{{ d.platform || d.type || '-' }}</td>
                  <td><span class="hc-badge" :class="d.status === 'active' ? 'hc-badge-green' : 'hc-badge-gray'">{{ d.status || 'unknown' }}</span></td>
                  <td>{{ d.trusted ? t('yes') : t('no') }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Helpers -->
          <div class="hc-settings-section" style="margin-top:24px">
            <div class="hc-settings-section-title">{{ t('helpersTitle') || t('navHelpers') || 'Helpers' }}</div>
            <div class="hc-settings-section-desc">{{ t('helpersDesc') || 'Browser Helper and Desktop Helper process status. Reclaim stops the process so it restarts on next use.' }}</div>
            <div v-if="infraHelpersUnavailable && !infraLoading" data-testid="settings-infra-helpers-unavailable" class="hc-empty" style="text-align:left;margin:8px 0 0">{{ t('settingsInfraHelpersWarning') || 'Helper status unavailable' }}</div>
            <div v-else-if="infraHelpers.length === 0 && !infraLoading" style="color:var(--ink3);padding:8px 0">{{ t('noData') || 'No helpers running' }}</div>
            <table v-else-if="infraHelpers.length > 0" class="hc-table" style="margin-top:8px">
              <thead><tr><th>{{ t('name') || 'Name' }}</th><th>{{ t('status') || 'Status' }}</th><th>{{ t('helpersSessionCount') || 'Sessions' }}</th><th>{{ t('helpersLastUse') || 'Last use' }}</th><th>{{ t('helpersIdleTimeout') || 'Idle timeout' }}</th><th>{{ t('actions') || 'Actions' }}</th></tr></thead>
              <tbody>
                <tr v-for="h in infraHelpers" :key="h.name || h.id">
                  <td>{{ helperDisplayName(h.name) }}</td>
                  <td><span class="hc-badge" :class="helperStatusBadge(h)">{{ helperStatusText(h.status) }}</span></td>
                  <td>{{ h.session_count || 0 }}</td>
                  <td>{{ h.last_use_at ? fmtTime(h.last_use_at) : '-' }}</td>
                  <td>{{ helperIdleTimeoutText(h) }}</td>
                  <td>
                    <button class="hc-btn hc-btn-sm hc-btn-secondary" :disabled="infraHelperAction === h.name || !helperCanReclaim(h)" @click="reclaimHelper(h.name)">{{ infraHelperAction === h.name ? (t('loading') || 'Working...') : (t('helpersReclaim') || 'Reclaim') }}</button>
                  </td>
                </tr>
              </tbody>
            </table>
            <div v-if="infraHelpers.length > 0 && infraHelpers.every(h => h.status === 'unavailable')" class="hc-empty" style="margin-top:12px;text-align:left">
              <div style="font-weight:600">{{ t('helpersNotConfigured') || 'Helpers not configured' }}</div>
              <div style="font-size:0.84rem;color:var(--ink3);margin-top:4px">{{ t('helpersNotConfiguredDesc') || 'Configure browser or desktop helpers in the hosts section to enable managed helper controls.' }}</div>
            </div>
          </div>

          <!-- Instances -->
          <div class="hc-settings-section" style="margin-top:24px">
            <div class="hc-settings-section-title">{{ t('navInstances') || 'Instances' }}</div>
            <div v-if="infraInstancesUnavailable && !infraLoading" class="hc-empty" style="text-align:left;margin:8px 0 0">{{ t('settingsInfraInstancesWarning') || 'Instance inventory unavailable' }}</div>
            <div v-else-if="infraInstances.length === 0 && !infraLoading" style="color:var(--ink3);padding:8px 0">{{ t('noData') || 'No instances' }}</div>
            <table v-else-if="infraInstances.length > 0" class="hc-table" style="margin-top:8px">
              <thead><tr><th>ID</th><th>{{ t('name') || 'Name' }}</th><th>{{ t('status') || 'Status' }}</th><th>Agent</th></tr></thead>
              <tbody>
                <tr v-for="inst in infraInstances" :key="inst.id">
                  <td style="font-family:var(--font-mono);font-size:0.82rem">{{ (inst.id || '').substring(0, 12) }}</td>
                  <td>{{ inst.name || '-' }}</td>
                  <td><span class="hc-badge" :class="inst.status === 'running' ? 'hc-badge-green' : inst.status === 'idle' ? 'hc-badge-gray' : 'hc-badge-orange'">{{ inst.status || 'unknown' }}</span></td>
                  <td>{{ inst.agent || '-' }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Channel Pairing -->
          <div class="hc-settings-section" style="margin-top:24px">
            <div class="hc-settings-section-title">{{ t('settingsChannelPairing') || 'Channel Pairing' }}</div>
            <div class="hc-settings-section-desc">{{ t('settingsChannelPairingDesc') || 'Create, verify, and revoke direct-message pairing records for external chat channels such as Feishu.' }}</div>
            <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:10px;align-items:end;margin-top:12px">
              <div>
                <div class="hc-form-label">{{ t('channel') }}</div>
                <input class="hc-form-input" v-model="infraPairingChannel" placeholder="feishu" />
              </div>
              <div>
                <div class="hc-form-label">{{ t('userId') }}</div>
                <input class="hc-form-input" v-model="infraPairingUserID" placeholder="ou_xxx" />
              </div>
              <div>
                <div class="hc-form-label">{{ t('name') }}</div>
                <input class="hc-form-input" v-model="infraPairingName" placeholder="Optional" />
              </div>
              <div>
                <button class="hc-btn hc-btn-primary" @click="createChannelPairing()">{{ t('settingsInfraPairDevice') }}</button>
              </div>
            </div>
            <div style="display:grid;grid-template-columns:minmax(180px,320px) auto;gap:10px;align-items:end;margin-top:12px">
              <div>
                <div class="hc-form-label">{{ t('settingsInfraGenerateCode') }}</div>
                <input class="hc-form-input" v-model="infraPairingCode" placeholder="123456" />
              </div>
              <div>
                <button class="hc-btn hc-btn-secondary" @click="verifyChannelPairing()">{{ t('settingsInfraGenerateCode') }}</button>
              </div>
            </div>
            <div v-if="infraPairingsUnavailable && !infraLoading" class="hc-empty" style="text-align:left;margin-top:12px">{{ t('settingsInfraPairingsWarning') || 'Pairing records unavailable' }}</div>
            <div v-else-if="infraPairings.length === 0 && !infraLoading" style="color:var(--ink3);padding:12px 0 4px">{{ t('noData') || 'No pairing records' }}</div>
            <table v-else-if="infraPairings.length > 0" class="hc-table" style="margin-top:12px">
              <thead><tr><th>Channel</th><th>User</th><th>{{ t('settingsCapPackName') }}</th><th>Status</th><th>Code</th><th>{{ t('settingsProfileCreated') }}</th><th>Actions</th></tr></thead>
              <tbody>
                <tr v-for="item in infraPairings" :key="(item.channel || '-') + ':' + (item.user_id || '-')">
                  <td>{{ item.channel || '-' }}</td>
                  <td style="font-family:var(--font-mono);font-size:0.82rem">{{ item.user_id || '-' }}</td>
                  <td>{{ item.display_name || '-' }}</td>
                  <td><span class="hc-badge" :class="item.status === 'verified' ? 'hc-badge-green' : item.status === 'pending' ? 'hc-badge-orange' : 'hc-badge-gray'">{{ item.status || 'unknown' }}</span></td>
                  <td style="font-family:var(--font-mono);font-size:0.82rem">{{ item.code || '-' }}</td>
                  <td>{{ fmtTime(item.created_at) }}</td>
                  <td><button class="hc-btn hc-btn-sm hc-btn-danger" @click="revokeChannelPairing(item)">{{ t('delete') }}</button></td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- =================================================================
             Diagnostics Tab
             ================================================================= -->
        <div v-if="activeTab === 'diagnostics'">
          <div v-if="diagWarnings.length > 0 && !diagLoading" class="hc-empty" style="margin-bottom:16px;text-align:left;padding:12px">
            <div v-for="warning in diagWarnings" :key="'diag-warning-' + warning" style="color:var(--warning);font-size:0.84rem;line-height:1.5">{{ warning }}</div>
          </div>
          <!-- System Health -->
          <div class="hc-settings-section" data-testid="settings-diagnostics-status" :data-unavailable="diagStatusUnavailable ? 'true' : 'false'">
            <div class="hc-settings-section-title">{{ t('overviewStatus') || 'System Health' }}</div>
            <div v-if="diagLoading" class="hc-loading">{{ t('loading') }}</div>
            <div v-else class="hc-monitor-grid" style="grid-template-columns:repeat(auto-fill,minmax(160px,1fr))">
              <div class="hc-monitor-card" style="padding:14px">
                <div class="hc-monitor-card-label">{{ t('status') || 'Status' }}</div>
                <div class="hc-monitor-card-value" style="font-size:1rem">
                  <span class="hc-status-dot" :class="diagStatusDotClass()" style="display:inline-block;width:10px;height:10px;border-radius:50%"></span>
                  {{ diagStatusLabel() }}
                </div>
              </div>
              <div class="hc-monitor-card" style="padding:14px">
                <div class="hc-monitor-card-label">{{ t('overviewUptime') || 'Uptime' }}</div>
                <div class="hc-monitor-card-value" style="font-size:1rem">{{ diagStatus ? (diagStatus.uptime || '-') : '-' }}</div>
              </div>
              <div class="hc-monitor-card" style="padding:14px">
                <div class="hc-monitor-card-label">{{ t('settingsVersionLabel') || 'Version' }}</div>
                <div class="hc-monitor-card-value" style="font-size:1rem">{{ diagStatus ? ('v' + (diagStatus.version || '?')) : '-' }}</div>
              </div>
            </div>
          </div>

          <!-- Usage Summary -->
          <div class="hc-settings-section" style="margin-top:24px">
            <div class="hc-settings-section-title">{{ t('navUsage') || 'Usage' }}</div>
            <div v-if="diagUsageUnavailable" data-testid="settings-diagnostics-usage-unavailable" class="hc-empty" style="text-align:left">{{ t('loadError') }}</div>
            <div v-else-if="diagUsage" class="hc-monitor-grid" style="grid-template-columns:repeat(auto-fill,minmax(160px,1fr))">
              <div class="hc-monitor-card" style="padding:14px">
                <div class="hc-monitor-card-label">{{ t('tokens') || 'Total Tokens' }}</div>
                <div class="hc-monitor-card-value" style="font-size:1rem">{{ diagUsage.total_tokens ? diagFormatTokens(diagUsage.total_tokens) : '0' }}</div>
              </div>
              <div class="hc-monitor-card" style="padding:14px">
                <div class="hc-monitor-card-label">{{ t('navRuns') || 'Total Runs' }}</div>
                <div class="hc-monitor-card-value" style="font-size:1rem">{{ diagUsage.total_runs || 0 }}</div>
              </div>
              <div class="hc-monitor-card" style="padding:14px">
                <div class="hc-monitor-card-label">{{ t('navSessions') || 'Sessions' }}</div>
                <div class="hc-monitor-card-value" style="font-size:1rem">{{ diagUsage.total_sessions || 0 }}</div>
              </div>
            </div>
            <div v-else style="color:var(--ink3);padding:8px 0">{{ t('noData') || 'No usage data available' }}</div>
          </div>

          <!-- Audit Log -->
          <div class="hc-settings-section" style="margin-top:24px">
            <div class="hc-settings-section-title">{{ t('navAudit') || 'Audit Log' }}</div>
            <div v-if="diagAuditUnavailable && !diagLoading" data-testid="settings-diagnostics-audit-unavailable" class="hc-empty" style="text-align:left">{{ t('loadError') }}</div>
            <div v-else-if="diagAuditEvents.length === 0 && !diagLoading" style="color:var(--ink3);padding:8px 0">{{ t('noData') || 'No audit events' }}</div>
            <table v-else-if="diagAuditEvents.length > 0" class="hc-table" style="margin-top:8px">
              <thead><tr><th>{{ t('type') || 'Event' }}</th><th>{{ t('name') || 'Actor' }}</th><th>{{ t('description') || 'Detail' }}</th><th>Time</th></tr></thead>
              <tbody>
                <tr v-for="e in diagAuditEvents.slice(0, 20)" :key="e.id">
                  <td><span class="hc-badge hc-badge-gray">{{ e.type || e.action || '-' }}</span></td>
                  <td>{{ e.actor || e.user || '-' }}</td>
                  <td style="font-size:0.82rem;max-width:300px;word-break:break-word">{{ e.detail || e.description || e.message || '-' }}</td>
                  <td>{{ fmtTime(e.created_at || e.timestamp) }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Logs -->
          <div class="hc-settings-section" style="margin-top:24px">
            <div class="hc-settings-section-title">{{ t('navLogs') || 'Logs' }}</div>
            <div v-if="diagLogsUnavailable && !diagLoading" data-testid="settings-diagnostics-logs-unavailable" class="hc-empty" style="text-align:left">{{ t('loadError') }}</div>
            <div v-else-if="diagLogEntries.length === 0 && !diagLoading" style="color:var(--ink3);padding:8px 0">{{ t('noData') || 'No log entries' }}</div>
            <div v-else-if="diagLogEntries.length > 0" style="max-height:300px;overflow:auto;background:var(--code-bg);border-radius:var(--radius-sm);padding:12px;font-family:var(--font-mono);font-size:0.78rem;line-height:1.6">
              <div v-for="(entry, idx) in diagLogEntries.slice(0, 50)" :key="idx" style="white-space:pre-wrap;word-break:break-all">{{ entry.message || entry.msg || entry }}</div>
            </div>
          </div>
        </div>

      </div>
    </div>
  `;

  // -------------------------------------------------------------------------
  // Component state and methods
  // -------------------------------------------------------------------------

  const coreReadinessSection = buildSettingsCoreReadinessSection({
    api,
    t,
    settledValue,
    capabilityHealth,
    moduleHealth,
    healthyCapabilityStates: HEALTHY_CAPABILITY_STATES,
    healthyModuleStates: HEALTHY_MODULE_STATES,
    connectedChannelStates: CONNECTED_CHANNEL_STATES,
    warningChannelStates: WARNING_CHANNEL_STATES,
  });
  const modelsSection = buildSettingsModelsSection({
    api,
    showToast,
    t,
    defaultProviderAPI,
    defaultProviderFieldValues,
    effectiveProviderAPISchema,
    normalizeProviderAPI,
    providerCapabilityBadges,
    providerFieldDisplayValue,
    providerFieldEmptyPayload,
    providerFieldIsTextarea: isProviderFieldTextarea,
    providerFieldPayloadValue,
    providerFieldRequiresExplicitMutation,
    providerFieldRows: providerFieldRowCount,
  });
  const pluginsSection = buildSettingsPluginsSection({
    api,
    showToast,
    t,
    settledValue,
    moduleHealth,
    healthyModuleStates: HEALTHY_MODULE_STATES,
    defaultPageSize: DEFAULT_PAGE_SIZE,
  });
  const browserSection = buildSettingsBrowserSection({
    api,
    showToast,
    t,
    paginate,
    totalPages,
    defaultPageSize: DEFAULT_PAGE_SIZE,
  });
  const diagnosticsSection = buildSettingsDiagnosticsSection({
    api,
    t,
    settledValue,
  });
  const channelsSection = buildSettingsChannelsSection({
    api,
    showToast,
    t,
    normalizeChannelTypeKey,
    defaultChannelFormConfig,
    effectiveChannelTypeOptions,
    effectiveChannelSchema,
    channelFieldDisplayValue,
    channelFieldRowCount,
    channelFieldPayloadValue,
  });
  const skillsSection = buildSettingsSkillsSection({
    api,
    showToast,
    t,
    paginate,
    totalPages,
    prettyData,
    formatStatusLabel,
    artifactPreviewPath,
    safeExternalURL,
    defaultPageSize: DEFAULT_PAGE_SIZE,
  });
  const securityConfigSection = buildSettingsSecurityConfigSection({
    api,
    showToast,
    t,
    layer2Features: LAYER2_FEATURES,
  });
  const infrastructureSection = buildSettingsInfrastructureSection({
    api,
    showToast,
    t,
    settledValue,
    normalizeHelperList,
    normalizeManagedHelperName,
    helperDisplayName,
  });

  const view = {
    $template,
    t,
    fmtTime,
    formatStatusLabel,
    formatFileSize,
    truncatePreview,
    tabs: TABS,
    themeOptions: ['light', 'dark'],
    execApprovalOptions: EXEC_APPROVAL_OPTIONS,
    layer2Features: LAYER2_FEATURES,
    setupCatalog: null,

    get apiTypeOptions() {
      return effectiveProviderAPIOptions(this.setupCatalog);
    },

    get activeTab() {
      return getInitialTab();
    },

    get selectedTheme() {
      return normalizeConsoleTheme(store && store.theme);
    },

    settingsGroups() {
      const groups = [];
      for (const tab of this.tabs) {
        const groupName = tab.group || 'Other';
        let entry = groups.find(item => item.name === groupName);
        if (!entry) {
          const i18nName = tab.groupKey ? (t(tab.groupKey) || groupName) : groupName;
          entry = { name: groupName, i18nName, items: [] };
          groups.push(entry);
        }
        entry.items.push(tab);
      }
      return groups;
    },

    activeTabMeta() {
      return this.tabs.find(tab => tab.id === this.activeTab) || null;
    },

    themeOptionLabel(theme) {
      return theme === 'dark'
        ? (t('settingsThemeDark') || 'Dark')
        : (t('settingsThemeLight') || 'Light');
    },

    setTheme(theme) {
      const next = normalizeConsoleTheme(theme);
      store.theme = next;
      document.documentElement.setAttribute('data-theme', next);
      localStorage.setItem('hc_theme', next);
    },

    ...coreReadinessSection,

    // -----------------------------------------------------------------------
    // Models state
    // -----------------------------------------------------------------------

    // -----------------------------------------------------------------------
    // Channels state
    // -----------------------------------------------------------------------

    ...channelsSection,

    async loadSetupCatalog() {
      if (this._setupCatalogPromise) {
        await this._setupCatalogPromise;
        return;
      }
      this._setupCatalogPromise = (async () => {
        try {
          this.setupCatalog = await api.get('/operator/setup/catalog', { background: true });
        } catch (_) {
          this.setupCatalog = null;
        } finally {
          this._setupCatalogPromise = null;
        }
      })();
      try {
        await this._setupCatalogPromise;
      } catch (_) {}
    },

    // -----------------------------------------------------------------------
    // Skills state
    // -----------------------------------------------------------------------

    // -----------------------------------------------------------------------
    // Plugins state
    // -----------------------------------------------------------------------

    ...pluginsSection,

    get pluginsTotalPages() {
      return totalPages(this.pluginsData, this.pluginsPageSize);
    },
    get pagedPlugins() {
      return paginate(this.pluginsData, this.pluginsPage, this.pluginsPageSize);
    },
    get pluginModulesTotalPages() {
      return totalPages(this.pluginsModules, this.pluginModulesPageSize);
    },
    get pagedPluginModules() {
      return paginate(this.pluginsModules, this.pluginModulesPage, this.pluginModulesPageSize);
    },

    // -----------------------------------------------------------------------
    // Browser state
    // -----------------------------------------------------------------------

    ...browserSection,

    // -----------------------------------------------------------------------
    // Security state
    // -----------------------------------------------------------------------

    // -----------------------------------------------------------------------
    // Config state
    // -----------------------------------------------------------------------

    // Infrastructure state
    ...infrastructureSection,

    // Diagnostics state
    ...diagnosticsSection,

    // -----------------------------------------------------------------------
    // Tab switching
    // -----------------------------------------------------------------------

    switchTab(id) {
      this.showAddForm = false;
      this.editingProvider = null;
      this.validationResult = null;
      this.testResult = null;
      this.cancelChannelForm();
      window.location.hash = '#/settings/' + id;
    },

    // -----------------------------------------------------------------------
    // Helpers
    // -----------------------------------------------------------------------

    channelDotStyle(ch) {
      const status = ch.health_status || ch.status || ch.health || 'unknown';
      const color = (status === 'ok' || status === 'healthy' || status === 'connected')
        ? 'var(--success)' : (status === 'degraded') ? 'var(--warning)' : 'var(--danger)';
      return 'background:' + color;
    },

    channelFeatureBadges(ch) {
      const caps = ch.capability_matrix || {};
      const tags = [];
      if (caps.threading) tags.push('threads');
      if (caps.thread_binding) tags.push('binding');
      if (caps.policy_controls) tags.push('policy');
      if (caps.dedupe) tags.push('dedupe');
      if (caps.reactions) tags.push('reactions');
      if (caps.edit_message) tags.push('edit');
      if (caps.rich_cards) tags.push('cards');
      if (caps.streaming_updates) tags.push('stream');
      if (caps.typing_indicator) tags.push('typing');
      if (caps.multi_account) tags.push('multi-account');
      if (caps.webhook_inbound) tags.push('webhook');
      if (caps.pairing) tags.push('pairing');
      return tags;
    },

    helperDisplayName(name) {
      return helperDisplayName(name);
    },

    helperStatusText(status) {
      return helperStatusText(status);
    },

    helperStatusBadge(helper) {
      const status = String(helper && helper.status || '').trim().toLowerCase();
      if (ACTIVE_HELPER_STATES.includes(status)) return 'hc-badge-green';
      if (WARNING_HELPER_STATES.includes(status)) return 'hc-badge-orange';
      if (status === 'error' || status === 'failed') return 'hc-badge-red';
      return 'hc-badge-gray';
    },

    helperIdleTimeoutText(helper) {
      return formatHelperIdleTimeout(helper && helper.idle_timeout_sec);
    },

    helperCanReclaim(helper) {
      const name = normalizeManagedHelperName(helper && helper.name);
      const status = String(helper && helper.status || '').trim().toLowerCase();
      return !!name && ACTIVE_HELPER_STATES.includes(status);
    },

    providerFeatureBadges(prov) {
      return providerCapabilityBadges(prov);
    },

    formatChannelPolicies(ch) {
      const cfg = ch.config || {};
      const parts = [];
      if (cfg.dm_policy) parts.push('DM ' + cfg.dm_policy);
      if (cfg.group_policy) parts.push('Group ' + cfg.group_policy);
      if (cfg.group_session_scope) parts.push('Scope ' + cfg.group_session_scope);
      if (cfg.reply_in_thread) parts.push('Thread ' + cfg.reply_in_thread);
      return parts.length ? parts.join(' | ') : '-';
    },

    resultBadge(status) {
      switch (String(status || '').trim()) {
        case 'ok':
        case 'ready':
          return 'hc-badge-green';
        case 'partial':
        case 'degraded':
          return 'hc-badge-orange';
        case 'error':
        case 'blocked':
        case 'failed':
          return 'hc-badge-red';
        default:
          return 'hc-badge-gray';
      }
    },

    // -----------------------------------------------------------------------
    // Channels methods
    // -----------------------------------------------------------------------

    // -----------------------------------------------------------------------
    // Skills methods
    // -----------------------------------------------------------------------

    // -----------------------------------------------------------------------
    // Plugins methods
    // -----------------------------------------------------------------------

    // -----------------------------------------------------------------------
    // Security methods
    // -----------------------------------------------------------------------

    // -----------------------------------------------------------------------
    // Config methods
    // -----------------------------------------------------------------------

    // -----------------------------------------------------------------------
    // Data loading
    // -----------------------------------------------------------------------

    // -----------------------------------------------------------------------
    // Tab data loading
    // -----------------------------------------------------------------------

    async loadTabData() {
      const readinessPromise = this.loadCoreReadiness();
      const setupCatalogPromise = this.setupCatalog ? Promise.resolve() : this.loadSetupCatalog();
      switch (this.activeTab) {
        case 'models':
          await Promise.all([setupCatalogPromise, this.loadModels(), this.loadPluginModules()]);
          break;
        case 'channels':
          await Promise.all([setupCatalogPromise, this.loadChannels()]);
          break;
        case 'skills':
          await Promise.all([this.loadSkills(), this.loadSkillCatalog()]);
          await this.ensureSkillSelection();
          break;
        case 'plugins': await this.loadPlugins(); break;
        case 'browser':
          await Promise.all([this.loadBrowserProfiles(), this.loadBrowserHostSettings()]);
          break;
        case 'security': await this.loadSecurity(); break;
        case 'config': await this.loadConfig(); break;
        case 'infrastructure': await this.loadInfrastructure(); break;
        case 'diagnostics': await this.loadDiagnostics(); break;
      }
      await readinessPromise;
    },

    // -----------------------------------------------------------------------
    // Lifecycle
    // -----------------------------------------------------------------------

    mounted() {
      this._lastTab = this.activeTab;
      this.loadTabData();
      boundRouteSync = () => {
        const tab = this.activeTab;
        if (tab !== this._lastTab) {
          this._lastTab = tab;
          this.loadTabData();
        }
      };
      window.addEventListener('hashchange', boundRouteSync);
    },

    unmounted() {
      if (boundRouteSync) {
        window.removeEventListener('hashchange', boundRouteSync);
        boundRouteSync = null;
      }
    },
  };

  return mergeSectionDescriptors(
    view,
    coreReadinessSection,
    modelsSection,
    channelsSection,
    skillsSection,
    pluginsSection,
    browserSection,
    securityConfigSection,
    infrastructureSection,
    diagnosticsSection,
  );
}
