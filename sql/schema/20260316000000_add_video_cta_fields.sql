-- +goose Up
ALTER TABLE videos ADD COLUMN cta_text TEXT;
ALTER TABLE videos ADD COLUMN cta_url TEXT;
ALTER TABLE videos ADD COLUMN cta_time_seconds INTEGER DEFAULT 0;


-- +goose Down
ALTER TABLE videos DROP COLUMN cta_text;
ALTER TABLE videos DROP COLUMN cta_url;
ALTER TABLE videos DROP COLUMN cta_time_seconds;