-- afthread's tables: threads, their transcripts, their inboxes, and the
-- runs of their agents with an event log. Apply it with your migration
-- tool; afthreadpg reads and writes these tables only.

CREATE TABLE af_threads (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner      text NOT NULL,
    agent      text NOT NULL DEFAULT '',
    title      text NOT NULL DEFAULT '',
    metadata   jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX af_threads_owner ON af_threads (owner, updated_at DESC);

-- One row per transcript message, in order.
CREATE TABLE af_messages (
    thread_id uuid NOT NULL REFERENCES af_threads (id) ON DELETE CASCADE,
    seq       integer NOT NULL,
    message   jsonb NOT NULL,
    PRIMARY KEY (thread_id, seq)
);

-- Messages waiting for a thread's agent.
CREATE TABLE af_inbox (
    id         bigserial PRIMARY KEY,
    thread_id  uuid NOT NULL REFERENCES af_threads (id) ON DELETE CASCADE,
    key        text NOT NULL DEFAULT '',
    sender     text NOT NULL DEFAULT '',
    content    text NOT NULL,
    status     text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'started', 'done', 'failed')),
    error      text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    claimed_at timestamptz
);
CREATE UNIQUE INDEX af_inbox_key ON af_inbox (thread_id, key) WHERE key <> '';
CREATE INDEX af_inbox_queued ON af_inbox (thread_id, id) WHERE status = 'queued';

CREATE TABLE af_runs (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id           uuid NOT NULL REFERENCES af_threads (id) ON DELETE CASCADE,
    status              text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'succeeded', 'failed')),
    started_at          timestamptz NOT NULL DEFAULT now(),
    ended_at            timestamptz,
    turns               bigint NOT NULL DEFAULT 0,
    tool_calls          bigint NOT NULL DEFAULT 0,
    input_tokens        bigint NOT NULL DEFAULT 0,
    cached_input_tokens bigint NOT NULL DEFAULT 0,
    output_tokens       bigint NOT NULL DEFAULT 0,
    error               text NOT NULL DEFAULT ''
);
CREATE INDEX af_runs_thread ON af_runs (thread_id, started_at DESC);

CREATE TABLE af_run_events (
    run_id     uuid NOT NULL REFERENCES af_runs (id) ON DELETE CASCADE,
    seq        bigint NOT NULL,
    kind       text NOT NULL,
    data       jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, seq)
);
