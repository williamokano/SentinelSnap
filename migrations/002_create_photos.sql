CREATE TABLE IF NOT EXISTS photos (
    id          BIGSERIAL PRIMARY KEY,
    snap_id     BIGINT NOT NULL REFERENCES snaps(id) ON DELETE CASCADE,
    url         TEXT NOT NULL,
    stored_key  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_photos_snap_id ON photos(snap_id);
