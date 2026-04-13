CREATE TABLE IF NOT EXISTS users
(
    id UUID PRIMARY KEY DEFAULT get_random_uuid(),
    username TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    password_hash BYTEA NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);

CREATE TABLE IF NOT EXISTS tokens
(
    id UUID PRIMARY KEY DEFAULT get_random_uuid(),
    token_hash BYTEA NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'active',
    user_id UUID NOT NULL,
    expires_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL

    CONSTRAINT fk_tokens_user
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE
        ON UPDATE NO ACTION
);

CREATE INDEX IF NOT EXISTS idx_tokens_token_hash ON tokens (token_hash);
CREATE INDEX IF NOT EXISTS idx_tokens_user_id ON tokens (user_id);

