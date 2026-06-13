-- Migration: 0007_users_admin_invites.sql
-- Adds admin flag to users and an invites table for invite-based registration.

ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;

CREATE TABLE invites (
    id          TEXT PRIMARY KEY,
    token       TEXT NOT NULL UNIQUE,
    created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    used_at     TEXT,
    used_by     TEXT REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX idx_invites_token ON invites(token);
