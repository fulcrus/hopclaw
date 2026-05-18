// ---------------------------------------------------------------------------
// Chat View - Petite Vue component
// ---------------------------------------------------------------------------

import { api, consolePath, getToken, showToast } from '../api.js';
import { t } from '../i18n/index.js';
import { renderMarkdown } from '../markdown.js';
import { artifactPreviewPath, parseArtifactID, safeExternalURL } from '../linking.js';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const RECONNECT_DELAY_MS = 3000;
const MAX_RECONNECT_ATTEMPTS = 30;
const SCROLL_THRESHOLD_PX = 100;
const SESSION_COUNTER_KEY = 'hc_session_counter';
const SESSION_KEY_STORAGE = 'hc_session_key';
const MAX_RECEIPT_DELIVERABLES = 3;
const SESSION_LIST_CACHE_TTL_MS = 3000;
const SSE_RESPONSE_LIMIT = 512 * 1024;

let sessionListLoadedAt = 0;
let sessionListInflight = null;

function receiptOutcomeTitles() {
  return {
    completed: t('chatReceiptCompleted') || 'Execution receipt',
    partial: t('chatReceiptPartial') || 'Result needs review',
    needs_confirmation: t('chatReceiptNeedsConfirmation') || 'Waiting for your confirmation',
    failed: t('chatReceiptFailed') || 'Run failed',
    cancelled: t('chatReceiptCancelled') || 'Run cancelled',
  };
}

function receiptActionLabelsMap() {
  return {
    open_deliverables: t('chatReceiptActionOpenDeliverables') || 'Open deliverables',
    review_result: t('chatReceiptActionReviewResult') || 'Review result',
    retry_run: t('chatReceiptActionRetryRun') || 'Retry run',
    review_approval: t('chatReceiptActionReviewApproval') || 'Review approval',
    provide_input: t('chatReceiptActionProvideInput') || 'Provide input',
    inspect_verification: t('chatReceiptActionInspectVerification') || 'Inspect verification',
  };
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

function esc(s) {
  if (!s) return '';
  const div = document.createElement('div');
  div.appendChild(document.createTextNode(s));
  return div.innerHTML;
}

function fmtTime(iso) {
  try { return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }); }
  catch (_) { return ''; }
}

function fmtDate(iso) {
  try { return new Date(iso).toLocaleDateString([], { month: 'short', day: 'numeric' }); }
  catch (_) { return ''; }
}

function formatFileSize(bytes) {
  if (!Number.isFinite(Number(bytes)) || Number(bytes) <= 0) return '';
  const units = ['B', 'KB', 'MB', 'GB'];
  let unitIdx = 0;
  let size = Number(bytes);
  while (size >= 1024 && unitIdx < units.length - 1) {
    size /= 1024;
    unitIdx++;
  }
  return size.toFixed(unitIdx === 0 ? 0 : 1) + ' ' + units[unitIdx];
}

function formatTitleCase(value) {
  if (!value) return '';
  return String(value).replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function receiptActionLabel(action) {
  if (!action) return '';
  return receiptActionLabelsMap()[action.kind] || action.label || formatTitleCase(action.kind);
}

function completionBundle(completion) {
  return completion && completion.bundle ? completion.bundle : null;
}

function completionResult(completion) {
  return completion && completion.result ? completion.result : null;
}

function completionVerification(completion) {
  return completion && completion.verification ? completion.verification : null;
}

function buildResultCard(runId, completion) {
  const bundle = completionBundle(completion);
  const result = completionResult(completion);
  const verification = completionVerification(completion);
  const deliverables = bundle && Array.isArray(bundle.deliverables) ? bundle.deliverables : [];
  const actions = bundle && Array.isArray(bundle.suggested_actions) ? bundle.suggested_actions : [];
  const outcome = String((bundle && bundle.outcome) || (completion && completion.outcome) || '').trim();
  const verificationData = (bundle && bundle.verification) || verification || null;
  const verificationStatus = String((verificationData && verificationData.status) || '').trim();
  const verificationSummary = String((verificationData && verificationData.summary) || '').trim();
  const summary = String((bundle && bundle.summary) || (result && result.summary) || '').trim();
  const updatedAt = String((bundle && bundle.updated_at) || (completion && completion.updated_at) || '').trim();
  const hasCardContent = summary || deliverables.length > 0 || actions.length > 0 || verificationStatus || updatedAt;
  if (!hasCardContent) return null;
  return {
    runId,
    title: receiptOutcomeTitles()[outcome] || (t('chatReceiptDefault') || 'Run receipt'),
    outcome,
    summary,
    verificationStatus,
    verificationSummary,
    deliverables: deliverables.slice(0, MAX_RECEIPT_DELIVERABLES).filter(Boolean).map(item => ({
      kind: item.kind || 'artifact',
      name: item.name || item.kind || 'deliverable',
      contentType: item.content_type || '',
      size: formatFileSize(item.size_bytes),
      previewText: item.preview_text || '',
      artifactId: parseArtifactID(item.uri),
      uri: item.uri || '',
    })),
    suggestedActions: actions.filter(Boolean).map(action => ({
      kind: action.kind || '',
      label: receiptActionLabel(action),
      reason: action.reason || '',
    })),
    updatedAt,
  };
}

function extractItems(data) {
  if (!data) return [];
  if (Array.isArray(data)) return data;
  return Array.isArray(data.items) ? data.items : [];
}

function shortID(value) {
  const text = String(value || '').trim();
  if (!text) return '-';
  if (text.length <= 12) return text;
  return text.slice(0, 12);
}

function retryInteractionPayloadForRun(sessionKey, runID) {
  const trimmedRunID = String(runID || '').trim();
  return {
    session_key: String(sessionKey || '').trim(),
    content: '',
    parent_run_id: trimmedRunID,
    structured_command: {
      kind: 'retry',
      run_id: trimmedRunID,
    },
  };
}

function newInteractionTurnId() {
  if (window.crypto && typeof window.crypto.randomUUID === 'function') {
    return 'turn-' + window.crypto.randomUUID();
  }
  return 'turn-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10);
}

function buildInteractionMetadata(turnId) {
  const metadata = {
    channel: 'webchat',
    chat_type: 'direct',
  };
  if (turnId) metadata.interaction_turn_id = String(turnId).trim();
  return metadata;
}

function buildSubmissionContentBlocks(text, attachmentBlocks) {
  const blocks = [];
  const trimmedText = String(text || '').trim();
  if (trimmedText) {
    blocks.push({ type: 'text', text: trimmedText });
  }
  for (const block of Array.isArray(attachmentBlocks) ? attachmentBlocks : []) {
    if (block) blocks.push(block);
  }
  return blocks;
}

function attachmentPreviewText(file) {
  if (!file) return '';
  const name = String(file.name || '').trim() || 'unnamed';
  const type = String(file.type || '').toLowerCase();
  if (type.startsWith('image/')) return '已附图片：' + name;
  return '已附文件：' + name;
}

function newestTime(item) {
  return String(
    (item && (item.updated_at || item.finished_at || item.started_at || item.created_at || item.time)) || ''
  ).trim();
}

function sortNewest(items) {
  return (items || []).slice().sort((left, right) => {
    return new Date(newestTime(right)).getTime() - new Date(newestTime(left)).getTime();
  });
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

function approvalTicketID(ticket) {
  return String((ticket && (ticket.id || ticket.ticket_id)) || '').trim();
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
  return approvalTicketID(ticket) || (t('approvalRequired') || 'Approval Required');
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

function buildApprovalPopup(ticket) {
  if (!ticket) return null;
  return {
    id: approvalTicketID(ticket),
    tool_name: approvalPrimaryTool(ticket),
    arguments: approvalArguments(ticket),
    reasons: approvalReasons(ticket),
    summary: approvalSummary(ticket),
    run_id: String(ticket.run_id || '').trim(),
    session_id: String(ticket.session_id || '').trim(),
  };
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
    run_id: String(ev && ev.run_id || attrs.run_id || '').trim(),
    created_at: String(attrs.created_at || '').trim(),
    tool_calls: Array.isArray(attrs.tool_calls) ? attrs.tool_calls : [],
    governance,
    resource_scope_summary: String(attrs.resource_scope_summary || '').trim(),
  };
}

async function fetchSharedSessionList(options = {}) {
  const force = options && options.force === true;
  const silent = !options || options.silent !== false;
  const sharedStore = window._hcStore;
  const cached = sharedStore && Array.isArray(sharedStore.sessionList) ? sharedStore.sessionList.slice() : [];
  const now = Date.now();

  if (!force && now - sessionListLoadedAt < SESSION_LIST_CACHE_TTL_MS) {
    return cached;
  }
  if (sessionListInflight) {
    return sessionListInflight;
  }

  sessionListInflight = api.get('/runtime/sessions', { silent })
    .then(data => {
      const items = sortNewest(extractItems(data));
      if (sharedStore) {
        sharedStore.sessionList = items;
      }
      sessionListLoadedAt = Date.now();
      return items;
    })
    .finally(() => {
      sessionListInflight = null;
    });

  return sessionListInflight;
}

function workspaceRunState(run) {
  if (!run) return '';
  const status = String(run.display_status || run.status || '').trim().toLowerCase();
  const verification = String(run.verification_status || '').trim().toLowerCase();
  if (status === 'completed' && verification === 'failed') return 'verification_failed';
  if (status === 'completed' && verification === 'warning') return 'completed_warning';
  return status;
}

function workspaceRunBadgeClass(run) {
  switch (workspaceRunState(run)) {
    case 'completed':
      return 'hc-badge-green';
    case 'completed_warning':
    case 'partial':
    case 'waiting_approval':
    case 'waiting_input':
    case 'queued':
    case 'running':
      return 'hc-badge-orange';
    case 'failed':
    case 'verification_failed':
      return 'hc-badge-red';
    case 'cancelled':
      return 'hc-badge-gray';
    default:
      return 'hc-badge-gray';
  }
}

function workspaceTaskBadgeClass(status) {
  switch (String(status || '').trim().toLowerCase()) {
    case 'completed':
      return 'hc-badge-green';
    case 'running':
      return 'hc-badge-orange';
    case 'failed':
      return 'hc-badge-red';
    case 'cancelled':
    case 'skipped':
      return 'hc-badge-gray';
    default:
      return 'hc-badge-blue';
  }
}

function workspaceVerificationBadgeClass(status) {
  switch (String(status || '').trim().toLowerCase()) {
    case 'passed':
      return 'hc-badge-green';
    case 'warning':
      return 'hc-badge-orange';
    case 'failed':
      return 'hc-badge-red';
    default:
      return 'hc-badge-gray';
  }
}

const BOT_AVATAR_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" width="16" height="16"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>';

function buildPreflightNotice(run) {
  const preflight = run && run.preflight;
  if (!preflight || !preflight.state || preflight.state === 'ready') return '';
  const checks = Array.isArray(preflight.checks) ? preflight.checks : [];
  const parts = [];
  for (const check of checks) {
    if (check.id === 'reference_gap') parts.push('缺少明确的文件、链接、截图或仓库引用。');
    else if (check.id === 'auto_prepare') parts.push('系统正在自动准备相关运行能力并验证后续交付。');
    else if (check.id === 'expected_confirmation') parts.push('这类任务可能会改动文件、应用或外部系统，执行时通常需要确认。');
  }
  const summary = parts.join('') || (preflight.summary || '').trim();
  let prompt = (preflight.prompt || '').trim();
  if (preflight.state === 'needs_confirmation') {
    if (checks.some(check => check.id === 'reference_gap')) prompt = '请直接补充具体的文件路径、网址、截图、仓库链接或目标对象。';
    else if (checks.some(check => check.id === 'expected_confirmation')) prompt = '如果确认继续，请直接回复确认，或补充更明确的范围。';
  }
  if (preflight.state === 'needs_confirmation') {
    return (summary + (prompt ? ' ' + prompt : '')).trim() || '前置条件需要确认后再继续。';
  }
  if (preflight.state === 'auto_preparing') {
    return summary || '系统正在自动准备执行前置条件。';
  }
  return '';
}

function buildPreflightReplyTemplate(preflight) {
  if (!preflight) return '';
  if (preflight.reply_template) return String(preflight.reply_template).trim();
  const slots = Array.isArray(preflight.clarification_slots) ? preflight.clarification_slots : [];
  if (slots.length) {
    return slots.map(slot => {
      const label = slot.label || formatTitleCase(slot.id || '补充信息');
      const placeholder = slot.placeholder || '<请补充>';
      return label + '：' + placeholder;
    }).join('\n');
  }
  if (!Array.isArray(preflight.checks) || !preflight.checks.length) return '';
  const ids = new Set(preflight.checks.map(check => String((check && check.id) || '').trim()).filter(Boolean));
  const lines = [];
  if (ids.has('reference_gap') || ids.has('source_target')) {
    lines.push('目标对象：<文件路径 / URL / 仓库 / 截图>');
  }
  if (ids.has('delivery_target')) {
    lines.push('发送位置：<邮箱 / Slack 频道 / 飞书群 / 当前会话>');
  }
  if (ids.has('schedule')) {
    lines.push('执行时间：<例如 从下周一开始，每天 09:00>');
  }
  if (ids.has('deployment_target')) {
    lines.push('部署目标：<staging / prod / 服务名 / URL>');
  }
  if (ids.has('expected_confirmation')) {
    lines.push('确认：继续');
  }
  if (!lines.length) return '';
  return lines.join('\n');
}

function buildPreflightCard(run) {
  const preflight = run && run.preflight;
  if (!preflight || !preflight.state || preflight.state === 'ready') return null;
  const checks = Array.isArray(preflight.checks) ? preflight.checks : [];
  const examples = Array.isArray(preflight.reply_hints) ? preflight.reply_hints.slice() : [];
  const slots = Array.isArray(preflight.clarification_slots) ? preflight.clarification_slots : [];
  const hasReferenceGap = checks.some(check => check.id === 'reference_gap');
  const hasExpectedConfirmation = checks.some(check => check.id === 'expected_confirmation');
  let question = (preflight.question || '').trim();
  let next = (preflight.continue_hint || '').trim();
  if (hasReferenceGap) {
    question = '补充具体的文件路径、网址、截图、仓库链接或目标对象。';
  } else if (hasExpectedConfirmation) {
    question = '回复确认继续，或补充更明确的执行范围。';
  }
  if (!next) {
    next = preflight.state === 'needs_confirmation'
      ? '我会继续当前任务，不用你从头再说一遍，并在回复前验证结果。'
      : '系统准备完成后会自动继续。';
  }
  const replyTemplate = buildPreflightReplyTemplate(preflight);
  const actions = [];
  if (replyTemplate) {
    actions.push({ label: '填入模板', value: replyTemplate, sendNow: false, kind: 'secondary' });
  }
  if (hasExpectedConfirmation) {
    actions.push({ label: '一键继续', value: '确认，继续', sendNow: true, kind: 'primary' });
  }
  return {
    state: preflight.state,
    title: preflight.state === 'needs_confirmation' ? '需要你补充后继续' : '系统正在自动准备',
    summary: buildPreflightNotice(run),
    question,
    slots: slots.map(slot => ({
      label: slot.label || formatTitleCase(slot.id || '补充信息'),
      question: slot.question || '',
      placeholder: slot.placeholder || '',
      inputMode: slot.input_mode || '',
      required: Boolean(slot.required),
      hints: Array.isArray(slot.hints) ? slot.hints : [],
    })),
    examples: examples.map(item => {
      if (item === 'Confirm and continue') return '确认，继续';
      if (item === 'Continue, but only change README.md') return '继续，但只改 README.md';
      return item;
    }),
    replyTemplate,
    actions,
    next,
  };
}

function taskContractGoal(contract) {
  return String((contract && contract.goal) || '').trim();
}

function taskContractTargetSummary(contract) {
  return String((contract && contract.target_summary) || '').trim();
}

function taskContractJobType(contract) {
  return formatTitleCase((contract && contract.job_type) || '');
}

function taskContractConfidenceText(contract) {
  const value = Number(contract && contract.confidence);
  if (!Number.isFinite(value) || value <= 0) return '';
  return 'Confidence ' + value.toFixed(2);
}

function taskContractSuggestedDomains(contract) {
  return contract && Array.isArray(contract.suggested_domains) ? contract.suggested_domains : [];
}

function taskContractMissingInfo(contract) {
  return contract && Array.isArray(contract.missing_info) ? contract.missing_info : [];
}

function taskContractDeliverables(contract) {
  return contract && Array.isArray(contract.expected_deliverables) ? contract.expected_deliverables : [];
}

function taskContractAcceptance(contract) {
  return contract && Array.isArray(contract.acceptance_criteria) ? contract.acceptance_criteria : [];
}

function taskContractResolvedInfo(contract) {
  return contract && Array.isArray(contract.resolved_info) ? contract.resolved_info : [];
}

function taskContractDeliverableTitle(item) {
  if (!item) return 'Deliverable';
  return formatTitleCase(item.kind || item.summary || 'deliverable');
}

function hasTaskContractContent(contract) {
  return Boolean(contract && (
    taskContractGoal(contract) ||
    taskContractSuggestedDomains(contract).length ||
    taskContractMissingInfo(contract).length ||
    taskContractResolvedInfo(contract).length ||
    taskContractDeliverables(contract).length ||
    taskContractAcceptance(contract).length
  ));
}

function buildTaskContractCard(run) {
  const contract = run && run.task_contract;
  if (!hasTaskContractContent(contract)) return null;
  return {
    title: taskContractGoal(contract) || 'Task Contract',
    targetSummary: taskContractTargetSummary(contract),
    jobType: taskContractJobType(contract),
    confidence: taskContractConfidenceText(contract),
    requiresExternalEffect: Boolean(contract && contract.requires_external_effect),
    requiresApproval: Boolean(contract && contract.requires_approval),
    suggestedDomains: taskContractSuggestedDomains(contract).map(formatTitleCase),
    goal: taskContractGoal(contract),
    missingInfo: taskContractMissingInfo(contract).map(item => ({
      title: item.label || item.summary || item.id || 'Missing input',
      question: item.question || '',
      placeholder: item.placeholder || '',
      hints: Array.isArray(item.hints) ? item.hints : [],
      required: Boolean(item.required),
    })),
    resolvedInfo: taskContractResolvedInfo(contract).map(item => ({
      title: item.label || item.id || 'Resolved input',
      value: item.value || '',
      inputMode: item.input_mode || '',
    })),
    deliverables: taskContractDeliverables(contract).map(item => ({
      title: taskContractDeliverableTitle(item),
      summary: item.summary || '',
      required: Boolean(item.required),
    })),
    acceptance: taskContractAcceptance(contract).map(item => ({
      title: item.summary || item.id || 'Acceptance rule',
      deliverableKinds: Array.isArray(item.deliverable_kinds) ? item.deliverable_kinds.map(formatTitleCase) : [],
      evidenceHints: Array.isArray(item.evidence_hints) ? item.evidence_hints : [],
      required: Boolean(item.required),
    })),
  };
}

function joined(items) {
  return (items || []).filter(Boolean).join(' · ');
}

function governanceDecision(receipt, trace) {
  if (receipt && receipt.policy) return receipt.policy;
  if (trace && trace.decision) return trace.decision;
  return null;
}

function governanceScopeText(scope) {
  if (!scope || typeof scope !== 'object') return '';
  return joined([scope.automation_id]);
}

function governanceToolNames(receipt, trace) {
  if (receipt && Array.isArray(receipt.tool_names) && receipt.tool_names.length) return receipt.tool_names;
  if (trace && Array.isArray(trace.tool_names) && trace.tool_names.length) return trace.tool_names;
  return [];
}

function governanceActionClass(action) {
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

function governanceApprovalClass(status) {
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

function buildGovernanceCard(receipt, trace, runId) {
  const item = receipt || null;
  const decision = governanceDecision(item, trace);
  const approval = item && item.approval ? item.approval : null;
  const toolNames = governanceToolNames(item, trace);
  const scopeText = governanceScopeText(item && item.scope);
  const snapshotId = String((item && item.effective_config_snapshot_id) || (trace && trace.effective_config_snapshot_id) || '').trim();
  const summary = String((item && item.summary) || (decision && decision.summary) || '').trim();
  const reasons = decision && Array.isArray(decision.reasons) ? decision.reasons.filter(Boolean) : [];
  const reasonCodes = decision && Array.isArray(decision.reason_codes) ? decision.reason_codes.filter(Boolean) : [];
  const policyLayers = decision && Array.isArray(decision.policy_layers) ? decision.policy_layers.filter(Boolean) : [];
  const auditLabels = decision && Array.isArray(decision.audit_labels) ? decision.audit_labels.filter(Boolean) : [];
  const defaultScope = String(decision && decision.approval_policy && decision.approval_policy.default_scope || '').trim();
  const maxScope = String(decision && decision.approval_policy && decision.approval_policy.max_scope || '').trim();
  const externalRefs = approval && Array.isArray(approval.external)
    ? approval.external
      .map(item => ({
        provider: String(item.provider || '').trim(),
        externalId: String(item.external_id || '').trim(),
        status: String(item.status || '').trim(),
        href: safeExternalURL(item.url || ''),
      }))
      .filter(item => item.provider || item.externalId || item.status || item.href)
    : [];
  const approvalProviders = joined(externalRefs.map(item => item.provider));
  const title =
    String((decision && decision.action) || '').trim() === 'deny'
      ? '执行被策略拦截'
      : String((decision && decision.action) || '').trim() === 'require_approval'
        ? '执行进入审批闸门'
        : '治理回执';

  const hasContent = summary || decision || approval || scopeText || snapshotId || toolNames.length || externalRefs.length;
  if (!hasContent) return null;

  return {
    runId: String(runId || '').trim(),
    title,
    summary: summary || '系统已生成本次任务的治理回执。',
    action: String(decision && decision.action || '').trim(),
    actionClass: governanceActionClass(decision && decision.action),
    policySource: String(decision && decision.policy_source || '').trim(),
    approvalStatus: String(approval && approval.status || '').trim(),
    approvalClass: governanceApprovalClass(approval && approval.status),
    approvalId: String(approval && approval.id || '').trim(),
    approvalKind: String(approval && approval.kind || '').trim(),
    approvalProviders,
    scopeText,
    toolNames,
    snapshotId,
    reasons,
    reasonCodes,
    policyLayers,
    auditLabels,
    defaultScope,
    maxScope,
    updatedAt: String(trace && trace.updated_at || '').trim(),
    externalRefs,
  };
}

// ---------------------------------------------------------------------------
// Chat View component
// ---------------------------------------------------------------------------

export function ChatView() {
  const store = window._hcStore;

  const state = {
    msgs: [],
    tools: [],
    streaming: false,
    streamText: '',
    runId: '',
    interactionTurnId: '',
    turnStreamSeen: false,
    completedInteractionTurnId: '',
    sessId: '',
    attachment: null,
    pendingFileDataURI: '',
    attachmentReading: false,
    retries: 0,
    lastEventId: '',
    sseBuf: '',
    modelName: '',
    tokenCount: 0,
    connectionStatus: 'connecting',
    approvalPopup: null,
    dismissedApprovalPopupID: '',
    input: '',
    expandedTools: {},
    dragOver: false,
    workspaceLoading: false,
    workspaceError: null,
    workspaceRunHistory: [],
    workspaceActiveRun: null,
    workspaceCompletion: null,
    workspaceArtifacts: [],
    workspaceApprovals: [],
  };

  let xhr = null;
  let _mounted = false;
  let dragCounter = 0;
  let boundDragenter = null;
  let boundDragleave = null;
  let boundDragover = null;
  let boundDrop = null;
  let reconnectTimer = null;
  let sseGeneration = 0;

  // Template string for Petite Vue
  const $template = `
    <div class="hc-chat hc-task-workspace">
      <div class="hc-chat-messages" id="hc-chat-messages">
        <div class="hc-task-workspace-layout">
          <div class="hc-task-workspace-main">
        <div class="hc-chat-inner" id="hc-chat-inner">
          <div v-if="msgs.length === 0 && !streaming" class="hc-chat-empty"
               style="display:flex;align-items:center;justify-content:center;padding-top:20vh;max-width:480px;margin:0 auto;">
            <div class="hc-chat-empty-inner" style="text-align:center;width:100%;">
              <div class="hc-chat-empty-status"
                   style="color:var(--muted,#888);font-size:0.8rem;margin-bottom:24px;">
                <span>{{ storeSessionKey() }}</span>
                <span class="hc-chat-empty-sep" style="margin:0 6px;">&middot;</span>
                <span>{{ modelName || 'Auto' }}</span>
              </div>
              <div class="hc-chat-starters"
                   style="display:flex;flex-wrap:wrap;gap:8px;justify-content:center;">
                <button v-for="starter in starterPrompts()" :key="starter.key" type="button"
                  class="hc-chat-starter-pill"
                  style="background:none;border:1px solid var(--border,#ddd);border-radius:999px;padding:6px 16px;font-size:0.85rem;color:var(--ink,#333);cursor:pointer;transition:background .15s,border-color .15s;"
                  @mouseenter="$event.target.style.background='var(--hover-bg,#f5f5f5)';$event.target.style.borderColor='var(--accent,#666)'"
                  @mouseleave="$event.target.style.background='none';$event.target.style.borderColor='var(--border,#ddd)'"
                  @click="useStarterPrompt(starter.prompt)">
                  {{ starter.prompt }}
                </button>
              </div>
            </div>
          </div>

          <div v-for="(m, mi) in msgs" :key="mi" class="hc-msg" :class="m.role"
               @mouseenter="$event.currentTarget.querySelector('.hc-msg-actions') && ($event.currentTarget.querySelector('.hc-msg-actions').style.opacity='1')"
               @mouseleave="$event.currentTarget.querySelector('.hc-msg-actions') && ($event.currentTarget.querySelector('.hc-msg-actions').style.opacity='0')">
            <div class="hc-msg-avatar" v-if="m.role === 'user'">U</div>
            <div class="hc-msg-avatar" v-else v-html="botAvatar"></div>
            <div class="hc-msg-body">
              <div class="hc-msg-bubble" v-if="m.role === 'user'" v-text="m.content"></div>
              <div class="hc-msg-bubble" v-else-if="m.content" v-html="renderMd(m.content)"></div>
              <div v-if="m.role === 'assistant' && m.content && !streaming" class="hc-msg-actions"
                   style="display:flex;flex-direction:row;gap:4px;opacity:0;transition:opacity .15s;margin-top:4px;">
                <button class="hc-msg-action-btn"
                  style="background:none;border:none;cursor:pointer;padding:2px 4px;border-radius:4px;color:var(--muted,#888);display:inline-flex;align-items:center;"
                  @mouseenter="$event.target.style.color='var(--ink,#333)'"
                  @mouseleave="$event.target.style.color='var(--muted,#888)'"
                  @click="copyMessage(m.content)" :title="t('copy') || 'Copy'">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                </button>
                <button class="hc-msg-action-btn"
                  style="background:none;border:none;cursor:pointer;padding:2px 4px;border-radius:4px;color:var(--muted,#888);display:inline-flex;align-items:center;"
                  @mouseenter="$event.target.style.color='var(--ink,#333)'"
                  @mouseleave="$event.target.style.color='var(--muted,#888)'"
                  @click="retryFromMessage(mi)" :title="t('retry') || 'Retry'">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
                </button>
                <a v-if="m.runId" class="hc-msg-action-btn"
                  style="background:none;border:none;cursor:pointer;padding:2px 4px;border-radius:4px;color:var(--muted,#888);display:inline-flex;align-items:center;text-decoration:none;"
                  @mouseenter="$event.target.style.color='var(--ink,#333)'"
                  @mouseleave="$event.target.style.color='var(--muted,#888)'"
                  :href="'#/runs/' + encodeURIComponent(m.runId)" :title="t('chatWorkspaceViewRun') || 'View Run'">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><circle cx="12" cy="12" r="10"/><polygon points="10,8 16,12 10,16"/></svg>
                </a>
              </div>
              <div v-if="m.preflightCard" class="hc-preflight-card" :class="m.preflightCard.state">
                <div class="hc-preflight-card-title">{{ m.preflightCard.title }}</div>
                <div v-if="m.preflightCard.summary" class="hc-preflight-card-summary">{{ m.preflightCard.summary }}</div>
                <div v-if="m.preflightCard.question" class="hc-preflight-card-section">
                  <div class="hc-preflight-card-label">请直接回复</div>
                  <div>{{ m.preflightCard.question }}</div>
                </div>
                <div v-if="m.preflightCard.replyTemplate" class="hc-preflight-card-section">
                  <div class="hc-preflight-card-label">推荐格式</div>
                  <pre class="hc-preflight-template">{{ m.preflightCard.replyTemplate }}</pre>
                </div>
                <div v-if="m.preflightCard.slots && m.preflightCard.slots.length" class="hc-preflight-card-section">
                  <div class="hc-preflight-card-label">需要补充</div>
                  <div class="hc-task-contract-list">
                    <div v-for="(slot, si) in m.preflightCard.slots" :key="'ps-'+mi+'-'+si" class="hc-task-contract-item">
                      <div class="hc-task-contract-item-head">
                        <div class="hc-task-contract-item-title">{{ slot.label }}</div>
                        <span v-if="slot.required" class="hc-badge hc-badge-red">{{ t('chatTaskContractRequired') || 'Required' }}</span>
                      </div>
                      <div v-if="slot.question" class="hc-task-contract-item-copy">{{ slot.question }}</div>
                      <div v-if="slot.placeholder" class="hc-task-contract-item-hints">格式：{{ slot.placeholder }}</div>
                      <div v-if="slot.hints && slot.hints.length" class="hc-task-contract-item-hints">{{ slot.hints.join(' / ') }}</div>
                    </div>
                  </div>
                </div>
                <div v-if="m.preflightCard.examples && m.preflightCard.examples.length" class="hc-preflight-card-section">
                  <div class="hc-preflight-card-label">示例</div>
                  <div class="hc-preflight-card-examples">
                    <button v-for="(ex, ei) in m.preflightCard.examples" :key="'px-'+mi+'-'+ei" type="button" class="hc-preflight-chip" @click="applyPreflightReply(ex, false)">{{ ex }}</button>
                  </div>
                </div>
                <div v-if="m.preflightCard.actions && m.preflightCard.actions.length" class="hc-preflight-card-actions">
                  <button
                    v-for="(action, ai) in m.preflightCard.actions"
                    :key="'pa-'+mi+'-'+ai"
                    type="button"
                    class="hc-btn hc-btn-sm"
                    :class="action.kind === 'primary' ? 'hc-btn-primary' : 'hc-btn-secondary'"
                    @click="applyPreflightReply(action.value, action.sendNow)"
                  >
                    {{ action.label }}
                  </button>
                </div>
                <div v-if="m.preflightCard.next" class="hc-preflight-card-section">
                  <div class="hc-preflight-card-label">收到后</div>
                  <div>{{ m.preflightCard.next }}</div>
                </div>
              </div>
              <div v-if="m.taskContractCard" class="hc-task-contract-card">
                <div class="hc-task-contract-header">
                  <div class="hc-task-contract-copy">
                    <div class="hc-task-contract-title">{{ m.taskContractCard.title }}</div>
                    <div v-if="m.taskContractCard.targetSummary" class="hc-task-contract-summary">{{ t('chatTaskContractTarget') || '目标' }}：{{ m.taskContractCard.targetSummary }}</div>
                  </div>
                  <div class="hc-task-contract-badges">
                    <span v-if="m.taskContractCard.jobType" class="hc-badge hc-badge-gray">{{ m.taskContractCard.jobType }}</span>
                    <span v-if="m.taskContractCard.confidence" class="hc-badge hc-badge-gray">{{ m.taskContractCard.confidence }}</span>
                    <span v-if="m.taskContractCard.requiresExternalEffect" class="hc-badge hc-badge-orange">{{ t('chatTaskContractExternalEffect') || '外部动作' }}</span>
                    <span v-if="m.taskContractCard.requiresApproval" class="hc-badge hc-badge-orange">{{ t('chatTaskContractApprovalAware') || '审批敏感' }}</span>
                  </div>
                </div>
                <div v-if="m.taskContractCard.suggestedDomains && m.taskContractCard.suggestedDomains.length" class="hc-task-contract-chips">
                  <span v-for="(domain, di) in m.taskContractCard.suggestedDomains" :key="'tcd-'+mi+'-'+di" class="hc-result-chip">{{ domain }}</span>
                </div>
                <div class="hc-task-contract-grid">
                  <div class="hc-task-contract-panel">
                    <div class="hc-task-contract-panel-title">{{ t('chatTaskContractGoal') || 'Goal' }}</div>
                    <div class="hc-task-contract-panel-copy">{{ m.taskContractCard.goal || '—' }}</div>
                  </div>
                  <div class="hc-task-contract-panel">
                    <div class="hc-task-contract-panel-title">{{ t('chatTaskContractMissingInfo') || 'Missing Info' }}</div>
                    <div v-if="m.taskContractCard.missingInfo && m.taskContractCard.missingInfo.length" class="hc-task-contract-list">
                      <div v-for="(item, ii) in m.taskContractCard.missingInfo" :key="'tcm-'+mi+'-'+ii" class="hc-task-contract-item">
                        <div class="hc-task-contract-item-head">
                          <div class="hc-task-contract-item-title">{{ item.title }}</div>
                          <span v-if="item.required" class="hc-badge hc-badge-red">{{ t('chatTaskContractRequired') || 'Required' }}</span>
                        </div>
                        <div v-if="item.question" class="hc-task-contract-item-copy">{{ item.question }}</div>
                        <div v-if="item.placeholder" class="hc-task-contract-item-hints">格式：{{ item.placeholder }}</div>
                        <div v-if="item.hints && item.hints.length" class="hc-task-contract-item-hints">{{ item.hints.join(' / ') }}</div>
                      </div>
                    </div>
                    <div v-else class="hc-task-contract-empty">{{ t('chatTaskContractNoMissingInfo') || '当前没有记录阻塞性的缺失信息。' }}</div>
                  </div>
                </div>
                <div class="hc-task-contract-grid">
                  <div class="hc-task-contract-panel">
                    <div class="hc-task-contract-panel-title">{{ t('chatTaskContractResolvedInputs') || 'Resolved Inputs' }}</div>
                    <div v-if="m.taskContractCard.resolvedInfo && m.taskContractCard.resolvedInfo.length" class="hc-task-contract-list">
                      <div v-for="(item, ii) in m.taskContractCard.resolvedInfo" :key="'tcr-'+mi+'-'+ii" class="hc-task-contract-item">
                        <div class="hc-task-contract-item-head">
                          <div class="hc-task-contract-item-title">{{ item.title }}</div>
                          <span class="hc-badge hc-badge-green">{{ t('chatTaskContractResolved') || 'Resolved' }}</span>
                        </div>
                        <div v-if="item.value" class="hc-task-contract-item-copy">{{ item.value }}</div>
                      </div>
                    </div>
                    <div v-else class="hc-task-contract-empty">{{ t('chatTaskContractNoResolvedInputs') || '当前还没有记录已确认的补充信息。' }}</div>
                  </div>
                  <div class="hc-task-contract-panel">
                    <div class="hc-task-contract-panel-title">{{ t('chatTaskContractDeliverables') || 'Deliverables' }}</div>
                    <div v-if="m.taskContractCard.deliverables && m.taskContractCard.deliverables.length" class="hc-task-contract-list">
                      <div v-for="(item, ii) in m.taskContractCard.deliverables" :key="'tcdl-'+mi+'-'+ii" class="hc-task-contract-item">
                        <div class="hc-task-contract-item-head">
                          <div class="hc-task-contract-item-title">{{ item.title }}</div>
                          <span v-if="item.required" class="hc-badge hc-badge-green">{{ t('chatTaskContractRequired') || 'Required' }}</span>
                        </div>
                        <div v-if="item.summary" class="hc-task-contract-item-copy">{{ item.summary }}</div>
                      </div>
                    </div>
                    <div v-else class="hc-task-contract-empty">{{ t('chatTaskContractNoDeliverables') || '当前没有记录明确交付物。' }}</div>
                  </div>
                  <div class="hc-task-contract-panel">
                    <div class="hc-task-contract-panel-title">{{ t('chatTaskContractAcceptance') || 'Acceptance' }}</div>
                    <div v-if="m.taskContractCard.acceptance && m.taskContractCard.acceptance.length" class="hc-task-contract-list">
                      <div v-for="(item, ii) in m.taskContractCard.acceptance" :key="'tca-'+mi+'-'+ii" class="hc-task-contract-item">
                        <div class="hc-task-contract-item-head">
                          <div class="hc-task-contract-item-title">{{ item.title }}</div>
                          <span v-if="item.required" class="hc-badge hc-badge-green">{{ t('chatTaskContractRequired') || 'Required' }}</span>
                        </div>
                        <div v-if="item.deliverableKinds && item.deliverableKinds.length" class="hc-task-contract-item-hints">
                          {{ item.deliverableKinds.join(' · ') }}
                        </div>
                        <div v-if="item.evidenceHints && item.evidenceHints.length" class="hc-task-contract-item-hints">
                          {{ t('chatTaskContractEvidence') || 'Evidence' }}: {{ item.evidenceHints.join(' / ') }}
                        </div>
                      </div>
                    </div>
                    <div v-else class="hc-task-contract-empty">{{ t('chatTaskContractNoAcceptance') || '当前没有记录明确验收条件。' }}</div>
                  </div>
                </div>
              </div>
              <div v-if="m.governanceCard" class="hc-task-contract-card hc-governance-receipt-card" :class="m.governanceCard.action">
                <div class="hc-task-contract-header">
                  <div class="hc-task-contract-copy">
                    <div class="hc-task-contract-title">{{ m.governanceCard.title }}</div>
                    <div v-if="m.governanceCard.summary" class="hc-task-contract-summary">{{ m.governanceCard.summary }}</div>
                  </div>
                  <div class="hc-task-contract-badges">
                    <span v-if="m.governanceCard.action" class="hc-badge" :class="m.governanceCard.actionClass">{{ formatStatusText(m.governanceCard.action) }}</span>
                    <span v-if="m.governanceCard.approvalStatus" class="hc-badge" :class="m.governanceCard.approvalClass">{{ formatStatusText(m.governanceCard.approvalStatus) }}</span>
                  </div>
                </div>
                <div class="hc-task-contract-chips">
                  <span v-if="m.governanceCard.scopeText" class="hc-result-chip">{{ m.governanceCard.scopeText }}</span>
                  <span v-if="m.governanceCard.approvalProviders" class="hc-result-chip">{{ m.governanceCard.approvalProviders }}</span>
                  <span v-if="m.governanceCard.snapshotId" class="hc-result-chip">{{ m.governanceCard.snapshotId }}</span>
                  <span v-for="(tool, gi) in m.governanceCard.toolNames" :key="'gt-'+mi+'-'+gi" class="hc-result-chip">{{ tool }}</span>
                </div>
                <div class="hc-task-contract-grid">
                  <div class="hc-task-contract-panel">
                    <div class="hc-task-contract-panel-title">{{ t('chatGovernancePolicy') || 'Policy' }}</div>
                    <div class="hc-task-contract-item-copy">{{ m.governanceCard.policySource || (t('chatGovernanceRuntimePolicy') || 'runtime policy') }}</div>
                    <div v-if="m.governanceCard.defaultScope || m.governanceCard.maxScope" class="hc-task-contract-item-hints">
                      {{ [m.governanceCard.defaultScope ? 'default ' + m.governanceCard.defaultScope : '', m.governanceCard.maxScope ? 'max ' + m.governanceCard.maxScope : ''].filter(Boolean).join(' · ') }}
                    </div>
                    <div v-if="m.governanceCard.reasons.length" class="hc-task-contract-list">
                      <div v-for="(reason, ri) in m.governanceCard.reasons" :key="'gr-'+mi+'-'+ri" class="hc-task-contract-item">
                        <div class="hc-task-contract-item-copy">{{ reason }}</div>
                      </div>
                    </div>
                  </div>
                  <div class="hc-task-contract-panel">
                    <div class="hc-task-contract-panel-title">{{ t('chatGovernanceApproval') || 'Approval' }}</div>
                    <div class="hc-task-contract-item-copy">{{ [m.governanceCard.approvalKind, m.governanceCard.approvalId].filter(Boolean).join(' · ') || (t('chatGovernanceNoApprovalTicket') || 'No approval ticket') }}</div>
                    <div v-if="m.governanceCard.externalRefs.length" class="hc-task-contract-list">
                      <div v-for="(ref, ri) in m.governanceCard.externalRefs" :key="'ge-'+mi+'-'+ri" class="hc-task-contract-item">
                        <div class="hc-task-contract-item-head">
                          <div class="hc-task-contract-item-title">{{ [ref.provider, ref.externalId].filter(Boolean).join(' · ') || (t('chatGovernanceExternalRef') || 'External ref') }}</div>
                          <span v-if="ref.status" class="hc-badge hc-badge-gray">{{ ref.status }}</span>
                        </div>
                        <a v-if="ref.href" class="hc-governance-link" :href="ref.href" target="_blank" rel="noopener noreferrer">{{ ref.href }}</a>
                      </div>
                    </div>
                  </div>
                </div>
                <div v-if="m.governanceCard.reasonCodes.length || m.governanceCard.policyLayers.length || m.governanceCard.auditLabels.length" class="hc-task-contract-grid">
                  <div v-if="m.governanceCard.reasonCodes.length" class="hc-task-contract-panel">
                    <div class="hc-task-contract-panel-title">{{ t('chatGovernanceReasonCodes') || 'Reason Codes' }}</div>
                    <div class="hc-task-contract-chips">
                      <span v-for="(item, ri) in m.governanceCard.reasonCodes" :key="'gc-'+mi+'-'+ri" class="hc-result-chip">{{ item }}</span>
                    </div>
                  </div>
                  <div v-if="m.governanceCard.policyLayers.length || m.governanceCard.auditLabels.length" class="hc-task-contract-panel">
                    <div class="hc-task-contract-panel-title">{{ t('chatGovernanceTrace') || 'Trace' }}</div>
                    <div v-if="m.governanceCard.policyLayers.length" class="hc-task-contract-item-hints">{{ m.governanceCard.policyLayers.join(' · ') }}</div>
                    <div v-if="m.governanceCard.auditLabels.length" class="hc-task-contract-item-hints">{{ m.governanceCard.auditLabels.join(' · ') }}</div>
                  </div>
                </div>
                <div class="hc-result-card-footer">
                  <span v-if="m.governanceCard.updatedAt" class="hc-result-card-updated">{{ t('chatGovernanceUpdated') || 'Updated' }} {{ fmtTime(m.governanceCard.updatedAt) }}</span>
                  <a v-if="m.governanceCard.runId" class="hc-btn hc-btn-sm hc-btn-secondary" :href="'#/runs/' + encodeURIComponent(m.governanceCard.runId)">{{ t('chatGovernanceViewGovernance') || 'View governance' }}</a>
                </div>
              </div>
              <div v-if="m.resultCard" class="hc-result-card">
                <div class="hc-result-card-header">
                  <div class="hc-result-card-copy">
                    <div class="hc-result-card-title">{{ m.resultCard.title }}</div>
                    <div v-if="m.resultCard.summary" class="hc-result-card-summary">{{ m.resultCard.summary }}</div>
                  </div>
                  <div class="hc-result-card-badges">
                    <span v-if="m.resultCard.outcome" class="hc-badge" :class="receiptOutcomeClass(m.resultCard.outcome)">{{ formatStatusText(m.resultCard.outcome) }}</span>
                    <span v-if="m.resultCard.verificationStatus" class="hc-badge" :class="receiptVerificationClass(m.resultCard.verificationStatus)">{{ formatStatusText(m.resultCard.verificationStatus) }}</span>
                  </div>
                </div>
                <div v-if="m.resultCard.verificationSummary" class="hc-result-card-note">{{ m.resultCard.verificationSummary }}</div>
                <div v-if="m.resultCard.deliverables && m.resultCard.deliverables.length" class="hc-result-card-deliverables">
                  <div v-for="(item, di) in m.resultCard.deliverables" :key="'rd-'+mi+'-'+di" class="hc-result-deliverable">
                    <div class="hc-result-deliverable-head">
                      <span class="hc-result-deliverable-name">{{ item.name }}</span>
                      <span v-if="item.size" class="hc-result-deliverable-meta">{{ item.size }}</span>
                    </div>
                    <div v-if="item.contentType" class="hc-result-deliverable-meta">{{ item.contentType }}</div>
                    <div v-if="item.previewText" class="hc-result-deliverable-preview">{{ item.previewText }}</div>
                  </div>
                </div>
                <div v-if="m.resultCard.suggestedActions && m.resultCard.suggestedActions.length" class="hc-result-card-actions">
                  <span v-for="(action, ai) in m.resultCard.suggestedActions" :key="'ra-'+mi+'-'+ai" class="hc-result-chip" :title="action.reason || ''">{{ action.label }}</span>
                </div>
                <div class="hc-result-card-footer">
                  <span v-if="m.resultCard.updatedAt" class="hc-result-card-updated">{{ t('chatResultUpdated') || 'Updated' }} {{ fmtTime(m.resultCard.updatedAt) }}</span>
                  <a v-if="m.resultCard.runId" class="hc-btn hc-btn-sm hc-btn-secondary" :href="'#/runs/' + encodeURIComponent(m.resultCard.runId)">{{ t('chatResultViewReceipt') || 'View receipt' }}</a>
                </div>
              </div>
              <div v-for="(tc, ti) in (m.toolCalls || [])" :key="'tc-'+mi+'-'+ti" class="hc-tool" :class="{ open: expandedTools['m'+mi+'t'+ti] }">
                <div class="hc-tool-header" @click="toggleTool('m'+mi+'t'+ti)">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" width="14" height="14"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>
                  <span class="hc-tool-name">{{ tc.name || 'tool' }}</span>
                  <span class="hc-tool-toggle">{{ expandedTools['m'+mi+'t'+ti] ? t('showLess') : t('showMore') }}</span>
                </div>
                <div class="hc-tool-body">
                  <div v-if="tc.input" style="margin-bottom:6px;color:var(--ink);font-weight:500">{{ t('toolCall') }}:</div>
                  <span v-if="tc.input">{{ formatToolData(tc.input) }}</span>
                  <div v-if="tc.result" style="margin-top:8px;color:var(--ink);font-weight:500">{{ t('toolResult') }}:</div>
                  <span v-if="tc.result">{{ formatToolData(tc.result) }}</span>
                </div>
              </div>
              <div v-if="m.time" class="hc-msg-time">{{ fmtTime(m.time) }}</div>
            </div>
          </div>
          <div v-for="(tc, ti) in tools" :key="'stc-'+ti" class="hc-tool" :class="{ open: expandedTools['s'+ti] }">
            <div class="hc-tool-header" @click="toggleTool('s'+ti)">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" width="14" height="14"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>
              <span class="hc-tool-name">{{ tc.name || 'tool' }}</span>
              <span class="hc-tool-toggle">{{ expandedTools['s'+ti] ? t('showLess') : t('showMore') }}</span>
            </div>
            <div class="hc-tool-body">
              <div v-if="tc.input" style="margin-bottom:6px;color:var(--ink);font-weight:500">{{ t('toolCall') }}:</div>
              <span v-if="tc.input">{{ formatToolData(tc.input) }}</span>
              <div v-if="tc.result" style="margin-top:8px;color:var(--ink);font-weight:500">{{ t('toolResult') }}:</div>
              <span v-if="tc.result">{{ formatToolData(tc.result) }}</span>
            </div>
          </div>
          <div v-if="streaming && streamText" class="hc-msg assistant">
            <div class="hc-msg-avatar" v-html="botAvatar"></div>
            <div class="hc-msg-body"><div class="hc-msg-bubble" v-html="renderMd(streamText)"></div></div>
          </div>
          <div v-if="streaming && !streamText && tools.length === 0" class="hc-typing">
            <div class="hc-typing-avatar" v-html="botAvatar"></div>
            <div class="hc-typing-dots"><span></span><span></span><span></span></div>
          </div>
        </div>
          </div>

          <aside class="hc-task-workspace-sidebar" data-testid="assistant-workspace">
            <div v-if="workspaceAllEmpty && !workspaceLoading && !workspaceError" class="hc-card hc-task-workspace-panel" data-testid="assistant-workspace-empty">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('taskWorkspaceStatusTitle') || 'Current status' }}</div>
                </div>
              </div>
              <div class="hc-empty-compact">{{ t('taskWorkspaceAllEmpty') || 'No run data for this thread yet. Start a conversation to see status, plan, outputs, and verification here.' }}</div>
            </div>
            <div v-if="!workspaceAllEmpty || workspaceLoading || workspaceError" class="hc-task-workspace-stack">
            <div class="hc-card hc-task-workspace-panel hc-task-workspace-banner">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('taskWorkspaceStatusTitle') || 'Current status' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('taskWorkspaceStatusDesc') || 'Truthful run, scope, and lifecycle projection for this thread.' }}</div>
                </div>
                <span v-if="workspaceActiveRun" class="hc-badge" :class="workspaceRunBadgeClass(workspaceActiveRun)">
                  {{ workspaceRunStateLabel(workspaceActiveRun) }}
                </span>
              </div>

              <div v-if="workspaceError" class="hc-governance-error">{{ workspaceError }}</div>
              <div v-else-if="workspaceLoading && !workspaceActiveRun" class="hc-loading">{{ t('loading') }}</div>
              <div v-else-if="!workspaceActiveRun" class="hc-state-block">
                <div class="hc-state-block-title">{{ t('taskWorkspaceNoRun') || 'No run has been started for this thread yet.' }}</div>
              </div>
              <div v-else class="hc-task-workspace-active">
                <div class="hc-detail-grid">
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('runId') || 'Run ID' }}</div>
                    <div class="hc-detail-stat-value">{{ shortID(workspaceActiveRun.id) }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('session') || 'Session' }}</div>
                    <div class="hc-detail-stat-value">{{ storeSessionKey() }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('started') || 'Started' }}</div>
                    <div class="hc-detail-stat-value">{{ fmtTime(workspaceActiveRun.started_at || workspaceActiveRun.created_at) || '-' }}</div>
                  </div>
                  <div class="hc-detail-stat">
                    <div class="hc-detail-stat-label">{{ t('scope') || 'Scope' }}</div>
                    <div class="hc-detail-stat-value">{{ workspaceScopeText(workspaceActiveRun) }}</div>
                  </div>
                </div>
                <div class="hc-task-workspace-note" v-if="workspaceActiveRun.error">{{ workspaceActiveRun.error }}</div>
                <div class="hc-task-workspace-note" v-else-if="workspaceActiveRun.verification_summary">{{ workspaceActiveRun.verification_summary }}</div>
                <div class="hc-state-block-actions">
                  <a class="hc-btn hc-btn-sm hc-btn-secondary" data-testid="assistant-workspace-open-run" :href="'#/runs/' + encodeURIComponent(workspaceActiveRun.id)">{{ t('taskWorkspaceOpenRun') || 'Open run' }}</a>
                  <a class="hc-btn hc-btn-sm hc-btn-ghost" href="#/approvals">{{ t('taskWorkspaceOpenApprovals') || 'Open approvals' }}</a>
                </div>
              </div>
            </div>

            <div class="hc-card hc-task-workspace-panel" data-testid="assistant-workspace-run-history">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('taskWorkspacePlanTitle') || 'Plan & steps' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('taskWorkspacePlanDesc') || 'The latest canonical plan projection from the runtime.' }}</div>
                </div>
              </div>
              <div v-if="workspacePlanTasks.length === 0" class="hc-empty-compact">{{ t('taskWorkspaceNoPlan') || 'No execution plan is attached to the latest run.' }}</div>
              <div v-else class="hc-task-workspace-list">
                <div v-for="task in workspacePlanTasks" :key="task.id" class="hc-task-workspace-item">
                  <div class="hc-task-workspace-item-head">
                    <div class="hc-task-workspace-item-title">{{ task.title || task.id }}</div>
                    <span class="hc-badge" :class="workspaceTaskBadgeClass(task.status || 'queued')">{{ formatStatusText(task.status || 'queued') }}</span>
                  </div>
                  <div class="hc-task-workspace-item-copy">{{ task.goal || task.title || task.id }}</div>
                  <div class="hc-task-workspace-item-meta">{{ workspaceTaskMeta(task) }}</div>
                </div>
              </div>
            </div>

            <div class="hc-card hc-task-workspace-panel">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('taskWorkspaceOutputsTitle') || 'Outputs & artifacts' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('taskWorkspaceOutputsDesc') || 'Files, screenshots, receipts, and generated artifacts for this thread.' }}</div>
                </div>
              </div>
              <div v-if="workspaceArtifacts.length === 0" class="hc-empty-compact">{{ t('taskWorkspaceNoArtifacts') || 'No artifacts have been recorded for this thread yet.' }}</div>
              <div v-else class="hc-task-workspace-list">
                <div v-for="artifact in workspaceArtifacts" :key="artifact.id" class="hc-task-workspace-item">
                  <div class="hc-task-workspace-item-head">
                    <div class="hc-task-workspace-item-title">{{ artifact.name || artifact.id }}</div>
                    <span class="hc-badge hc-badge-gray">{{ artifact.kind || 'artifact' }}</span>
                  </div>
                  <div class="hc-task-workspace-item-meta">{{ [artifact.content_type || '', formatFileSize(artifact.size)].filter(Boolean).join(' · ') || shortID(artifact.id) }}</div>
                  <div v-if="workspaceArtifactPreviewText(artifact)" class="hc-task-workspace-item-copy">{{ workspaceArtifactPreviewText(artifact) }}</div>
                  <div class="hc-state-block-actions" v-if="workspaceArtifactHref(artifact)">
                    <a class="hc-btn hc-btn-sm hc-btn-secondary" :href="workspaceArtifactHref(artifact)" target="_blank" rel="noopener noreferrer">{{ t('taskWorkspaceArtifactPreview') || 'Preview' }}</a>
                  </div>
                </div>
              </div>
            </div>

            <div class="hc-card hc-task-workspace-panel">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('taskWorkspaceApprovalsTitle') || 'Approval checkpoints' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('taskWorkspaceApprovalsDesc') || 'Pending human approvals for the active thread.' }}</div>
                </div>
              </div>
              <div v-if="workspaceApprovals.length === 0" class="hc-empty-compact">{{ t('taskWorkspaceNoApprovals') || 'No pending approvals for this thread.' }}</div>
              <div v-else class="hc-task-workspace-list">
                <div v-for="ticket in workspaceApprovals.slice(0, 4)" :key="ticket.id || ticket.ticket_id" class="hc-task-workspace-item">
                  <div class="hc-task-workspace-item-head">
                    <div class="hc-task-workspace-item-title">{{ approvalPrimaryTool(ticket) }}</div>
                    <span class="hc-badge hc-badge-orange">{{ formatStatusText(ticket.status || 'pending') }}</span>
                  </div>
                  <div class="hc-task-workspace-item-meta">{{ shortID(ticket.run_id) }} · {{ fmtTime(ticket.created_at) || '-' }}</div>
                  <div v-if="approvalReasons(ticket).length" class="hc-task-workspace-item-copy">{{ approvalReasons(ticket).slice(0, 2).join(' · ') }}</div>
                  <div v-else-if="approvalSummary(ticket)" class="hc-task-workspace-item-copy">{{ approvalSummary(ticket) }}</div>
                </div>
              </div>
            </div>

            <div class="hc-card hc-task-workspace-panel">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('taskWorkspaceVerificationTitle') || 'Verification' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('taskWorkspaceVerificationDesc') || 'Verification status and summary for the latest result bundle.' }}</div>
                </div>
                <span v-if="workspaceVerificationView && workspaceVerificationView.status" class="hc-badge" :class="workspaceVerificationBadgeClass(workspaceVerificationView.status)">
                  {{ formatStatusText(workspaceVerificationView.status) }}
                </span>
              </div>
              <div v-if="!workspaceVerificationView || !workspaceVerificationView.status" class="hc-empty-compact">{{ t('taskWorkspaceNoVerification') || 'No verification result has been recorded yet.' }}</div>
              <div v-if="workspaceVerificationView && workspaceVerificationView.status">
                <div class="hc-task-workspace-note">{{ workspaceVerificationView.summary || '-' }}</div>
                <div class="hc-task-workspace-note" v-if="workspaceVerificationChecksCount">{{ workspaceVerificationChecksCount }} {{ t('chatWorkspaceChecks') || 'checks' }}</div>
              </div>
            </div>

            <div class="hc-card hc-task-workspace-panel">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('taskWorkspaceDeliveryTitle') || 'Delivery & receipts' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('taskWorkspaceDeliveryDesc') || 'Outcome summary, deliverables, and next actions from the latest result bundle.' }}</div>
                </div>
              </div>
              <div v-if="!workspaceResultCard" class="hc-empty-compact">{{ t('taskWorkspaceNoDelivery') || 'No delivery receipt is available yet.' }}</div>
              <div v-if="workspaceResultCard">
                <div class="hc-task-workspace-item-head">
                  <div class="hc-task-workspace-item-title">{{ workspaceResultCard.title }}</div>
                  <div class="hc-task-workspace-chip-row">
                    <span v-if="workspaceResultCard.outcome" class="hc-badge" :class="receiptOutcomeClass(workspaceResultCard.outcome)">{{ formatStatusText(workspaceResultCard.outcome) }}</span>
                    <span v-if="workspaceResultCard.verificationStatus" class="hc-badge" :class="receiptVerificationClass(workspaceResultCard.verificationStatus)">{{ formatStatusText(workspaceResultCard.verificationStatus) }}</span>
                  </div>
                </div>
                <div v-if="workspaceResultCard.summary" class="hc-task-workspace-note">{{ workspaceResultCard.summary }}</div>
                <div v-if="workspaceResultCard.deliverables && workspaceResultCard.deliverables.length" class="hc-task-workspace-list">
                  <div v-for="item in workspaceResultCard.deliverables" :key="item.uri || item.name" class="hc-task-workspace-item">
                    <div class="hc-task-workspace-item-head">
                      <div class="hc-task-workspace-item-title">{{ item.name }}</div>
                      <span class="hc-badge hc-badge-gray">{{ item.kind || 'artifact' }}</span>
                    </div>
                    <div class="hc-task-workspace-item-meta">{{ [item.contentType, item.size].filter(Boolean).join(' · ') }}</div>
                    <div v-if="item.previewText" class="hc-task-workspace-item-copy">{{ item.previewText }}</div>
                  </div>
                </div>
              </div>
            </div>

            <div class="hc-card hc-task-workspace-panel">
              <div class="hc-detail-panel-head">
                <div class="hc-detail-panel-copy">
                  <div class="hc-detail-panel-title">{{ t('taskWorkspaceHistoryTitle') || 'Run history' }}</div>
                  <div class="hc-detail-panel-subtitle">{{ t('taskWorkspaceHistoryDesc') || 'Recent runs for this thread, ordered by latest activity.' }}</div>
                </div>
              </div>
              <div v-if="workspaceRunHistory.length === 0" class="hc-empty-compact">{{ t('taskWorkspaceNoHistory') || 'No run history is available yet.' }}</div>
              <div v-else class="hc-task-workspace-list">
                <a v-for="run in workspaceRunHistory.slice(0, 6)" :key="run.id" class="hc-task-workspace-item hc-task-workspace-link" :href="'#/runs/' + encodeURIComponent(run.id)">
                  <div class="hc-task-workspace-item-head">
                    <div class="hc-task-workspace-item-title">{{ shortID(run.id) }}</div>
                    <span class="hc-badge" :class="workspaceRunBadgeClass(run)">{{ workspaceRunStateLabel(run) }}</span>
                  </div>
                  <div class="hc-task-workspace-item-meta">{{ [run.execution_mode || '', fmtTime(run.updated_at || run.finished_at || run.started_at || run.created_at)].filter(Boolean).join(' · ') }}</div>
                  <div v-if="run.task_contract && run.task_contract.goal" class="hc-task-workspace-item-copy">{{ run.task_contract.goal }}</div>
                  <div v-else-if="run.preflight && run.preflight.summary" class="hc-task-workspace-item-copy">{{ run.preflight.summary }}</div>
                </a>
              </div>
            </div>
            </div>
          </aside>
        </div>
      </div>

      <div v-if="approvalPopup" class="hc-approval-popup">
        <div class="hc-approval-popup-backdrop" @click="dismissApprovalPopup()"></div>
        <div class="hc-approval-popup-card">
          <h3>{{ t('approvalRequired') || 'Approval Required' }}</h3>
          <div class="hc-approval-popup-tool">{{ approvalPopup.tool_name }}</div>
          <div v-if="approvalPopup.summary" class="hc-task-workspace-note">{{ approvalPopup.summary }}</div>
          <pre v-if="approvalPopup.arguments" class="hc-approval-popup-args">{{ formatToolData(approvalPopup.arguments) }}</pre>
          <div v-if="approvalPopup.reasons && approvalPopup.reasons.length > 0" class="hc-approval-popup-reasons">
            <div v-for="r in approvalPopup.reasons" class="hc-approval-popup-reason">{{ r }}</div>
          </div>
          <div class="hc-approval-popup-actions">
            <button class="hc-btn hc-btn-success" @click="resolveApproval(approvalPopup.id, 'approve')">{{ t('approve') || 'Approve' }}</button>
            <button class="hc-btn hc-btn-danger" @click="resolveApproval(approvalPopup.id, 'deny')">{{ t('deny') || 'Deny' }}</button>
            <button class="hc-btn hc-btn-ghost" @click="cancelApproval(approvalPopup.id)">{{ t('cancel') }}</button>
          </div>
        </div>
      </div>

      <div v-if="attachment" class="hc-attach-preview vis">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/></svg>
        <span class="hc-attach-name">{{ attachment.name }} ({{ formatFileSize(attachment.size) }})</span>
        <button class="hc-attach-remove" @click="clearAttachment()">x</button>
      </div>

      <div class="hc-chat-input-area">
        <div class="hc-chat-input-inner">
          <div class="hc-chat-input-heading">{{ t('chatComposerHint') }}</div>
          <div class="hc-chat-input-box">
            <button class="hc-attach-btn" @click="triggerFileInput()" :title="t('uploadFile')">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20"><path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/></svg>
            </button>
            <textarea id="hc-chat-input" :placeholder="t('placeholder')" rows="1"
              :disabled="streaming"
              @input="autoResize($event)"
              @keydown="handleKeydown($event)"
              v-model="input"></textarea>
          </div>
          <button class="hc-send-btn" :title="t('send')" :disabled="streaming" @click="send()">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>
          </button>
        </div>
      </div>

      <div v-if="modelName || tokenCount > 0" class="hc-chat-statusbar">
        <span v-if="modelName" class="hc-chat-status-model">{{ modelName }}</span>
        <span v-if="tokenCount > 0" class="hc-chat-status-tokens">{{ tokenCount }} {{ t('tokens') || 'tokens' }}</span>
      </div>

      <div class="hc-drop-overlay" :class="{ vis: dragOver }">{{ t('dropHere') }}</div>

      <input type="file" class="hc-file-input" id="hc-chat-file-input" @change="handleFileInput($event)" />
    </div>
  `;

  return {
    $template,
    ...state,
    botAvatar: BOT_AVATAR_SVG,
    t,
    shortID,

    // Helpers
    fmtTime,
    formatFileSize,
    approvalPrimaryTool,
    approvalReasons,
    approvalSummary,

    renderMd(text) {
      return renderMarkdown(text);
    },

    formatToolData(data) {
      if (!data) return '';
      return typeof data === 'string' ? data : JSON.stringify(data, null, 2);
    },

    formatStatusText(value) {
      return formatTitleCase(value);
    },

    workspaceRunStateLabel(run) {
      return formatTitleCase(workspaceRunState(run) || 'idle');
    },

    workspaceRunBadgeClass,
    workspaceTaskBadgeClass,
    workspaceVerificationBadgeClass,

    workspaceScopeText(run) {
      if (!run) return '-';
      if (run.governance && run.governance.scope) return governanceScopeText(run.governance.scope);
      if (run.scope) return governanceScopeText(run.scope);
      return '-';
    },

    workspaceTaskMeta(task) {
      if (!task) return '';
      const dependsOn = Array.isArray(task.depends_on || task.dependsOn) ? (task.depends_on || task.dependsOn) : [];
      const outputs = Array.isArray(task.outputs) ? task.outputs : [];
      return [
        task.kind ? formatTitleCase(task.kind) : '',
        dependsOn.length ? 'depends ' + dependsOn.join(', ') : '',
        outputs.length ? 'outputs ' + outputs.join(', ') : '',
      ].filter(Boolean).join(' · ');
    },

    workspaceArtifactHref(item) {
      return artifactPreviewPath(item);
    },

    workspaceArtifactPreviewText(item) {
      if (!item) return '';
      return String(
        item.preview_text ||
        (item.metadata && (item.metadata.preview_text || item.metadata.summary || item.metadata.preview)) ||
        ''
      ).trim();
    },

    get workspacePlanTasks() {
      const plan = this.workspaceActiveRun && this.workspaceActiveRun.plan;
      return plan && Array.isArray(plan.tasks) ? plan.tasks : [];
    },

    get workspaceVerificationView() {
      const verification = completionVerification(this.workspaceCompletion);
      return verification && verification.status ? verification : null;
    },

    get workspaceVerificationChecksCount() {
      const view = this.workspaceVerificationView;
      return view && Array.isArray(view.checks) ? view.checks.length : 0;
    },

    get workspaceResultCard() {
      if (!this.workspaceActiveRun) return null;
      return buildResultCard(this.workspaceActiveRun.id, this.workspaceCompletion);
    },

    get workspaceAllEmpty() {
      return !this.workspaceActiveRun
        && this.workspaceRunHistory.length === 0
        && this.workspacePlanTasks.length === 0
        && this.workspaceArtifacts.length === 0
        && this.workspaceApprovals.length === 0
        && !this.workspaceVerificationView
        && !this.workspaceResultCard;
    },

    receiptOutcomeClass(outcome) {
      switch (String(outcome || '').toLowerCase()) {
        case 'completed': return 'hc-badge-green';
        case 'partial': return 'hc-badge-orange';
        case 'failed': return 'hc-badge-red';
        case 'cancelled': return 'hc-badge-gray';
        case 'needs_confirmation': return 'hc-badge-orange';
        default: return 'hc-badge-gray';
      }
    },

    receiptVerificationClass(status) {
      switch (String(status || '').toLowerCase()) {
        case 'passed': return 'hc-badge-green';
        case 'warning': return 'hc-badge-orange';
        case 'failed': return 'hc-badge-red';
        default: return 'hc-badge-gray';
      }
    },

    statusLabel(status) {
      switch (status) {
        case 'ok': return t('connected') || 'Connected';
        case 'err': return t('disconnected') || 'Disconnected';
        default: return t('connecting') || 'Connecting';
      }
    },

    starterPrompts() {
      return [
        { key: '01', prompt: t('chatStarterInvestigate') },
        { key: '02', prompt: t('chatStarterApprove') },
        { key: '03', prompt: t('chatStarterSetup') },
        { key: '04', prompt: t('chatStarterAudit') },
      ];
    },

    useStarterPrompt(prompt) {
      if (this.streaming) return;
      this.input = prompt;
      this.$nextTick(() => this.focusInput());
    },

    storeSessionKey() {
      return window._hcStore?.sessionKey || localStorage.getItem(SESSION_KEY_STORAGE) || 'webchat';
    },

    async ensureSessionContext() {
      let sessions = Array.isArray(store.sessionList) ? store.sessionList.slice() : [];
      const wantedKey = this.storeSessionKey();
      let match = sessions.find(item => (item.key || item.id) === wantedKey) || null;
      if (!match) {
        sessions = await fetchSharedSessionList().catch(() => []);
        match = sessions.find(item => (item.key || item.id) === wantedKey) || null;
      }
      if (match && match.id) {
        this.sessId = match.id;
      }
      return match || null;
    },

    async refreshWorkspace() {
      this.workspaceLoading = true;
      this.workspaceError = null;
      try {
        const session = await this.ensureSessionContext();
        if (!_mounted) return;
        const sessionID = String((session && session.id) || this.sessId || '').trim();
        if (!sessionID) {
          this.workspaceRunHistory = [];
          this.workspaceActiveRun = null;
          this.workspaceCompletion = null;
          this.workspaceArtifacts = [];
          this.workspaceApprovals = [];
          return;
        }

        const [runsData, approvalsData, artifactsData] = await Promise.all([
          api.get('/runtime/runs?session_id=' + encodeURIComponent(sessionID) + '&include=verification&limit=8').catch(() => null),
          api.get('/runtime/approvals?status=pending').catch(() => null),
          api.get('/operator/artifacts?session_id=' + encodeURIComponent(sessionID) + '&limit=6').catch(() => null),
        ]);
        if (!_mounted) return;

        const runs = sortNewest(extractItems(runsData));
        const activeRun = runs[0] || null;
        const approvals = sortNewest(extractItems(approvalsData).filter(ticket => {
          const ticketSessionID = String(ticket.session_id || '').trim();
          const ticketSessionKey = String(ticket.session_key || '').trim();
          return ticketSessionID === sessionID || ticketSessionKey === this.storeSessionKey();
        }));
        const artifacts = sortNewest(extractItems(artifactsData)).slice(0, 6);

        let completion = null;
        if (activeRun && activeRun.id) {
          completion = await api.get('/runtime/runs/' + encodeURIComponent(activeRun.id) + '/completion').catch(() => null);
          if (!_mounted) return;
        }

        this.workspaceRunHistory = runs;
        this.workspaceActiveRun = activeRun;
        this.workspaceCompletion = completion;
        this.workspaceArtifacts = artifacts;
        this.workspaceApprovals = approvals;
        const activeApproval = approvals.find(ticket => String(ticket.run_id || '').trim() === String((activeRun && activeRun.id) || '').trim()) || approvals[0] || null;
        const activeApprovalID = approvalTicketID(activeApproval);
        if (this.dismissedApprovalPopupID && !approvals.some(ticket => approvalTicketID(ticket) === this.dismissedApprovalPopupID)) {
          this.dismissedApprovalPopupID = '';
        }
        if (activeRun && String(activeRun.status || '').trim().toLowerCase() === 'waiting_approval' && activeApproval && this.dismissedApprovalPopupID !== activeApprovalID) {
          this.approvalPopup = buildApprovalPopup(activeApproval);
        } else if (this.approvalPopup) {
          const popupID = approvalTicketID(this.approvalPopup);
          if (!popupID || !approvals.some(ticket => approvalTicketID(ticket) === popupID)) {
            this.approvalPopup = null;
          } else if (activeApprovalID && popupID === activeApprovalID) {
            this.approvalPopup = buildApprovalPopup(activeApproval);
          }
        }
      } catch (err) {
        if (_mounted) {
          this.workspaceError = err && err.message ? err.message : String(err);
        }
      } finally {
        if (_mounted) this.workspaceLoading = false;
      }
    },

    hasAuthToken() {
      return Boolean(getToken());
    },

    toggleTool(key) {
      this.expandedTools = { ...this.expandedTools, [key]: !this.expandedTools[key] };
    },

    autoResize(e) {
      const ta = e.target;
      ta.style.height = 'auto';
      ta.style.height = Math.min(ta.scrollHeight, 160) + 'px';
    },

    handleKeydown(e) {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        this.send();
      }
    },

    triggerFileInput() {
      const fi = document.getElementById('hc-chat-file-input');
      if (fi) fi.click();
    },

    clearAttachment() {
      this.attachment = null;
      this.pendingFileDataURI = '';
      this.attachmentReading = false;
    },

    setAttachment(file) {
      this.attachment = file || null;
      this.pendingFileDataURI = '';
      this.attachmentReading = false;
      if (!file || !this.isImageAttachment(file)) return;

      this.attachmentReading = true;
      const reader = new FileReader();
      reader.onload = (event) => {
        if (!_mounted) return;
        this.pendingFileDataURI = String(event && event.target && event.target.result || '');
        this.attachmentReading = false;
      };
      reader.onerror = () => {
        if (!_mounted) return;
        this.pendingFileDataURI = '';
        this.attachmentReading = false;
        showToast(t('uploadFile') || 'Upload file', 'error');
      };
      reader.readAsDataURL(file);
    },

    isImageAttachment(file) {
      if (!file) return false;
      const type = String(file.type || '').toLowerCase();
      if (type.startsWith('image/')) return true;
      const name = String(file.name || '').toLowerCase();
      return /\.(png|jpe?g|gif|webp)$/.test(name);
    },

    parseDataURI(uri) {
      const value = String(uri || '').trim();
      if (!value.startsWith('data:')) return null;
      const comma = value.indexOf(',');
      if (comma <= 5) return null;
      const header = value.slice(5, comma);
      if (!/;base64$/i.test(header)) return null;
      return {
        mediaType: header.replace(/;base64$/i, ''),
        data: value.slice(comma + 1),
      };
    },

    attachmentContentBlocks(uploadResp) {
      if (!this.attachment) return [];
      const file = this.attachment;
      const filename = String((uploadResp && uploadResp.filename) || file.name || '').trim();
      const contentType = String((uploadResp && uploadResp.content_type) || file.type || '').trim();
      if (this.isImageAttachment(file)) {
        const parsed = this.parseDataURI(this.pendingFileDataURI);
        if (parsed && parsed.data) {
          return [{
            type: 'image',
            label: filename,
            media_type: parsed.mediaType || contentType,
            data: parsed.data,
          }];
        }
      }
      const attachmentRef = String((uploadResp && uploadResp.attachment_ref) || '').trim();
      if (!attachmentRef) return [];
      return [{
        type: 'file',
        label: filename,
        media_type: contentType,
        media_ref: 'upload://' + attachmentRef,
      }];
    },

    handleFileInput(e) {
      const fi = e.target;
      if (fi.files && fi.files.length > 0) {
        this.setAttachment(fi.files[0]);
      }
      fi.value = '';
    },

    scrollToBottom() {
      const el = document.getElementById('hc-chat-messages');
      if (el) el.scrollTop = el.scrollHeight;
    },

    focusInput() {
      const ta = document.getElementById('hc-chat-input');
      if (!ta) return;
      ta.focus();
      ta.style.height = 'auto';
      ta.style.height = Math.min(ta.scrollHeight, 160) + 'px';
    },

    applyPreflightReply(value, sendNow = false) {
      if (this.streaming) return;
      if (value) {
        this.input = value;
      }
      this.$nextTick(() => this.focusInput());
      if (sendNow && this.input.trim()) {
        this.send();
      }
    },

    // ---------------------------------------------------------------------------
    // Message actions
    // ---------------------------------------------------------------------------

    copyMessage(content) {
      navigator.clipboard.writeText(content).then(() => {
        showToast(t('copied') || 'Copied', 'success');
      }).catch(() => {});
    },

    retryFromMessage(mi) {
      if (this.streaming) return;
      const current = this.msgs[mi] || null;
      const runID = String(current && current.runId || '').trim();
      if (runID) {
        this.streaming = true;
        this.streamText = '';
        this.tools = [];
        showToast((t('chatReceiptActionRetryRun') || 'Retry run') + ' ' + shortID(runID), 'info');
        this.$nextTick(() => this.scrollToBottom());
        const turnId = newInteractionTurnId();
        this.interactionTurnId = turnId;
        this.turnStreamSeen = false;
        this.completedInteractionTurnId = '';
        const payload = retryInteractionPayloadForRun(store.sessionKey, runID);
        payload.metadata = buildInteractionMetadata(turnId);
        api.post('/runtime/interact', payload).then(result => {
          if (_mounted) this.handleInteractionResult(result, { turnId });
        }).catch(err => { if (_mounted) this.handleSendError(err); });
        return;
      }
      // Find the user message before this assistant message
      for (let i = mi - 1; i >= 0; i--) {
        if (this.msgs[i].role === 'user') {
          this.input = this.msgs[i].content;
          this.$nextTick(() => this.send());
          return;
        }
      }
    },

    // ---------------------------------------------------------------------------
    // Interaction & run submission
    // ---------------------------------------------------------------------------

    handleSubmittedRun(run) {
      if (!_mounted || !run) return;
      this.runId = run.id || '';
      this.sessId = run.session_id || '';
      const notice = buildPreflightNotice(run);
      const preflightCard = buildPreflightCard(run);
      const taskContractCard = buildTaskContractCard(run);
      const governanceCard = buildGovernanceCard(run.governance, run.governance_trace, run.id);
      if (!notice && !preflightCard && !taskContractCard && !governanceCard) {
        this.refreshWorkspace();
        return;
      }
      this.msgs = [...this.msgs, {
        role: 'assistant',
        content: preflightCard || taskContractCard || governanceCard ? '' : ('前置条件：' + notice),
        preflightCard,
        taskContractCard,
        governanceCard,
        time: new Date().toISOString(),
      }];
      this.$nextTick(() => this.scrollToBottom());
      this.refreshWorkspace();
    },

    handleInteractionResult(result, submission = {}) {
      if (!_mounted || !result) return;
      const decision = result.decision || {};
      const run = result.run || null;
      const turnId = String((submission && submission.turnId) || '').trim();
      if (decision.reply_act === 'task_accept' && run) {
        if (turnId && this.interactionTurnId === turnId) {
          this.interactionTurnId = '';
          this.turnStreamSeen = false;
        }
        this.handleSubmittedRun(run);
        return;
      }

      if (decision.reply_act === 'resume_ack' && run && result.approval_status !== 'denied') {
        if (turnId && this.interactionTurnId === turnId) {
          this.interactionTurnId = '';
          this.turnStreamSeen = false;
        }
        this.runId = run.id || '';
        this.sessId = run.session_id || '';
        const hasPendingPreflight = Boolean(run.preflight && run.preflight.state && run.preflight.state !== 'ready');
        const message = String(result.message || '').trim();
        if (message && !hasPendingPreflight) {
          this.msgs = [...this.msgs, {
            role: 'assistant',
            content: message,
            time: new Date().toISOString(),
          }];
        }
        this.handleSubmittedRun(run);
        return;
      }

      if (turnId) {
        if (this.completedInteractionTurnId === turnId) {
          this.refreshWorkspace();
          return;
        }
        if (this.interactionTurnId === turnId && this.turnStreamSeen) {
          if (String(result.error || '').trim()) {
            this.streaming = false;
            this.streamText = '';
            this.tools = [];
            this.interactionTurnId = '';
            this.turnStreamSeen = false;
            this.msgs = [...this.msgs, {
              role: 'assistant',
              content: String(result.message || result.error || '').trim(),
              time: new Date().toISOString(),
            }];
            this.$nextTick(() => this.scrollToBottom());
          }
          this.refreshWorkspace();
          return;
        }
      }

      this.streaming = false;
      this.streamText = '';
      this.tools = [];
      if (turnId && this.interactionTurnId === turnId) {
        this.interactionTurnId = '';
        this.turnStreamSeen = false;
      }

      const message = String(result.message || '').trim();
      if (message) {
        this.msgs = [...this.msgs, {
          role: 'assistant',
          content: message,
          time: new Date().toISOString(),
        }];
        this.$nextTick(() => this.scrollToBottom());
      }
      this.refreshWorkspace();
    },

    isNearBottom() {
      const el = document.getElementById('hc-chat-messages');
      if (!el) return true;
      return el.scrollHeight - el.scrollTop - el.clientHeight < SCROLL_THRESHOLD_PX;
    },

    // ---------------------------------------------------------------------------
    // Send message
    // ---------------------------------------------------------------------------

    send() {
      const text = this.input.trim();
      if ((!text && !this.attachment) || this.streaming) return;
      if (this.attachment && this.attachmentReading) {
        showToast(t('uploadFile') || 'Upload file', 'info');
        return;
      }

      const previewText = text || attachmentPreviewText(this.attachment);
      this.msgs.push({ role: 'user', content: previewText, time: new Date().toISOString() });
      this.input = '';
      // Reset textarea height
      const ta = document.getElementById('hc-chat-input');
      if (ta) ta.style.height = 'auto';

      this.streaming = true;
      this.streamText = '';
      this.tools = [];
      const turnId = newInteractionTurnId();
      this.interactionTurnId = turnId;
      this.turnStreamSeen = false;
      this.completedInteractionTurnId = '';
      this.$nextTick(() => this.scrollToBottom());

      const submitBody = {
        session_key: store.sessionKey,
        content: text,
        metadata: buildInteractionMetadata(turnId),
      };

      if (this.attachment) {
        const formData = new FormData();
        formData.append('file', this.attachment);
        const headers = {};
        const tok = getToken();
        if (tok) headers['Authorization'] = 'Bearer ' + tok;
        const csrf = document.cookie.split(';').map(v => v.trim()).find(v => v.startsWith('_hopclaw_csrf='));
        if (csrf) headers['X-CSRF-Token'] = decodeURIComponent(csrf.split('=').slice(1).join('='));

        fetch(consolePath('/upload'), { method: 'POST', headers, body: formData })
          .then(r => r.json())
          .then(data => {
            if (!_mounted) return;
            const contentBlocks = buildSubmissionContentBlocks(text, this.attachmentContentBlocks(data));
            if (contentBlocks.length > 0) {
              submitBody.content_blocks = contentBlocks;
            }
            return api.post('/runtime/interact', submitBody);
          })
          .then(result => {
            if (!_mounted) return;
            this.clearAttachment();
            this.handleInteractionResult(result, { turnId });
          })
          .catch(err => { if (_mounted) this.handleSendError(err); });
        return;
      }

      api.post('/runtime/interact', submitBody).then(result => {
        this.handleInteractionResult(result, { turnId });
      }).catch(err => { if (_mounted) this.handleSendError(err); });
    },

    handleSendError(err) {
      this.streaming = false;
      this.streamText = '';
      this.tools = [];
      this.interactionTurnId = '';
      this.turnStreamSeen = false;
      this.msgs.push({
        role: 'assistant',
        content: '[Error: ' + err.message + ']',
        time: new Date().toISOString(),
      });
      this.$nextTick(() => this.scrollToBottom());
    },

    appendResultCard(runId, completion) {
      const card = buildResultCard(runId, completion);
      const result = completionResult(completion);
      const governanceCard = buildGovernanceCard(
        (completion && completion.governance) || (result && result.governance) || null,
        null,
        runId
      );
      if (!card && !governanceCard) return false;
      this.msgs = [...this.msgs, {
        role: 'assistant',
        content: '',
        resultCard: card,
        governanceCard,
        time: new Date().toISOString(),
        runId,
      }];
      this.$nextTick(() => this.scrollToBottom());
      return true;
    },

    async appendFallbackAssistant(runId, toolsCopy) {
      try {
        const run = await api.get('/runtime/runs/' + encodeURIComponent(runId));
        if (!_mounted || !run) return;
        const sid = run.session_id || '';
        if (!sid) return;
        const sess = await api.get('/runtime/sessions/' + encodeURIComponent(sid) + '?include=messages');
        if (!_mounted || !sess) return;
        const msgs = sess.messages || [];
        for (let i = msgs.length - 1; i >= 0; i--) {
          if ((msgs[i].role) === 'assistant') {
            this.msgs = [...this.msgs, {
              role: 'assistant',
              content: msgs[i].content || '',
              time: new Date().toISOString(),
              toolCalls: toolsCopy,
              runId,
            }];
            this.$nextTick(() => this.scrollToBottom());
            this.refreshSessions();
            return;
          }
        }
      } catch (_) {}
    },

    // ---------------------------------------------------------------------------
    // Approvals
    // ---------------------------------------------------------------------------

    async resolveApproval(ticketId, decision) {
      try {
        const status = decision === 'approve' ? 'approved' : 'denied';
        await api.post('/operator/approvals/' + encodeURIComponent(ticketId) + '/resolve', { status, decision });
        if (!_mounted) return;
        this.approvalPopup = null;
        if (this.dismissedApprovalPopupID === ticketId) this.dismissedApprovalPopupID = '';
        showToast(decision === 'approve' ? 'Approved' : 'Denied', 'success');
        this.refreshWorkspace();
      } catch (_) { /* toast shown by api */ }
    },

    async cancelApproval(ticketId) {
      try {
        await api.post('/operator/approvals/' + encodeURIComponent(ticketId) + '/cancel', {});
        if (!_mounted) return;
        this.approvalPopup = null;
        if (this.dismissedApprovalPopupID === ticketId) this.dismissedApprovalPopupID = '';
        showToast('Cancelled', 'info');
        this.refreshWorkspace();
      } catch (_) { /* toast shown by api */ }
    },

    dismissApprovalPopup() {
      const popupID = approvalTicketID(this.approvalPopup);
      if (popupID) this.dismissedApprovalPopupID = popupID;
      this.approvalPopup = null;
    },

    // ---------------------------------------------------------------------------
    // SSE connection
    // ---------------------------------------------------------------------------

    connectSSE() {
      const generation = ++sseGeneration;
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
      if (xhr) {
        try { xhr.abort(); } catch (_) {}
        xhr = null;
      }
      this.connectionStatus = 'connecting';

      let url = '/runtime/events/stream';
      if (this.lastEventId) url += '?since=' + encodeURIComponent(this.lastEventId);

      const x = new XMLHttpRequest();
      let lastIdx = 0;
      x.open('GET', url, true);

      const tok = getToken();
      if (tok) x.setRequestHeader('Authorization', 'Bearer ' + tok);
      x.setRequestHeader('Accept', 'text/event-stream');
      x.setRequestHeader('Cache-Control', 'no-cache');

      const self = this;
      xhr = x;

      x.onreadystatechange = function () {
        if (!_mounted || generation !== sseGeneration || xhr !== x) {
          try { x.abort(); } catch (_) {}
          return;
        }

        if (x.readyState === XMLHttpRequest.HEADERS_RECEIVED) {
          if (x.status === 200) {
            if (_mounted) {
              self.connectionStatus = 'ok';
              self.retries = 0;
            }
          } else {
            if (_mounted) {
              self.connectionStatus = 'err';
            }
            xhr = null;
            try { x.abort(); } catch (_) {}
            if (_mounted) self.scheduleReconnect(x.status, generation);
            return;
          }
        }
        if (x.readyState >= 3 && _mounted) {
          const chunk = x.responseText.substring(lastIdx);
          lastIdx = x.responseText.length;
          self.parseSSEChunk(chunk);
          if (x.responseText.length > SSE_RESPONSE_LIMIT) {
            try { x.abort(); } catch (_) {}
            xhr = null;
            if (_mounted && generation === sseGeneration) self.connectSSE();
            return;
          }
        }
        if (x.readyState === 4) {
          if (generation !== sseGeneration || xhr !== x) return;
          xhr = null;
          if (_mounted) {
            self.connectionStatus = 'err';
            self.scheduleReconnect(x.status, generation);
          }
        }
      };

      x.onerror = function () {
        if (!_mounted || generation !== sseGeneration || xhr !== x) return;
        xhr = null;
        if (_mounted) {
          self.connectionStatus = 'err';
          self.scheduleReconnect(x.status || 0, generation);
        }
      };

      x.send();
    },

    parseSSEChunk(chunk) {
      this.sseBuf += chunk;
      const lines = this.sseBuf.split('\n');
      this.sseBuf = lines.pop() || '';
      for (const line of lines) {
        const trimmed = line.trim();
        if (trimmed.indexOf('data: ') === 0) {
          try { this.handleEvent(JSON.parse(trimmed.substring(6))); }
          catch (_) { /* skip malformed */ }
        }
      }
    },

    handleEvent(ev) {
      if (!_mounted) return;
      if (ev.id) this.lastEventId = ev.id;

      const eventTurnId = String((ev.attrs && ev.attrs.interaction_turn_id) || '').trim();
      const isCurrentRun = this.streaming && !!this.runId && ev.run_id === this.runId;
      const isCurrentTurn = this.streaming && !this.runId && !!this.interactionTurnId && eventTurnId === this.interactionTurnId;

      switch (ev.type) {
        case 'model.text_delta':
          if (isCurrentRun || isCurrentTurn) {
            if (isCurrentTurn) this.turnStreamSeen = true;
            this.streamText += (ev.attrs && ev.attrs.delta) || '';
          }
          break;

        case 'model.stream_complete':
          if (isCurrentTurn) {
            this.turnStreamSeen = true;
            this.finalizeRunlessTurn(this.interactionTurnId);
          }
          break;

        case 'tool.executed':
          if (isCurrentRun && ev.attrs) {
            const toolNames = ev.attrs.tool_names || [];
            const results = ev.attrs.results || [];
            for (let i = 0; i < toolNames.length; i++) {
              this.tools = [...this.tools, {
                id: ev.id + '-' + i,
                name: toolNames[i] || 'tool',
                input: ev.attrs.inputs ? ev.attrs.inputs[i] : null,
                result: results[i] || null,
              }];
            }
          }
          break;

        case 'run.completed':
          if (isCurrentRun) {
            if (ev.attrs) {
              if (ev.attrs.model) this.modelName = ev.attrs.model;
              if (ev.attrs.total_tokens) this.tokenCount += ev.attrs.total_tokens;
            }
            this.finalizeRun(ev.run_id);
          }
          break;

        case 'run.failed':
          if (isCurrentRun) {
            if (!this.streamText) this.streamText = '[Error: run failed]';
            if (ev.attrs && ev.attrs.error) this.streamText = '[Error: ' + ev.attrs.error + ']';
            this.finalizeRun(ev.run_id);
          }
          break;

        case 'run.cancelled':
          if (isCurrentRun) {
            if (!this.streamText) this.streamText = '[Run cancelled]';
            this.finalizeRun(ev.run_id);
          }
          break;

        case 'approval.requested':
        case 'approval_requested':
          if (ev.attrs) {
            const ticket = approvalFromEvent(ev);
            const popup = buildApprovalPopup(ticket);
            if (popup && popup.id && this.dismissedApprovalPopupID === popup.id) {
              this.dismissedApprovalPopupID = '';
            }
            this.approvalPopup = popup;
            this.refreshWorkspace();
          }
          break;

        case 'approval.resolved':
        case 'approval_resolved': {
          const approvalID = String((ev.attrs && (ev.attrs.approval_id || ev.attrs.ticket_id || ev.attrs.id)) || '').trim();
          if (approvalID && this.approvalPopup && approvalTicketID(this.approvalPopup) === approvalID) {
            this.approvalPopup = null;
          }
          if (approvalID && this.dismissedApprovalPopupID === approvalID) {
            this.dismissedApprovalPopupID = '';
          }
          this.refreshWorkspace();
          break;
        }
      }
    },

    finalizeRun(runId) {
      if (!this.streaming) return;

      const text = this.streamText;
      const toolsCopy = this.tools.slice();
      this.streaming = false;
      this.streamText = '';
      this.tools = [];
      this.runId = '';
      this.interactionTurnId = '';
      this.turnStreamSeen = false;

      if (text) {
        this.msgs = [...this.msgs, { role: 'assistant', content: text, time: new Date().toISOString(), toolCalls: toolsCopy, runId: runId }];
        this.$nextTick(() => this.scrollToBottom());
      }
      api.get('/runtime/runs/' + encodeURIComponent(runId) + '/completion').catch(() => null).then(async completion => {
        if (!_mounted) return;
        const result = completionResult(completion);
        const output = String((result && result.output) || '').trim();
        if (!text && output) {
          this.msgs = [...this.msgs, {
            role: 'assistant',
            content: output,
            time: new Date().toISOString(),
            toolCalls: toolsCopy,
            runId,
          }];
          this.$nextTick(() => this.scrollToBottom());
        }
        const appendedCard = this.appendResultCard(runId, completion);
        if (!text && !output && !appendedCard) {
          await this.appendFallbackAssistant(runId, toolsCopy);
          this.refreshWorkspace();
          return;
        }
        this.refreshSessions();
        this.refreshWorkspace();
      }).catch(() => {});
    },

    finalizeRunlessTurn(turnId) {
      if (!this.streaming) return;
      const currentTurnId = String(turnId || '').trim();
      if (currentTurnId && currentTurnId !== this.interactionTurnId) return;

      const text = this.streamText;
      this.streaming = false;
      this.streamText = '';
      this.tools = [];
      this.runId = '';
      this.completedInteractionTurnId = currentTurnId;
      this.interactionTurnId = '';
      this.turnStreamSeen = false;

      if (text) {
        this.msgs = [...this.msgs, {
          role: 'assistant',
          content: text,
          time: new Date().toISOString(),
        }];
        this.$nextTick(() => this.scrollToBottom());
      }
      this.refreshSessions();
      this.refreshWorkspace();
    },

    scheduleReconnect(status = 0, generation = sseGeneration) {
      if (!_mounted || generation !== sseGeneration) return;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      this.retries++;
      if (this.retries > MAX_RECONNECT_ATTEMPTS) {
        showToast(t('connectionLost') || 'Connection lost. Please refresh.', 'error');
        return;
      }
      const delay = status === 429 ? 10000 : RECONNECT_DELAY_MS;
      reconnectTimer = setTimeout(() => {
        if (_mounted && generation === sseGeneration) this.connectSSE();
      }, delay);
    },

    // ---------------------------------------------------------------------------
    // Session management
    // ---------------------------------------------------------------------------

    async refreshSessions() {
      try {
        const items = await fetchSharedSessionList({ force: true, background: true });
        if (!_mounted) return;
        store.sessionList = items;
      } catch (_) { /* ignore */ }
    },

    async loadHistory() {
      try {
        const items = await fetchSharedSessionList({ force: true, background: true });
        if (!_mounted) return;
        store.sessionList = items;

        let sessId = '';
        for (const item of items) {
          if ((item.key || item.id) === store.sessionKey) {
            sessId = item.id || '';
            this.sessId = sessId;
            break;
          }
        }
        if (!sessId) return;

        const sess = await api.get('/runtime/sessions/' + encodeURIComponent(sessId) + '?include=messages');
        if (!_mounted || !sess) return;

        const msgs = sess.messages || [];
        const loadedMsgs = [];
        for (const m of msgs) {
          const role = m.role || '';
          if (role === 'user' || role === 'assistant') {
            const tc = [];
            if (role === 'assistant' && m.tool_calls) {
              for (const tcall of m.tool_calls) {
                tc.push({ id: tcall.ID || tcall.id || '', name: tcall.Name || tcall.name || '', input: tcall.Arguments || tcall.arguments || '' });
              }
            }
            loadedMsgs.push({
              role,
              content: m.content || '',
              time: m.created_at || '',
              toolCalls: tc,
            });
          }
        }
        this.msgs = loadedMsgs;
        if (sess.model) this.modelName = sess.model;
        this.$nextTick(() => this.scrollToBottom());
        this.refreshWorkspace();
      } catch (_) { /* ignore */ }
    },

    // ---------------------------------------------------------------------------
    // Drag & drop
    // ---------------------------------------------------------------------------

    setupDragDrop() {
      const self = this;
      boundDragenter = (e) => {
        e.preventDefault();
        dragCounter++;
        self.dragOver = true;
      };
      boundDragleave = (e) => {
        e.preventDefault();
        dragCounter--;
        if (dragCounter <= 0) {
          dragCounter = 0;
          self.dragOver = false;
        }
      };
      boundDragover = (e) => { e.preventDefault(); };
      boundDrop = (e) => {
        e.preventDefault();
        dragCounter = 0;
        self.dragOver = false;
        if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length > 0) {
          self.setAttachment(e.dataTransfer.files[0]);
        }
      };

      document.addEventListener('dragenter', boundDragenter);
      document.addEventListener('dragleave', boundDragleave);
      document.addEventListener('dragover', boundDragover);
      document.addEventListener('drop', boundDrop);
    },

    teardownDragDrop() {
      if (boundDragenter) document.removeEventListener('dragenter', boundDragenter);
      if (boundDragleave) document.removeEventListener('dragleave', boundDragleave);
      if (boundDragover) document.removeEventListener('dragover', boundDragover);
      if (boundDrop) document.removeEventListener('drop', boundDrop);
      boundDragenter = null;
      boundDragleave = null;
      boundDragover = null;
      boundDrop = null;
    },

    // ---------------------------------------------------------------------------
    // Lifecycle
    // ---------------------------------------------------------------------------

    mounted() {
      _mounted = true;
      this.setupDragDrop();
      this.loadHistory();
      this.refreshWorkspace();
      this.connectSSE();
    },

    unmounted() {
      _mounted = false;
      sseGeneration++;
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
      if (xhr) { try { xhr.abort(); } catch (_) {} xhr = null; }
      this.teardownDragDrop();
    },
  };
}

// ---------------------------------------------------------------------------
// Session List component (sidebar)
// ---------------------------------------------------------------------------

export function SessionList() {
  const store = window._hcStore;
  let _mounted = false;

  const $template = `
    <div>
      <input class="hc-session-search" type="text" v-model="searchQuery"
        :placeholder="t('search') || 'Search...'" />
      <div v-if="filteredSessions().length === 0 && !searchQuery" class="hc-session-empty">{{ t('noSessions') }}</div>
      <div v-else-if="filteredSessions().length === 0 && searchQuery" class="hc-session-empty hc-session-empty-search">No matches</div>
      <div v-else>
        <button class="hc-new-session-btn" @click="newSession()">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          {{ t('newSession') }}
        </button>
        <div v-for="sess in filteredSessions()" :key="sess.key || sess.id"
             class="hc-session-item" :class="{ active: (sess.key || sess.id) === store.sessionKey }"
             @click="switchSession(sess.key || sess.id)"
             @mouseenter="$event.currentTarget.querySelector('.hc-sess-delete') && ($event.currentTarget.querySelector('.hc-sess-delete').style.display='inline-flex')"
             @mouseleave="$event.currentTarget.querySelector('.hc-sess-delete') && ($event.currentTarget.querySelector('.hc-sess-delete').style.display='none')">
          <div class="hc-sess-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" width="16" height="16"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg></div>
          <div class="hc-sess-info">
            <div class="hc-sess-key">{{ sess.key || sess.id }}</div>
            <div class="hc-sess-meta">{{ sess.message_count || 0 }} {{ t('messages') }}{{ fmtDate(sess.updated_at || sess.created_at) ? ' \u00b7 ' + fmtDate(sess.updated_at || sess.created_at) : '' }}</div>
          </div>
          <button class="hc-sess-delete"
            @click.stop="deleteSession(sess.id || sess.ID)" :title="t('delete')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
          </button>
        </div>
      </div>
    </div>
  `;

  return {
    $template,
    store,
    t,
    searchQuery: '',

    fmtDate,

    filteredSessions() {
      const q = (this.searchQuery || '').trim().toLowerCase();
      if (!q) return store.sessionList;
      return store.sessionList.filter(sess => {
        const key = (sess.key || sess.id || '').toLowerCase();
        return key.indexOf(q) !== -1;
      });
    },

    async loadSessions() {
      try {
        const items = await fetchSharedSessionList({ force: true, background: true });
        if (!_mounted) return;
        store.sessionList = items;
      } catch (_) { /* ignore */ }
    },

    async deleteSession(id) {
      if (!confirm(t('sessionsConfirmDelete') || 'Delete this session?')) return;
      try {
        await api.del('/runtime/sessions/' + encodeURIComponent(id));
        showToast(t('delete') || 'Deleted', 'success');
        this.loadSessions();
      } catch (_) { /* ignore */ }
    },

    newSession() {
      const counter = parseInt(localStorage.getItem(SESSION_COUNTER_KEY) || '0', 10) + 1;
      localStorage.setItem(SESSION_COUNTER_KEY, String(counter));
      const newKey = 'webchat-' + counter;
      store.sessionKey = newKey;
      localStorage.setItem(SESSION_KEY_STORAGE, newKey);

      // Navigate to assistant view (or reload if already there)
      const target = '#/assistant';
      if (store.route === target) {
        store.route = '';
        requestAnimationFrame(() => { store.route = target; });
      } else {
        window.location.hash = target;
      }

      // Close sidebar on mobile
      store.sidebarOpen = false;
    },

    switchSession(key) {
      if (key === store.sessionKey) return;
      store.sessionKey = key;
      localStorage.setItem(SESSION_KEY_STORAGE, key);

      // Navigate to assistant view (or reload if already there)
      const target = '#/assistant';
      if (store.route === target) {
        // Force chat view to reload by toggling route
        store.route = '';
        requestAnimationFrame(() => { store.route = target; });
      } else {
        window.location.hash = target;
      }

      store.sidebarOpen = false;
    },

    mounted() {
      _mounted = true;
    },
    unmounted() {
      _mounted = false;
    },
  };
}
