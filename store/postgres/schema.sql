CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS soul_assignments (
    connector  TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    soul_name  TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (connector, user_id)
);

CREATE TABLE IF NOT EXISTS memories (
    id         BIGSERIAL PRIMARY KEY,
    connector  TEXT        NOT NULL,
    user_id    TEXT        NOT NULL,
    fact       TEXT        NOT NULL,
    embedding  vector(768) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS memories_embedding_idx
    ON memories USING hnsw (embedding vector_cosine_ops);
