-- +goose Up
CREATE TABLE IF NOT EXISTS click_events (
    id          BIGSERIAL PRIMARY KEY,
    code        TEXT        NOT NULL REFERENCES links(code) ON DELETE CASCADE,
    clicked_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    referrer    TEXT        NOT NULL DEFAULT '',
    user_agent  TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS click_events_code_clicked_at_idx
    ON click_events (code, clicked_at DESC);

-- +goose Down
DROP TABLE IF EXISTS click_events;
