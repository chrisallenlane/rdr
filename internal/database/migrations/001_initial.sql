-- users
CREATE TABLE users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    username   TEXT    NOT NULL UNIQUE,
    password   TEXT    NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- sessions
CREATE TABLE sessions (
    id         TEXT     PRIMARY KEY,
    user_id    INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

-- feeds
CREATE TABLE feeds (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    url             TEXT    NOT NULL,
    title           TEXT    NOT NULL DEFAULT '',
    site_url        TEXT    NOT NULL DEFAULT '',
    favicon_url     TEXT    NOT NULL DEFAULT '',
    last_fetched_at      DATETIME,
    last_fetch_error     TEXT    NOT NULL DEFAULT '',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, url)
);
CREATE INDEX idx_feeds_user_id ON feeds(user_id);

-- items
CREATE TABLE items (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id      INTEGER  NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    guid         TEXT     NOT NULL,
    title        TEXT     NOT NULL DEFAULT '',
    content      TEXT     NOT NULL DEFAULT '',
    url          TEXT     NOT NULL DEFAULT '',
    published_at DATETIME,
    read         INTEGER  NOT NULL DEFAULT 0,
    read_at      DATETIME,
    starred      INTEGER  NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(feed_id, guid)
);
CREATE INDEX idx_items_feed_id ON items(feed_id);
CREATE INDEX idx_items_published_at ON items(published_at);
CREATE INDEX idx_items_read ON items(read);

-- lists
CREATE TABLE lists (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, name)
);
CREATE INDEX idx_lists_user_id ON lists(user_id);

-- list_feeds (join table)
CREATE TABLE list_feeds (
    list_id INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
    feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    PRIMARY KEY (list_id, feed_id)
);

-- full-text search (FTS5)
CREATE VIRTUAL TABLE items_fts USING fts5(
    title,
    content,
    content=items,
    content_rowid=id
);

-- triggers to keep FTS in sync
CREATE TRIGGER items_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
END;

CREATE TRIGGER items_ad AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, content) VALUES ('delete', old.id, old.title, old.content);
END;

CREATE TRIGGER items_au AFTER UPDATE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, content) VALUES ('delete', old.id, old.title, old.content);
    INSERT INTO items_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
END;

-- migrations tracking
CREATE TABLE schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
