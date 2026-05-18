export function operatorStatusState(status) {
  const state = String(status && status.state || '').trim().toLowerCase();
  if (state) return state;
  if (!status || typeof status !== 'object') return 'unknown';
  if (status.ok === true) return 'ready';
  if (status.ok === false) return 'blocked';
  return 'unknown';
}

export function operatorStatusSummary(status) {
  if (!status || typeof status !== 'object') return '';
  const summary = String(status.summary || '').trim();
  if (summary) return summary;
  const warnings = Array.isArray(status.warnings) ? status.warnings : [];
  for (const item of warnings) {
    const text = String(item || '').trim();
    if (text) return text;
  }
  return '';
}

export function operatorStartupDiagnostics(status) {
  return String(status && status.user_surface && status.user_surface.startup_diagnostics || '').trim().toLowerCase();
}

export function operatorStatusDotClass(status, unavailable) {
  if (unavailable) return 'warn';
  switch (operatorStatusState(status)) {
    case 'ready':
      return 'ok';
    case 'degraded':
      return 'warn';
    default:
      return 'err';
  }
}

export function operatorStatusLabel(status, unavailable, t) {
  if (unavailable) return t('overviewUnavailable') || 'Unavailable';
  switch (operatorStatusState(status)) {
    case 'ready':
      return t('overviewHealthy') || 'Healthy';
    case 'degraded':
      return t('overviewDegraded') || 'Degraded';
    case 'blocked':
    case 'unhealthy':
      return t('overviewDown') || 'Down';
    default:
      return t('overviewNeedsAttention') || 'Needs attention';
  }
}
