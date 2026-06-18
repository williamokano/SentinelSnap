-- Photos may now exist without a snap (simple uploads have no GPS, so no snap).
ALTER TABLE photos ALTER COLUMN snap_id DROP NOT NULL;
