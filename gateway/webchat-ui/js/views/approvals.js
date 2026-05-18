// ---------------------------------------------------------------------------
// Approvals View - Petite Vue component
// ---------------------------------------------------------------------------

import { api, consolePath, getToken, showToast } from '../api.js';
import { t } from '../i18n/index.js';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const SSE_RECONNECT_MS = 5000;
const AUTO_REFRESH_MS = 30000;
const SCOPE_OPTIONS = ['once', 'session', 'always'];
const ARGS_TRUNCATE_LEN = 500;
const DEFAULT_PAGE_SIZE = 20;
const SSE_RESPONSE_LIMIT = 512 * 1024;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function fmtTime(iso) {
  try {
    return new Date(iso).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  } catch (_) { return ''; }
}

function capitalise(s) {
  if (!s) return '';
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function extractItems(data) {
  if (!data) return [];
  if (Array.isArray(data)) return data;
  return data.items || data.tickets || [];
}

function approvalTicketID(ticket) {
  return String((ticket && (ticket.id || ticket.ticket_id)) || '').trim();
}

function dedupeStrings(items) {
  const seen = new Set();
  const out = [];
  for (const item of Array.isArray(items) ? items : []) {
    const value = String(item || '').trim();
    if (!value || seen.has(value)) continue;
    seen.add(value);
    out.push(value);
  }
  return out;
}

function approvalToolNames(ticket) {
  const names = [];
  if (ticket && Array.isArray(ticket.tool_calls)) {
    for (const call of ticket.tool_calls) {
      if (call && call.name) names.push(call.name);
    }
  }
  if (ticket && ticket.governance && Array.isArray(ticket.governance.tool_names)) {
    names.push(...ticket.governance.tool_names);
  }
  if (ticket && ticket.tool_name) names.push(ticket.tool_name);
  if (ticket && ticket.tool) names.push(ticket.tool);
  return dedupeStrings(names);
}

function approvalPrimaryTool(ticket) {
  const tools = approvalToolNames(ticket);
  if (tools.length > 0) return tools[0];
  return approvalTicketID(ticket) || (t('approvalsUnknownTool') || 'Unknown Tool');
}

function approvalArguments(ticket) {
  if (!ticket) return null;
  if (ticket.arguments != null) return ticket.arguments;
  if (ticket.args != null) return ticket.args;
  if (Array.isArray(ticket.tool_calls)) {
    for (const call of ticket.tool_calls) {
      if (call && call.input != null) return call.input;
    }
  }
  return null;
}

function approvalReasons(ticket) {
  const reasons = [];
  if (ticket && Array.isArray(ticket.reasons)) reasons.push(...ticket.reasons);
  if (ticket && ticket.governance && ticket.governance.policy && Array.isArray(ticket.governance.policy.reasons)) {
    reasons.push(...ticket.governance.policy.reasons);
  }
  return dedupeStrings(reasons);
}

function approvalSummary(ticket) {
  if (!ticket) return '';
  return String(
    ticket.resource_scope_summary ||
    (ticket.governance && ticket.governance.summary) ||
    (ticket.governance && ticket.governance.policy && ticket.governance.policy.summary) ||
    ''
  ).trim();
}

function approvalFromEvent(ev) {
  const attrs = (ev && ev.attrs) || {};
  const toolNames = dedupeStrings([
    ...(Array.isArray(attrs.tool_names) ? attrs.tool_names : []),
    ...(Array.isArray(attrs.policy_tool_names) ? attrs.policy_tool_names : []),
    attrs.tool_name || '',
  ]);
  const policyReasons = dedupeStrings([
    ...(Array.isArray(attrs.policy_reasons) ? attrs.policy_reasons : []),
    ...(Array.isArray(attrs.reasons) ? attrs.reasons : []),
  ]);
  const governance = {
    tool_names: toolNames,
    summary: String(attrs.summary || attrs.policy_summary || '').trim(),
    policy: {
      summary: String(attrs.policy_summary || '').trim(),
      reasons: policyReasons,
    },
  };
  return {
    id: attrs.approval_id || attrs.ticket_id || attrs.id || '',
    tool_name: String(attrs.tool_name || '').trim(),
    arguments: attrs.arguments != null ? attrs.arguments : null,
    reasons: Array.isArray(attrs.reasons) ? attrs.reasons : [],
    session_id: String(ev && ev.session_id || attrs.session_id || '').trim(),
    session_key: String(attrs.session_key || '').trim(),
    run_id: String(ev && ev.run_id || attrs.run_id || '').trim(),
    created_at: String(attrs.created_at || '').trim() || new Date().toISOString(),
    status: String(attrs.status || 'pending').trim(),
    tool_calls: Array.isArray(attrs.tool_calls) ? attrs.tool_calls : [],
    governance,
    resource_scope_summary: String(attrs.resource_scope_summary || '').trim(),
  };
}

// ---------------------------------------------------------------------------
// Risk Level Helpers
// ---------------------------------------------------------------------------

const HIGH_RISK_TOOLS = ['bash', 'shell', 'exec', 'rm', 'delete', 'drop', 'destroy', 'deploy', 'push'];
const MEDIUM_RISK_TOOLS = ['write', 'edit', 'create', 'update', 'patch', 'post', 'send'];

function deriveRiskLevel(ticket) {
  const tool = approvalPrimaryTool(ticket).toLowerCase();
  const reasons = approvalReasons(ticket);
  if (HIGH_RISK_TOOLS.some(t => tool.includes(t))) return 'high';
  if (reasons.some(r => /destruct|danger|irrevers|production/i.test(r))) return 'high';
  if (MEDIUM_RISK_TOOLS.some(t => tool.includes(t))) return 'medium';
  if (reasons.length > 0) return 'medium';
  return 'low';
}

function riskBadgeClass(level) {
  if (level === 'high') return 'hc-badge-red';
  if (level === 'medium') return 'hc-badge-orange';
  return 'hc-badge-green';
}

// ---------------------------------------------------------------------------
// Approvals View component
// ---------------------------------------------------------------------------

export function ApprovalsView() {
  let _mounted = false;
  let sseXhr = null;
  let sseBuf = '';
  let refreshTimer = null;
  let reconnectTimer = null;
  let sseGeneration = 0;

  const $template = `
    <div class="hc-approvals">
      <div class="hc-approvals-header" style="display:flex;align-items:center;gap:12px">
        <h2 class="hc-page-title">{{ t('approvalsTitle') }}</h2>
        <button class="hc-btn hc-btn-sm hc-btn-ghost" style="margin-left:auto" @click="loadApprovals()">{{ t('refresh') }}</button>
        <a href="#/settings/security" style="font-size:0.82rem;color:var(--accent);text-decoration:none">{{ t('approvalsPolicies') || 'Manage Policies' }} &rarr;</a>
      </div>

      <!-- Tabs -->
      <div class="hc-tabs">
        <button class="hc-tab" data-testid="approvals-tab-pending" :class="{ active: tab === 'pending' }" @click="tab = 'pending'">
          {{ t('approvalsPending') }}
          <span v-if="pending.length > 0" class="hc-tab-badge">{{ pending.length }}</span>
        </button>
        <button class="hc-tab" data-testid="approvals-tab-resolved" :class="{ active: tab === 'resolved' }" @click="tab = 'resolved'">
          {{ t('resolved') }}
        </button>
      </div>

      <div v-if="error" class="hc-empty">
        <p>{{ t('loadError') }}</p>
        <p style="color:var(--text-muted);font-size:0.85rem">{{ error }}</p>
        <button class="hc-btn hc-btn-secondary" style="margin-top:8px" @click="loadApprovals()">{{ t('retryLoad') }}</button>
      </div>
      <div v-else-if="loading" class="hc-loading">{{ t('loading') }}</div>

      <!-- Pending tab -->
      <div v-else-if="tab === 'pending'">
        <div v-if="pending.length === 0" class="hc-empty" data-testid="approvals-empty-pending">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="48" height="48"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>
          <p>{{ t('approvalsNoItems') }}</p>
        </div>
        <template v-else>
        <!-- Batch controls -->
        <div style="display:flex;align-items:center;gap:12px;margin-bottom:12px">
          <label style="display:flex;align-items:center;gap:6px;font-size:0.82rem;cursor:pointer">
            <input type="checkbox" data-testid="approvals-batch-mode" v-model="batchMode" @change="selectedTickets = []" />
            {{ t('batchMode') || 'Batch Mode' }}
          </label>
          <template v-if="batchMode && selectedTickets.length > 0">
            <span style="font-size:0.82rem;color:var(--ink)">{{ selectedTickets.length }} {{ t('selected') || 'selected' }}</span>
            <button class="hc-btn hc-btn-sm hc-btn-success" data-testid="approvals-batch-approve" @click="batchApprove()">{{ t('approvalsApprove') || 'Approve' }} ({{ selectedTickets.length }})</button>
            <button class="hc-btn hc-btn-sm hc-btn-danger" data-testid="approvals-batch-deny" @click="batchDeny()">{{ t('approvalsDeny') || 'Deny' }} ({{ selectedTickets.length }})</button>
          </template>
        </div>
          <div class="hc-approval-cards">
          <div v-for="ticket in pending" :key="approvalTicketID(ticket)" class="hc-approval-card" :data-testid="'approval-card-' + approvalTicketID(ticket)">
            <!-- Batch checkbox -->
            <div v-if="batchMode" style="margin-bottom:8px">
              <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
                <input type="checkbox" :data-testid="'approval-select-' + approvalTicketID(ticket)" :checked="selectedTickets.includes(approvalTicketID(ticket))" @change="toggleSelectTicket(approvalTicketID(ticket))" />
                <span style="font-size:0.82rem">{{ t('select') || 'Select' }}</span>
              </label>
            </div>
            <!-- Header -->
            <div class="hc-approval-card-header">
              <div class="hc-approval-card-tool">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>
                <span>{{ approvalPrimaryTool(ticket) }}</span>
                <span class="hc-badge" :class="riskBadgeClass(deriveRiskLevel(ticket))" style="font-size:0.72rem;margin-left:8px">
                  {{ deriveRiskLevel(ticket) === 'high' ? (t('approvalsHighRisk') || 'High Risk') : deriveRiskLevel(ticket) === 'medium' ? (t('approvalsMediumRisk') || 'Medium') : (t('approvalsLowRisk') || 'Low') }}
                </span>
              </div>
              <span v-if="ticket.created_at" class="hc-approval-card-time">{{ fmtTime(ticket.created_at) }}</span>
            </div>
            <!-- Arguments -->
            <pre v-if="approvalArguments(ticket)" class="hc-approval-card-args">{{ truncateArgs(approvalArguments(ticket)) }}</pre>
            <!-- Session -->
            <div v-if="ticket.session_id || ticket.session_key" class="hc-approval-card-session">
              <span class="hc-approval-card-label">{{ t('approvalsSession') }}:</span>
              {{ ticket.session_key || ticket.session_id || '' }}
            </div>
            <!-- Context -->
            <div class="hc-approval-card-context" style="font-size:0.82rem;color:var(--ink3);margin:8px 0;display:flex;flex-wrap:wrap;gap:12px">
              <span v-if="ticket.run_id">{{ t('approvalsRun') || 'Run' }}: <a :href="'#/runs'" style="color:var(--accent)">{{ (ticket.run_id || '').substring(0, 12) }}</a></span>
              <span v-if="ticket.agent">{{ t('approvalsAgent') || 'Agent' }}: {{ ticket.agent }}</span>
              <span v-if="ticket.initiator || ticket.user">{{ t('approvalsBy') || 'By' }}: {{ ticket.initiator || ticket.user || 'system' }}</span>
            </div>
            <div v-if="approvalSummary(ticket)" class="hc-approval-card-context" style="font-size:0.82rem;color:var(--ink3);margin:8px 0">
              {{ approvalSummary(ticket) }}
            </div>
            <!-- Reasons -->
            <div v-if="approvalReasons(ticket).length > 0" class="hc-approval-card-reasons">
              <span class="hc-approval-card-label">{{ t('approvalsReason') }}:</span>
              <ul><li v-for="r in approvalReasons(ticket)">{{ r }}</li></ul>
            </div>
            <!-- Scope -->
            <div class="hc-approval-card-scope">
              <span class="hc-approval-card-label">{{ t('scope') }}:</span>
              <div class="hc-scope-btns">
                <button v-for="s in scopeOptions" :key="s"
                  :data-testid="'approval-scope-' + s + '-' + approvalTicketID(ticket)"
                  class="hc-scope-btn" :class="{ active: getScope(ticket) === s }"
                  @click="setScope(ticket, s)">
                  {{ capitalise(s) }}
                </button>
              </div>
            </div>
            <!-- Actions -->
            <div class="hc-approval-card-actions">
              <button class="hc-btn hc-btn-success" :data-testid="'approval-approve-' + approvalTicketID(ticket)" @click="resolveTicket(ticket, 'approved')">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><polyline points="20 6 9 17 4 12"/></svg>
                {{ t('approvalsApprove') }}
              </button>
              <button class="hc-btn hc-btn-danger" :data-testid="'approval-deny-' + approvalTicketID(ticket)" @click="resolveTicket(ticket, 'denied')">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                {{ t('approvalsDeny') }}
              </button>
              <button class="hc-btn hc-btn-ghost" :data-testid="'approval-cancel-' + approvalTicketID(ticket)" @click="cancelTicket(ticket)">{{ t('cancel') }}</button>
            </div>
          </div>
        </div>
        </template>
      </div>

      <!-- Resolved tab -->
      <div v-else>
        <div v-if="resolved.length === 0" class="hc-empty">{{ t('noData') }}</div>
        <div v-else class="hc-table-wrap">
          <table class="hc-table">
            <thead><tr>
              <th>{{ t('approvalsToolName') }}</th>
              <th>{{ t('riskLevel') || 'Risk' }}</th>
              <th>{{ t('status') }}</th>
              <th>{{ t('scope') || 'Scope' }}</th>
              <th>{{ t('approvalsResolvedBy') }}</th>
              <th>{{ t('approvalsSession') }}</th>
              <th>{{ t('approvalsTime') }}</th>
            </tr></thead>
            <tbody>
              <tr v-for="ticket in paginatedResolved" :key="ticket.id">
                <td>{{ approvalPrimaryTool(ticket) }}</td>
                <td><span class="hc-badge" :class="riskBadgeClass(deriveRiskLevel(ticket))">{{ capitalise(deriveRiskLevel(ticket)) }}</span></td>
                <td><span class="hc-badge" :class="resolutionBadge(ticket)">{{ capitalise(ticket.resolution || ticket.status || 'resolved') }}</span></td>
                <td>{{ ticket.scope || 'once' }}</td>
                <td>{{ ticket.resolved_by || ticket.resolver || '-' }}</td>
                <td>{{ ticket.session_key || ticket.session_id || '-' }}</td>
                <td>{{ fmtTime(ticket.resolved_at || ticket.updated_at || '') }}</td>
              </tr>
            </tbody>
          </table>

          <div v-if="resolvedTotalPages > 1" class="hc-pagination">
            <span class="hc-pagination-info">{{ (resolvedPage-1)*Number(resolvedPageSize)+1 }}-{{ Math.min(resolvedPage*Number(resolvedPageSize), resolved.length) }} {{ t('of') }} {{ resolved.length }}</span>
            <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="resolvedPage<=1" @click="resolvedPrevPage()">&#8249;</button>
            <span class="hc-pagination-pages">{{ resolvedPage }} / {{ resolvedTotalPages }}</span>
            <button class="hc-btn hc-btn-sm hc-btn-ghost" :disabled="resolvedPage>=resolvedTotalPages" @click="resolvedNextPage()">&#8250;</button>
            <select class="hc-form-select hc-pagination-size" v-model="resolvedPageSize" @change="resolvedPage=1">
              <option value="20">20</option>
              <option value="50">50</option>
              <option value="100">100</option>
            </select>
          </div>
        </div>
      </div>
    </div>
  `;

  return {
    $template,
    t,
    scopeOptions: SCOPE_OPTIONS,

    tab: 'pending',
    pending: [],
    resolved: [],
    loading: true,
    error: null,
    selectedScope: {},
    resolvedPage: 1,
    resolvedPageSize: DEFAULT_PAGE_SIZE,
    batchMode: false,
    selectedTickets: [],

    // Helpers
    fmtTime,
    capitalise,
    deriveRiskLevel,
    riskBadgeClass,
    approvalTicketID,
    approvalPrimaryTool,
    approvalArguments,
    approvalReasons,
    approvalSummary,
    HIGH_RISK_TOOLS,
    MEDIUM_RISK_TOOLS,

    get resolvedTotalPages() {
      return Math.max(1, Math.ceil(this.resolved.length / Number(this.resolvedPageSize)));
    },

    get paginatedResolved() {
      const size = Number(this.resolvedPageSize);
      const start = (this.resolvedPage - 1) * size;
      return this.resolved.slice(start, start + size);
    },

    resolvedPrevPage() { if (this.resolvedPage > 1) this.resolvedPage--; },
    resolvedNextPage() { if (this.resolvedPage < this.resolvedTotalPages) this.resolvedPage++; },

    getScope(ticket) {
      const id = ticket.id || ticket.ticket_id || '';
      return this.selectedScope[id] || 'once';
    },

    setScope(ticket, scope) {
      const id = ticket.id || ticket.ticket_id || '';
      this.selectedScope = { ...this.selectedScope, [id]: scope };
    },

    truncateArgs(args) {
      let str = typeof args === 'string' ? args : JSON.stringify(args, null, 2);
      if (str.length > ARGS_TRUNCATE_LEN) str = str.substring(0, ARGS_TRUNCATE_LEN) + '...';
      return str;
    },

    resolutionBadge(ticket) {
      const r = ticket.resolution || ticket.status || 'resolved';
      if (r === 'approved') return 'hc-badge-green';
      if (r === 'denied') return 'hc-badge-red';
      return 'hc-badge-gray';
    },

    async resolveTicket(ticket, status) {
      const id = ticket.id || ticket.ticket_id || '';
      const scope = this.selectedScope[id] || 'once';
      try {
        await api.post('/operator/approvals/' + encodeURIComponent(id) + '/resolve', { status, scope, by: 'operator' });
        showToast(status === 'approved' ? (t('approvalsApproved') || 'Approved') : (t('approvalsDenied') || 'Denied'), 'success');
        this.loadApprovals();
      } catch (_) {}
    },

    async cancelTicket(ticket) {
      const id = ticket.id || ticket.ticket_id || '';
      try {
        await api.post('/operator/approvals/' + encodeURIComponent(id) + '/cancel', {});
        showToast(t('approvalsCancelled') || 'Cancelled', 'info');
        this.loadApprovals();
      } catch (_) {}
    },

    toggleSelectTicket(id) {
      const idx = this.selectedTickets.indexOf(id);
      if (idx >= 0) {
        this.selectedTickets = this.selectedTickets.filter(x => x !== id);
      } else {
        this.selectedTickets = [...this.selectedTickets, id];
      }
    },

    async batchApprove() {
      for (const id of this.selectedTickets) {
        const scope = this.selectedScope[id] || 'once';
        try {
          await api.post('/operator/approvals/' + encodeURIComponent(id) + '/resolve', { status: 'approved', scope, by: 'operator' });
        } catch (_) {}
      }
      showToast(this.selectedTickets.length + ' ' + (t('approvalsBatchApproved') || 'approved'), 'success');
      this.selectedTickets = [];
      this.loadApprovals();
    },

    async batchDeny() {
      for (const id of this.selectedTickets) {
        try {
          await api.post('/operator/approvals/' + encodeURIComponent(id) + '/resolve', { status: 'denied', by: 'operator' });
        } catch (_) {}
      }
      showToast(this.selectedTickets.length + ' ' + (t('approvalsBatchDenied') || 'denied'), 'success');
      this.selectedTickets = [];
      this.loadApprovals();
    },

    async loadApprovals() {
      this.error = null;
      try {
        const [pendingData, approvedData, deniedData] = await Promise.all([
          api.get('/operator/approvals?status=pending').catch(() => null),
          api.get('/operator/approvals?status=approved').catch(() => null),
          api.get('/operator/approvals?status=denied').catch(() => null),
        ]);
        if (!_mounted) return;
        this.pending = extractItems(pendingData);
        this.resolved = [
          ...extractItems(approvedData),
          ...extractItems(deniedData),
        ].sort((a, b) =>
          new Date(b.resolved_at || b.updated_at || 0) - new Date(a.resolved_at || a.updated_at || 0)
        );
        this.loading = false;
      } catch (err) {
        if (_mounted) {
          this.loading = false;
          this.error = err.message || String(err);
        }
      }
    },

    shouldAutoRefresh() {
      if (document.hidden) return false;
      if (this.batchMode) return false;
      if (this.selectedTickets.length > 0) return false;
      return true;
    },

    // Browser notifications
    requestNotificationPermission() {
      if ('Notification' in window && Notification.permission === 'default') {
        Notification.requestPermission();
      }
    },

    notifyNewApproval(ticket) {
      if ('Notification' in window && Notification.permission === 'granted') {
        const n = new Notification('Approval Required', {
          body: 'Tool: ' + approvalPrimaryTool(ticket),
          icon: consolePath('/icon-192.svg'),
          tag: 'hc-approval-' + approvalTicketID(ticket),
        });
        n.onclick = () => {
          window.focus();
          this.tab = 'pending';
        };
      }
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
            const eventType = String((ev && ev.type) || '').trim();
            if ((eventType === 'approval.requested' || eventType === 'approval_requested') && ev.attrs) {
              const ticket = approvalFromEvent(ev);
              const ticketID = approvalTicketID(ticket);
              if (!this.pending.find(p => approvalTicketID(p) === ticketID)) {
                this.notifyNewApproval(ticket);
                if (this.shouldAutoRefresh()) {
                  this.loadApprovals();
                } else if (ticketID) {
                  this.pending = [ticket, ...this.pending];
                }
              }
            }
            if (eventType === 'approval.resolved' || eventType === 'approval_resolved') {
              if (this.shouldAutoRefresh()) {
                this.loadApprovals();
              } else {
                const ticketID = String((ev.attrs && (ev.attrs.approval_id || ev.attrs.ticket_id || ev.attrs.id)) || '').trim();
                if (ticketID) {
                  this.pending = this.pending.filter(ticket => approvalTicketID(ticket) !== ticketID);
                }
              }
            }
          } catch (_) {}
        }
      }
    },

    // Lifecycle
    mounted() {
      _mounted = true;
      this.loadApprovals();
      this.startSSE();
      this.requestNotificationPermission();
      refreshTimer = setInterval(() => {
        if (_mounted && this.shouldAutoRefresh()) this.loadApprovals();
      }, AUTO_REFRESH_MS);
    },

    unmounted() {
      _mounted = false;
      sseGeneration++;
      if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
      if (sseXhr) { try { sseXhr.abort(); } catch (_) {} sseXhr = null; }
      if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null; }
    },
  };
}
