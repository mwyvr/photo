-- Migration: 0003_photo_file_type.sql
-- Adds file_type column which was added to the codebase after 0002 had
-- already been applied to existing databases.

ALTER TABLE photos ADD COLUMN file_type TEXT NOT NULL DEFAULT '';
