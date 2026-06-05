CREATE TABLE IF NOT EXISTS account_totp (
    account_id   BIGINT PRIMARY KEY,
    secret       TEXT NOT NULL,
    confirmed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS account_totp_backup_codes (
    id         BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL,
    code_hash  TEXT NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now()
);
