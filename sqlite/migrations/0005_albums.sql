-- Migration: 0005_albums.sql
-- Adds albums and album_photos tables.

CREATE TABLE albums (
    id             TEXT     PRIMARY KEY,
    user_id        TEXT     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT     NOT NULL,
    description    TEXT     NOT NULL DEFAULT '',
    cover_photo_id TEXT     REFERENCES photos(id) ON DELETE SET NULL,
    created_at     DATETIME NOT NULL,
    updated_at     DATETIME NOT NULL
);

CREATE INDEX idx_albums_user_id ON albums (user_id);

-- album_photos is the join table. position is 1-based and determines
-- display order within the album. Gaps in position are allowed and
-- normalised lazily rather than on every change.
CREATE TABLE album_photos (
    album_id TEXT    NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    photo_id TEXT    NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (album_id, photo_id)
);

CREATE INDEX idx_album_photos_album_id ON album_photos (album_id, position);
CREATE INDEX idx_album_photos_photo_id ON album_photos (photo_id);
