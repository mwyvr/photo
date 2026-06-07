-- Migration: 0001_initial_schema.sql
-- All primary keys are TEXT storing 16-character kid IDs.

CREATE TABLE users (
    id            TEXT     PRIMARY KEY,
    username      TEXT     NOT NULL UNIQUE COLLATE NOCASE,
    email         TEXT     NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT     NOT NULL,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

CREATE TABLE sessions (
    id         TEXT     PRIMARY KEY,
    user_id    TEXT     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT     NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE INDEX idx_sessions_user_id    ON sessions (user_id);
CREATE INDEX idx_sessions_token_hash ON sessions (token_hash);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);

CREATE TABLE photos (
    id              TEXT     PRIMARY KEY,
    user_id         TEXT     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename        TEXT     NOT NULL,
    stored_path     TEXT     NOT NULL UNIQUE,
    sha256          TEXT     NOT NULL UNIQUE,
    mime_type       TEXT     NOT NULL,
    file_size_bytes INTEGER  NOT NULL DEFAULT 0,

    -- Flat EXIF fields for indexed querying.
    camera_make     TEXT,
    camera_model    TEXT,
    lens_model      TEXT,
    focal_length    TEXT,
    aperture        TEXT,
    shutter_speed   TEXT,
    iso             INTEGER,
    gps_lat         REAL,
    gps_lon         REAL,
    captured_at     DATETIME,

    -- Denormalised reverse-geocoded place name, e.g. "Tokyo, Japan".
    -- Populated at import if GPS coords are present; empty otherwise.
    location_name   TEXT     NOT NULL DEFAULT '',

    -- Full exiftool JSON output blob.
    exif_raw        TEXT,

    description     TEXT     NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);

CREATE INDEX idx_photos_user_id      ON photos (user_id);
CREATE INDEX idx_photos_captured_at  ON photos (captured_at);
CREATE INDEX idx_photos_camera_model ON photos (camera_model);
CREATE INDEX idx_photos_sha256       ON photos (sha256);
CREATE INDEX idx_photos_location     ON photos (location_name);

CREATE TABLE tags (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE  -- always lowercase
);

CREATE TABLE photo_tags (
    photo_id TEXT NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
    tag_id   TEXT NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
    PRIMARY KEY (photo_id, tag_id)
);

CREATE INDEX idx_photo_tags_photo_id ON photo_tags (photo_id);
CREATE INDEX idx_photo_tags_tag_id   ON photo_tags (tag_id);
