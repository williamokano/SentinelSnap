-- Stored thumbnail key for a photo. NULL means no thumbnail has been generated
-- yet; it is filled lazily the first time a photo's thumbnail is requested.
-- The original is always served from stored_key.
ALTER TABLE photos ADD COLUMN IF NOT EXISTS thumb_key TEXT;
