-- Initial Murmur schema.
--
-- The vault index (vault_files and its children) is a cache: it can be dropped
-- and rebuilt at any time with `murmur index --rebuild`. The capture, routing
-- and undo tables hold user history and are never rebuilt.

CREATE TABLE vault_files (
    id         INTEGER PRIMARY KEY,
    rel_path   TEXT    NOT NULL UNIQUE,
    file_name  TEXT    NOT NULL,
    title      TEXT    NOT NULL DEFAULT '',
    excerpt    TEXT    NOT NULL DEFAULT '',
    mod_time   INTEGER NOT NULL DEFAULT 0,
    size       INTEGER NOT NULL DEFAULT 0,
    indexed_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE headings (
    id      INTEGER PRIMARY KEY,
    file_id INTEGER NOT NULL REFERENCES vault_files(id) ON DELETE CASCADE,
    level   INTEGER NOT NULL,
    text    TEXT    NOT NULL,
    line    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_headings_file ON headings(file_id);

CREATE TABLE tags (
    id      INTEGER PRIMARY KEY,
    file_id INTEGER NOT NULL REFERENCES vault_files(id) ON DELETE CASCADE,
    tag     TEXT    NOT NULL
);
CREATE INDEX idx_tags_file ON tags(file_id);
CREATE INDEX idx_tags_tag ON tags(tag);

CREATE TABLE aliases (
    id      INTEGER PRIMARY KEY,
    file_id INTEGER NOT NULL REFERENCES vault_files(id) ON DELETE CASCADE,
    alias   TEXT    NOT NULL
);
CREATE INDEX idx_aliases_file ON aliases(file_id);

CREATE TABLE links (
    id      INTEGER PRIMARY KEY,
    file_id INTEGER NOT NULL REFERENCES vault_files(id) ON DELETE CASCADE,
    target  TEXT    NOT NULL
);
CREATE INDEX idx_links_file ON links(file_id);

CREATE TABLE captures (
    id           INTEGER PRIMARY KEY,
    created_at   INTEGER NOT NULL,
    raw          TEXT    NOT NULL,
    markdown     TEXT    NOT NULL DEFAULT '',
    note_path    TEXT    NOT NULL DEFAULT '',
    section      TEXT    NOT NULL DEFAULT '',
    content_type TEXT    NOT NULL DEFAULT '',
    tags         TEXT    NOT NULL DEFAULT '',
    confidence   REAL    NOT NULL DEFAULT 0,
    source       TEXT    NOT NULL DEFAULT '',
    corrected    INTEGER NOT NULL DEFAULT 0,
    undone       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_captures_created ON captures(created_at DESC);

CREATE TABLE routing_candidates (
    id         INTEGER PRIMARY KEY,
    capture_id INTEGER NOT NULL REFERENCES captures(id) ON DELETE CASCADE,
    note_path  TEXT    NOT NULL,
    score      REAL    NOT NULL DEFAULT 0,
    rank       INTEGER NOT NULL DEFAULT 0,
    reason     TEXT    NOT NULL DEFAULT '',
    chosen     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_candidates_capture ON routing_candidates(capture_id);

-- Lightweight learning signal: a weighted token -> destination table. There is
-- no model here, just counts that nudge ranking towards past choices.
CREATE TABLE routing_corrections (
    id           INTEGER PRIMARY KEY,
    token        TEXT    NOT NULL,
    note_path    TEXT    NOT NULL,
    content_type TEXT    NOT NULL DEFAULT '',
    section      TEXT    NOT NULL DEFAULT '',
    weight       REAL    NOT NULL DEFAULT 0,
    hits         INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL DEFAULT 0,
    UNIQUE(token, note_path)
);
CREATE INDEX idx_corrections_token ON routing_corrections(token);

CREATE TABLE write_transactions (
    id           INTEGER PRIMARY KEY,
    capture_id   INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    path         TEXT    NOT NULL,
    hash_before  TEXT    NOT NULL DEFAULT '',
    hash_after   TEXT    NOT NULL DEFAULT '',
    inserted     TEXT    NOT NULL DEFAULT '',
    section      TEXT    NOT NULL DEFAULT '',
    mode         TEXT    NOT NULL DEFAULT '',
    created_file INTEGER NOT NULL DEFAULT 0,
    backup       TEXT    NOT NULL DEFAULT '',
    undone       INTEGER NOT NULL DEFAULT 0,
    undone_at    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_write_created ON write_transactions(created_at DESC);

CREATE TABLE settings_metadata (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT 0
);
