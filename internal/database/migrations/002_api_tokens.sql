-- +goose Up
-- +goose StatementBegin

CREATE TABLE api_tokens (
    id            INTEGER  PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT     NOT NULL,
    token_hash    TEXT     NOT NULL UNIQUE,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at  DATETIME,
    expires_at    DATETIME
);
CREATE INDEX idx_api_tokens_user_id    ON api_tokens(user_id);
CREATE INDEX idx_api_tokens_token_hash ON api_tokens(token_hash);

-- +goose StatementEnd

-- +goose Down
-- Up-only migration; rollback via backup restore.
