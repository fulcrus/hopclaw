// ---------------------------------------------------------------------------
// Shared link and artifact helpers for the console UI
// ---------------------------------------------------------------------------

export const ARTIFACT_URI_PREFIX = 'artifact://local/';

const URL_PROTOCOLS = new Set(['http:', 'https:']);

function escapeAttr(value) {
  return String(value || '').replace(/"/g, '&quot;');
}

export function safeExternalURL(target) {
  const value = String(target || '').trim();
  if (!value || value.startsWith('//')) return '';
  if (value.startsWith('#')) return value;
  try {
    const parsed = new URL(value, window.location.origin);
    if (!URL_PROTOCOLS.has(parsed.protocol)) return '';
    return parsed.toString();
  } catch (_) {
    return '';
  }
}

export function safeImageSource(target) {
  const value = String(target || '').trim();
  if (!value || value.startsWith('//')) return '';
  if (/^data:image\//i.test(value) || /^blob:/i.test(value)) return value;
  return safeExternalURL(value);
}

export function parseArtifactID(value) {
  const trimmed = String(value || '').trim();
  if (!trimmed) return '';
  return trimmed.indexOf(ARTIFACT_URI_PREFIX) === 0 ? trimmed.substring(ARTIFACT_URI_PREFIX.length) : trimmed;
}

export function artifactPreviewPath(input) {
  if (!input) return '';
  let id = '';
  if (typeof input === 'string') {
    id = parseArtifactID(input);
  } else if (typeof input === 'object') {
    id = String(input.id || '').trim() || parseArtifactID(input.uri || input.target || '');
  }
  if (!id) return '';
  return '/operator/artifacts/' + encodeURIComponent(id) + '/preview';
}

export function openSafeWindow(target) {
  const href = safeExternalURL(target) || artifactPreviewPath(target);
  if (!href) return false;
  window.open(href, '_blank', 'noopener,noreferrer');
  return true;
}

export function safeLinkHTML(label, href) {
  const safeHref = safeExternalURL(href);
  if (!safeHref) return label;
  return '<a href="' + escapeAttr(safeHref) + '" target="_blank" rel="noopener noreferrer">' + label + '</a>';
}
