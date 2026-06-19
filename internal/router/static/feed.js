// Photo feed: a scrollable list of every photo (snap-linked and standalone),
// newest first. Each card shows the image, when it was created, and a ⋯ menu
// whose only action (for now) is Delete. Clicking an image opens a zoomable
// viewer.

const feedEl = document.getElementById('feed');
const emptyEl = document.getElementById('feed-empty');

// Tracks rendered cards by photo id so SSE events can update in place.
const cards = new Map();

function photoToken(photo) {
  // URL is "/photos/{token}"; the token is also the delete capability.
  return photo.url.split('/').pop();
}

function updateEmptyState() {
  emptyEl.hidden = cards.size > 0;
}

function thumbUrl(photoUrl) {
  return photoUrl + '/thumb';
}

function buildCard(photo) {
  const card = document.createElement('article');
  card.className = 'feed-card';
  card.dataset.photoId = photo.id;
  card.dataset.fullUrl = photo.url;
  if (photo.snap_id != null) card.dataset.snapId = photo.snap_id;

  card.innerHTML = `
    <div class="feed-card-bar">
      <span class="feed-date">${escapeHtml(formatDate(photo.created_at))}</span>
      <div class="feed-menu">
        <button class="feed-menu-btn" aria-label="Photo options" aria-haspopup="true" aria-expanded="false">&#8943;</button>
        <div class="feed-menu-list" hidden>
          <button class="feed-delete">Delete</button>
        </div>
      </div>
    </div>
    <div class="feed-photo">
      <img class="feed-img" src="${escapeHtml(thumbUrl(photo.url))}" alt="photo" loading="lazy" />
    </div>
  `;
  return card;
}

// Insert a card so that the list stays ordered newest first. Photos arriving
// via SSE are newer than everything already shown, so they go on top.
function addPhoto(photo, { prepend = false } = {}) {
  if (cards.has(photo.id)) return;
  const card = buildCard(photo);
  cards.set(photo.id, card);
  // The empty-state placeholder stays as feedEl's first child (hidden once
  // there are cards), so inserting at firstChild puts new photos on top.
  if (prepend) feedEl.insertBefore(card, feedEl.firstChild);
  else feedEl.appendChild(card);
  updateEmptyState();
}

function removePhoto(id) {
  const card = cards.get(id);
  if (!card) return;
  card.remove();
  cards.delete(id);
  updateEmptyState();
}

function removeBySnap(snapId) {
  for (const [id, card] of [...cards]) {
    if (card.dataset.snapId === String(snapId)) removePhoto(id);
  }
}

async function deletePhoto(token) {
  if (!confirm('Delete this photo? This cannot be undone.')) return;
  const res = await apiFetch('/photos/' + encodeURIComponent(token), { method: 'DELETE' });
  if (!res.ok) showToast('Failed to delete photo.', 'danger');
  // On success the photo_deleted SSE event removes the card and toasts.
}

// ---- Event delegation: menu toggle, delete, open viewer ----

document.addEventListener('click', e => {
  const menuBtn = e.target.closest('.feed-menu-btn');
  if (menuBtn) {
    const list = menuBtn.parentElement.querySelector('.feed-menu-list');
    const open = list.hidden;
    closeAllMenus();
    list.hidden = !open;
    menuBtn.setAttribute('aria-expanded', String(open));
    return;
  }

  const del = e.target.closest('.feed-delete');
  if (del) {
    const card = del.closest('.feed-card');
    const img = card.querySelector('.feed-img');
    closeAllMenus();
    deletePhoto(photoToken({ url: img.getAttribute('src') }));
    return;
  }

  const img = e.target.closest('.feed-img');
  if (img) {
    const card = img.closest('.feed-card');
    openViewer(card ? card.dataset.fullUrl : img.src);
    return;
  }

  // A click anywhere else closes any open menu.
  closeAllMenus();
});

function closeAllMenus() {
  document.querySelectorAll('.feed-menu-list').forEach(l => { l.hidden = true; });
  document.querySelectorAll('.feed-menu-btn').forEach(b => b.setAttribute('aria-expanded', 'false'));
}

// ---- Zoomable viewer (pan + wheel zoom / double-click / pinch-zoom) ----
//
// Touch events are used for mobile (touchstart/move/end) so we can call
// preventDefault() reliably — pointer events cannot preventDefault on passive
// touch handlers in most browsers. Mouse events handle desktop drag; wheel
// handles scroll-to-zoom on desktop.

const viewer = document.getElementById('viewer');
const viewerStage = document.getElementById('viewer-stage');
const viewerImg = document.getElementById('viewer-img');
const MIN_SCALE = 1;
const MAX_SCALE = 8;
let vScale = 1, vX = 0, vY = 0;

function applyTransform() {
  viewerImg.style.transform = `translate(${vX}px, ${vY}px) scale(${vScale})`;
}

function resetTransform() {
  vScale = 1; vX = 0; vY = 0;
  applyTransform();
}

function clampScale(s) { return Math.min(MAX_SCALE, Math.max(MIN_SCALE, s)); }

function imgCenter() {
  const r = viewerImg.getBoundingClientRect();
  return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
}

function zoomAt(clientX, clientY, newScale) {
  const prev = vScale;
  vScale = clampScale(newScale);
  const c = imgCenter();
  const cx = clientX - c.x;
  const cy = clientY - c.y;
  vX -= cx * (vScale / prev - 1);
  vY -= cy * (vScale / prev - 1);
  if (vScale === MIN_SCALE) { vX = 0; vY = 0; }
  applyTransform();
}

function openViewer(src) {
  viewerImg.src = src;
  resetTransform();
  viewer.classList.add('open');
}

function closeViewer() {
  viewer.classList.remove('open');
  viewerImg.src = '';
}

document.getElementById('viewer-close').addEventListener('click', closeViewer);
viewer.addEventListener('click', e => { if (e.target === viewer || e.target === viewerStage) closeViewer(); });
document.addEventListener('keydown', e => { if (e.key === 'Escape') closeViewer(); });

// ---- Mouse: drag to pan ----
let mousePan = null;

viewerImg.addEventListener('mousedown', e => {
  if (e.button !== 0) return;
  e.preventDefault();
  mousePan = { ox: vX - e.clientX, oy: vY - e.clientY };
});

window.addEventListener('mousemove', e => {
  if (!mousePan || vScale <= MIN_SCALE) return;
  vX = e.clientX + mousePan.ox;
  vY = e.clientY + mousePan.oy;
  applyTransform();
});

window.addEventListener('mouseup', () => { mousePan = null; });

// Double-click to toggle 2.5× zoom at click point.
viewerImg.addEventListener('dblclick', e => {
  if (vScale > MIN_SCALE) resetTransform();
  else zoomAt(e.clientX, e.clientY, 2.5);
});

// ---- Wheel: zoom anchored on cursor ----
viewer.addEventListener('wheel', e => {
  if (!viewer.classList.contains('open')) return;
  e.preventDefault();
  zoomAt(e.clientX, e.clientY, vScale * (e.deltaY < 0 ? 1.15 : 1 / 1.15));
}, { passive: false });

// ---- Touch: single-finger pan, two-finger pinch-zoom ----

let touches = {}; // pointerId-like keyed by touch.identifier
let touchPan = null;
let pinch = null;

function touchMidpoint(t1, t2) {
  return {
    x: (t1.clientX + t2.clientX) / 2,
    y: (t1.clientY + t2.clientY) / 2,
    dist: Math.hypot(t1.clientX - t2.clientX, t1.clientY - t2.clientY) || 1,
  };
}

viewerImg.addEventListener('touchstart', e => {
  e.preventDefault();
  for (const t of e.changedTouches) {
    touches[t.identifier] = t;
  }
  const ids = Object.keys(touches);
  if (ids.length === 1) {
    const t = touches[ids[0]];
    touchPan = { ox: vX - t.clientX, oy: vY - t.clientY };
    pinch = null;
  } else if (ids.length === 2) {
    touchPan = null;
    const t1 = touches[ids[0]], t2 = touches[ids[1]];
    pinch = { ...touchMidpoint(t1, t2), scale: vScale };
  }
}, { passive: false });

viewerImg.addEventListener('touchmove', e => {
  e.preventDefault();
  for (const t of e.changedTouches) {
    touches[t.identifier] = t;
  }
  const ids = Object.keys(touches);
  if (ids.length === 2 && pinch) {
    const t1 = touches[ids[0]], t2 = touches[ids[1]];
    const now = touchMidpoint(t1, t2);
    const newScale = clampScale(pinch.scale * (now.dist / pinch.dist));
    // Zoom anchored on the pinch midpoint.
    const prev = vScale;
    vScale = newScale;
    vX += (now.x - pinch.x) - (pinch.x - imgCenter().x) * (newScale / prev - 1);
    vY += (now.y - pinch.y) - (pinch.y - imgCenter().y) * (newScale / prev - 1);
    applyTransform();
  } else if (ids.length === 1 && touchPan && vScale > MIN_SCALE) {
    const t = touches[ids[0]];
    vX = t.clientX + touchPan.ox;
    vY = t.clientY + touchPan.oy;
    applyTransform();
  }
}, { passive: false });

function onTouchEnd(e) {
  e.preventDefault();
  for (const t of e.changedTouches) {
    delete touches[t.identifier];
  }
  const ids = Object.keys(touches);
  pinch = null;
  if (ids.length === 1) {
    const t = touches[ids[0]];
    touchPan = { ox: vX - t.clientX, oy: vY - t.clientY };
  } else if (ids.length === 0) {
    touchPan = null;
    if (vScale <= MIN_SCALE) resetTransform();
  }
}
viewerImg.addEventListener('touchend', onTouchEnd, { passive: false });
viewerImg.addEventListener('touchcancel', onTouchEnd, { passive: false });

// ---- Loading + live updates ----

async function loadPhotos() {
  let photos;
  try {
    const res = await apiFetch('/photos');
    photos = await res.json();
  } catch (err) {
    console.error('Failed to load photos', err);
    return;
  }
  photos = photos || [];

  // Reconcile: drop cards the server no longer has, add the rest in order.
  const seen = new Set(photos.map(p => p.id));
  for (const id of [...cards.keys()]) {
    if (!seen.has(id)) removePhoto(id);
  }
  // Server returns newest first; append in that order.
  photos.forEach(p => addPhoto(p));
  updateEmptyState();
}

function connectEvents() {
  const es = new EventSource(eventsUrl());
  let wasDisconnected = false;

  es.addEventListener('open', () => {
    if (!wasDisconnected) return;
    wasDisconnected = false;
    loadPhotos();
  });

  es.addEventListener('photo', e => {
    addPhoto(JSON.parse(e.data), { prepend: true });
  });

  es.addEventListener('photo_deleted', e => {
    const { id } = JSON.parse(e.data);
    removePhoto(id);
    showToast('Photo deleted', 'danger');
  });

  // A new snap's photos belong in the feed too.
  es.addEventListener('snap', e => {
    const snap = JSON.parse(e.data);
    (snap.photos || []).forEach(p => addPhoto(p, { prepend: true }));
  });

  // When a snap is deleted (including when its last photo was removed), drop
  // any of its photos still shown in the feed.
  es.addEventListener('snap_deleted', e => {
    const { id } = JSON.parse(e.data);
    removeBySnap(id);
  });

  es.onerror = () => { wasDisconnected = true; };
}

loadPhotos().then(connectEvents);
