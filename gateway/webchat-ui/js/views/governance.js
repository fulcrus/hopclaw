// ---------------------------------------------------------------------------
// Governance View - governance operations, control-plane inventory, audit
// ---------------------------------------------------------------------------

import { api, showToast } from '../api.js';
import { t } from '../i18n/index.js';

const AUTO_REFRESH_MS = 15000;
const DEFAULT_DELIVERY_LIMIT = 100;
const DEFAULT_EVENT_LIMIT = 20;
const DEFAULT_AUDIT_LIMIT = 50;
const GOVERNANCE_ALERT_TEMPLATE_ID = 'governance-dead-letter-webhook';
const GOVERNANCE_SUBTABS = [
  { id: 'operations', labelKey: 'governanceTabOperations', fallback: 'Operations' },
  { id: 'controlplane', labelKey: 'governanceTabControlPlane', fallback: 'Control Plane' },
  { id: 'audit', labelKey: 'governanceTabAudit', fallback: 'Audit' },
];
const GOVERNANCE_HOOK_TEMPLATE_SHORTCUTS = [
  { id: 'governance-dead-letter-webhook', labelKey: 'governanceTemplateDeadLetter', fallback: 'Dead-letter webhook' },
  { id: 'governance-retry-escalation-hook', labelKey: 'governanceTemplateRetry', fallback: 'Retry escalation' },
  { id: 'governance-redrive-audit-webhook', labelKey: 'governanceTemplateRedriveAudit', fallback: 'Redrive audit' },
  { id: 'slack-governance-dead-letter-command', labelKey: 'governanceTemplateSlack', fallback: 'Slack alert' },
  { id: 'feishu-governance-dead-letter-command', labelKey: 'governanceTemplateFeishu', fallback: 'Feishu alert' },
  { id: 'email-governance-dead-letter-command', labelKey: 'governanceTemplateEmail', fallback: 'Email alert' },
  { id: 'ticket-governance-dead-letter-command', labelKey: 'governanceTemplateTicket', fallback: 'Incident ticket' },
  { id: 'approval-resolved-callback-webhook', labelKey: 'governanceTemplateApprovalCallback', fallback: 'Approval callback' },
];

function defaultAuditFilters() {
  return {
    family: '',
    severity: '',
    type: '',
    adapter_name: '',
    run_id: '',
    session_id: '',
  };
}

function fmtTime(iso) {
  try {
    return new Date(iso).toLocaleString([], {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  } catch (_) {
    return '';
  }
}

function fmtAgo(iso) {
  const ts = new Date(iso).getTime();
  if (!ts) return '-';
  const diffMs = Date.now() - ts;
  const diffSec = Math.round(diffMs / 1000);
  if (Math.abs(diffSec) < 60) return diffSec + 's';
  const diffMin = Math.round(diffSec / 60);
  if (Math.abs(diffMin) < 60) return diffMin + 'm';
  const diffHour = Math.round(diffMin / 60);
  if (Math.abs(diffHour) < 24) return diffHour + 'h';
  const diffDay = Math.round(diffHour / 24);
  return diffDay + 'd';
}

function formatDuration(value) {
  const num = Number(value);
  if (!Number.isFinite(num) || num <= 0) return '-';
  const ms = num / 1000000;
  if (ms < 1000) return Math.round(ms) + 'ms';
  const sec = ms / 1000;
  if (sec < 60) return Math.round(sec) + 's';
  const min = sec / 60;
  if (min < 60) return Math.round(min) + 'm';
  const hour = min / 60;
  if (hour < 24) return Math.round(hour) + 'h';
  return Math.round(hour / 24) + 'd';
}

function shortID(value) {
  const text = String(value || '').trim();
  if (!text) return '-';
  if (text.length <= 12) return text;
  return text.slice(0, 12);
}

function titleCase(value) {
  return String(value || '')
    .replace(/_/g, ' ')
    .replace(/\b\w/g, ch => ch.toUpperCase());
}

function extractItems(data) {
  if (!data) return [];
  if (Array.isArray(data)) return data;
  return Array.isArray(data.items) ? data.items : [];
}

function stringAttr(event, key) {
  if (!event || !event.attrs) return '';
  const value = event.attrs[key];
  return typeof value === 'string' ? value : '';
}

function joined(items) {
  return (items || []).filter(Boolean).join(', ') || '-';
}

function deliveryStatusBadgeClass(status) {
  const value = String(status || '').toLowerCase();
  if (value === 'delivered') return 'hc-badge-green';
  if (value === 'dead_letter') return 'hc-badge-red';
  if (value === 'pending') return 'hc-badge-orange';
  return 'hc-badge-gray';
}

function healthStatusBadgeClass(status) {
  const value = String(status || '').toLowerCase();
  if (value === 'ok') return 'hc-badge-green';
  if (value === 'warn') return 'hc-badge-orange';
  if (value === 'critical') return 'hc-badge-red';
  return 'hc-badge-gray';
}

function healthStatusDotClass(status) {
  const value = String(status || '').toLowerCase();
  if (value === 'ok') return 'ok';
  if (value === 'warn') return 'warn';
  return 'err';
}

function eventTypeBadgeClass(type) {
  const value = String(type || '').toLowerCase();
  if (value.includes('dead_letter') || value.includes('failed')) return 'hc-badge-red';
  if (value.includes('retry') || value.includes('queued')) return 'hc-badge-orange';
  if (value.includes('delivered') || value.includes('completed') || value.includes('redriven')) return 'hc-badge-green';
  return 'hc-badge-blue';
}

function healthStatusText(status) {
  const value = String(status || '').toLowerCase();
  if (value === 'ok') return t('governanceHealthOk') || 'Healthy';
  if (value === 'warn') return t('governanceHealthWarn') || 'Needs review';
  if (value === 'critical') return t('governanceHealthCritical') || 'Critical';
  return titleCase(value || 'unknown');
}

function buildDeliveryFilter(filters, limit) {
  const out = {};
  if (filters.status) out.status = filters.status;
  if (filters.adapter_name) out.adapter_name = filters.adapter_name.trim();
  if (filters.q) out.q = filters.q.trim();
  if (limit) out.limit = limit;
  return out;
}

function queryString(params) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params || {})) {
    if (value === undefined || value === null || value === '') continue;
    query.set(key, String(value));
  }
  const encoded = query.toString();
  return encoded ? '?' + encoded : '';
}

function serviceUnavailableStatus(status) {
  return status === 404 || status === 501 || status === 503;
}

function unavailableMessage(err) {
  if (!err) return '';
  const payload = err.payload || null;
  const text = String(
    (payload && (payload.error || payload.message)) ||
    err.message ||
    err.body ||
    ''
  ).trim();
  return text || (t('governanceControllerUnavailable') || 'governance delivery controller is not configured');
}

function defaultUnavailableMessage() {
  return t('governanceControllerUnavailable') || 'governance delivery controller is not configured';
}

function governanceOperationsAvailability(status, adapters, options = {}) {
  const snapshot = status && status.effective_config;
  if (snapshot && Array.isArray(snapshot.governance_adapter_names)) {
    return snapshot.governance_adapter_names.filter(Boolean).length > 0;
  }

  if (options.adaptersLoaded) {
    return (Array.isArray(adapters) ? adapters : []).some(item => item && item.registered && item.enabled !== false);
  }

  const governance = status && status.governance;
  if (governance && typeof governance.adapter_count === 'number' && governance.adapter_count === 0) {
    return false;
  }

  return null;
}

function emptyHealth(summary) {
  return {
    status: 'unknown',
    summary: summary || '-',
    dead_letter_count: 0,
    redrivable_count: 0,
    stale_pending_count: 0,
    oldest_pending_at: '',
    pending_count: 0,
    delivered_count: 0,
    adapters_impacted: [],
  };
}

function governanceTemplateRoute(templateID) {
  return '#/automation/hooks?template=' + encodeURIComponent(String(templateID || '').trim());
}

function auditFamily(event) {
  const explicit = stringAttr(event, 'family');
  if (explicit) return explicit;
  const type = String((event && event.type) || '').trim();
  if (type.includes('.')) return type.split('.')[0];
  if (event && event.governance) return 'governance';
  if (auditApprovalID(event) !== '-') return 'approval';
  return 'security';
}

function severityBadgeClass(value) {
  const severity = String(value || '').toLowerCase();
  if (severity === 'critical' || severity === 'high') return 'hc-badge-red';
  if (severity === 'medium' || severity === 'warn') return 'hc-badge-orange';
  if (severity === 'low' || severity === 'info') return 'hc-badge-blue';
  return 'hc-badge-gray';
}

function boolBadgeClass(value) {
  return value ? 'hc-badge-green' : 'hc-badge-gray';
}

function registrationBadgeClass(value) {
  return value ? 'hc-badge-green' : 'hc-badge-orange';
}

function readinessBadgeClass(value) {
  return value ? 'hc-badge-green' : 'hc-badge-red';
}

function mapSummaryText(value) {
  if (!value || typeof value !== 'object') return '-';
  const entries = Object.entries(value);
  if (entries.length === 0) return '-';
  return entries.map(([key, count]) => key + ' ' + count).join(' · ');
}

function inlineValue(value) {
  if (value === null || value === undefined || value === '') return '';
  if (Array.isArray(value)) return value.filter(Boolean).join(', ');
  if (typeof value === 'object') {
    try {
      return JSON.stringify(value);
    } catch (_) {
      return '[object]';
    }
  }
  return String(value);
}

function metadataText(metadata) {
  if (!metadata || typeof metadata !== 'object') return '-';
  const entries = Object.entries(metadata)
    .map(([key, value]) => [key, inlineValue(value)])
    .filter(([, value]) => value);
  if (entries.length === 0) return '-';
  return entries.slice(0, 4).map(([key, value]) => key + ': ' + value).join(' · ');
}

function scopeText(value) {
  if (!value || typeof value !== 'object') return '-';
  return joined([value.automation_id]) || '-';
}

function eventScopeText(event) {
  if (!event) return '-';
  if (event.governance && event.governance.scope) return scopeText(event.governance.scope);
  if (event.attrs && event.attrs.scope && typeof event.attrs.scope === 'object') return scopeText(event.attrs.scope);
  return '-';
}

function auditSummary(event) {
  if (!event) return '-';
  return String(
    event.summary ||
      stringAttr(event, 'summary') ||
      stringAttr(event, 'policy_summary') ||
      stringAttr(event, 'error') ||
      ''
  ).trim() || '-';
}

function auditApprovalID(event) {
  if (!event) return '-';
  if (event.governance && event.governance.approval && event.governance.approval.id) {
    return event.governance.approval.id;
  }
  return stringAttr(event, 'approval_id') || '-';
}

function auditAdapterName(event) {
  return stringAttr(event, 'adapter_name') || '-';
}

function callbackAuthText(item) {
  if (!item || !item.callback_auth || !item.callback_auth.protected) return '-';
  const auth = item.callback_auth;
  return joined([
    auth.mode ? titleCase(auth.mode) : '',
    auth.header_name || '',
    auth.signature_header || '',
    formatDuration(auth.max_age),
  ]);
}

function providerFeaturesText(item) {
  if (!item) return '-';
  const flags = [];
  if (item.submit_enabled) flags.push('submit');
  if (item.update_enabled) flags.push('update');
  if (item.sync_enabled) flags.push('sync');
  return flags.length ? flags.join(' · ') : '-';
}

function kindsText(item) {
  return joined(item && item.kinds);
}

function stringifyJSON(value) {
  if (value === null || value === undefined || value === '') return '';
  try {
    return JSON.stringify(value, null, 2);
  } catch (_) {
    return String(value);
  }
}

function countIf(items, predicate) {
  return (items || []).filter(predicate).length;
}

function capabilitySummary(snapshot) {
  if (!snapshot || !snapshot.capabilities) return '-';
  const capability = snapshot.capabilities;
  const items = [];
  if (capability.builtins_enabled) items.push('builtins');
  if (capability.local_exec_enabled) items.push('local exec');
  if (capability.search_enabled) items.push('search');
  if (capability.email_enabled) items.push('email');
  if (capability.calendar_enabled) items.push('calendar');
  if (capability.watch_enabled) items.push('watch');
  if (capability.cron_enabled) items.push('cron');
  if (capability.browser_enabled) items.push('browser');
  if (capability.desktop_enabled) items.push('desktop');
  return items.length ? items.join(' · ') : '-';
}

function layerSummary(snapshot) {
  if (!snapshot || !Array.isArray(snapshot.layers) || snapshot.layers.length === 0) return '-';
  return snapshot.layers.map(layer => joined([layer.name, layer.kind, layer.source])).join(' · ');
}

function approvalPolicySummary(snapshot) {
  if (!snapshot || !snapshot.approval) return '-';
  const approval = snapshot.approval;
  return joined([
    approval.exec_mode,
    approval.skill_install_policy,
    approval.default_grant_scope ? 'default ' + approval.default_grant_scope : '',
    approval.max_grant_scope ? 'max ' + approval.max_grant_scope : '',
    approval.require_approval_for_write ? 'write approval' : '',
    approval.deny_destructive ? 'deny destructive' : '',
  ]);
}

export function GovernanceView() {
  let _mounted = false;
  let refreshTimer = null;

  const $template = `
    <div class="hc-governance">
      <div class="hc-governance-toolbar">
        <div>
          <h2 class="hc-page-title">{{ t('governanceTitle') || 'Governance' }}</h2>
          <p class="hc-governance-subtitle">{{ pageSubtitle }}</p>
        </div>
        <div class="hc-governance-toolbar-actions">
          <span v-if="refreshing" class="hc-badge hc-badge-blue">{{ t('loading') }}</span>
          <button class="hc-btn hc-btn-secondary" @click="refresh()" :disabled="refreshing">{{ t('refresh') }}</button>
          <button v-if="activeTab === 'operations'" class="hc-btn hc-btn-secondary" @click="openHookTemplate(defaultAlertTemplateID)">
            {{ t('governanceCreateAlertHook') || 'Create alert hook' }}
          </button>
          <button
            v-if="activeTab === 'operations'"
            class="hc-btn hc-btn-danger"
            data-testid="governance-redrive-visible"
            @click="redriveVisible()"
            :disabled="refreshing || visibleRedrivableCount === 0">
            {{ t('governanceRedriveVisible') || 'Redrive visible' }}
          </button>
        </div>
      </div>

      <div class="hc-tabs">
        <button
          v-for="tab in subtabs"
          :key="tab.id"
          :data-testid="'governance-subtab-' + tab.id"
          class="hc-tab"
          :class="{ active: activeTab === tab.id }"
          @click="setTab(tab.id)">
          {{ t(tab.labelKey) || tab.fallback }}
          <span v-if="tabBadge(tab.id)" class="hc-tab-badge">{{ tabBadge(tab.id) }}</span>
        </button>
      </div>

      <div v-if="loading" class="hc-loading">{{ t('loading') }}</div>

      <div v-else-if="activeTab === 'operations'" class="hc-governance-tab">
        <div class="hc-card" style="margin-bottom:16px">
          <div class="hc-detail-panel-head">
            <div class="hc-detail-panel-copy">
              <div class="hc-detail-panel-title">{{ t('governanceRoutingTitle') || 'Alert routing' }}</div>
              <div class="hc-detail-panel-subtitle">{{ t('governanceRoutingDesc') || 'Start from governance and hook templates.' }}</div>
            </div>
            <div class="hc-governance-toolbar-actions">
              <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="openAutomationHooks()">
                {{ t('governanceOpenAutomationHooks') || 'Open hook library' }}
              </button>
            </div>
          </div>
          <div style="display:flex;flex-wrap:wrap;gap:8px">
            <button
              v-for="shortcut in governanceTemplateShortcuts"
              :key="shortcut.id"
              class="hc-btn hc-btn-sm hc-btn-secondary"
              @click="openHookTemplate(shortcut.id)">
              {{ t(shortcut.labelKey) || shortcut.fallback }}
            </button>
          </div>
        </div>

        <div v-if="operationsError" class="hc-card" data-testid="governance-operations-error" :data-unavailable="operationsUnavailable ? 'true' : 'false'">
          <div class="hc-governance-error">{{ operationsError }}</div>
        </div>

        <div v-if="operationsLoading && !hasOperationsData" class="hc-loading">{{ t('loading') }}</div>
        <div v-else>
          <div class="hc-governance-metrics">
            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceHealth') || 'Delivery health' }}</div>
              <div class="hc-monitor-card-value hc-monitor-card-inline">
                <span class="hc-status-dot" :class="healthStatusDotClass(health && health.status)"></span>
                {{ healthStatusText(health && health.status) }}
              </div>
              <div class="hc-monitor-card-sub">{{ (health && health.summary) || '-' }}</div>
            </div>

            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceDeadLetters') || 'Dead letters' }}</div>
              <div class="hc-monitor-card-value">{{ health ? health.dead_letter_count : 0 }}</div>
              <div class="hc-monitor-card-sub">{{ t('governanceRedrivable') || 'Redrivable' }} · {{ health ? health.redrivable_count : 0 }}</div>
            </div>

            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceStale') || 'Stale pending' }}</div>
              <div class="hc-monitor-card-value">{{ health ? health.stale_pending_count : 0 }}</div>
              <div class="hc-monitor-card-sub">
                <span v-if="health && health.oldest_pending_at">{{ fmtAgo(health.oldest_pending_at) }} · {{ fmtTime(health.oldest_pending_at) }}</span>
                <span v-else>-</span>
              </div>
            </div>

            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceDeliveries') || 'Deliveries' }}</div>
              <div class="hc-monitor-card-value">{{ stats ? stats.total : deliveries.length }}</div>
              <div class="hc-monitor-card-sub">{{ t('governancePendingSuffix') || 'Pending' }} · {{ health ? health.pending_count : 0 }} · {{ t('governanceDeliveredCountSuffix') || 'Delivered' }} · {{ health ? health.delivered_count : 0 }}</div>
            </div>

            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceImpactedAdapters') || 'Impacted adapters' }}</div>
              <div class="hc-monitor-card-value">{{ health && health.adapters_impacted ? health.adapters_impacted.length : 0 }}</div>
              <div class="hc-monitor-card-sub">{{ joined(health && health.adapters_impacted) }}</div>
            </div>
          </div>

          <div class="hc-card">
            <div class="hc-detail-panel-head">
              <div class="hc-detail-panel-copy">
                <div class="hc-detail-panel-title">{{ t('governanceFilters') || 'Filters' }}</div>
                <div class="hc-detail-panel-subtitle">{{ t('governanceSummary') || 'Summary' }} · {{ deliveries.length }} {{ t('governanceVisible') || 'visible' }} · {{ visibleRedrivableCount }} {{ t('governanceRedrivableSuffix') || 'redrivable' }}</div>
              </div>
            </div>

            <div class="hc-governance-filterbar">
              <select class="hc-form-select" v-model="filters.status">
                <option value="">{{ t('allItems') || 'All' }}</option>
                <option value="pending">{{ t('governancePending') || 'Pending' }}</option>
                <option value="delivered">{{ t('governanceDelivered') || 'Delivered' }}</option>
                <option value="dead_letter">{{ t('governanceDeadLetter') || 'Dead Letter' }}</option>
              </select>
              <input class="hc-form-input" v-model="filters.adapter_name" :placeholder="(t('name') || 'Name') + ' / adapter'" />
              <input class="hc-form-input" v-model="filters.q" :placeholder="t('searchPlaceholder') || 'Search...'" />
              <button class="hc-btn hc-btn-secondary" @click="applyFilters()" :disabled="refreshing">{{ t('filter') }}</button>
              <button class="hc-btn hc-btn-ghost" @click="resetFilters()" :disabled="refreshing">{{ t('reset') }}</button>
            </div>

            <div class="hc-governance-chiprow">
              <div class="hc-governance-card-meta">
                <span class="hc-badge" :class="healthStatusBadgeClass(health && health.status)">{{ healthStatusText(health && health.status) }}</span>
                <span class="hc-badge hc-badge-gray">{{ selectedCount }} {{ t('selected') || 'selected' }}</span>
              </div>
              <div class="hc-governance-toolbar-actions">
                <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="selectVisible()" :disabled="visibleRedrivableCount === 0">{{ t('governanceSelectVisible') || 'Select visible' }}</button>
                <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="clearSelection()" :disabled="selectedCount === 0">{{ t('governanceClearSelection') || 'Clear selection' }}</button>
                <button class="hc-btn hc-btn-sm hc-btn-primary" data-testid="governance-redrive-selected" @click="redriveSelected()" :disabled="refreshing || selectedRedrivableCount === 0">{{ t('governanceRedriveSelected') || 'Redrive selected' }}</button>
              </div>
            </div>
          </div>

          <div class="hc-governance-layout">
            <div class="hc-list-panel">
              <div class="hc-card">
                <div class="hc-detail-panel-head">
                  <div class="hc-detail-panel-copy">
                    <div class="hc-detail-panel-title">{{ t('governanceDeliveries') || 'Deliveries' }}</div>
                    <div class="hc-detail-panel-subtitle">{{ t('governanceDeliveryQueue') || 'Delivery queue, retries, and operator recovery.' }}</div>
                  </div>
                </div>

                <div v-if="deliveries.length === 0" class="hc-empty-compact">{{ t('governanceNoDeliveries') || 'No governance deliveries match the current filters.' }}</div>
                <div v-else class="hc-list-card-grid">
                  <div
                    v-for="item in deliveries"
                    :key="item.id"
                    :data-testid="'governance-delivery-' + item.id"
                    class="hc-list-card"
                    :class="{ active: activeDeliveryID === item.id }"
                    @click="selectDelivery(item)">
                    <div class="hc-list-card-head">
                      <div>
                        <div class="hc-governance-list-title">
                          <label class="hc-governance-checkbox" v-if="item.can_redrive" @click.stop>
                            <input type="checkbox" :checked="isSelected(item.id)" @change="toggleSelected(item.id)" />
                          </label>
                          <strong>{{ item.record.summary || shortID(item.record.event_id) }}</strong>
                          <span class="hc-badge" :class="deliveryStatusBadgeClass(item.status)">{{ titleCase(item.status) }}</span>
                          <span class="hc-badge hc-badge-gray">{{ item.adapter_name || '-' }}</span>
                        </div>
                        <div class="hc-list-card-subtitle">{{ titleCase(item.record.kind) }} · {{ item.record.event_type || '-' }}</div>
                      </div>
                      <button
                        v-if="item.can_redrive"
                        class="hc-btn hc-btn-sm hc-btn-secondary"
                        :data-testid="'governance-redrive-one-' + item.id"
                        @click.stop="redriveOne(item)"
                        :disabled="refreshing">
                        {{ t('retry') || 'Retry' }}
                      </button>
                    </div>

                    <div class="hc-list-card-content">
                      <div class="hc-governance-card-meta">
                        <span>{{ t('governanceRunSuffix') || 'Run' }} · {{ shortID(item.record.run_id) }}</span>
                        <span>{{ t('governanceSessionSuffix') || 'Session' }} · {{ shortID(item.record.session_id) }}</span>
                        <span>{{ t('governanceAttemptsSuffix') || 'Attempts' }} · {{ item.attempts || 0 }}/{{ item.max_attempts || 0 }}</span>
                      </div>
                      <div v-if="item.last_error" class="hc-governance-error">{{ item.last_error }}</div>
                    </div>

                    <div class="hc-list-card-footer">
                      <span class="hc-list-card-meta">{{ t('governanceUpdatedAt') || 'Updated' }} · {{ fmtTime(item.updated_at) }}</span>
                      <span class="hc-list-card-meta" v-if="item.next_attempt_at">{{ t('governanceNextSuffix') || 'Next' }} · {{ fmtAgo(item.next_attempt_at) }}</span>
                      <span class="hc-list-card-meta" v-else-if="item.delivered_at">{{ t('governanceDeliveredSuffix') || 'Delivered' }} · {{ fmtAgo(item.delivered_at) }}</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <div class="hc-governance-detail-stack">
              <div class="hc-card hc-detail-panel">
                <div v-if="!activeDelivery" class="hc-state-block hc-state-block-inline">
                  <div class="hc-state-block-title">{{ t('governanceDetailEmpty') || 'Choose a delivery to inspect its full governance record.' }}</div>
                </div>

                <div v-else>
                  <div class="hc-detail-panel-head">
                    <div class="hc-detail-panel-copy">
                      <div class="hc-detail-panel-title">{{ activeDelivery.record.summary || activeDelivery.id }}</div>
                      <div class="hc-detail-panel-subtitle">{{ activeDelivery.adapter_name }} · {{ activeDelivery.record.event_type || '-' }}</div>
                    </div>
                    <div class="hc-governance-toolbar-actions">
                      <span class="hc-badge" :class="deliveryStatusBadgeClass(activeDelivery.status)">{{ titleCase(activeDelivery.status) }}</span>
                      <button
                        v-if="activeDelivery.can_redrive"
                        class="hc-btn hc-btn-sm hc-btn-primary"
                        :data-testid="'governance-redrive-one-' + activeDelivery.id"
                        @click="redriveOne(activeDelivery)"
                        :disabled="refreshing">
                        {{ t('retry') || 'Retry' }}
                      </button>
                    </div>
                  </div>

                  <div class="hc-detail-grid">
                    <div class="hc-detail-stat">
                      <div class="hc-detail-stat-label">{{ t('governanceDeliveryId') || 'Delivery ID' }}</div>
                      <div class="hc-detail-stat-value">{{ activeDelivery.id }}</div>
                    </div>
                    <div class="hc-detail-stat">
                      <div class="hc-detail-stat-label">{{ t('governanceAttempts') || 'Attempts' }}</div>
                      <div class="hc-detail-stat-value">{{ activeDelivery.attempts || 0 }}/{{ activeDelivery.max_attempts || 0 }}</div>
                    </div>
                    <div class="hc-detail-stat">
                      <div class="hc-detail-stat-label">{{ t('governanceRun') || 'Run' }}</div>
                      <div class="hc-detail-stat-value">{{ activeDelivery.record.run_id || '-' }}</div>
                    </div>
                    <div class="hc-detail-stat">
                      <div class="hc-detail-stat-label">{{ t('governanceSession') || 'Session' }}</div>
                      <div class="hc-detail-stat-value">{{ activeDelivery.record.session_id || '-' }}</div>
                    </div>
                    <div class="hc-detail-stat">
                      <div class="hc-detail-stat-label">{{ t('governanceSeverity') || 'Severity' }}</div>
                      <div class="hc-detail-stat-value">{{ activeDelivery.record.severity || '-' }}</div>
                    </div>
                    <div class="hc-detail-stat">
                      <div class="hc-detail-stat-label">{{ t('governanceSecurityCategory') || 'Security Category' }}</div>
                      <div class="hc-detail-stat-value">{{ activeDelivery.record.security_category || '-' }}</div>
                    </div>
                    <div class="hc-detail-stat">
                      <div class="hc-detail-stat-label">{{ t('governanceNextAttempt') || 'Next Attempt' }}</div>
                      <div class="hc-detail-stat-value">{{ activeDelivery.next_attempt_at ? fmtTime(activeDelivery.next_attempt_at) : '-' }}</div>
                    </div>
                    <div class="hc-detail-stat">
                      <div class="hc-detail-stat-label">{{ t('governanceDeliveredAt') || 'Delivered At' }}</div>
                      <div class="hc-detail-stat-value">{{ activeDelivery.delivered_at ? fmtTime(activeDelivery.delivered_at) : '-' }}</div>
                    </div>
                  </div>

                  <div class="hc-governance-summary-list">
                    <div class="hc-detail-stat">
                      <div class="hc-detail-stat-label">{{ t('governanceTools') || 'Tools' }}</div>
                      <div class="hc-detail-stat-value">{{ joined(activeDelivery.record.tool_names) }}</div>
                    </div>
                    <div class="hc-detail-stat">
                      <div class="hc-detail-stat-label">{{ t('scope') || 'Scope' }}</div>
                      <div class="hc-detail-stat-value">{{ activeScopeText(activeDelivery) }}</div>
                    </div>
                    <div class="hc-detail-stat">
                      <div class="hc-detail-stat-label">{{ t('governanceConfigSnapshot') || 'Config Snapshot' }}</div>
                      <div class="hc-detail-stat-value">{{ activeDelivery.record.effective_config_snapshot_id || '-' }}</div>
                    </div>
                  </div>

                  <pre v-if="activeDelivery.last_error" class="hc-detail-pre">{{ activeDelivery.last_error }}</pre>
                </div>
              </div>

              <div class="hc-card hc-governance-events">
                <div class="hc-detail-panel-head">
                  <div class="hc-detail-panel-copy">
                    <div class="hc-detail-panel-title">{{ t('governanceEvents') || 'Events' }}</div>
                    <div class="hc-detail-panel-subtitle">{{ t('governanceEventsSubtitle') || 'Recent governance lifecycle events from the runtime bus.' }}</div>
                  </div>
                </div>

                <div v-if="events.length === 0" class="hc-empty-compact">{{ t('governanceNoEvents') || 'No governance events yet.' }}</div>
                <div v-else class="hc-table-wrap">
                  <table class="hc-table">
                    <thead>
                      <tr>
                        <th>{{ t('status') }}</th>
                        <th>{{ t('type') }}</th>
                        <th>{{ t('name') }}</th>
                        <th>{{ t('timing') }}</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="event in events" :key="event.id">
                        <td><span class="hc-badge" :class="eventTypeBadgeClass(event.type)">{{ titleCase(stringAttr(event, 'delivery_status') || event.type) }}</span></td>
                        <td>{{ event.type }}</td>
                        <td>{{ stringAttr(event, 'adapter_name') || '-' }}</td>
                        <td>{{ fmtTime(event.time) }}</td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div v-else-if="activeTab === 'controlplane'" class="hc-governance-tab">
        <div v-if="controlPlaneError" class="hc-card">
          <div class="hc-governance-error">{{ controlPlaneError }}</div>
        </div>

        <div v-if="controlPlaneLoading && !hasControlPlaneData" class="hc-loading">{{ t('loading') }}</div>
        <div v-else>
          <div class="hc-governance-metrics hc-governance-metrics-wide">
            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceControlPlane') || 'Control plane' }}</div>
              <div class="hc-monitor-card-value">
                <span class="hc-badge" :class="readinessBadgeClass(controlPlaneStatus && controlPlaneStatus.ok)">{{ controlPlaneReadinessText }}</span>
              </div>
              <div class="hc-monitor-card-sub">{{ controlPlaneIssueCount }} {{ t('governanceIssues') || 'issues' }}</div>
            </div>

            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('navSecurity') || 'Security' }}</div>
              <div class="hc-monitor-card-value">{{ controlPlaneStatus && controlPlaneStatus.auth && controlPlaneStatus.auth.configured ? (t('yes') || 'Yes') : (t('no') || 'No') }}</div>
              <div class="hc-monitor-card-sub">{{ controlPlaneStatus && controlPlaneStatus.auth && controlPlaneStatus.auth.ready ? (t('governanceReady') || 'Ready') : (t('governanceNeedsAttention') || 'Needs attention') }}</div>
            </div>

            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceAuthZTitle') || 'AuthZ' }}</div>
              <div class="hc-monitor-card-value">{{ authzKindLabel }}</div>
              <div class="hc-monitor-card-sub">{{ authzBindingCount }} {{ t('governanceBindingCount') || 'bindings' }}</div>
            </div>

            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceApprovalProviders') || 'Approval providers' }}</div>
              <div class="hc-monitor-card-value">{{ approvalProviders.length }}</div>
              <div class="hc-monitor-card-sub">{{ registeredApprovalProviders }} {{ t('governanceRegistered') || 'registered' }}</div>
            </div>

            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceAdaptersTitle') || 'Governance adapters' }}</div>
              <div class="hc-monitor-card-value">{{ governanceAdapters.length }}</div>
              <div class="hc-monitor-card-sub">{{ registeredGovernanceAdapters }} {{ t('governanceRegistered') || 'registered' }}</div>
            </div>

            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceAuditSinks') || 'Audit sinks' }}</div>
              <div class="hc-monitor-card-value">{{ auditSinks.length }}</div>
              <div class="hc-monitor-card-sub">{{ credentials && credentials.count ? credentials.count : 0 }} {{ t('governanceCredentials') || 'credentials' }}</div>
            </div>
          </div>

          <div class="hc-governance-control-grid">
            <div class="hc-card">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('governanceReadiness') || 'Readiness' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('governanceControlPlaneDesc') || 'Inspect auth, AuthZ, policy, approvals, governance adapters, audit sinks, and snapshot health.' }}</div>
                </div>
                <span class="hc-badge" :class="readinessBadgeClass(controlPlaneStatus && controlPlaneStatus.ok)">{{ controlPlaneReadinessText }}</span>
              </div>

              <div v-if="controlPlaneIssueCount === 0" class="hc-state-block">
                <div class="hc-state-block-title">{{ t('governanceNoIssues') || 'No control-plane issues detected.' }}</div>
                <div class="hc-state-block-copy">{{ t('governanceControlPlaneDesc') || 'Inspect auth, AuthZ, policy, approvals, governance adapters, audit sinks, and snapshot health.' }}</div>
              </div>
              <ul v-else class="hc-governance-issue-list">
                <li v-for="issue in controlPlaneIssues" :key="issue">{{ issue }}</li>
              </ul>

              <div class="hc-detail-grid">
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceAuthConfigured') || 'Auth configured' }}</div>
                  <div class="hc-detail-stat-value">{{ controlPlaneStatus && controlPlaneStatus.auth && controlPlaneStatus.auth.configured ? (t('yes') || 'Yes') : (t('no') || 'No') }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceReady') || 'Ready' }}</div>
                  <div class="hc-detail-stat-value">{{ controlPlaneStatus && controlPlaneStatus.auth && controlPlaneStatus.auth.ready ? (t('yes') || 'Yes') : (t('no') || 'No') }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceStoreAvailable') || 'Store available' }}</div>
                  <div class="hc-detail-stat-value">{{ controlPlaneStatus && controlPlaneStatus.approvals && controlPlaneStatus.approvals.store_available ? (t('yes') || 'Yes') : (t('no') || 'No') }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceCurrentDecision') || 'Current decision' }}</div>
                  <div class="hc-detail-stat-value">{{ authzDecisionLabel }}</div>
                </div>
              </div>
            </div>

            <div class="hc-card">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('governanceEffectiveConfigTitle') || 'Effective config' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('governanceEffectiveConfigDesc') || 'Runtime snapshot of model, policy, adapters, capabilities, and profile layers.' }}</div>
                </div>
              </div>

              <div v-if="!effectiveConfig" class="hc-empty-compact">{{ t('noData') || 'No data' }}</div>
              <div v-else>
                <div class="hc-detail-grid">
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceSnapshotId') || 'Snapshot ID' }}</div>
                    <div class="hc-detail-stat-value">{{ effectiveConfig.id || '-' }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('edition') || 'Edition' }}</div>
                    <div class="hc-detail-stat-value">{{ effectiveConfig.edition || '-' }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceRuntimeProfile') || 'Runtime profile' }}</div>
                    <div class="hc-detail-stat-value">{{ effectiveConfig.runtime_profile || '-' }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceDefaultModel') || 'Default model' }}</div>
                    <div class="hc-detail-stat-value">{{ effectiveConfig.default_model || '-' }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceStoreBackend') || 'Store backend' }}</div>
                    <div class="hc-detail-stat-value">{{ effectiveConfig.store_backend || '-' }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceGeneratedAt') || 'Generated at' }}</div>
                    <div class="hc-detail-stat-value">{{ fmtTime(effectiveConfig.generated_at) }}</div>
                  </div>
                </div>

                <div class="hc-governance-summary-list">
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceCapabilities') || 'Capabilities' }}</div>
                    <div class="hc-detail-stat-value">{{ capabilitySummary(effectiveConfig) }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governancePolicyPacks') || 'Policy packs' }}</div>
                    <div class="hc-detail-stat-value">{{ joined(effectiveConfig.policy_pack_ids) }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceSnapshotLayers') || 'Snapshot layers' }}</div>
                    <div class="hc-detail-stat-value">{{ layerSummary(effectiveConfig) }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceApprovalPolicy') || 'Approval policy' }}</div>
                    <div class="hc-detail-stat-value">{{ approvalPolicySummary(effectiveConfig) }}</div>
                  </div>
                </div>
              </div>
            </div>

            <div class="hc-card">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('governanceRuntimeFactsTitle') || 'Runtime facts' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('governanceRuntimeFactsDesc') || 'Deterministic runtime-facts projection used to explain managed config truth, injected env coverage, and context hashing.' }}</div>
                </div>
              </div>

              <div v-if="!runtimeFacts" class="hc-empty-compact">{{ t('noData') || 'No data' }}</div>
              <div v-else class="hc-detail-grid">
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceContextFingerprint') || 'Context fingerprint' }}</div>
                  <div class="hc-detail-stat-value">{{ runtimeFacts.context_fingerprint || '-' }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceManagedSkills') || 'Managed skills' }}</div>
                  <div class="hc-detail-stat-value">{{ runtimeFacts.managed_skill_count || 0 }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceInjectedEnv') || 'Injected env' }}</div>
                  <div class="hc-detail-stat-value">{{ runtimeFacts.managed_injected_env_count || 0 }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceConfigTruth') || 'Config truth' }}</div>
                  <div class="hc-detail-stat-value">{{ runtimeFacts.config_truth_count || 0 }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceSecretPresence') || 'Secret presence' }}</div>
                  <div class="hc-detail-stat-value">{{ runtimeFacts.resolved_secret_presence_count || 0 }}</div>
                </div>
              </div>
            </div>

            <div class="hc-card">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('governanceChildEnvPolicyTitle') || 'Child env policy' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('governanceChildEnvPolicyDesc') || 'Execution subprocesses inherit a bounded baseline plus overlays, never the full host process environment.' }}</div>
                </div>
              </div>

              <div v-if="!childEnvPolicy" class="hc-empty-compact">{{ t('noData') || 'No data' }}</div>
              <div v-else>
                <div class="hc-detail-grid">
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceOverlaySupport') || 'Overlay support' }}</div>
                    <div class="hc-detail-stat-value">{{ childEnvPolicy.overlay_supported ? (t('yes') || 'Yes') : (t('no') || 'No') }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceHostMutation') || 'Host mutation' }}</div>
                    <div class="hc-detail-stat-value">{{ childEnvPolicy.mutates_host_process ? (t('yes') || 'Yes') : (t('no') || 'No') }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceFullEnvInheritance') || 'Full env inheritance' }}</div>
                    <div class="hc-detail-stat-value">{{ childEnvPolicy.inherits_full_parent_env ? (t('yes') || 'Yes') : (t('no') || 'No') }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceModuleExecBaseline') || 'Module exec baseline' }}</div>
                    <div class="hc-detail-stat-value">{{ (childEnvPolicy.module_exec_baseline_keys && childEnvPolicy.module_exec_baseline_keys.length) || 0 }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceInstallerExecBaseline') || 'Installer exec baseline' }}</div>
                    <div class="hc-detail-stat-value">{{ (childEnvPolicy.installer_exec_baseline_keys && childEnvPolicy.installer_exec_baseline_keys.length) || 0 }}</div>
                  </div>
                </div>

                <div class="hc-governance-summary-list">
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceModuleExecBaseline') || 'Module exec baseline' }}</div>
                    <div class="hc-detail-stat-value">{{ joined(childEnvPolicy.module_exec_baseline_keys) }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceInstallerExecBaseline') || 'Installer exec baseline' }}</div>
                    <div class="hc-detail-stat-value">{{ joined(childEnvPolicy.installer_exec_baseline_keys) }}</div>
                  </div>
                </div>
              </div>
            </div>

            <div class="hc-card">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('governanceAuthZTitle') || 'AuthZ' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('governanceAuthZDesc') || 'Active authorization decider, stable resource/action catalog, and current request decision exposed by the operator gateway.' }}</div>
                </div>
              </div>

              <div class="hc-detail-grid">
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceKind') || 'Kind' }}</div>
                  <div class="hc-detail-stat-value">{{ (authzSummary && authzSummary.kind) || '-' }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceMode') || 'Mode' }}</div>
                  <div class="hc-detail-stat-value">{{ (authzSummary && authzSummary.mode) || '-' }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceDefaultEffect') || 'Default effect' }}</div>
                  <div class="hc-detail-stat-value">{{ (authzSummary && authzSummary.default_effect) || '-' }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceBindingCount') || 'Binding count' }}</div>
                  <div class="hc-detail-stat-value">{{ authzBindingCount }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceResourceCount') || 'Resource count' }}</div>
                  <div class="hc-detail-stat-value">{{ (authzSummary && authzSummary.resources && authzSummary.resources.length) || 0 }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceActionCount') || 'Action count' }}</div>
                  <div class="hc-detail-stat-value">{{ (authzSummary && authzSummary.actions && authzSummary.actions.length) || 0 }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceIdentity') || 'Identity' }}</div>
                  <div class="hc-detail-stat-value">{{ identitySummary }}</div>
                </div>
              </div>

              <div v-if="authzBindings.length === 0" class="hc-empty-compact">{{ t('noData') || 'No data' }}</div>
              <div v-else class="hc-table-wrap">
                <table class="hc-table">
                  <thead>
                    <tr>
                      <th>{{ t('name') }}</th>
                      <th>{{ t('type') }}</th>
                      <th>{{ t('description') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="binding in authzBindings" :key="binding.kind + '-' + binding.name">
                      <td>{{ binding.name }}</td>
                      <td>{{ binding.kind || '-' }}</td>
                      <td>{{ binding.description || '-' }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <div class="hc-card">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('governancePolicyTitle') || 'Policy engine' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('governancePolicyDesc') || 'Runtime policy composition, approval gates, and destructive-action guards.' }}</div>
                </div>
              </div>

              <div class="hc-detail-grid">
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governancePolicyKind') || 'Policy kind' }}</div>
                  <div class="hc-detail-stat-value">{{ policyRuntime && policyRuntime.kind ? policyRuntime.kind : '-' }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governancePolicyLayers') || 'Policy layers' }}</div>
                  <div class="hc-detail-stat-value">{{ policyLayers.length }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceRequireWriteApproval') || 'Write approval' }}</div>
                  <div class="hc-detail-stat-value">{{ policyWriteApprovalCount }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceDenyDestructive') || 'Deny destructive' }}</div>
                  <div class="hc-detail-stat-value">{{ policyDenyDestructiveCount }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceAuditWired') || 'Audit wired' }}</div>
                  <div class="hc-detail-stat-value">{{ policyAuditWiredCount }}</div>
                </div>
                <div class="hc-detail-stat">
                  <div class="hc-detail-stat-label">{{ t('governanceGrantStore') || 'Grant store' }}</div>
                  <div class="hc-detail-stat-value">{{ policyGrantStoreCount }}</div>
                </div>
              </div>

              <div v-if="policyLayers.length === 0" class="hc-empty-compact">{{ t('noData') || 'No data' }}</div>
              <div v-else class="hc-table-wrap">
                <table class="hc-table">
                  <thead>
                    <tr>
                      <th>#</th>
                      <th>{{ t('name') }}</th>
                      <th>{{ t('type') }}</th>
                      <th>{{ t('description') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="layer in policyLayers" :key="layer.layer_order + '-' + layer.name + '-' + layer.type">
                      <td>{{ layer.layer_order || '-' }}</td>
                      <td>{{ layer.name || '-' }}</td>
                      <td>{{ layer.type || '-' }}</td>
                      <td>{{ policyLayerSummary(layer) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <div class="hc-card">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('governanceApprovalProviders') || 'Approval providers' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('governanceApprovalProvidersDesc') || 'External approval connectors and callback protection primitives.' }}</div>
                </div>
              </div>

              <div v-if="approvalProviders.length === 0" class="hc-empty-compact">{{ t('noData') || 'No data' }}</div>
              <div v-else class="hc-table-wrap">
                <table class="hc-table">
                  <thead>
                    <tr>
                      <th>{{ t('name') }}</th>
                      <th>{{ t('type') }}</th>
                      <th>{{ t('enabled') || 'Enabled' }}</th>
                      <th>{{ t('governanceRegistered') || 'Registered' }}</th>
                      <th>{{ t('governanceFeatures') || 'Features' }}</th>
                      <th>{{ t('governanceCallbackAuth') || 'Callback auth' }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="item in approvalProviders" :key="item.name">
                      <td>
                        <div>{{ item.name || '-' }}</div>
                        <div class="hc-governance-card-meta">{{ metadataText(item.metadata) }}</div>
                      </td>
                      <td>{{ item.type || '-' }}</td>
                      <td><span class="hc-badge" :class="boolBadgeClass(item.enabled)">{{ item.enabled ? (t('enabled') || 'Enabled') : (t('disabled') || 'Disabled') }}</span></td>
                      <td><span class="hc-badge" :class="registrationBadgeClass(item.registered)">{{ item.registered ? (t('governanceRegistered') || 'Registered') : (t('governanceNotRegistered') || 'Not registered') }}</span></td>
                      <td>{{ providerFeaturesText(item) }}</td>
                      <td>{{ callbackAuthText(item) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <div class="hc-card">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('governanceAdaptersTitle') || 'Governance adapters' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('governanceGovernanceAdaptersDesc') || 'Adapter registry for governance delivery surfaces and snapshot projection.' }}</div>
                </div>
              </div>

              <div v-if="governanceAdapters.length === 0" class="hc-empty-compact">{{ t('noData') || 'No data' }}</div>
              <div v-else class="hc-table-wrap">
                <table class="hc-table">
                  <thead>
                    <tr>
                      <th>{{ t('name') }}</th>
                      <th>{{ t('type') }}</th>
                      <th>{{ t('enabled') || 'Enabled' }}</th>
                      <th>{{ t('governanceRegistered') || 'Registered' }}</th>
                      <th>{{ t('governanceKinds') || 'Kinds' }}</th>
                      <th>{{ t('governanceIncludeSnapshot') || 'Include snapshot' }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="item in governanceAdapters" :key="item.name">
                      <td>
                        <div>{{ item.name || '-' }}</div>
                        <div class="hc-governance-card-meta">{{ metadataText(item.metadata) }}</div>
                      </td>
                      <td>{{ item.type || '-' }}</td>
                      <td><span class="hc-badge" :class="boolBadgeClass(item.enabled)">{{ item.enabled ? (t('enabled') || 'Enabled') : (t('disabled') || 'Disabled') }}</span></td>
                      <td><span class="hc-badge" :class="registrationBadgeClass(item.registered)">{{ item.registered ? (t('governanceRegistered') || 'Registered') : (t('governanceNotRegistered') || 'Not registered') }}</span></td>
                      <td>{{ kindsText(item) }}</td>
                      <td><span class="hc-badge" :class="boolBadgeClass(item.include_snapshot)">{{ item.include_snapshot ? (t('yes') || 'Yes') : (t('no') || 'No') }}</span></td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <div class="hc-card">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('governanceAuditSinks') || 'Audit sinks' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('governanceAuditSinksDesc') || 'Audit export registry exposed as base infrastructure, without workflow semantics.' }}</div>
                </div>
              </div>

              <div v-if="auditSinks.length === 0" class="hc-empty-compact">{{ t('noData') || 'No data' }}</div>
              <div v-else class="hc-table-wrap">
                <table class="hc-table">
                  <thead>
                    <tr>
                      <th>{{ t('name') }}</th>
                      <th>{{ t('type') }}</th>
                      <th>{{ t('enabled') || 'Enabled' }}</th>
                      <th>{{ t('governanceRegistered') || 'Registered' }}</th>
                      <th>{{ t('governanceTarget') || 'Target' }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="item in auditSinks" :key="item.name">
                      <td>
                        <div>{{ item.name || '-' }}</div>
                        <div class="hc-governance-card-meta">{{ metadataText(item.metadata) }}</div>
                      </td>
                      <td>{{ item.type || '-' }}</td>
                      <td><span class="hc-badge" :class="boolBadgeClass(item.enabled)">{{ item.enabled ? (t('enabled') || 'Enabled') : (t('disabled') || 'Disabled') }}</span></td>
                      <td><span class="hc-badge" :class="registrationBadgeClass(item.registered)">{{ item.registered ? (t('governanceRegistered') || 'Registered') : (t('governanceNotRegistered') || 'Not registered') }}</span></td>
                      <td>{{ item.target || '-' }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <div class="hc-card hc-governance-span-full">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('governanceCredentials') || 'Credentials' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('governanceCredentialsDesc') || 'SecretRef inventory only: path, kind, and locator without exposing secret material.' }}</div>
                </div>
                <div class="hc-governance-card-meta">{{ mapSummaryText(credentials && credentials.by_kind) }}</div>
              </div>

              <div v-if="credentialItems.length === 0" class="hc-empty-compact">{{ t('noData') || 'No data' }}</div>
              <div v-else class="hc-table-wrap">
                <table class="hc-table">
                  <thead>
                    <tr>
                      <th>{{ t('governancePath') || 'Path' }}</th>
                      <th>{{ t('type') }}</th>
                      <th>{{ t('governanceLocator') || 'Locator' }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="item in credentialItems" :key="item.path">
                      <td>{{ item.path || '-' }}</td>
                      <td>{{ item.kind || '-' }}</td>
                      <td>{{ item.locator || '-' }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div v-else class="hc-governance-tab">
        <div v-if="auditError" class="hc-card">
          <div class="hc-governance-error">{{ auditError }}</div>
        </div>

        <div v-if="auditLoading && !hasAuditData" class="hc-loading">{{ t('loading') }}</div>
        <div v-else>
          <div class="hc-governance-metrics hc-governance-metrics-audit">
            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceAuditTitle') || 'Audit' }}</div>
              <div class="hc-monitor-card-value">{{ auditEvents.length }}</div>
              <div class="hc-monitor-card-sub">{{ t('governanceAuditDesc') || 'Unified security, approval, and governance event stream.' }}</div>
            </div>
            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceAuditSecurity') || 'Security' }}</div>
              <div class="hc-monitor-card-value">{{ auditFamilyCount('security') }}</div>
              <div class="hc-monitor-card-sub">{{ t('governanceAuditHighPlus') || 'High+' }} · {{ auditHighSeverityCount }}</div>
            </div>
            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceAuditApprovalMetric') || 'Approval' }}</div>
              <div class="hc-monitor-card-value">{{ auditFamilyCount('approval') }}</div>
              <div class="hc-monitor-card-sub">{{ t('governanceAuditRefs') || 'Refs' }} · {{ auditApprovalRefCount }}</div>
            </div>
            <div class="hc-monitor-card">
              <div class="hc-monitor-card-label">{{ t('governanceAuditGovernance') || 'Governance' }}</div>
              <div class="hc-monitor-card-value">{{ auditFamilyCount('governance') }}</div>
              <div class="hc-monitor-card-sub">{{ t('governanceAuditAdapters') || 'Adapters' }} · {{ auditAdapterRefCount }}</div>
            </div>
          </div>

          <div class="hc-card">
            <div class="hc-detail-panel-head">
              <div class="hc-detail-panel-copy">
                <div class="hc-detail-panel-title">{{ t('governanceAuditFilters') || 'Audit filters' }}</div>
                <div class="hc-detail-panel-subtitle">{{ t('governanceAuditStreamDesc') || 'Filter by family, severity, scope, adapter, run, or session without crossing into business-layer workflows.' }}</div>
              </div>
            </div>

            <div class="hc-governance-filterbar">
              <select class="hc-form-select" v-model="auditFilters.family">
                <option value="">{{ t('allItems') || 'All' }}</option>
                <option value="security">{{ t('governanceAuditSecurity') || 'Security' }}</option>
                <option value="approval">{{ t('governanceAuditApprovalMetric') || 'Approval' }}</option>
                <option value="governance">{{ t('governanceAuditGovernance') || 'Governance' }}</option>
              </select>
              <select class="hc-form-select" v-model="auditFilters.severity">
                <option value="">{{ t('allItems') || 'All' }}</option>
                <option value="critical">{{ t('governanceAuditCritical') || 'Critical' }}</option>
                <option value="high">{{ t('governanceAuditHigh') || 'High' }}</option>
                <option value="medium">{{ t('governanceAuditMedium') || 'Medium' }}</option>
                <option value="low">{{ t('governanceAuditLow') || 'Low' }}</option>
                <option value="info">{{ t('governanceAuditInfo') || 'Info' }}</option>
              </select>
              <input class="hc-form-input" v-model="auditFilters.type" :placeholder="t('type') || 'Type'" />
              <input class="hc-form-input" v-model="auditFilters.adapter_name" :placeholder="(t('name') || 'Name') + ' / adapter'" />
              <input class="hc-form-input" v-model="auditFilters.run_id" :placeholder="t('runId') || 'Run ID'" />
              <input class="hc-form-input" v-model="auditFilters.session_id" :placeholder="t('session') || 'Session'" />
              <button class="hc-btn hc-btn-secondary" @click="applyAuditFilters()" :disabled="refreshing">{{ t('filter') }}</button>
              <button class="hc-btn hc-btn-ghost" @click="resetAuditFilters()" :disabled="refreshing">{{ t('reset') }}</button>
            </div>
          </div>

          <div class="hc-governance-audit-layout">
            <div class="hc-card hc-governance-audit-table">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('governanceAuditTitle') || 'Audit' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ auditEvents.length }} {{ t('governanceAuditEventsLoaded') || 'events loaded' }}</div>
                </div>
              </div>

              <div v-if="auditEvents.length === 0" class="hc-empty-compact">{{ t('governanceNoAuditEvents') || 'No audit events match the current filters.' }}</div>
              <div v-else class="hc-table-wrap">
                <table class="hc-table">
                  <thead>
                    <tr>
                      <th>{{ t('governanceAuditFamily') || 'Family' }}</th>
                      <th>{{ t('status') }}</th>
                      <th>{{ t('type') }}</th>
                      <th>{{ t('governanceAuditSummary') || 'Summary' }}</th>
                      <th>{{ t('scope') || 'Scope' }}</th>
                      <th>{{ t('timing') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr
                      v-for="event in auditEvents"
                      :key="event.id"
                      class="hc-governance-clickable-row"
                      :class="{ 'hc-governance-row-active': activeAuditEventID === event.id }"
                      @click="selectAuditEvent(event)">
                      <td><span class="hc-badge hc-badge-gray">{{ titleCase(auditFamily(event)) }}</span></td>
                      <td><span class="hc-badge" :class="severityBadgeClass(event.severity)">{{ event.severity || '-' }}</span></td>
                      <td>{{ event.type || '-' }}</td>
                      <td>{{ auditSummary(event) }}</td>
                      <td>{{ eventScopeText(event) }}</td>
                      <td>{{ fmtTime(event.time) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <div class="hc-card hc-detail-panel">
              <div v-if="!activeAuditEvent" class="hc-state-block hc-state-block-inline">
                <div class="hc-state-block-title">{{ t('governanceAuditDetailEmpty') || 'Choose an audit event to inspect its projected control-plane record.' }}</div>
              </div>

              <div v-else>
                <div class="hc-detail-panel-head">
                  <div class="hc-detail-panel-copy">
                    <div class="hc-detail-panel-title">{{ auditSummary(activeAuditEvent) }}</div>
                    <div class="hc-detail-panel-subtitle">{{ activeAuditEvent.type || '-' }} · {{ fmtTime(activeAuditEvent.time) }}</div>
                  </div>
                  <div class="hc-governance-toolbar-actions">
                    <span class="hc-badge" :class="severityBadgeClass(activeAuditEvent.severity)">{{ activeAuditEvent.severity || '-' }}</span>
                    <span class="hc-badge hc-badge-gray">{{ titleCase(auditFamily(activeAuditEvent)) }}</span>
                  </div>
                </div>

                <div class="hc-detail-grid">
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceAuditEventId') || 'Event ID' }}</div>
                    <div class="hc-detail-stat-value">{{ activeAuditEvent.id || '-' }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceAuditFamily') || 'Family' }}</div>
                    <div class="hc-detail-stat-value">{{ auditFamily(activeAuditEvent) }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('runId') || 'Run ID' }}</div>
                    <div class="hc-detail-stat-value">{{ activeAuditEvent.run_id || '-' }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('session') || 'Session' }}</div>
                    <div class="hc-detail-stat-value">{{ activeAuditEvent.session_id || '-' }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceAuditAdapter') || 'Adapter' }}</div>
                    <div class="hc-detail-stat-value">{{ auditAdapterName(activeAuditEvent) }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceAuditApproval') || 'Approval' }}</div>
                    <div class="hc-detail-stat-value">{{ auditApprovalID(activeAuditEvent) }}</div>
                  </div>
                </div>

                <div class="hc-governance-summary-list">
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('scope') || 'Scope' }}</div>
                    <div class="hc-detail-stat-value">{{ eventScopeText(activeAuditEvent) }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceTools') || 'Tools' }}</div>
                    <div class="hc-detail-stat-value">{{ joined(activeAuditEvent.governance && activeAuditEvent.governance.tool_names) }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('governanceConfigSnapshot') || 'Config Snapshot' }}</div>
                    <div class="hc-detail-stat-value">{{ activeAuditEvent.governance && activeAuditEvent.governance.effective_config_snapshot_id ? activeAuditEvent.governance.effective_config_snapshot_id : '-' }}</div>
                  </div>
                </div>

                <pre v-if="activeAuditEvent.attrs" class="hc-detail-pre">{{ stringifyJSON(activeAuditEvent.attrs) }}</pre>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  `;

  return {
    $template,
    t,
    fmtTime,
    fmtAgo,
    shortID,
    titleCase,
    joined,
    deliveryStatusBadgeClass,
    healthStatusBadgeClass,
    healthStatusDotClass,
    eventTypeBadgeClass,
    healthStatusText,
    readinessBadgeClass,
    severityBadgeClass,
    boolBadgeClass,
    registrationBadgeClass,
    auditFamily,
    eventScopeText,
    auditSummary,
    auditApprovalID,
    auditAdapterName,
    stringifyJSON,
    providerFeaturesText,
    callbackAuthText,
    kindsText,
    metadataText,
    mapSummaryText,
    capabilitySummary,
    layerSummary,
    approvalPolicySummary,
    countIf,
    subtabs: GOVERNANCE_SUBTABS,
    governanceTemplateShortcuts: GOVERNANCE_HOOK_TEMPLATE_SHORTCUTS,
    defaultAlertTemplateID: GOVERNANCE_ALERT_TEMPLATE_ID,

    loading: true,
    busy: false,
    activeTab: 'operations',

    operationsLoading: false,
    operationsError: null,
    operationsUnavailable: false,
    operationsUnavailableMessage: '',
    governanceOperationsAvailable: null,
    health: null,
    stats: null,
    deliveries: [],
    events: [],
    activeDeliveryID: '',
    deliveryDetail: null,
    selectedIDs: [],
    filters: {
      status: '',
      adapter_name: '',
      q: '',
    },

    controlPlaneLoading: false,
    controlPlaneError: null,
    controlPlaneStatus: null,
    approvalProviders: [],
    governanceAdapters: [],
    auditSinks: [],
    credentials: { items: [], count: 0, by_kind: null },
    policyRuntime: null,
    authzState: { summary: { bindings: [], resources: [], actions: [] }, decision: null, identity: null },

    auditLoading: false,
    auditError: null,
    auditEvents: [],
    activeAuditEventID: '',
    auditFilters: defaultAuditFilters(),
    auditNextCursor: '',
    auditCursorStatus: '',

    get refreshing() {
      return this.busy || this.operationsLoading || this.controlPlaneLoading || this.auditLoading;
    },

    get pageSubtitle() {
      if (this.activeTab === 'controlplane') {
        if (this.controlPlaneIssueCount > 0) {
          return this.controlPlaneIssueCount + ' ' + (t('governanceIssues') || 'issues') + ' · ' + (this.controlPlaneIssues[0] || '');
        }
        return t('governanceControlPlaneDesc') || 'Inspect auth, AuthZ, policy, approvals, governance adapters, audit sinks, and snapshot health.';
      }
      if (this.activeTab === 'audit') {
        return t('governanceAuditDesc') || 'Unified security, approval, and governance event stream.';
      }
      if (this.operationsUnavailableMessage) {
        return this.operationsUnavailableMessage;
      }
      return (this.health && this.health.summary) || (t('governanceDesc') || 'Operate governance delivery and recovery from one place.');
    },

    get hasOperationsData() {
      return !!(this.health || this.stats || this.deliveries.length || this.events.length);
    },

    get hasControlPlaneData() {
      return !!(this.controlPlaneStatus || this.approvalProviders.length || this.governanceAdapters.length || this.auditSinks.length || this.credentialItems.length);
    },

    get hasAuditData() {
      return this.auditEvents.length > 0;
    },

    get selectedCount() {
      return this.selectedIDs.length;
    },

    get selectedRedrivableCount() {
      return this.deliveries.filter(item => item.can_redrive && this.selectedIDs.includes(item.id)).length;
    },

    get visibleRedrivableCount() {
      return this.deliveries.filter(item => item.can_redrive).length;
    },

    get activeDelivery() {
      if (this.deliveryDetail && this.deliveryDetail.id === this.activeDeliveryID) return this.deliveryDetail;
      return this.deliveries.find(item => item.id === this.activeDeliveryID) || null;
    },

    get controlPlaneIssues() {
      return (this.controlPlaneStatus && this.controlPlaneStatus.issues) || [];
    },

    get controlPlaneIssueCount() {
      return this.controlPlaneIssues.length;
    },

    get controlPlaneReadinessText() {
      return this.controlPlaneStatus && this.controlPlaneStatus.ok ? (t('governanceReady') || 'Ready') : (t('governanceNeedsAttention') || 'Needs attention');
    },

    get effectiveConfig() {
      return this.controlPlaneStatus && this.controlPlaneStatus.effective_config ? this.controlPlaneStatus.effective_config : null;
    },

    get runtimeFacts() {
      return this.controlPlaneStatus && this.controlPlaneStatus.runtime_facts ? this.controlPlaneStatus.runtime_facts : null;
    },

    get childEnvPolicy() {
      return this.controlPlaneStatus && this.controlPlaneStatus.child_env_policy ? this.controlPlaneStatus.child_env_policy : null;
    },

    get credentialItems() {
      return extractItems(this.credentials);
    },

    get authzSummary() {
      return (this.authzState && this.authzState.summary) || { bindings: [], resources: [], actions: [] };
    },

    get authzDecision() {
      return this.authzState && this.authzState.decision ? this.authzState.decision : null;
    },

    get authzDecisionLabel() {
      const decision = this.authzDecision;
      if (!decision) return '-';
      const label = decision.allowed ? (t('governanceAllowed') || 'Allowed') : (t('governanceDenied') || 'Denied');
      const role = decision.metadata && decision.metadata.resolved_role ? decision.metadata.resolved_role : '';
      return joined([label, role, decision.source || '']);
    },

    get identitySummary() {
      const identity = this.authzState && this.authzState.identity;
      if (!identity) return '-';
      return joined([
        identity.subject,
        identity.provider,
        Array.isArray(identity.scopes) && identity.scopes.length ? identity.scopes.join('/') : '',
      ]);
    },

    get authzBindings() {
      return (this.authzSummary && this.authzSummary.bindings) || [];
    },

    get authzBindingCount() {
      return this.authzBindings.length;
    },

    get authzKindLabel() {
      return joined([
        this.authzSummary && this.authzSummary.kind ? this.authzSummary.kind : '',
        this.authzSummary && this.authzSummary.mode ? this.authzSummary.mode : '',
      ]) || '-';
    },

    get policyLayers() {
      return (this.policyRuntime && this.policyRuntime.layers) || [];
    },

    get policyWriteApprovalCount() {
      return countIf(this.policyLayers, layer => layer.require_approval_for_write);
    },

    get policyDenyDestructiveCount() {
      return countIf(this.policyLayers, layer => layer.deny_destructive);
    },

    get policyAuditWiredCount() {
      return countIf(this.policyLayers, layer => layer.security_audit_wired);
    },

    get policyGrantStoreCount() {
      return countIf(this.policyLayers, layer => layer.grant_store_wired);
    },

    get registeredApprovalProviders() {
      return countIf(this.approvalProviders, item => item.registered);
    },

    get registeredGovernanceAdapters() {
      return countIf(this.governanceAdapters, item => item.registered);
    },

    get auditHighSeverityCount() {
      return countIf(this.auditEvents, item => ['high', 'critical'].includes(String(item.severity || '').toLowerCase()));
    },

    get auditApprovalRefCount() {
      return countIf(this.auditEvents, item => auditApprovalID(item) !== '-');
    },

    get auditAdapterRefCount() {
      return countIf(this.auditEvents, item => auditAdapterName(item) !== '-');
    },

    get activeAuditEvent() {
      return this.auditEvents.find(item => item.id === this.activeAuditEventID) || null;
    },

    activeScopeText(item) {
      if (!item || !item.record || !item.record.scope) return '-';
      return scopeText(item.record.scope);
    },

    isSelected(id) {
      return this.selectedIDs.includes(id);
    },

    toggleSelected(id) {
      if (this.selectedIDs.includes(id)) {
        this.selectedIDs = this.selectedIDs.filter(item => item !== id);
        return;
      }
      this.selectedIDs = [...this.selectedIDs, id];
    },

    clearSelection() {
      this.selectedIDs = [];
    },

    selectVisible() {
      this.selectedIDs = this.deliveries.filter(item => item.can_redrive).map(item => item.id);
    },

    setTab(tabID) {
      this.activeTab = tabID;
      if (tabID === 'controlplane' && !this.hasControlPlaneData && !this.controlPlaneLoading) {
        this.loadControlPlane();
      }
      if (tabID === 'audit' && !this.hasAuditData && !this.auditLoading) {
        this.loadAudit();
      }
    },

    tabBadge(tabID) {
      if (tabID === 'operations') {
        const count = this.health && this.health.dead_letter_count ? this.health.dead_letter_count : 0;
        return count ? String(count) : '';
      }
      if (tabID === 'controlplane') {
        return this.controlPlaneIssueCount ? String(this.controlPlaneIssueCount) : '';
      }
      if (tabID === 'audit') {
        return this.auditEvents.length ? String(this.auditEvents.length) : '';
      }
      return '';
    },

    buildDeliveryQuery(limit = DEFAULT_DELIVERY_LIMIT) {
      return queryString(buildDeliveryFilter(this.filters, limit));
    },

    buildEventQuery() {
      const params = { limit: DEFAULT_EVENT_LIMIT };
      if (this.filters.adapter_name) params.adapter_name = this.filters.adapter_name.trim();
      if (this.filters.status) params.delivery_status = this.filters.status;
      return queryString(params);
    },

    buildAuditQuery() {
      const params = { limit: DEFAULT_AUDIT_LIMIT };
      for (const [key, value] of Object.entries(this.auditFilters || {})) {
        if (!value) continue;
        params[key] = value.trim ? value.trim() : value;
      }
      return queryString(params);
    },

    openAutomationHooks() {
      window.location.hash = '#/automation/hooks';
    },

    openHookTemplate(templateID) {
      const id = String(templateID || '').trim();
      if (!id) return;
      window.location.hash = governanceTemplateRoute(id);
    },

    policyLayerSummary(layer) {
      if (!layer) return '-';
      const items = [];
      if (layer.require_approval_for_write) items.push('write approval');
      if (layer.require_approval_community) items.push('community approval');
      if (layer.deny_destructive) items.push('deny destructive');
      if (layer.security_audit_wired) items.push('audit wired');
      if (layer.grant_store_wired) items.push('grant store');
      if (layer.skill_install_policy) items.push('skill ' + layer.skill_install_policy);
      return items.length ? items.join(' · ') : '-';
    },

    auditFamilyCount(name) {
      return countIf(this.auditEvents, item => auditFamily(item) === name);
    },

    async loadOperations(options = {}) {
      const forceCheck = options && options.forceCheck === true;
      const controllerUnavailable =
        this.governanceOperationsAvailable === false ||
        (this.operationsUnavailable && !forceCheck);

      this.operationsLoading = true;
      this.operationsError = null;
      try {
        if (controllerUnavailable) {
          const message = this.operationsUnavailableMessage || defaultUnavailableMessage();
          let eventsError = null;
          try {
            const events = await api.get('/operator/governance/events' + this.buildEventQuery(), { background: true });
            if (_mounted) {
              this.events = extractItems(events);
            }
          } catch (err) {
            eventsError = unavailableMessage(err);
            if (_mounted) {
              this.events = [];
            }
          }
          if (!_mounted) return;
          this.operationsUnavailable = true;
          this.operationsUnavailableMessage = message;
          this.operationsError = eventsError ? [message, eventsError].filter(Boolean).join(' · ') : message;
          this.health = emptyHealth(message);
          this.stats = { total: 0 };
          this.deliveries = [];
          this.selectedIDs = [];
          this.activeDeliveryID = '';
          this.deliveryDetail = null;
          return;
        }

        const requests = [
          ['health', api.get('/operator/governance/health' + this.buildDeliveryQuery(), { background: true })],
          ['stats', api.get('/operator/governance/deliveries/stats' + this.buildDeliveryQuery(), { background: true })],
          ['deliveries', api.get('/operator/governance/deliveries' + this.buildDeliveryQuery(), { background: true })],
          ['events', api.get('/operator/governance/events' + this.buildEventQuery(), { background: true })],
        ];
        const settled = await Promise.allSettled(requests.map(([, promise]) => promise));
        if (!_mounted) return;

        const payloads = {};
        const failures = {};
        requests.forEach(([name], index) => {
          const result = settled[index];
          if (result.status === 'fulfilled') {
            payloads[name] = result.value;
          } else {
            failures[name] = result.reason;
          }
        });

        const deliveryFailures = ['health', 'stats', 'deliveries']
          .map(name => failures[name])
          .filter(Boolean);
        const deliveryUnavailable =
          deliveryFailures.length === 3 &&
          deliveryFailures.every(err => serviceUnavailableStatus(Number(err && err.status)));

        if (deliveryUnavailable) {
          this.operationsUnavailable = true;
          this.operationsUnavailableMessage = unavailableMessage(deliveryFailures[0]);
          this.operationsError = this.operationsUnavailableMessage;
          this.health = emptyHealth(this.operationsUnavailableMessage);
          this.stats = { total: 0 };
          this.deliveries = [];
          this.events = extractItems(payloads.events);
          this.selectedIDs = [];
          this.activeDeliveryID = '';
          this.deliveryDetail = null;
          return;
        }

        this.operationsUnavailable = false;
        this.operationsUnavailableMessage = '';
        this.health = payloads.health || null;
        this.stats = payloads.stats || null;
        this.deliveries = extractItems(payloads.deliveries);
        this.events = extractItems(payloads.events);
        this.selectedIDs = this.selectedIDs.filter(id => this.deliveries.some(item => item.id === id && item.can_redrive));
        if (this.activeDeliveryID) {
          const match = this.deliveries.find(item => item.id === this.activeDeliveryID);
          if (match) {
            this.deliveryDetail = match;
          } else {
            this.activeDeliveryID = '';
            this.deliveryDetail = null;
          }
        }
        if (!this.activeDeliveryID && this.deliveries.length > 0) {
          this.activeDeliveryID = this.deliveries[0].id;
          this.deliveryDetail = this.deliveries[0];
        }
        const errors = [];
        if (failures.health) errors.push(unavailableMessage(failures.health));
        if (failures.stats) errors.push(unavailableMessage(failures.stats));
        if (failures.deliveries) errors.push(unavailableMessage(failures.deliveries));
        if (failures.events) errors.push(unavailableMessage(failures.events));
        this.operationsError = errors.length ? Array.from(new Set(errors)).join(' · ') : null;
      } catch (err) {
        if (_mounted) {
          this.operationsError = err.message || String(err);
        }
      } finally {
        if (_mounted) {
          this.operationsLoading = false;
        }
      }
    },

    async loadControlPlane() {
      this.controlPlaneLoading = true;
      this.controlPlaneError = null;

      const requests = [
        ['status', api.get('/operator/controlplane/status')],
        ['approvals', api.get('/operator/approvals/providers')],
        ['adapters', api.get('/operator/governance/adapters')],
        ['sinks', api.get('/operator/audit/sinks')],
        ['policy', api.get('/operator/policy/engines')],
        ['credentials', api.get('/operator/config/credentials')],
        ['authz', api.get('/operator/authz')],
      ];

      try {
        const settled = await Promise.allSettled(requests.map(([, promise]) => promise));
        if (!_mounted) return;

        const payloads = {};
        const failures = {};
        requests.forEach(([name], index) => {
          const result = settled[index];
          if (result.status === 'fulfilled') {
            payloads[name] = result.value;
          } else {
            failures[name] = result.reason;
          }
        });

        this.controlPlaneStatus = payloads.status || this.controlPlaneStatus || null;
        const status = this.controlPlaneStatus || {};

        this.approvalProviders =
          extractItems(payloads.approvals).length > 0
            ? extractItems(payloads.approvals)
            : ((status.approvals && status.approvals.providers) || []);
        this.governanceAdapters =
          extractItems(payloads.adapters).length > 0
            ? extractItems(payloads.adapters)
            : ((status.governance && status.governance.adapters) || []);
        this.governanceOperationsAvailable = governanceOperationsAvailability(
          status,
          this.governanceAdapters,
          {
            adaptersLoaded:
              Object.prototype.hasOwnProperty.call(payloads, 'adapters') ||
              !!(status.governance && Array.isArray(status.governance.adapters)),
          },
        );
        this.auditSinks =
          extractItems(payloads.sinks).length > 0
            ? extractItems(payloads.sinks)
            : ((status.audit && status.audit.sinks) || []);
        this.credentials = payloads.credentials || status.credentials || { items: [], count: 0, by_kind: null };
        this.policyRuntime = (payloads.policy && payloads.policy.policy) || status.policy || null;
        this.authzState = payloads.authz || { summary: status.authz || { bindings: [], resources: [], actions: [] }, decision: null, identity: null };

        const errors = [];
        if (!payloads.status) errors.push((failures.status && failures.status.message) || 'control-plane status unavailable');
        if (!payloads.authz && !(status && status.authz)) errors.push((failures.authz && failures.authz.message) || 'authz summary unavailable');
        if (!payloads.policy && !(status && status.policy)) errors.push((failures.policy && failures.policy.message) || 'policy summary unavailable');
        if (!payloads.credentials && !(status && status.credentials)) errors.push((failures.credentials && failures.credentials.message) || 'credential inventory unavailable');
        this.controlPlaneError = errors.length ? errors.join(' · ') : null;
      } finally {
        if (_mounted) {
          this.controlPlaneLoading = false;
        }
      }
    },

    async loadAudit() {
      this.auditLoading = true;
      this.auditError = null;
      try {
        const result = await api.get('/operator/audit/events' + this.buildAuditQuery());
        if (!_mounted) return;
        this.auditEvents = extractItems(result);
        this.auditNextCursor = (result && result.next_cursor) || '';
        this.auditCursorStatus = (result && result.cursor_status) || '';
        if (this.activeAuditEventID) {
          const match = this.auditEvents.find(item => item.id === this.activeAuditEventID);
          if (!match) this.activeAuditEventID = '';
        }
        if (!this.activeAuditEventID && this.auditEvents.length > 0) {
          this.activeAuditEventID = this.auditEvents[0].id;
        }
      } catch (err) {
        if (_mounted) {
          this.auditError = err.message || String(err);
        }
      } finally {
        if (_mounted) {
          this.auditLoading = false;
        }
      }
    },

    async loadAll(options = {}) {
      const forceOperations = options && options.forceOperations === true;
      await Promise.all([this.loadControlPlane(), this.loadAudit()]);
      await this.loadOperations({ forceCheck: forceOperations });
      if (_mounted) {
        this.loading = false;
        this.busy = false;
      }
    },

    async refresh() {
      this.busy = true;
      await this.loadAll({ forceOperations: true });
    },

    async applyFilters() {
      this.selectedIDs = [];
      await this.loadOperations({ forceCheck: true });
    },

    async resetFilters() {
      this.filters = { status: '', adapter_name: '', q: '' };
      this.selectedIDs = [];
      await this.loadOperations({ forceCheck: true });
    },

    async applyAuditFilters() {
      await this.loadAudit();
    },

    async resetAuditFilters() {
      this.auditFilters = defaultAuditFilters();
      await this.loadAudit();
    },

    selectAuditEvent(item) {
      this.activeAuditEventID = item && item.id ? item.id : '';
    },

    async selectDelivery(item) {
      this.activeDeliveryID = item.id;
      this.deliveryDetail = item;
      try {
        const detail = await api.get('/operator/governance/deliveries/' + encodeURIComponent(item.id));
        if (!_mounted || this.activeDeliveryID !== item.id) return;
        this.deliveryDetail = detail || item;
      } catch (_) {
        this.deliveryDetail = item;
      }
    },

    async redriveOne(item) {
      this.busy = true;
      try {
        const result = await api.post('/operator/governance/deliveries/' + encodeURIComponent(item.id) + '/redrive', {}, { errorToast: false });
        const updated = result && typeof result.updated === 'number' ? result.updated : 1;
        showToast('Redriven ' + updated + ' delivery', 'success');
        await Promise.all([this.loadOperations(), this.loadAudit()]);
      } catch (err) {
        showToast((err && err.message) || 'Failed to redrive delivery', 'error');
      } finally {
        if (_mounted) this.busy = false;
      }
    },

    async redriveSelected() {
      if (this.selectedRedrivableCount === 0) return;
      this.busy = true;
      try {
        const result = await api.post('/operator/governance/deliveries/redrive', { ids: this.selectedIDs }, { errorToast: false });
        const updated = result && typeof result.updated === 'number' ? result.updated : this.selectedRedrivableCount;
        showToast('Redriven ' + updated + ' delivery', 'success');
        this.selectedIDs = [];
        await Promise.all([this.loadOperations(), this.loadAudit()]);
      } catch (err) {
        showToast((err && err.message) || 'Failed to redrive selected deliveries', 'error');
      } finally {
        if (_mounted) this.busy = false;
      }
    },

    async redriveVisible() {
      if (this.visibleRedrivableCount === 0) return;
      this.busy = true;
      try {
        const result = await api.post('/operator/governance/deliveries/redrive', {
          filter: buildDeliveryFilter(this.filters, this.deliveries.length || DEFAULT_DELIVERY_LIMIT),
        }, { errorToast: false });
        const updated = result && typeof result.updated === 'number' ? result.updated : this.visibleRedrivableCount;
        showToast('Redriven ' + updated + ' delivery', 'success');
        this.selectedIDs = [];
        await Promise.all([this.loadOperations(), this.loadAudit()]);
      } catch (err) {
        showToast((err && err.message) || 'Failed to redrive visible deliveries', 'error');
      } finally {
        if (_mounted) this.busy = false;
      }
    },

    mounted() {
      _mounted = true;
      this.loadAll({ forceOperations: true });
      refreshTimer = setInterval(() => {
        if (_mounted && !this.refreshing) this.loadAll();
      }, AUTO_REFRESH_MS);
    },

    unmounted() {
      _mounted = false;
      if (refreshTimer) {
        clearInterval(refreshTimer);
        refreshTimer = null;
      }
    },
  };
}
