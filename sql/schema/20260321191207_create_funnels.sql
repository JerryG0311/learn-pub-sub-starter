-- +goose Up
CREATE TABLE funnels (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE funnel_steps (
    id TEXT PRIMARY KEY,
    funnel_id TEXT NOT NULL,
    step_type TEXT NOT NULL,
    video_id TEXT,
    position INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (funnel_id) REFERENCES funnels(id) ON DELETE CASCADE
);

CREATE INDEX idx_funnels_user_id ON funnels(user_id);
CREATE INDEX idx_funnel_steps_funnel_id ON funnel_steps(funnel_id);
CREATE INDEX idx_funnel_steps_funnel_position ON funnel_steps(funnel_id, position);


-- +goose Down
DROP INDEX IF EXISTS idx_funnel_steps_funnel_position;
DROP INDEX IF EXISTS idx_funnel_steps_funnel_id;
DROP INDEX IF EXISTS idx_funnels_user_id;

DROP TABLE IF EXISTS funnel_steps;
DROP TABLE IF EXISTS funnels;