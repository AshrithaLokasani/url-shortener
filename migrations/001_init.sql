-- +goose Up
CREATE TABLE IF NOT EXISTS links (
    code              TEXT PRIMARY KEY,
    original_url      TEXT        NOT NULL,
    owner_token_hash  BYTEA       NOT NULL,
    hit_count         BIGINT      NOT NULL DEFAULT 0,
    active            BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NULL
);

-- +goose Down
DROP TABLE IF EXISTS links;
