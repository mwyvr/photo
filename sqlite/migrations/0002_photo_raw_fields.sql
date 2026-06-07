-- Migration: 0002_photo_raw_fields.sql
-- Adds RAW file identification and a placeholder for RAW/JPEG partner linking.

ALTER TABLE photos ADD COLUMN is_raw        INTEGER NOT NULL DEFAULT 0;
ALTER TABLE photos ADD COLUMN raw_partner_id TEXT REFERENCES photos(id);

CREATE INDEX idx_photos_is_raw ON photos (is_raw);
