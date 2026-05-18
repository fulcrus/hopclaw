// ---------------------------------------------------------------------------
// HTTP API wrapper & toast notifications
// ---------------------------------------------------------------------------

const TOKEN_KEY = 'hc_auth_token';
const TOAST_DURATION_MS = 4000;
const TOAST_FADE_MS = 300;
const CSRF_COOKIE = '_hopclaw_csrf';

// ---------------------------------------------------------------------------
// Token management
// ---------------------------------------------------------------------------

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || '';
}

export function setToken(token) {
  if (token) {
    localStorage.setItem(TOKEN_KEY, token);
  } else {
    localStorage.removeItem(TOKEN_KEY);
  }
}

export function consoleBasePath() {
  const path = window.location.pathname || '/dashboard/';
  if (path.startsWith('/dashboard')) return '/dashboard';
  if (path.startsWith('/webchat')) return '/webchat';
  return '/dashboard';
}

export function consolePath(pathname = '') {
  const suffix = pathname.startsWith('/') ? pathname : '/' + pathname;
  return consoleBasePath() + suffix;
}

// ---------------------------------------------------------------------------
// Toast notification system
// ---------------------------------------------------------------------------

let toastContainer = null;

function ensureToastContainer() {
  if (toastContainer) return toastContainer;
  toastContainer = document.createElement('div');
  toastContainer.className = 'hc-toast-container';
  document.body.appendChild(toastContainer);
  return toastContainer;
}

/**
 * Show a floating toast notification.
 * @param {string} message - The message to display.
 * @param {'info'|'error'|'success'|'warning'} type - Toast type for styling.
 */
export function showToast(message, type = 'info') {
  const container = ensureToastContainer();
  const toast = document.createElement('div');
  toast.className = 'hc-toast hc-toast-' + type;
  toast.setAttribute('data-toast-type', type);
  if (!Array.isArray(window.__hcToastLog)) {
    window.__hcToastLog = [];
  }
  window.__hcToastLog.push({
    message: String(message || ''),
    type: String(type || 'info'),
    at: Date.now(),
  });
  toast.innerHTML = '<span class="hc-toast-msg">' + escapeHtml(message) + '</span>';
  container.appendChild(toast);

  // Auto-dismiss with exit animation
  setTimeout(() => {
    toast.classList.add('hc-toast-out');
    setTimeout(() => {
      if (toast.parentNode) toast.parentNode.removeChild(toast);
    }, TOAST_FADE_MS);
  }, TOAST_DURATION_MS);
}

function escapeHtml(s) {
  const d = document.createElement('div');
  d.appendChild(document.createTextNode(s));
  return d.innerHTML;
}

function getCookie(name) {
  const cookies = document.cookie ? document.cookie.split(';') : [];
  for (const raw of cookies) {
    const part = raw.trim();
    if (!part.startsWith(name + '=')) continue;
    return decodeURIComponent(part.slice(name.length + 1));
  }
  return '';
}

// ---------------------------------------------------------------------------
// HTTP request helper
// ---------------------------------------------------------------------------

function suppressErrorToast(method, options) {
  if (!options) return false;
  if (options.background === true) return true;
  if (options.errorToast === false) return true;
  if (options.silent === true) return true;
  return false;
}

async function request(method, path, body, options = {}) {
  const quietErrors = suppressErrorToast(method, options);
  const headers = { 'Content-Type': 'application/json' };
  const tok = getToken();
  if (tok) headers['Authorization'] = 'Bearer ' + tok;
  if (method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS') {
    const csrf = getCookie(CSRF_COOKIE);
    if (csrf) headers['X-CSRF-Token'] = csrf;
  }

  const opts = { method, headers };
  if (body !== undefined) opts.body = JSON.stringify(body);

  let res;
  try {
    res = await fetch(path, opts);
  } catch (cause) {
    const msg = (cause && cause.message) || 'Network request failed';
    if (!quietErrors) showToast(msg, 'error');
    const err = cause instanceof Error ? cause : new Error(msg);
    err.status = 0;
    err.body = '';
    err.payload = null;
    err.code = '';
    throw err;
  }

  if (!res.ok) {
    const text = await res.text();
    let msg = 'HTTP ' + res.status;
    let parsed = null;
    try {
      parsed = JSON.parse(text);
      msg = parsed.error || parsed.message || msg;
    } catch (_) {
      if (text) msg = text;
    }
    if (!quietErrors) showToast(msg, 'error');
    const err = new Error(msg);
    err.status = res.status;
    err.body = text;
    err.payload = parsed;
    err.code = parsed && typeof parsed.code === 'string' ? parsed.code : '';
    throw err;
  }

  // Handle 204 No Content
  if (res.status === 204) return null;

  const contentType = res.headers.get('content-type') || '';
  if (contentType.includes('application/json')) {
    return res.json();
  }

  return res.text();
}

// ---------------------------------------------------------------------------
// Public API object
// ---------------------------------------------------------------------------

export const api = {
  get:   (path, options) => request('GET', path, undefined, options),
  post:  (path, body, options) => request('POST', path, body, options),
  put:   (path, body, options) => request('PUT', path, body, options),
  patch: (path, body, options) => request('PATCH', path, body, options),
  del:   (path, options) => request('DELETE', path, undefined, options),
};
