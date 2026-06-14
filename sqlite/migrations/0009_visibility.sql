-- Migration: 0009_visibility.sql
-- Replaces the boolean published column on photos and albums with a
-- three-state visibility enum: private | household | published.
-- Adds share_token (nullable, unique) to both tables for link-based sharing.
--
-- Existing data mapping:
--   published = 1  →  visibility = 'published'
--   published = 0  →  visibility = 'household'  (household is the new default)
--
-- SQLite cannot rename a column with a UNIQUE or INDEX constraint easily,
-- so both tables are recreated using the 12-step table-recreation pattern.

PRAGMA foreign_keys=OFF;

-- Photos ---------------------------------------------------------------

CREATE TABLE photos_new (
    id             TEXT     PRIMARY KEY,
    user_id        TEXT     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename       TEXT     NOT NULL,
    stored_path    TEXT     NOT NULL UNIQUE,
    sha256         TEXT     NOT NULL UNIQUE,
    file_type      TEXT     NOT NULL DEFAULT '',
    mime_type      TEXT     NOT NULL DEFAULT '',
    file_size_bytes INTEGER NOT NULL DEFAULT 0,
    camera_make    TEXT     NOT NULL DEFAULT '',
    camera_model   TEXT     NOT NULL DEFAULT '',
    lens_model     TEXT     NOT NULL DEFAULT '',
    focal_length   TEXT     NOT NULL DEFAULT '',
    aperture       TEXT     NOT NULL DEFAULT '',
    shutter_speed  TEXT     NOT NULL DEFAULT '',
    iso            INTEGER,
    gps_lat        REAL,
    gps_lon        REAL,
    captured_at    DATETIME,
    location_name  TEXT     NOT NULL DEFAULT '',
    exif_raw       TEXT     NOT NULL DEFAULT '',
    description    TEXT     NOT NULL DEFAULT '',
    is_raw         INTEGER  NOT NULL DEFAULT 0,
    raw_partner_id TEXT,
    visibility     TEXT     NOT NULL DEFAULT 'household',
    share_token    TEXT     UNIQUE,
    thumb_path     TEXT,
    created_at     DATETIME NOT NULL,
    updated_at     DATETIME NOT NULL
);

INSERT INTO photos_new
    SELECT
        id, user_id, filename, stored_path, sha256,
        file_type, mime_type, file_size_bytes,
        camera_make, camera_model, lens_model, focal_length,
        aperture, shutter_speed, iso,
        gps_lat, gps_lon, captured_at, location_name,
        exif_raw, description, is_raw,
        raw_partner_id,
        CASE WHEN published = 1 THEN 'published' ELSE 'household' END,
        NULL,   -- share_token: none on migration
        thumb_path, created_at, updated_at
    FROM photos;

DROP TABLE photos;
ALTER TABLE photos_new RENAME TO photos;

CREATE INDEX idx_photos_user_id    ON photos(user_id);
CREATE INDEX idx_photos_captured_at ON photos(captured_at);
CREATE INDEX idx_photos_visibility  ON photos(visibility);
CREATE INDEX idx_photos_share_token ON photos(share_token) WHERE share_token IS NOT NULL;

-- Albums ---------------------------------------------------------------

CREATE TABLE albums_new (
    id             TEXT     PRIMARY KEY,
    user_id        TEXT     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT     NOT NULL,
    slug           TEXT     NOT NULL UNIQUE,
    description    TEXT     NOT NULL DEFAULT '',
    cover_photo_id TEXT     REFERENCES photos(id) ON DELETE SET NULL,
    visibility     TEXT     NOT NULL DEFAULT 'household',
    share_token    TEXT     UNIQUE,
    created_at     DATETIME NOT NULL,
    updated_at     DATETIME NOT NULL
);

INSERT INTO albums_new
    SELECT
        id, user_id, name, slug, description, cover_photo_id,
        'household',  -- default all existing albums to household
        NULL,         -- share_token: none on migration
        created_at, updated_at
    FROM albums;

DROP TABLE albums;
ALTER TABLE albums_new RENAME TO albums;

CREATE INDEX idx_albums_user_id    ON albums(user_id);
CREATE INDEX idx_albums_share_token ON albums(share_token) WHERE share_token IS NOT NULL;

PRAGMA foreign_keys=ON;
