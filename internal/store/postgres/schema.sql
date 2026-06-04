CREATE TABLE IF NOT EXISTS soul_assignments (
    connector  TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    soul_name  TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (connector, user_id)
);

CREATE TABLE IF NOT EXISTS role_assignments (
    connector  TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    role_name  TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (connector, user_id)
);

CREATE TABLE IF NOT EXISTS allowlist (
    connector  TEXT NOT NULL,
    number     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (connector, number)
);

CREATE TABLE IF NOT EXISTS memories (
    id         BIGSERIAL PRIMARY KEY,
    connector  TEXT        NOT NULL,
    user_id    TEXT        NOT NULL,
    fact       TEXT        NOT NULL,
    embedding  vector(3072) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index added when memory grows beyond ~10k rows per user.
-- For now exact cosine scan is fast enough.

-- Cross-connector identity links. A "satellite" identity (connector,user_id)
-- points at a canonical "hub" (canon_connector,canon_user_id); the hub has no
-- row (it is self-canonical). Lets one person share soul/role/memory across
-- connectors. UNIQUE keeps at most one satellite per connector per hub.
CREATE TABLE IF NOT EXISTS identity_links (
    connector       TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    canon_connector TEXT NOT NULL,
    canon_user_id   TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (connector, user_id),
    UNIQUE (canon_connector, canon_user_id, connector)
);

-- Scheduled reminders. schedule is a Go duration ("1m") or a cron expr
-- ("0 9 * * *"); instruction is run through the LLM and the reply sent to chat_id.
CREATE TABLE IF NOT EXISTS reminders (
    id          BIGSERIAL PRIMARY KEY,
    connector   TEXT NOT NULL,
    user_id     TEXT NOT NULL,
    chat_id     TEXT NOT NULL,
    schedule    TEXT NOT NULL,
    instruction TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS reminders_owner_idx ON reminders (connector, user_id);
