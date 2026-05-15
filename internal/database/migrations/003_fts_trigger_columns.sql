-- +goose Up
-- +goose StatementBegin

-- Scope items_au to fire only when FTS-indexed columns change.
--
-- The original trigger (in 001_initial.sql) fires on every UPDATE, even
-- on read/starred toggles where title/content/description don't change,
-- generating unnecessary FTS5 segment churn. Scoping with OF eliminates
-- the wasted work without changing semantics for content updates.
--
-- DROP + CREATE happen inside the goose-managed transaction, so the
-- trigger is always present from the application's perspective.
DROP TRIGGER IF EXISTS items_au;

CREATE TRIGGER items_au
AFTER UPDATE OF title, content, description ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, content, description)
    VALUES ('delete', old.id, old.title, old.content, old.description);
    INSERT INTO items_fts(rowid, title, content, description)
    VALUES (new.id, new.title, new.content, new.description);
END;

-- +goose StatementEnd

-- +goose Down
-- Up-only migration; rollback via backup restore.
