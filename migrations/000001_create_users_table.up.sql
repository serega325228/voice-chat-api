CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users
(
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    password_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);

CREATE TABLE IF NOT EXISTS tokens
(
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id UUID NOT NULL,
    user_id UUID NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT fk_tokens_user
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE
        ON UPDATE NO ACTION
);

CREATE INDEX IF NOT EXISTS idx_tokens_family_id ON tokens (family_id);
CREATE INDEX IF NOT EXISTS idx_tokens_token_hash ON tokens (token_hash);
CREATE INDEX IF NOT EXISTS idx_tokens_user_id ON tokens (user_id);

CREATE INDEX IF NOT EXISTS idx_tokens_family_active
ON tokens (family_id)
WHERE status = 'active';
