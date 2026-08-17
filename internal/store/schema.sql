CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    vedrow_sub TEXT NOT NULL UNIQUE,
    username   TEXT NOT NULL,
    email      TEXT NOT NULL DEFAULT '',
    avatar_url TEXT NOT NULL DEFAULT '',
    is_admin   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS login_states (
    state      TEXT PRIMARY KEY,
    verifier   TEXT NOT NULL,
    next       TEXT NOT NULL DEFAULT '',
    expires_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS modpacks (
    slug        TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS versions (
    modpack_slug TEXT NOT NULL REFERENCES modpacks (slug) ON DELETE CASCADE,
    number       INTEGER NOT NULL,
    notes        TEXT NOT NULL DEFAULT '',
    file_count   INTEGER NOT NULL,
    total_bytes  INTEGER NOT NULL,
    fingerprint  TEXT NOT NULL,
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (modpack_slug, number)
);

CREATE TABLE IF NOT EXISTS version_files (
    modpack_slug TEXT NOT NULL,
    number       INTEGER NOT NULL,
    path         TEXT NOT NULL,
    grp          TEXT NOT NULL DEFAULT '',
    size         INTEGER NOT NULL,
    sha256       TEXT NOT NULL,
    optional     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (modpack_slug, number, path),
    FOREIGN KEY (modpack_slug, number)
        REFERENCES versions (modpack_slug, number) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS launcher_builds (
    version    TEXT PRIMARY KEY,
    filename   TEXT NOT NULL,
    size       INTEGER NOT NULL,
    sha256     TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
