-- Add description column to items.
ALTER TABLE items ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- Recreate FTS5 table to include description.
-- FTS5 virtual tables cannot be ALTERed; drop and rebuild.
DROP TRIGGER items_ai;
DROP TRIGGER items_ad;
DROP TRIGGER items_au;
DROP TABLE items_fts;

CREATE VIRTUAL TABLE items_fts USING fts5(
    title,
    content,
    description,
    content=items,
    content_rowid=id
);

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

INSERT INTO items_fts(items_fts) VALUES('rebuild');

-- Per-user settings table.
CREATE TABLE user_settings (
    user_id           INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    show_descriptions INTEGER NOT NULL DEFAULT 1
);
