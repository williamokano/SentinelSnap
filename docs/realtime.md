# Real-time updates — Server-Sent Events

SentinelSnap pushes live updates to all connected browser tabs using [Server-Sent Events (SSE)](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events). There is no WebSocket — SSE is one-directional (server → browser), which is all that is needed here.

## Why SSE and not WebSocket

| | SSE | WebSocket |
|---|---|---|
| Direction | Server → client only | Bidirectional |
| Browser reconnect | Automatic | Manual |
| Extra dependency | None | Needs a library |
| Suitable for push-only feeds | Yes | Overkill |

SentinelSnap only needs the server to push events to browsers. SSE reconnects automatically if the connection drops, requires no library on either side, and is simpler to implement securely.

## How the connection works

```
Browser                          Server
  │                                │
  │── GET /events ────────────────►│  (long-lived HTTP connection)
  │                                │  Content-Type: text/event-stream
  │◄── event: snap ────────────────│  new snap posted from phone
  │◄── event: snap_updated ────────│  someone renamed a snap
  │◄── event: snap_deleted ────────│  a snap was deleted
  │                                │
  │  (connection drops)            │
  │── GET /events ────────────────►│  browser auto-reconnects
```

The browser keeps one persistent HTTP connection open. The server holds a registry of all live connections in an in-memory hub. When something happens, the hub serialises the payload to JSON and writes it to every connected client simultaneously.

## Hub internals

`internal/hub/hub.go` — the hub is a goroutine-safe map of channels, one per connected client.

- `Broadcast(eventType, payload)` marshals the payload and sends the SSE-formatted message (`event: <type>\ndata: <json>\n\n`) to every client channel with a non-blocking send (slow or stale clients are skipped without blocking the caller).
- `ServeSSE(w, r)` is the HTTP handler for `GET /events`. It registers a buffered channel, streams messages to the client, and deregisters on disconnect.

The hub lives in process memory. A server restart clears all subscribers — browsers reconnect automatically via `EventSource` and reload the current state from `GET /snaps`.

## SSE event reference

All event payloads are JSON objects.

### `snap` — new snap created

Fired after `POST /snaps` succeeds.

```
event: snap
data: {
  "id": 7,
  "name": "",
  "latitude": 52.5200,
  "longitude": 13.4050,
  "created_at": "2024-01-01T12:00:00Z",
  "photos": [
    { "id": 12, "snap_id": 7, "url": "/photos/<token>", "created_at": "..." }
  ]
}
```

Frontend action: adds the pin to the map and shows a toast.

---

### `snap_updated` — snap renamed

Fired after `PATCH /snaps/{id}` succeeds.

```
event: snap_updated
data: { "id": 7, "name": "Front door" }
```

Frontend action: updates the existing marker's popup title in place and shows a toast.

---

### `snap_deleted` — snap deleted

Fired after `DELETE /snaps/{id}` succeeds.

```
event: snap_deleted
data: { "id": 7 }
```

Frontend action: removes the marker from the map and shows a red toast. The feed page also drops any cards belonging to that snap.

---

### `photo` — standalone photo uploaded

Fired after `POST /photos` succeeds (one event per photo). Carries the photo, not a snap — these GPS-less uploads appear only in the feed, never on the map.

```
event: photo
data: { "id": 12, "url": "/photos/<token>", "created_at": "2024-01-01T12:00:00Z" }
```

Frontend action (feed page): prepends a new card.

---

### `photo_deleted` — photo deleted

Fired after `DELETE /photos/{token}` succeeds. If the deleted photo was a snap's last one, a `snap_deleted` event fires as well.

```
event: photo_deleted
data: { "id": 12 }
```

Frontend action (feed page): removes the card and shows a red toast.

---

## Frontend integration

The browser connects on page load via the native `EventSource` API — no library needed:

```js
const es = new EventSource('/events');

es.addEventListener('snap', e => {
  const snap = JSON.parse(e.data);
  addSnapMarker(snap);
  showToast(`New snap: ${snapLabel(snap)}`);
});

es.addEventListener('snap_updated', e => {
  const { id, name } = JSON.parse(e.data);
  updateSnapMarker(id, name);
  showToast(`Renamed: ${name || 'Snap #' + id}`, 'info');
});

es.addEventListener('snap_deleted', e => {
  const { id } = JSON.parse(e.data);
  removeSnapMarker(id);
  showToast(`Snap #${id} deleted`, 'danger');
});
```

`EventSource` reconnects automatically if the server restarts or the network hiccups. The browser sends a `Last-Event-ID` header on reconnect (the server currently ignores it — all state is re-fetched from `GET /snaps` on page load).

## Event type constants

Event type strings are defined as Go constants in `internal/hub/hub.go` to avoid magic strings across the codebase:

```go
const (
    EventSnapCreated  = "snap"
    EventSnapUpdated  = "snap_updated"
    EventSnapDeleted  = "snap_deleted"
    EventPhotoCreated = "photo"
    EventPhotoDeleted = "photo_deleted"
)
```

All `hub.Broadcast(...)` calls in the handlers reference these constants.
