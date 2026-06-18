// Shared helpers used by both the map page (app.js) and the feed page
// (feed.js): API token handling, the authenticated fetch wrapper, toasts,
// and small formatting utilities.

const API_TOKEN_KEY = 'apiToken';

function getApiToken() {
  return localStorage.getItem(API_TOKEN_KEY) || '';
}

// Wrapper around fetch that attaches the stored bearer token and, on a 401,
// prompts for a token, stores it, and retries once. When the server runs
// without API_TOKEN configured this is a plain fetch.
async function apiFetch(url, options = {}) {
  const doFetch = () => {
    const headers = Object.assign({}, options.headers);
    const token = getApiToken();
    if (token) headers['Authorization'] = 'Bearer ' + token;
    return fetch(url, Object.assign({}, options, { headers }));
  };
  let res = await doFetch();
  if (res.status === 401) {
    const entered = prompt('This server requires an API token:');
    if (entered === null) return res;
    localStorage.setItem(API_TOKEN_KEY, entered.trim());
    res = await doFetch();
  }
  return res;
}

function showToast(msg, variant = '') {
  const container = document.getElementById('toast-container');
  if (!container) return;
  const el = document.createElement('div');
  el.className = 'toast' + (variant ? ' toast-' + variant : '');
  el.textContent = msg;
  container.appendChild(el);
  requestAnimationFrame(() => { requestAnimationFrame(() => el.classList.add('show')); });
  setTimeout(() => {
    el.classList.remove('show');
    el.addEventListener('transitionend', () => el.remove(), { once: true });
  }, 3500);
}

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function formatDate(iso) {
  const d = new Date(iso);
  // Guard against missing/zero timestamps (e.g. the Go zero time).
  if (isNaN(d.getTime()) || d.getFullYear() <= 1) return 'just now';
  return d.toLocaleString();
}

// Build the SSE URL, carrying the token as a query parameter since
// EventSource cannot set headers.
function eventsUrl() {
  const token = getApiToken();
  return token ? '/events?token=' + encodeURIComponent(token) : '/events';
}
