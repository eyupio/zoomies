CREATE TABLE user_preferences (
    user_id     TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    preferences TEXT    NOT NULL DEFAULT '{}',
    updated_at  INTEGER NOT NULL
);
