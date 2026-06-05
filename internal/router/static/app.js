const entries = new Map();
const clusterGroup = L.markerClusterGroup();

let spiderfiedMarkers = null;
clusterGroup.on('spiderfied',   e => { spiderfiedMarkers = e.markers.slice(); });
clusterGroup.on('unspiderfied', ()  => { spiderfiedMarkers = null; });

function withSpiderfyPreserved(fn) {
  const saved = spiderfiedMarkers ? spiderfiedMarkers.slice() : null;
  fn();
  if (!saved) return;
  setTimeout(() => {
    for (const m of saved) {
      if (!clusterGroup.hasLayer(m)) continue;
      const parent = clusterGroup.getVisibleParent(m);
      if (parent && typeof parent.spiderfy === 'function') {
        parent.spiderfy();
        return;
      }
    }
  }, 0);
}

const map = L.map('map').setView([20, 0], 2);

const TILE_THEMES = {
  default: {
    url: 'https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png',
    options: {
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
    }
  },
  dark: {
    url: 'https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png',
    options: {
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>',
      subdomains: 'abcd',
      maxZoom: 20
    }
  },
  satellite: {
    url: 'https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}',
    options: {
      attribution: 'Tiles &copy; Esri &mdash; Source: Esri, i-cubed, USDA, USGS, AEX, GeoEye, Getmapping, Aerogrid, IGN, IGP, UPR-EGP, and the GIS User Community',
      maxZoom: 19
    }
  }
};

let currentTileLayer = null;

function setTheme(name) {
  if (!TILE_THEMES[name]) return;
  if (currentTileLayer) map.removeLayer(currentTileLayer);
  const { url, options } = TILE_THEMES[name];
  currentTileLayer = L.tileLayer(url, options).addTo(map);
  localStorage.setItem('mapTheme', name);
  document.querySelectorAll('.theme-toggle button').forEach(btn => btn.classList.remove('active'));
  const activeBtn = document.getElementById('theme-' + name);
  if (activeBtn) activeBtn.classList.add('active');
}

setTheme(localStorage.getItem('mapTheme') || 'default');

document.getElementById('theme-default').addEventListener('click', () => setTheme('default'));
document.getElementById('theme-dark').addEventListener('click', () => setTheme('dark'));
document.getElementById('theme-satellite').addEventListener('click', () => setTheme('satellite'));

const lightbox = document.getElementById('lightbox');
const lightboxImg = document.getElementById('lightbox-img');
document.getElementById('lightbox-close').addEventListener('click', () => lightbox.classList.remove('open'));
lightbox.addEventListener('click', e => { if (e.target === lightbox) lightbox.classList.remove('open'); });

function openLightbox(url) {
  lightboxImg.src = url;
  lightbox.classList.add('open');
}

// Event delegation for popup buttons and photo thumbnails
document.addEventListener('click', e => {
  const editBtn = e.target.closest('.btn-edit');
  if (editBtn) {
    e.stopPropagation();
    startRename(parseInt(editBtn.dataset.snapId, 10));
    return;
  }
  const deleteBtn = e.target.closest('.btn-delete');
  if (deleteBtn) {
    deleteSnap(parseInt(deleteBtn.dataset.snapId, 10));
    return;
  }
  const photo = e.target.closest('.popup-photos img');
  if (photo) {
    openLightbox(photo.src);
  }
});

function showToast(msg, variant = '') {
  const container = document.getElementById('toast-container');
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

function snapLabel(snap) {
  return snap.name || ('Snap #' + snap.id);
}

function buildPopup(snap) {
  const photosHtml = (snap.photos || []).map(p =>
    `<img src="${p.url}" alt="photo" />`
  ).join('');
  return `
    <span class="popup-title" id="snap-title-${snap.id}">${snapLabel(snap)}</span>
    <div class="popup-meta">${formatDate(snap.created_at)}<br/>${snap.latitude.toFixed(5)}, ${snap.longitude.toFixed(5)}</div>
    <div class="popup-photos">${photosHtml || '<em>No photos</em>'}</div>
    <div class="popup-actions">
      <button class="btn-edit" data-snap-id="${snap.id}">Edit</button>
      <button class="btn-delete" data-snap-id="${snap.id}">Delete</button>
    </div>
  `;
}

function startRename(id) {
  const entry = entries.get(id);
  if (!entry) return;
  const titleEl = document.getElementById('snap-title-' + id);
  if (!titleEl) return;
  const current = entry.snap.name || '';
  const input = document.createElement('input');
  input.className = 'name-input';
  input.value = current;
  input.maxLength = 100;
  input.placeholder = 'Snap #' + id;
  titleEl.replaceWith(input);
  input.focus();
  input.select();

  let saved = false;
  async function save() {
    if (saved) return;
    saved = true;
    const name = input.value.trim();
    input.replaceWith(titleEl);
    if (name === current) return;
    await fetch(`/snaps/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    });
  }

  input.addEventListener('blur', save);
  input.addEventListener('keydown', e => {
    if (e.key === 'Enter') { e.preventDefault(); input.blur(); }
    if (e.key === 'Escape') { saved = true; input.replaceWith(titleEl); }
  });
}

async function deleteSnap(id) {
  if (!confirm(`Delete "${snapLabel(entries.get(id)?.snap)}"? This cannot be undone.`)) return;
  const res = await fetch(`/snaps/${id}`, { method: 'DELETE' });
  if (!res.ok) alert('Failed to delete snap.');
}

function formatDate(iso) {
  return new Date(iso).toLocaleString();
}

function addSnapMarker(snap) {
  if (entries.has(snap.id)) return;
  const latlng = [snap.latitude, snap.longitude];
  const marker = L.marker(latlng);
  marker.bindPopup(buildPopup(snap), { maxWidth: 320 });
  entries.set(snap.id, { marker, snap });
  withSpiderfyPreserved(() => clusterGroup.addLayer(marker));
}

function updateSnapMarker(id, name) {
  const entry = entries.get(id);
  if (!entry) return;
  entry.snap.name = name;
  entry.marker.setPopupContent(buildPopup(entry.snap));
}

async function loadSnaps() {
  let snaps;
  try {
    const res = await fetch('/snaps');
    snaps = await res.json();
  } catch (e) {
    console.error('Failed to load snaps', e);
    return;
  }

  if (!snaps || snaps.length === 0) return;

  const bounds = [];
  snaps.forEach(snap => {
    addSnapMarker(snap);
    bounds.push([snap.latitude, snap.longitude]);
  });

  map.addLayer(clusterGroup);
  if (bounds.length > 0) {
    map.fitBounds(bounds, { padding: [40, 40], maxZoom: 14 });
  }
}

function connectEvents() {
  const es = new EventSource('/events');

  es.addEventListener('snap', e => {
    const snap = JSON.parse(e.data);
    addSnapMarker(snap);
    map.addLayer(clusterGroup);
    showToast(`New snap received: ${snapLabel(snap)}`);
  });

  es.addEventListener('snap_updated', e => {
    const { id, name } = JSON.parse(e.data);
    updateSnapMarker(id, name);
    showToast(`Snap renamed: ${name || 'Snap #' + id}`, 'info');
  });

  es.addEventListener('snap_deleted', e => {
    const { id } = JSON.parse(e.data);
    const entry = entries.get(id);
    if (entry) {
      withSpiderfyPreserved(() => { clusterGroup.removeLayer(entry.marker); entries.delete(id); });
    }
    showToast(`Snap #${id} deleted`, 'danger');
  });

  es.onerror = () => {};
}

loadSnaps();
connectEvents();
