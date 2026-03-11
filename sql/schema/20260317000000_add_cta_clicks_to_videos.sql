-- +goose Up
ALTER TABLE videos ADD COLUMN cta_clicks INTEGER DEFAULT 0;


-- +goose Down
ALTER TABLE videos DROP COLUMN cta_clicks;