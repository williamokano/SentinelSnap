-- Per-photo view counter, bumped each time a photo is opened. Starts at 0.
ALTER TABLE photos ADD COLUMN IF NOT EXISTS view_count BIGINT NOT NULL DEFAULT 0;
