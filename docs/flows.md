# Request flows

End-to-end walkthrough of every major operation in SentinelSnap.

## Posting a snap (phone → server → all browsers)

```
Phone
  1. Takes photos, gets GPS coordinates
  2. Base64-encodes each photo
  3. POST /snaps  { latitude, longitude, photos: ["<b64>", ...] }

Server — handler.CreateSnap
  4. Validates request (lat/lng present, at least one photo)
     - Accepts latitude/longitude as both number and string
       (iOS Shortcuts sends coordinates as strings)
  5. INSERT INTO snaps (latitude, longitude) → snapID
  6. For each photo:
     a. base64-decode
     b. detect content type (JPEG/PNG/GIF/WebP)
     c. generate random token (32-byte hex, used for serving)
     d. generate random filename key: photos/<token><ext>
     e. write file to disk (LocalStorage.Put)
     f. INSERT INTO photos (snap_id, stored_key, token) → photoID
     g. build Photo{URL: "/photos/<token>"}
  7. If any step 6 fails → rollback (delete stored files + delete snap row)
  8. hub.Broadcast(EventSnapCreated, snap)
  9. Respond 201 with full snap JSON

Hub
  10. Sends SSE "snap" event to every connected browser tab

Each Browser Tab
  11. EventSource receives "snap" event
  12. Adds Leaflet marker at (lat, lng) with popup
  13. Shows toast: "New snap received: <name>"
```

## Serving a photo

```
Browser
  1. GET /photos/<token>

Server — handler.ServePhoto
  2. SELECT stored_key FROM photos WHERE token = $1
     - token is a random 32-byte hex string, not guessable
  3. Open file at stored_key path on disk
  4. Detect content type from file extension
  5. Stream file bytes with correct Content-Type header
```

Photo URLs (`/photos/<token>`) are the only public reference to a file. The underlying path on disk (`photos/<token><ext>`) is never exposed in any API response. Sequential IDs and directory listing are both absent.

## Renaming a snap

```
Browser
  1. User clicks the snap name in the popup
  2. Inline input appears, user types new name (max 100 chars), presses Enter or clicks away
  3. PATCH /snaps/{id}  { "name": "Front door" }

Server — handler.UpdateSnap
  4. Validate: name ≤ 100 characters
  5. UPDATE snaps SET name = $2 WHERE id = $1
     - Returns ErrNotFound if no rows affected
  6. hub.Broadcast(EventSnapUpdated, {id, name})
  7. Respond 200 { id, name }

Hub
  8. Sends SSE "snap_updated" event to every connected browser tab

Each Browser Tab
  9. EventSource receives "snap_updated" event
  10. Calls marker.setPopupContent(buildPopup(updatedSnap))
  11. Shows blue toast: "Snap renamed: Front door"
```

Empty string clears the custom name; the popup falls back to "Snap #N".

## Deleting a snap

```
Browser
  1. User clicks "Delete" in the popup
  2. Confirmation dialog: "Delete '<name>'? This cannot be undone."
  3. DELETE /snaps/{id}

Server — handler.DeleteSnap
  4. SELECT snap + photos WHERE id = $1 (returns 404 if not found)
  5. For each photo: delete file from disk (best-effort, logs warning on failure)
  6. DELETE FROM snaps WHERE id = $1 (cascades to photos via FK)
  7. hub.Broadcast(EventSnapDeleted, {id})
  8. Respond 204 No Content

Hub
  9. Sends SSE "snap_deleted" event to every connected browser tab

Each Browser Tab
  10. EventSource receives "snap_deleted" event
  11. Removes Leaflet marker from cluster group
  12. Shows red toast: "Snap #N deleted"
```

Note: the browser that triggered the delete does not remove the marker directly. It waits for its own SSE event, same as every other tab. This ensures all views stay consistent.

## Database schema

```sql
snaps
  id          BIGSERIAL PRIMARY KEY
  name        VARCHAR(100)          -- null means no custom name
  latitude    DOUBLE PRECISION NOT NULL
  longitude   DOUBLE PRECISION NOT NULL
  created_at  TIMESTAMPTZ DEFAULT now()

photos
  id          BIGSERIAL PRIMARY KEY
  snap_id     BIGINT REFERENCES snaps(id) ON DELETE CASCADE
  stored_key  TEXT NOT NULL         -- disk path, never exposed in API
  token       TEXT NOT NULL UNIQUE  -- random 32-byte hex, used in URLs
  created_at  TIMESTAMPTZ DEFAULT now()
```

Migrations run automatically at startup from embedded SQL files in `internal/migrate/`.

## Storage layout

Files are stored flat under `LOCAL_UPLOAD_DIR`:

```
uploads/
  photos/
    3f8a1c...e9.jpg   ← token is the filename, no snap ID in path
    b2d04f...12.png
```

The snap ID does not appear in the path. Knowing one token gives access to exactly one photo and reveals nothing about other snaps.
