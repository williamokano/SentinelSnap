ALTER TABLE photos ADD COLUMN IF NOT EXISTS token TEXT;
UPDATE photos SET token = md5(random()::text || id::text) WHERE token IS NULL;
ALTER TABLE photos ALTER COLUMN token SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS photos_token_key ON photos (token);
