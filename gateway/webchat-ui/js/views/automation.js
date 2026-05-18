// ---------------------------------------------------------------------------
// Automation View - unified tabbed page for Agents, Schedules, Watch, Hooks
// ---------------------------------------------------------------------------

import { api, showToast } from '../api.js';
import { t } from '../i18n/index.js';
import { artifactPreviewPath, safeExternalURL } from '../linking.js';
import { buildAutomationSchedulesSection } from './automation/schedules.js';
import { buildAutomationStarterTemplatesSection } from './automation/starter-templates.js';
import { buildAutomationWatchSection } from './automation/watch.js';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const DEFAULT_PAGE_SIZE = 20;
const AUTO_REFRESH_MS = 15000;
const SYSTEM_PROMPT_PREVIEW_LEN = 500;
const RESPONSE_BODY_TRUNCATE_LEN = 300;
const PREVIEW_TEXT_LIMIT = 240;

const RESULT_ACTION_LABELS = {
  review_result: () => t('automationActionReviewResult') || 'Review result',
  review_approval: () => t('automationActionReviewApproval') || 'Review approval',
  provide_input: () => t('automationActionProvideInput') || 'Provide input',
  retry_run: () => t('automationActionRetryRun') || 'Retry run',
  open_deliverables: () => t('automationActionOpenDeliverables') || 'Open deliverables',
  inspect_verification: () => t('automationActionInspectVerification') || 'Inspect verification',
};

const FALLBACK_HOOK_EVENT_SPECS = [
  { trigger: 'run.completed', category: 'run', allowed_phases: ['post'], can_block: false, supports_async: true },
  { trigger: 'run.failed', category: 'run', allowed_phases: ['error'], can_block: false, supports_async: true },
  { trigger: 'tool.executed', category: 'tool', allowed_phases: ['pre', 'post', 'error'], can_block: true, supports_async: true },
  { trigger: 'session.created', category: 'session', allowed_phases: ['post'], can_block: false, supports_async: true },
  { trigger: 'approval.requested', category: 'approval', allowed_phases: ['post'], can_block: false, supports_async: true },
  { trigger: 'approval.resolved', category: 'approval', allowed_phases: ['post'], can_block: false, supports_async: true },
  { trigger: 'approval.timed_out', category: 'approval', allowed_phases: ['post'], can_block: false, supports_async: true },
  { trigger: 'approval.grace_warning', category: 'approval', allowed_phases: ['post'], can_block: false, supports_async: true },
  { trigger: 'governance.delivery.queued', category: 'governance', allowed_phases: ['post'], can_block: false, supports_async: true },
  { trigger: 'governance.delivery.retry_scheduled', category: 'governance', allowed_phases: ['post'], can_block: false, supports_async: true },
  { trigger: 'governance.delivery.delivered', category: 'governance', allowed_phases: ['post'], can_block: false, supports_async: true },
  { trigger: 'governance.delivery.dead_lettered', category: 'governance', allowed_phases: ['post'], can_block: false, supports_async: true },
  { trigger: 'governance.delivery.redriven', category: 'governance', allowed_phases: ['post'], can_block: false, supports_async: true },
];

const HOOK_PHASE_OPTIONS = [
  { value: 'pre', label: 'Pre' },
  { value: 'post', label: 'Post' },
  { value: 'error', label: 'Error' },
];

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

function fmtTime(iso) {
  if (!iso) return '-';
  try {
    return new Date(iso).toLocaleString([], { dateStyle: 'short', timeStyle: 'medium' });
  } catch (_) {
    return iso;
  }
}

function fmtRelative(iso) {
  if (!iso) return '-';
  const d = new Date(iso);
  const diff = d - new Date();
  const s = Math.floor(diff / 1000);
  if (Math.abs(s) < 60) return s >= 0 ? s + 's' : t('cronPast') || 'past';
  if (s < 0) return t('cronPast') || 'past';
  if (s < 3600) return Math.floor(s / 60) + 'm';
  return Math.floor(s / 3600) + 'h ' + Math.floor((s % 3600) / 60) + 'm';
}

function mergeSectionDescriptors(target, ...sections) {
  for (const section of sections) {
    if (!section) continue;
    Object.defineProperties(target, Object.getOwnPropertyDescriptors(section));
  }
  return target;
}

function truncate(s, max) {
  if (!s) return '';
  return s.length > max ? s.substring(0, max) + '...' : s;
}

function truncateBody(body) {
  if (!body) return '';
  const str = typeof body === 'string' ? body : JSON.stringify(body);
  if (str.length > RESPONSE_BODY_TRUNCATE_LEN) return str.substring(0, RESPONSE_BODY_TRUNCATE_LEN) + '...';
  return str;
}

function truncateMessage(msg) {
  if (!msg) return '-';
  const maxLen = 60;
  if (msg.length > maxLen) return msg.substring(0, maxLen) + '...';
  return msg;
}

function automationStatusBadge(status) {
  const value = String(status || '').toLowerCase();
  if (!value) return 'hc-badge-gray';
  if (value === 'ok' || value === 'passed' || value === 'completed') return 'hc-badge-green';
  if (value === 'triggered') return 'hc-badge-blue';
  if (value === 'warning') return 'hc-badge-orange';
  if (value === 'error' || value === 'failed') return 'hc-badge-red';
  return 'hc-badge-gray';
}

function automationNeedsAttention(status) {
  const value = String(status || '').toLowerCase();
  return value === 'error' || value === 'failed' || value === 'warning' || value === 'cancelled';
}

function formatStatusText(value) {
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

function truncatePreview(text) {
  const value = String(text || '').trim();
  if (value.length <= PREVIEW_TEXT_LIMIT) return value;
  return value.slice(0, PREVIEW_TEXT_LIMIT) + '…';
}

function normalizeHookEvents(data) {
  const items = Array.isArray(data) ? data : ((data && data.items) || []);
  return items.map(item => ({
    trigger: String((item && item.trigger) || '').trim(),
    description: String((item && item.description) || '').trim(),
    category: String((item && item.category) || '').trim(),
    allowed_phases: Array.isArray(item && item.allowed_phases) ? item.allowed_phases.map(String) : [],
    can_block: item && item.can_block === true,
    supports_async: item && item.supports_async === true,
  })).filter(item => item.trigger);
}

function fallbackHookEvents() {
  return FALLBACK_HOOK_EVENT_SPECS.map(item => ({
    trigger: String(item.trigger || '').trim(),
    description: String(item.description || '').trim(),
    category: String(item.category || '').trim(),
    allowed_phases: Array.isArray(item.allowed_phases) ? item.allowed_phases.map(String) : [],
    can_block: item.can_block === true,
    supports_async: item.supports_async === true,
  })).filter(item => item.trigger);
}

function buildHookEventOptions(catalog, currentTrigger) {
  const source = Array.isArray(catalog) && catalog.length ? catalog : fallbackHookEvents();
  const seen = new Set();
  const items = [];
  for (const item of source) {
    if (!item || !item.trigger || seen.has(item.trigger)) continue;
    seen.add(item.trigger);
    items.push(item);
  }
  const current = String(currentTrigger || '').trim();
  if (current && !seen.has(current)) {
    items.push({
      trigger: current,
      description: '',
      category: '',
      allowed_phases: ['post'],
      can_block: false,
      supports_async: true,
    });
  }
  return items;
}

function findHookEventSpec(catalog, trigger) {
  const current = String(trigger || '').trim();
  if (!current) return null;
  return buildHookEventOptions(catalog, current).find(item => item.trigger === current) || null;
}

function buildHookPhaseOptions(spec) {
  const allowed = (spec && Array.isArray(spec.allowed_phases) && spec.allowed_phases.length)
    ? spec.allowed_phases
    : ['post'];
  return HOOK_PHASE_OPTIONS.filter(option => allowed.includes(option.value));
}

function normalizeHookConfig(values, catalog) {
  const next = Object.assign({}, values || {});
  const eventOptions = buildHookEventOptions(catalog, next.trigger);
  if (!String(next.trigger || '').trim() && eventOptions.length) {
    next.trigger = eventOptions[0].trigger;
  }
  const spec = findHookEventSpec(eventOptions, next.trigger);
  const phaseOptions = buildHookPhaseOptions(spec);
  const currentPhase = String(next.phase || '').trim();
  if ((!currentPhase || !phaseOptions.some(option => option.value === currentPhase)) && phaseOptions.length) {
    next.phase = phaseOptions[0].value;
  }
  if ((spec && spec.supports_async === false) || next.phase === 'pre') {
    next.async = false;
  }
  return next;
}

function selectOptionValue(option) {
  if (option && typeof option === 'object') {
    if (option.value != null) return String(option.value);
    if (option.trigger != null) return String(option.trigger);
  }
  return String(option || '');
}

function selectOptionLabel(option) {
  if (option && typeof option === 'object') {
    if (option.label) return option.label;
    if (option.trigger) {
      const trigger = String(option.trigger);
      const description = String(option.description || '').trim();
      return description ? `${trigger} — ${description}` : trigger;
    }
  }
  return formatStatusText(selectOptionValue(option));
}

function selectOptionsIncludeValue(options, value) {
  const current = String(value || '').trim();
  if (!current) return true;
  return (Array.isArray(options) ? options : []).some(option => selectOptionValue(option) === current);
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

function suggestedActionLabel(action) {
  if (!action) return '';
  const labelFn = RESULT_ACTION_LABELS[action.kind];
  return (labelFn ? labelFn() : null) || action.label || formatStatusText(action.kind || '');
}

function automationDetailCompletion(detail) {
  return detail && detail.latest_completion ? detail.latest_completion : null;
}

function automationDetailResult(detail) {
  const completion = automationDetailCompletion(detail);
  return completion && completion.result ? completion.result : null;
}

function automationDetailBundle(detail) {
  const completion = automationDetailCompletion(detail);
  return completion && completion.bundle ? completion.bundle : null;
}

function automationDetailDelivery(detail) {
  const completion = automationDetailCompletion(detail);
  if (completion && completion.delivery) return completion.delivery;
  const bundle = automationDetailBundle(detail);
  return bundle && bundle.delivery ? bundle.delivery : null;
}

function automationDetailOutcome(detail) {
  const completion = automationDetailCompletion(detail);
  const bundle = automationDetailBundle(detail);
  if (bundle && bundle.outcome) return bundle.outcome;
  const result = automationDetailResult(detail);
  if (completion && completion.outcome) return completion.outcome;
  return result && result.outcome ? result.outcome : '';
}

function automationDetailSummary(detail) {
  const bundle = automationDetailBundle(detail);
  if (bundle && bundle.summary) return bundle.summary;
  const result = automationDetailResult(detail);
  if (result && result.summary) return result.summary;
  return '';
}

function automationDetailBlocks(detail) {
  const delivery = automationDetailDelivery(detail);
  return delivery && Array.isArray(delivery.blocks) ? delivery.blocks : [];
}

function automationDetailArtifacts(detail) {
  const bundle = automationDetailBundle(detail);
  if (bundle && Array.isArray(bundle.deliverables)) return bundle.deliverables;
  const result = automationDetailResult(detail);
  return result && Array.isArray(result.deliverables) ? result.deliverables : [];
}

function automationDetailActions(detail) {
  const delivery = automationDetailDelivery(detail);
  if (delivery && Array.isArray(delivery.next_actions) && delivery.next_actions.length) return delivery.next_actions;
  const result = automationDetailResult(detail);
  return result && Array.isArray(result.next_actions) ? result.next_actions : [];
}

function automationDetailSuggestedActions(detail) {
  const bundle = automationDetailBundle(detail);
  return bundle && Array.isArray(bundle.suggested_actions) ? bundle.suggested_actions : [];
}

function automationDetailVerification(detail) {
  const completion = automationDetailCompletion(detail);
  return completion && completion.verification ? completion.verification : null;
}

function automationDetailVerificationStatus(detail) {
  const verification = automationDetailVerification(detail);
  return verification && verification.status ? verification.status : '';
}

function automationDetailVerificationSummary(detail) {
  const verification = automationDetailVerification(detail);
  return verification && verification.summary ? verification.summary : '';
}

function automationDetailVerificationChecks(detail) {
  const verification = automationDetailVerification(detail);
  return verification && Array.isArray(verification.checks) ? verification.checks : [];
}

function automationHasResultReceipt(detail) {
  const completion = automationDetailCompletion(detail);
  if (!completion) return false;
  return !!(
    automationDetailResult(detail) ||
    automationDetailBlocks(detail).length > 0 ||
    automationDetailArtifacts(detail).length > 0 ||
    automationDetailActions(detail).length > 0 ||
    automationDetailSuggestedActions(detail).length > 0
  );
}

function automationDetailRunID(detail) {
  const result = automationDetailResult(detail);
  if (result && result.run_id) return result.run_id;
  const item = detail && detail.item ? detail.item : null;
  return item && item.last_execution ? item.last_execution.run_id || '' : '';
}

function automationDetailRunHref(detail) {
  if (detail && detail.run_path) return detail.run_path;
  const runID = automationDetailRunID(detail);
  return runID ? '#/runs/' + encodeURIComponent(runID) : '';
}

function automationExecutionRunHref(execution) {
  const runID = execution && execution.run_id ? String(execution.run_id).trim() : '';
  return runID ? '#/runs/' + encodeURIComponent(runID) : '';
}

function automationHookPayloadPreview(detail) {
  return detail && detail.latest_payload_preview ? detail.latest_payload_preview : null;
}

function automationResultBlockText(block) {
  if (!block) return '';
  return String(block.content || '').trim();
}

function automationResultBlockData(block) {
  if (!block || block.data == null || block.data === '') return '';
  return prettyData(block.data);
}

function automationResultActionTitle(action) {
  if (!action) return 'Action';
  return String(action.label || suggestedActionLabel(action) || action.kind || 'Action').trim();
}

function automationResultActionDescription(action) {
  if (!action) return '';
  const params = action.params ? prettyData(action.params) : '';
  const reason = String(action.reason || '').trim();
  if (reason && params) return reason + '\n' + params;
  return reason || params;
}

function automationResultActionHref(action) {
  if (!action) return '';
  const kind = String(action.kind || '').trim();
  const target = String(action.target || '').trim();
  if (!target) return '';
  if (kind === 'open_artifact') return artifactPreviewPath({ uri: target });
  if (kind === 'open_url') return safeExternalURL(target);
  return '';
}

function automationNotificationStats(item) {
  return item && item.notifications && typeof item.notifications === 'object' ? item.notifications : null;
}

function automationNotificationTarget(item) {
  const delivery = item && item.delivery && typeof item.delivery === 'object' ? item.delivery : null;
  if (!delivery) return '';
  const channel = String(delivery.channel || '').trim();
  const target = String(delivery.target || '').trim();
  if (channel && target) return `${channel} · ${target}`;
  return channel || target || '';
}

function automationNotificationTotal(item) {
  const stats = automationNotificationStats(item);
  return stats ? Number(stats.total_count || 0) : 0;
}

function automationNotificationFailures(item) {
  const stats = automationNotificationStats(item);
  return stats ? Number(stats.failure_count || 0) : 0;
}

function automationNotificationToday(item) {
  const stats = automationNotificationStats(item);
  return stats ? Number(stats.today_count || 0) : 0;
}

function automationNotificationLastStatus(item) {
  const stats = automationNotificationStats(item);
  return stats ? String(stats.last_status || '').trim() : '';
}

function automationNotificationLastAttemptAt(item) {
  const stats = automationNotificationStats(item);
  return stats ? stats.last_attempt_at || '' : '';
}

function automationNotificationLastDeliveredAt(item) {
  const stats = automationNotificationStats(item);
  return stats ? stats.last_delivered_at || '' : '';
}

function automationNotificationLastError(item) {
  const stats = automationNotificationStats(item);
  return stats ? String(stats.last_error || '').trim() : '';
}

function automationNotificationStatus(item) {
  const status = automationNotificationLastStatus(item).toLowerCase();
  if (status) return status;
  if (automationNotificationTotal(item) > 0 || automationNotificationFailures(item) > 0) return 'tracked';
  if (automationNotificationTarget(item)) return 'configured';
  return '';
}

function automationNotificationStatusBadge(item) {
  const status = automationNotificationStatus(item);
  if (status === 'delivered') return 'hc-badge-green';
  if (status === 'failed') return 'hc-badge-red';
  if (status === 'tracked') return 'hc-badge-blue';
  if (status === 'configured') return 'hc-badge-gray';
  return 'hc-badge-gray';
}

function automationNotificationStatusText(item) {
  const status = automationNotificationStatus(item);
  if (status === 'delivered') return t('automationNotifDelivered') || 'Delivered';
  if (status === 'failed') return t('automationNotifFailed') || 'Failed';
  if (status === 'tracked') return t('automationNotifTracked') || 'Tracked';
  if (status === 'configured') return t('automationNotifConfigured') || 'Configured';
  return t('automationNotifNotConfigured') || 'Not configured';
}

function automationHasNotificationPanel(item) {
  return !!(automationNotificationStats(item) || automationNotificationTarget(item));
}

function automationNotificationNeedsAttention(item) {
  return automationNotificationStatus(item) === 'failed';
}

function automationNotificationBlurb(item) {
  if (!automationHasNotificationPanel(item)) return '';
  const parts = [];
  const target = automationNotificationTarget(item);
  if (target) parts.push(target);
  const counters = [];
  const today = automationNotificationToday(item);
  const total = automationNotificationTotal(item);
  const failures = automationNotificationFailures(item);
  if (today > 0) counters.push(`${t('automationNotifToday') || 'today'} ${today}`);
  if (total > 0) counters.push(`${t('automationNotifTotal') || 'total'} ${total}`);
  if (failures > 0) counters.push(`${t('automationNotifFailed2') || 'failed'} ${failures}`);
  if (counters.length === 0) counters.push(automationNotificationStatusText(item));
  parts.push(counters.join(' · '));
  return parts.join(' · ');
}

function aggregateNotificationSummary(items) {
  const summary = {
    totalCount: 0,
    failureCount: 0,
    todayCount: 0,
    trackedCount: 0,
    attentionCount: 0,
  };
  for (const item of Array.isArray(items) ? items : []) {
    if (!automationHasNotificationPanel(item)) continue;
    summary.trackedCount++;
    summary.totalCount += automationNotificationTotal(item);
    summary.failureCount += automationNotificationFailures(item);
    summary.todayCount += automationNotificationToday(item);
    if (automationNotificationNeedsAttention(item)) {
      summary.attentionCount++;
    }
  }
  return summary;
}

function hookTriggerSummary(hook) {
  const trigger = String((hook && hook.trigger) || '').trim();
  const phase = String((hook && hook.phase) || '').trim();
  if (trigger && phase) return trigger + ' · ' + phase;
  return trigger || phase || '-';
}

function hookTargetLabel(hook) {
  if (!hook) return '-';
  if (hook.kind === 'http') return hook.url || '-';
  if (hook.kind === 'command') return hook.command || '-';
  return '-';
}

function hookLastExecution(hook) {
  return hook && hook.last_execution ? hook.last_execution : null;
}

function hookHeadersMapToText(headers) {
  if (!headers || typeof headers !== 'object') return '';
  return Object.keys(headers)
    .sort((left, right) => left.localeCompare(right))
    .map(key => `${key}: ${headers[key] == null ? '' : String(headers[key])}`)
    .join('\n');
}

function parseHookHeadersText(text) {
  const value = String(text || '').trim();
  if (!value) return { headers: {}, error: '' };
  const headers = {};
  const lines = value.split(/\r?\n/);
  for (let index = 0; index < lines.length; index++) {
    const line = String(lines[index] || '').trim();
    if (!line) continue;
    const splitAt = line.indexOf(':');
    if (splitAt <= 0) {
      return {
        headers: null,
        error: `Invalid header on line ${index + 1}. Use "Header-Name: value".`,
      };
    }
    const name = line.slice(0, splitAt).trim();
    const headerValue = line.slice(splitAt + 1).trim();
    if (!name) {
      return {
        headers: null,
        error: `Invalid header on line ${index + 1}. Header name is required.`,
      };
    }
    headers[name] = headerValue;
  }
  return { headers, error: '' };
}

// ---------------------------------------------------------------------------
// Watch helpers
// ---------------------------------------------------------------------------

function sourceLabel(item) {
  const source = item && item.source ? item.source : {};
  const kind = source.kind || item.source_kind;
  const http = source.http || {};
  const file = source.file || {};
  const feed = source.feed || {};
  const mailbox = source.mailbox || {};
  const browserSnapshot = source.browser_snapshot || {};
  const calendar = source.calendar || {};
  const webhook = source.webhook || {};
  const structuredInbox = source.structured_app_inbox || {};
  if (kind === 'http') return http.url || item.source_url || '-';
  if (kind === 'file') return file.path || item.source_path || '-';
  if (kind === 'feed') return feed.url || item.source_url || '-';
  if (kind === 'browser_snapshot') return browserSnapshot.url || item.source_url || '-';
  if (kind === 'calendar') {
    const query = calendar.query || item.calendar_query || '';
    return query ? 'calendar query=' + query : 'calendar';
  }
  if (kind === 'mailbox') {
    const folder = mailbox.folder || item.mailbox_folder || 'INBOX';
    const query = mailbox.query || item.mailbox_query || '';
    return query ? folder + ' query=' + query : folder;
  }
  if (kind === 'webhook') {
    const sessionKey = webhook.session_key || item.source_session_key || '';
    if (sessionKey) return sessionKey;
    const webhookId = webhook.webhook_id || item.webhook_id || '';
    const senderId = webhook.sender_id || item.webhook_sender_id || '';
    if (webhookId && senderId) return 'webhook:' + webhookId + ':' + senderId;
    if (webhookId) return 'webhook:' + webhookId;
    return 'webhook inbox';
  }
  if (kind === 'structured_app_inbox') {
    return structuredInbox.session_key || item.source_session_key || '-';
  }
  return item.source_url || item.source_path || '-';
}

function watchStatusBadge(status) {
  const value = String(status || '').toLowerCase();
  if (value === 'triggered') return 'hc-badge-blue';
  if (value === 'unchanged' || value === 'primed') return 'hc-badge-gray';
  if (value === 'error') return 'hc-badge-red';
  return 'hc-badge-green';
}

function watchExecution(item) {
  return item && item.last_execution ? item.last_execution : null;
}

function watchExecutionStatus(item) {
  const last = watchExecution(item);
  return (last && last.status) || item.last_status || '';
}

function watchVerificationStatus(item) {
  const last = watchExecution(item);
  return (last && last.verification_status) || item.last_verification_status || '';
}

function watchVerificationSummary(item) {
  const last = watchExecution(item);
  return (last && last.verification_summary) || item.last_verification_summary || '';
}

function watchExecutionSummary(item) {
  const last = watchExecution(item);
  if (!last) return '';
  return last.error || last.summary || '';
}

function watchNextCheckAt(item) {
  return item.next_run_at || item.NextRunAt || item.next_check_at || item.NextCheckAt || '';
}

function watchLastCheckAt(item) {
  return item.last_run_at || item.LastRunAt || item.last_checked_at || item.LastCheckedAt || '';
}

function watchItemID(item) {
  return item.id || item.ID || '';
}

function verificationBadge(status) {
  const value = String(status || '').toLowerCase();
  if (value === 'failed') return 'hc-badge-red';
  if (value === 'warning') return 'hc-badge-orange';
  if (value === 'passed') return 'hc-badge-green';
  return 'hc-badge-gray';
}

function defaultNewWatch() {
  const view = {
    name: '',
    interval: '5m',
    sourceKind: 'http',
    sourceUrl: '',
    sourcePath: '',
    calendarQuery: '',
    calendarLimit: 50,
    sourceSessionKey: '',
    webhookId: '',
    webhookSenderId: '',
    inboxLimit: 20,
    mailboxFolder: 'INBOX',
    mailboxQuery: '',
    mailboxLimit: 20,
    prompt: '',
    enabled: true,
    fireOnStart: false,
  };

  return view;
}

function buildCreateSource(form) {
  const kind = String(form.sourceKind || 'http');
  if (kind === 'file') {
    return { kind, file: { path: String(form.sourcePath || '').trim() } };
  }
  if (kind === 'feed') {
    return { kind, feed: { url: String(form.sourceUrl || '').trim() } };
  }
  if (kind === 'mailbox') {
    const limit = Number(form.mailboxLimit || 0);
    return {
      kind,
      mailbox: {
        folder: String(form.mailboxFolder || 'INBOX').trim() || 'INBOX',
        query: String(form.mailboxQuery || '').trim(),
        limit: Number.isFinite(limit) && limit > 0 ? limit : 20,
      },
    };
  }
  if (kind === 'calendar') {
    const limit = Number(form.calendarLimit || 0);
    return {
      kind,
      calendar: {
        query: String(form.calendarQuery || '').trim(),
        limit: Number.isFinite(limit) && limit > 0 ? limit : 50,
      },
    };
  }
  if (kind === 'webhook') {
    const limit = Number(form.inboxLimit || 0);
    return {
      kind,
      webhook: {
        session_key: String(form.sourceSessionKey || '').trim(),
        webhook_id: String(form.webhookId || '').trim(),
        sender_id: String(form.webhookSenderId || '').trim(),
        limit: Number.isFinite(limit) && limit > 0 ? limit : 20,
      },
    };
  }
  if (kind === 'structured_app_inbox') {
    const limit = Number(form.inboxLimit || 0);
    return {
      kind,
      structured_app_inbox: {
        session_key: String(form.sourceSessionKey || '').trim(),
        limit: Number.isFinite(limit) && limit > 0 ? limit : 20,
      },
    };
  }
  if (kind === 'browser_snapshot') {
    return { kind, browser_snapshot: { url: String(form.sourceUrl || '').trim() } };
  }
  return { kind: 'http', http: { url: String(form.sourceUrl || '').trim() } };
}

function watchToForm(item) {
  const source = item && item.source ? item.source : {};
  const http = source.http || {};
  const file = source.file || {};
  const feed = source.feed || {};
  const mailbox = source.mailbox || {};
  const browserSnapshot = source.browser_snapshot || {};
  const calendar = source.calendar || {};
  const webhook = source.webhook || {};
  const structuredInbox = source.structured_app_inbox || {};
  return {
    name: String(item && item.name || ''),
    interval: String(item && item.interval || '5m'),
    sourceKind: String(source.kind || item.source_kind || 'http'),
    sourceUrl: String(http.url || feed.url || browserSnapshot.url || item.source_url || ''),
    sourcePath: String(file.path || item.source_path || ''),
    sourceSessionKey: String(webhook.session_key || structuredInbox.session_key || item.source_session_key || ''),
    calendarQuery: String(calendar.query || item.calendar_query || ''),
    calendarLimit: Number(calendar.limit || item.calendar_limit || 50),
    webhookId: String(webhook.webhook_id || item.webhook_id || ''),
    webhookSenderId: String(webhook.sender_id || item.webhook_sender_id || ''),
    inboxLimit: Number(webhook.limit || structuredInbox.limit || item.inbox_limit || 20),
    mailboxFolder: String(mailbox.folder || item.mailbox_folder || 'INBOX'),
    mailboxQuery: String(mailbox.query || item.mailbox_query || ''),
    mailboxLimit: Number(mailbox.limit || item.mailbox_limit || 20),
    prompt: String(item && item.prompt || ''),
    enabled: item ? !!item.enabled : true,
    fireOnStart: item ? !!item.fire_on_start : false,
  };
}

// ---------------------------------------------------------------------------
// Schedule helpers
// ---------------------------------------------------------------------------

function scheduleStr(job) {
  const sched = job.schedule || job.Schedule || {};
  if (typeof sched === 'string') return sched;
  let str = sched.expression || sched.Expression || sched.cron || sched.Cron || sched.every || sched.Every || sched.interval || sched.Interval || sched.at || sched.At || '-';
  if (typeof str === 'object') str = JSON.stringify(str);
  return String(str);
}

function scheduleExecution(item) {
  return item && item.last_execution ? item.last_execution : null;
}

function scheduleExecutionText(item) {
  const last = scheduleExecution(item);
  if (!last) return '';
  return last.error || last.summary || '';
}

function jobEnabled(job) {
  if (job.enabled !== undefined) return job.enabled;
  if (job.Enabled !== undefined) return job.Enabled;
  return true;
}

// ---------------------------------------------------------------------------
// Hook helpers
// ---------------------------------------------------------------------------

function statusCodeClass(code) {
  if (!code) return 'hc-badge-gray';
  if (code >= 200 && code < 300) return 'hc-badge-green';
  if (code >= 400 && code < 500) return 'hc-badge-orange';
  if (code >= 500) return 'hc-badge-red';
  return 'hc-badge-gray';
}

function templateKindLabel(kind) {
  const value = String(kind || '').trim();
  if (value === 'cron') return t('automationKindScheduledRun') || 'Scheduled Run';
  if (value === 'wakeup') return t('automationKindWakeupTrigger') || 'Wakeup Trigger';
  if (value === 'watch') return t('automationKindWatchWorkflow') || 'Watch Workflow';
  if (value === 'hook') return t('automationKindIntegrationHook') || 'Integration Hook';
  return t('automationKindAutomation') || 'Automation';
}

function templateKindBadge(kind) {
  const value = String(kind || '').trim();
  if (value === 'cron') return 'hc-badge-blue';
  if (value === 'wakeup') return 'hc-badge-purple';
  if (value === 'watch') return 'hc-badge-green';
  if (value === 'hook') return 'hc-badge-orange';
  return 'hc-badge-gray';
}

const TEMPLATE_TAB_KINDS = {
  schedules: ['cron', 'wakeup'],
  watch: ['watch'],
  hooks: ['hook'],
};

function buildTemplateFieldDefs() {
  return {
    name: { label: t('automationFieldName') || 'Name', type: 'text', placeholder: t('automationFieldNamePlaceholder') || 'Automation name' },
    schedule: { label: t('automationFieldSchedule') || 'Schedule', type: 'text', placeholder: '0 9 * * 1-5' },
    prompt: { label: t('automationFieldPrompt') || 'Prompt', type: 'textarea', placeholder: t('automationFieldPromptPlaceholder') || 'Describe what this automation should do.' },
    session_key: { label: t('automationFieldSessionKey') || 'Session Key', type: 'text', placeholder: 'team:daily-report' },
    model: { label: t('automationFieldModel') || 'Model', type: 'text', placeholder: t('automationFieldModelPlaceholder') || 'Optional model override' },
    channel: { label: t('automationFieldChannel') || 'Channel', type: 'text', placeholder: 'webchat' },
    message: { label: t('automationFieldMessage') || 'Message', type: 'textarea', placeholder: t('automationFieldMessagePlaceholder') || 'Message injected when the trigger fires.' },
    interval: { label: t('automationFieldInterval') || 'Interval', type: 'text', placeholder: '5m' },
    source_url: { label: t('automationFieldSourceUrl') || 'Source URL', type: 'text', placeholder: 'https://example.com' },
    source_path: { label: t('automationFieldSourcePath') || 'Source Path', type: 'text', placeholder: '/path/to/file' },
    source_session_key: { label: t('automationFieldSourceSessionKey') || 'Source Session Key', type: 'text', placeholder: 'slack:C123' },
    calendar_query: { label: t('automationFieldCalendarQuery') || 'Calendar Query', type: 'text', placeholder: t('automationFieldCalendarQueryPlaceholder') || 'today OR next 7 days' },
    calendar_limit: { label: t('automationFieldCalendarLimit') || 'Calendar Limit', type: 'number', min: 1 },
    webhook_id: { label: t('automationFieldWebhookId') || 'Webhook ID', type: 'text', placeholder: 'wh_demo' },
    webhook_sender_id: { label: t('automationFieldSenderId') || 'Sender ID', type: 'text', placeholder: 'user_123' },
    inbox_limit: { label: t('automationFieldInboxLimit') || 'Inbox Limit', type: 'number', min: 1 },
    mailbox_folder: { label: t('automationFieldMailboxFolder') || 'Mailbox Folder', type: 'text', placeholder: 'INBOX' },
    mailbox_query: { label: t('automationFieldMailboxQuery') || 'Mailbox Query', type: 'text', placeholder: t('automationFieldMailboxQueryPlaceholder') || 'from:customer@example.com' },
    mailbox_limit: { label: t('automationFieldMailboxLimit') || 'Mailbox Limit', type: 'number', min: 1 },
    fire_on_start: { label: t('automationFieldFireOnStart') || 'Fire On Start', type: 'checkbox' },
    trigger: { label: t('automationFieldTrigger') || 'Trigger', type: 'select' },
    phase: { label: t('automationFieldPhase') || 'Phase', type: 'select' },
    url: { label: t('automationFieldWebhookUrl') || 'Webhook URL', type: 'text', placeholder: t('automationFieldWebhookUrlPlaceholder') || 'https://example.com/webhooks/hopclaw' },
    command: { label: t('automationFieldCommand') || 'Command', type: 'text', placeholder: t('automationFieldCommandPlaceholder') || 'notify-ops.sh' },
    filter: { label: t('automationFieldFilter') || 'Filter', type: 'text', placeholder: t('automationFieldFilterPlaceholder') || 'event.tool_name == "notify.send"' },
    retry_count: { label: t('automationFieldRetryCount') || 'Retry Count', type: 'number', min: 0 },
    timeout_sec: { label: t('automationFieldTimeoutSec') || 'Timeout (seconds)', type: 'number', min: 1 },
    async: { label: t('automationFieldAsyncDispatch') || 'Async Dispatch', type: 'checkbox' },
    enabled: { label: t('automationFieldEnabled') || 'Enabled', type: 'checkbox' },
  };
}

const TEMPLATE_RECOMMENDED_FIELDS = {
  cron: ['name', 'session_key', 'model', 'enabled'],
  wakeup: ['name', 'channel', 'session_key', 'enabled'],
  watch: ['name', 'interval', 'fire_on_start', 'enabled'],
  hook: ['name', 'trigger', 'phase', 'enabled'],
};

function templateKindsForTab(tab) {
  return TEMPLATE_TAB_KINDS[String(tab || '').trim()] || [];
}

function templateTabForKind(kind) {
  const value = String(kind || '').trim();
  if (value === 'cron' || value === 'wakeup') return 'schedules';
  if (value === 'watch') return 'watch';
  if (value === 'hook') return 'hooks';
  return 'schedules';
}

function templateDefaults(template) {
  if (!template) return {};
  const kind = String(template.kind || '').trim();
  if (kind === 'cron') return Object.assign({}, template.cron_defaults || {});
  if (kind === 'wakeup') return Object.assign({}, template.wakeup_defaults || {});
  if (kind === 'watch') return Object.assign({}, template.watch_defaults || {});
  if (kind === 'hook') return Object.assign({}, template.hook_defaults || {});
  return {};
}

function templateFieldDef(field) {
  const defs = buildTemplateFieldDefs();
  return Object.assign({}, defs[field] || { label: formatStatusText(field), type: 'text' });
}

function templateWizardFieldList(template, requiredOnly) {
  if (!template) return [];
  const kind = String(template.kind || '').trim();
  const required = Array.isArray(template.required_fields) ? template.required_fields : [];
  const requiredFields = required.map(item => {
    const base = templateFieldDef(item.field);
    return Object.assign(base, item, { field: item.field, required: item.required !== false });
  });
  if (requiredOnly) return requiredFields;
  const seen = new Set(requiredFields.map(item => item.field));
  const recommended = (TEMPLATE_RECOMMENDED_FIELDS[kind] || [])
    .filter(field => !seen.has(field))
    .map(field => Object.assign(templateFieldDef(field), { field, required: false }));
  return requiredFields.concat(recommended);
}

function templateValuePresent(value) {
  if (typeof value === 'boolean') return true;
  if (typeof value === 'number') return Number.isFinite(value);
  return String(value || '').trim() !== '';
}

function templateCanQuickCreate(template) {
  const defaults = templateDefaults(template);
  return templateWizardFieldList(template, true).every(field => templateValuePresent(defaults[field.field]));
}

// ---------------------------------------------------------------------------
// Template
// ---------------------------------------------------------------------------

const TEMPLATE = `
<div class="hc-page-shell hc-page-shell-spacious">
  <div class="hc-card hc-page-hero">
    <div class="hc-page-section-head">
      <div class="hc-page-hero-copy">
        <h2 class="hc-page-hero-title">{{ t('automationTitle') || 'Automation' }}</h2>
        <div class="hc-page-hero-subtitle">
          {{ t('automationIntroText') }}
        </div>
      </div>
      <div class="hc-page-section-actions">
        <button class="hc-btn hc-btn-secondary" @click="refreshAutomationTab()">{{ t('refresh') || 'Refresh' }}</button>
        <button v-if="tab === 'schedules'" class="hc-btn hc-btn-primary" @click="schedOpenCreate('cron')">{{ t('cronAdd') || 'Add Cron' }}</button>
        <button v-if="tab === 'watch'" class="hc-btn hc-btn-primary" @click="watchToggleForm()">{{ watchShowForm ? (t('cancel') || 'Cancel') : (t('watchCreate') || 'Create Watch') }}</button>
        <button v-if="tab === 'hooks'" class="hc-btn hc-btn-primary" @click="hookOpenCreate()">{{ t('hooksAdd') || 'Add Hook' }}</button>
      </div>
    </div>
    <div class="hc-page-metrics">
      <div v-for="metric in automationMetrics()" :key="metric.label" class="hc-page-metric-card">
        <div class="hc-page-metric-label">{{ metric.label }}</div>
        <div class="hc-page-metric-value">{{ metric.value }}</div>
        <div class="hc-page-metric-note">{{ metric.note }}</div>
      </div>
    </div>
  </div>
  <div v-if="tab !== 'agents'" class="hc-card hc-automation-template-strip">
    <div class="hc-page-section-head" style="cursor:pointer" @click="starterTemplatesCollapsed = !starterTemplatesCollapsed">
      <div class="hc-page-hero-copy">
        <h3 class="hc-page-hero-title" style="font-size:1rem">
          <span style="display:inline-block;transition:transform 0.2s;margin-right:6px" :style="starterTemplatesCollapsed ? '' : 'transform:rotate(90deg)'">&#9654;</span>
          {{ t('automationStarterTitle') }}
        </h3>
        <div class="hc-page-hero-subtitle">
          {{ t('automationStarterDesc') }}
        </div>
      </div>
      <div class="hc-page-section-actions">
        <button class="hc-btn hc-btn-secondary" @click.stop="loadStarterTemplates()">{{ t('refresh') || 'Refresh' }}</button>
      </div>
    </div>
    <div v-show="!starterTemplatesCollapsed">
    <div v-if="starterTemplatesLoading" class="hc-state-block">
      <div class="hc-state-block-title">{{ t('loading') || 'Loading...' }}</div>
      <div class="hc-state-block-copy">{{ t('automationStarterLoading') }}</div>
    </div>
    <div v-else-if="starterTemplatesError" class="hc-state-block">
      <div class="hc-state-block-title">{{ t('loadError') || 'Load failed' }}</div>
      <div class="hc-state-block-actions">
        <button class="hc-btn hc-btn-sm hc-btn-primary" @click="loadStarterTemplates()">{{ t('retryLoad') || 'Retry' }}</button>
      </div>
    </div>
    <div v-else-if="starterTemplatesForCurrentTab().length === 0" class="hc-state-block">
      <div class="hc-state-block-title">{{ t('automationNoStarterForTab') || 'No starter workflows for this tab yet' }}</div>
      <div class="hc-state-block-copy">{{ t('automationMoreTemplatesHint') || 'More starter templates can be added here over time.' }}</div>
    </div>
    <div v-else class="hc-automation-template-grid">
      <article v-for="template in starterTemplatesForCurrentTab()" :key="template.id" class="hc-automation-template-card">
        <div class="hc-automation-template-head">
          <div>
            <div class="hc-automation-template-title">{{ template.name }}</div>
            <div class="hc-automation-template-subtitle">{{ template.headline || template.summary }}</div>
          </div>
          <span class="hc-badge" :class="templateKindBadge(template.kind)">{{ templateKindLabel(template.kind) }}</span>
        </div>
        <div v-if="template.summary" class="hc-automation-template-summary">{{ template.summary }}</div>
        <div v-if="template.outcome" class="hc-automation-template-outcome">{{ template.outcome }}</div>
        <div v-if="template.tags && template.tags.length" class="hc-result-chip-row">
          <span v-for="tag in template.tags" :key="template.id + '-tag-' + tag" class="hc-result-chip">{{ tag }}</span>
        </div>
        <div v-if="template.setup_hints && template.setup_hints.length" class="hc-automation-template-hints">
          <div v-for="(hint, idx) in template.setup_hints.slice(0, 2)" :key="template.id + '-hint-' + idx" class="hc-automation-template-hint">• {{ hint }}</div>
        </div>
        <div class="hc-automation-template-actions">
          <button class="hc-btn hc-btn-sm hc-btn-primary" :data-testid="'automation-starter-use-' + template.id" @click="openStarterTemplateWizard(template)">{{ t('automationUseTemplate') || 'Use template' }}</button>
          <button
            class="hc-btn hc-btn-sm hc-btn-secondary"
            :disabled="templateWizardCreating || !templateCanQuickCreate(template)"
            @click="quickCreateStarterTemplate(template)"
          >
            {{ templateWizardCreating && templateWizardTemplate && templateWizardTemplate.id === template.id ? (t('automationCreating') || 'Creating\u2026') : (t('automationOneClickCreate') || 'One-click create') }}
          </button>
        </div>
      </article>
    </div>
    </div>
  </div>

  <div v-if="templateWizardOpen && templateWizardTemplate" class="hc-template-wizard-overlay" data-testid="automation-template-wizard" @click.self="closeStarterTemplateWizard()">
    <div class="hc-template-wizard">
      <div class="hc-template-wizard-head">
        <div>
          <div class="hc-settings-context-kicker">{{ templateKindLabel(templateWizardKind()) }}</div>
          <div class="hc-template-wizard-title">{{ templateWizardName() }}</div>
          <div class="hc-template-wizard-copy">{{ templateWizardHeadline() }}</div>
        </div>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="closeStarterTemplateWizard()">{{ t('automationWizardClose') || 'Close' }}</button>
      </div>

      <div class="hc-template-wizard-grid">
        <div class="hc-template-wizard-main">
          <div class="hc-template-wizard-section">
            <div class="hc-result-section-head">
              <div>
                <div class="hc-result-block-title">{{ t('automationWizardRequiredInputs') || 'Required inputs' }}</div>
                <div class="hc-state-block-copy">{{ t('automationWizardRequiredDesc') || 'Keep the first step small. We only ask for the fields that matter for a valid first run.' }}</div>
              </div>
              <span class="hc-badge" :class="templateKindBadge(templateWizardKind())">{{ templateKindLabel(templateWizardKind()) }}</span>
            </div>

            <div class="hc-template-wizard-fields">
              <div v-for="field in templateWizardRequiredFields()" :key="'req-' + field.field" class="hc-form-group">
                <label class="hc-form-label">{{ field.label || formatStatusText(field.field) }}{{ field.required ? ' *' : '' }}</label>
                <textarea
                  v-if="field.type === 'textarea'"
                  class="hc-form-input hc-template-wizard-textarea"
                  :data-testid="'automation-template-field-' + field.field"
                  :rows="field.rows || 4"
                  :placeholder="field.placeholder || ''"
                  :value="templateWizardValues[field.field] || ''"
                  @input="updateStarterTemplateField(field.field, $event.target.value)"
                ></textarea>
                <select
                  v-else-if="field.type === 'select'"
                  class="hc-form-select"
                  :data-testid="'automation-template-field-' + field.field"
                  :value="templateWizardValues[field.field] || ''"
                  @change="updateStarterTemplateField(field.field, $event.target.value)"
                >
                  <option
                    v-if="templateWizardMissingSelectValue(field)"
                    :value="templateWizardValues[field.field] || ''"
                  >{{ templateWizardCurrentOptionLabel(field) }}</option>
                  <option
                    v-for="opt in templateWizardFieldOptions(field)"
                    :key="field.field + '-' + templateWizardOptionValue(opt)"
                    :value="templateWizardOptionValue(opt)"
                  >{{ templateWizardOptionLabel(opt) }}</option>
                </select>
                <label v-else-if="field.type === 'checkbox'" class="hc-checkbox-label">
                  <input type="checkbox" :data-testid="'automation-template-field-' + field.field" :checked="templateWizardValues[field.field] === true" @change="updateStarterTemplateField(field.field, $event.target.checked)" />
                  <span>{{ field.help || field.placeholder || 'Enabled' }}</span>
                </label>
                <input
                  v-else
                  class="hc-form-input"
                  :data-testid="'automation-template-field-' + field.field"
                  :type="field.type === 'number' ? 'number' : 'text'"
                  :min="field.min || null"
                  :placeholder="field.placeholder || ''"
                  :value="templateWizardValues[field.field] || ''"
                  @input="updateStarterTemplateField(field.field, field.type === 'number' ? Number($event.target.value) : $event.target.value)"
                />
                <div v-if="field.help" class="hc-template-wizard-help">{{ field.help }}</div>
              </div>
            </div>
          </div>

          <div v-if="templateWizardRecommendedFields().length" class="hc-template-wizard-section">
            <div class="hc-result-section-head">
              <div>
                <div class="hc-result-block-title">{{ t('automationWizardRecommendedTweaks') || 'Recommended tweaks' }}</div>
                <div class="hc-state-block-copy">{{ t('automationWizardRecommendedDesc') || 'Optional adjustments before the first run. You can still go deeper in advanced edit.' }}</div>
              </div>
            </div>
            <div class="hc-template-wizard-fields hc-template-wizard-fields-compact">
              <div v-for="field in templateWizardRecommendedFields()" :key="'opt-' + field.field" class="hc-form-group">
                <label class="hc-form-label">{{ field.label || formatStatusText(field.field) }}</label>
                <select
                  v-if="field.type === 'select'"
                  class="hc-form-select"
                  :value="templateWizardValues[field.field] || ''"
                  @change="updateStarterTemplateField(field.field, $event.target.value)"
                >
                  <option
                    v-if="templateWizardMissingSelectValue(field)"
                    :value="templateWizardValues[field.field] || ''"
                  >{{ templateWizardCurrentOptionLabel(field) }}</option>
                  <option
                    v-for="opt in templateWizardFieldOptions(field)"
                    :key="field.field + '-' + templateWizardOptionValue(opt)"
                    :value="templateWizardOptionValue(opt)"
                  >{{ templateWizardOptionLabel(opt) }}</option>
                </select>
                <label v-else-if="field.type === 'checkbox'" class="hc-checkbox-label">
                  <input type="checkbox" :checked="templateWizardValues[field.field] === true" @change="updateStarterTemplateField(field.field, $event.target.checked)" />
                  <span>{{ field.label || formatStatusText(field.field) }}</span>
                </label>
                <input
                  v-else
                  class="hc-form-input"
                  :type="field.type === 'number' ? 'number' : 'text'"
                  :min="field.min || null"
                  :placeholder="field.placeholder || ''"
                  :value="templateWizardValues[field.field] || ''"
                  @input="updateStarterTemplateField(field.field, field.type === 'number' ? Number($event.target.value) : $event.target.value)"
                />
              </div>
            </div>
          </div>
        </div>

        <aside class="hc-template-wizard-side">
          <div class="hc-template-wizard-section">
            <div class="hc-result-block-title">{{ t('automationWizardExpectedOutcome') || 'Expected outcome' }}</div>
            <div class="hc-template-wizard-copy">{{ templateWizardOutcome() }}</div>
          </div>

          <div v-if="templateWizardSetupHints().length" class="hc-template-wizard-section">
            <div class="hc-result-block-title">{{ t('automationWizardSetupHints') || 'Setup hints' }}</div>
            <div class="hc-template-wizard-points">
              <div v-for="(hint, idx) in templateWizardSetupHints()" :key="'wizard-hint-' + idx" class="hc-automation-template-hint">• {{ hint }}</div>
            </div>
          </div>

          <div class="hc-template-wizard-section">
            <div class="hc-result-block-title">{{ t('automationWizardCreationMode') || 'Creation mode' }}</div>
            <div class="hc-template-wizard-copy">
              {{ t('automationWizardCreationModeDesc') || 'Create this starter directly now, or jump into the advanced form if you want to expose every field.' }}
            </div>
            <div class="hc-template-wizard-actions">
              <button class="hc-btn hc-btn-primary" :disabled="templateWizardCreating || !templateWizardCanCreate()" @click="createStarterTemplateFromWizard()">
                {{ templateWizardCreating ? (t('automationCreating') || 'Creating\u2026') : (t('automationWizardCreateNow') || 'Create now') }}
              </button>
              <button class="hc-btn hc-btn-secondary" data-testid="automation-template-advanced-edit" :disabled="templateWizardCreating" @click="applyStarterTemplateFromWizard()">
                {{ t('automationWizardAdvancedEdit') || 'Advanced edit' }}
              </button>
            </div>
          </div>
        </aside>
      </div>
    </div>
  </div>

  <div class="hc-tabs">
    <button class="hc-tab" data-testid="automation-tab-agents" :class="{ active: tab === 'agents' }" @click="switchTab('agents')">
      {{ t('agentsTitle') || 'Agents' }}
    </button>
    <button class="hc-tab" data-testid="automation-tab-schedules" :class="{ active: tab === 'schedules' }" @click="switchTab('schedules')">
      {{ t('schedulesTitle') || 'Schedules' }}
      <span v-if="schedTotalCount > 0" class="hc-tab-badge">{{ schedTotalCount }}</span>
    </button>
    <button class="hc-tab" data-testid="automation-tab-watch" :class="{ active: tab === 'watch' }" @click="switchTab('watch')">
      {{ t('navWatch') || 'Watch' }}
      <span v-if="watchItems.length > 0" class="hc-tab-badge">{{ watchItems.length }}</span>
    </button>
    <button class="hc-tab" data-testid="automation-tab-hooks" :class="{ active: tab === 'hooks' }" @click="switchTab('hooks')">
      {{ t('hooksTitle') || 'Hooks' }}
      <span v-if="hookItems.length > 0" class="hc-tab-badge">{{ hookItems.length }}</span>
    </button>
  </div>

  <!-- ===================================================================== -->
  <!-- AGENTS TAB                                                            -->
  <!-- ===================================================================== -->
  <div v-if="tab === 'agents'" class="hc-page-shell">
    <div class="hc-toolbar-grid hc-toolbar-grid-compact">
      <input class="hc-search-input" type="text" :placeholder="t('searchPlaceholder')" v-model="agSearch" @input="agPage=1" />
      <button class="hc-btn hc-btn-secondary" @click="loadAgents()">{{ t('refresh') || 'Refresh' }}</button>
      <div></div>
    </div>

    <div v-if="agLoading" class="hc-state-block">
      <div class="hc-state-block-title">{{ t('loading') || 'Loading...' }}</div>
      <div class="hc-state-block-copy">{{ t('automationAgentsLoadingCopy') || 'Refreshing agent profiles and execution defaults.' }}</div>
    </div>
    <div v-else-if="agError" class="hc-state-block">
      <div>{{ t('loadError') }}</div>
      <div class="hc-state-block-actions">
        <button class="hc-btn hc-btn-sm hc-btn-primary" @click="loadAgents()">{{ t('retryLoad') }}</button>
      </div>
    </div>
    <div v-else-if="agFiltered.length === 0 && agSearch" class="hc-state-block">
      <div class="hc-state-block-title">{{ t('noResults') || 'No results' }}</div>
      <div class="hc-state-block-copy">{{ t('automationAgentsNoFilterResult') || 'Try a different agent name or clear the filter.' }}</div>
    </div>
    <div v-else-if="agFiltered.length === 0" class="hc-state-block">
      <div class="hc-state-block-title">{{ t('agentsNoAgents') || 'No agents configured' }}</div>
      <div class="hc-state-block-copy">{{ t('automationAgentsEmptyCopy') || 'Agent profiles will appear here once the runtime exposes them.' }}</div>
    </div>
    <div v-else class="hc-runs-table-wrap">
      <table class="hc-table">
        <thead><tr>
          <th>{{ t('agentsName') || 'Name' }}</th>
          <th>{{ t('agentsModel') || 'Model' }}</th>
          <th>{{ t('agentsTools') || 'Tools' }}</th>
          <th>{{ t('agentsSkills') || 'Skills' }}</th>
        </tr></thead>
        <tbody v-for="a in agPaginated" :key="agName(a)">
          <tr class="hc-runs-row" :class="{ expanded: agExpanded === agName(a) }" @click="agToggle(agName(a))" style="cursor:pointer">
            <td><strong>{{ agName(a) }}</strong></td>
            <td>{{ a.model || a.Model || '-' }}</td>
            <td>{{ (a.tools || a.Tools || []).length }}</td>
            <td>{{ (a.skills || a.Skills || []).length }}</td>
          </tr>
          <tr v-if="agExpanded === agName(a)" class="hc-runs-detail-row">
            <td colspan="4">
              <div class="hc-run-detail">
                <div v-if="agDetailLoading" class="hc-loading" style="padding:12px">{{ t('loading') }}</div>
                <div v-else-if="agDetail">
                  <div v-if="agSystemPrompt(agDetail)" class="hc-run-detail-section">
                    <h4>{{ t('agentsSystemPrompt') || 'System Prompt' }}</h4>
                    <pre style="white-space:pre-wrap;font-size:0.82rem;background:var(--code-bg);padding:12px;border-radius:var(--radius-sm);max-height:200px;overflow:auto">{{ truncate(agSystemPrompt(agDetail), promptPreviewLen) }}</pre>
                  </div>
                  <div style="margin-top:12px">
                    <button class="hc-btn hc-btn-sm hc-btn-primary" @click.stop="agOpenEdit()">{{ t('agentsEdit') || 'Edit Config' }}</button>
                  </div>
                  <div v-if="agTools(agDetail).length > 0" class="hc-run-detail-section" style="margin-top:12px">
                    <h4>{{ t('agentsTools') || 'Tools' }} ({{ agTools(agDetail).length }})</h4>
                    <div style="display:flex;flex-wrap:wrap;gap:6px;margin-top:6px">
                      <span v-for="tool in agTools(agDetail)" :key="tool" class="hc-badge hc-badge-blue" style="font-size:0.78rem">{{ tool }}</span>
                    </div>
                  </div>
                  <div v-if="agSkills(agDetail).length > 0" class="hc-run-detail-section" style="margin-top:12px">
                    <h4>{{ t('agentsSkills') || 'Skills' }} ({{ agSkills(agDetail).length }})</h4>
                    <div style="display:flex;flex-wrap:wrap;gap:6px;margin-top:6px">
                      <span v-for="sk in agSkills(agDetail)" :key="sk" class="hc-badge hc-badge-green" style="font-size:0.78rem">{{ sk }}</span>
                    </div>
                  </div>
                </div>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-if="agTotalPages > 1" class="hc-pagination">
        <span class="hc-pagination-info">{{ (agPage-1)*Number(agPageSize)+1 }}-{{ Math.min(agPage*Number(agPageSize), agFiltered.length) }} {{ t('of') }} {{ agFiltered.length }}</span>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="agPage<=1" @click="agPage--">&#8249;</button>
        <span class="hc-pagination-pages">{{ agPage }} / {{ agTotalPages }}</span>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="agPage>=agTotalPages" @click="agPage++">&#8250;</button>
        <select class="hc-form-select hc-pagination-size" v-model="agPageSize" @change="agPage=1">
          <option value="20">20</option>
          <option value="50">50</option>
          <option value="100">100</option>
        </select>
      </div>
    </div>

    <!-- Agent Edit Modal -->
    <div v-if="agShowEdit" style="position:fixed;inset:0;z-index:1000;display:flex;align-items:center;justify-content:center;background:rgba(0,0,0,0.5)" @click.self="agShowEdit=false">
      <div style="background:var(--surface);border-radius:var(--radius-sm);padding:24px;width:100%;max-width:560px;max-height:90vh;overflow:auto;box-shadow:0 8px 32px rgba(0,0,0,0.2)">
        <h3 style="margin:0 0 4px 0;font-size:1rem">{{ t('agentsEdit') || 'Edit Agent Config' }}</h3>
        <p style="margin:0 0 16px 0;font-size:0.82rem;color:var(--ink3)">{{ t('agentsEditDesc') || 'Update agent configuration' }}</p>
        <div style="display:grid;gap:12px">
          <div>
            <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('agentsSystemPrompt') || 'System Prompt' }}</label>
            <textarea class="hc-form-input" v-model="agEditForm.system_prompt" rows="4" style="width:100%;resize:vertical"></textarea>
          </div>
          <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
            <div>
              <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('agentsDefaultModel') || 'Default Model' }}</label>
              <input class="hc-form-input" type="text" v-model="agEditForm.default_model" style="width:100%" />
            </div>
            <div>
              <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('agentsTriageModel') || 'Triage Model' }}</label>
              <input class="hc-form-input" type="text" v-model="agEditForm.triage_model" style="width:100%" />
            </div>
          </div>
          <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
            <div>
              <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('agentsMaxToolRounds') || 'Max Tool Rounds' }}</label>
              <input class="hc-form-input" type="number" v-model.number="agEditForm.max_tool_rounds" min="0" style="width:100%" />
            </div>
            <div>
              <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('agentsQueueMode') || 'Queue Mode' }}</label>
              <select class="hc-form-select" v-model="agEditForm.queue_mode" style="width:100%">
                <option value="enqueue">enqueue</option>
                <option value="sequential">sequential</option>
              </select>
            </div>
          </div>
          <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
            <div>
              <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('agentsDedupeWindow') || 'Dedupe Window' }}</label>
              <input class="hc-form-input" type="text" v-model="agEditForm.dedupe_window" placeholder="1m" style="width:100%" />
            </div>
            <div>
              <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('agentsContextWindow') || 'Context Window' }}</label>
              <input class="hc-form-input" type="number" v-model.number="agEditForm.default_context_window" min="0" placeholder="0 = 128000" style="width:100%" />
            </div>
          </div>
        </div>
        <div style="margin-top:16px;display:flex;gap:8px;justify-content:flex-end">
          <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="agShowEdit=false">{{ t('cancel') || 'Cancel' }}</button>
          <button class="hc-btn hc-btn-sm hc-btn-primary" :disabled="agSaving" @click="agSaveConfig()">{{ agSaving ? (t('loading') || 'Saving...') : (t('save') || 'Save') }}</button>
        </div>
      </div>
    </div>
  </div>

  <!-- ===================================================================== -->
  <!-- SCHEDULES TAB (Cron + Wakeup merged)                                  -->
  <!-- ===================================================================== -->
  <div v-if="tab === 'schedules'" class="hc-list-detail-layout">
    <div class="hc-list-panel">
    <div class="hc-runs-filters" style="margin-bottom:12px">
      <input class="hc-search-input" type="text" :placeholder="t('searchPlaceholder')" v-model="schedSearch" @input="schedPage=1" />
      <select class="hc-form-select hc-filter-select" v-model="schedTypeFilter" @change="schedPage=1" style="margin-left:8px">
        <option value="">{{ t('all') || 'All Types' }}</option>
        <option value="cron">{{ t('cronTitle') || 'Cron' }}</option>
        <option value="wakeup">{{ t('wakeupTitle') || 'Wakeup' }}</option>
      </select>
      <button class="hc-btn hc-btn-sm hc-btn-primary" data-testid="automation-schedules-add-cron" style="margin-left:8px" @click="schedOpenCreate('cron')">{{ t('cronCreateJob') || 'Add Cron' }}</button>
      <button class="hc-btn hc-btn-sm hc-btn-secondary" style="margin-left:4px" @click="schedOpenCreate('wakeup')">{{ t('wakeupAdd') || 'Add Trigger' }}</button>
    </div>

    <!-- Cron create form -->
    <div v-if="schedShowForm && schedFormType === 'cron'" class="hc-card" data-testid="automation-schedules-cron-form" style="margin-bottom:16px;padding:16px;border:1px solid var(--border);border-radius:var(--radius-sm)">
      <h3 style="margin:0 0 12px 0;font-size:0.95rem">{{ t('cronCreateJob') || 'Create Cron Job' }}</h3>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('cronName') || 'Name' }} *</label>
          <input class="hc-form-input" data-testid="automation-schedules-cron-name" type="text" v-model="schedCronForm.name" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('cronExpression') || 'Cron Expression' }} *</label>
          <input class="hc-form-input" data-testid="automation-schedules-cron-expression" type="text" v-model="schedCronForm.schedule" placeholder="*/5 * * * *" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceSessionKey') || 'Session Key' }}</label>
          <input class="hc-form-input" data-testid="automation-schedules-cron-session-key" type="text" v-model="schedCronForm.sessionKey" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('model') || 'Model' }}</label>
          <input class="hc-form-input" data-testid="automation-schedules-cron-model" type="text" v-model="schedCronForm.model" style="width:100%" />
        </div>
        <div style="display:flex;align-items:end;gap:12px">
          <label style="display:flex;align-items:center;gap:6px;font-size:0.82rem;cursor:pointer">
            <input type="checkbox" v-model="schedCronForm.enabled" /> {{ t('enabled') || 'Enabled' }}
          </label>
        </div>
      </div>
      <div style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('cronPrompt') || 'Prompt' }}</label>
        <textarea class="hc-form-input" data-testid="automation-schedules-cron-prompt" v-model="schedCronForm.prompt" rows="3" style="width:100%;resize:vertical"></textarea>
      </div>
      <div style="font-size:0.78rem;color:var(--ink3);margin-top:6px">{{ t('cronCreateHint') || 'Cron expression: minute hour day month weekday' }}</div>
      <div style="margin-top:12px;display:flex;gap:8px">
        <button class="hc-btn hc-btn-sm hc-btn-primary" data-testid="automation-schedules-cron-create" :disabled="!schedCronForm.name || !schedCronForm.schedule || schedSaving" @click="schedCreateCron()">{{ t('cronCreateJob') || 'Create' }}</button>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="schedShowForm=false">{{ t('cancel') || 'Cancel' }}</button>
      </div>
    </div>

    <!-- Wakeup create/edit form -->
    <div v-if="schedShowForm && schedFormType === 'wakeup'" class="hc-card" style="margin-bottom:16px;padding:16px;border:1px solid var(--border);border-radius:var(--radius-sm)">
      <h3 style="margin:0 0 12px 0;font-size:0.95rem">{{ schedEditingId ? (t('wakeupEdit') || 'Edit Trigger') : (t('wakeupAdd') || 'Add Trigger') }}</h3>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('wakeupName') || 'Name' }} *</label>
          <input class="hc-form-input" type="text" v-model="schedWakeupForm.name" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('wakeupSchedule') || 'Schedule' }} *</label>
          <input class="hc-form-input" type="text" v-model="schedWakeupForm.schedule" placeholder="0 9 * * *" style="width:100%" />
          <div style="font-size:0.75rem;color:var(--ink3);margin-top:2px">{{ t('wakeupScheduleHint') || 'Cron expression' }}</div>
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('wakeupChannel') || 'Channel' }}</label>
          <input class="hc-form-input" type="text" v-model="schedWakeupForm.channel" placeholder="webchat" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceSessionKey') || 'Session Key' }}</label>
          <input class="hc-form-input" type="text" v-model="schedWakeupForm.sessionKey" style="width:100%" />
        </div>
      </div>
      <div style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('wakeupMessage') || 'Message' }} *</label>
        <textarea class="hc-form-input" v-model="schedWakeupForm.message" rows="3" :placeholder="t('wakeupMessagePlaceholder') || 'Please summarize today\\'s news'" style="width:100%;resize:vertical"></textarea>
      </div>
      <div style="margin-top:10px;display:flex;align-items:center;gap:12px">
        <label style="display:flex;align-items:center;gap:6px;font-size:0.82rem;cursor:pointer">
          <input type="checkbox" v-model="schedWakeupForm.enabled" /> {{ t('wakeupEnabled') || 'Enabled' }}
        </label>
      </div>
      <div style="margin-top:12px;display:flex;gap:8px">
        <button class="hc-btn hc-btn-sm hc-btn-primary" :disabled="!schedWakeupForm.name || !schedWakeupForm.schedule || !schedWakeupForm.message || schedSaving" @click="schedSaveWakeup()">{{ schedEditingId ? (t('save') || 'Save') : (t('create') || 'Create') }}</button>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="schedShowForm=false">{{ t('cancel') || 'Cancel' }}</button>
      </div>
    </div>

    <div v-if="schedLoading" class="hc-loading">{{ t('loading') }}</div>
    <div v-else-if="schedError && !schedCronNotEnabled && !schedWakeupNotEnabled" class="hc-empty">
      <div>{{ t('loadError') }}</div>
      <button class="hc-btn hc-btn-sm hc-btn-primary" style="margin-top:8px" @click="loadSchedules()">{{ t('retryLoad') }}</button>
    </div>
    <div v-else-if="schedCronNotEnabled && schedWakeupNotEnabled" class="hc-empty" data-testid="automation-schedules-unavailable" style="max-width:520px;margin:40px auto;text-align:center">
      <div style="font-size:1.1rem;font-weight:600;margin-bottom:8px">{{ t('schedulesNotEnabled') || 'Scheduling Not Enabled' }}</div>
      <p style="font-size:0.84rem;color:var(--ink3);margin:0 0 16px 0;line-height:1.5">{{ t('schedulesNotEnabledDesc') || 'Neither Cron nor Wakeup services are enabled. Enable them in your configuration.' }}</p>
      <button class="hc-btn hc-btn-primary" data-testid="automation-schedules-enable-cron" :disabled="schedEnabling" @click="schedEnableCron()">{{ schedEnabling ? (t('cronEnabling') || 'Enabling...') : (t('cronEnable') || 'Enable Cron') }}</button>
    </div>
    <div v-else-if="schedFiltered.length === 0 && schedSearch" class="hc-empty">{{ t('noResults') }}</div>
    <div v-else-if="schedFiltered.length === 0" class="hc-empty" data-testid="automation-schedules-empty">{{ t('schedulesEmpty') || 'No scheduled jobs or triggers' }}</div>
    <div v-else class="hc-runs-table-wrap">
      <table class="hc-table">
        <thead><tr>
          <th>{{ t('cronName') || 'Name' }}</th>
          <th>{{ t('type') || 'Type' }}</th>
          <th>{{ t('cronSchedule') || 'Schedule' }}</th>
          <th>{{ t('enabled') || 'Enabled' }}</th>
          <th>{{ t('status') || 'Status' }}</th>
          <th>{{ t('cronNextRun') || 'Next Run' }}</th>
          <th>{{ t('cronLastRun') || 'Last Run' }}</th>
          <th>{{ t('actions') }}</th>
        </tr></thead>
        <tbody>
          <tr v-for="item in schedPaginated" :key="item._uid">
            <td>
              <strong>{{ item.name || item.id || '-' }}</strong>
              <div v-if="item._type === 'wakeup' && (item.message || item.prompt_preview)" style="font-size:0.75rem;color:var(--ink3);margin-top:2px;max-width:180px">{{ truncateMessage(item.message || item.prompt_preview) }}</div>
              <div v-else-if="scheduleExecutionText(item)" style="font-size:0.75rem;color:var(--ink3);margin-top:2px;max-width:220px">{{ truncateMessage(scheduleExecutionText(item)) }}</div>
              <div v-if="automationNotificationBlurb(item)" style="font-size:0.75rem;color:var(--ink3);margin-top:4px;max-width:240px;word-break:break-word">{{ automationNotificationBlurb(item) }}</div>
            </td>
            <td>
              <span v-if="item._type === 'cron'" class="hc-badge hc-badge-blue">{{ t('cronTitle') || 'Cron' }}</span>
              <span v-else class="hc-badge hc-badge-purple">{{ t('wakeupTitle') || 'Wakeup' }}</span>
            </td>
            <td style="font-family:var(--font-mono);font-size:0.82rem;white-space:nowrap">
              <template v-if="item._type === 'cron'">{{ scheduleStr(item) }}</template>
              <template v-else>{{ item.schedule || '-' }}</template>
            </td>
            <td>
              <span v-if="item._type === 'cron' ? jobEnabled(item) : item.enabled" class="hc-badge hc-badge-green">{{ t('yes') || 'Yes' }}</span>
              <span v-else class="hc-badge hc-badge-red">{{ t('no') || 'No' }}</span>
            </td>
            <td>
              <span class="hc-badge" :class="automationStatusBadge(scheduleExecution(item) && scheduleExecution(item).status)">{{ (scheduleExecution(item) && scheduleExecution(item).status) || '-' }}</span>
            </td>
            <td style="white-space:nowrap">{{ fmtRelative(item.next_run_at || item.NextRunAt) }}</td>
            <td style="white-space:nowrap">{{ fmtTime(item.last_run_at || item.LastRunAt) }}</td>
            <td style="white-space:nowrap">
              <template v-if="item._type === 'cron'">
                <button class="hc-btn hc-btn-sm hc-btn-secondary" @click.stop="openAutomationDetail(item.kind || item._type, item.id || item.ID)">{{ t('details') || 'Details' }}</button>
                <button class="hc-btn hc-btn-sm hc-btn-primary" @click.stop="schedTriggerCron(item.id || item.ID)">{{ t('cronRun') || 'Run' }}</button>
                <button class="hc-btn hc-btn-sm hc-btn-danger" style="margin-left:6px" @click.stop="schedDeleteCron(item.id || item.ID)">{{ t('delete') || 'Delete' }}</button>
              </template>
              <template v-else>
                <button class="hc-btn hc-btn-sm hc-btn-secondary" @click.stop="openAutomationDetail(item.kind || item._type, item.id)">{{ t('details') || 'Details' }}</button>
                <button class="hc-btn hc-btn-sm hc-btn-secondary" @click.stop="schedEditWakeup(item)">{{ t('edit') || 'Edit' }}</button>
                <button class="hc-btn hc-btn-sm hc-btn-danger" style="margin-left:6px" @click.stop="schedDeleteWakeup(item.id)">{{ t('delete') || 'Delete' }}</button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-if="schedTotalPages > 1" class="hc-pagination">
        <span class="hc-pagination-info">{{ (schedPage-1)*Number(schedPageSize)+1 }}-{{ Math.min(schedPage*Number(schedPageSize), schedFiltered.length) }} {{ t('of') }} {{ schedFiltered.length }}</span>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="schedPage<=1" @click="schedPage--">&#8249;</button>
        <span class="hc-pagination-pages">{{ schedPage }} / {{ schedTotalPages }}</span>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="schedPage>=schedTotalPages" @click="schedPage++">&#8250;</button>
        <select class="hc-form-select hc-pagination-size" v-model="schedPageSize" @change="schedPage=1">
          <option value="20">20</option>
          <option value="50">50</option>
          <option value="100">100</option>
        </select>
      </div>
    </div>
    </div>
    <div v-if="automationDetailVisibleForTab('schedules')" class="hc-card hc-detail-panel" style="padding:16px">
      <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:12px">
        <div>
          <div style="font-size:1rem;font-weight:600">{{ (automationDetail && automationDetail.item && automationDetail.item.name) || (t('details') || 'Details') }}</div>
          <div style="font-size:0.82rem;color:var(--ink3);margin-top:4px">{{ (automationDetail && automationDetail.item && automationDetail.item.kind) ? formatStatusText(automationDetail.item.kind) : '' }}</div>
        </div>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="closeAutomationDetail()">{{ t('close') || 'Close' }}</button>
      </div>
      <div v-if="automationDetailLoading" class="hc-loading">{{ t('loading') }}</div>
      <div v-else-if="automationDetailError" class="hc-empty">
        <div>{{ t('loadError') }}</div>
        <button class="hc-btn hc-btn-sm hc-btn-primary" style="margin-top:8px" @click="loadAutomationDetail()">{{ t('retryLoad') }}</button>
      </div>
      <div v-else-if="automationDetail && automationDetail.item">
        <div class="hc-run-detail-grid">
          <div><span class="hc-run-detail-label">{{ t('type') || 'Type' }}</span><span>{{ formatStatusText(automationDetail.item.kind) }}</span></div>
          <div><span class="hc-run-detail-label">{{ t('cronSchedule') || 'Schedule' }}</span><span>{{ automationDetail.item.schedule || '-' }}</span></div>
          <div v-if="automationDetail.item.session_key"><span class="hc-run-detail-label">{{ t('watchSourceSessionKey') || 'Session Key' }}</span><span>{{ automationDetail.item.session_key }}</span></div>
          <div v-if="automationDetail.item.model"><span class="hc-run-detail-label">{{ t('model') || 'Model' }}</span><span>{{ automationDetail.item.model }}</span></div>
          <div v-if="automationDetail.item.channel"><span class="hc-run-detail-label">{{ t('wakeupChannel') || 'Channel' }}</span><span>{{ automationDetail.item.channel }}</span></div>
          <div><span class="hc-run-detail-label">{{ t('enabled') || 'Enabled' }}</span><span>{{ automationDetail.item.enabled ? (t('yes') || 'Yes') : (t('no') || 'No') }}</span></div>
          <div v-if="automationDetail.item.next_run_at"><span class="hc-run-detail-label">{{ t('cronNextRun') || 'Next Run' }}</span><span>{{ fmtTime(automationDetail.item.next_run_at) }}</span></div>
          <div v-if="automationDetail.item.last_run_at"><span class="hc-run-detail-label">{{ t('cronLastRun') || 'Last Run' }}</span><span>{{ fmtTime(automationDetail.item.last_run_at) }}</span></div>
        </div>
        <div v-if="automationHasNotificationPanel(automationDetail.item)" class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('automationNotifications') || 'Notifications' }}</h4>
          <div class="hc-run-detail-grid">
            <div v-if="automationNotificationTarget(automationDetail.item)"><span class="hc-run-detail-label">{{ t('automationDelivery') || 'Delivery' }}</span><span>{{ automationNotificationTarget(automationDetail.item) }}</span></div>
            <div><span class="hc-run-detail-label">{{ t('status') || 'Status' }}</span><span><span class="hc-badge" :class="automationNotificationStatusBadge(automationDetail.item)">{{ automationNotificationStatusText(automationDetail.item) }}</span></span></div>
            <div><span class="hc-run-detail-label">{{ t('automationDeliveredToday') || 'Delivered Today' }}</span><span>{{ automationNotificationToday(automationDetail.item) }}</span></div>
            <div><span class="hc-run-detail-label">{{ t('automationDeliveredTotal') || 'Delivered Total' }}</span><span>{{ automationNotificationTotal(automationDetail.item) }}</span></div>
            <div><span class="hc-run-detail-label">{{ t('automationFailures') || 'Failures' }}</span><span>{{ automationNotificationFailures(automationDetail.item) }}</span></div>
            <div v-if="automationNotificationLastDeliveredAt(automationDetail.item)"><span class="hc-run-detail-label">{{ t('automationLastDelivered') || 'Last Delivered' }}</span><span>{{ fmtTime(automationNotificationLastDeliveredAt(automationDetail.item)) }}</span></div>
            <div v-else-if="automationNotificationLastAttemptAt(automationDetail.item)"><span class="hc-run-detail-label">{{ t('automationLastAttempt') || 'Last Attempt' }}</span><span>{{ fmtTime(automationNotificationLastAttemptAt(automationDetail.item)) }}</span></div>
          </div>
          <div v-if="automationNotificationLastError(automationDetail.item)" class="hc-result-block" style="margin-top:12px">
            <div class="hc-result-block-title">{{ t('automationLastDeliveryError') || 'Last Delivery Error' }}</div>
            <div class="hc-result-block-content" style="color:var(--danger)">{{ automationNotificationLastError(automationDetail.item) }}</div>
          </div>
        </div>
        <div v-if="automationDetail.item.prompt_preview || automationDetail.item.message" class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('result') || 'Result' }}</h4>
          <div style="color:var(--ink3)">{{ automationDetail.item.prompt_preview || automationDetail.item.message }}</div>
        </div>
        <div class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('hooksResults') || 'Recent Results' }}</h4>
          <div v-if="!automationDetail.recent_executions || automationDetail.recent_executions.length === 0" style="color:var(--ink3)">{{ t('noData') || 'No data' }}</div>
          <div v-else class="hc-result-block-list">
            <div v-for="(execution, idx) in automationDetail.recent_executions" :key="'exec-s-'+idx" class="hc-result-block">
              <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap">
                <div class="hc-result-block-title">{{ fmtTime(execution.occurred_at) }}</div>
                <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                  <span class="hc-badge" :class="automationStatusBadge(execution.status)">{{ formatStatusText(execution.status) }}</span>
                  <span v-if="execution.verification_status" class="hc-badge" :class="automationStatusBadge(execution.verification_status)">{{ formatStatusText(execution.verification_status) }}</span>
                  <a v-if="automationDetailRunHref(automationDetail)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="automationDetailRunHref(automationDetail)">{{ t('automationViewReceipt') || 'View receipt' }}</a>
                </div>
              </div>
              <div v-if="execution.summary" class="hc-result-block-content">{{ execution.summary }}</div>
              <div v-if="execution.error" class="hc-result-block-content" style="color:var(--danger)">{{ execution.error }}</div>
              <div v-if="execution.verification_summary" class="hc-result-block-content">{{ execution.verification_summary }}</div>
            </div>
          </div>
        </div>
        <div v-if="automationDetail.error_signatures && automationDetail.error_signatures.length" class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('automationFailureSignatures') || 'Failure Signatures' }}</h4>
          <div class="hc-result-block-list">
            <div v-for="(signature, idx) in automationDetail.error_signatures" :key="'hook-signature-'+idx" class="hc-result-block">
              <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap">
                <div class="hc-result-block-title">{{ signature.signature }}</div>
                <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                  <span class="hc-badge hc-badge-red">{{ signature.count }} {{ t('automationHitCount') || 'hit(s)' }}</span>
                  <span v-if="signature.last_occurred_at" class="hc-badge hc-badge-gray">{{ fmtTime(signature.last_occurred_at) }}</span>
                </div>
              </div>
              <div v-if="signature.last_error" class="hc-result-block-content" style="color:var(--danger)">{{ signature.last_error }}</div>
            </div>
          </div>
        </div>
        <div class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('automationDebug') || 'Debug' }}</h4>
          <div class="hc-result-panel">
            <div class="hc-result-panel-header">
              <div class="hc-result-panel-copy">
                <div class="hc-result-panel-title">{{ t('automationHookDebugConsole') || 'Hook debug console' }}</div>
                <div class="hc-result-panel-summary">{{ t('automationHookDebugDesc') || 'Test with custom payload or replay the most recent execution context.' }}</div>
              </div>
              <div class="hc-result-panel-badges">
                <span v-if="automationDetail.can_replay" class="hc-badge hc-badge-blue">{{ t('automationReplayReady') || 'Replay Ready' }}</span>
              </div>
            </div>
            <div class="hc-run-detail-grid" style="margin-top:12px">
              <div><span class="hc-run-detail-label">{{ t('automationDefaultTrigger') || 'Default Trigger' }}</span><span>{{ automationDetail.item.schedule || '-' }}</span></div>
              <div><span class="hc-run-detail-label">{{ t('automationReplay') || 'Replay' }}</span><span>{{ automationDetail.can_replay ? (t('automationReplayAvailable') || 'Available') : (t('automationReplayNoPayload') || 'No recent payload') }}</span></div>
            </div>
            <div style="margin-top:12px;display:grid;grid-template-columns:1fr 160px;gap:10px">
              <div>
                <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('automationTriggerOverride') || 'Trigger Override' }}</label>
                <select class="hc-form-select" v-model="hookDebugTrigger" style="width:100%">
                  <option value="">{{ t('automationUseHookDefault') || 'Use hook default' }}</option>
                  <option v-for="evt in hookEventOptions" :key="'debug-'+evt.trigger" :value="evt.trigger">{{ evt.trigger }}</option>
                </select>
              </div>
              <div>
                <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('automationPhaseOverride') || 'Phase Override' }}</label>
                <select class="hc-form-select" v-model="hookDebugPhase" style="width:100%">
                  <option value="">{{ t('automationUseHookDefault') || 'Use hook default' }}</option>
                  <option value="pre">Pre</option>
                  <option value="post">Post</option>
                  <option value="error">Error</option>
                </select>
              </div>
            </div>
            <div v-if="automationHookPayloadPreview(automationDetail)" class="hc-result-section" style="margin-top:12px">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationLatestPayloadPreview') || 'Latest Payload Preview' }}</div>
              </div>
              <pre class="hc-run-detail-tool-args">{{ prettyData(automationHookPayloadPreview(automationDetail)) }}</pre>
            </div>
            <div class="hc-result-section" style="margin-top:12px">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationCustomTestPayload') || 'Custom Test Payload' }}</div>
              </div>
              <textarea class="hc-form-input" v-model="hookDebugPayload" rows="8" style="width:100%;resize:vertical;font-family:var(--font-mono)" placeholder='{"run_id":"run-demo","session_id":"sess-demo","tool_name":"notify.send"}'></textarea>
            </div>
            <div style="margin-top:12px;display:flex;gap:8px;flex-wrap:wrap">
              <button class="hc-btn hc-btn-sm hc-btn-primary" :disabled="hookDebugRunning" @click="hookTestFire()">{{ hookDebugRunning ? (t('automationRunning') || 'Running\u2026') : (t('automationTestFire') || 'Test Fire') }}</button>
              <button class="hc-btn hc-btn-sm hc-btn-secondary" :disabled="hookDebugRunning || !automationDetail.can_replay" @click="hookReplayLast()">{{ hookDebugRunning ? (t('automationRunning') || 'Running\u2026') : (t('automationReplayLast') || 'Replay Last') }}</button>
              <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="hookDebugRunning" @click="hookResetDebugPayload()">{{ t('automationResetPayload') || 'Reset Payload' }}</button>
            </div>
            <div v-if="hookDebugLastResult" class="hc-result-section" style="margin-top:12px">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationLastDebugResult') || 'Last Debug Result' }}</div>
              </div>
              <div class="hc-result-block">
                <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap">
                  <div class="hc-result-block-title">{{ hookDebugLastResult.trigger || 'debug' }} · {{ hookDebugLastResult.phase || 'post' }}</div>
                  <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                    <span class="hc-badge" :class="automationStatusBadge(hookDebugLastResult.status)">{{ formatStatusText(hookDebugLastResult.status) }}</span>
                    <a v-if="automationExecutionRunHref(hookDebugLastResult)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="automationExecutionRunHref(hookDebugLastResult)">{{ t('automationViewReceipt') || 'View receipt' }}</a>
                  </div>
                </div>
                <div v-if="hookDebugLastResult.summary" class="hc-result-block-content">{{ hookDebugLastResult.summary }}</div>
                <div v-if="hookDebugLastResult.error" class="hc-result-block-content" style="color:var(--danger)">{{ hookDebugLastResult.error }}</div>
                <pre v-if="hookDebugLastResult.payload_preview" class="hc-run-detail-tool-args">{{ prettyData(hookDebugLastResult.payload_preview) }}</pre>
              </div>
            </div>
          </div>
        </div>
        <div v-if="automationHasResultReceipt(automationDetail)" class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('details') || 'Details' }}</h4>
          <div class="hc-result-panel">
            <div class="hc-result-panel-header">
              <div class="hc-result-panel-copy">
                <div class="hc-result-panel-title">{{ automationDetailSummary(automationDetail) || ((t('result') || 'Result') + ' ' + (t('automationReceiptSuffix') || 'receipt')) }}</div>
                <div v-if="automationDetailResult(automationDetail) && automationDetailResult(automationDetail).output" class="hc-result-panel-summary">{{ automationDetailResult(automationDetail).output }}</div>
              </div>
              <div class="hc-result-panel-badges">
                <span v-if="automationDetailOutcome(automationDetail)" class="hc-badge" :class="automationStatusBadge(automationDetailOutcome(automationDetail))">{{ formatStatusText(automationDetailOutcome(automationDetail)) }}</span>
                <span v-if="automationDetailVerificationStatus(automationDetail)" class="hc-badge" :class="automationStatusBadge(automationDetailVerificationStatus(automationDetail))">{{ formatStatusText(automationDetailVerificationStatus(automationDetail)) }}</span>
              </div>
            </div>
            <div v-if="automationDetailActions(automationDetail).length" class="hc-result-section">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationActions') || 'Actions' }}</div>
                <div class="hc-result-deliverable-meta">{{ automationDetailActions(automationDetail).length }} {{ t('automationItemCount') || 'item(s)' }}</div>
              </div>
              <div class="hc-result-action-grid">
                <div v-for="(action, idx) in automationDetailActions(automationDetail)" :key="'sched-action-'+idx" class="hc-result-action-card">
                  <div class="hc-result-action-head">
                    <div>
                      <div class="hc-result-deliverable-name">{{ automationResultActionTitle(action) }}</div>
                      <div class="hc-result-deliverable-sub">{{ action.kind || 'action' }}</div>
                    </div>
                  </div>
                  <div v-if="automationResultActionDescription(action)" class="hc-result-action-body">{{ automationResultActionDescription(action) }}</div>
                  <div class="hc-result-deliverable-actions">
                    <a v-if="automationResultActionHref(action)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="automationResultActionHref(action)" target="_blank" rel="noopener noreferrer">{{ t('automationOpenAction') || 'Open' }}</a>
                    <span v-else-if="action.target" class="hc-result-deliverable-uri">{{ action.target }}</span>
                  </div>
                </div>
              </div>
            </div>
            <div v-if="automationDetailBlocks(automationDetail).length" class="hc-result-section">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationBlocks') || 'Blocks' }}</div>
                <div class="hc-result-deliverable-meta">{{ automationDetailBlocks(automationDetail).length }} {{ t('automationItemCount') || 'item(s)' }}</div>
              </div>
              <div class="hc-result-block-list">
                <div v-for="(block, idx) in automationDetailBlocks(automationDetail)" :key="'sched-block-'+idx" class="hc-result-block">
                  <div class="hc-result-block-title">{{ block.title || formatStatusText(block.kind || 'summary') }}</div>
                  <div v-if="automationResultBlockText(block)" class="hc-result-block-content">{{ automationResultBlockText(block) }}</div>
                  <pre v-if="automationResultBlockData(block)" class="hc-run-detail-tool-args">{{ automationResultBlockData(block) }}</pre>
                </div>
              </div>
            </div>
            <div v-if="automationDetailArtifacts(automationDetail).length" class="hc-result-section">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationArtifacts') || 'Artifacts' }}</div>
                <div class="hc-result-deliverable-meta">{{ automationDetailArtifacts(automationDetail).length }} {{ t('automationItemCount') || 'item(s)' }}</div>
              </div>
              <div class="hc-result-deliverable-grid">
                <div v-for="(artifact, idx) in automationDetailArtifacts(automationDetail)" :key="'sched-artifact-'+idx" class="hc-result-deliverable-card">
                  <div class="hc-result-deliverable-head">
                    <div>
                      <div class="hc-result-deliverable-name">{{ artifact.name || artifact.label || artifact.kind || 'artifact' }}</div>
                      <div class="hc-result-deliverable-sub">{{ artifact.kind || 'artifact' }}</div>
                    </div>
                    <span v-if="artifact.size_bytes" class="hc-result-deliverable-meta">{{ formatFileSize(artifact.size_bytes) }}</span>
                  </div>
                  <div v-if="artifact.content_type" class="hc-result-deliverable-meta">{{ artifact.content_type }}</div>
                  <div v-if="artifact.preview_text" class="hc-result-deliverable-preview">{{ truncatePreview(artifact.preview_text) }}</div>
                  <div class="hc-result-deliverable-actions">
                    <a v-if="artifactPreviewPath(artifact)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="artifactPreviewPath(artifact)" target="_blank" rel="noopener noreferrer">{{ t('automationOpenPreview') || 'Open preview' }}</a>
                    <span v-else-if="artifact.uri" class="hc-result-deliverable-uri">{{ artifact.uri }}</span>
                  </div>
                </div>
              </div>
            </div>
            <div v-if="automationDetailSuggestedActions(automationDetail).length" class="hc-result-chip-row">
              <span v-for="(action, idx) in automationDetailSuggestedActions(automationDetail)" :key="'sched-suggest-'+idx" class="hc-result-chip" :title="action.reason || ''">{{ suggestedActionLabel(action) }}</span>
            </div>
          </div>
        </div>
        <div v-if="automationDetailVerificationChecks(automationDetail).length" class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('automationVerification') || 'Verification' }}</h4>
          <div class="hc-verification-checks">
            <div v-for="check in automationDetailVerificationChecks(automationDetail)" :key="check.name" class="hc-verification-check">
              <div class="hc-verification-check-header">
                <strong>{{ check.name }}</strong>
                <span class="hc-badge" :class="automationStatusBadge(check.status)">{{ formatStatusText(check.status) }}</span>
                <span v-if="check.domain" style="color:var(--ink3)">{{ check.domain }}</span>
                <span v-if="check.requirement" class="hc-badge hc-badge-gray">{{ formatStatusText(check.requirement) }}</span>
              </div>
              <div v-if="check.summary" class="hc-verification-check-summary">{{ check.summary }}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- ===================================================================== -->
  <!-- WATCH TAB                                                             -->
  <!-- ===================================================================== -->
  <div v-if="tab === 'watch'" class="hc-list-detail-layout">
    <div class="hc-list-panel">
    <div class="hc-runs-filters" style="margin-bottom:12px">
      <input class="hc-search-input" type="text" :placeholder="t('searchPlaceholder')" v-model="watchSearch" @input="watchPage=1" />
      <button class="hc-btn hc-btn-sm hc-btn-primary" style="margin-left:8px" @click="watchToggleForm()">{{ watchEditingId ? (t('watchCreate') || 'Create') : (t('watchCreate') || 'Create') }}</button>
    </div>

    <div v-if="watchShowForm" class="hc-card" style="margin-bottom:16px;padding:16px;border:1px solid var(--border);border-radius:var(--radius-sm)">
      <h3 style="margin:0 0 12px 0;font-size:0.95rem">{{ watchEditingId ? (t('watchEdit') || 'Edit Watch') : (t('watchCreate') || 'Create Watch') }}</h3>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchName') || 'Name' }} *</label>
          <input class="hc-form-input" type="text" v-model="watchForm.name" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchInterval') || 'Interval' }} *</label>
          <input class="hc-form-input" type="text" v-model="watchForm.interval" placeholder="5m" style="width:100%" />
        </div>
      </div>
      <div style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceKind') || 'Source Kind' }} *</label>
        <select class="hc-form-select" v-model="watchForm.sourceKind" style="width:100%">
          <option value="http">{{ t('watchSourceKindHttp') || 'HTTP' }}</option>
          <option value="file">{{ t('watchSourceKindFile') || 'File' }}</option>
          <option value="feed">{{ t('watchSourceKindFeed') || 'RSS/Atom Feed' }}</option>
          <option value="mailbox">{{ t('watchSourceKindMailbox') || 'IMAP Mailbox' }}</option>
          <option value="browser_snapshot">{{ t('watchSourceKindBrowserSnapshot') || 'Browser Snapshot' }}</option>
          <option value="calendar">{{ t('watchSourceKindCalendar') || 'CalDAV Calendar' }}</option>
          <option value="webhook">{{ t('watchSourceKindWebhook') || 'Webhook Inbox' }}</option>
          <option value="structured_app_inbox">{{ t('watchSourceKindStructuredInbox') || 'Structured Inbox' }}</option>
        </select>
      </div>
      <div v-if="watchUsesUrl(watchForm.sourceKind)" style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceUrl') || 'Source URL' }} *</label>
        <input class="hc-form-input" type="text" v-model="watchForm.sourceUrl" :placeholder="watchUrlPlaceholder(watchForm.sourceKind)" style="width:100%" />
      </div>
      <div v-else-if="watchForm.sourceKind === 'file'" style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourcePath') || 'File Path' }} *</label>
        <input class="hc-form-input" type="text" v-model="watchForm.sourcePath" placeholder="/path/to/file.txt" style="width:100%" />
      </div>
      <div v-else-if="watchForm.sourceKind === 'mailbox'" style="margin-top:10px;display:grid;grid-template-columns:1fr 1fr 140px;gap:10px">
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceMailboxFolder') || 'Folder' }}</label>
          <input class="hc-form-input" type="text" v-model="watchForm.mailboxFolder" placeholder="INBOX" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceMailboxQuery') || 'Query' }}</label>
          <input class="hc-form-input" type="text" v-model="watchForm.mailboxQuery" :placeholder="t('optional') || 'Optional'" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceMailboxLimit') || 'Limit' }}</label>
          <input class="hc-form-input" type="number" min="1" max="200" v-model="watchForm.mailboxLimit" style="width:100%" />
        </div>
      </div>
      <div v-else-if="watchForm.sourceKind === 'calendar'" style="margin-top:10px;display:grid;grid-template-columns:1fr 160px;gap:10px">
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceCalendarQuery') || 'Calendar Query' }}</label>
          <input class="hc-form-input" type="text" v-model="watchForm.calendarQuery" :placeholder="t('optional') || 'Optional'" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceCalendarLimit') || 'Limit' }}</label>
          <input class="hc-form-input" type="number" min="1" max="200" v-model="watchForm.calendarLimit" style="width:100%" />
        </div>
      </div>
      <div v-else-if="watchForm.sourceKind === 'webhook'" style="margin-top:10px;display:grid;grid-template-columns:1fr 1fr 140px;gap:10px">
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceSessionKey') || 'Session Key' }}</label>
          <input class="hc-form-input" type="text" v-model="watchForm.sourceSessionKey" :placeholder="t('optional') || 'Optional'" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceWebhookId') || 'Webhook ID' }}</label>
          <input class="hc-form-input" type="text" v-model="watchForm.webhookId" :placeholder="t('optional') || 'Optional'" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceWebhookSenderId') || 'Sender ID' }}</label>
          <input class="hc-form-input" type="text" v-model="watchForm.webhookSenderId" :placeholder="t('optional') || 'Optional'" style="width:100%" />
        </div>
      </div>
      <div v-else-if="watchForm.sourceKind === 'structured_app_inbox'" style="margin-top:10px;display:grid;grid-template-columns:1fr 160px;gap:10px">
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceSessionKey') || 'Session Key' }} *</label>
          <input class="hc-form-input" type="text" v-model="watchForm.sourceSessionKey" placeholder="slack:C123 or webhook:demo:user-1" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceInboxLimit') || 'Limit' }}</label>
          <input class="hc-form-input" type="number" min="1" max="200" v-model="watchForm.inboxLimit" style="width:100%" />
        </div>
      </div>
      <div v-if="watchForm.sourceKind === 'webhook'" style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchSourceInboxLimit') || 'Inbox Limit' }}</label>
        <input class="hc-form-input" type="number" min="1" max="200" v-model="watchForm.inboxLimit" style="width:160px" />
      </div>
      <div style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('watchPrompt') || 'Prompt' }}</label>
        <textarea class="hc-form-input" v-model="watchForm.prompt" rows="3" style="width:100%;resize:vertical"></textarea>
      </div>
      <div style="margin-top:10px;display:flex;gap:16px;flex-wrap:wrap">
        <label style="display:flex;align-items:center;gap:6px;font-size:0.82rem;cursor:pointer">
          <input type="checkbox" v-model="watchForm.enabled" /> {{ t('watchEnabled') || 'Enabled' }}
        </label>
        <label style="display:flex;align-items:center;gap:6px;font-size:0.82rem;cursor:pointer">
          <input type="checkbox" v-model="watchForm.fireOnStart" /> {{ t('watchFireOnStart') || 'Fire on Start' }}
        </label>
      </div>
      <div style="font-size:0.78rem;color:var(--ink3);margin-top:6px">{{ t('watchCreateHint') || 'The agent will be triggered when the source changes.' }}</div>
      <div style="margin-top:12px;display:flex;gap:8px">
        <button class="hc-btn hc-btn-sm hc-btn-primary" :disabled="watchCreating || !watchCanCreate()" @click="watchSave()">{{ watchEditingId ? (t('save') || 'Save') : (t('watchCreate') || 'Create') }}</button>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="watchCancelForm()">{{ t('cancel') || 'Cancel' }}</button>
      </div>
    </div>

    <div v-if="watchLoading" class="hc-loading">{{ t('loading') }}</div>
    <div v-else-if="watchUnavailable" class="hc-empty" style="max-width:520px;margin:40px auto;text-align:center">
      <div style="font-size:1.1rem;font-weight:600;margin-bottom:8px">{{ t('watchUnavailable') || 'Watch Not Available' }}</div>
      <p style="font-size:0.84rem;color:var(--ink3);margin:0;line-height:1.5">{{ t('watchUnavailableDesc') || 'The watch service is not available on this instance.' }}</p>
    </div>
    <div v-else-if="watchError" class="hc-empty">
      <div>{{ t('loadError') }}</div>
      <button class="hc-btn hc-btn-sm hc-btn-primary" style="margin-top:8px" @click="loadWatch()">{{ t('retryLoad') }}</button>
    </div>
    <div v-else-if="watchFiltered.length === 0 && watchSearch" class="hc-empty">{{ t('noResults') }}</div>
    <div v-else-if="watchFiltered.length === 0" class="hc-empty">{{ t('watchEmpty') || 'No watch items' }}</div>
    <div v-else class="hc-runs-table-wrap">
      <table class="hc-table">
        <thead><tr>
          <th>{{ t('watchName') || 'Name' }}</th>
          <th>{{ t('watchSource') || 'Source' }}</th>
          <th>{{ t('watchStatus') || 'Status' }}</th>
          <th>{{ t('watchNextCheck') || 'Next Check' }}</th>
          <th>{{ t('watchLastCheck') || 'Last Check' }}</th>
          <th>{{ t('actions') }}</th>
        </tr></thead>
        <tbody>
          <tr v-for="item in watchPaginated" :key="item.id">
            <td>
              <strong>{{ item.name || '-' }}</strong>
              <div style="font-size:0.78rem;color:var(--ink3);margin-top:2px">{{ item.interval || item.schedule || '-' }}</div>
              <div v-if="watchExecutionSummary(item)" style="font-size:0.75rem;color:var(--ink3);margin-top:4px;max-width:240px;word-break:break-word">{{ truncateMessage(watchExecutionSummary(item)) }}</div>
              <div v-if="automationNotificationBlurb(item)" style="font-size:0.75rem;color:var(--ink3);margin-top:4px;max-width:240px;word-break:break-word">{{ automationNotificationBlurb(item) }}</div>
            </td>
            <td style="max-width:320px">
              <div style="word-break:break-all">{{ item.source_label || sourceLabel(item) }}</div>
            </td>
            <td>
              <span class="hc-badge" :class="watchStatusBadge(watchExecutionStatus(item))">{{ watchExecutionStatus(item) || '-' }}</span>
              <div v-if="watchVerificationStatus(item)" style="margin-top:4px">
                <span class="hc-badge" :class="verificationBadge(watchVerificationStatus(item))">{{ watchVerificationStatus(item) }}</span>
              </div>
              <div style="font-size:0.75rem;color:var(--ink3);margin-top:4px">{{ item.enabled ? (t('enabled') || 'Enabled') : (t('disabled') || 'Disabled') }}</div>
              <div v-if="watchVerificationSummary(item)" style="font-size:0.75rem;color:var(--ink3);margin-top:4px;max-width:240px;word-break:break-word">{{ watchVerificationSummary(item) }}</div>
            </td>
            <td>{{ fmtRelative(watchNextCheckAt(item)) }}</td>
            <td>{{ fmtTime(watchLastCheckAt(item)) }}</td>
            <td>
              <button class="hc-btn hc-btn-sm hc-btn-secondary" @click.stop="openAutomationDetail(item.kind || 'watch', watchItemID(item))">{{ t('details') || 'Details' }}</button>
              <button class="hc-btn hc-btn-sm hc-btn-primary" @click.stop="watchTrigger(watchItemID(item))">{{ t('watchRun') || 'Run' }}</button>
              <button class="hc-btn hc-btn-sm hc-btn-ghost" style="margin-left:6px" @click.stop="watchBeginEdit(item)">{{ t('edit') || 'Edit' }}</button>
              <button class="hc-btn hc-btn-sm hc-btn-ghost" style="margin-left:6px" @click.stop="watchToggleEnabled(item)">{{ item.enabled ? (t('disable') || 'Disable') : (t('enable') || 'Enable') }}</button>
              <button class="hc-btn hc-btn-sm hc-btn-danger" style="margin-left:6px" @click.stop="watchDelete(watchItemID(item))">{{ t('delete') || 'Delete' }}</button>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-if="watchTotalPages > 1" class="hc-pagination">
        <span class="hc-pagination-info">{{ (watchPage-1)*Number(watchPageSize)+1 }}-{{ Math.min(watchPage*Number(watchPageSize), watchFiltered.length) }} {{ t('of') }} {{ watchFiltered.length }}</span>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="watchPage<=1" @click="watchPage--">&#8249;</button>
        <span class="hc-pagination-pages">{{ watchPage }} / {{ watchTotalPages }}</span>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="watchPage>=watchTotalPages" @click="watchPage++">&#8250;</button>
        <select class="hc-form-select hc-pagination-size" v-model="watchPageSize" @change="watchPage=1">
          <option value="20">20</option>
          <option value="50">50</option>
          <option value="100">100</option>
        </select>
      </div>
    </div>
    </div>
    <div v-if="automationDetailVisibleForTab('watch')" class="hc-card hc-detail-panel" style="padding:16px">
      <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:12px">
        <div>
          <div style="font-size:1rem;font-weight:600">{{ (automationDetail && automationDetail.item && automationDetail.item.name) || (t('details') || 'Details') }}</div>
          <div style="font-size:0.82rem;color:var(--ink3);margin-top:4px">{{ (automationDetail && automationDetail.item && automationDetail.item.source_label) || '' }}</div>
        </div>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="closeAutomationDetail()">{{ t('close') || 'Close' }}</button>
      </div>
      <div v-if="automationDetailLoading" class="hc-loading">{{ t('loading') }}</div>
      <div v-else-if="automationDetailError" class="hc-empty">
        <div>{{ t('loadError') }}</div>
        <button class="hc-btn hc-btn-sm hc-btn-primary" style="margin-top:8px" @click="loadAutomationDetail()">{{ t('retryLoad') }}</button>
      </div>
      <div v-else-if="automationDetail && automationDetail.item">
        <div class="hc-run-detail-grid">
          <div><span class="hc-run-detail-label">{{ t('watchSource') || 'Source' }}</span><span>{{ automationDetail.item.source_label || '-' }}</span></div>
          <div><span class="hc-run-detail-label">{{ t('watchInterval') || 'Interval' }}</span><span>{{ automationDetail.item.schedule || '-' }}</span></div>
          <div v-if="automationDetail.item.session_key"><span class="hc-run-detail-label">{{ t('watchSourceSessionKey') || 'Session Key' }}</span><span>{{ automationDetail.item.session_key }}</span></div>
          <div v-if="automationDetail.item.model"><span class="hc-run-detail-label">{{ t('model') || 'Model' }}</span><span>{{ automationDetail.item.model }}</span></div>
          <div><span class="hc-run-detail-label">{{ t('watchStatus') || 'Status' }}</span><span>{{ formatStatusText((automationDetail.item.last_execution && automationDetail.item.last_execution.status) || '') || '-' }}</span></div>
          <div><span class="hc-run-detail-label">{{ t('watchNextCheck') || 'Next Check' }}</span><span>{{ fmtTime(automationDetail.item.next_run_at) }}</span></div>
          <div><span class="hc-run-detail-label">{{ t('watchLastCheck') || 'Last Check' }}</span><span>{{ fmtTime(automationDetail.item.last_run_at) }}</span></div>
        </div>
        <div v-if="automationHasNotificationPanel(automationDetail.item)" class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('automationNotifications') || 'Notifications' }}</h4>
          <div class="hc-run-detail-grid">
            <div v-if="automationNotificationTarget(automationDetail.item)"><span class="hc-run-detail-label">{{ t('automationDelivery') || 'Delivery' }}</span><span>{{ automationNotificationTarget(automationDetail.item) }}</span></div>
            <div><span class="hc-run-detail-label">{{ t('status') || 'Status' }}</span><span><span class="hc-badge" :class="automationNotificationStatusBadge(automationDetail.item)">{{ automationNotificationStatusText(automationDetail.item) }}</span></span></div>
            <div><span class="hc-run-detail-label">{{ t('automationDeliveredToday') || 'Delivered Today' }}</span><span>{{ automationNotificationToday(automationDetail.item) }}</span></div>
            <div><span class="hc-run-detail-label">{{ t('automationDeliveredTotal') || 'Delivered Total' }}</span><span>{{ automationNotificationTotal(automationDetail.item) }}</span></div>
            <div><span class="hc-run-detail-label">{{ t('automationFailures') || 'Failures' }}</span><span>{{ automationNotificationFailures(automationDetail.item) }}</span></div>
            <div v-if="automationNotificationLastDeliveredAt(automationDetail.item)"><span class="hc-run-detail-label">{{ t('automationLastDelivered') || 'Last Delivered' }}</span><span>{{ fmtTime(automationNotificationLastDeliveredAt(automationDetail.item)) }}</span></div>
            <div v-else-if="automationNotificationLastAttemptAt(automationDetail.item)"><span class="hc-run-detail-label">{{ t('automationLastAttempt') || 'Last Attempt' }}</span><span>{{ fmtTime(automationNotificationLastAttemptAt(automationDetail.item)) }}</span></div>
          </div>
          <div v-if="automationNotificationLastError(automationDetail.item)" class="hc-result-block" style="margin-top:12px">
            <div class="hc-result-block-title">{{ t('automationLastDeliveryError') || 'Last Delivery Error' }}</div>
            <div class="hc-result-block-content" style="color:var(--danger)">{{ automationNotificationLastError(automationDetail.item) }}</div>
          </div>
        </div>
        <div v-if="automationDetail.item.prompt_preview" class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('watchPrompt') || 'Prompt' }}</h4>
          <div style="color:var(--ink3)">{{ automationDetail.item.prompt_preview }}</div>
        </div>
        <div class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('hooksResults') || 'Recent Results' }}</h4>
          <div v-if="!automationDetail.recent_executions || automationDetail.recent_executions.length === 0" style="color:var(--ink3)">{{ t('noData') || 'No data' }}</div>
          <div v-else class="hc-result-block-list">
            <div v-for="(execution, idx) in automationDetail.recent_executions" :key="'exec-w-'+idx" class="hc-result-block">
              <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap">
                <div class="hc-result-block-title">{{ fmtTime(execution.occurred_at) }}</div>
                <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                  <span class="hc-badge" :class="automationStatusBadge(execution.status)">{{ formatStatusText(execution.status) }}</span>
                  <span v-if="execution.verification_status" class="hc-badge" :class="automationStatusBadge(execution.verification_status)">{{ formatStatusText(execution.verification_status) }}</span>
                  <a v-if="automationDetailRunHref(automationDetail)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="automationDetailRunHref(automationDetail)">{{ t('automationViewReceipt') || 'View receipt' }}</a>
                </div>
              </div>
              <div v-if="execution.summary" class="hc-result-block-content">{{ execution.summary }}</div>
              <div v-if="execution.error" class="hc-result-block-content" style="color:var(--danger)">{{ execution.error }}</div>
              <div v-if="execution.verification_summary" class="hc-result-block-content">{{ execution.verification_summary }}</div>
            </div>
          </div>
        </div>
        <div v-if="automationHasResultReceipt(automationDetail)" class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('details') || 'Details' }}</h4>
          <div class="hc-result-panel">
            <div class="hc-result-panel-header">
              <div class="hc-result-panel-copy">
                <div class="hc-result-panel-title">{{ automationDetailSummary(automationDetail) || ((t('result') || 'Result') + ' ' + (t('automationReceiptSuffix') || 'receipt')) }}</div>
                <div v-if="automationDetailResult(automationDetail) && automationDetailResult(automationDetail).output" class="hc-result-panel-summary">{{ automationDetailResult(automationDetail).output }}</div>
              </div>
              <div class="hc-result-panel-badges">
                <span v-if="automationDetailOutcome(automationDetail)" class="hc-badge" :class="automationStatusBadge(automationDetailOutcome(automationDetail))">{{ formatStatusText(automationDetailOutcome(automationDetail)) }}</span>
                <span v-if="automationDetailVerificationStatus(automationDetail)" class="hc-badge" :class="automationStatusBadge(automationDetailVerificationStatus(automationDetail))">{{ formatStatusText(automationDetailVerificationStatus(automationDetail)) }}</span>
              </div>
            </div>
            <div v-if="automationDetailActions(automationDetail).length" class="hc-result-section">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationActions') || 'Actions' }}</div>
                <div class="hc-result-deliverable-meta">{{ automationDetailActions(automationDetail).length }} {{ t('automationItemCount') || 'item(s)' }}</div>
              </div>
              <div class="hc-result-action-grid">
                <div v-for="(action, idx) in automationDetailActions(automationDetail)" :key="'watch-action-'+idx" class="hc-result-action-card">
                  <div class="hc-result-action-head">
                    <div>
                      <div class="hc-result-deliverable-name">{{ automationResultActionTitle(action) }}</div>
                      <div class="hc-result-deliverable-sub">{{ action.kind || 'action' }}</div>
                    </div>
                  </div>
                  <div v-if="automationResultActionDescription(action)" class="hc-result-action-body">{{ automationResultActionDescription(action) }}</div>
                  <div class="hc-result-deliverable-actions">
                    <a v-if="automationResultActionHref(action)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="automationResultActionHref(action)" target="_blank" rel="noopener noreferrer">{{ t('automationOpenAction') || 'Open' }}</a>
                    <span v-else-if="action.target" class="hc-result-deliverable-uri">{{ action.target }}</span>
                  </div>
                </div>
              </div>
            </div>
            <div v-if="automationDetailBlocks(automationDetail).length" class="hc-result-section">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationBlocks') || 'Blocks' }}</div>
                <div class="hc-result-deliverable-meta">{{ automationDetailBlocks(automationDetail).length }} {{ t('automationItemCount') || 'item(s)' }}</div>
              </div>
              <div class="hc-result-block-list">
                <div v-for="(block, idx) in automationDetailBlocks(automationDetail)" :key="'watch-block-'+idx" class="hc-result-block">
                  <div class="hc-result-block-title">{{ block.title || formatStatusText(block.kind || 'summary') }}</div>
                  <div v-if="automationResultBlockText(block)" class="hc-result-block-content">{{ automationResultBlockText(block) }}</div>
                  <pre v-if="automationResultBlockData(block)" class="hc-run-detail-tool-args">{{ automationResultBlockData(block) }}</pre>
                </div>
              </div>
            </div>
            <div v-if="automationDetailArtifacts(automationDetail).length" class="hc-result-section">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationArtifacts') || 'Artifacts' }}</div>
                <div class="hc-result-deliverable-meta">{{ automationDetailArtifacts(automationDetail).length }} {{ t('automationItemCount') || 'item(s)' }}</div>
              </div>
              <div class="hc-result-deliverable-grid">
                <div v-for="(artifact, idx) in automationDetailArtifacts(automationDetail)" :key="'watch-artifact-'+idx" class="hc-result-deliverable-card">
                  <div class="hc-result-deliverable-head">
                    <div>
                      <div class="hc-result-deliverable-name">{{ artifact.name || artifact.label || artifact.kind || 'artifact' }}</div>
                      <div class="hc-result-deliverable-sub">{{ artifact.kind || 'artifact' }}</div>
                    </div>
                    <span v-if="artifact.size_bytes" class="hc-result-deliverable-meta">{{ formatFileSize(artifact.size_bytes) }}</span>
                  </div>
                  <div v-if="artifact.content_type" class="hc-result-deliverable-meta">{{ artifact.content_type }}</div>
                  <div v-if="artifact.preview_text" class="hc-result-deliverable-preview">{{ truncatePreview(artifact.preview_text) }}</div>
                  <div class="hc-result-deliverable-actions">
                    <a v-if="artifactPreviewPath(artifact)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="artifactPreviewPath(artifact)" target="_blank" rel="noopener noreferrer">{{ t('automationOpenPreview') || 'Open preview' }}</a>
                    <span v-else-if="artifact.uri" class="hc-result-deliverable-uri">{{ artifact.uri }}</span>
                  </div>
                </div>
              </div>
            </div>
            <div v-if="automationDetailSuggestedActions(automationDetail).length" class="hc-result-chip-row">
              <span v-for="(action, idx) in automationDetailSuggestedActions(automationDetail)" :key="'watch-suggest-'+idx" class="hc-result-chip" :title="action.reason || ''">{{ suggestedActionLabel(action) }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- ===================================================================== -->
  <!-- HOOKS TAB                                                             -->
  <!-- ===================================================================== -->
  <div v-if="tab === 'hooks'" class="hc-list-detail-layout">
    <div class="hc-list-panel">
    <div class="hc-runs-filters" style="margin-bottom:12px">
      <input class="hc-search-input" type="text" :placeholder="t('searchPlaceholder')" v-model="hookSearch" @input="hookPage=1" />
      <button class="hc-btn hc-btn-sm hc-btn-primary" data-testid="automation-hooks-add" style="margin-left:8px" @click="hookOpenCreate()">{{ t('hooksAdd') || 'Add Hook' }}</button>
    </div>

    <!-- Hook create/edit form -->
    <div v-if="hookShowForm" class="hc-card" data-testid="automation-hook-form" style="margin-bottom:16px;padding:16px;border:1px solid var(--border);border-radius:var(--radius-sm)">
      <h3 style="margin:0 0 12px 0;font-size:0.95rem">{{ hookEditingId ? (t('hooksEdit') || 'Edit Hook') : (t('hooksAdd') || 'Add Hook') }}</h3>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('hooksName') || 'Name' }} *</label>
          <input class="hc-form-input" data-testid="automation-hook-field-name" type="text" v-model="hookForm.name" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('hooksEvents') || 'Trigger' }} *</label>
          <select class="hc-form-select" data-testid="automation-hook-field-trigger" v-model="hookForm.trigger" style="width:100%" @change="hookNormalizeEventSelection()">
            <option v-for="evt in hookEventOptions" :key="evt.trigger" :value="evt.trigger">{{ evt.trigger }}{{ evt.description ? ' — ' + evt.description : '' }}</option>
          </select>
          <div v-if="selectedHookEventSpec && selectedHookEventSpec.description" style="font-size:0.76rem;color:var(--ink3);margin-top:4px">
            {{ selectedHookEventSpec.description }}<span v-if="selectedHookEventSpec.can_block"> · blocking</span><span v-if="selectedHookEventSpec.category"> · {{ selectedHookEventSpec.category }}</span>
          </div>
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('automationHookKind') || 'Kind' }}</label>
          <select class="hc-form-select" data-testid="automation-hook-field-kind" v-model="hookForm.kind" style="width:100%">
            <option value="http">HTTP</option>
            <option value="command">{{ t('automationHookCommandLabel') || 'Command' }}</option>
          </select>
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('automationHookPhase') || 'Phase' }}</label>
          <select class="hc-form-select" data-testid="automation-hook-field-phase" v-model="hookForm.phase" style="width:100%">
            <option v-for="phase in hookPhaseOptions" :key="phase.value" :value="phase.value">{{ phase.label }}</option>
          </select>
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('hooksRetryCount') || 'Retry Count' }}</label>
          <input class="hc-form-input" type="number" v-model.number="hookForm.retry_count" min="0" max="10" style="width:100%" />
        </div>
        <div>
          <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('hooksTimeout') || 'Timeout (sec)' }}</label>
          <input class="hc-form-input" type="number" v-model.number="hookForm.timeout_sec" min="1" max="300" style="width:100%" />
        </div>
      </div>
      <div v-if="hookForm.kind === 'http'" style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('hooksUrl') || 'URL' }} *</label>
        <input class="hc-form-input" data-testid="automation-hook-field-url" type="text" v-model="hookForm.url" placeholder="https://example.com/webhook" style="width:100%" />
      </div>
      <div v-if="hookForm.kind === 'http'" style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('hooksHeaders') || 'Headers' }}</label>
        <textarea class="hc-form-input" data-testid="automation-hook-field-headers" v-model="hookForm.headers_text" rows="4" :placeholder="hookHeadersPlaceholderText()" style="width:100%;resize:vertical"></textarea>
        <div style="font-size:0.76rem;color:var(--ink3);margin-top:4px">
          {{ t('hooksHeadersHelp') || 'One header per line. Use this for Authorization, routing keys, callback correlation, or vendor-required metadata.' }}
        </div>
      </div>
      <div v-else style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('automationHookCommandLabel') || 'Command' }} *</label>
        <input class="hc-form-input" type="text" v-model="hookForm.command" placeholder="echo hook-fired" style="width:100%" />
      </div>
      <div style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('automationFieldFilter') || 'Filter' }}</label>
        <input class="hc-form-input" type="text" v-model="hookForm.filter" :placeholder="t('optional') || 'Optional'" style="width:100%" />
      </div>
      <div style="margin-top:10px">
        <label style="display:block;font-size:0.82rem;margin-bottom:4px;font-weight:500">{{ t('hooksSecret') || 'Secret' }}</label>
        <input class="hc-form-input" type="password" v-model="hookForm.secret" :placeholder="t('hooksSecretPlaceholder') || 'Optional'" style="width:100%" />
      </div>
      <div style="margin-top:10px;display:flex;align-items:center;gap:12px">
        <label style="display:flex;align-items:center;gap:6px;font-size:0.82rem;cursor:pointer">
          <input type="checkbox" v-model="hookForm.enabled" /> {{ t('hooksEnabled') || 'Enabled' }}
        </label>
        <label style="display:flex;align-items:center;gap:6px;font-size:0.82rem;cursor:pointer">
          <input type="checkbox" v-model="hookForm.async" :disabled="!selectedHookEventSpec || selectedHookEventSpec.supports_async === false || hookForm.phase === 'pre'" /> {{ t('automationHookAsync') || 'Async' }}
        </label>
      </div>
      <div style="margin-top:12px;display:flex;gap:8px">
        <button class="hc-btn hc-btn-sm hc-btn-primary" data-testid="automation-hook-save" :disabled="!hookForm.name || !hookCanSave() || hookSaving" @click="hookSave()">{{ hookEditingId ? (t('save') || 'Save') : (t('create') || 'Create') }}</button>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="hookShowForm=false">{{ t('cancel') || 'Cancel' }}</button>
      </div>
    </div>

    <div v-if="hookLoading" class="hc-loading">{{ t('loading') }}</div>
    <div v-else-if="hookError" class="hc-empty">
      <div>{{ t('loadError') }}</div>
      <button class="hc-btn hc-btn-sm hc-btn-primary" style="margin-top:8px" @click="loadHooks()">{{ t('retryLoad') }}</button>
    </div>
    <div v-else-if="hookFiltered.length === 0 && hookSearch" class="hc-empty">{{ t('noResults') }}</div>
    <div v-else-if="hookFiltered.length === 0" class="hc-empty">{{ t('hooksNoItems') || 'No webhooks configured' }}</div>
    <div v-else class="hc-runs-table-wrap">
      <table class="hc-table">
        <thead><tr>
          <th>{{ t('hooksName') || 'Name' }}</th>
          <th>{{ t('hooksEvents') || 'Trigger' }}</th>
          <th>{{ t('automationHookTarget') || 'Target' }}</th>
          <th>{{ t('hooksEnabled') || 'Enabled' }}</th>
          <th>{{ t('hooksLastTriggered') || 'Last Triggered' }}</th>
          <th>{{ t('actions') }}</th>
        </tr></thead>
        <tbody>
          <tr v-for="hook in hookPaginated" :key="hook.id">
              <td>
                <strong>{{ hook.name || hook.id }}</strong>
                <div v-if="hookLastExecution(hook) && (hookLastExecution(hook).summary || hookLastExecution(hook).error)" style="font-size:0.75rem;color:var(--ink3);margin-top:4px;max-width:240px;word-break:break-word">
                  {{ truncateMessage(hookLastExecution(hook).error || hookLastExecution(hook).summary) }}
                </div>
              </td>
              <td style="font-size:0.82rem">{{ hookTriggerSummary(hook) }}</td>
              <td style="font-size:0.82rem;word-break:break-all;max-width:240px">{{ hookTargetLabel(hook) }}</td>
              <td>
                <span v-if="hook.enabled" class="hc-badge hc-badge-green">{{ t('yes') || 'Yes' }}</span>
                <span v-else class="hc-badge hc-badge-red">{{ t('no') || 'No' }}</span>
              </td>
              <td>
                <span v-if="hookLastExecution(hook)" class="hc-badge" :class="automationStatusBadge(hookLastExecution(hook).status)">{{ formatStatusText(hookLastExecution(hook).status) }}</span>
                <div style="margin-top:4px">{{ fmtTime((hookLastExecution(hook) && hookLastExecution(hook).occurred_at) || hook.updated_at) }}</div>
              </td>
              <td>
                <button class="hc-btn hc-btn-sm hc-btn-secondary" @click.stop="openAutomationDetail('hook', hook.id)">{{ t('details') || 'Details' }}</button>
                <button class="hc-btn hc-btn-sm hc-btn-secondary" @click.stop="hookOpenEdit(hook)">{{ t('edit') || 'Edit' }}</button>
                <button class="hc-btn hc-btn-sm hc-btn-danger" style="margin-left:6px" @click.stop="hookDelete(hook.id)">{{ t('delete') || 'Delete' }}</button>
              </td>
          </tr>
        </tbody>
      </table>
      <div v-if="hookTotalPages > 1" class="hc-pagination">
        <span class="hc-pagination-info">{{ (hookPage-1)*Number(hookPageSize)+1 }}-{{ Math.min(hookPage*Number(hookPageSize), hookFiltered.length) }} {{ t('of') }} {{ hookFiltered.length }}</span>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="hookPage<=1" @click="hookPage--">&#8249;</button>
        <span class="hc-pagination-pages">{{ hookPage }} / {{ hookTotalPages }}</span>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="hookPage>=hookTotalPages" @click="hookPage++">&#8250;</button>
        <select class="hc-form-select hc-pagination-size" v-model="hookPageSize" @change="hookPage=1">
          <option value="20">20</option>
          <option value="50">50</option>
          <option value="100">100</option>
        </select>
      </div>
    </div>
    </div>
    <div v-if="automationDetailVisibleForTab('hooks')" class="hc-card hc-detail-panel" style="padding:16px">
      <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:12px">
        <div>
          <div style="font-size:1rem;font-weight:600">{{ (automationDetail && automationDetail.item && automationDetail.item.name) || (t('details') || 'Details') }}</div>
          <div style="font-size:0.82rem;color:var(--ink3);margin-top:4px">{{ (automationDetail && automationDetail.item && automationDetail.item.source_label) || '' }}</div>
        </div>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="closeAutomationDetail()">{{ t('close') || 'Close' }}</button>
      </div>
      <div v-if="automationDetailLoading" class="hc-loading">{{ t('loading') }}</div>
      <div v-else-if="automationDetailError" class="hc-empty">
        <div>{{ t('loadError') }}</div>
        <button class="hc-btn hc-btn-sm hc-btn-primary" style="margin-top:8px" @click="loadAutomationDetail()">{{ t('retryLoad') }}</button>
      </div>
      <div v-else-if="automationDetail && automationDetail.item">
        <div class="hc-run-detail-grid">
          <div><span class="hc-run-detail-label">{{ t('hooksEvents') || 'Trigger' }}</span><span>{{ automationDetail.item.schedule || '-' }}</span></div>
          <div><span class="hc-run-detail-label">{{ t('automationHookKind') || 'Kind' }}</span><span>{{ formatStatusText(automationDetail.item.source_kind) }}</span></div>
          <div><span class="hc-run-detail-label">{{ t('automationHookTarget') || 'Target' }}</span><span>{{ automationDetail.item.source_label || '-' }}</span></div>
          <div><span class="hc-run-detail-label">{{ t('hooksEnabled') || 'Enabled' }}</span><span>{{ automationDetail.item.enabled ? (t('yes') || 'Yes') : (t('no') || 'No') }}</span></div>
          <div v-if="automationDetail.item.last_run_at"><span class="hc-run-detail-label">{{ t('hooksLastTriggered') || 'Last Triggered' }}</span><span>{{ fmtTime(automationDetail.item.last_run_at) }}</span></div>
        </div>
        <div class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('hooksResults') || 'Recent Results' }}</h4>
          <div v-if="!automationDetail.recent_executions || automationDetail.recent_executions.length === 0" style="color:var(--ink3)">{{ t('noData') || 'No data' }}</div>
          <div v-else class="hc-result-block-list">
            <div v-for="(execution, idx) in automationDetail.recent_executions" :key="'exec-h-'+idx" class="hc-result-block">
              <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap">
                <div class="hc-result-block-title">{{ fmtTime(execution.occurred_at) }}</div>
                <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                  <span class="hc-badge" :class="automationStatusBadge(execution.status)">{{ formatStatusText(execution.status) }}</span>
                  <a v-if="automationExecutionRunHref(execution)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="automationExecutionRunHref(execution)">{{ t('automationViewReceipt') || 'View receipt' }}</a>
                </div>
              </div>
              <div v-if="execution.tool_name || execution.session_id || execution.target_label" class="hc-result-block-content" style="color:var(--ink3)">
                <span v-if="execution.tool_name">{{ t('automationHookTool') || 'Tool' }}: {{ execution.tool_name }}</span>
                <template v-if="execution.session_id">
                  <span v-if="execution.tool_name"> · </span>
                  <span>{{ t('automationHookSession') || 'Session' }}: {{ execution.session_id }}</span>
                </template>
                <template v-if="execution.target_label">
                  <span v-if="execution.tool_name || execution.session_id"> · </span>
                  <span>{{ t('automationHookTargetLabel') || 'Target' }}: {{ execution.target_label }}</span>
                </template>
              </div>
              <div v-if="execution.summary" class="hc-result-block-content">{{ execution.summary }}</div>
              <div v-if="execution.error" class="hc-result-block-content" style="color:var(--danger)">{{ execution.error }}</div>
            </div>
          </div>
        </div>
        <div v-if="automationHasResultReceipt(automationDetail)" class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('details') || 'Details' }}</h4>
          <div class="hc-result-panel">
            <div class="hc-result-panel-header">
              <div class="hc-result-panel-copy">
                <div class="hc-result-panel-title">{{ automationDetailSummary(automationDetail) || ((t('result') || 'Result') + ' ' + (t('automationReceiptSuffix') || 'receipt')) }}</div>
                <div v-if="automationDetailResult(automationDetail) && automationDetailResult(automationDetail).output" class="hc-result-panel-summary">{{ automationDetailResult(automationDetail).output }}</div>
              </div>
              <div class="hc-result-panel-badges">
                <span v-if="automationDetailOutcome(automationDetail)" class="hc-badge" :class="automationStatusBadge(automationDetailOutcome(automationDetail))">{{ formatStatusText(automationDetailOutcome(automationDetail)) }}</span>
                <span v-if="automationDetailVerificationStatus(automationDetail)" class="hc-badge" :class="automationStatusBadge(automationDetailVerificationStatus(automationDetail))">{{ formatStatusText(automationDetailVerificationStatus(automationDetail)) }}</span>
              </div>
            </div>
            <div v-if="automationDetailActions(automationDetail).length" class="hc-result-section">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationActions') || 'Actions' }}</div>
                <div class="hc-result-deliverable-meta">{{ automationDetailActions(automationDetail).length }} {{ t('automationItemCount') || 'item(s)' }}</div>
              </div>
              <div class="hc-result-action-grid">
                <div v-for="(action, idx) in automationDetailActions(automationDetail)" :key="'hook-action-'+idx" class="hc-result-action-card">
                  <div class="hc-result-action-head">
                    <div>
                      <div class="hc-result-deliverable-name">{{ automationResultActionTitle(action) }}</div>
                      <div class="hc-result-deliverable-sub">{{ action.kind || 'action' }}</div>
                    </div>
                  </div>
                  <div v-if="automationResultActionDescription(action)" class="hc-result-action-body">{{ automationResultActionDescription(action) }}</div>
                  <div class="hc-result-deliverable-actions">
                    <a v-if="automationResultActionHref(action)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="automationResultActionHref(action)" target="_blank" rel="noopener noreferrer">{{ t('automationOpenAction') || 'Open' }}</a>
                    <span v-else-if="action.target" class="hc-result-deliverable-uri">{{ action.target }}</span>
                  </div>
                </div>
              </div>
            </div>
            <div v-if="automationDetailBlocks(automationDetail).length" class="hc-result-section">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationBlocks') || 'Blocks' }}</div>
                <div class="hc-result-deliverable-meta">{{ automationDetailBlocks(automationDetail).length }} {{ t('automationItemCount') || 'item(s)' }}</div>
              </div>
              <div class="hc-result-block-list">
                <div v-for="(block, idx) in automationDetailBlocks(automationDetail)" :key="'hook-block-'+idx" class="hc-result-block">
                  <div class="hc-result-block-title">{{ block.title || formatStatusText(block.kind || 'summary') }}</div>
                  <div v-if="automationResultBlockText(block)" class="hc-result-block-content">{{ automationResultBlockText(block) }}</div>
                  <pre v-if="automationResultBlockData(block)" class="hc-run-detail-tool-args">{{ automationResultBlockData(block) }}</pre>
                </div>
              </div>
            </div>
            <div v-if="automationDetailArtifacts(automationDetail).length" class="hc-result-section">
              <div class="hc-result-section-head">
                <div class="hc-result-block-title">{{ t('automationArtifacts') || 'Artifacts' }}</div>
                <div class="hc-result-deliverable-meta">{{ automationDetailArtifacts(automationDetail).length }} {{ t('automationItemCount') || 'item(s)' }}</div>
              </div>
              <div class="hc-result-deliverable-grid">
                <div v-for="(artifact, idx) in automationDetailArtifacts(automationDetail)" :key="'hook-artifact-'+idx" class="hc-result-deliverable-card">
                  <div class="hc-result-deliverable-head">
                    <div>
                      <div class="hc-result-deliverable-name">{{ artifact.name || artifact.label || artifact.kind || 'artifact' }}</div>
                      <div class="hc-result-deliverable-sub">{{ artifact.kind || 'artifact' }}</div>
                    </div>
                    <span v-if="artifact.size_bytes" class="hc-result-deliverable-meta">{{ formatFileSize(artifact.size_bytes) }}</span>
                  </div>
                  <div v-if="artifact.content_type" class="hc-result-deliverable-meta">{{ artifact.content_type }}</div>
                  <div v-if="artifact.preview_text" class="hc-result-deliverable-preview">{{ truncatePreview(artifact.preview_text) }}</div>
                  <div class="hc-result-deliverable-actions">
                    <a v-if="artifactPreviewPath(artifact)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="artifactPreviewPath(artifact)" target="_blank" rel="noopener noreferrer">{{ t('automationOpenPreview') || 'Open preview' }}</a>
                    <span v-else-if="artifact.uri" class="hc-result-deliverable-uri">{{ artifact.uri }}</span>
                  </div>
                </div>
              </div>
            </div>
            <div v-if="automationDetailSuggestedActions(automationDetail).length" class="hc-result-chip-row">
              <span v-for="(action, idx) in automationDetailSuggestedActions(automationDetail)" :key="'hook-suggest-'+idx" class="hc-result-chip" :title="action.reason || ''">{{ suggestedActionLabel(action) }}</span>
            </div>
          </div>
        </div>
        <div v-if="automationDetailVerificationChecks(automationDetail).length" class="hc-run-detail-section" style="margin-top:12px">
          <h4>{{ t('automationVerification') || 'Verification' }}</h4>
          <div class="hc-verification-checks">
            <div v-for="check in automationDetailVerificationChecks(automationDetail)" :key="check.name" class="hc-verification-check">
              <div class="hc-verification-check-header">
                <strong>{{ check.name }}</strong>
                <span class="hc-badge" :class="automationStatusBadge(check.status)">{{ formatStatusText(check.status) }}</span>
                <span v-if="check.domain" style="color:var(--ink3)">{{ check.domain }}</span>
                <span v-if="check.requirement" class="hc-badge hc-badge-gray">{{ formatStatusText(check.requirement) }}</span>
              </div>
              <div v-if="check.summary" class="hc-verification-check-summary">{{ check.summary }}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</div>
`;

// ---------------------------------------------------------------------------
// Automation View component
// ---------------------------------------------------------------------------

export function AutomationView() {
  const store = window._hcStore;
  let _mounted = false;
  let watchRefreshTimer = null;
  let boundRouteSync = null;

  // Track which tabs have been loaded to enable lazy loading
  const tabLoaded = { agents: false, schedules: false, watch: false, hooks: false };

  function automationRouteState() {
    const hash = String((store && store.route) || window.location.hash || '').trim();
    const [pathPart, queryPart = ''] = hash.split('?');
    return {
      hash,
      path: pathPart || '#/automation',
      query: new URLSearchParams(queryPart),
    };
  }

  function resolveAutomationTab() {
    const route = automationRouteState();
    const sub = route.path.replace('#/automation', '').replace(/^\//, '').trim();
    if (sub === 'schedules' || sub === 'watch' || sub === 'hooks' || sub === 'agents') return sub;
    return 'schedules';
  }

  async function loadAutomationTab(view, name, options = {}) {
    const force = options && options.force === true;
    if (!force && tabLoaded[name]) return;
    tabLoaded[name] = true;
    if (name === 'agents') {
      await view.loadAgents();
      return;
    }
    if (name === 'schedules') {
      await view.loadSchedules();
      return;
    }
    if (name === 'watch') {
      await view.loadWatch();
      return;
    }
    if (name === 'hooks') {
      await view.loadHooks();
    }
  }

  const starterTemplatesSection = buildAutomationStarterTemplatesSection({
    api,
    showToast,
    t,
    store,
    isMounted: () => _mounted,
    routeState: automationRouteState,
    templateKindsForTab,
    templateTabForKind,
    templateDefaults,
    templateWizardFieldList,
    templateValuePresent,
    templateCanQuickCreate,
    buildHookEventOptions,
    findHookEventSpec,
    buildHookPhaseOptions,
    selectOptionValue,
    selectOptionLabel,
    selectOptionsIncludeValue,
    normalizeHookConfig,
    formatStatusText,
    defaultNewWatch,
    buildCreateSource,
  });
  const watchSection = buildAutomationWatchSection({
    api,
    showToast,
    t,
    defaultPageSize: DEFAULT_PAGE_SIZE,
    isMounted: () => _mounted,
    defaultNewWatch,
    buildCreateSource,
    watchItemID,
    watchToForm,
    sourceLabel,
  });
  const schedulesSection = buildAutomationSchedulesSection({
    api,
    showToast,
    t,
    defaultPageSize: DEFAULT_PAGE_SIZE,
    isMounted: () => _mounted,
  });

  const view = {
    $template: TEMPLATE,
    t,

    // Shared helpers exposed to template
    fmtTime,
    fmtRelative,
    truncate,
    truncateBody,
    truncateMessage,
    sourceLabel,
    scheduleExecution,
    automationStatusBadge,
    hookTriggerSummary,
    hookTargetLabel,
    watchStatusBadge,
    verificationBadge,
    scheduleStr,
    jobEnabled,
    statusCodeClass,
    formatStatusText,
    formatFileSize,
    truncatePreview,
    prettyData,
    suggestedActionLabel,
    artifactPreviewPath,
    automationDetailResult,
    automationDetailOutcome,
    automationDetailSummary,
    automationDetailBlocks,
    automationDetailArtifacts,
    automationDetailActions,
    automationDetailSuggestedActions,
    automationDetailVerification,
    automationDetailVerificationChecks,
    automationDetailVerificationStatus,
    automationDetailVerificationSummary,
    automationHasResultReceipt,
    automationDetailRunHref,
    automationExecutionRunHref,
    automationHookPayloadPreview,
    automationResultBlockText,
    automationResultBlockData,
    automationResultActionTitle,
    automationResultActionDescription,
    automationResultActionHref,
    automationNotificationStatusBadge,
    automationNotificationStatusText,
    automationNotificationTarget,
    automationNotificationToday,
    automationNotificationTotal,
    automationNotificationFailures,
    automationNotificationLastAttemptAt,
    automationNotificationLastDeliveredAt,
    automationNotificationLastError,
    automationHasNotificationPanel,
    automationNotificationBlurb,
    templateKindLabel,
    templateKindBadge,

    refreshAutomationTab() {
      if (this.tab === 'agents') return this.loadAgents();
      if (this.tab === 'schedules') return this.loadSchedules();
      if (this.tab === 'watch') return this.loadWatch();
      if (this.tab === 'hooks') return this.loadHooks();
      return null;
    },

    automationMetrics() {
      const watchAttention = this.watchItems.filter(item =>
        automationNeedsAttention(watchExecutionStatus(item)) || automationNeedsAttention(watchVerificationStatus(item))
      ).length;
      const hookAttention = this.hookItems.filter(hook =>
        automationNeedsAttention(hookLastExecution(hook) && hookLastExecution(hook).status)
      ).length;
      const scheduleAttention = this.schedMerged.filter(item =>
        automationNeedsAttention(scheduleExecution(item) && scheduleExecution(item).status) ||
        automationNeedsAttention(scheduleExecution(item) && scheduleExecution(item).verification_status)
      ).length;
      const totalAutomations = this.schedTotalCount + this.watchItems.length + this.hookItems.length;
      const activeAutomations = this.schedMerged.filter(item => item._type === 'cron' ? jobEnabled(item) : item.enabled).length +
        this.watchItems.filter(item => item.enabled).length +
        this.hookItems.filter(item => item.enabled).length;
      const recentReceipts = this.schedMerged.filter(item => !!scheduleExecution(item)).length +
        this.watchItems.filter(item => !!watchExecution(item)).length +
        this.hookItems.filter(item => !!hookLastExecution(item)).length;
      const attentionCount = scheduleAttention + watchAttention + hookAttention;
      const notifications = aggregateNotificationSummary(this.schedMerged.concat(this.watchItems, this.hookItems));
      return [
        {
          label: t('automationTitle') || 'Automation',
          value: totalAutomations,
          note: t('automationMetricManagementNote') || 'Schedules, watch jobs, and hooks under active management',
        },
        {
          label: t('automationMetricEnabled') || 'Enabled',
          value: activeAutomations,
          note: t('automationMetricEnabledNote') || 'Enabled automation entries ready to trigger',
        },
        {
          label: t('automationMetricDeliveredToday') || 'Delivered Today',
          value: notifications.todayCount,
          note: notifications.trackedCount > 0
            ? (t('automationMetricTrackedNote') || `${notifications.trackedCount} direct-delivery automations are being tracked`).replace('{count}', notifications.trackedCount)
            : (t('automationMetricNoTracked') || 'No tracked direct-delivery automations yet'),
        },
        {
          label: t('automationMetricDeliveryFailures') || 'Delivery Failures',
          value: notifications.failureCount,
          note: notifications.failureCount > 0
            ? (notifications.attentionCount > 0
              ? (t('automationMetricFailureActive') || `${notifications.attentionCount} automations are currently failing delivery`).replace('{count}', notifications.attentionCount)
              : (t('automationMetricFailureRecovered') || 'Failures were recorded previously, but the latest attempts recovered'))
            : (t('automationMetricNoDeliveryFailures') || 'No failed deliveries recorded'),
        },
        {
          label: t('automationMetricReceipts') || 'Receipts',
          value: recentReceipts,
          note: t('automationMetricReceiptsNote') || 'Entries with recent execution evidence attached',
        },
        {
          label: t('automationMetricAttention') || 'Attention',
          value: attentionCount,
          note: attentionCount > 0 ? (t('automationMetricAttentionNeeded') || 'Failures or warnings need operator review') : (t('automationMetricNoFailures') || 'No recent failures detected'),
        },
      ];
    },

    ...starterTemplatesSection,

    // ---------------------------------------------------------------------------
    // Tab management
    // ---------------------------------------------------------------------------

    tab: resolveAutomationTab(),

    switchTab(name) {
      this.tab = name;
      window.location.hash = '#/automation/' + name;
      loadAutomationTab(this, name);
    },

    // =========================================================================
    // AGENTS STATE
    // =========================================================================

    agItems: [],
    agLoading: false,
    agError: false,
    agSearch: '',
    agPage: 1,
    agPageSize: DEFAULT_PAGE_SIZE,
    agExpanded: null,
    agDetail: null,
    agDetailLoading: false,
    agShowEdit: false,
    agSaving: false,
    agEditForm: {
      system_prompt: '',
      default_model: '',
      triage_model: '',
      max_tool_rounds: 0,
      queue_mode: 'enqueue',
      dedupe_window: '',
      default_context_window: 0,
    },
    promptPreviewLen: SYSTEM_PROMPT_PREVIEW_LEN,

    automationDetail: null,
    automationDetailLoading: false,
    automationDetailError: false,
    automationDetailKind: '',
    automationDetailID: '',
    hookDebugRunning: false,
    hookDebugTrigger: '',
    hookDebugPhase: '',
    hookDebugPayload: '{\n  "run_id": "run-demo",\n  "session_id": "sess-demo",\n  "tool_name": "notify.send"\n}',
    hookDebugLastResult: null,

    get agFiltered() {
      const q = (this.agSearch || '').toLowerCase().trim();
      if (!q) return this.agItems;
      return this.agItems.filter(a => {
        const name = (a.name || a.Name || '').toLowerCase();
        return name.includes(q);
      });
    },

    get agTotalPages() {
      return Math.max(1, Math.ceil(this.agFiltered.length / Number(this.agPageSize)));
    },

    get agPaginated() {
      const start = (this.agPage - 1) * Number(this.agPageSize);
      return this.agFiltered.slice(start, start + Number(this.agPageSize));
    },

    agName(a) {
      return a.name || a.Name || '-';
    },

    agSystemPrompt(a) {
      const agent = a.agent || a;
      return agent.system_prompt || agent.SystemPrompt || '';
    },

    automationDetailVisibleForTab(tab) {
      const detail = this.automationDetail;
      const kind = (detail && detail.item && detail.item.kind) || this.automationDetailKind || '';
      if (tab === 'schedules') return kind === 'cron' || kind === 'wakeup';
      if (tab === 'watch') return kind === 'watch';
      if (tab === 'hooks') return kind === 'hook';
      return false;
    },

    closeAutomationDetail() {
      this.automationDetail = null;
      this.automationDetailLoading = false;
      this.automationDetailError = false;
      this.automationDetailKind = '';
      this.automationDetailID = '';
      this.hookDebugLastResult = null;
    },

    async openAutomationDetail(kind, id) {
      this.automationDetailKind = String(kind || '').trim();
      this.automationDetailID = String(id || '').trim();
      await this.loadAutomationDetail();
    },

    async loadAutomationDetail() {
      if (!this.automationDetailKind || !this.automationDetailID) return;
      this.automationDetailLoading = true;
      this.automationDetailError = false;
      try {
        const data = await api.get('/operator/automation/items/' + encodeURIComponent(this.automationDetailKind) + '/' + encodeURIComponent(this.automationDetailID));
        if (!_mounted) return;
        this.automationDetail = data || null;
        if (this.automationDetailVisibleForTab('hooks')) this.hookSyncDebugPayload();
      } catch (_) {
        if (!_mounted) return;
        this.automationDetailError = true;
        this.automationDetail = null;
      }
      if (_mounted) this.automationDetailLoading = false;
    },

    agTools(a) {
      const agent = a.agent || a;
      return agent.tools || agent.Tools || [];
    },

    agSkills(a) {
      const agent = a.agent || a;
      return agent.skills || agent.Skills || [];
    },

    agToggle(name) {
      if (this.agExpanded === name) {
        this.agExpanded = null;
        this.agDetail = null;
      } else {
        this.agExpanded = name;
        this.agLoadDetail(name);
      }
    },

    async agLoadDetail(name) {
      this.agDetailLoading = true;
      this.agDetail = null;
      try {
        const data = await api.get('/operator/agents/' + encodeURIComponent(name));
        if (!_mounted) return;
        this.agDetail = data;
      } catch (_) {
        if (_mounted) this.agDetail = null;
      }
      if (_mounted) this.agDetailLoading = false;
    },

    async loadAgents() {
      this.agLoading = true;
      this.agError = false;
      try {
        const data = await api.get('/operator/agents');
        if (!_mounted) return;
        this.agItems = Array.isArray(data) ? data : (data.items || data.agents || []);
      } catch (_) {
        if (_mounted) this.agError = true;
      }
      if (_mounted) this.agLoading = false;
    },

    async agOpenEdit() {
      try {
        const cfg = await api.get('/operator/config/agent');
        this.agEditForm = {
          system_prompt: cfg.system_prompt || '',
          default_model: cfg.default_model || '',
          triage_model: cfg.triage_model || '',
          max_tool_rounds: cfg.max_tool_rounds || 0,
          queue_mode: cfg.queue_mode || 'enqueue',
          dedupe_window: cfg.dedupe_window || '',
          default_context_window: cfg.default_context_window || 0,
        };
      } catch (_) {
        this.agEditForm = {
          system_prompt: '',
          default_model: '',
          triage_model: '',
          max_tool_rounds: 0,
          queue_mode: 'enqueue',
          dedupe_window: '',
          default_context_window: 0,
        };
      }
      this.agShowEdit = true;
    },

    async agSaveConfig() {
      this.agSaving = true;
      try {
        await api.put('/operator/config/agent', this.agEditForm, { errorToast: false });
        showToast(t('agentsSaved') || 'Agent config saved', 'success');
        this.agShowEdit = false;
        this.loadAgents();
      } catch (err) {
        showToast((err && err.message) || (t('loadError') || 'Failed to save'), 'error');
      }
      this.agSaving = false;
    },

    // =========================================================================
    // SCHEDULES STATE (Cron + Wakeup merged)
    // =========================================================================

    ...schedulesSection,

    // =========================================================================
    // WATCH STATE
    // =========================================================================

    ...watchSection,

    // =========================================================================
    // HOOKS STATE
    // =========================================================================

    hookItems: [],
    hookLoading: false,
    hookError: false,
    hookSearch: '',
    hookPage: 1,
    hookPageSize: DEFAULT_PAGE_SIZE,
    hookShowForm: false,
    hookSaving: false,
    hookEditingId: null,
    hookForm: { name: '', trigger: 'run.completed', kind: 'http', phase: 'post', url: '', command: '', headers_text: '', filter: '', secret: '', retry_count: 3, timeout_sec: 30, async: false, enabled: true },
    hookEventsCatalog: [],

    get hookFiltered() {
      const q = (this.hookSearch || '').toLowerCase().trim();
      if (!q) return this.hookItems;
      return this.hookItems.filter(hook => {
        const name = (hook.name || hook.id || '').toLowerCase();
        return name.includes(q);
      });
    },

    get hookTotalPages() {
      return Math.max(1, Math.ceil(this.hookFiltered.length / Number(this.hookPageSize)));
    },

    get hookPaginated() {
      const start = (this.hookPage - 1) * Number(this.hookPageSize);
      return this.hookFiltered.slice(start, start + Number(this.hookPageSize));
    },

    get hookEventOptions() {
      return buildHookEventOptions(this.hookEventsCatalog, this.hookForm && this.hookForm.trigger);
    },

    get selectedHookEventSpec() {
      return findHookEventSpec(this.hookEventsCatalog, this.hookForm && this.hookForm.trigger);
    },

    get hookPhaseOptions() {
      return buildHookPhaseOptions(this.selectedHookEventSpec);
    },

    hookHeadersPlaceholderText() {
      return t('hooksHeadersPlaceholder') || 'Authorization: Bearer <token>\nX-Route-Key: prod';
    },

	    hookOpenCreate() {
	      this.hookEditingId = null;
	      this.hookForm = { name: '', trigger: 'run.completed', kind: 'http', phase: 'post', url: '', command: '', headers_text: '', filter: '', secret: '', retry_count: 3, timeout_sec: 30, async: false, enabled: true };
	      this.hookNormalizeEventSelection();
	      this.hookShowForm = true;
	    },

    hookOpenEdit(hook) {
      this.hookEditingId = hook.id;
      this.hookForm = {
        name: hook.name || '',
        trigger: hook.trigger || 'run.completed',
	        kind: hook.kind || 'http',
	        phase: hook.phase || 'post',
	        url: hook.url || '',
	        command: hook.command || '',
	        headers_text: hookHeadersMapToText(hook.headers),
	        filter: hook.filter || '',
	        secret: '',
	        retry_count: hook.retry_count != null ? hook.retry_count : 3,
        timeout_sec: hook.timeout_sec != null ? hook.timeout_sec : (hook.timeout != null ? hook.timeout : 30),
        async: hook.async === true,
        enabled: hook.enabled !== false,
      };
      this.hookNormalizeEventSelection();
      this.hookShowForm = true;
    },

    hookNormalizeEventSelection() {
      this.hookForm = normalizeHookConfig(this.hookForm, this.hookEventsCatalog);
    },

    hookCanSave() {
      if (!this.hookForm.name || !this.hookForm.trigger || !this.hookForm.kind) return false;
      if (!this.hookPhaseOptions.some(option => option.value === this.hookForm.phase)) return false;
      if (this.hookForm.kind === 'http') return !!this.hookForm.url;
      if (this.hookForm.kind === 'command') return !!this.hookForm.command;
      return false;
    },

    hookDefaultDebugPayload() {
      const detail = this.automationDetail || {};
      const preview = detail.latest_payload_preview;
      if (preview && typeof preview === 'object') return prettyData(preview);
      return '{\n  "run_id": "run-demo",\n  "session_id": "sess-demo",\n  "tool_name": "notify.send"\n}';
    },

    hookCurrentItem() {
      const id = String(this.automationDetailID || '').trim();
      return this.hookItems.find(hook => String(hook.id || '').trim() === id) || null;
    },

    hookSyncDebugPayload() {
      if (this.automationDetailKind !== 'hook') return;
      const hook = this.hookCurrentItem();
      this.hookDebugTrigger = hook && hook.trigger ? String(hook.trigger) : '';
      this.hookDebugPhase = hook && hook.phase ? String(hook.phase) : '';
      this.hookDebugPayload = this.hookDefaultDebugPayload();
    },

    hookResetDebugPayload() {
      this.hookDebugPayload = this.hookDefaultDebugPayload();
    },

    hookParseDebugPayload() {
      const raw = String(this.hookDebugPayload || '').trim();
      if (!raw) return {};
      try {
        const parsed = JSON.parse(raw);
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
          showToast(t('automationPayloadMustBeObject') || 'Payload must be a JSON object', 'error');
          return null;
        }
        return parsed;
      } catch (_) {
        showToast(t('automationPayloadMustBeJSON') || 'Payload must be valid JSON', 'error');
        return null;
      }
    },

    async hookTestFire() {
      if (!this.automationDetailID || this.hookDebugRunning) return;
      const payload = this.hookParseDebugPayload();
      if (payload == null) return;
      this.hookDebugRunning = true;
      try {
        const body = { payload };
        if (this.hookDebugTrigger) body.trigger = this.hookDebugTrigger;
        if (this.hookDebugPhase) body.phase = this.hookDebugPhase;
        const data = await api.post('/operator/hooks/' + encodeURIComponent(this.automationDetailID) + '/fire', body);
        this.hookDebugLastResult = data && data.result ? data.result : null;
        showToast(t('automationHookTestFired') || 'Hook test fired', 'success');
        await this.loadHooks();
        await this.loadAutomationDetail();
      } catch (_) {}
      this.hookDebugRunning = false;
    },

    async hookReplayLast() {
      if (!this.automationDetailID || this.hookDebugRunning) return;
      this.hookDebugRunning = true;
      try {
        const data = await api.post('/operator/hooks/' + encodeURIComponent(this.automationDetailID) + '/replay', {});
        this.hookDebugLastResult = data && data.result ? data.result : null;
        showToast(t('automationHookReplayCompleted') || 'Hook replay completed', 'success');
        await this.loadHooks();
        await this.loadAutomationDetail();
      } catch (_) {}
      this.hookDebugRunning = false;
    },

	    async hookSave() {
	      if (!this.hookCanSave()) return;
	      let parsedHeaders = {};
	      if (this.hookForm.kind === 'http') {
	        const parsed = parseHookHeadersText(this.hookForm.headers_text);
	        if (parsed.error) {
	          showToast(parsed.error, 'error');
	          return;
	        }
	        parsedHeaders = parsed.headers || {};
	      }
	      this.hookSaving = true;
	      try {
	        const body = {
	          name: this.hookForm.name,
          trigger: this.hookForm.trigger,
          kind: this.hookForm.kind,
          phase: this.hookForm.phase,
          retry_count: this.hookForm.retry_count,
          timeout: this.hookForm.timeout_sec,
          async: this.hookForm.async,
	          enabled: this.hookForm.enabled,
	        };
	        if (this.hookForm.kind === 'http') {
	          body.url = this.hookForm.url;
	          body.headers = parsedHeaders;
	        }
        if (this.hookForm.kind === 'command') body.command = this.hookForm.command;
        if (this.hookForm.filter) body.filter = this.hookForm.filter;
        if (this.hookForm.secret) body.secret = this.hookForm.secret;
        if (this.hookEditingId) {
          await api.patch('/operator/hooks/' + encodeURIComponent(this.hookEditingId), body, { errorToast: false });
          showToast(t('hooksSaved') || 'Hook updated', 'success');
        } else {
          await api.post('/operator/hooks', body, { errorToast: false });
          showToast(t('hooksCreateSuccess') || 'Hook created', 'success');
        }
        this.hookShowForm = false;
        this.hookEditingId = null;
        this.loadHooks();
      } catch (err) {
        showToast((err && err.message) || (t('hooksSaveError') || 'Failed to save hook'), 'error');
      }
      this.hookSaving = false;
    },

    async hookDelete(id) {
      if (!confirm(t('hooksConfirmDelete') || 'Delete this webhook?')) return;
      try {
        await api.del('/operator/hooks/' + encodeURIComponent(id), { errorToast: false });
        showToast(t('hooksDeleted') || 'Hook deleted', 'success');
        if (this.automationDetailKind === 'hook' && this.automationDetailID === id) this.closeAutomationDetail();
        this.loadHooks();
      } catch (err) {
        showToast((err && err.message) || (t('hooksDeleteError') || 'Failed to delete hook'), 'error');
      }
    },

    async loadHooks() {
      this.hookLoading = true;
      this.hookError = false;
      try {
        const [rawData, automationData, hookEvents] = await Promise.all([
          api.get('/operator/hooks'),
          api.get('/operator/automation/items?kinds=hook').catch(() => ({ items: [] })),
          api.get('/operator/hooks/events').catch(() => ({ items: [] })),
        ]);
        if (!_mounted) return;
        const hooks = Array.isArray(rawData) ? rawData : (rawData.items || []);
        const automationItems = Array.isArray(automationData.items) ? automationData.items : [];
        const automationByID = new Map(automationItems.map(item => [item.id, item]));
        this.hookEventsCatalog = normalizeHookEvents(hookEvents);
        this.hookItems = hooks.map(hook => Object.assign({}, hook, automationByID.get(hook.id) || {}));
        this.hookNormalizeEventSelection();
        if (this.templateWizardOpen && this.templateWizardTemplate && String(this.templateWizardTemplate.kind || '').trim() === 'hook') {
          this.templateWizardValues = normalizeHookConfig(this.templateWizardValues, this.hookEventsCatalog);
        }
      } catch (_) {
        if (_mounted) this.hookError = true;
      }
      if (_mounted && this.automationDetailVisibleForTab('hooks') && this.automationDetailKind && this.automationDetailID) {
        this.loadAutomationDetail();
      }
      if (_mounted) this.hookLoading = false;
    },

    // =========================================================================
    // Lifecycle
    // =========================================================================

    mounted() {
      _mounted = true;
      this.tab = resolveAutomationTab();
      boundRouteSync = () => {
        if (!_mounted) return;
        const tab = resolveAutomationTab();
        this.tab = tab;
        loadAutomationTab(this, tab);
        this.consumeRouteTemplateSelection();
      };
      window.addEventListener('hashchange', boundRouteSync);
      tabLoaded.agents = false;
      tabLoaded.schedules = false;
      tabLoaded.watch = false;
      tabLoaded.hooks = false;
      this.loadStarterTemplates();
      loadAutomationTab(this, this.tab);
      // Start watch auto-refresh timer (runs only when watch tab is active)
      watchRefreshTimer = setInterval(() => {
        if (_mounted && this.tab === 'watch') this.loadWatch();
      }, AUTO_REFRESH_MS);
    },

    unmounted() {
      _mounted = false;
      if (boundRouteSync) {
        window.removeEventListener('hashchange', boundRouteSync);
        boundRouteSync = null;
      }
      if (watchRefreshTimer) {
        clearInterval(watchRefreshTimer);
        watchRefreshTimer = null;
      }
    },
  };

  return mergeSectionDescriptors(
    view,
    starterTemplatesSection,
    schedulesSection,
    watchSection,
  );
}
