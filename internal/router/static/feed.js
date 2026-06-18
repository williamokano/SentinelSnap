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

function buildCard(photo) {
  const card = document.createElement('article');
  card.className = 'feed-card';
  card.dataset.photoId = photo.id;
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
    <img class="feed-img" src="${escapeHtml(photo.url)}" alt="photo" loading="lazy" />
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
    openViewer(img.src);
    return;
  }

  // A click anywhere else closes any open menu.
  closeAllMenus();
});

function closeAllMenus() {
  document.querySelectorAll('.feed-menu-list').forEach(l => { l.hidden = true; });
  document.querySelectorAll('.feed-menu-btn').forEach(b => b.setAttribute('aria-expanded', 'false'));
}

// ---- Zoomable viewer (pan + wheel/double-click/pinch zoom) ----

const viewer = document.getElementById('viewer');
const viewerImg = document.getElementById('viewer-img');
const MIN_SCALE = 1;
const MAX_SCALE = 6;
let vScale = 1, vX = 0, vY = 0;

function applyTransform() {
  viewerImg.style.transform = `translate(${vX}px, ${vY}px) scale(${vScale})`;
}

function resetTransform() {
  vScale = 1; vX = 0; vY = 0;
  applyTransform();
}

function openViewer(src) {
  viewerImg.src = src;
  resetTransform();
  viewer.classList.add('open');
}

function closeViewer() {
  viewer.classList.remove('open');
}

document.getElementById('viewer-close').addEventListener('click', closeViewer);
viewer.addEventListener('click', e => { if (e.target === viewer) closeViewer(); });
document.addEventListener('keydown', e => { if (e.key === 'Escape') closeViewer(); });

function clampScale(s) { return Math.min(MAX_SCALE, Math.max(MIN_SCALE, s)); }

// Wheel zoom, anchored on the cursor.
viewer.addEventListener('wheel', e => {
  if (!viewer.classList.contains('open')) return;
  e.preventDefault();
  const prev = vScale;
  vScale = clampScale(vScale * (e.deltaY < 0 ? 1.15 : 1 / 1.15));
  const rect = viewerImg.getBoundingClientRect();
  const cx = e.clientX - (rect.left + rect.width / 2);
  const cy = e.clientY - (rect.top + rect.height / 2);
  const ratio = vScale / prev - 1;
  vX -= cx * ratio;
  vY -= cy * ratio;
  if (vScale === MIN_SCALE) { vX = 0; vY = 0; }
  applyTransform();
}, { passive: false });

// Double-click / double-tap toggles between fit and 2.5x.
viewerImg.addEventListener('dblclick', () => {
  if (vScale > MIN_SCALE) resetTransform();
  else { vScale = 2.5; applyTransform(); }
});

// Pointer-based pan (mouse + single touch) and two-pointer pinch zoom.
const pointers = new Map();
let panStart = null;
let pinchStart = null;

viewerImg.addEventListener('pointerdown', e => {
  viewerImg.setPointerCapture(e.pointerId);
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
  if (pointers.size === 1) {
    panStart = { x: e.clientX - vX, y: e.clientY - vY };
  } else if (pointers.size === 2) {
    panStart = null;
    pinchStart = pinchState();
  }
});

viewerImg.addEventListener('pointermove', e => {
  if (!pointers.has(e.pointerId)) return;
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });

  if (pointers.size === 2 && pinchStart) {
    const now = pinchState();
    const prev = vScale;
    vScale = clampScale(pinchStart.scale * (now.dist / pinchStart.dist));
    const ratio = vScale / prev - 1;
    vX -= (pinchStart.cx) * ratio;
    vY -= (pinchStart.cy) * ratio;
    applyTransform();
  } else if (pointers.size === 1 && panStart && vScale > MIN_SCALE) {
    vX = e.clientX - panStart.x;
    vY = e.clientY - panStart.y;
    applyTransform();
  }
});

function endPointer(e) {
  pointers.delete(e.pointerId);
  if (pointers.size < 2) pinchStart = null;
  if (pointers.size === 1) {
    const [p] = pointers.values();
    panStart = { x: p.x - vX, y: p.y - vY };
  }
  if (pointers.size === 0 && vScale === MIN_SCALE) resetTransform();
}
viewerImg.addEventListener('pointerup', endPointer);
viewerImg.addEventListener('pointercancel', endPointer);

// Geometry of the current two-pointer gesture, relative to the image center.
function pinchState() {
  const pts = [...pointers.values()];
  const dx = pts[0].x - pts[1].x;
  const dy = pts[0].y - pts[1].y;
  const rect = viewerImg.getBoundingClientRect();
  const midX = (pts[0].x + pts[1].x) / 2;
  const midY = (pts[0].y + pts[1].y) / 2;
  return {
    dist: Math.hypot(dx, dy) || 1,
    scale: vScale,
    cx: midX - (rect.left + rect.width / 2),
    cy: midY - (rect.top + rect.height / 2),
  };
}

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
