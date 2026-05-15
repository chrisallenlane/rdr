-- +goose Up
-- +goose StatementBegin

-- idx_feeds_list_id covers the sidebar unread-count subqueries and
-- list-detail filters that filter feeds by list_id (without this,
-- those queries scan the per-user feed slice and post-filter on
-- list_id).
CREATE INDEX IF NOT EXISTS idx_feeds_list_id
    ON feeds(list_id);

-- idx_items_feed_published_at is a composite seek-and-range index
-- covering both the main item-listing query (ORDER BY published_at
-- DESC per feed) and adjacentItemID prev/next navigation. The DESC
-- direction matches the listing queries' ORDER BY clause so the
-- planner can use the index without an explicit sort step.
CREATE INDEX IF NOT EXISTS idx_items_feed_published_at
    ON items(feed_id, published_at DESC);

-- +goose StatementEnd

-- +goose Down
-- Up-only migration; rollback via backup restore.
