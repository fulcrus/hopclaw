import { operatorStatusDotClass, operatorStatusLabel, operatorStatusState, operatorStatusSummary } from '../status_state.js';

export function buildSettingsDiagnosticsSection({
  api,
  t,
  settledValue,
}) {
  return {
    diagLoading: false,
    diagWarnings: [],
    diagStatusUnavailable: false,
    diagUsageUnavailable: false,
    diagAuditUnavailable: false,
    diagLogsUnavailable: false,
    diagStatus: null,
    diagUsage: null,
    diagAuditEvents: [],
    diagLogEntries: [],

    diagFormatTokens(n) {
      if (!n) return '0';
      if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
      if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
      return String(n);
    },

    diagStatusDotClass() {
      return operatorStatusDotClass(this.diagStatus, this.diagStatusUnavailable);
    },

    diagStatusLabel() {
      if (this.diagStatusUnavailable) return t('settingsDiagStatusLabel') || 'Unavailable';
      return operatorStatusLabel(this.diagStatus, this.diagStatusUnavailable, t);
    },

    async loadDiagnostics() {
      this.diagLoading = true;
      try {
        const [statusResult, usageResult, auditResult, logsResult] = await Promise.allSettled([
          api.get('/operator/status', { background: true }),
          api.get('/operator/usage/summary', { background: true }),
          api.get('/operator/audit/events', { background: true }),
          api.get('/operator/wire/entries', { background: true }),
        ]);
        const status = settledValue(statusResult);
        const usage = settledValue(usageResult);
        const audit = settledValue(auditResult);
        const logs = settledValue(logsResult);
        this.diagStatusUnavailable = statusResult.status !== 'fulfilled';
        this.diagUsageUnavailable = usageResult.status !== 'fulfilled';
        this.diagAuditUnavailable = auditResult.status !== 'fulfilled';
        this.diagLogsUnavailable = logsResult.status !== 'fulfilled';
        this.diagStatus = status;
        this.diagUsage = usage;
        const auditItems = audit ? (Array.isArray(audit) ? audit : (audit.items || audit.events || [])) : [];
        this.diagAuditEvents = auditItems;
        const logItems = logs ? (Array.isArray(logs) ? logs : (logs.items || logs.entries || [])) : [];
        this.diagLogEntries = logItems;
        const warnings = [];
        if (this.diagStatusUnavailable) warnings.push(t('settingsDiagStatusUnavailable') || 'System status unavailable');
        else {
          const state = operatorStatusState(status);
          const summary = operatorStatusSummary(status);
          if (state === 'degraded') warnings.push(summary || (t('overviewWarningDegraded') || 'System is running in degraded mode'));
          else if (state !== 'ready') warnings.push(summary || (t('overviewDown') || 'Down'));
        }
        if (this.diagUsageUnavailable) warnings.push(t('settingsDiagUsageUnavailable') || 'Usage summary unavailable');
        if (this.diagAuditUnavailable) warnings.push(t('settingsDiagAuditUnavailable') || 'Audit events unavailable');
        if (this.diagLogsUnavailable) warnings.push(t('settingsDiagLogsUnavailable') || 'Wire/log entries unavailable');
        this.diagWarnings = warnings;
      } catch (_) {
        this.diagStatus = null;
        this.diagUsage = null;
        this.diagAuditEvents = [];
        this.diagLogEntries = [];
        this.diagStatusUnavailable = true;
        this.diagUsageUnavailable = true;
        this.diagAuditUnavailable = true;
        this.diagLogsUnavailable = true;
        this.diagWarnings = [t('settingsDiagDataUnavailable') || 'Diagnostics data unavailable'];
      }
      this.diagLoading = false;
    },
  };
}
