-- Migration: 0008_user_names.sql
-- Username is now required to be an email address (validated in Go), so the
-- separate email column is redundant. Adds first/last name for display.
--
-- SQLite cannot DROP COLUMN a column with a UNIQUE constraint directly, so
-- this recreates the table following SQLite's documented procedure for
-- schema changes: https://www.sqlite.org/lang_altertable.html

PRAGMA foreign_keys=OFF;

CREATE TABLE users_new (
    id            TEXT     PRIMARY KEY,
    username      TEXT     NOT NULL UNIQUE COLLATE NOCASE,
    first_name    TEXT     NOT NULL DEFAULT '',
    last_name     TEXT     NOT NULL DEFAULT '',
    password_hash TEXT     NOT NULL,
    is_admin      INTEGER  NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

INSERT INTO users_new (id, username, first_name, last_name, password_hash, is_admin, created_at, updated_at)
SELECT id, username, '', '', password_hash, is_admin, created_at, updated_at
FROM users;

DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

PRAGMA foreign_keys=ON;
