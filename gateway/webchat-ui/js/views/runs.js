// ---------------------------------------------------------------------------
// Runs View - Petite Vue component
// ---------------------------------------------------------------------------

import { api, getToken, showToast } from '../api.js';
import { t } from '../i18n/index.js';
import { artifactPreviewPath, parseArtifactID, safeExternalURL } from '../linking.js';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const AUTO_REFRESH_MS = 5000;
const SSE_RECONNECT_MS = 5000;
const DEFAULT_PAGE_SIZE = 20;
const PREVIEW_TEXT_LIMIT = 220;
const RUN_ROUTE_PREFIX = '#/runs/';
const SSE_RESPONSE_LIMIT = 512 * 1024;

const STATUS_CLASSES = {
  queued: 'hc-badge-gray',
  waiting_input: 'hc-badge-orange',
  needs_confirmation: 'hc-badge-orange',
  running: 'hc-badge-blue hc-badge-pulse',
  waiting_approval: 'hc-badge-orange',
  completed: 'hc-badge-green',
  partial: 'hc-badge-orange',
  completed_warning: 'hc-badge-orange',
  verification_failed: 'hc-badge-red',
  failed: 'hc-badge-red',
  cancelled: 'hc-badge-gray',
  in_progress: 'hc-badge-blue hc-badge-pulse',
};

const PREFLIGHT_CLASSES = {
  ready: 'hc-badge-green',
  auto_preparing: 'hc-badge-blue',
  needs_confirmation: 'hc-badge-orange',
};

const VERIFICATION_CLASSES = {
  passed: 'hc-badge-green',
  warning: 'hc-badge-orange',
  failed: 'hc-badge-red',
  skipped: 'hc-badge-gray',
};

function suggestedActionLabels() {
  return {
    open_deliverables: t('runsSuggestedOpenDeliverables') || 'Open deliverables',
    review_result: t('runsSuggestedReviewResult') || 'Review result',
    retry_run: t('runsSuggestedRetryRun') || 'Retry run',
    review_approval: t('runsSuggestedReviewApproval') || 'Review approval',
    provide_input: t('runsSuggestedProvideInput') || 'Provide input',
    inspect_verification: t('runsSuggestedInspectVerification') || 'Inspect verification',
  };
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function fmtTime(iso) {
  try { return new Date(iso).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }); }
  catch (_) { return ''; }
}

function fmtDuration(startIso, endIso) {
  if (!startIso) return '-';
  const start = new Date(startIso).getTime();
  const end = endIso ? new Date(endIso).getTime() : Date.now();
  const ms = end - start;
  if (ms < 1000) return ms + 'ms';
  if (ms < 60000) return (ms / 1000).toFixed(1) + 's';
  return Math.floor(ms / 60000) + 'm ' + Math.floor((ms % 60000) / 1000) + 's';
}

function formatStatus(status) {
  if (!status) return '-';
  return status.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function deriveDisplayStatus(run) {
  if (!run) return '';
  const outcome = String(run.outcome || '').toLowerCase();
  if (outcome) return outcome;
  const status = String(run.status || run.Status || '').toLowerCase();
  const verification = String(run.verification_status || '').toLowerCase();
  if (status === 'completed' && verification === 'failed') return 'verification_failed';
  if (status === 'completed' && verification === 'warning') return 'completed_warning';
  return status;
}

function shortId(id) {
  if (!id) return '-';
  return id.length > 12 ? id.substring(0, 12) + '...' : id;
}

function formatTokens(n) {
  if (!n) return '0';
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
  return String(n);
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

function retryInteractionPayload(run) {
  const runID = String(run && run.id || '').trim();
  return {
    session_key: String(run && run.session_key || '').trim(),
    content: '',
    parent_run_id: runID,
    structured_command: {
      kind: 'retry',
      run_id: runID,
    },
  };
}

function submitRetryInteraction(run) {
  return api.post('/runtime/interact', retryInteractionPayload(run));
}

function formatPreflight(state) {
  if (!state) return '-';
  return state.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function formatVerification(state) {
  if (!state) return '-';
  return state.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function formatTriageSource(source) {
  if (!source) return '-';
  return String(source).replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function truncatePreview(text) {
  const value = String(text || '').trim();
  if (value.length <= PREVIEW_TEXT_LIMIT) return value;
  return value.slice(0, PREVIEW_TEXT_LIMIT) + '…';
}

function prettyData(value) {
  if (value == null) return '';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch (_) {
    return String(value);
  }
}

function suggestedActionLabel(action) {
  if (!action) return '';
  return suggestedActionLabels()[action.kind] || action.label || formatStatus(action.kind || '');
}

function joined(items) {
  return (items || []).filter(Boolean).join(' · ');
}

function governanceDecision(receipt, trace) {
  if (receipt && receipt.policy) return receipt.policy;
  if (trace && trace.decision) return trace.decision;
  return null;
}

function governanceApproval(receipt) {
  return receipt && receipt.approval ? receipt.approval : null;
}

function governanceTools(receipt, trace) {
  if (receipt && Array.isArray(receipt.tool_names) && receipt.tool_names.length) return receipt.tool_names;
  if (trace && Array.isArray(trace.tool_names) && trace.tool_names.length) return trace.tool_names;
  return [];
}

function governanceScopeText(receipt) {
  const scope = receipt && receipt.scope;
  if (!scope || typeof scope !== 'object') return '';
  return joined([scope.automation_id]);
}

function governanceSnapshotId(receipt, trace) {
  return String((receipt && receipt.effective_config_snapshot_id) || (trace && trace.effective_config_snapshot_id) || '').trim();
}

function policyActionClass(action) {
  switch (String(action || '').trim().toLowerCase()) {
    case 'allow':
      return 'hc-badge-green';
    case 'require_approval':
      return 'hc-badge-orange';
    case 'deny':
      return 'hc-badge-red';
    default:
      return 'hc-badge-gray';
  }
}

function approvalStatusClass(status) {
  switch (String(status || '').trim().toLowerCase()) {
    case 'approved':
      return 'hc-badge-green';
    case 'pending':
      return 'hc-badge-orange';
    case 'denied':
      return 'hc-badge-red';
    case 'cancelled':
      return 'hc-badge-gray';
    default:
      return 'hc-badge-gray';
  }
}

function governanceSummary(receipt, trace) {
  return String((receipt && receipt.summary) || (governanceDecision(receipt, trace) && governanceDecision(receipt, trace).summary) || '').trim();
}

function approvalProvidersText(approval) {
  if (!approval || !Array.isArray(approval.external)) return '';
  return joined(approval.external.map(item => item && item.provider));
}

// ---------------------------------------------------------------------------
// Runs View component
// ---------------------------------------------------------------------------

export function RunsView() {
  let _mounted = false;
  let refreshTimer = null;
  let sseXhr = null;
  let sseBuf = '';
  let reconnectTimer = null;
  let sseGeneration = 0;

  const $template = `
    <div class="hc-runs">
      <div class="hc-runs-header">
        <h2 class="hc-page-title">{{ t('runsTitle') || 'Runs' }}</h2>
        <div class="hc-runs-filters">
          <select class="hc-form-select hc-filter-select" v-model="filterStatus" @change="applyFilters()">
            <option value="">{{ t('allStatuses') || 'All Statuses' }}</option>
            <option v-for="s in statusKeys" :key="s" :value="s">{{ formatStatus(s) }}</option>
          </select>
          <select class="hc-form-select hc-filter-select" v-model="filterSession" @change="applyFilters()">
            <option value="">{{ t('allSessions') || 'All Sessions' }}</option>
            <option v-for="sess in sessions" :key="sess.key || sess.id" :value="sess.key || sess.id">{{ sess.key || sess.id }}</option>
          </select>
          <input class="hc-form-input hc-filter-input" type="text" v-model="filterModel" :placeholder="t('filterByModel') || 'Model...'" @input="applyFilters()" style="max-width:140px" />
          <button class="hc-btn hc-btn-sm hc-btn-ghost" data-testid="runs-export-all" @click="exportAllRuns()" style="margin-left:auto">{{ t('export') || 'Export' }}</button>
        </div>
      </div>

      <div v-if="error" class="hc-empty">
        <p>{{ t('loadError') }}</p>
        <p style="color:var(--text-muted);font-size:0.85rem">{{ error }}</p>
        <button class="hc-btn hc-btn-secondary" style="margin-top:8px" @click="loadRuns()">{{ t('retryLoad') }}</button>
      </div>
      <div v-else-if="loading" class="hc-loading">{{ t('loading') }}</div>
      <div v-else-if="filteredRuns.length === 0" class="hc-empty">{{ t('noRuns') || 'No runs found.' }}</div>
      <div v-else class="hc-runs-table-wrap">
        <div v-if="selectedRuns.length > 0" data-testid="runs-batch-actions" style="display:flex;align-items:center;gap:12px;padding:10px 0;border-bottom:1px solid var(--border);margin-bottom:12px">
          <span style="font-size:0.84rem;color:var(--ink)">{{ selectedRuns.length }} {{ t('selected') || 'selected' }}</span>
          <button class="hc-btn hc-btn-sm hc-btn-secondary" data-testid="runs-batch-retry" @click="batchRetry()">{{ t('retry') || 'Retry' }} {{ t('selected') || 'Selected' }}</button>
          <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="exportSelected()">{{ t('export') || 'Export' }}</button>
          <button class="hc-btn hc-btn-sm hc-btn-ghost" @click="clearSelection()">{{ t('cancel') }}</button>
        </div>
        <table class="hc-table">
          <thead><tr>
            <th style="width:36px"><input type="checkbox" @change="toggleSelectAll()" :checked="selectedRuns.length === paginatedRuns.length && paginatedRuns.length > 0" /></th>
            <th>{{ t('runId') || 'ID' }}</th>
            <th>{{ t('session') || 'Session' }}</th>
            <th>{{ t('status') || 'Status' }}</th>
            <th>{{ t('model') || 'Model' }}</th>
            <th>{{ t('started') || 'Started' }}</th>
            <th>{{ t('duration') || 'Duration' }}</th>
            <th>{{ t('toolRounds') || 'Tools' }}</th>
            <th>{{ t('tokens') || 'Tokens' }}</th>
            <th></th>
          </tr></thead>
          <tbody v-for="run in paginatedRuns" :key="run.id">
              <tr class="hc-runs-row" :data-testid="'runs-row-' + run.id" :class="{ expanded: expandedRunId === run.id }" @click="toggleExpand(run.id, $event)">
                <td @click.stop><input type="checkbox" :data-testid="'runs-select-' + run.id" :checked="selectedRuns.includes(run.id)" @change="toggleSelectRun(run.id)" /></td>
                <td class="hc-runs-id">{{ shortId(run.id) }}</td>
                <td>{{ run.session_key || run.session_id || '-' }}</td>
                <td>
                  <div style="display:flex;flex-direction:column;gap:6px;align-items:flex-start">
                    <span class="hc-badge" :class="statusClass(run.display_status || run.status)">{{ formatStatus(run.display_status || run.status) }}</span>
                    <span v-if="run.verification_summary && (run.display_status === 'partial' || run.display_status === 'completed_warning' || run.display_status === 'verification_failed')" style="color:var(--text-muted);font-size:0.75rem;max-width:180px;word-break:break-word">{{ run.verification_summary }}</span>
                    <span v-if="run.preflight && run.preflight.state && run.preflight.state !== 'ready'" class="hc-badge" :class="preflightClass(run.preflight.state)">{{ formatPreflight(run.preflight.state) }}</span>
                    <span v-if="governanceDecision(run.governance, run.governance_trace) && governanceDecision(run.governance, run.governance_trace).action" class="hc-badge" :class="policyActionClass(governanceDecision(run.governance, run.governance_trace).action)">{{ formatStatus(governanceDecision(run.governance, run.governance_trace).action) }}</span>
                    <span v-if="governanceApproval(run.governance) && governanceApproval(run.governance).status" class="hc-badge" :class="approvalStatusClass(governanceApproval(run.governance).status)">{{ formatStatus(governanceApproval(run.governance).status) }}</span>
                    <span v-if="governanceSummary(run.governance, run.governance_trace)" style="color:var(--text-muted);font-size:0.75rem;max-width:220px;word-break:break-word">{{ governanceSummary(run.governance, run.governance_trace) }}</span>
                    <span v-if="run.triage" style="color:var(--text-muted);font-size:0.75rem">
                      {{ t('runsTriage') || 'Triage' }}: {{ formatTriageSource(run.triage.source) }}<template v-if="run.triage.cache_hit"> ({{ t('runsTriggerCache') || 'cache' }})</template>
                    </span>
                  </div>
                </td>
                <td>{{ run.model || '-' }}</td>
                <td>{{ fmtTime(run.started_at || run.created_at) }}</td>
                <td>{{ fmtDuration(run.started_at || run.created_at, run.finished_at) }}</td>
                <td>{{ run.tool_rounds || run.tool_call_count || 0 }}</td>
                <td>{{ formatTokens(run.total_tokens || run.token_count || 0) }}</td>
                <td class="hc-runs-actions">
                  <button v-if="run.status === 'running' || run.status === 'waiting_approval' || run.status === 'waiting_input'"
                    class="hc-btn hc-btn-sm hc-btn-danger" :data-testid="'runs-cancel-' + run.id" @click.stop="cancelRun(run.id)">{{ t('cancel') }}</button>
                  <button v-if="run.status === 'failed' || run.display_status === 'verification_failed'"
                    class="hc-btn hc-btn-sm hc-btn-secondary" :data-testid="'runs-retry-' + run.id" @click.stop="retryRun(run)">{{ t('retry') || 'Retry' }}</button>
                </td>
              </tr>
              <tr v-if="expandedRunId === run.id" class="hc-runs-detail-row">
                <td colspan="10">
                  <div class="hc-run-detail">
                    <div v-if="run.error" class="hc-run-detail-error">
                      <strong>{{ t('error') || 'Error' }}:</strong> {{ run.error }}
                    </div>
                    <div v-if="run.preflight && run.preflight.state" class="hc-run-detail-section">
                      <h4>{{ t('runsPreflight') || 'Preflight' }}</h4>
                      <div class="hc-run-detail-grid">
                        <div><span class="hc-run-detail-label">{{ t('runsPreflightState') || 'State' }}</span><span>{{ formatPreflight(run.preflight.state) }}</span></div>
                        <div><span class="hc-run-detail-label">{{ t('runsPreflightBlocking') || 'Blocking' }}</span><span>{{ run.preflight.blocking ? (t('yes') || 'Yes') : (t('no') || 'No') }}</span></div>
                      </div>
                      <div v-if="run.preflight.summary" style="margin-top:8px;color:var(--text-muted)">{{ run.preflight.summary }}</div>
                      <div v-if="run.preflight.question" style="margin-top:8px"><strong>{{ t('runsPreflightReplyWith') || 'Reply with' }}:</strong> {{ run.preflight.question }}</div>
                      <div v-if="run.preflight.reply_template" style="margin-top:8px">
                        <strong>{{ t('runsPreflightTemplate') || 'Template' }}:</strong>
                        <pre class="hc-preflight-template" style="margin-top:6px">{{ run.preflight.reply_template }}</pre>
                      </div>
                      <div v-if="run.preflight.reply_hints && run.preflight.reply_hints.length" style="margin-top:8px;color:var(--text-muted)">
                        <strong>{{ t('runsPreflightExamples') || 'Examples' }}:</strong> {{ run.preflight.reply_hints.join(' / ') }}
                      </div>
                      <div v-if="run.preflight.clarification_slots && run.preflight.clarification_slots.length" style="margin-top:10px;display:flex;flex-direction:column;gap:8px">
                        <div v-for="slot in run.preflight.clarification_slots" :key="'slot-'+slot.id" style="padding:10px 12px;border:1px solid var(--border);border-radius:10px;background:var(--panel-soft)">
                          <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                            <strong>{{ slot.label || formatStatus(slot.id) }}</strong>
                            <span v-if="slot.required" class="hc-badge hc-badge-red">{{ t('runsPreflightRequired') || 'Required' }}</span>
                          </div>
                          <div v-if="slot.question" style="margin-top:6px">{{ slot.question }}</div>
                          <div v-if="slot.placeholder" style="margin-top:6px;color:var(--text-muted)">{{ t('runsPreflightFormat') || 'Format' }}: {{ slot.placeholder }}</div>
                          <div v-if="slot.hints && slot.hints.length" style="margin-top:6px;color:var(--text-muted)">{{ t('runsPreflightExamples') || 'Examples' }}: {{ slot.hints.join(' / ') }}</div>
                        </div>
                      </div>
                      <div v-if="run.preflight.continue_hint" style="margin-top:8px;color:var(--text-muted)">
                        <strong>{{ t('runsPreflightNext') || 'Next' }}:</strong> {{ run.preflight.continue_hint }}
                      </div>
                      <div v-if="run.preflight.checks && run.preflight.checks.length" style="margin-top:10px;display:flex;flex-direction:column;gap:8px">
                        <div v-for="check in run.preflight.checks" :key="check.id" style="padding:10px 12px;border:1px solid var(--border);border-radius:10px;background:var(--panel-soft)">
                          <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                            <strong>{{ check.title || check.id }}</strong>
                            <span class="hc-badge" :class="preflightClass(check.state)">{{ formatPreflight(check.state) }}</span>
                            <span v-if="check.blocking" class="hc-badge hc-badge-red">{{ t('runsPreflightBlocking') || 'Blocking' }}</span>
                          </div>
                          <div v-if="check.detail" style="margin-top:6px;color:var(--text-muted)">{{ check.detail }}</div>
                        </div>
                      </div>
                    </div>
                    <div v-if="hasGovernance()" class="hc-run-detail-section">
                      <h4>{{ t('runsGovernance') || 'Governance' }}</h4>
                      <div class="hc-result-panel hc-contract-panel">
                        <div class="hc-result-panel-header">
                          <div class="hc-result-panel-copy">
                            <div class="hc-result-panel-title">{{ expandedGovernanceTitle() }}</div>
                            <div v-if="expandedGovernanceSummary()" class="hc-result-panel-summary">{{ expandedGovernanceSummary() }}</div>
                          </div>
                          <div class="hc-result-panel-badges">
                            <span v-if="expandedGovernanceAction()" class="hc-badge" :class="policyActionClass(expandedGovernanceAction())">{{ formatStatus(expandedGovernanceAction()) }}</span>
                            <span v-if="expandedGovernanceApprovalStatus()" class="hc-badge" :class="approvalStatusClass(expandedGovernanceApprovalStatus())">{{ formatStatus(expandedGovernanceApprovalStatus()) }}</span>
                          </div>
                        </div>
                        <div class="hc-run-detail-grid">
                          <div v-if="expandedGovernanceSnapshotId()"><span class="hc-run-detail-label">{{ t('runsGovernanceSnapshot') || 'Snapshot' }}</span><span>{{ expandedGovernanceSnapshotId() }}</span></div>
                          <div v-if="expandedGovernanceScopeText()"><span class="hc-run-detail-label">{{ t('scope') || 'Scope' }}</span><span>{{ expandedGovernanceScopeText() }}</span></div>
                          <div v-if="expandedGovernancePolicySource()"><span class="hc-run-detail-label">{{ t('runsGovernancePolicySource') || 'Policy Source' }}</span><span>{{ expandedGovernancePolicySource() }}</span></div>
                          <div v-if="expandedGovernanceApprovalId()"><span class="hc-run-detail-label">{{ t('runsGovernanceApprovalId') || 'Approval ID' }}</span><span>{{ expandedGovernanceApprovalId() }}</span></div>
                          <div v-if="expandedGovernanceApprovalKind()"><span class="hc-run-detail-label">{{ t('runsGovernanceApprovalKind') || 'Approval Kind' }}</span><span>{{ expandedGovernanceApprovalKind() }}</span></div>
                          <div v-if="expandedGovernanceUpdatedAt()"><span class="hc-run-detail-label">{{ t('runsGovernanceUpdated') || 'Updated' }}</span><span>{{ fmtTime(expandedGovernanceUpdatedAt()) }}</span></div>
                        </div>
                        <div v-if="expandedGovernanceToolNames().length" class="hc-result-chip-row">
                          <span v-for="(tool, idx) in expandedGovernanceToolNames()" :key="'gov-tool-'+idx" class="hc-result-chip">{{ tool }}</span>
                        </div>
                        <div class="hc-contract-grid">
                          <div class="hc-contract-card">
                            <div class="hc-contract-card-title">{{ t('runsGovernancePolicy') || 'Policy' }}</div>
                            <div class="hc-contract-card-copy">{{ expandedGovernancePolicySource() || (t('runsGovernanceRuntimePolicy') || 'runtime policy') }}</div>
                            <div v-if="expandedGovernanceDecision() && expandedGovernanceDecision().approval_policy && (expandedGovernanceDecision().approval_policy.default_scope || expandedGovernanceDecision().approval_policy.max_scope)" class="hc-contract-card-note">
                              {{ [expandedGovernanceDecision().approval_policy.default_scope ? 'default ' + expandedGovernanceDecision().approval_policy.default_scope : '', expandedGovernanceDecision().approval_policy.max_scope ? 'max ' + expandedGovernanceDecision().approval_policy.max_scope : ''].filter(Boolean).join(' · ') }}
                            </div>
                            <div v-if="expandedGovernanceReasons().length" class="hc-contract-list">
                              <div v-for="(reason, idx) in expandedGovernanceReasons()" :key="'gov-reason-'+idx" class="hc-contract-list-item">
                                <div class="hc-contract-card-copy">{{ reason }}</div>
                              </div>
                            </div>
                            <div v-else class="hc-contract-empty">{{ t('runsGovernanceNoPolicyReasons') || 'No explicit policy reasons were persisted for this run.' }}</div>
                          </div>
                          <div class="hc-contract-card">
                            <div class="hc-contract-card-title">{{ t('runsGovernanceApproval') || 'Approval' }}</div>
                            <div class="hc-contract-card-copy">{{ [expandedGovernanceApprovalKind(), expandedGovernanceApprovalId()].filter(Boolean).join(' · ') || (t('runsGovernanceNoApprovalTicket') || 'No approval ticket') }}</div>
                            <div v-if="expandedGovernanceApprovalProviders()" class="hc-contract-card-note">{{ expandedGovernanceApprovalProviders() }}</div>
                            <div v-if="expandedGovernanceExternalRefs().length" class="hc-contract-list">
                              <div v-for="(ref, idx) in expandedGovernanceExternalRefs()" :key="'gov-ref-'+idx" class="hc-contract-list-item">
                                <div class="hc-contract-item-head">
                                  <div class="hc-result-deliverable-name">{{ [ref.provider, ref.external_id].filter(Boolean).join(' · ') || (t('runsGovernanceExternalRef') || 'External ref') }}</div>
                                  <span v-if="ref.status" class="hc-badge hc-badge-gray">{{ ref.status }}</span>
                                </div>
                                <a v-if="safeExternalURL(ref.url)" class="hc-governance-link" :href="safeExternalURL(ref.url)" target="_blank" rel="noopener noreferrer">{{ safeExternalURL(ref.url) }}</a>
                              </div>
                            </div>
                          </div>
                        </div>
                        <div v-if="expandedGovernanceReasonCodes().length || expandedGovernancePolicyLayers().length || expandedGovernanceAuditLabels().length" class="hc-contract-grid">
                          <div v-if="expandedGovernanceReasonCodes().length" class="hc-contract-card">
                            <div class="hc-contract-card-title">{{ t('runsGovernanceReasonCodes') || 'Reason Codes' }}</div>
                            <div class="hc-result-chip-row">
                              <span v-for="(item, idx) in expandedGovernanceReasonCodes()" :key="'gov-code-'+idx" class="hc-result-chip">{{ item }}</span>
                            </div>
                          </div>
                          <div class="hc-contract-card">
                            <div class="hc-contract-card-title">{{ t('runsGovernanceTrace') || 'Trace' }}</div>
                            <div v-if="expandedGovernancePolicyLayers().length" class="hc-contract-hints">{{ expandedGovernancePolicyLayers().join(' · ') }}</div>
                            <div v-if="expandedGovernanceAuditLabels().length" class="hc-contract-hints">{{ expandedGovernanceAuditLabels().join(' · ') }}</div>
                            <div v-if="!expandedGovernancePolicyLayers().length && !expandedGovernanceAuditLabels().length" class="hc-contract-empty">{{ t('runsGovernanceNoTraceLabels') || 'No policy trace labels were persisted for this run.' }}</div>
                          </div>
                        </div>
                      </div>
                    </div>
                    <div class="hc-run-detail-section">
                      <h4>{{ t('timing') || 'Timing' }}</h4>
                      <div class="hc-run-detail-grid">
                        <div><span class="hc-run-detail-label">{{ t('created') || 'Created' }}</span><span>{{ fmtTime(run.created_at) }}</span></div>
                        <div v-if="run.started_at"><span class="hc-run-detail-label">{{ t('started') || 'Started' }}</span><span>{{ fmtTime(run.started_at) }}</span></div>
                        <div v-if="run.finished_at"><span class="hc-run-detail-label">{{ t('finished') || 'Finished' }}</span><span>{{ fmtTime(run.finished_at) }}</span></div>
                      </div>
                    </div>
                    <div v-if="expandedRunDetail && expandedRunDetail.triage" class="hc-run-detail-section">
                      <h4>{{ t('runsTriage') || 'Triage' }}</h4>
                      <div class="hc-run-detail-grid">
                        <div><span class="hc-run-detail-label">{{ t('runsTriageSource') || 'Source' }}</span><span>{{ formatTriageSource(expandedRunDetail.triage.source) }}</span></div>
                        <div><span class="hc-run-detail-label">{{ t('runsTriageCacheHit') || 'Cache Hit' }}</span><span>{{ expandedRunDetail.triage.cache_hit ? (t('yes') || 'Yes') : (t('no') || 'No') }}</span></div>
                        <div v-if="expandedRunDetail.triage.confidence"><span class="hc-run-detail-label">{{ t('runsTriageConfidence') || 'Confidence' }}</span><span>{{ Number(expandedRunDetail.triage.confidence).toFixed(2) }}</span></div>
                        <div v-if="expandedRunDetail.triage.generated_at"><span class="hc-run-detail-label">{{ t('runsTriageGenerated') || 'Generated' }}</span><span>{{ fmtTime(expandedRunDetail.triage.generated_at) }}</span></div>
                      </div>
                      <div v-if="expandedRunDetail.triage.reason" style="margin-top:8px;color:var(--text-muted)">{{ expandedRunDetail.triage.reason }}</div>
                      <div v-if="expandedRunDetail.triage.suggested_domains && expandedRunDetail.triage.suggested_domains.length" style="margin-top:8px;color:var(--text-muted)">
                        <strong>{{ t('runsTriageDomains') || 'Domains' }}:</strong> {{ expandedRunDetail.triage.suggested_domains.join(', ') }}
                      </div>
                    </div>
                    <div v-if="hasTaskContract()" class="hc-run-detail-section">
                      <h4>{{ t('runsTaskContract') || 'Task Contract' }}</h4>
                      <div class="hc-result-panel hc-contract-panel">
                        <div class="hc-result-panel-header">
                          <div class="hc-result-panel-copy">
                            <div class="hc-result-panel-title">{{ taskContractGoal() || (t('runsTaskContract') || 'Task contract') }}</div>
                            <div v-if="taskContractTargetSummary()" class="hc-result-panel-summary">{{ t('runsTaskContractTarget') || 'Concrete target' }}: {{ taskContractTargetSummary() }}</div>
                          </div>
                          <div class="hc-result-panel-badges">
                            <span v-if="taskContractJobType()" class="hc-badge hc-badge-gray">{{ taskContractJobType() }}</span>
                            <span v-if="taskContractConfidenceText()" class="hc-badge hc-badge-gray">{{ taskContractConfidenceText() }}</span>
                            <span v-if="taskContractRequiresExternalEffect()" class="hc-badge hc-badge-orange">{{ t('runsTaskContractExternalEffect') || 'External effect' }}</span>
                            <span v-if="taskContractRequiresApproval()" class="hc-badge hc-badge-orange">{{ t('runsTaskContractApprovalAware') || 'Approval-aware' }}</span>
                          </div>
                        </div>
                        <div v-if="taskContractSuggestedDomains().length" class="hc-result-chip-row">
                          <span v-for="(domain, idx) in taskContractSuggestedDomains()" :key="'tc-domain-'+idx" class="hc-result-chip">{{ formatStatus(domain) }}</span>
                        </div>
                        <div class="hc-contract-grid">
                          <div class="hc-contract-card">
                            <div class="hc-contract-card-title">{{ t('runsTaskContractGoal') || 'Goal' }}</div>
                            <div class="hc-contract-card-copy">{{ taskContractGoal() || '-' }}</div>
                            <div v-if="taskContractTargetSummary()" class="hc-contract-card-note">{{ t('runsTaskContractTarget') || 'Concrete target' }}: {{ taskContractTargetSummary() }}</div>
                          </div>
                          <div class="hc-contract-card">
                            <div class="hc-contract-card-title">{{ t('runsTaskContractMissingInfo') || 'Missing Info' }}</div>
                            <div v-if="taskContractMissingInfo().length" class="hc-contract-list">
                              <div v-for="(item, idx) in taskContractMissingInfo()" :key="'tc-missing-'+idx" class="hc-contract-list-item">
                                <div class="hc-contract-item-head">
                                  <div class="hc-result-deliverable-name">{{ item.label || item.summary || item.id || (t('runsTaskContractMissingInput') || 'Missing input') }}</div>
                                  <span v-if="item.required" class="hc-badge hc-badge-red">{{ t('runsPreflightRequired') || 'Required' }}</span>
                                </div>
                                <div v-if="item.question" class="hc-contract-card-copy">{{ item.question }}</div>
                                <div v-if="item.placeholder" class="hc-contract-hints">{{ t('runsPreflightFormat') || 'Format' }}: {{ item.placeholder }}</div>
                                <div v-if="item.hints && item.hints.length" class="hc-contract-hints">{{ item.hints.join(' / ') }}</div>
                              </div>
                            </div>
                            <div v-else class="hc-contract-empty">{{ t('runsTaskContractNoMissingInfo') || 'No blocking gaps were recorded for this run.' }}</div>
                          </div>
                        </div>
                        <div class="hc-contract-grid">
                          <div class="hc-contract-card">
                            <div class="hc-contract-card-title">{{ t('runsTaskContractResolvedInputs') || 'Resolved Inputs' }}</div>
                            <div v-if="taskContractResolvedInfo().length" class="hc-contract-list">
                              <div v-for="(item, idx) in taskContractResolvedInfo()" :key="'tc-resolved-'+idx" class="hc-contract-list-item">
                                <div class="hc-contract-item-head">
                                  <div class="hc-result-deliverable-name">{{ item.label || formatStatus(item.id) }}</div>
                                  <span class="hc-badge hc-badge-green">{{ t('resolved') || 'Resolved' }}</span>
                                </div>
                                <div v-if="item.value" class="hc-contract-card-copy">{{ item.value }}</div>
                              </div>
                            </div>
                            <div v-else class="hc-contract-empty">{{ t('runsTaskContractNoResolvedInputs') || 'No clarified inputs were persisted for this run.' }}</div>
                          </div>
                        </div>
                        <div class="hc-contract-grid">
                          <div class="hc-contract-card">
                            <div class="hc-contract-card-title">{{ t('runsTaskContractDeliverables') || 'Deliverables' }}</div>
                            <div v-if="taskContractDeliverables().length" class="hc-contract-list">
                              <div v-for="(item, idx) in taskContractDeliverables()" :key="'tc-deliverable-'+idx" class="hc-contract-list-item">
                                <div class="hc-contract-item-head">
                                  <div class="hc-result-deliverable-name">{{ taskContractDeliverableTitle(item) }}</div>
                                  <span v-if="item.required" class="hc-badge hc-badge-green">{{ t('runsPreflightRequired') || 'Required' }}</span>
                                </div>
                                <div v-if="item.summary" class="hc-contract-card-copy">{{ item.summary }}</div>
                              </div>
                            </div>
                            <div v-else class="hc-contract-empty">{{ t('runsTaskContractNoDeliverables') || 'No explicit deliverables were persisted for this run.' }}</div>
                          </div>
                          <div class="hc-contract-card">
                            <div class="hc-contract-card-title">{{ t('runsTaskContractAcceptance') || 'Acceptance' }}</div>
                            <div v-if="taskContractAcceptance().length" class="hc-contract-list">
                              <div v-for="(item, idx) in taskContractAcceptance()" :key="'tc-accept-'+idx" class="hc-contract-list-item">
                                <div class="hc-contract-item-head">
                                  <div class="hc-result-deliverable-name">{{ item.summary || item.id || (t('runsTaskContractAcceptanceRule') || 'Acceptance rule') }}</div>
                                  <span v-if="item.required" class="hc-badge hc-badge-green">{{ t('runsPreflightRequired') || 'Required' }}</span>
                                </div>
                                <div v-if="item.deliverable_kinds && item.deliverable_kinds.length" class="hc-contract-hints">
                                  {{ item.deliverable_kinds.map(formatStatus).join(' · ') }}
                                </div>
                                <div v-if="item.evidence_hints && item.evidence_hints.length" class="hc-contract-hints">
                                  {{ t('runsTaskContractEvidence') || 'Evidence' }}: {{ item.evidence_hints.join(' / ') }}
                                </div>
                              </div>
                            </div>
                            <div v-else class="hc-contract-empty">{{ t('runsTaskContractNoAcceptance') || 'No explicit acceptance rules were persisted for this run.' }}</div>
                          </div>
                        </div>
                      </div>
                    </div>
                    <div v-if="expandedRunDetail && hasResultReceipt()" class="hc-run-detail-section">
                      <h4>{{ t('result') || 'Result' }}</h4>
                      <div class="hc-result-panel">
                        <div class="hc-result-panel-header">
                          <div class="hc-result-panel-copy">
                            <div class="hc-result-panel-title">{{ expandedResultTitle() }}</div>
                            <div v-if="expandedResultSummary()" class="hc-result-panel-summary">{{ expandedResultSummary() }}</div>
                          </div>
                          <div class="hc-result-panel-badges">
                            <span v-if="expandedResultOutcome()" class="hc-badge" :class="statusClass(expandedResultOutcome())">{{ formatStatus(expandedResultOutcome()) }}</span>
                            <span v-if="expandedVerificationStatus()" class="hc-badge" :class="verificationClass(expandedVerificationStatus())">{{ formatVerification(expandedVerificationStatus()) }}</span>
                          </div>
                        </div>
                        <div v-if="expandedResultActions().length" class="hc-result-section">
                          <div class="hc-result-section-head">
                            <div class="hc-result-block-title">{{ t('runsActions') || 'Actions' }}</div>
                            <div class="hc-result-deliverable-meta">{{ expandedResultActions().length }} {{ t('runsItems') || 'item(s)' }}</div>
                          </div>
                          <div class="hc-result-action-grid">
                            <div v-for="(action, idx) in expandedResultActions()" :key="'ra-'+idx" class="hc-result-action-card">
                              <div class="hc-result-action-head">
                                <div>
                                  <div class="hc-result-deliverable-name">{{ resultActionTitle(action) }}</div>
                                  <div class="hc-result-deliverable-sub">{{ action.kind || 'action' }}</div>
                                </div>
                              </div>
                              <div v-if="resultActionDescription(action)" class="hc-result-action-body">{{ resultActionDescription(action) }}</div>
                              <div class="hc-result-deliverable-actions">
                                <a v-if="resultActionHref(action)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="resultActionHref(action)" target="_blank" rel="noopener noreferrer">{{ t('runsOpen') || 'Open' }}</a>
                                <button v-else-if="canTriggerResultAction(action)" class="hc-btn hc-btn-sm hc-btn-secondary" @click="triggerResultAction(action)">{{ t('runsRun') || 'Run' }}</button>
                                <span v-else-if="action.target" class="hc-result-deliverable-uri">{{ action.target }}</span>
                              </div>
                            </div>
                          </div>
                        </div>
                        <div v-if="expandedResultBlocks().length" class="hc-result-section">
                          <div class="hc-result-section-head">
                            <div class="hc-result-block-title">{{ t('runsBlocks') || 'Blocks' }}</div>
                            <div class="hc-result-deliverable-meta">{{ expandedResultBlocks().length }} {{ t('runsItems') || 'item(s)' }}</div>
                          </div>
                          <div class="hc-result-block-list">
                            <div v-for="(block, idx) in expandedResultBlocks()" :key="'rb-'+idx" class="hc-result-block">
                              <div class="hc-result-block-title">{{ block.title || formatStatus(block.kind || 'summary') }}</div>
                              <div v-if="resultBlockText(block)" class="hc-result-block-content">{{ resultBlockText(block) }}</div>
                              <pre v-if="resultBlockData(block)" class="hc-run-detail-tool-args">{{ resultBlockData(block) }}</pre>
                            </div>
                          </div>
                        </div>
                        <pre v-else-if="expandedOutput()" class="hc-run-detail-tool-args">{{ expandedOutput() }}</pre>
                        <div v-if="expandedResultArtifacts().length" class="hc-result-section">
                          <div class="hc-result-section-head">
                            <div class="hc-result-block-title">{{ t('runsArtifacts') || 'Artifacts' }}</div>
                            <div class="hc-result-deliverable-meta">{{ expandedResultArtifacts().length }} {{ t('runsItems') || 'item(s)' }}</div>
                          </div>
                          <div class="hc-result-deliverable-grid">
                            <div v-for="(item, idx) in expandedResultArtifacts()" :key="'deliverable-'+idx" class="hc-result-deliverable-card">
                              <div class="hc-result-deliverable-head">
                                <div>
                                  <div class="hc-result-deliverable-name">{{ item.name || item.label || item.kind || 'artifact' }}</div>
                                  <div class="hc-result-deliverable-sub">{{ item.kind || 'artifact' }}<template v-if="item.tool_name"> · {{ item.tool_name }}</template></div>
                                </div>
                                <span v-if="item.size_bytes" class="hc-result-deliverable-meta">{{ formatFileSize(item.size_bytes) }}</span>
                              </div>
                              <div v-if="item.content_type" class="hc-result-deliverable-meta">{{ item.content_type }}</div>
                              <div v-if="item.preview_text" class="hc-result-deliverable-preview">{{ truncatePreview(item.preview_text) }}</div>
                              <div class="hc-result-deliverable-actions">
                                <a v-if="deliverablePreviewHref(item)" class="hc-btn hc-btn-sm hc-btn-secondary" :href="deliverablePreviewHref(item)" target="_blank" rel="noopener noreferrer">{{ t('runsOpenPreview') || 'Open preview' }}</a>
                                <span v-else-if="item.uri" class="hc-result-deliverable-uri">{{ item.uri }}</span>
                              </div>
                            </div>
                          </div>
                        </div>
                        <div v-if="expandedSuggestedActions().length" class="hc-result-chip-row">
                          <span v-for="(action, idx) in expandedSuggestedActions()" :key="'sa-'+idx" class="hc-result-chip" :title="action.reason || ''">{{ suggestedActionLabel(action) }}</span>
                        </div>
                      </div>
                    </div>
                    <div v-if="expandedRunDetail && expandedVerificationStatus()" class="hc-run-detail-section">
                      <h4>{{ t('runsVerification') || 'Verification' }}</h4>
                      <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-bottom:8px">
                        <span class="hc-badge" :class="verificationClass(expandedVerificationStatus())">{{ formatVerification(expandedVerificationStatus()) }}</span>
                        <span v-if="expandedVerificationSummary()" style="color:var(--text-muted)">{{ expandedVerificationSummary() }}</span>
                      </div>
                      <div v-if="expandedVerificationChecks().length" class="hc-verification-checks">
                        <div v-for="check in expandedVerificationChecks()" :key="check.name" class="hc-verification-check">
                          <div class="hc-verification-check-header">
                            <strong>{{ check.name }}</strong>
                            <span class="hc-badge" :class="verificationClass(check.status)">{{ formatVerification(check.status) }}</span>
                            <span v-if="check.domain" style="color:var(--text-muted)">{{ check.domain }}</span>
                            <span v-if="check.requirement" class="hc-badge hc-badge-gray">{{ formatStatus(check.requirement) }}</span>
                          </div>
                          <div v-if="check.summary" class="hc-verification-check-summary">{{ check.summary }}</div>
                          <div v-if="check.evidence && check.evidence.length" class="hc-verification-evidence">
                            <div v-for="(item, idx) in check.evidence" :key="check.name + '-e-' + idx" class="hc-evidence-item">{{ item }}</div>
                          </div>
                          <div v-if="check.issues && check.issues.length" class="hc-verification-issues">
                            <div v-for="(issue, idx) in check.issues" :key="check.name + '-' + idx" style="font-size:0.85rem">
                              <strong>{{ issue.severity || 'issue' }}:</strong> {{ issue.message }}
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                    <div v-if="expandedRunDetail && expandedRunArtifacts && expandedRunArtifacts.length > 0" class="hc-run-detail-section">
                      <h4>{{ t('navArtifacts') || 'Artifacts' }}</h4>
                      <div class="hc-artifact-grid">
                        <div v-for="a in expandedRunArtifacts" :key="a.id" class="hc-artifact-card">
                          <div class="hc-artifact-card-head">
                            <div>
                              <div class="hc-result-deliverable-name">{{ a.name || a.id }}</div>
                              <div class="hc-result-deliverable-sub">{{ a.kind || 'artifact' }}</div>
                            </div>
                            <span v-if="a.size" class="hc-result-deliverable-meta">{{ formatFileSize(a.size) }}</span>
                          </div>
                          <div v-if="a.content_type" class="hc-result-deliverable-meta">{{ a.content_type }}</div>
                          <div v-if="artifactPreviewText(a)" class="hc-result-deliverable-preview">{{ truncatePreview(artifactPreviewText(a)) }}</div>
                          <div class="hc-result-deliverable-actions">
                            <a class="hc-btn hc-btn-sm hc-btn-secondary" :href="artifactPreviewPath(a)" target="_blank" rel="noopener noreferrer">{{ t('runsOpenPreview') || 'Open preview' }}</a>
                            <span v-if="a.created_at" class="hc-result-deliverable-meta">{{ fmtTime(a.created_at) }}</span>
                          </div>
                        </div>
                      </div>
                    </div>
                    <div v-if="expandedRunDetail && expandedRunDetail.tool_calls && expandedRunDetail.tool_calls.length > 0" class="hc-run-detail-section">
                      <h4>{{ t('toolCalls') || 'Tool Calls' }}</h4>
                      <div class="hc-run-detail-tools">
                        <div v-for="tc in expandedRunDetail.tool_calls" class="hc-run-detail-tool">
                          <span class="hc-run-detail-tool-name">{{ tc.name || tc.Name || 'tool' }}</span>
                          <pre v-if="tc.arguments || tc.Arguments" class="hc-run-detail-tool-args">{{ formatArgs(tc.arguments || tc.Arguments) }}</pre>
                        </div>
                      </div>
                    </div>
                  </div>
                </td>
              </tr>
          </tbody>
        </table>

        <div v-if="totalPages > 1" class="hc-pagination">
          <span class="hc-pagination-info">{{ (page-1)*Number(pageSize)+1 }}-{{ Math.min(page*Number(pageSize), filteredRuns.length) }} {{ t('of') }} {{ filteredRuns.length }}</span>
          <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="page<=1" @click="prevPage()">&#8249;</button>
          <span class="hc-pagination-pages">{{ page }} / {{ totalPages }}</span>
          <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="page>=totalPages" @click="nextPage()">&#8250;</button>
          <select class="hc-form-select hc-pagination-size" v-model="pageSize" @change="page=1">
            <option value="20">20</option>
            <option value="50">50</option>
            <option value="100">100</option>
          </select>
        </div>
      </div>
    </div>
  `;

  return {
    $template,
    t,

    sessions: [],
    runs: [],
    filteredRuns: [],
    filterStatus: '',
    filterSession: '',
    filterModel: '',
    selectedRuns: [],
    expandedRunId: null,
    expandedRunDetail: null,
    expandedCompletion: null,
    expandedRunArtifacts: [],
    loading: true,
    error: null,
    statusKeys: Object.keys(STATUS_CLASSES),
    page: 1,
    pageSize: DEFAULT_PAGE_SIZE,

    // Helpers
    fmtTime,
    fmtDuration,
    formatStatus,
    shortId,
    formatTokens,
    formatFileSize,
    formatPreflight,
    formatVerification,
    formatTriageSource,
    truncatePreview,
    suggestedActionLabel,
    artifactPreviewPath,
    safeExternalURL,
    joined,
    governanceDecision,
    governanceApproval,
    governanceSummary,
    policyActionClass,
    approvalStatusClass,

    statusClass(status) {
      return STATUS_CLASSES[status] || 'hc-badge-gray';
    },

    preflightClass(state) {
      return PREFLIGHT_CLASSES[state] || 'hc-badge-gray';
    },

    verificationClass(state) {
      return VERIFICATION_CLASSES[state] || 'hc-badge-gray';
    },

    runIdFromHash() {
      const match = String(window.location.hash || '').match(/^#\/runs\/([^/?]+)/);
      return match ? decodeURIComponent(match[1]) : '';
    },

    expandedResult() {
      return this.expandedCompletion && this.expandedCompletion.result ? this.expandedCompletion.result : null;
    },

    expandedVerification() {
      return this.expandedCompletion && this.expandedCompletion.verification ? this.expandedCompletion.verification : null;
    },

    expandedBundle() {
      return this.expandedCompletion && this.expandedCompletion.bundle ? this.expandedCompletion.bundle : null;
    },

    expandedTaskContract() {
      return this.expandedRunDetail && this.expandedRunDetail.task_contract ? this.expandedRunDetail.task_contract : null;
    },

    expandedGovernance() {
      return this.expandedRunDetail && this.expandedRunDetail.governance ? this.expandedRunDetail.governance : null;
    },

    expandedGovernanceTrace() {
      return this.expandedRunDetail && this.expandedRunDetail.governance_trace ? this.expandedRunDetail.governance_trace : null;
    },

    hasGovernance() {
      return Boolean(
        this.expandedGovernance() ||
        this.expandedGovernanceTrace()
      );
    },

    expandedGovernanceDecision() {
      return governanceDecision(this.expandedGovernance(), this.expandedGovernanceTrace());
    },

    expandedGovernanceAction() {
      return String(this.expandedGovernanceDecision() && this.expandedGovernanceDecision().action || '').trim();
    },

    expandedGovernanceSummary() {
      return governanceSummary(this.expandedGovernance(), this.expandedGovernanceTrace());
    },

    expandedGovernanceTitle() {
      switch (this.expandedGovernanceAction()) {
        case 'deny':
          return 'Execution blocked by policy';
        case 'require_approval':
          return 'Approval-gated execution';
        default:
          return 'Governance receipt';
      }
    },

    expandedGovernancePolicySource() {
      return String(this.expandedGovernanceDecision() && this.expandedGovernanceDecision().policy_source || '').trim();
    },

    expandedGovernanceApproval() {
      return governanceApproval(this.expandedGovernance());
    },

    expandedGovernanceApprovalStatus() {
      return String(this.expandedGovernanceApproval() && this.expandedGovernanceApproval().status || '').trim();
    },

    expandedGovernanceApprovalId() {
      return String(this.expandedGovernanceApproval() && this.expandedGovernanceApproval().id || '').trim();
    },

    expandedGovernanceApprovalKind() {
      return String(this.expandedGovernanceApproval() && this.expandedGovernanceApproval().kind || '').trim();
    },

    expandedGovernanceApprovalProviders() {
      return approvalProvidersText(this.expandedGovernanceApproval());
    },

    expandedGovernanceExternalRefs() {
      return this.expandedGovernanceApproval() && Array.isArray(this.expandedGovernanceApproval().external)
        ? this.expandedGovernanceApproval().external
        : [];
    },

    expandedGovernanceScopeText() {
      return governanceScopeText(this.expandedGovernance());
    },

    expandedGovernanceToolNames() {
      return governanceTools(this.expandedGovernance(), this.expandedGovernanceTrace());
    },

    expandedGovernanceSnapshotId() {
      return governanceSnapshotId(this.expandedGovernance(), this.expandedGovernanceTrace());
    },

    expandedGovernanceUpdatedAt() {
      return String(this.expandedGovernanceTrace() && this.expandedGovernanceTrace().updated_at || '').trim();
    },

    expandedGovernanceReasons() {
      return this.expandedGovernanceDecision() && Array.isArray(this.expandedGovernanceDecision().reasons)
        ? this.expandedGovernanceDecision().reasons.filter(Boolean)
        : [];
    },

    expandedGovernanceReasonCodes() {
      return this.expandedGovernanceDecision() && Array.isArray(this.expandedGovernanceDecision().reason_codes)
        ? this.expandedGovernanceDecision().reason_codes.filter(Boolean)
        : [];
    },

    expandedGovernancePolicyLayers() {
      return this.expandedGovernanceDecision() && Array.isArray(this.expandedGovernanceDecision().policy_layers)
        ? this.expandedGovernanceDecision().policy_layers.filter(Boolean)
        : [];
    },

    expandedGovernanceAuditLabels() {
      return this.expandedGovernanceDecision() && Array.isArray(this.expandedGovernanceDecision().audit_labels)
        ? this.expandedGovernanceDecision().audit_labels.filter(Boolean)
        : [];
    },

    hasTaskContract() {
      return Boolean(this.expandedTaskContract() && (
        this.taskContractGoal() ||
        this.taskContractSuggestedDomains().length ||
        this.taskContractMissingInfo().length ||
        this.taskContractResolvedInfo().length ||
        this.taskContractDeliverables().length ||
        this.taskContractAcceptance().length
      ));
    },

    taskContractGoal() {
      const contract = this.expandedTaskContract();
      return String((contract && contract.goal) || '').trim();
    },

    taskContractTargetSummary() {
      const contract = this.expandedTaskContract();
      return String((contract && contract.target_summary) || '').trim();
    },

    taskContractJobType() {
      const contract = this.expandedTaskContract();
      return formatStatus((contract && contract.job_type) || '');
    },

    taskContractConfidenceText() {
      const contract = this.expandedTaskContract();
      const value = Number(contract && contract.confidence);
      if (!Number.isFinite(value) || value <= 0) return '';
      return 'Confidence ' + value.toFixed(2);
    },

    taskContractRequiresExternalEffect() {
      const contract = this.expandedTaskContract();
      return Boolean(contract && contract.requires_external_effect);
    },

    taskContractRequiresApproval() {
      const contract = this.expandedTaskContract();
      return Boolean(contract && contract.requires_approval);
    },

    taskContractSuggestedDomains() {
      const contract = this.expandedTaskContract();
      return contract && Array.isArray(contract.suggested_domains) ? contract.suggested_domains : [];
    },

    taskContractMissingInfo() {
      const contract = this.expandedTaskContract();
      return contract && Array.isArray(contract.missing_info) ? contract.missing_info : [];
    },

    taskContractResolvedInfo() {
      const contract = this.expandedTaskContract();
      return contract && Array.isArray(contract.resolved_info) ? contract.resolved_info : [];
    },

    taskContractDeliverables() {
      const contract = this.expandedTaskContract();
      return contract && Array.isArray(contract.expected_deliverables) ? contract.expected_deliverables : [];
    },

    taskContractAcceptance() {
      const contract = this.expandedTaskContract();
      return contract && Array.isArray(contract.acceptance_criteria) ? contract.acceptance_criteria : [];
    },

    taskContractDeliverableTitle(item) {
      if (!item) return 'Deliverable';
      return formatStatus(item.kind || item.summary || 'deliverable');
    },

    expandedOutput() {
      const result = this.expandedResult();
      return String((result && result.output) || '').trim();
    },

    expandedVerificationStatus() {
      const verification = this.expandedVerification();
      return String((verification && verification.status) || '').trim();
    },

    expandedVerificationSummary() {
      const verification = this.expandedVerification();
      return String((verification && verification.summary) || '').trim();
    },

    expandedVerificationChecks() {
      const verification = this.expandedVerification();
      return verification && Array.isArray(verification.checks) ? verification.checks : [];
    },

    expandedResultOutcome() {
      const bundle = this.expandedBundle();
      const result = this.expandedResult();
      return String((bundle && bundle.outcome) || (this.expandedCompletion && this.expandedCompletion.outcome) || (result && result.outcome) || '').trim();
    },

    expandedResultTitle() {
      const outcome = this.expandedResultOutcome();
      if (!outcome) return 'Run receipt';
      switch (outcome) {
        case 'completed': return 'Execution receipt';
        case 'partial': return 'Result needs review';
        case 'failed': return 'Run failed';
        case 'cancelled': return 'Run cancelled';
        case 'needs_confirmation': return 'Waiting for your confirmation';
        default: return formatStatus(outcome);
      }
    },

    expandedResultSummary() {
      const bundle = this.expandedBundle();
      const result = this.expandedResult();
      return String((bundle && bundle.summary) || (result && result.summary) || '').trim();
    },

    expandedDeliveryBlocks() {
      const bundle = this.expandedBundle();
      const delivery = (this.expandedCompletion && this.expandedCompletion.delivery) || (bundle && bundle.delivery) || null;
      return delivery && Array.isArray(delivery.blocks) ? delivery.blocks : [];
    },

    expandedResultBlocks() {
      const blocks = this.expandedDeliveryBlocks();
      if (blocks.length) return blocks;
      if (this.expandedOutput()) {
        return [{ kind: 'text', title: 'Output', content: this.expandedOutput() }];
      }
      return [];
    },

    expandedSuggestedActions() {
      const bundle = this.expandedBundle();
      return bundle && Array.isArray(bundle.suggested_actions) ? bundle.suggested_actions : [];
    },

    expandedResultActions() {
      const bundle = this.expandedBundle();
      const result = this.expandedResult();
      const delivery = (this.expandedCompletion && this.expandedCompletion.delivery) || (bundle && bundle.delivery) || null;
      const actions = [];
      if (delivery && Array.isArray(delivery.next_actions)) {
        actions.push(...delivery.next_actions);
      }
      if (result && Array.isArray(result.next_actions)) {
        actions.push(...result.next_actions);
      }
      if (bundle && Array.isArray(bundle.suggested_actions)) {
        for (const item of bundle.suggested_actions) {
          actions.push({
            kind: item.kind,
            label: item.label,
            target: '',
            reason: item.reason,
          });
        }
      }
      return actions.filter(Boolean);
    },

    expandedDeliverables() {
      const bundle = this.expandedBundle();
      const result = this.expandedResult();
      const items = bundle && Array.isArray(bundle.deliverables)
        ? bundle.deliverables
        : (result && Array.isArray(result.deliverables) ? result.deliverables : []);
      return items.map(item => {
        const artifact = this.findArtifactByURI(item.uri);
        return Object.assign({}, item, artifact ? {
          name: item.name || artifact.name || artifact.id,
          content_type: item.content_type || artifact.content_type || '',
          size_bytes: item.size_bytes || artifact.size || 0,
          preview_text: item.preview_text || this.artifactPreviewText(artifact),
        } : {});
      });
    },

    expandedResultArtifacts() {
      const bundle = this.expandedBundle();
      const delivery = (this.expandedCompletion && this.expandedCompletion.delivery) || (bundle && bundle.delivery) || null;
      const items = [];
      if (delivery && Array.isArray(delivery.attachments)) {
        for (const item of delivery.attachments) {
          items.push({
            kind: item.kind,
            name: item.label,
            uri: item.uri,
            content_type: item.content_type,
          });
        }
      }
      items.push(...this.expandedDeliverables());
      const seen = new Set();
      return items.filter(item => {
        const key = String(item.uri || item.name || item.label || item.kind || '').trim();
        if (!key || seen.has(key)) return false;
        seen.add(key);
        return true;
      });
    },

    hasResultReceipt() {
      return Boolean(
        this.expandedResultSummary() ||
        this.expandedOutput() ||
        this.expandedResultArtifacts().length > 0 ||
        this.expandedResultBlocks().length > 0 ||
        this.expandedResultActions().length > 0 ||
        this.expandedSuggestedActions().length > 0
      );
    },

    artifactPreviewText(artifact) {
      if (!artifact || !artifact.metadata) return '';
      return String(artifact.metadata.preview_text || artifact.metadata.summary || artifact.metadata.preview || '').trim();
    },

    resultBlockText(block) {
      if (!block) return '';
      return String(block.content || '').trim();
    },

    resultBlockData(block) {
      if (!block || block.data == null || block.data === '') return '';
      return prettyData(block.data);
    },

    findArtifactByURI(uri) {
      const artifactID = parseArtifactID(uri);
      if (!artifactID || !Array.isArray(this.expandedRunArtifacts)) return null;
      return this.expandedRunArtifacts.find(item => item.id === artifactID) || null;
    },

    deliverablePreviewHref(item) {
      return artifactPreviewPath(item && item.uri);
    },

    resultActionTitle(action) {
      if (!action) return 'Action';
      return String(action.label || suggestedActionLabel(action) || action.kind || 'Action').trim();
    },

    resultActionDescription(action) {
      if (!action) return '';
      const params = action.params ? prettyData(action.params) : '';
      const reason = String(action.reason || '').trim();
      if (reason && params) return reason + '\n' + params;
      return reason || params;
    },

    resultActionHref(action) {
      if (!action) return '';
      const kind = String(action.kind || '').trim();
      const target = String(action.target || '').trim();
      if (!target) return '';
      if (kind === 'open_artifact') return this.deliverablePreviewHref({ uri: target });
      if (kind === 'open_url') return safeExternalURL(target);
      return '';
    },

    canTriggerResultAction(action) {
      if (!action) return false;
      const kind = String(action.kind || '').trim();
      return kind === 'retry_run';
    },

    async triggerResultAction(action) {
      if (!this.expandedRunDetail || !action) return;
      const kind = String(action.kind || '').trim();
      if (kind === 'retry_run') {
        try {
          await submitRetryInteraction(this.expandedRunDetail);
          showToast('Retry submitted', 'success');
          this.loadRuns();
        } catch (err) {
          showToast((err && err.message) || 'Retry failed', 'error');
        }
      }
    },

    formatArgs(args) {
      if (!args) return '';
      return typeof args === 'string' ? args : JSON.stringify(args, null, 2);
    },

    get totalPages() {
      return Math.max(1, Math.ceil(this.filteredRuns.length / Number(this.pageSize)));
    },

    get paginatedRuns() {
      const size = Number(this.pageSize);
      const start = (this.page - 1) * size;
      return this.filteredRuns.slice(start, start + size);
    },

    prevPage() { if (this.page > 1) this.page--; },
    nextPage() { if (this.page < this.totalPages) this.page++; },

    applyFilters() {
      this.filteredRuns = this.runs.filter(run => {
        if (this.filterStatus && (run.display_status || run.status) !== this.filterStatus) return false;
        if (this.filterSession && (run.session_key || '') !== this.filterSession) return false;
        if (this.filterModel) {
          const mq = this.filterModel.toLowerCase();
          if (!(run.model || '').toLowerCase().includes(mq)) return false;
        }
        return true;
      });
      this.page = 1;
    },

    toggleExpand(runId, e) {
      if (e.target.closest('[class*="hc-btn"]')) return;
      if (this.expandedRunId === runId) {
        this.expandedRunId = null;
        this.expandedRunDetail = null;
        this.expandedCompletion = null;
        if (window.location.hash.indexOf(RUN_ROUTE_PREFIX) === 0) {
          window.location.hash = '#/runs';
        }
      } else {
        this.expandedRunId = runId;
        this.loadRunDetail(runId);
        window.location.hash = RUN_ROUTE_PREFIX + encodeURIComponent(runId);
      }
    },

    async cancelRun(runId) {
      try {
        await api.post('/runtime/runs/' + encodeURIComponent(runId) + '/cancel', {});
        showToast(t('runCancelled') || 'Run cancelled', 'success');
        this.loadRuns();
      } catch (_) {}
    },

    async retryRun(run) {
      try {
        await submitRetryInteraction(run);
        showToast(t('runRetried') || 'New run started', 'success');
        this.loadRuns();
      } catch (_) {}
    },

    async loadRuns() {
      this.error = null;
      try {
        const [sessionData, runData] = await Promise.all([
          api.get('/runtime/sessions'),
          api.get('/runtime/runs?include=verification'),
        ]);
        if (!_mounted) return;
        this.sessions = (sessionData.items || []).sort((a, b) =>
          new Date(b.updated_at || b.created_at) - new Date(a.updated_at || a.created_at)
        );
        const sessionIndex = new Map(this.sessions.map(sess => [sess.id || sess.ID, sess]));
        const allRuns = (runData.items || []).map(run => {
          const session = sessionIndex.get(run.session_id || run.SessionID);
          return {
            ...run,
            session_key: run.session_key || run.SessionKey || (session ? (session.key || session.id) : ''),
            session_id: run.session_id || run.SessionID,
            display_status: deriveDisplayStatus(run),
          };
        });
        allRuns.sort((a, b) => new Date(b.created_at || 0) - new Date(a.created_at || 0));
        this.runs = allRuns;
        this.loading = false;
        this.applyFilters();
        const hashRunId = this.runIdFromHash();
        if (hashRunId && this.expandedRunId !== hashRunId) {
          this.expandedRunId = hashRunId;
          this.loadRunDetail(hashRunId);
        }
      } catch (err) {
        if (_mounted) {
          this.loading = false;
          this.error = err.message || String(err);
        }
      }
    },

    async loadRunDetail(runId) {
      try {
        const [run, completion] = await Promise.all([
          api.get('/runtime/runs/' + encodeURIComponent(runId)).catch(() => null),
          api.get('/runtime/runs/' + encodeURIComponent(runId) + '/completion').catch(() => null),
        ]);
        if (!_mounted) return;
        this.expandedRunDetail = run || null;
        this.expandedCompletion = completion || null;
        const artifactsData = await api.get('/operator/artifacts?run_id=' + encodeURIComponent(runId)).catch(() => null);
        this.expandedRunArtifacts = artifactsData ? (Array.isArray(artifactsData) ? artifactsData : (artifactsData.items || [])) : [];
      } catch (_) {
        if (_mounted) {
          this.expandedRunDetail = null;
          this.expandedCompletion = null;
          this.expandedRunArtifacts = [];
        }
      }
    },

    toggleSelectRun(runId) {
      const idx = this.selectedRuns.indexOf(runId);
      if (idx >= 0) {
        this.selectedRuns = this.selectedRuns.filter(id => id !== runId);
      } else {
        this.selectedRuns = [...this.selectedRuns, runId];
      }
    },

    toggleSelectAll() {
      if (this.selectedRuns.length === this.paginatedRuns.length) {
        this.selectedRuns = [];
      } else {
        this.selectedRuns = this.paginatedRuns.map(r => r.id);
      }
    },

    clearSelection() {
      this.selectedRuns = [];
    },

    async batchRetry() {
      const toRetry = this.runs.filter(r => this.selectedRuns.includes(r.id) && (r.status === 'failed' || r.display_status === 'verification_failed'));
      for (const run of toRetry) {
        try {
          await submitRetryInteraction(run);
        } catch (_) {}
      }
      showToast((toRetry.length || 0) + ' runs retried', 'success');
      this.selectedRuns = [];
      this.loadRuns();
    },

    exportSelected() {
      const data = this.runs.filter(r => this.selectedRuns.includes(r.id));
      this.downloadJSON(data, 'runs-export.json');
    },

    exportAllRuns() {
      this.downloadJSON(this.filteredRuns, 'runs-export.json');
    },

    downloadJSON(data, filename) {
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      a.click();
      URL.revokeObjectURL(url);
    },

    // SSE
    startSSE() {
      const generation = ++sseGeneration;
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
      if (sseXhr) {
        try { sseXhr.abort(); } catch (_) {}
        sseXhr = null;
      }
      sseBuf = '';

      const x = new XMLHttpRequest();
      let lastIdx = 0;
      x.open('GET', '/runtime/events/stream', true);

      const tok = getToken();
      if (tok) x.setRequestHeader('Authorization', 'Bearer ' + tok);
      x.setRequestHeader('Accept', 'text/event-stream');
      x.setRequestHeader('Cache-Control', 'no-cache');

      const self = this;
      sseXhr = x;
      x.onreadystatechange = function () {
        if (!_mounted || generation !== sseGeneration || sseXhr !== x) {
          try { x.abort(); } catch (_) {}
          return;
        }
        if (x.readyState >= 3 && x.status === 200) {
          const chunk = x.responseText.substring(lastIdx);
          lastIdx = x.responseText.length;
          self.parseSSE(chunk);
          if (x.responseText.length > SSE_RESPONSE_LIMIT) {
            try { x.abort(); } catch (_) {}
            sseXhr = null;
            if (_mounted && generation === sseGeneration) self.startSSE();
            return;
          }
        }
        if (x.readyState === 4 && _mounted) {
          if (generation !== sseGeneration || sseXhr !== x) return;
          sseXhr = null;
          reconnectTimer = setTimeout(() => {
            if (_mounted && generation === sseGeneration) self.startSSE();
          }, x.status === 429 ? 10000 : SSE_RECONNECT_MS);
        }
      };
      x.onerror = function () {
        if (!_mounted || generation !== sseGeneration || sseXhr !== x) return;
        sseXhr = null;
        reconnectTimer = setTimeout(() => {
          if (_mounted && generation === sseGeneration) self.startSSE();
        }, x.status === 429 ? 10000 : SSE_RECONNECT_MS);
      };
      x.send();
    },

    parseSSE(chunk) {
      sseBuf += chunk;
      const lines = sseBuf.split('\n');
      sseBuf = lines.pop() || '';
      for (const line of lines) {
        const trimmed = line.trim();
        if (trimmed.indexOf('data: ') === 0) {
          try {
            const ev = JSON.parse(trimmed.substring(6));
            if (ev.type === 'run.completed' || ev.type === 'run.failed' || ev.type === 'run.cancelled') {
              this.loadRuns();
            }
          } catch (_) {}
        }
      }
    },

    // Lifecycle
    mounted() {
      _mounted = true;
      this.loadRuns();
      this.startSSE();
      refreshTimer = setInterval(() => {
        const hasActive = this.runs.some(r => r.status === 'running' || r.status === 'queued' || r.status === 'waiting_approval' || r.status === 'waiting_input');
        if (hasActive) this.loadRuns();
      }, AUTO_REFRESH_MS);
    },

    unmounted() {
      _mounted = false;
      sseGeneration++;
      if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null; }
      if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
      if (sseXhr) { try { sseXhr.abort(); } catch (_) {} sseXhr = null; }
    },
  };
}
