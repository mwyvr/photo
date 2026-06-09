-- Migration: 0004_published_and_thumb.sql
-- Adds public visibility flag and thumbnail cache path to photos.

ALTER TABLE photos ADD COLUMN published  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE photos ADD COLUMN thumb_path TEXT;

CREATE INDEX idx_photos_published ON photos (published);
