// ---------------------------------------------------------------------------
// Knowledge View - unified console shell for managed memory + artifacts
// ---------------------------------------------------------------------------

import { api, showToast } from '../api.js';
import { t } from '../i18n/index.js';
import { artifactPreviewPath, openSafeWindow } from '../linking.js';

const DEFAULT_PAGE_SIZE = 20;
const MEM_VALUE_TRUNCATE_LEN = 120;
const ART_NAME_TRUNCATE_LEN = 60;
const SHORT_ID_LEN = 12;
const NOTEBOOK_PREVIEW_LEN = 1200;
const KIND_OPTIONS = ['file', 'screenshot', 'document'];
const SOURCE_KIND_OPTIONS = [
  { value: 'local_dir', label: 'Local Directory', description: 'Index a maintained folder without moving the source of truth.' },
  { value: 'git_repo', label: 'Git Repository', description: 'Index a checked-out repo with docs, code, and runbooks.' },
  { value: 'web_urls', label: 'Web URLs', description: 'Index published pages or docs URLs maintained elsewhere.' },
  { value: 'feishu_docs', label: 'Feishu Docs', description: 'Index Feishu documents while your team keeps editing in Feishu.' },
  { value: 'notion', label: 'Notion', description: 'Index Notion pages while source content stays in Notion.' },
  { value: 'confluence', label: 'Confluence', description: 'Index Confluence pages and descendants for internal runbooks.' },
  { value: 'google_drive', label: 'Google Drive / Docs', description: 'Index Google Docs and Drive files while content stays in Google Workspace.' },
  { value: 'yuque', label: 'Yuque', description: 'Index Yuque repos and docs while your team keeps editing in Yuque.' },
  { value: 'tencent_docs', label: 'Tencent Docs', description: 'Index Tencent Docs exports for retrieval during execution.' },
];
let sourceKindCatalog = normalizeSourceKindOptions(SOURCE_KIND_OPTIONS);
function buildSourceStatusOptions() {
  return [
    { value: '', label: t('allStatuses') || 'All Statuses' },
    { value: 'ready', label: t('knowledgeStatusReady') || 'Ready' },
    { value: 'syncing', label: t('knowledgeStatusSyncing') || 'Syncing' },
    { value: 'degraded', label: t('knowledgeStatusDegraded') || 'Needs Attention' },
    { value: 'blocked', label: t('knowledgeStatusBlocked') || 'Blocked' },
  ];
}
function buildMemNamespaceOptions() {
  return [
    { value: '', label: t('all') || 'All' },
    { value: 'profile', label: t('knowledgeNsProfile') || 'Profile' },
    { value: 'workspace', label: t('knowledgeNsWorkspace') || 'Workspace' },
    { value: 'project', label: t('knowledgeNsProject') || 'Project' },
    { value: 'task', label: t('knowledgeNsTask') || 'Task' },
    { value: 'general', label: t('knowledgeNsGeneral') || 'General' },
  ];
}

function memoryDefaultScopeKey(namespace) {
  switch (String(namespace || '').trim()) {
    case 'profile':
      return 'user';
    default:
      return '';
  }
}

function memoryScopePlaceholder(namespace) {
  switch (String(namespace || '').trim()) {
    case 'profile':
      return t('knowledgeAppliesProfilePlaceholder') || 'you';
    case 'workspace':
      return t('knowledgeAppliesWorkspacePlaceholder') || 'workspace or repo name';
    case 'project':
      return t('knowledgeAppliesProjectPlaceholder') || 'project name';
    case 'task':
      return t('knowledgeAppliesTaskPlaceholder') || 'conversation key or task id';
    default:
      return t('knowledgeAppliesSharedPlaceholder') || 'leave blank for shared memory';
  }
}

function memoryAppliesToLabel(entry) {
  const namespace = String(entry && entry.namespace || '').trim();
  const scope = String(entry && entry.scope_key || '').trim();
  switch (namespace) {
    case 'profile':
      return scope || (t('knowledgeAppliesProfileDefault') || 'you');
    case 'workspace':
      return scope || (t('knowledgeAppliesCurrentWorkspace') || 'current workspace');
    case 'project':
      return scope || (t('knowledgeAppliesCurrentProject') || 'current project');
    case 'task':
      return scope || (t('knowledgeAppliesCurrentTask') || 'current conversation');
    default:
      return scope || (t('knowledgeAppliesSharedDefault') || 'shared across conversations');
  }
}

function fmtTime(iso) {
  try {
    return new Date(iso).toLocaleString([], {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  } catch (_) {
    return '';
  }
}

function truncate(s, max) {
  if (!s) return '';
  return s.length > max ? s.substring(0, max) + '...' : s;
}

function shortId(id) {
  if (!id) return '-';
  return id.length > SHORT_ID_LEN ? id.substring(0, SHORT_ID_LEN) + '...' : id;
}

function fmtSize(bytes) {
  if (bytes == null || bytes < 0) return '-';
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  const k = 1024;
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), units.length - 1);
  const val = bytes / Math.pow(k, i);
  return (i === 0 ? val : val.toFixed(1)) + ' ' + units[i];
}

function latestTimestamp(items, field) {
  let latest = '';
  for (const item of items || []) {
    const value = String(item && item[field] || '').trim();
    if (!value) continue;
    if (!latest || new Date(value) > new Date(latest)) latest = value;
  }
  return latest;
}

function totalArtifactBytes(items) {
  return (items || []).reduce((sum, item) => {
    const value = Number(item && item.size);
    return sum + (Number.isFinite(value) && value > 0 ? value : 0);
  }, 0);
}

function namespaceLabel(namespace) {
  const found = buildMemNamespaceOptions().find(item => item.value === String(namespace || '').trim());
  return found ? found.label : (namespace || t('knowledgeNsGeneral') || 'General');
}

function namespaceBadgeClass(namespace) {
  switch (String(namespace || '').trim()) {
    case 'profile': return 'hc-badge-blue';
    case 'workspace': return 'hc-badge-green';
    case 'project': return 'hc-badge-orange';
    case 'task': return 'hc-badge-purple';
    default: return 'hc-badge-gray';
  }
}

function confidenceText(value) {
  const num = Number(value);
  if (!Number.isFinite(num) || num <= 0) return '-';
  return (num * 100).toFixed(num >= 0.995 ? 0 : 1) + '%';
}

function sourceKindLabel(kind) {
  const found = sourceKindCatalog.find(item => item.value === String(kind || '').trim());
  return found ? found.label : prettyField(kind || 'source');
}

function sourceStatusLabel(status) {
  const found = buildSourceStatusOptions().find(item => item.value === String(status || '').trim());
  return found ? found.label : prettyField(status || 'ready');
}

function sourceStatusBadgeClass(status) {
  switch (String(status || '').trim()) {
    case 'ready': return 'hc-badge-green';
    case 'syncing': return 'hc-badge-blue';
    case 'degraded': return 'hc-badge-orange';
    case 'blocked': return 'hc-badge-red';
    default: return 'hc-badge-gray';
  }
}

function listToLines(items) {
  return (items || []).join('\n');
}

function normalizeSourceKindOptions(items) {
  const rawItems = Array.isArray(items) && items.length ? items : SOURCE_KIND_OPTIONS;
  return rawItems
    .map(item => {
      const value = String(item && (item.value || item.kind) || '').trim();
      if (!value) return null;
      const fields = Array.isArray(item && item.fields)
        ? item.fields.map(normalizeSourceField).filter(Boolean)
        : [];
      const requirements = Array.isArray(item && item.requirements)
        ? item.requirements.map(normalizeSourceRequirement).filter(Boolean)
        : [];
      return {
        value,
        label: String(item && item.label || prettyField(value)),
        description: String(item && (item.description || item.connector_note) || '').trim(),
        fields,
        requirements,
      };
    })
    .filter(Boolean);
}

function normalizeSourceField(field) {
  const id = String(field && field.id || '').trim();
  if (!id) return null;
  const type = String(field && field.type || 'string').trim() || 'string';
  const rows = Number(field && field.rows);
  const hasDefaultValue = field && (Object.prototype.hasOwnProperty.call(field, 'default_value') || Object.prototype.hasOwnProperty.call(field, 'defaultValue'));
  return {
    id,
    scope: String(field && field.scope || 'config').trim() || 'config',
    key: String(field && (field.key || field.id) || id).trim() || id,
    label: String(field && field.label || prettyField(id)),
    description: String(field && field.description || '').trim(),
    type,
    required: Boolean(field && field.required),
    secret: Boolean(field && field.secret),
    placeholder: String(field && field.placeholder || '').trim(),
    defaultValue: hasDefaultValue ? (Object.prototype.hasOwnProperty.call(field, 'default_value') ? field.default_value : field.defaultValue) : undefined,
    rows: Number.isFinite(rows) && rows > 0 ? rows : (type === 'string_list' ? 4 : 0),
    aliases: Array.isArray(field && field.aliases)
      ? field.aliases.map(item => String(item || '').trim()).filter(Boolean)
      : [],
  };
}

function normalizeSourceRequirement(requirement) {
  const groups = Array.isArray(requirement && requirement.any_of)
    ? requirement.any_of
    : (Array.isArray(requirement && requirement.anyOf) ? requirement.anyOf : []);
  const anyOf = Array.isArray(groups)
    ? groups
      .map(group => Array.isArray(group) ? group.map(item => String(item || '').trim()).filter(Boolean) : [])
      .filter(group => group.length > 0)
    : [];
  if (anyOf.length === 0) return null;
  return {
    anyOf,
    description: String(requirement && requirement.description || '').trim(),
  };
}

function sourceKindDescriptorFor(kind, catalog = sourceKindCatalog) {
  const value = String(kind || '').trim();
  return (catalog || []).find(item => item.value === value) || null;
}

function linesToList(value) {
  return String(value || '')
    .split(/\r?\n/)
    .map(item => item.trim())
    .filter(Boolean);
}

function prettyField(value) {
  if (!value) return '-';
  return String(value).replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function notebookPreview(content) {
  const value = String(content || '').trim();
  if (!value) return '';
  return value.length > NOTEBOOK_PREVIEW_LEN ? value.slice(0, NOTEBOOK_PREVIEW_LEN) + '\n…' : value;
}

function sourceFieldDefaultValue(field) {
  if (!field) return '';
  if (field.type === 'boolean') {
    return field.defaultValue === undefined ? false : Boolean(field.defaultValue);
  }
  if (field.type === 'string_list') {
    return Array.isArray(field.defaultValue) ? listToLines(field.defaultValue) : String(field.defaultValue || '');
  }
  return String(field.defaultValue || '');
}

function buildSourceFieldValues(kind, catalog = sourceKindCatalog) {
  const descriptor = sourceKindDescriptorFor(kind, catalog);
  const values = {};
  for (const field of (descriptor && descriptor.fields) || []) {
    values[field.id] = sourceFieldDefaultValue(field);
  }
  return values;
}

function emptySourceForm(kind = 'local_dir', catalog = sourceKindCatalog) {
  return {
    name: '',
    kind,
    enabled: true,
    values: buildSourceFieldValues(kind, catalog),
  };
}

export function KnowledgeView() {
  let _mounted = false;

  const $template = `
    <div class="hc-page-shell hc-page-shell-spacious">
      <div class="hc-page-intro">
        <div class="hc-page-intro-copy">
          <div class="hc-page-intro-title">{{ t('knowledgeTitle') || 'Knowledge' }}</div>
          <div class="hc-page-intro-text">
            {{ t('knowledgeIntroText') }}
          </div>
          <div class="hc-result-chip-row" style="margin-top:12px">
            <span class="hc-result-chip">{{ t('knowledgeChipMemory') }}</span>
            <span class="hc-result-chip">{{ t('knowledgeChipSources') }}</span>
            <span class="hc-result-chip">{{ t('knowledgeChipArtifacts') }}</span>
          </div>
        </div>
        <div class="hc-page-section-actions">
          <button class="hc-btn hc-btn-secondary" @click="refreshActiveTab()">{{ t('refresh') || 'Refresh' }}</button>
          <button v-if="tab === 'memory'" class="hc-btn hc-btn-primary" data-testid="knowledge-memory-toggle-form" @click="toggleMemAdd()">{{ memShowAdd ? (t('cancel') || 'Cancel') : (t('addEntry') || 'Add Entry') }}</button>
          <button v-else-if="tab === 'sources'" class="hc-btn hc-btn-primary" data-testid="knowledge-sources-toggle-form" @click="openSourceCreate()">{{ srcShowForm ? (t('cancel') || 'Cancel') : t('knowledgeAddSource') }}</button>
        </div>
        <div class="hc-page-metrics">
          <div v-for="metric in knowledgeMetrics()" :key="metric.label" class="hc-page-metric-card">
            <div class="hc-page-metric-label">{{ metric.label }}</div>
            <div class="hc-page-metric-value">{{ metric.value }}</div>
            <div class="hc-page-metric-note">{{ metric.note }}</div>
          </div>
        </div>
      </div>

      <div class="hc-tabs">
        <button class="hc-tab" data-testid="knowledge-tab-memory" :class="{ active: tab === 'memory' }" @click="switchTab('memory')">{{ t('memoryTitle') || 'Memory' }}</button>
        <button class="hc-tab" data-testid="knowledge-tab-sources" :class="{ active: tab === 'sources' }" @click="switchTab('sources')">{{ t('knowledgeSourcesTab') }}</button>
        <button class="hc-tab" data-testid="knowledge-tab-artifacts" :class="{ active: tab === 'artifacts' }" @click="switchTab('artifacts')">{{ t('artifactsTitle') || 'Artifacts' }}</button>
      </div>

      <div v-if="tab === 'memory'" class="hc-list-detail-layout">
        <div class="hc-card hc-list-panel">
          <div class="hc-page-section-head">
            <div class="hc-page-section-copy">
              <div class="hc-settings-section-title">{{ t('memoryTitle') || 'Memory' }}</div>
              <div class="hc-settings-section-desc">{{ t('knowledgeMemoryDesc') }}</div>
            </div>
          </div>

          <div style="display:flex;flex-wrap:wrap;gap:8px;margin-bottom:12px">
            <input class="hc-form-input" type="text" v-model="memSearch" :placeholder="t('searchMemory') || 'Search memory...'" @keyup.enter="loadMemory()" style="flex:1;min-width:140px" />
            <input class="hc-form-input" type="text" v-model="memScopeKey" :placeholder="t('knowledgeAppliesTo') || 'Applies To'" @keyup.enter="loadMemory()" style="flex:0 1 180px;min-width:120px" />
            <button class="hc-btn hc-btn-secondary" @click="loadMemory()">{{ t('search') || 'Search' }}</button>
            <button class="hc-btn hc-btn-ghost" @click="clearMemoryFilters()">{{ t('clear') || 'Clear' }}</button>
          </div>

          <div class="hc-result-chip-row" style="margin-bottom:12px">
            <button
              v-for="item in memNamespaceOptions"
              :key="'mem-chip-' + item.value"
              type="button"
              class="hc-result-chip"
              :style="item.value === memNamespace ? 'border-color:var(--accent);color:var(--accent)' : ''"
              @click="memNamespace = item.value; loadMemory()"
            >
              {{ item.label }}
            </button>
          </div>

          <div v-if="memError" class="hc-state-block">
            <div class="hc-state-block-title">{{ t('loadError') || 'Failed to load memory' }}</div>
            <div class="hc-state-block-copy">{{ memError }}</div>
            <div class="hc-state-block-actions">
              <button class="hc-btn hc-btn-primary hc-btn-sm" @click="refreshMemorySurface()">{{ t('retryLoad') || 'Retry' }}</button>
            </div>
          </div>
          <div v-else-if="memLoading" class="hc-state-block">
            <div class="hc-state-block-title">{{ t('loading') || 'Loading...' }}</div>
            <div class="hc-state-block-copy">{{ t('knowledgeMemoryLoading') }}</div>
          </div>
          <div v-else-if="memItems.length === 0" class="hc-state-block" data-testid="knowledge-memory-empty">
            <div class="hc-state-block-title">{{ t('noMemoryEntries') || 'No memory entries found.' }}</div>
            <div class="hc-state-block-copy">{{ t('knowledgeMemoryEmpty') }}</div>
            <div class="hc-state-block-actions">
              <button class="hc-btn hc-btn-primary hc-btn-sm" @click="toggleMemAdd()">{{ t('addEntry') || 'Add Entry' }}</button>
            </div>
          </div>
          <div v-else class="hc-list-card-grid">
            <button
              v-for="entry in memPaginated"
              :key="entry.key"
              type="button"
              :data-testid="'knowledge-memory-entry-' + entry.key"
              class="hc-list-card"
              :class="{ active: memSelectedKey === entry.key && !memShowAdd }"
              @click="memSelect(entry.key)"
            >
              <div class="hc-list-card-head">
                <div>
                  <div class="hc-list-card-title">{{ entry.label || prettyField(entry.field) || entry.key }}</div>
                  <div class="hc-list-card-subtitle">{{ truncate(entry.value, memTruncateLen) }}</div>
                </div>
                <span class="hc-badge" :class="namespaceBadgeClass(entry.namespace)">{{ namespaceLabel(entry.namespace) }}</span>
              </div>
              <div class="hc-list-card-content">
                {{ memoryAppliesToLabel(entry) }} · {{ fmtTime(entry.updated_at) || '-' }}
              </div>
            </button>
          </div>

          <div v-if="memItems.length > 0 && memTotalPages > 1" class="hc-pagination">
            <span class="hc-pagination-info">{{ (memPage-1)*Number(memPageSize)+1 }}-{{ Math.min(memPage*Number(memPageSize), memItems.length) }} {{ t('of') || 'of' }} {{ memItems.length }}</span>
            <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="memPage<=1" @click="memPrevPage()">&#8249;</button>
            <span class="hc-pagination-pages">{{ memPage }} / {{ memTotalPages }}</span>
            <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="memPage>=memTotalPages" @click="memNextPage()">&#8250;</button>
            <select class="hc-form-select hc-pagination-size" v-model="memPageSize" @change="memPage=1; memSyncSelection()">
              <option value="20">20</option>
              <option value="50">50</option>
              <option value="100">100</option>
            </select>
          </div>
        </div>

        <div class="hc-card hc-detail-panel">
          <div v-if="memShowAdd">
            <div class="hc-detail-panel-head">
              <div class="hc-detail-panel-copy">
                <div class="hc-detail-panel-title">{{ t('addEntry') || 'Add Entry' }}</div>
                <div class="hc-detail-panel-subtitle">{{ t('knowledgeMemoryAddDesc') }}</div>
              </div>
            </div>
            <div class="hc-detail-grid">
              <div class="hc-form-group">
                <label class="hc-form-label">{{ t('knowledgeMemoryType') || 'Memory Type' }}</label>
                <select class="hc-form-select" v-model="memNewNamespace" @change="memApplyNamespaceDefaults()">
                  <option v-for="item in memNamespaceOptions.filter(item => item.value)" :key="'new-ns-' + item.value" :value="item.value">{{ item.label }}</option>
                </select>
              </div>
              <div class="hc-form-group">
                <label class="hc-form-label">{{ t('knowledgeAppliesTo') || 'Applies To' }}</label>
                <input class="hc-form-input" type="text" v-model="memNewScopeKey" :placeholder="memoryScopePlaceholder(memNewNamespace)" />
              </div>
              <div class="hc-form-group">
                <label class="hc-form-label">{{ t('knowledgeField') || 'Field' }}</label>
                <input class="hc-form-input" data-testid="knowledge-memory-field-field" type="text" v-model="memNewField" placeholder="name / reply_language / goal" />
              </div>
              <div class="hc-form-group">
                <label class="hc-form-label">{{ t('name') || 'Name' }}</label>
                <input class="hc-form-input" data-testid="knowledge-memory-field-name" type="text" v-model="memNewLabel" placeholder="Optional display label" />
              </div>
            </div>
            <div class="hc-form-group">
              <label class="hc-form-label">{{ t('value') || 'Value' }}</label>
              <textarea class="hc-form-input" data-testid="knowledge-memory-field-value" v-model="memNewValue" :placeholder="t('enterValue') || 'Enter value...'" rows="12" style="resize:vertical"></textarea>
            </div>
            <div class="hc-detail-panel-actions">
              <span class="hc-result-chip">{{ t('knowledgeManagedRecord') || 'Managed record' }}</span>
              <div class="hc-toolbar-actions">
                <button class="hc-btn hc-btn-ghost" @click="toggleMemAdd()">{{ t('cancel') || 'Cancel' }}</button>
                <button class="hc-btn hc-btn-primary" data-testid="knowledge-memory-save" :disabled="!memCanSaveNew()" @click="memSaveNew()">{{ t('save') || 'Save' }}</button>
              </div>
            </div>
          </div>

          <div v-else-if="memSelectedEntry">
            <div class="hc-detail-panel-head">
              <div class="hc-detail-panel-copy">
                <div class="hc-detail-panel-title">{{ memSelectedEntry.label || prettyField(memSelectedEntry.field) || memSelectedEntry.key }}</div>
                <div class="hc-detail-panel-subtitle">{{ memSelectedEntry.key }} · {{ t('updated') || 'Updated' }} {{ fmtTime(memSelectedEntry.updated_at) || '-' }}</div>
              </div>
              <div class="hc-toolbar-actions">
                <button v-if="memEditingKey !== memSelectedEntry.key" class="hc-btn hc-btn-secondary" @click="memStartEdit(memSelectedEntry)">{{ t('edit') || 'Edit' }}</button>
                <button class="hc-btn hc-btn-danger" data-testid="knowledge-memory-delete" @click="memDelete(memSelectedEntry.key)">{{ t('delete') || 'Delete' }}</button>
              </div>
            </div>

            <div class="hc-detail-grid">
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('knowledgeMemoryType') || 'Memory Type' }}</div>
                <div class="hc-detail-stat-value"><span class="hc-badge" :class="namespaceBadgeClass(memSelectedEntry.namespace)">{{ namespaceLabel(memSelectedEntry.namespace) }}</span></div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('knowledgeAppliesTo') || 'Applies To' }}</div>
                <div class="hc-detail-stat-value" style="font-family:var(--font-mono)">{{ memoryAppliesToLabel(memSelectedEntry) }}</div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('knowledgeField') || 'Field' }}</div>
                <div class="hc-detail-stat-value">{{ prettyField(memSelectedEntry.field) }}</div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('knowledgeConfidence') || 'Confidence' }}</div>
                <div class="hc-detail-stat-value">{{ confidenceText(memSelectedEntry.confidence) }}</div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('knowledgeSource') || 'Source' }}</div>
                <div class="hc-detail-stat-value">{{ memSelectedEntry.source || '-' }}</div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('knowledgeEvidence') || 'Evidence' }}</div>
                <div class="hc-detail-stat-value">{{ memSelectedEntry.evidence_count || 0 }}</div>
              </div>
            </div>

            <div v-if="memSelectedEntry.tags && memSelectedEntry.tags.length" class="hc-result-chip-row" style="margin-bottom:12px">
              <span v-for="tag in memSelectedEntry.tags" :key="memSelectedEntry.key + '-tag-' + tag" class="hc-result-chip">{{ tag }}</span>
            </div>

            <div v-if="memEditingKey === memSelectedEntry.key" class="hc-form-group" style="margin-bottom:16px">
              <label class="hc-form-label">{{ t('editValue') || 'Edit Value' }}</label>
              <textarea class="hc-form-input" v-model="memEditValue" rows="14" style="resize:vertical;font-family:var(--font-mono)"></textarea>
              <div class="hc-detail-panel-actions">
                <span class="hc-result-chip">{{ t('knowledgeEditing') || 'Editing' }}</span>
                <div class="hc-toolbar-actions">
                  <button class="hc-btn hc-btn-ghost" @click="memCancelEdit()">{{ t('cancel') || 'Cancel' }}</button>
                  <button class="hc-btn hc-btn-primary" @click="memSaveEdit(memSelectedEntry.key)">{{ t('save') || 'Save' }}</button>
                </div>
              </div>
            </div>
            <pre v-else class="hc-detail-pre">{{ memSelectedEntry.value || '' }}</pre>

            <div v-if="memSelectedEntry.previous_values && memSelectedEntry.previous_values.length" class="hc-state-block hc-state-block-inline" style="margin-top:16px">
              <div class="hc-state-block-title">{{ t('knowledgePreviousValues') || 'Previous Values' }}</div>
              <div class="hc-state-block-copy">{{ t('knowledgePreviousValuesDesc') || 'Conflict merges keep earlier values instead of silently overwriting them.' }}</div>
              <pre class="hc-detail-pre" style="margin-top:12px">{{ memSelectedEntry.previous_values.join('\\n\\n---\\n\\n') }}</pre>
            </div>

            <div class="hc-state-block hc-state-block-inline" style="margin-top:16px">
              <div class="hc-state-block-title">{{ t('knowledgeMemoryNotebook') || 'Memory Notebook' }}</div>
              <div class="hc-state-block-copy">{{ memNotebookPathSummary() }}</div>
              <pre class="hc-detail-pre" style="margin-top:12px">{{ memNotebookPreview }}</pre>
            </div>
          </div>

          <div v-else class="hc-state-block hc-state-block-inline">
            <div class="hc-state-block-title">{{ t('memoryTitle') || 'Memory' }}</div>
            <div class="hc-state-block-copy">{{ t('knowledgeMemoryDetailEmpty') }}</div>
          </div>
        </div>
      </div>

      <div v-if="tab === 'sources'" class="hc-list-detail-layout">
        <div class="hc-card hc-list-panel">
          <div class="hc-page-section-head">
            <div class="hc-page-section-copy">
              <div class="hc-settings-section-title">{{ t('knowledgeSourcesTitle') }}</div>
              <div class="hc-settings-section-desc">{{ t('knowledgeSourcesDesc') }}</div>
            </div>
          </div>

          <div style="display:flex;flex-wrap:wrap;gap:8px;margin-bottom:12px">
            <input class="hc-form-input" type="text" v-model="srcSearch" :placeholder="t('knowledgeSourcesSearch')" @input="srcApplyFilters()" style="flex:1;min-width:140px" />
            <select class="hc-form-select" v-model="srcKindFilter" @change="srcApplyFilters()" style="flex:0 1 180px;min-width:120px">
              <option value="">{{ t('knowledgeSourcesAllKinds') }}</option>
              <option v-for="item in sourceKindOptions" :key="'src-kind-' + item.value" :value="item.value">{{ item.label }}</option>
            </select>
            <select class="hc-form-select" v-model="srcStatusFilter" @change="srcApplyFilters()" style="flex:0 1 160px;min-width:120px">
              <option v-for="item in sourceStatusOptions" :key="'src-status-' + item.value" :value="item.value">{{ item.label }}</option>
            </select>
            <button class="hc-btn hc-btn-ghost" @click="clearSourceFilters()">{{ t('clear') || 'Clear' }}</button>
          </div>

          <div v-if="srcError" class="hc-state-block">
            <div class="hc-state-block-title">{{ t('knowledgeSourcesLoadError') }}</div>
            <div class="hc-state-block-copy">{{ srcError }}</div>
            <div class="hc-state-block-actions">
              <button class="hc-btn hc-btn-primary hc-btn-sm" @click="loadSources()">{{ t('retryLoad') }}</button>
            </div>
          </div>
          <div v-else-if="srcLoading" class="hc-state-block">
            <div class="hc-state-block-title">{{ t('loading') || 'Loading...' }}</div>
            <div class="hc-state-block-copy">{{ t('knowledgeSourcesLoading') }}</div>
          </div>
          <div v-else-if="srcFiltered.length === 0" class="hc-state-block" data-testid="knowledge-sources-empty">
            <div class="hc-state-block-title">{{ t('knowledgeSourcesEmpty') }}</div>
            <div class="hc-state-block-copy">{{ t('knowledgeSourcesEmptyDesc') }}</div>
            <div class="hc-state-block-actions">
              <button class="hc-btn hc-btn-primary hc-btn-sm" @click="openSourceCreate()">{{ t('knowledgeAddSource') }}</button>
            </div>
          </div>
          <div v-else class="hc-list-card-grid">
            <button
              v-for="source in srcFiltered"
              :key="source.id"
              type="button"
              :data-testid="'knowledge-source-entry-' + source.id"
              class="hc-list-card"
              :class="{ active: srcSelectedID === source.id && !srcShowForm }"
              @click="srcSelect(source.id)"
            >
              <div class="hc-list-card-head">
                <div>
                  <div class="hc-list-card-title">{{ source.name || source.id }}</div>
                  <div class="hc-list-card-subtitle">{{ sourceKindLabel(source.kind) }} · {{ source.stats && source.stats.documents || 0 }} docs · {{ source.stats && source.stats.chunks || 0 }} chunks</div>
                </div>
                <span class="hc-badge" :class="sourceStatusBadgeClass(source.status)">{{ sourceStatusLabel(source.status) }}</span>
              </div>
              <div class="hc-list-card-content">
                {{ source.enabled ? (t('knowledgeSourceEnabledLabel') || 'Enabled') : (t('knowledgeSourceDisabledLabel') || 'Disabled') }} · {{ source.last_sync_at ? ((t('knowledgeSourceSynced') || 'Synced') + ' ' + fmtTime(source.last_sync_at)) : (t('knowledgeSourceNotSyncedYet') || 'Not synced yet') }}
              </div>
            </button>
          </div>
        </div>

        <div class="hc-card hc-detail-panel">
          <div v-if="srcShowForm">
            <div class="hc-detail-panel-head">
              <div class="hc-detail-panel-copy">
                <div class="hc-detail-panel-title">{{ srcEditingID ? t('knowledgeEditSource') : t('knowledgeAddSource') }}</div>
                <div class="hc-detail-panel-subtitle">{{ t('knowledgeSourceFormDesc') }}</div>
              </div>
            </div>

            <div class="hc-detail-grid">
              <div class="hc-form-group">
                <label class="hc-form-label">{{ t('name') || 'Name' }}</label>
                <input class="hc-form-input" data-testid="knowledge-source-field-name" type="text" v-model="srcForm.name" placeholder="Engineering docs / Client FAQ / Research URLs" />
              </div>
              <div class="hc-form-group">
                <label class="hc-form-label">{{ t('kind') || 'Kind' }}</label>
                <select class="hc-form-select" data-testid="knowledge-source-field-kind" v-model="srcForm.kind" :disabled="Boolean(srcEditingID)" @change="srcChangeKind()">
                  <option v-for="item in sourceKindOptions" :key="'src-form-kind-' + item.value" :value="item.value">{{ item.label }}</option>
                </select>
              </div>
            </div>

            <div class="hc-form-group">
              <label class="hc-form-label">
                <input type="checkbox" v-model="srcForm.enabled" style="margin-right:8px" />
                {{ t('knowledgeEnableSourceImmediately') || 'Enable source immediately' }}
              </label>
            </div>
            <div v-for="field in currentSourceFields()" :key="'src-field-' + srcForm.kind + '-' + field.id">
              <div v-if="field.type === 'boolean'" class="hc-form-group">
                <label class="hc-form-label">
                  <input type="checkbox" v-model="srcForm.values[field.id]" style="margin-right:8px" />
                  {{ field.label }}
                </label>
                <div v-if="field.description" class="hc-form-hint">{{ field.description }}</div>
              </div>
              <div v-else class="hc-form-group">
                <label class="hc-form-label">{{ field.label }}</label>
                  <textarea
                    v-if="field.type === 'string_list'"
                    class="hc-form-input"
                    :data-testid="'knowledge-source-field-' + field.id"
                    v-model="srcForm.values[field.id]"
                  :rows="sourceFieldRows(field)"
                  :placeholder="sourceFieldPlaceholder(field)"
                  style="resize:vertical"
                ></textarea>
                <input
                  v-else
                  class="hc-form-input"
                  :data-testid="'knowledge-source-field-' + field.id"
                  :type="field.secret ? 'password' : 'text'"
                  v-model="srcForm.values[field.id]"
                  :placeholder="sourceFieldPlaceholder(field)"
                />
                <div v-if="field.description" class="hc-form-hint">{{ field.description }}</div>
              </div>
            </div>

            <div class="hc-state-block hc-state-block-inline" style="margin-top:16px">
              <div class="hc-state-block-title">{{ sourceKindLabel(srcForm.kind) }}</div>
              <div class="hc-state-block-copy">{{ currentSourceKindDescription() }}</div>
            </div>

            <div class="hc-detail-panel-actions">
              <span class="hc-result-chip">{{ t('knowledgeExternalFirstKnowledge') || 'External-first knowledge' }}</span>
              <div class="hc-toolbar-actions">
                <button class="hc-btn hc-btn-ghost" @click="closeSourceForm()">{{ t('cancel') || 'Cancel' }}</button>
                <button class="hc-btn hc-btn-secondary" :disabled="!srcCanSave()" @click="saveSource(false)">{{ t('save') || 'Save' }}</button>
                <button class="hc-btn hc-btn-primary" data-testid="knowledge-source-create-sync" :disabled="!srcCanSave()" @click="saveSource(true)">{{ srcEditingID ? (t('knowledgeSaveAndSync') || 'Save & Sync') : (t('knowledgeCreateAndSync') || 'Create & Sync') }}</button>
              </div>
            </div>
          </div>

          <div v-else-if="srcSelectedItem">
            <div class="hc-detail-panel-head">
              <div class="hc-detail-panel-copy">
                <div class="hc-detail-panel-title">{{ srcSelectedItem.name || srcSelectedItem.id }}</div>
                <div class="hc-detail-panel-subtitle">{{ sourceKindLabel(srcSelectedItem.kind) }} · {{ srcSelectedItem.id }}</div>
              </div>
              <div class="hc-toolbar-actions">
                <button class="hc-btn hc-btn-secondary" @click="syncSource(srcSelectedItem)">{{ t('knowledgeSourceSync') || 'Sync' }}</button>
                <button class="hc-btn hc-btn-ghost" @click="openSourceEdit(srcSelectedItem)">{{ t('edit') || 'Edit' }}</button>
                <button class="hc-btn hc-btn-danger" data-testid="knowledge-source-delete" @click="deleteSource(srcSelectedItem)">{{ t('delete') || 'Delete' }}</button>
              </div>
            </div>

            <div class="hc-detail-grid">
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('knowledgeSourceStatus') || 'Status' }}</div>
                <div class="hc-detail-stat-value"><span class="hc-badge" :class="sourceStatusBadgeClass(srcSelectedItem.status)">{{ sourceStatusLabel(srcSelectedItem.status) }}</span></div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('knowledgeSourceDocuments') || 'Documents' }}</div>
                <div class="hc-detail-stat-value">{{ srcSelectedItem.stats && srcSelectedItem.stats.documents || 0 }}</div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('knowledgeSourceChunks') || 'Chunks' }}</div>
                <div class="hc-detail-stat-value">{{ srcSelectedItem.stats && srcSelectedItem.stats.chunks || 0 }}</div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('size') || 'Size' }}</div>
                <div class="hc-detail-stat-value">{{ fmtSize(srcSelectedItem.stats && srcSelectedItem.stats.bytes || 0) }}</div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('enabled') || 'Enabled' }}</div>
                <div class="hc-detail-stat-value">{{ srcSelectedItem.enabled ? t('yes') : t('no') }}</div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('knowledgeSourceLastSync') || 'Last Sync' }}</div>
                <div class="hc-detail-stat-value">{{ srcSelectedItem.last_sync_at ? fmtTime(srcSelectedItem.last_sync_at) : '-' }}</div>
              </div>
            </div>

            <div class="hc-state-block hc-state-block-inline" style="margin-top:16px">
              <div class="hc-state-block-title">{{ t('knowledgeSourceConnectorHealth') || 'Connector Health' }}</div>
              <div class="hc-state-block-copy">{{ sourceHealthCopy(srcSelectedItem) }}</div>
              <div class="hc-result-chip-row" style="margin-top:12px">
                <span class="hc-result-chip">{{ sourceHealthLabel(srcSelectedItem) }}</span>
                <span class="hc-result-chip">{{ sourceRecentSummary(srcSelectedItem) }}</span>
                <span class="hc-result-chip">{{ sourceRetrievalSummary(srcSelectedItem) }}</span>
              </div>
            </div>

            <div v-if="srcSelectedItem.configured_secrets && srcSelectedItem.configured_secrets.length" class="hc-state-block hc-state-block-inline" style="margin-top:16px">
              <div class="hc-state-block-title">{{ t('knowledgeSourceConfiguredSecrets') || 'Configured Secrets' }}</div>
              <div class="hc-state-block-copy">{{ t('knowledgeSourceConfiguredSecretsDesc') || 'Stored in keychain / secret store and intentionally never echoed back in the console response.' }}</div>
              <div class="hc-result-chip-row" style="margin-top:12px">
                <span v-for="field in srcSelectedItem.configured_secrets" :key="srcSelectedItem.id + '-secret-' + field" class="hc-result-chip">{{ prettyField(field) }}</span>
              </div>
            </div>

            <div class="hc-state-block hc-state-block-inline" style="margin-top:16px">
              <div class="hc-state-block-title">{{ t('knowledgeSourceConnectorNote') || 'Connector Note' }}</div>
              <div class="hc-state-block-copy">{{ srcSelectedItem.connector_note || currentSourceKindDescription(srcSelectedItem.kind) }}</div>
            </div>

            <div v-if="srcSelectedItem.last_error" class="hc-state-block hc-state-block-inline" style="margin-top:16px">
              <div class="hc-state-block-title">{{ t('knowledgeSourceErrorSummary') || 'Error Summary' }}</div>
              <div class="hc-state-block-copy">{{ srcSelectedItem.last_error }}</div>
            </div>
            <div v-else class="hc-state-block hc-state-block-inline" style="margin-top:16px">
              <div class="hc-state-block-title">{{ t('knowledgeSourceErrorSummary') || 'Error Summary' }}</div>
              <div class="hc-state-block-copy">{{ t('knowledgeSourceNoRecentError') || 'No recent connector error is recorded for this source.' }}</div>
            </div>

            <div class="hc-state-block hc-state-block-inline" style="margin-top:16px">
              <div class="hc-state-block-title">{{ t('knowledgeSourceRecentSync') || 'Recent Sync' }}</div>
              <div class="hc-state-block-copy">{{ sourceRecentCopy(srcSelectedItem) }}</div>
            </div>

            <div class="hc-state-block hc-state-block-inline" style="margin-top:16px">
              <div class="hc-state-block-title">{{ t('knowledgeSourceConfiguration') || 'Configuration' }}</div>
              <pre class="hc-detail-pre" style="margin-top:12px">{{ sourceConfigPreview(srcSelectedItem) }}</pre>
            </div>

            <div class="hc-page-section-head" style="margin-top:18px">
              <div class="hc-page-section-copy">
                <div class="hc-settings-section-title">{{ t('knowledgeSourceRetrievalPreview') || 'Retrieval Preview' }}</div>
                <div class="hc-settings-section-desc">{{ t('knowledgeSourceRetrievalPreviewDesc') || 'Preview what the agent can retrieve after indexing, and verify the source is usable before wiring it into real tasks.' }}</div>
              </div>
            </div>
            <div class="hc-toolbar-grid">
              <input class="hc-form-input" type="text" v-model="srcSearchQuery" :placeholder="t('knowledgeSourceSearchPlaceholder') || 'Search indexed knowledge...'" @keyup.enter="runSourceSearch()" />
              <button class="hc-btn hc-btn-primary" @click="runSourceSearch()">{{ t('search') || 'Search' }}</button>
              <button class="hc-btn hc-btn-ghost" @click="clearSourceSearch()">{{ t('clear') || 'Clear' }}</button>
            </div>

            <div v-if="srcSearchError" class="hc-state-block hc-state-block-inline" style="margin-top:12px">
              <div class="hc-state-block-title">{{ t('knowledgeSourceSearchFailed') || 'Search failed' }}</div>
              <div class="hc-state-block-copy">{{ srcSearchError }}</div>
            </div>
            <div v-else-if="srcSearchLoading" class="hc-state-block hc-state-block-inline" style="margin-top:12px">
              <div class="hc-state-block-title">{{ t('loading') || 'Loading...' }}</div>
              <div class="hc-state-block-copy">{{ t('knowledgeSourceSearchingChunks') || 'Searching indexed chunks for the current source.' }}</div>
            </div>
            <div v-else-if="srcSearchQuery.trim() && srcSearchResults.length === 0" class="hc-state-block hc-state-block-inline" style="margin-top:12px">
              <div class="hc-state-block-title">{{ t('knowledgeSourceNoMatches') || 'No matches' }}</div>
              <div class="hc-state-block-copy">{{ t('knowledgeSourceNoMatchesDesc') || 'Try another phrase or sync the source again if the content changed.' }}</div>
            </div>
            <div v-else-if="srcSearchResults.length" class="hc-list-card-grid" style="margin-top:12px">
              <div v-for="item in srcSearchResults" :key="item.chunk_id" class="hc-list-card" style="cursor:default">
                <div class="hc-list-card-head">
                  <div>
                    <div class="hc-list-card-title">{{ item.title || item.document_id || item.path || item.chunk_id }}</div>
                    <div class="hc-list-card-subtitle">{{ truncate(item.path || item.uri || item.document_id || '-', 88) }}</div>
                  </div>
                  <span class="hc-badge hc-badge-gray">{{ Number(item.score || 0).toFixed(2) }}</span>
                </div>
                <div class="hc-list-card-content">{{ item.preview || '-' }}</div>
              </div>
            </div>
          </div>

          <div v-else class="hc-state-block hc-state-block-inline">
            <div class="hc-state-block-title">{{ t('knowledgeSourcesTitle') || 'Sources' }}</div>
            <div class="hc-state-block-copy">{{ t('knowledgeSourceDetailEmpty') || 'Pick a knowledge source on the left to inspect sync status, indexed stats, and live search results.' }}</div>
          </div>
        </div>
      </div>

      <div v-if="tab === 'artifacts'" class="hc-list-detail-layout">
        <div class="hc-card hc-list-panel">
          <div class="hc-page-section-head">
            <div class="hc-page-section-copy">
              <div class="hc-settings-section-title">{{ t('artifactsTitle') || 'Artifacts' }}</div>
              <div class="hc-settings-section-desc">{{ t('knowledgeArtifactsDesc') }}</div>
            </div>
          </div>
          <div style="display:flex;flex-wrap:wrap;gap:8px;margin-bottom:12px">
            <input class="hc-form-input" type="text" v-model="artSearchName" :placeholder="t('searchByName') || 'Search by name...'" @input="artApplyFilters()" style="flex:1;min-width:120px" />
            <select class="hc-form-select" v-model="artFilterKind" @change="artApplyFilters()" style="flex:0 0 auto">
              <option value="">{{ t('allKinds') || 'All Kinds' }}</option>
              <option v-for="k in artKindOptions" :key="k" :value="k">{{ k }}</option>
            </select>
            <button class="hc-btn hc-btn-ghost" @click="clearArtifactFilters()">{{ t('clear') || 'Clear' }}</button>
          </div>

          <div v-if="artError" class="hc-state-block">
            <div class="hc-state-block-title">{{ t('loadError') || 'Failed to load artifacts' }}</div>
            <div class="hc-state-block-copy">{{ artError }}</div>
            <div class="hc-state-block-actions">
              <button class="hc-btn hc-btn-primary hc-btn-sm" @click="loadArtifacts()">{{ t('retryLoad') || 'Retry' }}</button>
            </div>
          </div>
          <div v-else-if="artLoading" class="hc-state-block">
            <div class="hc-state-block-title">{{ t('loading') || 'Loading...' }}</div>
            <div class="hc-state-block-copy">{{ t('knowledgeArtifactsLoading') }}</div>
          </div>
          <div v-else-if="artFiltered.length === 0" class="hc-state-block">
            <div class="hc-state-block-title">{{ t('noArtifacts') || 'No artifacts found.' }}</div>
            <div class="hc-state-block-copy">{{ t('knowledgeArtifactsEmpty') }}</div>
          </div>
          <div v-else class="hc-list-card-grid">
            <button
              v-for="artifact in artPaginated"
              :key="artifact.id"
              type="button"
              class="hc-list-card"
              :class="{ active: artSelectedID === artifact.id }"
              @click="artSelect(artifact.id)"
            >
              <div class="hc-list-card-head">
                <div>
                  <div class="hc-list-card-title">{{ truncate(artifact.name || artifact.id || '-', artTruncateLen) }}</div>
                  <div class="hc-list-card-subtitle">{{ artifact.kind || '-' }} · {{ fmtSize(artifact.size) }}</div>
                </div>
                <span class="hc-badge hc-badge-gray">{{ fmtTime(artifact.created_at) || '-' }}</span>
              </div>
              <div class="hc-list-card-content">
                Session {{ shortId(artifact.session_id) }} · Run {{ shortId(artifact.run_id) }}
              </div>
            </button>
          </div>

          <div v-if="artFiltered.length > 0 && artTotalPages > 1" class="hc-pagination">
            <span class="hc-pagination-info">{{ (artPage-1)*Number(artPageSize)+1 }}-{{ Math.min(artPage*Number(artPageSize), artFiltered.length) }} {{ t('of') || 'of' }} {{ artFiltered.length }}</span>
            <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="artPage<=1" @click="artPrevPage()">&#8249;</button>
            <span class="hc-pagination-pages">{{ artPage }} / {{ artTotalPages }}</span>
            <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="artPage>=artTotalPages" @click="artNextPage()">&#8250;</button>
            <select class="hc-form-select hc-pagination-size" v-model="artPageSize" @change="artPage=1; artSyncSelection()">
              <option value="20">20</option>
              <option value="50">50</option>
              <option value="100">100</option>
            </select>
          </div>
        </div>

        <div class="hc-card hc-detail-panel">
          <div v-if="artSelectedItem">
            <div class="hc-detail-panel-head">
              <div class="hc-detail-panel-copy">
                <div class="hc-detail-panel-title">{{ artSelectedItem.name || artSelectedItem.id || '-' }}</div>
                <div class="hc-detail-panel-subtitle">{{ artSelectedItem.kind || '-' }} · {{ fmtTime(artSelectedItem.created_at) || '-' }}</div>
              </div>
              <div class="hc-toolbar-actions">
                <button class="hc-btn hc-btn-secondary" @click="artPreview(artSelectedItem)">{{ t('preview') || 'Preview' }}</button>
                <a class="hc-btn hc-btn-ghost" :href="artifactPreviewHref(artSelectedItem)" download>{{ t('download') || 'Download' }}</a>
              </div>
            </div>

            <div class="hc-detail-grid">
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('kind') || 'Kind' }}</div>
                <div class="hc-detail-stat-value">{{ artSelectedItem.kind || '-' }}</div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('size') || 'Size' }}</div>
                <div class="hc-detail-stat-value">{{ fmtSize(artSelectedItem.size) }}</div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('session') || 'Session' }}</div>
                <div class="hc-detail-stat-value" style="font-family:var(--font-mono)">{{ artSelectedItem.session_id || '-' }}</div>
              </div>
              <div class="hc-detail-stat">
                <div class="hc-detail-stat-label">{{ t('run') || 'Run' }}</div>
                <div class="hc-detail-stat-value" style="font-family:var(--font-mono)">{{ artSelectedItem.run_id || '-' }}</div>
              </div>
            </div>

            <pre class="hc-detail-pre">{{ artifactPreviewHref(artSelectedItem) || artSelectedItem.uri || '-' }}</pre>
          </div>
          <div v-else class="hc-state-block hc-state-block-inline">
            <div class="hc-state-block-title">{{ t('artifactsTitle') || 'Artifacts' }}</div>
            <div class="hc-state-block-copy">{{ t('knowledgeArtifactDetailEmpty') || 'Pick an artifact on the left to inspect its metadata and open the generated output.' }}</div>
          </div>
        </div>
      </div>
    </div>
  `;

  return {
    $template,
    t,
    fmtTime,
    fmtSize,
    truncate,
    shortId,
    prettyField,
    namespaceLabel,
    namespaceBadgeClass,
    confidenceText,
    memoryAppliesToLabel,
    memoryScopePlaceholder,
    sourceKindLabel,
    sourceStatusLabel,
    sourceStatusBadgeClass,

    tab: 'memory',

    memItems: [],
    memLoading: true,
    memError: null,
    memSearch: '',
    memNamespace: '',
    memScopeKey: '',
    get memNamespaceOptions() { return buildMemNamespaceOptions(); },
    memSelectedKey: '',
    memEditingKey: null,
    memEditValue: '',
    memShowAdd: false,
    memNewNamespace: 'profile',
    memNewScopeKey: 'user',
    memNewField: '',
    memNewLabel: '',
    memNewValue: '',
    memPage: 1,
    memPageSize: DEFAULT_PAGE_SIZE,
    memTruncateLen: MEM_VALUE_TRUNCATE_LEN,
    memNotebook: null,
    memNotebookError: null,

    sourceKindOptions: normalizeSourceKindOptions(sourceKindCatalog),
    get sourceStatusOptions() { return buildSourceStatusOptions(); },
    srcItems: [],
    srcFiltered: [],
    srcLoading: false,
    srcError: null,
    srcSearch: '',
    srcKindFilter: '',
    srcStatusFilter: '',
    srcSelectedID: '',
    srcShowForm: false,
    srcEditingID: '',
    srcForm: emptySourceForm(),
    srcSearchQuery: '',
    srcSearchResults: [],
    srcSearchLoading: false,
    srcSearchError: null,

    artItems: [],
    artFiltered: [],
    artLoading: false,
    artError: null,
    artSearchName: '',
    artFilterKind: '',
    artFilterSession: '',
    artKindOptions: KIND_OPTIONS,
    artSelectedID: '',
    artPage: 1,
    artPageSize: DEFAULT_PAGE_SIZE,
    artTruncateLen: ART_NAME_TRUNCATE_LEN,

    artifactPreviewHref(item) {
      return artifactPreviewPath(item);
    },

    switchTab(newTab) {
      this.tab = newTab;
      if (newTab === 'memory' && this.memItems.length === 0 && !this.memLoading) this.refreshMemorySurface();
      if (newTab === 'sources' && this.srcItems.length === 0 && !this.srcLoading) this.loadSources();
      if (newTab === 'artifacts' && this.artItems.length === 0 && !this.artLoading) this.loadArtifacts();
    },

    refreshActiveTab() {
      if (this.tab === 'memory') this.refreshMemorySurface();
      else if (this.tab === 'sources') this.loadSources();
      else this.loadArtifacts();
    },

    refreshMemorySurface() {
      this.loadMemory();
      this.loadMemoryNotebook();
    },

    knowledgeMetrics() {
      const memLatest = latestTimestamp(this.memItems, 'updated_at');
      const srcLatest = latestTimestamp(this.srcItems, 'last_sync_at') || latestTimestamp(this.srcItems, 'updated_at');
      const artLatest = latestTimestamp(this.artItems, 'created_at');
      return [
        {
          label: t('memoryTitle') || 'Memory',
          value: this.memItems.length,
          note: memLatest ? ((t('knowledgeMetricLastUpdate') || 'Last update') + ' ' + fmtTime(memLatest)) : (t('knowledgeMetricNoEntriesYet') || 'No entries yet'),
        },
        {
          label: t('knowledgeMetricSources') || 'Sources',
          value: this.srcItems.length,
          note: srcLatest ? ((t('knowledgeMetricLatestSync') || 'Latest sync') + ' ' + fmtTime(srcLatest)) : (t('knowledgeMetricNoSourcesYet') || 'No sources configured yet'),
        },
        {
          label: t('knowledgeMetricIndexedChunks') || 'Indexed Chunks',
          value: this.totalSourceChunks(),
          note: this.enabledSourceCount() + ' ' + (this.enabledSourceCount() === 1 ? (t('knowledgeMetricEnabledSource') || 'enabled source') : (t('knowledgeMetricEnabledSources') || 'enabled sources')),
        },
        {
          label: t('artifactsTitle') || 'Artifacts',
          value: fmtSize(totalArtifactBytes(this.artItems)),
          note: artLatest ? ((t('knowledgeMetricLatestArtifact') || 'Latest artifact') + ' ' + fmtTime(artLatest)) : (t('knowledgeMetricNoDeliverables') || 'No deliverables yet'),
        },
      ];
    },

    distinctMemoryNamespaces() {
      return new Set((this.memItems || []).map(item => item.namespace || 'general')).size;
    },

    totalSourceChunks() {
      return (this.srcItems || []).reduce((sum, item) => sum + Number(item && item.stats && item.stats.chunks || 0), 0);
    },

    enabledSourceCount() {
      return (this.srcItems || []).filter(item => item && item.enabled).length;
    },

    get memTotalPages() {
      return Math.max(1, Math.ceil(this.memItems.length / Number(this.memPageSize)));
    },

    get memPaginated() {
      const size = Number(this.memPageSize);
      const start = (this.memPage - 1) * size;
      return this.memItems.slice(start, start + size);
    },

    get memSelectedEntry() {
      return this.memItems.find(entry => entry.key === this.memSelectedKey) || null;
    },

    get srcSelectedItem() {
      return this.srcItems.find(item => item.id === this.srcSelectedID) || null;
    },

    get memNotebookPreview() {
      return notebookPreview(this.memNotebook && this.memNotebook.content);
    },

    memNotebookPathSummary() {
      if (this.memNotebookError) return this.memNotebookError;
      if (!this.memNotebook) return t('knowledgeNotebookUnavailable') || 'Notebook is not available yet.';
      const parts = [];
      if (this.memNotebook.path) parts.push(this.memNotebook.path);
      if (this.memNotebook.updated_at) parts.push('Updated ' + fmtTime(this.memNotebook.updated_at));
      return parts.join(' · ');
    },

    memPrevPage() {
      if (this.memPage > 1) {
        this.memPage--;
        this.memSyncSelection();
      }
    },

    memNextPage() {
      if (this.memPage < this.memTotalPages) {
        this.memPage++;
        this.memSyncSelection();
      }
    },

    memSelect(key) {
      this.memSelectedKey = key;
      this.memShowAdd = false;
      this.memEditingKey = null;
      this.memEditValue = '';
    },

    memSyncSelection() {
      if (this.memSelectedKey && this.memItems.some(entry => entry.key === this.memSelectedKey)) return;
      const first = this.memPaginated[0] || this.memItems[0] || null;
      this.memSelectedKey = first ? first.key : '';
    },

    clearMemoryFilters() {
      this.memSearch = '';
      this.memNamespace = '';
      this.memScopeKey = '';
      this.refreshMemorySurface();
    },

    memApplyNamespaceDefaults() {
      this.memNewScopeKey = memoryDefaultScopeKey(this.memNewNamespace);
    },

    toggleMemAdd() {
      this.memShowAdd = !this.memShowAdd;
      this.memEditingKey = null;
      this.memEditValue = '';
      if (this.memShowAdd) {
        this.memNewNamespace = this.memNamespace || 'profile';
        this.memNewScopeKey = memoryDefaultScopeKey(this.memNewNamespace);
        this.memNewField = '';
        this.memNewLabel = '';
        this.memNewValue = '';
      }
    },

    memCanSaveNew() {
      return Boolean(String(this.memNewNamespace || '').trim() && String(this.memNewField || '').trim() && String(this.memNewValue || '').trim());
    },

    memStartEdit(entry) {
      this.memSelectedKey = entry.key;
      this.memShowAdd = false;
      this.memEditingKey = entry.key;
      this.memEditValue = entry.value || '';
    },

    memCancelEdit() {
      this.memEditingKey = null;
      this.memEditValue = '';
    },

    async memSaveEdit(key) {
      try {
        await api.put('/runtime/memory/' + encodeURIComponent(key), { value: this.memEditValue }, { errorToast: false });
        showToast(t('memorySaved') || 'Entry updated', 'success');
        this.memEditingKey = null;
        await this.loadMemory();
        await this.loadMemoryNotebook();
        this.memSelectedKey = key;
      } catch (err) {
        showToast(err.message || (t('loadError') || 'Failed to save'), 'error');
      }
    },

    async memSaveNew() {
      try {
        const entry = await api.post('/runtime/memory/records', {
          namespace: this.memNewNamespace,
          scope_key: this.memNewScopeKey,
          field: this.memNewField,
          label: this.memNewLabel,
          value: this.memNewValue,
          source: 'console',
          confidence: 1,
        }, { errorToast: false });
        showToast(t('memorySaved') || 'Entry created', 'success');
        this.memShowAdd = false;
        await this.loadMemory();
        await this.loadMemoryNotebook();
        this.memSelectedKey = entry && entry.key ? entry.key : '';
      } catch (err) {
        showToast(err.message || (t('loadError') || 'Failed to save'), 'error');
      }
    },

    async memDelete(key) {
      if (!confirm((t('memoryConfirmDelete') || 'Delete entry "') + key + '"?')) return;
      try {
        await api.del('/runtime/memory/' + encodeURIComponent(key), { errorToast: false });
        showToast(t('memoryDeleted') || 'Entry deleted', 'success');
        if (this.memSelectedKey === key) this.memSelectedKey = '';
        this.memEditingKey = null;
        await this.loadMemory();
        await this.loadMemoryNotebook();
      } catch (err) {
        showToast(err.message || (t('loadError') || 'Failed to delete'), 'error');
      }
    },

    async loadMemory() {
      this.memError = null;
      this.memLoading = true;
      try {
        const params = new URLSearchParams();
        if (this.memSearch.trim()) params.set('q', this.memSearch.trim());
        if (this.memNamespace.trim()) params.set('namespace', this.memNamespace.trim());
        if (this.memScopeKey.trim()) params.set('scope_key', this.memScopeKey.trim());
        params.set('managed_only', 'true');
        const data = await api.get('/runtime/memory?' + params.toString());
        if (!_mounted) return;
        const items = Array.isArray(data) ? data : (data.items || []);
        this.memItems = items;
        this.memPage = 1;
        this.memSyncSelection();
      } catch (err) {
        if (_mounted) this.memError = err.message || String(err);
      }
      if (_mounted) this.memLoading = false;
    },

    async loadMemoryNotebook() {
      this.memNotebookError = null;
      try {
        const data = await api.get('/runtime/memory/notebook');
        if (_mounted) this.memNotebook = data || null;
      } catch (err) {
        if (_mounted) this.memNotebookError = err.message || String(err);
      }
    },

    sourceConfigPreview(source) {
      if (!source) return '';
      return JSON.stringify({
        kind: source.kind,
        enabled: source.enabled,
        path: source.path || '',
        urls: source.urls || [],
        config: source.config || {},
        configured_secrets: source.configured_secrets || [],
        include_globs: source.include_globs || [],
        exclude_globs: source.exclude_globs || [],
      }, null, 2);
    },

    currentSourceKindDescription(kind) {
      const value = String(kind || this.srcForm.kind || '').trim();
      const found = this.sourceKindOptions.find(item => item.value === value);
      return found ? found.description : (t('knowledgeSourceDefaultKindDesc') || 'Keep source material maintained elsewhere and let HopClaw index it.');
    },

    defaultSourceKind() {
      return (this.sourceKindOptions[0] && this.sourceKindOptions[0].value) || 'local_dir';
    },

    sourceKindDescriptor(kind) {
      return sourceKindDescriptorFor(kind || this.srcForm.kind, sourceKindCatalog) || sourceKindDescriptorFor(kind || this.srcForm.kind, this.sourceKindOptions);
    },

    currentSourceFields() {
      const descriptor = this.sourceKindDescriptor(this.srcForm.kind);
      return Array.isArray(descriptor && descriptor.fields) ? descriptor.fields : [];
    },

    sourceFieldRows(field) {
      return Number(field && field.rows) > 0 ? Number(field.rows) : 4;
    },

    sourceFieldPlaceholder(field) {
      if (!field) return '';
      if (field.secret) return this.sourceSecretPlaceholder(field.key || field.id);
      return field.placeholder || '';
    },

    readSourceFieldRaw(source, field, key) {
      if (!source || !field) return undefined;
      const scope = String(field.scope || 'config').trim();
      const fieldKey = String(key || field.key || field.id || '').trim();
      if (!fieldKey) return undefined;
      if (scope === 'root') return source[fieldKey];
      return source.config && source.config[fieldKey];
    },

    readSourceFieldList(source, field) {
      const out = [];
      const pushRaw = raw => {
        if (Array.isArray(raw)) {
          for (const item of raw) {
            const value = String(item || '').trim();
            if (value) out.push(value);
          }
          return;
        }
        const value = String(raw || '').trim();
        if (value) out.push(value);
      };
      pushRaw(this.readSourceFieldRaw(source, field));
      for (const alias of field.aliases || []) {
        pushRaw(this.readSourceFieldRaw(source, field, alias));
      }
      return out;
    },

    syncSourceFormFields() {
      const defaults = buildSourceFieldValues(this.srcForm.kind || this.defaultSourceKind(), sourceKindCatalog);
      this.srcForm.values = {
        ...defaults,
        ...(this.srcForm.values || {}),
      };
    },

    sourceSecretPlaceholder(field) {
      const configured = Array.isArray(this.srcSelectedItem && this.srcSelectedItem.configured_secrets)
        ? this.srcSelectedItem.configured_secrets
        : [];
      return this.srcEditingID && configured.includes(field) ? (t('knowledgeSecretPlaceholderConfigured') || 'Configured — leave blank to keep current value') : (t('knowledgeSecretPlaceholderEnter') || 'Enter secret value');
    },

    sourceHealthLabel(source) {
      if (!source) return t('knowledgeHealthUnknown') || 'Unknown';
      if (!source.enabled) return t('knowledgeHealthBlocked') || 'Blocked';
      if (source.last_error) return t('knowledgeHealthNeedsAttention') || 'Needs attention';
      if (!source.last_sync_at) return t('knowledgeHealthNotSyncedYet') || 'Not synced yet';
      return source.status === 'ready' ? (t('knowledgeHealthHealthy') || 'Healthy') : sourceStatusLabel(source.status);
    },

    sourceHealthCopy(source) {
      if (!source) return '';
      if (!source.enabled) return t('knowledgeHealthDisabledCopy') || 'This source is disabled, so retrieval and sync are intentionally blocked.';
      if (source.last_error) return t('knowledgeHealthErrorCopy') || 'Connector is configured but the latest sync ended with an error. Review the summary below and sync again after fixing credentials or source paths.';
      if (!source.last_sync_at) return t('knowledgeHealthNotSyncedCopy') || 'Connector is configured, but it has not been synced yet. Run the first sync to build a searchable retrieval index.';
      return t('knowledgeHealthReadyCopy') || 'Connector is configured, secrets are stored safely, and the latest sync produced a searchable source snapshot.';
    },

    sourceRecentSummary(source) {
      if (!source || !source.last_sync_at) return t('knowledgeNoCompletedSync') || 'No completed sync yet';
      const stats = source.stats || {};
      return `${stats.documents || 0} docs · ${stats.chunks || 0} chunks · ${fmtSize(stats.bytes || 0)} · ${fmtTime(source.last_sync_at)}`;
    },

    sourceRecentCopy(source) {
      if (!source || !source.last_sync_at) {
        return 'No successful sync is recorded yet. Once synced, HopClaw will show indexed documents, chunk counts, and byte volume here.';
      }
      const stats = source.stats || {};
      return `Latest successful sync captured ${stats.documents || 0} documents, ${stats.chunks || 0} chunks, and ${fmtSize(stats.bytes || 0)} from the external source.`;
    },

    sourceRetrievalSummary(source) {
      if (!source) return t('knowledgeRetrievalUnavailable') || 'Retrieval preview unavailable';
      if (!source.enabled) return t('knowledgeDisabledForRetrieval') || 'Disabled for retrieval';
      const stats = source.stats || {};
      if (!stats.chunks) return t('knowledgeSyncToEnableRetrieval') || 'Sync to enable retrieval preview';
      return `${stats.chunks} ${t('knowledgeIndexedChunksReady') || 'indexed chunks ready for retrieval'}`;
    },

    buildSourceFieldPayloadValue(field) {
      const raw = this.srcForm.values && this.srcForm.values[field.id];
      if (field.type === 'boolean') return Boolean(raw);
      if (field.type === 'string_list') return linesToList(raw);
      return String(raw || '').trim();
    },

    getSourcePayload() {
      const kind = this.srcForm.kind;
      const descriptor = this.sourceKindDescriptor(kind);
      const config = {};
      const payload = {
        name: String(this.srcForm.name || '').trim(),
        kind,
        enabled: Boolean(this.srcForm.enabled),
        path: '',
        urls: [],
        config,
        include_globs: [],
        exclude_globs: [],
      };
      for (const field of (descriptor && descriptor.fields) || []) {
        const value = this.buildSourceFieldPayloadValue(field);
        if (String(field.scope || 'config').trim() === 'root') {
          payload[field.key] = value;
        } else {
          config[field.key] = value;
        }
      }
      return {
        ...payload,
      };
    },

    sourcePayloadFieldHasValue(payload, field) {
      if (!payload || !field) return false;
      const scope = String(field.scope || 'config').trim();
      const container = scope === 'root' ? payload : (payload.config || {});
      const value = container[field.key];
      if (field.type === 'boolean') return value === true || value === false;
      if (field.type === 'string_list') return Array.isArray(value) ? value.length > 0 : Boolean(String(value || '').trim());
      return Boolean(String(value || '').trim());
    },

    sourcePayloadFieldByID(payload, descriptor, fieldID) {
      const field = ((descriptor && descriptor.fields) || []).find(item => item.id === fieldID);
      return field ? this.sourcePayloadFieldHasValue(payload, field) : false;
    },

    srcCanSave() {
      const payload = this.getSourcePayload();
      const descriptor = this.sourceKindDescriptor(payload.kind);
      if (!payload.name || !payload.kind || !descriptor) return false;
      for (const field of (descriptor.fields || [])) {
        if (field.required && !this.sourcePayloadFieldHasValue(payload, field)) return false;
      }
      for (const requirement of (descriptor.requirements || [])) {
        const options = Array.isArray(requirement.anyOf) ? requirement.anyOf : [];
        if (options.length === 0) continue;
        const satisfied = options.some(group => group.every(fieldID => this.sourcePayloadFieldByID(payload, descriptor, fieldID)));
        if (!satisfied) return false;
      }
      return true;
    },

    resetSourceForm() {
      this.srcForm = emptySourceForm(this.defaultSourceKind(), sourceKindCatalog);
    },

    srcChangeKind() {
      this.srcForm.values = buildSourceFieldValues(this.srcForm.kind || this.defaultSourceKind(), sourceKindCatalog);
    },

    openSourceCreate() {
      this.srcEditingID = '';
      this.srcShowForm = true;
      this.resetSourceForm();
    },

    openSourceEdit(source) {
      const descriptor = this.sourceKindDescriptor(source && source.kind);
      const values = buildSourceFieldValues(source && source.kind, sourceKindCatalog);
      for (const field of (descriptor && descriptor.fields) || []) {
        if (field.type === 'boolean') {
          const raw = this.readSourceFieldRaw(source, field);
          values[field.id] = raw == null ? sourceFieldDefaultValue(field) : Boolean(raw);
          continue;
        }
        if (field.type === 'string_list') {
          const rawItems = this.readSourceFieldList(source, field);
          values[field.id] = rawItems.length ? listToLines(rawItems) : sourceFieldDefaultValue(field);
          continue;
        }
        const raw = this.readSourceFieldRaw(source, field);
        values[field.id] = raw == null ? sourceFieldDefaultValue(field) : String(raw || '');
      }
      this.srcSelectedID = source.id;
      this.srcEditingID = source.id;
      this.srcShowForm = true;
      this.srcForm = {
        ...emptySourceForm(source.kind || this.defaultSourceKind(), sourceKindCatalog),
        name: source.name || '',
        kind: source.kind || 'local_dir',
        enabled: Boolean(source.enabled),
        values,
      };
    },

    closeSourceForm() {
      this.srcShowForm = false;
      this.srcEditingID = '';
      this.resetSourceForm();
    },

    srcSelect(id) {
      this.srcSelectedID = id;
      this.srcShowForm = false;
      this.srcEditingID = '';
    },

    srcApplyFilters() {
      const q = String(this.srcSearch || '').trim().toLowerCase();
      this.srcFiltered = this.srcItems.filter(item => {
        if (this.srcKindFilter && item.kind !== this.srcKindFilter) return false;
        if (this.srcStatusFilter && item.status !== this.srcStatusFilter) return false;
        if (!q) return true;
        return [
          item.name,
          item.id,
          item.kind,
          item.path,
          ...(item.urls || []),
        ].filter(Boolean).join(' ').toLowerCase().includes(q);
      });
      if (!this.srcSelectedID || !this.srcFiltered.some(item => item.id === this.srcSelectedID)) {
        this.srcSelectedID = (this.srcFiltered[0] && this.srcFiltered[0].id) || '';
      }
    },

    clearSourceFilters() {
      this.srcSearch = '';
      this.srcKindFilter = '';
      this.srcStatusFilter = '';
      this.srcApplyFilters();
    },

    async loadSources() {
      this.srcError = null;
      this.srcLoading = true;
      try {
        const data = await api.get('/operator/knowledge/sources');
        if (!_mounted) return;
        const items = Array.isArray(data) ? data : (data.items || []);
        sourceKindCatalog = normalizeSourceKindOptions(data && data.supported_kinds);
        this.sourceKindOptions = normalizeSourceKindOptions(sourceKindCatalog);
        this.syncSourceFormFields();
        this.srcItems = items.sort((a, b) => String(a.name || '').localeCompare(String(b.name || '')));
        this.srcApplyFilters();
      } catch (err) {
        if (_mounted) this.srcError = err.message || String(err);
      }
      if (_mounted) this.srcLoading = false;
    },

    async saveSource(syncAfter) {
      const payload = this.getSourcePayload();
      try {
        let response;
        if (this.srcEditingID) {
          response = await api.patch('/operator/knowledge/sources/' + encodeURIComponent(this.srcEditingID), payload, { errorToast: false });
        } else {
          response = await api.post('/operator/knowledge/sources', payload, { errorToast: false });
        }
        const item = response && (response.item || response);
        showToast(this.srcEditingID ? (t('knowledgeSourceUpdated') || 'Source updated') : (t('knowledgeSourceCreated') || 'Source created'), 'success');
        this.srcShowForm = false;
        this.srcEditingID = '';
        await this.loadSources();
        const id = item && item.id ? item.id : this.srcSelectedID;
        if (id) this.srcSelectedID = id;
        if (syncAfter && id) await this.syncSource({ id });
      } catch (err) {
        showToast(err.message || (t('knowledgeSourceSaveFailed') || 'Failed to save source'), 'error');
      }
    },

    async syncSource(source) {
      if (!source || !source.id) return;
      try {
        await api.post('/operator/knowledge/sources/' + encodeURIComponent(source.id) + '/sync', {}, { errorToast: false });
        showToast(t('knowledgeSourceSynced') || 'Source synced', 'success');
        await this.loadSources();
        this.srcSelectedID = source.id;
        if (this.srcSearchQuery.trim()) await this.runSourceSearch();
      } catch (err) {
        showToast(err.message || (t('knowledgeSourceSyncFailed') || 'Failed to sync source'), 'error');
      }
    },

    async deleteSource(source) {
      if (!source || !source.id) return;
      if (!confirm((t('knowledgeDeleteSourceConfirm') || 'Delete source') + ' "' + (source.name || source.id) + '"?')) return;
      try {
        await api.del('/operator/knowledge/sources/' + encodeURIComponent(source.id), { errorToast: false });
        showToast(t('knowledgeSourceDeleted') || 'Source deleted', 'success');
        if (this.srcSelectedID === source.id) this.srcSelectedID = '';
        this.clearSourceSearch();
        await this.loadSources();
      } catch (err) {
        showToast(err.message || (t('knowledgeSourceDeleteFailed') || 'Failed to delete source'), 'error');
      }
    },

    clearSourceSearch() {
      this.srcSearchQuery = '';
      this.srcSearchResults = [];
      this.srcSearchError = null;
    },

    async runSourceSearch() {
      if (!this.srcSelectedItem || !String(this.srcSearchQuery || '').trim()) {
        this.srcSearchResults = [];
        this.srcSearchError = null;
        return;
      }
      this.srcSearchLoading = true;
      this.srcSearchError = null;
      try {
        const params = new URLSearchParams();
        params.set('q', String(this.srcSearchQuery || '').trim());
        params.set('source_id', this.srcSelectedItem.id);
        params.set('limit', '8');
        const data = await api.get('/operator/knowledge/search?' + params.toString());
        if (_mounted) this.srcSearchResults = Array.isArray(data) ? data : (data.items || []);
      } catch (err) {
        if (_mounted) this.srcSearchError = err.message || String(err);
      }
      if (_mounted) this.srcSearchLoading = false;
    },

    get artTotalPages() {
      return Math.max(1, Math.ceil(this.artFiltered.length / Number(this.artPageSize)));
    },

    get artPaginated() {
      const size = Number(this.artPageSize);
      const start = (this.artPage - 1) * size;
      return this.artFiltered.slice(start, start + size);
    },

    get artSelectedItem() {
      return this.artFiltered.find(item => item.id === this.artSelectedID) || this.artItems.find(item => item.id === this.artSelectedID) || null;
    },

    artPrevPage() {
      if (this.artPage > 1) {
        this.artPage--;
        this.artSyncSelection();
      }
    },

    artNextPage() {
      if (this.artPage < this.artTotalPages) {
        this.artPage++;
        this.artSyncSelection();
      }
    },

    artSelect(id) {
      this.artSelectedID = id;
    },

    artSyncSelection() {
      if (this.artSelectedID && this.artFiltered.some(item => item.id === this.artSelectedID)) return;
      const first = this.artPaginated[0] || this.artFiltered[0] || null;
      this.artSelectedID = first ? first.id : '';
    },

    clearArtifactFilters() {
      this.artSearchName = '';
      this.artFilterKind = '';
      this.artFilterSession = '';
      this.artApplyFilters();
    },

    artApplyFilters() {
      const nameQ = this.artSearchName.toLowerCase();
      this.artFiltered = this.artItems.filter(item => {
        if (this.artFilterKind && item.kind !== this.artFilterKind) return false;
        if (this.artFilterSession && !(item.session_id || '').includes(this.artFilterSession)) return false;
        if (nameQ && !(item.name || '').toLowerCase().includes(nameQ)) return false;
        return true;
      });
      this.artPage = 1;
      this.artSyncSelection();
    },

    artPreview(item) {
      if (!openSafeWindow(this.artifactPreviewHref(item))) {
        showToast(t('loadError') || 'Failed to open preview', 'error');
      }
    },

    async loadArtifacts() {
      this.artError = null;
      this.artLoading = true;
      try {
        const params = new URLSearchParams();
        if (this.artFilterKind) params.set('kind', this.artFilterKind);
        if (this.artFilterSession) params.set('session_id', this.artFilterSession);
        const qs = params.toString();
        const url = '/operator/artifacts' + (qs ? '?' + qs : '');
        const data = await api.get(url);
        if (!_mounted) return;
        const items = Array.isArray(data) ? data : (data.items || []);
        this.artItems = items.sort((a, b) => new Date(b.created_at || 0) - new Date(a.created_at || 0));
        this.artApplyFilters();
      } catch (err) {
        if (_mounted) this.artError = err.message || String(err);
      }
      if (_mounted) this.artLoading = false;
    },

    mounted() {
      _mounted = true;
      this.refreshMemorySurface();
      this.loadSources();
      this.loadArtifacts();
    },

    unmounted() {
      _mounted = false;
    },
  };
}
