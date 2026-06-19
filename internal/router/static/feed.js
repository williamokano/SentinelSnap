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
    closeAllMenus();
    // Use the full photo URL (/photos/{token}), not the <img> src, which points
    // at the thumbnail (/photos/{token}/thumb) and would yield "thumb" as the token.
    deletePhoto(photoToken({ url: card.dataset.fullUrl }));
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
// Panning, wheel zoom and two-finger pinch-zoom are handled by Panzoom
// (https://github.com/timmywil/panzoom) — a small, well-tested library that
// gets the fiddly pointer/touch maths right across desktop and mobile. We just
// wire it to the viewer image and layer wheel zoom and double-tap-to-toggle on
// top. Panzoom is loaded from the CDN in feed.html (see the <script> tag).

const viewer = document.getElementById('viewer');
const viewerStage = document.getElementById('viewer-stage');
const viewerImg = document.getElementById('viewer-img');
const MIN_SCALE = 1;
const MAX_SCALE = 8;

const panzoom = Panzoom(viewerImg, {
  minScale: MIN_SCALE,
  maxScale: MAX_SCALE,
  // Don't let the image be dragged around until it's actually zoomed in.
  panOnlyWhenZoomed: true,
  cursor: 'grab',
});

// Wheel zoom, anchored on the cursor. Panzoom recommends binding this to the
// element's parent so the focal-point maths line up.
viewerStage.addEventListener('wheel', e => {
  if (!viewer.classList.contains('open')) return;
  panzoom.zoomWithWheel(e);
}, { passive: false });

// Double-click / double-tap toggles between fit and 2.5×, anchored on the point.
viewerImg.addEventListener('dblclick', e => {
  if (panzoom.getScale() > MIN_SCALE) panzoom.reset();
  else panzoom.zoomToPoint(2.5, e);
});

function openViewer(src) {
  viewerImg.src = src;
  panzoom.reset({ animate: false });
  viewer.classList.add('open');
}

function closeViewer() {
  viewer.classList.remove('open');
  panzoom.reset({ animate: false });
  viewerImg.src = '';
}

document.getElementById('viewer-close').addEventListener('click', closeViewer);
viewer.addEventListener('click', e => { if (e.target === viewer || e.target === viewerStage) closeViewer(); });
document.addEventListener('keydown', e => { if (e.key === 'Escape') closeViewer(); });

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
