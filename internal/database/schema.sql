-- users
CREATE TABLE users (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    username   TEXT     NOT NULL UNIQUE,
    password   TEXT     NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- sessions
CREATE TABLE sessions (
    id         TEXT     PRIMARY KEY,
    user_id    INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_sessions_user_id  ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

-- lists
CREATE TABLE lists (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT     NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, name)
);
CREATE INDEX idx_lists_user_id ON lists(user_id);

-- feeds
CREATE TABLE feeds (
    id                   INTEGER  PRIMARY KEY AUTOINCREMENT,
    user_id              INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    list_id              INTEGER  REFERENCES lists(id) ON DELETE SET NULL,
    url                  TEXT     NOT NULL,
    title                TEXT     NOT NULL DEFAULT '',
    site_url             TEXT     NOT NULL DEFAULT '',
    favicon_url          TEXT     NOT NULL DEFAULT '',
    last_fetched_at      DATETIME,
    last_fetch_error     TEXT     NOT NULL DEFAULT '',
    consecutive_failures INTEGER  NOT NULL DEFAULT 0,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, url)
);
CREATE INDEX idx_feeds_user_id ON feeds(user_id);

-- items
CREATE TABLE items (
    id           INTEGER  PRIMARY KEY AUTOINCREMENT,
    feed_id      INTEGER  NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    guid         TEXT     NOT NULL,
    title        TEXT     NOT NULL DEFAULT '',
    content      TEXT     NOT NULL DEFAULT '',
    description  TEXT     NOT NULL DEFAULT '',
    url          TEXT     NOT NULL DEFAULT '',
    published_at DATETIME,
    read         INTEGER  NOT NULL DEFAULT 0,
    read_at      DATETIME,
    starred      INTEGER  NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(feed_id, guid)
);
CREATE INDEX idx_items_feed_id      ON items(feed_id);
CREATE INDEX idx_items_published_at ON items(published_at);
CREATE INDEX idx_items_read         ON items(read);

-- full-text search (FTS5)
CREATE VIRTUAL TABLE items_fts USING fts5(
    title,
    content,
    description,
    content=items,
    content_rowid=id
);

-- triggers to keep FTS in sync
CREATE TRIGGER items_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, content, description)
    VALUES (new.id, new.title, new.content, new.description);
END;

CREATE TRIGGER items_ad AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, content, description)
    VALUES ('delete', old.id, old.title, old.content, old.description);
END;

CREATE TRIGGER items_au AFTER UPDATE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, content, description)
    VALUES ('delete', old.id, old.title, old.content, old.description);
    INSERT INTO items_fts(rowid, title, content, description)
    VALUES (new.id, new.title, new.content, new.description);
END;

-- per-user settings
CREATE TABLE user_settings (
    user_id           INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    show_descriptions INTEGER NOT NULL DEFAULT 1,
    date_display      INTEGER NOT NULL DEFAULT 0
);
