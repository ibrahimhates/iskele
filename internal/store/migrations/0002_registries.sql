-- Private registry credentials, used when pulling an image the public
-- registries do not serve.
--
-- The password column holds AES-256-GCM ciphertext produced by
-- internal/crypto.SecretBox, keyed by the master key in secret_key_file. It
-- never leaves the daemon in plaintext: the API returns the username and the
-- server address, never the password, and there is no endpoint that reads it
-- back. A database file that leaks without the key file is not enough to pull
-- from a private registry.
CREATE TABLE registries (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    -- The registry host, e.g. "ghcr.io" or "registry.example.com:5000".
    -- Normalized on write so a lookup by image reference can match it.
    server     TEXT NOT NULL,
    username   TEXT NOT NULL DEFAULT '',
    -- Encrypted; empty for an anonymous entry.
    password   TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    -- The last time a pull used this credential, so an operator can tell which
    -- entries still matter.
    last_used_at TEXT
);

-- One credential per server: two entries for the same host would make which
-- one authenticates a pull a matter of row order.
CREATE UNIQUE INDEX idx_registries_server ON registries(server);
CREATE UNIQUE INDEX idx_registries_name ON registries(name);
