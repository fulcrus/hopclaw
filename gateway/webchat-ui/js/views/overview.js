// ---------------------------------------------------------------------------
// Overview View - Operator home
// ---------------------------------------------------------------------------

import { api } from '../api.js';
import { t } from '../i18n/index.js';
import { operatorStartupDiagnostics, operatorStatusDotClass, operatorStatusLabel, operatorStatusState, operatorStatusSummary } from './status_state.js';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const AUTO_REFRESH_MS = 30000;
const HEALTHY_CAPABILITY_STATES = ['healthy', 'ok', 'ready', 'green'];
const HEALTHY_MODULE_STATES = ['healthy', 'ok', 'ready', 'green'];
const CONNECTED_CHANNEL_STATES = ['connected'];
const WARNING_CHANNEL_STATES = ['startup_grace', 'stale_socket'];
const SUCCESS_RUN_STATES = ['completed', 'partial', 'completed_warning'];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function fmtTime(iso) {
  try {
    return new Date(iso).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  } catch (_) { return ''; }
}

function capHealth(value) {
  if (!value) return '';
  if (typeof value === 'string') return value.toLowerCase();
  if (typeof value === 'object') return (value.status || value.Status || '').toLowerCase();
  return String(value).toLowerCase();
}

function moduleHealth(value) {
  if (!value) return '';
  if (typeof value === 'string') return value.toLowerCase();
  if (typeof value === 'object') return (value.status || value.Status || '').toLowerCase();
  return String(value).toLowerCase();
}

function healthDot(health) {
  if (HEALTHY_CAPABILITY_STATES.includes(health)) return 'ok';
  if (health === 'degraded' || health === 'yellow' || health === 'warning') return 'warn';
  return 'err';
}

function formatStatusText(value) {
  if (!value) return '-';
  return String(value).replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function uniqueNames(items) {
  return Array.from(new Set((items || []).filter(Boolean)));
}

function overviewRunStatus(run) {
  if (!run) return '';
  const outcome = String(run.outcome || '').toLowerCase();
  if (outcome) return outcome;
  const status = String(run.status || run.Status || '').toLowerCase();
  const verification = String(run.verification_status || '').toLowerCase();
  if (status === 'completed' && verification === 'failed') return 'verification_failed';
  if (status === 'completed' && verification === 'warning') return 'completed_warning';
  return status;
}

function overviewCompletionSummary(completion) {
  if (!completion) return '';
  const bundle = completion.bundle || {};
  const result = completion.result || {};
  return String(bundle.summary || result.summary || result.output || '').trim();
}

function overviewCompletionBlocks(completion) {
  if (!completion) return [];
  const bundle = completion.bundle || {};
  const result = completion.result || {};
  const delivery = completion.delivery || bundle.delivery || {};
  if (Array.isArray(delivery.blocks) && delivery.blocks.length) return delivery.blocks;
  if (result.output) return [{ kind: 'text', title: 'Output', content: result.output }];
  return [];
}

function overviewCompletionArtifacts(completion) {
  if (!completion) return [];
  const bundle = completion.bundle || {};
  const result = completion.result || {};
  const delivery = completion.delivery || bundle.delivery || {};
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
  if (Array.isArray(bundle.deliverables)) items.push(...bundle.deliverables);
  if (Array.isArray(result.deliverables)) items.push(...result.deliverables);
  const seen = new Set();
  return items.filter(item => {
    const key = String(item.uri || item.name || item.label || item.kind || '').trim();
    if (!key || seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function settledValue(result) {
  return result && result.status === 'fulfilled' ? result.value : null;
}

function formatPercent(value, digits = 0) {
  const num = Number(value);
  if (!Number.isFinite(num)) return '-';
  const scaled = (num * 100).toFixed(digits);
  return scaled.replace(/\.0+$/, '').replace(/(\.\d*[1-9])0+$/, '$1') + '%';
}

function normalizeItems(payload) {
  if (Array.isArray(payload)) return payload;
  if (!payload || typeof payload !== 'object') return [];
  if (Array.isArray(payload.items)) return payload.items;
  if (Array.isArray(payload.suites)) return payload.suites;
  return [];
}

// ---------------------------------------------------------------------------
// Overview View component
// ---------------------------------------------------------------------------

export function OverviewView() {
  let _mounted = false;
  let refreshTimer = null;

  const $template = `
    <div
      class="hc-monitor"
      data-testid="overview-view"
      :data-status-unavailable="statusUnavailable ? 'true' : 'false'"
      :data-backend-issue="backendIssueActive() ? 'true' : 'false'"
      :data-runs-unavailable="runsUnavailable ? 'true' : 'false'"
    >
      <div class="hc-overview-hero">
        <div class="hc-page-intro-copy">
          <div class="hc-page-intro-title">{{ t('overviewTitle') || 'Overview' }}</div>
          <p class="hc-overview-hero-copy">
            {{ overviewHeadline() }}
          </p>
        </div>
        <div class="hc-nav-chip-list">
          <a class="hc-nav-chip" data-testid="overview-open-workspace" href="#/assistant">{{ t('overviewOpenWorkspace') || 'Open workspace' }}</a>
          <a class="hc-nav-chip" href="#/settings/models">{{ t('overviewReviewModels') || 'Review models' }}</a>
          <a class="hc-nav-chip" href="#/settings/channels">{{ t('overviewReviewChannels') || 'Review channels' }}</a>
        </div>
      </div>

      <div v-if="loading" class="hc-loading">{{ t('loading') }}</div>
      <div v-else-if="error" class="hc-empty">
        <div style="color:var(--danger);margin-bottom:12px">{{ t('loadError') }}</div>
        <button class="hc-btn hc-btn-primary" @click="loadAll()">{{ t('retryLoad') }}</button>
      </div>
      <div v-else class="hc-overview-body">
        <div class="hc-overview-metrics">
          <div class="hc-monitor-card">
            <div class="hc-monitor-card-label">{{ t('overviewStatus') || 'System status' }}</div>
            <div class="hc-monitor-card-value hc-monitor-card-inline">
              <span class="hc-status-dot" :class="statusDotClass()"></span>
              {{ statusLabel() }}
            </div>
            <div class="hc-monitor-card-sub" v-if="statusStatusSubline()">{{ statusStatusSubline() }}</div>
          </div>

          <div class="hc-monitor-card">
            <div class="hc-monitor-card-label">{{ t('overviewSetupReadiness') || 'Setup readiness' }}</div>
            <div class="hc-monitor-card-value">{{ setupReadinessValue() }}</div>
            <div class="hc-monitor-card-sub">{{ setupReadinessSummary() }}</div>
          </div>

          <div
            class="hc-monitor-card"
            data-testid="overview-release-readiness-card"
            :data-state="releaseReadinessState()"
          >
            <div class="hc-monitor-card-label">{{ t('overviewReleaseReadiness') || 'Release readiness' }}</div>
            <div class="hc-monitor-card-value">{{ releaseReadinessValue() }}</div>
            <div class="hc-monitor-card-sub">{{ releaseReadinessSummary() }}</div>
          </div>

          <div class="hc-monitor-card">
            <div class="hc-monitor-card-label">{{ t('overviewOperatorHelpers') || 'Operator helpers' }}</div>
            <div class="hc-monitor-card-value">{{ helperReadyValue() }}</div>
            <div class="hc-monitor-card-sub">{{ helperSummary() }}</div>
          </div>

          <div class="hc-monitor-card">
            <div class="hc-monitor-card-label">{{ t('overviewChannels') || 'Channels' }}</div>
            <div class="hc-monitor-card-value">{{ channelMetricValue() }}</div>
            <div class="hc-monitor-card-sub">{{ channelSummary() }}</div>
          </div>

          <div class="hc-monitor-card">
            <div class="hc-monitor-card-label">{{ t('overviewCapabilityPacks') || 'Capability packs' }}</div>
            <div class="hc-monitor-card-value">{{ moduleMetricValue() }}</div>
            <div class="hc-monitor-card-sub">{{ moduleSummary() }}</div>
          </div>

          <div class="hc-monitor-card">
            <div class="hc-monitor-card-label">{{ t('overviewRecentRuns') || 'Recent runs' }}</div>
            <div class="hc-monitor-card-value">{{ recentRunsValue() }}</div>
            <div class="hc-monitor-card-sub">
              <span v-if="runsUnavailable" style="color:var(--warning)">{{ t('overviewRunHistoryUnavailable') || 'Run history unavailable' }}</span>
              <span v-else-if="failedRuns > 0" style="color:var(--danger)">{{ failedRuns }} {{ t('overviewFailed') || 'failed' }}</span>
              <span v-else style="color:var(--success)">{{ t('overviewNoRecentFailures') || 'No recent failures' }}</span>
            </div>
          </div>
        </div>

        <div v-if="warnings.length > 0" class="hc-overview-warning-stack">
          <div v-for="w in warnings" :key="w" class="hc-overview-warning">{{ w }}</div>
        </div>

        <div class="hc-overview-grid">
          <div class="hc-card">
            <div class="hc-overview-section-head">
              <div>
                <div class="hc-overview-section-kicker">{{ t('overviewGettingReady') || 'Getting ready' }}</div>
                <div class="hc-overview-section-title">{{ t('overviewFinishSetup') || 'Finish the operator setup' }}</div>
              </div>
              <a class="hc-btn hc-btn-sm hc-btn-ghost" href="#/setup">{{ t('overviewOpenSetup') || 'Open setup' }}</a>
            </div>
            <div class="hc-readiness-list">
              <a v-for="item in readinessChecklist()" :key="item.key" class="hc-readiness-item" :href="item.href">
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
          </div>

          <div class="hc-card">
            <div class="hc-overview-section-head">
              <div>
                <div class="hc-overview-section-kicker">{{ t('overviewLiveStatus') || 'Live status' }}</div>
                <div class="hc-overview-section-title">{{ t('overviewCapabilitiesAndChannels') || 'Capabilities and channels' }}</div>
              </div>
              <a class="hc-btn hc-btn-sm hc-btn-ghost" href="#/settings/infrastructure">{{ t('overviewOpenInfra') || 'Open infra' }}</a>
            </div>
            <div v-if="modules.length === 0" class="hc-state-block hc-state-block-inline">
              <div class="hc-state-block-title">{{ modulesUnavailable ? (t('overviewCapPackUnavailableTitle') || 'Capability pack snapshot unavailable') : (t('overviewCapPackNoneTitle') || 'No capability packs reported yet') }}</div>
              <div class="hc-state-block-copy">{{ modulesUnavailable ? (t('overviewCapPackUnavailableDesc') || 'The console could not load capability pack status from the backend.') : (t('overviewCapPackNoneDesc') || 'Builtin packs and plugin packs will appear here with their source and health.') }}</div>
            </div>
            <div v-else class="hc-channel-list" style="margin-bottom:12px">
              <div v-for="item in moduleHighlights()" :key="moduleKey(item)" class="hc-channel-item">
                <div class="hc-channel-item-main">
                  <span class="hc-status-dot" :class="healthDot(moduleHealthStr(item))"></span>
                  <strong>{{ moduleName(item) }}</strong>
                  <span class="hc-channel-item-state">{{ formatStatusText(moduleHealthStr(item)) }}</span>
                </div>
                <div class="hc-channel-item-meta">
                  <span>{{ formatStatusText(moduleSource(item)) }}</span>
                  <span v-if="moduleDelivery(item)">{{ formatStatusText(moduleDelivery(item)) }}</span>
                  <span v-if="moduleVersion(item)">v{{ moduleVersion(item) }}</span>
                </div>
              </div>
            </div>
            <div v-if="capabilities.length === 0" class="hc-state-block hc-state-block-inline">
              <div class="hc-state-block-title">{{ capabilitiesUnavailable ? (t('overviewCapSnapshotUnavailableTitle') || 'Capability snapshot unavailable') : (t('overviewCapNoneTitle') || 'No capabilities reported yet') }}</div>
              <div class="hc-state-block-copy">{{ capabilitiesUnavailable ? (t('overviewCapSnapshotUnavailableDesc') || 'The console could not load operator capability health from the backend.') : (t('overviewCapNoneDesc') || 'Configure helpers or providers and their runtime health will appear here.') }}</div>
            </div>
            <div v-else class="hc-capability-pills">
              <div v-for="cap in capabilities" :key="capName(cap)" class="hc-capability-pill">
                <span class="hc-status-dot" :class="healthDot(capHealthStr(cap))"></span>
                <span>{{ capName(cap) }}</span>
                <span class="hc-capability-pill-status">{{ formatStatusText(capHealthStr(cap)) }}</span>
              </div>
            </div>
            <div v-if="channelHealth.length === 0" class="hc-state-block hc-state-block-inline" style="margin-top:12px">
              <div class="hc-state-block-title">{{ channelHealthUnavailable ? (t('overviewChannelHealthUnavailableTitle') || 'Channel health unavailable') : (t('overviewNoActiveChannelsTitle') || 'No active channel connections yet') }}</div>
              <div class="hc-state-block-copy">{{ channelHealthUnavailable ? (t('overviewChannelHealthUnavailableDesc') || 'The console could not load channel health, so this section is incomplete.') : (t('overviewNoActiveChannelsDesc') || 'Connect a primary work channel and live delivery health will appear here.') }}</div>
            </div>
            <div v-else class="hc-channel-list">
              <div v-for="item in channelHealth" :key="item.name" class="hc-channel-item">
                <div class="hc-channel-item-main">
                  <span class="hc-status-dot" :class="channelHealthDot(item.state)"></span>
                  <strong>{{ item.name }}</strong>
                  <span class="hc-channel-item-state">{{ formatStatusText(item.state) }}</span>
                </div>
                <div class="hc-channel-item-meta">
                  <span v-if="item.active_runs">{{ t('overviewRuns') || 'Runs' }} {{ item.active_runs }}</span>
                  <span v-if="item.restart_count">{{ t('overviewRestarts') || 'Restarts' }} {{ item.restart_count }}</span>
                  <span v-if="item.since">{{ fmtTime(item.since) }}</span>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div class="hc-overview-grid">
          <div class="hc-card">
            <div class="hc-overview-section-head">
              <div>
                <div class="hc-overview-section-kicker">{{ t('overviewFirstSuccessPath') || 'First success path' }}</div>
                <div class="hc-overview-section-title">{{ t('overviewFirstSuccessTitle') || 'Get from setup to a real result' }}</div>
              </div>
              <a class="hc-btn hc-btn-sm hc-btn-ghost" href="#/assistant">{{ t('overviewOpenAssistant') || 'Open assistant' }}</a>
            </div>
            <div class="hc-overview-step-list">
              <a v-for="step in firstSuccessSteps()" :key="step.key" class="hc-overview-step-card" :href="step.href">
                <div class="hc-overview-step-main">
                  <span class="hc-readiness-icon" :class="step.ok ? 'ok' : (step.warn ? 'warn' : 'todo')">{{ step.ok ? '✓' : (step.warn ? '!' : '•') }}</span>
                  <div>
                    <div class="hc-readiness-title">{{ step.title }}</div>
                    <div class="hc-readiness-desc">{{ step.desc }}</div>
                  </div>
                </div>
                <span class="hc-readiness-cta">{{ step.cta }}</span>
              </a>
            </div>
          </div>

          <div class="hc-card">
            <div class="hc-overview-section-head">
              <div>
                <div class="hc-overview-section-kicker">{{ t('overviewTrustSignal') || 'Trust signal' }}</div>
                <div class="hc-overview-section-title">{{ t('overviewMostRecentSuccess') || 'Most recent successful example' }}</div>
              </div>
              <a v-if="recentSuccess && recentSuccess.runHref" class="hc-btn hc-btn-sm hc-btn-ghost" :href="recentSuccess.runHref">{{ t('overviewOpenReceipt') || 'Open receipt' }}</a>
              <a v-else class="hc-btn hc-btn-sm hc-btn-ghost" href="#/assistant">{{ t('overviewCreateFirstSuccess') || 'Create first success' }}</a>
            </div>
            <div v-if="recentSuccess" class="hc-overview-proof-card">
              <div class="hc-overview-proof-head">
                <div>
                  <div class="hc-overview-workflow-title">{{ recentSuccess.title }}</div>
                  <div class="hc-readiness-desc">{{ recentSuccess.when }}</div>
                </div>
                <span class="hc-badge" :class="recentSuccess.badgeClass">{{ recentSuccess.badge }}</span>
              </div>
              <div v-if="recentSuccess.summary" class="hc-overview-proof-summary">{{ recentSuccess.summary }}</div>
              <div v-if="recentSuccess.artifacts.length" class="hc-result-chip-row">
                <span v-for="item in recentSuccess.artifacts.slice(0, 3)" :key="recentSuccess.runId + '-' + (item.uri || item.name)" class="hc-result-chip">{{ item.name || item.label || item.kind || 'artifact' }}</span>
              </div>
              <div v-if="recentSuccess.verification" class="hc-overview-proof-meta">
                {{ t('overviewVerification') || 'Verification' }}: {{ recentSuccess.verification }}
              </div>
              <div v-if="recentSuccess.blocks.length" class="hc-overview-proof-preview">
                {{ recentSuccess.blocks[0].content || '' }}
              </div>
            </div>
            <div v-else class="hc-state-block hc-state-block-inline">
              <div class="hc-state-block-title">{{ recentSuccessEmptyTitle() }}</div>
              <div class="hc-state-block-copy">{{ recentSuccessEmptyCopy() }}</div>
            </div>
          </div>
        </div>

        <div
          class="hc-card"
          data-testid="overview-quality-panel"
          :data-unavailable="qualityPanelUnavailable() ? 'true' : 'false'"
        >
          <div class="hc-overview-section-head">
            <div>
              <div class="hc-overview-section-kicker">{{ t('overviewQualitySignal') || 'Quality signal' }}</div>
              <div class="hc-overview-section-title">{{ t('overviewQualityAndEvals') || 'Quality and evals' }}</div>
            </div>
            <a class="hc-btn hc-btn-sm hc-btn-ghost" href="#/runs">{{ t('overviewOpenRuns') || 'Open runs' }}</a>
          </div>

          <div v-if="qualityPanelUnavailable()" class="hc-state-block hc-state-block-inline">
            <div class="hc-state-block-title">{{ t('overviewQualityUnavailableTitle') || 'Quality signals unavailable' }}</div>
            <div class="hc-state-block-copy">{{ t('overviewQualityUnavailableDesc') || 'The console could not load quality summary, release readiness, or eval suites yet.' }}</div>
          </div>

          <div v-else class="hc-settings-context-grid">
            <div class="hc-result-card" data-testid="overview-release-readiness-panel" :data-state="releaseReadinessState()">
              <div class="hc-result-card-header">
                <div class="hc-result-card-copy">
                  <div class="hc-settings-context-kicker">{{ t('overviewReleaseReadiness') || 'Release readiness' }}</div>
                  <div class="hc-result-card-title">{{ releaseReadinessPanelTitle() }}</div>
                </div>
                <span class="hc-badge" :class="releaseReadinessBadgeClass()">{{ releaseReadinessBadgeLabel() }}</span>
              </div>
              <div class="hc-result-card-summary">{{ releaseReadinessPanelCopy() }}</div>

              <div v-if="qualitySignalWarnings().length" class="hc-result-chip-row">
                <span v-for="warning in qualitySignalWarnings()" :key="warning" class="hc-result-chip">{{ warning }}</span>
              </div>

              <div v-if="qualityMetricCards().length" class="hc-run-detail-grid">
                <div v-for="metric in qualityMetricCards()" :key="metric.key">
                  <span class="hc-run-detail-label">{{ metric.label }}</span>
                  <strong>{{ metric.value }}</strong>
                  <span class="hc-readiness-desc">{{ metric.detail }}</span>
                </div>
              </div>

              <div v-if="releaseReadinessChecks().length" class="hc-result-deliverable-grid">
                <div v-for="check in releaseReadinessChecks()" :key="check.id" class="hc-result-deliverable-card">
                  <div class="hc-result-deliverable-head">
                    <strong>{{ releaseReadinessCheckTitle(check) }}</strong>
                    <span class="hc-badge" :class="releaseReadinessCheckBadgeClass(check)">{{ releaseReadinessCheckStatus(check) }}</span>
                  </div>
                  <div class="hc-result-panel-summary">{{ check.summary || '-' }}</div>
                  <div class="hc-result-chip-row">
                    <span v-if="check.total || check.count" class="hc-result-chip">{{ releaseReadinessCheckCount(check) }}</span>
                    <span v-if="releaseReadinessCheckThreshold(check)" class="hc-result-chip">{{ releaseReadinessCheckThreshold(check) }}</span>
                    <span v-if="releaseReadinessCheckMeasured(check)" class="hc-result-chip">{{ releaseReadinessCheckMeasured(check) }}</span>
                  </div>
                </div>
              </div>
            </div>

            <div class="hc-result-panel">
              <div class="hc-result-panel-header">
                <div class="hc-result-panel-copy">
                  <div class="hc-settings-context-kicker">{{ t('overviewEvalSuites') || 'Eval suites' }}</div>
                  <div class="hc-result-panel-title">{{ t('overviewQualityAndEvals') || 'Quality and evals' }}</div>
                </div>
              </div>

              <div v-if="evalSuitesUnavailable" class="hc-state-block hc-state-block-inline">
                <div class="hc-state-block-title">{{ t('overviewEvalSuitesUnavailable') || 'Eval suites unavailable' }}</div>
                <div class="hc-state-block-copy">{{ t('overviewQualityUnavailableDesc') || 'The console could not load quality summary, release readiness, or eval suites yet.' }}</div>
              </div>

              <div v-else-if="evalSuites.length === 0" class="hc-state-block hc-state-block-inline">
                <div class="hc-state-block-title">{{ t('overviewNoEvalSuites') || 'No eval suites published yet' }}</div>
                <div class="hc-state-block-copy">{{ t('overviewNoEvalRunYet') || 'No eval suite has been run from the console yet.' }}</div>
              </div>

              <div v-else class="hc-result-deliverable-grid">
                <div v-for="suite in evalSuites" :key="suite.id || suite.name" class="hc-result-deliverable-card">
                  <div class="hc-result-deliverable-head">
                    <strong>{{ evalSuiteName(suite) }}</strong>
                    <span class="hc-badge hc-badge-gray">{{ evalSuiteCaseCount(suite) }} {{ t('overviewSuiteCases') || 'cases' }}</span>
                  </div>
                  <div class="hc-result-panel-summary">{{ evalSuiteDescription(suite) }}</div>
                  <div class="hc-result-chip-row">
                    <span class="hc-result-chip">{{ evalSuiteSurfaceLabel(suite) }}</span>
                    <span
                      v-for="item in evalSuitePrerequisites(suite)"
                      :key="(suite.id || suite.name) + '-' + item"
                      class="hc-result-chip"
                    >
                      {{ item }}
                    </span>
                  </div>
                  <div class="hc-result-card-actions">
                    <button
                      class="hc-btn hc-btn-sm hc-btn-primary"
                      :data-testid="evalSuiteRunTestID(suite)"
                      @click="runEvalSuite(suite)"
                      :disabled="evalRunLoading"
                    >
                      {{ evalRunButtonLabel(suite) }}
                    </button>
                  </div>
                </div>
              </div>

              <div v-if="evalRunLoading" class="hc-result-card-note">
                {{ t('overviewRunningSuite') || 'Running...' }}
              </div>

              <div v-if="evalRunError" class="hc-run-detail-error">{{ evalRunError }}</div>

              <div v-if="evalRunReport" class="hc-result-card" data-testid="overview-eval-run-report" :data-state="evalRunReportStatus(evalRunReport)">
                <div class="hc-result-card-header">
                  <div class="hc-result-card-copy">
                    <div class="hc-settings-context-kicker">{{ t('overviewLatestEvalRun') || 'Latest eval run' }}</div>
                    <div class="hc-result-card-title">{{ evalRunReportTitle(evalRunReport) }}</div>
                  </div>
                  <span class="hc-badge" :class="evalRunReportBadgeClass(evalRunReport)">{{ evalRunReportStatusLabel(evalRunReport) }}</span>
                </div>
                <div class="hc-result-card-summary">{{ evalRunReportSummary(evalRunReport) }}</div>
                <div class="hc-result-chip-row">
                  <span class="hc-result-chip">{{ evalRunReportCountLabel(evalRunReport) }}</span>
                  <span class="hc-result-chip">{{ evalRunReportDurationLabel(evalRunReport) }}</span>
                </div>
                <div v-if="evalRunQualityMetricCards(evalRunReport).length" class="hc-run-detail-grid">
                  <div v-for="metric in evalRunQualityMetricCards(evalRunReport)" :key="'eval-' + metric.key">
                    <span class="hc-run-detail-label">{{ metric.label }}</span>
                    <strong>{{ metric.value }}</strong>
                    <span class="hc-readiness-desc">{{ metric.detail }}</span>
                  </div>
                </div>
                <div v-if="evalRunCases(evalRunReport).length" class="hc-result-deliverable-grid">
                  <div v-for="item in evalRunCases(evalRunReport)" :key="item.id || item.name" class="hc-result-deliverable-card">
                    <div class="hc-result-deliverable-head">
                      <strong>{{ item.name || item.id || 'case' }}</strong>
                      <span class="hc-badge" :class="evalRunCaseBadgeClass(item)">{{ evalRunCaseStatus(item) }}</span>
                    </div>
                    <div class="hc-result-panel-summary">{{ evalRunCaseSummary(item) }}</div>
                    <div class="hc-result-chip-row">
                      <span v-if="item.run_id" class="hc-result-chip">run {{ item.run_id }}</span>
                      <span v-if="item.verification_status" class="hc-result-chip">{{ formatStatusText(item.verification_status) }}</span>
                      <span v-if="item.expected_surface" class="hc-result-chip">{{ item.expected_surface }}</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div class="hc-card">
          <div class="hc-overview-section-head">
            <div>
              <div class="hc-overview-section-kicker">{{ t('overviewStartWithTemplates') || 'Start with templates' }}</div>
              <div class="hc-overview-section-title">{{ t('overviewRecommendedStarters') || 'Recommended starter workflows' }}</div>
            </div>
            <a class="hc-btn hc-btn-sm hc-btn-ghost" href="#/automation/schedules">{{ t('overviewOpenAutomation') || 'Open automation' }}</a>
          </div>
          <div class="hc-overview-workflow-grid">
            <a v-for="workflow in starterWorkflows()" :key="workflow.title" class="hc-overview-workflow-card" :href="workflow.href">
              <div class="hc-overview-workflow-head">
                <div class="hc-overview-workflow-title">{{ workflow.title }}</div>
                <span class="hc-badge" :class="workflow.badgeClass">{{ workflow.badge }}</span>
              </div>
              <div class="hc-readiness-desc">{{ workflow.desc }}</div>
            </a>
          </div>
        </div>
      </div>
    </div>
  `;

  return {
    $template,
    t,

    statusData: null,
    setupStatus: null,
    modules: [],
    capabilities: [],
    channelHealth: [],
    pendingApprovals: 0,
    recentRunsTotal: 0,
    failedRuns: 0,
    loading: true,
    error: false,
    warnings: [],
    degradedModules: 0,
    degradedCaps: 0,
    connectedChannels: 0,
    warningChannels: 0,
    recentSuccess: null,
    qualitySummary: null,
    releaseReadiness: null,
    evalSuites: [],
    evalRunLoading: false,
    evalRunSuiteID: '',
    evalRunReport: null,
    evalRunError: '',
    statusUnavailable: false,
    setupUnavailable: false,
    modulesUnavailable: false,
    capabilitiesUnavailable: false,
    channelHealthUnavailable: false,
    approvalsUnavailable: false,
    runsUnavailable: false,
    recentSuccessUnavailable: false,
    qualitySummaryUnavailable: false,
    releaseReadinessUnavailable: false,
    evalSuitesUnavailable: false,

    fmtTime,
    healthDot,
    formatStatusText,

    get statusState() {
      return operatorStatusState(this.statusData);
    },

    statusDotClass() {
      return operatorStatusDotClass(this.statusData, this.statusUnavailable);
    },

    statusLabel() {
      return operatorStatusLabel(this.statusData, this.statusUnavailable, t);
    },

    statusStatusSubline() {
      if (!this.statusData) return '';
      const meta = [];
      if (this.statusData.version) meta.push('v' + this.statusData.version);
      if (this.statusData.uptime) meta.push(this.statusData.uptime);
      const summary = operatorStatusSummary(this.statusData);
      if (this.statusState === 'ready' || !summary) return meta.join(' · ');
      return [summary].concat(meta).filter(Boolean).join(' · ');
    },

    capName(capability) {
      const manifest = capability.manifest || capability.Manifest || {};
      return capability.name || capability.Name || manifest.name || manifest.Name || '?';
    },

    capHealthStr(capability) {
      return capHealth(capability.health || capability.Health) || 'unknown';
    },

    moduleKey(module) {
      return module.id || module.ID || this.moduleName(module);
    },

    moduleName(module) {
      return module.name || module.Name || module.id || module.ID || '?';
    },

    moduleHealthStr(module) {
      return moduleHealth(module.health || module.Health) || 'unknown';
    },

    moduleSource(module) {
      return String(module.source || module.Source || 'builtin').trim().toLowerCase() || 'builtin';
    },

    moduleDelivery(module) {
      return String(module.delivery || module.Delivery || '').trim().toLowerCase();
    },

    moduleVersion(module) {
      return String(module.version || module.Version || '').trim();
    },

    moduleMetricValue() {
      return this.modulesUnavailable ? '-' : String(this.modules.length);
    },

    moduleSummary() {
      if (this.modulesUnavailable) return t('overviewCapPackSnapshotUnavailable') || 'Capability pack snapshot unavailable';
      if (this.modules.length === 0) return t('overviewNoCapPacksReported') || 'No capability packs reported';
      const counts = this.modules.reduce((acc, item) => {
        const key = this.moduleSource(item);
        acc[key] = (acc[key] || 0) + 1;
        return acc;
      }, {});
      const parts = Object.keys(counts)
        .sort()
        .map(key => counts[key] + ' ' + key);
      if (this.degradedModules > 0) parts.push(this.degradedModules + ' ' + (t('overviewNeedAttention') || 'need attention'));
      return parts.join(' · ');
    },

    moduleHighlights() {
      return this.modules
        .slice()
        .sort((left, right) => {
          const leftHealth = this.moduleHealthStr(left);
          const rightHealth = this.moduleHealthStr(right);
          const leftScore = (!HEALTHY_MODULE_STATES.includes(leftHealth) && leftHealth !== 'unknown' ? 4 : 0) +
            (this.moduleSource(left) !== 'builtin' ? 2 : 0);
          const rightScore = (!HEALTHY_MODULE_STATES.includes(rightHealth) && rightHealth !== 'unknown' ? 4 : 0) +
            (this.moduleSource(right) !== 'builtin' ? 2 : 0);
          if (leftScore !== rightScore) return rightScore - leftScore;
          return this.moduleName(left).localeCompare(this.moduleName(right));
        })
        .slice(0, 6);
    },

    capabilityReady(name) {
      return this.capabilities.some(capability => {
        const capNameValue = this.capName(capability).toLowerCase();
        return (capNameValue === name || capNameValue.indexOf(name + '.') === 0) &&
          HEALTHY_CAPABILITY_STATES.includes(this.capHealthStr(capability));
      });
    },

    configuredProviderNames() {
      return uniqueNames((this.setupStatus && this.setupStatus.providers) || []);
    },

    detectedProviderNames() {
      const items = (this.setupStatus && this.setupStatus.detected_providers) || [];
      return uniqueNames(items.map(item => typeof item === 'string' ? item : (item.name || item.id || '')));
    },

    readinessTotal() {
      return 4;
    },

    setupReadinessValue() {
      if (this.setupUnavailable) return '?/' + this.readinessTotal();
      return this.readinessCompleted() + '/' + this.readinessTotal();
    },

    setupReadinessSummary() {
      if (this.setupUnavailable) return t('overviewSetupReadinessUnavailable') || 'Setup readiness unavailable';
      return this.configuredProviderNames().join(', ') || (t('overviewNoProviderConfigured') || 'No provider configured');
    },

    qualityPanelUnavailable() {
      return this.qualitySummaryUnavailable && this.releaseReadinessUnavailable && this.evalSuitesUnavailable;
    },

    releaseReadinessState() {
      if (this.releaseReadinessUnavailable) return 'unavailable';
      return this.releaseReadiness && this.releaseReadiness.ready ? 'ready' : 'blocked';
    },

    releaseReadinessValue() {
      if (this.releaseReadinessUnavailable) return t('overviewUnavailable') || 'Unavailable';
      return this.releaseReadiness && this.releaseReadiness.ready
        ? (t('overviewReadyLabel') || 'Ready')
        : (t('overviewBlockedLabel') || 'Blocked');
    },

    releaseReadinessBadgeLabel() {
      return this.releaseReadinessValue();
    },

    releaseReadinessBadgeClass() {
      const state = this.releaseReadinessState();
      if (state === 'ready') return 'hc-badge-green';
      if (state === 'blocked') return 'hc-badge-orange';
      return 'hc-badge-gray';
    },

    releaseReadinessPassedCount() {
      const checks = (this.releaseReadiness && this.releaseReadiness.checks) || [];
      return checks.filter(check => String(check.status || '').toLowerCase() === 'passed').length;
    },

    releaseReadinessCheckCountTotal() {
      return ((this.releaseReadiness && this.releaseReadiness.checks) || []).length;
    },

    releaseReadinessBlockerCount() {
      return ((this.releaseReadiness && this.releaseReadiness.blockers) || []).length;
    },

    releaseReadinessSummary() {
      if (this.releaseReadinessUnavailable) return t('overviewReleaseReadinessUnavailable') || 'Release readiness unavailable';
      const total = this.releaseReadinessCheckCountTotal();
      const passed = this.releaseReadinessPassedCount();
      const blockers = this.releaseReadinessBlockerCount();
      if (blockers > 0) {
        return blockers + ' ' + (t('overviewBlockers') || 'blockers') + ' · ' + passed + '/' + total + ' ' + (t('overviewChecksPassing') || 'checks passing');
      }
      return passed + '/' + total + ' ' + (t('overviewChecksPassing') || 'checks passing');
    },

    releaseReadinessPanelTitle() {
      if (this.releaseReadinessUnavailable) return t('overviewReleaseReadinessUnavailable') || 'Release readiness unavailable';
      const total = this.releaseReadinessCheckCountTotal();
      const blockers = this.releaseReadinessBlockerCount();
      if (this.releaseReadiness && this.releaseReadiness.ready) {
        return this.releaseReadinessValue() + ' · ' + total + '/' + total;
      }
      return this.releaseReadinessValue() + ' · ' + blockers + ' ' + (t('overviewBlockers') || 'blockers');
    },

    releaseReadinessPanelCopy() {
      if (this.releaseReadinessUnavailable) return t('overviewReleaseReadinessUnavailable') || 'Release readiness unavailable';
      const report = this.releaseReadiness || {};
      if (report.ready) {
        return this.releaseReadinessPassedCount() + '/' + this.releaseReadinessCheckCountTotal() + ' ' + (t('overviewChecksPassing') || 'checks passing');
      }
      const blocker = ((report.blockers || [])[0] || {}).summary;
      return blocker || this.releaseReadinessSummary();
    },

    qualitySignalWarnings() {
      const items = [];
      if (this.qualitySummaryUnavailable) items.push(t('overviewQualitySummaryUnavailable') || 'Quality summary unavailable');
      if (this.releaseReadinessUnavailable) items.push(t('overviewReleaseReadinessUnavailable') || 'Release readiness unavailable');
      if (this.evalSuitesUnavailable) items.push(t('overviewEvalSuitesUnavailable') || 'Eval suites unavailable');
      return items;
    },

    qualityMetricCards() {
      if (!this.qualitySummary || this.qualitySummaryUnavailable) return [];
      return [
        {
          key: 'terminal_runs',
          label: t('overviewTerminalRuns') || 'Terminal runs',
          value: String(this.qualitySummary.terminal_run_count || 0),
          detail: String(this.qualitySummary.run_count || 0) + ' ' + (t('overviewRecentRuns') || 'Recent runs'),
        },
        {
          key: 'task_success',
          label: t('overviewTaskSuccessRate') || 'Task success rate',
          value: formatPercent(this.qualitySummary.task_success && this.qualitySummary.task_success.rate, 0),
          detail: this.rateDetail(this.qualitySummary.task_success),
        },
        {
          key: 'false_success',
          label: t('overviewFalseSuccessRate') || 'False-success rate',
          value: formatPercent(this.qualitySummary.false_success && this.qualitySummary.false_success.rate, 0),
          detail: this.rateDetail(this.qualitySummary.false_success),
        },
        {
          key: 'fallback',
          label: t('overviewFallbackRate') || 'Fallback rate',
          value: formatPercent(this.qualitySummary.fallback && this.qualitySummary.fallback.rate, 0),
          detail: this.rateDetail(this.qualitySummary.fallback),
        },
        {
          key: 'profile_hit',
          label: t('overviewProfileHitRate') || 'Profile hit rate',
          value: formatPercent(this.qualitySummary.profile_hit && this.qualitySummary.profile_hit.rate, 0),
          detail: this.rateDetail(this.qualitySummary.profile_hit),
        },
      ];
    },

    rateDetail(rate) {
      if (!rate || typeof rate !== 'object') return '0/0';
      return String(rate.count || 0) + '/' + String(rate.total || 0);
    },

    releaseReadinessChecks() {
      if (this.releaseReadinessUnavailable || !this.releaseReadiness) return [];
      const blockers = Array.isArray(this.releaseReadiness.blockers) ? this.releaseReadiness.blockers : [];
      if (blockers.length > 0) return blockers;
      return Array.isArray(this.releaseReadiness.checks) ? this.releaseReadiness.checks.slice(0, 4) : [];
    },

    releaseReadinessCheckTitle(check) {
      return formatStatusText(check && check.id ? check.id : 'check');
    },

    releaseReadinessCheckStatus(check) {
      return formatStatusText(check && check.status ? check.status : 'unknown');
    },

    releaseReadinessCheckBadgeClass(check) {
      const status = String((check && check.status) || '').toLowerCase();
      if (status === 'passed') return 'hc-badge-green';
      if (status === 'blocked') return 'hc-badge-orange';
      return 'hc-badge-gray';
    },

    releaseReadinessCheckCount(check) {
      if (!check || (!check.total && !check.count)) return '';
      return String(check.count || 0) + '/' + String(check.total || 0);
    },

    releaseReadinessCheckThreshold(check) {
      if (!check || !check.comparator || typeof check.threshold !== 'number') return '';
      return (t('overviewCheckThreshold') || 'Threshold') + ' ' + check.comparator + ' ' + formatPercent(check.threshold, 1);
    },

    releaseReadinessCheckMeasured(check) {
      if (!check || typeof check.measured !== 'number' || !check.total) return '';
      return (t('overviewCheckMeasured') || 'Measured') + ' ' + formatPercent(check.measured, 1);
    },

    evalSuiteName(suite) {
      return String((suite && (suite.name || suite.id)) || 'suite').trim();
    },

    evalSuiteDescription(suite) {
      return String((suite && (suite.description || suite.surface || suite.id)) || '').trim();
    },

    evalSuiteCaseCount(suite) {
      return Array.isArray(suite && suite.cases) ? suite.cases.length : 0;
    },

    evalSuiteSurfaceLabel(suite) {
      const surface = String((suite && suite.surface) || '').trim();
      if (!surface) return t('overviewNoPrerequisites') || 'No extra prerequisites';
      return formatStatusText(surface);
    },

    evalSuitePrerequisites(suite) {
      const items = Array.isArray(suite && suite.prerequisites) ? suite.prerequisites.filter(Boolean) : [];
      if (items.length > 0) return items;
      return [t('overviewNoPrerequisites') || 'No extra prerequisites'];
    },

    evalSuiteRunTestID(suite) {
      const raw = String((suite && (suite.id || suite.name)) || 'suite').trim().toLowerCase();
      const slug = raw.replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'suite';
      return 'overview-eval-run-' + slug;
    },

    evalRunButtonLabel(suite) {
      if (this.evalRunLoading && this.evalRunSuiteID === String((suite && suite.id) || '')) {
        return t('overviewRunningSuite') || 'Running...';
      }
      return t('overviewRunSuite') || 'Run suite';
    },

    evalRunReportStatus(report) {
      return String((report && report.status) || 'unknown').toLowerCase();
    },

    evalRunReportStatusLabel(report) {
      return formatStatusText(this.evalRunReportStatus(report));
    },

    evalRunReportBadgeClass(report) {
      return this.evalRunReportStatus(report) === 'passed' ? 'hc-badge-green' : 'hc-badge-orange';
    },

    evalRunReportTitle(report) {
      const suite = report && report.suite ? report.suite : {};
      return this.evalSuiteName(suite);
    },

    evalRunReportSummary(report) {
      if (!report) return t('overviewNoEvalRunYet') || 'No eval suite has been run from the console yet.';
      return String(report.passed || 0) + ' passed · ' + String(report.failed || 0) + ' failed · ' + String(report.errored || 0) + ' errored';
    },

    evalRunReportCountLabel(report) {
      return String((report && report.case_count) || 0) + ' ' + (t('overviewSuiteCases') || 'cases');
    },

    evalRunReportDurationLabel(report) {
      const duration = Number((report && report.duration_ms) || 0);
      return (t('overviewDuration') || 'Duration') + ' ' + (duration > 0 ? Math.round(duration / 100) / 10 + 's' : '-');
    },

    evalRunQualityMetricCards(report) {
      if (!report || !report.quality) return [];
      const quality = report.quality;
      return [
        {
          key: 'task_success',
          label: t('overviewTaskSuccessRate') || 'Task success rate',
          value: formatPercent(quality.task_success && quality.task_success.rate, 0),
          detail: this.rateDetail(quality.task_success),
        },
        {
          key: 'fallback',
          label: t('overviewFallbackRate') || 'Fallback rate',
          value: formatPercent(quality.fallback && quality.fallback.rate, 0),
          detail: this.rateDetail(quality.fallback),
        },
        {
          key: 'profile_hit',
          label: t('overviewProfileHitRate') || 'Profile hit rate',
          value: formatPercent(quality.profile_hit && quality.profile_hit.rate, 0),
          detail: this.rateDetail(quality.profile_hit),
        },
      ];
    },

    evalRunCases(report) {
      return Array.isArray(report && report.cases) ? report.cases.slice(0, 6) : [];
    },

    evalRunCaseStatus(item) {
      return formatStatusText(item && item.status ? item.status : 'unknown');
    },

    evalRunCaseBadgeClass(item) {
      const status = String((item && item.status) || '').toLowerCase();
      if (status === 'passed') return 'hc-badge-green';
      if (status === 'error') return 'hc-badge-red';
      return 'hc-badge-orange';
    },

    evalRunCaseSummary(item) {
      if (!item) return '-';
      return String(item.error || item.verification_summary || item.prompt || '').trim() || '-';
    },

    applyQualitySignals(qualitySummary, releaseReadiness, evalSuitesData) {
      this.qualitySummary = qualitySummary;
      this.qualitySummaryUnavailable = !qualitySummary;
      this.releaseReadiness = releaseReadiness;
      this.releaseReadinessUnavailable = !releaseReadiness;
      this.evalSuites = normalizeItems(evalSuitesData);
      this.evalSuitesUnavailable = this.evalSuites.length === 0 && !evalSuitesData;
    },

    async refreshQualitySignals() {
      const [qualitySummaryResult, releaseReadinessResult, evalSuitesResult] = await Promise.allSettled([
        api.get('/operator/quality/summary', { background: true }),
        api.get('/operator/quality/release-readiness', { background: true }),
        api.get('/operator/evals/suites', { background: true }),
      ]);
      if (!_mounted) return;
      this.applyQualitySignals(
        settledValue(qualitySummaryResult),
        settledValue(releaseReadinessResult),
        settledValue(evalSuitesResult)
      );
    },

    async runEvalSuite(suite) {
      const suiteID = String((suite && suite.id) || '').trim();
      if (!suiteID || this.evalRunLoading) return;
      this.evalRunLoading = true;
      this.evalRunSuiteID = suiteID;
      this.evalRunError = '';
      try {
        this.evalRunReport = await api.post('/operator/evals/run', { suite_id: suiteID });
        await this.refreshQualitySignals();
      } catch (err) {
        this.evalRunError = (err && err.message) || (t('overviewEvalRunFailed') || 'Eval run failed');
      }
      this.evalRunLoading = false;
      this.evalRunSuiteID = '';
    },

    readinessCompleted() {
      const checks = this.readinessChecklist().filter(item => item.ok);
      return checks.length;
    },

    helperReadyCount() {
      let total = 0;
      if (this.capabilityReady('browser')) total++;
      if (this.capabilityReady('desktop')) total++;
      return total;
    },

    helperReadyValue() {
      return this.capabilitiesUnavailable ? '?/2' : this.helperReadyCount() + '/2';
    },

    helperSummary() {
      if (this.capabilitiesUnavailable) return t('overviewCapSnapshotUnavailableSummary') || 'Capability snapshot unavailable';
      const parts = [];
      parts.push(this.capabilityReady('browser') ? (t('overviewBrowserReady') || 'Browser ready') : (t('overviewBrowserPending') || 'Browser pending'));
      parts.push(this.capabilityReady('desktop') ? (t('overviewDesktopReady') || 'Desktop ready') : (t('overviewDesktopPending') || 'Desktop pending'));
      return parts.join(' · ');
    },

    channelSummary() {
      if (this.channelHealthUnavailable) return t('overviewChannelUnavailableSummary') || 'Channel health unavailable';
      if (this.channelHealth.length === 0) return t('overviewNoActiveChannelsSummary') || 'No active channels';
      if (this.warningChannels > 0) return this.warningChannels + ' ' + (t('overviewNeedReview') || 'need review');
      return this.connectedChannels + ' ' + (t('overviewConnectedSuffix') || 'connected');
    },

    channelMetricValue() {
      return this.channelHealthUnavailable ? '-' : String(this.connectedChannels);
    },

    recentRunsValue() {
      return this.runsUnavailable ? '-' : String(this.recentRunsTotal);
    },

    channelHealthDot(state) {
      const lowered = String(state || '').toLowerCase();
      if (CONNECTED_CHANNEL_STATES.includes(lowered)) return 'ok';
      if (WARNING_CHANNEL_STATES.includes(lowered)) return 'warn';
      return 'err';
    },

    overviewHeadline() {
      if (this.statusUnavailable || this.setupUnavailable || this.capabilitiesUnavailable || this.channelHealthUnavailable || this.runsUnavailable) {
        return t('overviewHeadlineUnavailable') || 'Some runtime signals are unavailable. Fix backend reachability first so this page reflects the real system.';
      }
      const configured = this.configuredProviderNames();
      if (!configured.length) {
        const detected = this.detectedProviderNames();
        if (detected.length) {
          return t('overviewHeadlineDetected') || 'API keys are already detectable from the environment. Finish model setup and you can run your first task immediately.';
        }
        return t('overviewHeadlineNoProvider') || 'Finish models, helpers, and channels here so the console feels trustworthy before you ask HopClaw to act.';
      }
      if (this.failedRuns > 0 || this.warningChannels > 0 || this.degradedCaps > 0) {
        return t('overviewHeadlineRoughEdges') || 'Core runtime is up, but there are a few rough edges worth fixing before you rely on production tasks.';
      }
      return t('overviewHeadlineReady') || 'Runtime, helpers, and channels are in good shape. This console is ready to drive real work.';
    },

    backendIssueActive() {
      return this.statusUnavailable || this.error;
    },

    fatalBackendIssue() {
      return this.statusUnavailable && this.setupUnavailable && this.capabilitiesUnavailable && this.channelHealthUnavailable && this.runsUnavailable;
    },

    readinessChecklist() {
      const providers = this.configuredProviderNames();
      const detected = this.detectedProviderNames();
      return [
        {
          key: 'models',
          title: t('overviewConfigureProvider') || 'Configure at least one model provider',
          desc: this.setupUnavailable
            ? (t('overviewSetupUnavailable') || 'Setup status is unavailable right now')
            : (providers.length ? ((t('overviewReadyProviders') || 'Ready') + ': ' + providers.join(', ')) : (detected.length ? ((t('overviewDetectedKeys') || 'Detected env keys') + ': ' + detected.join(', ')) : (t('overviewNoProviderDetected') || 'No provider detected yet'))),
          href: providers.length ? '#/settings/models' : '#/setup',
          cta: providers.length ? (t('overviewReview') || 'Review') : (t('overviewConfigure') || 'Configure'),
          ok: !this.setupUnavailable && providers.length > 0,
          warn: this.setupUnavailable || (!providers.length && detected.length > 0),
        },
        {
          key: 'browser',
          title: t('overviewVerifyBrowser') || 'Verify browser automation',
          desc: this.capabilitiesUnavailable
            ? (t('overviewCapUnavailable') || 'Capability snapshot is unavailable right now')
            : (this.capabilityReady('browser') ? (t('overviewBrowserHealthy') || 'Browser capability reports healthy') : (t('overviewBrowserNotHealthy') || 'Browser helper is not healthy yet')),
          href: '#/settings/infrastructure',
          cta: this.capabilityReady('browser') ? (t('overviewInspect') || 'Inspect') : (t('overviewFix') || 'Fix'),
          ok: !this.capabilitiesUnavailable && this.capabilityReady('browser'),
          warn: this.capabilitiesUnavailable,
        },
        {
          key: 'desktop',
          title: t('overviewVerifyDesktop') || 'Verify desktop control',
          desc: this.capabilitiesUnavailable
            ? (t('overviewCapUnavailable') || 'Capability snapshot is unavailable right now')
            : (this.capabilityReady('desktop') ? (t('overviewDesktopHealthy') || 'Desktop capability reports healthy') : (t('overviewDesktopNotReady') || 'Desktop operator session is not ready yet')),
          href: '#/settings/infrastructure',
          cta: this.capabilityReady('desktop') ? (t('overviewInspect') || 'Inspect') : (t('overviewFix') || 'Fix'),
          ok: !this.capabilitiesUnavailable && this.capabilityReady('desktop'),
          warn: this.capabilitiesUnavailable,
        },
        {
          key: 'channels',
          title: t('overviewConnectChannel') || 'Connect your primary work channel',
          desc: this.channelHealthUnavailable
            ? (t('overviewChannelHealthUnavailable') || 'Channel health is unavailable right now')
            : (this.connectedChannels > 0 ? (this.connectedChannels + ' ' + (t('overviewConnectedChannels') || 'connected channel(s)')) : (t('overviewNoChannelConnected') || 'No channel is actively connected')),
          href: '#/settings/channels',
          cta: this.connectedChannels > 0 ? (t('overviewManage') || 'Manage') : (t('overviewConnect') || 'Connect'),
          ok: !this.channelHealthUnavailable && this.connectedChannels > 0,
          warn: this.channelHealthUnavailable || this.warningChannels > 0,
        },
      ];
    },

    firstSuccessSteps() {
      const providers = this.configuredProviderNames();
      const haveProvider = providers.length > 0;
      const haveAutomationSurface = !this.runsUnavailable && this.recentRunsTotal > 0;
      return [
        {
          key: 'provider',
          title: t('overviewConnectModel') || 'Connect a model',
          desc: haveProvider ? ((t('overviewReadyProviders') || 'Ready') + ': ' + providers.join(', ')) : (t('overviewPickProvider') || 'Pick a provider and validate one working model first.'),
          href: haveProvider ? '#/settings/models' : '#/setup',
          cta: haveProvider ? (t('overviewReview') || 'Review') : (t('overviewConfigure') || 'Configure'),
          ok: haveProvider,
          warn: false,
        },
        {
          key: 'template',
          title: t('overviewChooseTemplate') || 'Choose a workflow template',
          desc: t('overviewChooseTemplateDesc') || 'Start from cron, watch, wakeup, or hook templates instead of building everything from scratch.',
          href: '#/automation/schedules',
          cta: t('overviewBrowse') || 'Browse',
          ok: false,
          warn: false,
        },
        {
          key: 'run',
          title: t('overviewRunOneTask') || 'Run one real task',
          desc: this.runsUnavailable
            ? (t('overviewRunHistoryUnavailableNow') || 'Recent run history is unavailable right now.')
            : (haveAutomationSurface ? ((t('overviewExecutionEvidence') || 'Recent execution evidence found in') + ' ' + this.recentRunsTotal + ' ' + (t('overviewRunsSuffix') || 'run(s).')) : (t('overviewUseAssistant') || 'Use the assistant to execute one real task and produce a receipt.')),
          href: '#/assistant',
          cta: haveAutomationSurface ? (t('overviewRunAgain') || 'Run again') : (t('overviewStart') || 'Start'),
          ok: haveAutomationSurface,
          warn: this.runsUnavailable,
        },
        {
          key: 'receipt',
          title: t('overviewReviewReceipt') || 'Review the receipt and outputs',
          desc: this.runsUnavailable
            ? (t('overviewRunEvidenceUnavailable') || 'Recent run evidence is unavailable right now.')
            : (this.failedRuns > 0 ? (this.failedRuns + ' ' + (t('overviewRunsNeedAttention') || 'recent run(s) need attention.')) : (t('overviewUseRunsInspect') || 'Use Runs to inspect artifacts, verification, and next actions.')),
          href: '#/runs',
          cta: this.failedRuns > 0 ? (t('overviewFixIssues') || 'Fix issues') : (t('overviewInspect') || 'Inspect'),
          ok: !this.runsUnavailable && haveAutomationSurface && this.failedRuns === 0,
          warn: this.runsUnavailable || this.failedRuns > 0,
        },
      ];
    },

    recentSuccessEmptyTitle() {
      return this.runsUnavailable ? (t('overviewRunHistoryUnavailableEmpty') || 'Recent run history unavailable') : (t('overviewNoSuccessReceipt') || 'No successful receipt yet');
    },

    recentSuccessEmptyCopy() {
      if (this.runsUnavailable) {
        return t('overviewCannotLoadRuns') || 'The console could not load recent runs, so it cannot show a trustworthy success example yet.';
      }
      if (this.recentSuccessUnavailable) {
        return t('overviewCompletionUnavailable') || 'Recent runs loaded, but the latest completion receipt could not be retrieved from the backend.';
      }
      return t('overviewFinishOneTask') || 'Finish one real task and the latest validated result will show up here automatically.';
    },

    starterWorkflows() {
      return [
        {
          title: t('overviewWorkflowDailyReport') || 'Daily report briefing',
          desc: t('overviewWorkflowDailyReportDesc') || 'Generate a concise work summary on a fixed schedule and reuse it across your team.',
          href: '#/automation/schedules',
          badge: t('overviewWorkflowCron') || 'Cron',
          badgeClass: 'hc-badge-blue',
        },
        {
          title: t('overviewWorkflowMarketWatch') || 'Market signal watch',
          desc: t('overviewWorkflowMarketWatchDesc') || 'Monitor a page or feed, surface real changes, and produce a short operator summary.',
          href: '#/automation/watch',
          badge: t('overviewWorkflowWatch') || 'Watch',
          badgeClass: 'hc-badge-green',
        },
        {
          title: t('overviewWorkflowCustomerReply') || 'Customer reply draft',
          desc: t('overviewWorkflowCustomerReplyDesc') || 'Watch a structured inbox and draft approval-friendly customer replies.',
          href: '#/automation/watch',
          badge: t('overviewWorkflowInbox') || 'Inbox',
          badgeClass: 'hc-badge-green',
        },
        {
          title: t('overviewWorkflowRunHook') || 'Run completion webhook',
          desc: t('overviewWorkflowRunHookDesc') || 'Push finished run events into downstream systems, dashboards, or internal automation.',
          href: '#/automation/hooks',
          badge: t('overviewWorkflowHook') || 'Hook',
          badgeClass: 'hc-badge-orange',
        },
      ];
    },

    recentSuccessBadgeClass(outcome) {
      const value = String(outcome || '').toLowerCase();
      if (value === 'completed') return 'hc-badge-green';
      if (value === 'partial' || value === 'completed_warning') return 'hc-badge-orange';
      return 'hc-badge-gray';
    },

    buildRecentSuccess(run, completion) {
      if (!run || !completion) return null;
      const summary = overviewCompletionSummary(completion);
      const blocks = overviewCompletionBlocks(completion);
      const artifacts = overviewCompletionArtifacts(completion);
      const verification = String((completion.verification && completion.verification.summary) || '').trim();
      const outcome = overviewRunStatus(run);
      if (!summary && blocks.length === 0 && artifacts.length === 0) return null;
      return {
        runId: run.id || run.ID || '',
        runHref: '#/runs/' + encodeURIComponent(run.id || run.ID || ''),
        title: String(run.session_key || run.session_id || summary.slice(0, 72) || run.id || 'Recent run').trim(),
        when: fmtTime(run.updated_at || run.ended_at || run.created_at),
        badge: formatStatusText(outcome || 'completed'),
        badgeClass: this.recentSuccessBadgeClass(outcome),
        summary,
        verification,
        artifacts,
        blocks,
      };
    },

    async loadAll() {
      this.loading = true;
      this.error = false;
      try {
        const qualitySignalsPromise = Promise.allSettled([
          api.get('/operator/quality/summary', { background: true }),
          api.get('/operator/quality/release-readiness', { background: true }),
          api.get('/operator/evals/suites', { background: true }),
        ]);
        const [statusResult, extensionsResult, capsResult, approvalsResult, runsResult, setupResult, channelHealthResult] = await Promise.allSettled([
          api.get('/operator/status', { background: true }),
          api.get('/operator/extensions', { background: true }),
          api.get('/operator/capabilities', { background: true }),
          api.get('/operator/approvals?status=pending', { background: true }),
          api.get('/runtime/runs?limit=20', { background: true }),
          api.get('/operator/setup/status', { background: true }),
          api.get('/operator/channels/health', { background: true }),
        ]);
        if (!_mounted) return;
        const [qualitySummaryResult, releaseReadinessResult, evalSuitesResult] = await qualitySignalsPromise;
        if (!_mounted) return;

        const status = settledValue(statusResult);
        const extensionsData = settledValue(extensionsResult);
        const caps = settledValue(capsResult);
        const approvals = settledValue(approvalsResult);
        const runs = settledValue(runsResult);
        const setupStatus = settledValue(setupResult);
        const channelHealth = settledValue(channelHealthResult);
        this.applyQualitySignals(
          settledValue(qualitySummaryResult),
          settledValue(releaseReadinessResult),
          settledValue(evalSuitesResult)
        );

        this.statusData = status;
        this.statusUnavailable = !status;
        this.setupStatus = setupStatus;
        this.setupUnavailable = !setupStatus;
        const extensionModules = extensionsData ? (Array.isArray(extensionsData.modules) ? extensionsData.modules : []) : [];
        const extensionCaps = extensionsData ? (Array.isArray(extensionsData.capabilities) ? extensionsData.capabilities : []) : [];
        const extensionChannels = extensionsData ? (Array.isArray(extensionsData.channels) ? extensionsData.channels : []) : [];
        const moduleSourceAvailable = (extensionsData && Array.isArray(extensionsData.modules));
        this.modules = extensionModules;
        this.modulesUnavailable = this.modules.length === 0 && !moduleSourceAvailable;
        const capabilityFallback = Array.isArray(caps) ? caps : (caps ? (caps.items || caps.capabilities || []) : []);
        const capabilitySourceAvailable = (extensionsData && Array.isArray(extensionsData.capabilities)) || Boolean(caps);
        this.capabilities = extensionCaps.length
          ? extensionCaps
          : capabilityFallback;
        this.capabilitiesUnavailable = this.capabilities.length === 0 && !capabilitySourceAvailable;
        const channelFallback = channelHealth ? (Array.isArray(channelHealth) ? channelHealth : (channelHealth.items || [])) : [];
        const channelSourceAvailable = (extensionsData && Array.isArray(extensionsData.channels)) || Boolean(channelHealth);
        this.channelHealth = extensionChannels.length
          ? extensionChannels.map(item => item.health || { name: item.name || '', state: item.status || 'unknown' })
          : channelFallback;
        this.channelHealthUnavailable = this.channelHealth.length === 0 && !channelSourceAvailable;

        this.approvalsUnavailable = !approvals;
        const pendingItems = approvals ? (Array.isArray(approvals) ? approvals : (approvals.items || approvals.tickets || [])) : [];
        this.pendingApprovals = pendingItems.length;

        this.runsUnavailable = !runs;
        const runItems = runs ? (Array.isArray(runs) ? runs : (runs.items || [])) : [];
        const sortedRuns = runItems.slice().sort((a, b) =>
          new Date(b.updated_at || b.ended_at || b.created_at || 0) - new Date(a.updated_at || a.ended_at || a.created_at || 0)
        );
        this.recentRunsTotal = runItems.length;
        this.failedRuns = runItems.filter(item => {
          const statusValue = overviewRunStatus(item);
          return statusValue === 'failed' || statusValue === 'verification_failed';
        }).length;
        this.recentSuccess = null;
        this.recentSuccessUnavailable = false;

        const successCandidate = sortedRuns.find(item => SUCCESS_RUN_STATES.includes(overviewRunStatus(item)));
        if (successCandidate) {
          const successID = successCandidate.id || successCandidate.ID || '';
          if (successID) {
            try {
              const completion = await api.get('/runtime/runs/' + encodeURIComponent(successID) + '/completion', { background: true });
              if (_mounted) this.recentSuccess = this.buildRecentSuccess(successCandidate, completion);
            } catch (_) {
              if (_mounted) this.recentSuccessUnavailable = true;
            }
          }
        }

        this.degradedModules = this.modules.filter(module => {
          const health = this.moduleHealthStr(module);
          return !HEALTHY_MODULE_STATES.includes(health) && health !== 'unknown';
        }).length;
        this.degradedCaps = this.capabilities.filter(capability => {
          const health = this.capHealthStr(capability);
          return !HEALTHY_CAPABILITY_STATES.includes(health) && health !== 'unknown';
        }).length;

        this.connectedChannels = this.channelHealth.filter(item => CONNECTED_CHANNEL_STATES.includes(String(item.state || '').toLowerCase())).length;
        this.warningChannels = this.channelHealth.filter(item => WARNING_CHANNEL_STATES.includes(String(item.state || '').toLowerCase())).length;

        const warnings = [];
        const startupDiagnostics = operatorStartupDiagnostics(status);
        const statusState = operatorStatusState(status);
        const statusSummary = operatorStatusSummary(status);
        if (this.statusUnavailable) warnings.push('Operator status unavailable');
        else if (statusState === 'degraded') warnings.push(statusSummary || (t('overviewWarningDegraded') || 'System is running in degraded mode'));
        else if (statusState !== 'ready') warnings.push(statusSummary || (t('overviewWarningDown') || 'System health check failed'));
        if (this.setupUnavailable) warnings.push('Setup readiness unavailable');
        if (this.modulesUnavailable) warnings.push('Capability pack snapshot unavailable');
        else if (this.degradedModules > 0) warnings.push(this.degradedModules + ' capability pack(s) need attention');
        if (this.capabilitiesUnavailable) warnings.push('Capability snapshot unavailable');
        else if (this.capabilities.length === 0) warnings.push(t('overviewWarningNoCaps') || 'No capabilities registered — check model configuration');
        if (this.degradedCaps > 0) warnings.push(this.degradedCaps + ' capability reports need attention');
        if (this.channelHealthUnavailable) warnings.push('Channel health unavailable');
        else if (this.warningChannels > 0) warnings.push(this.warningChannels + ' channel connection(s) are unstable');
        if (this.approvalsUnavailable) warnings.push('Pending approvals snapshot unavailable');
        else if (this.pendingApprovals > 0) warnings.push(this.pendingApprovals + ' approval request(s) are waiting');
        if (this.runsUnavailable) warnings.push('Recent run history unavailable');
        else if (this.recentSuccessUnavailable) warnings.push('Latest run completion receipt unavailable');
        if (this.releaseReadinessUnavailable) warnings.push('Release readiness unavailable');
        else if (!this.releaseReadiness.ready) warnings.push(this.releaseReadinessBlockerCount() + ' release readiness blocker(s)');
        if (this.qualitySummaryUnavailable) warnings.push('Quality summary unavailable');
        if (this.evalSuitesUnavailable) warnings.push('Eval suites unavailable');
        const updateInfo = status && status.update;
        if (updateInfo && updateInfo.up_to_date === false) {
          const latest = updateInfo.latest_version || 'a newer release';
          warnings.push('Update available: ' + latest + '. Run `hopclaw update`.');
        }
        this.warnings = startupDiagnostics === 'quiet_when_healthy' && warnings.length > 1
          ? warnings.slice(0, 1)
          : warnings;

        if (this.fatalBackendIssue()) {
          this.error = true;
        }
      } catch (_) {
        if (_mounted) this.error = true;
      }
      if (_mounted) this.loading = false;
    },

    mounted() {
      _mounted = true;
      this.loadAll();
      refreshTimer = setInterval(() => { if (_mounted) this.loadAll(); }, AUTO_REFRESH_MS);
    },

    unmounted() {
      _mounted = false;
      if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null; }
    },
  };
}
