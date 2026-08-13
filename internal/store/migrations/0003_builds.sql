-- Image builds started from the panel.
--
-- The row is the record of what was built and by whom; the log itself is
-- archived to a file under <data_dir>/builds/<id>.log rather than a column,
-- because a build log routinely runs to megabytes and SQLite would carry all
-- of it in every listing query.
CREATE TABLE builds (
    id           TEXT PRIMARY KEY,
    -- Who started it. Kept as text alongside the id so the history still reads
    -- correctly after the account is deleted.
    user_id      TEXT NOT NULL DEFAULT '',
    username     TEXT NOT NULL DEFAULT '',
    -- The context directory and the Dockerfile inside it.
    context_dir  TEXT NOT NULL,
    dockerfile   TEXT NOT NULL DEFAULT 'Dockerfile',
    -- Comma-separated tags, as the operator entered them.
    tags         TEXT NOT NULL DEFAULT '',
    target       TEXT NOT NULL DEFAULT '',
    platform     TEXT NOT NULL DEFAULT '',
    no_cache     INTEGER NOT NULL DEFAULT 0,
    pull         INTEGER NOT NULL DEFAULT 0,
    status       TEXT NOT NULL CHECK (status IN ('running', 'success', 'failed', 'canceled')),
    -- The image the build produced, empty unless it succeeded.
    image_id     TEXT NOT NULL DEFAULT '',
    -- The engine's own explanation when it did not.
    error        TEXT NOT NULL DEFAULT '',
    -- Context size, so an operator can see why a build was slow to start.
    context_files INTEGER NOT NULL DEFAULT 0,
    context_bytes INTEGER NOT NULL DEFAULT 0,
    started_at   TEXT NOT NULL,
    finished_at  TEXT,
    -- Whether the archived log file still exists; retention removes the file
    -- long before the row.
    log_archived INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_builds_started ON builds(started_at DESC);
CREATE INDEX idx_builds_status ON builds(status);
CREATE INDEX idx_builds_user ON builds(user_id);
