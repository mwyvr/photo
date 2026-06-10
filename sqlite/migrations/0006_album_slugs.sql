-- Migration: 0006_album_slugs.sql
-- Adds immutable URL slug to albums.
-- Existing albums get a slug derived from their name in Go at startup
-- (SQLite string functions are too limited for reliable slug generation).
-- The unique index is added after the Go migration populates the column.

ALTER TABLE albums ADD COLUMN slug TEXT NOT NULL DEFAULT '';
