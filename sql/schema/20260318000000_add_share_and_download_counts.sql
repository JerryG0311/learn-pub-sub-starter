-- +goose Up
ALTER TABLE videos ADD COLUMN share_count INTEGER DEFAULT 0;
ALTER TABLE videos ADD COLUMN download_count INTEGER DEFAULT 0;


-- +goose Down
ALTER TABLE videos DROP COLUMN share_count;
ALTER TABLE videos DROP COLUMN download_count;
