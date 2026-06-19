-- Thumbnails generated before EXIF-orientation handling are rotated on disk
-- (the decoder ignored the camera's Orientation tag). Clear thumb_key so every
-- thumbnail is regenerated on next request; regeneration overwrites the old
-- file at the same deterministic key. Thumbnails are a regenerable cache, so
-- this is safe — at worst a photo's thumbnail is rebuilt once.
UPDATE photos SET thumb_key = NULL;
