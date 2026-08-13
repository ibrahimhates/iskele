-- Compose stacks.
--
-- The compose file and the .env live in columns rather than on disk: a stack
-- written in the editor has no file anywhere, and keeping the two sources in
-- different places would mean an operator editing one and deploying the other.
-- A file- or git-backed stack still keeps its content here — that is the copy
-- that was deployed, which is the copy worth having when the working tree has
-- since moved on.
CREATE TABLE stacks (
    id              TEXT PRIMARY KEY,
    -- The name is what labels every container the stack creates, so it has to
    -- be unique on this host and cannot change without orphaning them.
    name            TEXT NOT NULL UNIQUE,
    -- Where the content comes from: typed into the editor, read from a file on
    -- this host, or cloned from a git repository.
    source          TEXT NOT NULL CHECK (source IN ('editor', 'file', 'git')),
    -- For source=file: the compose file's path, inside allowed_paths.
    path            TEXT NOT NULL DEFAULT '',
    -- For source=git: where it came from and which ref was checked out.
    git_url         TEXT NOT NULL DEFAULT '',
    git_ref         TEXT NOT NULL DEFAULT '',
    -- The commit the working copy is at, so an operator can tell whether a
    -- pull would change anything.
    git_commit      TEXT NOT NULL DEFAULT '',
    compose_content TEXT NOT NULL DEFAULT '',
    env_content     TEXT NOT NULL DEFAULT '',
    -- Where the stack's working copy lives, for relative paths and git.
    working_dir     TEXT NOT NULL DEFAULT '',
    -- What the last deploy did, not what the containers are doing now: the
    -- engine is the authority on that and is asked directly.
    status          TEXT NOT NULL DEFAULT 'created'
                    CHECK (status IN ('created', 'deploying', 'deployed', 'failed', 'stopped')),
    last_error      TEXT NOT NULL DEFAULT '',
    last_deployed_at TEXT,
    -- Who created it. Kept as text alongside the id so the record still reads
    -- correctly after the account is deleted.
    created_by      TEXT NOT NULL DEFAULT '',
    created_by_id   TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX idx_stacks_updated ON stacks(updated_at DESC);
CREATE INDEX idx_stacks_status ON stacks(status);
