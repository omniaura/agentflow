-- name: CreateThread :one
INSERT INTO af_threads (owner, agent, title, metadata)
VALUES (@owner, @agent, @title, @metadata)
RETURNING *;

-- name: CreateThreadWithID :one
INSERT INTO af_threads (id, owner, agent, title, metadata)
VALUES (@id, @owner, @agent, @title, @metadata)
RETURNING *;

-- name: GetThread :one
SELECT * FROM af_threads WHERE id = @id AND owner = @owner;

-- name: LockThread :one
SELECT id FROM af_threads WHERE id = @id AND owner = @owner FOR UPDATE;

-- name: TouchThread :exec
UPDATE af_threads SET updated_at = now() WHERE id = @id;

-- name: Transcript :many
SELECT message FROM af_messages WHERE thread_id = @thread_id ORDER BY seq;

-- name: LastSeq :one
SELECT coalesce(max(seq), 0)::integer FROM af_messages WHERE thread_id = @thread_id;

-- name: InsertMessage :exec
INSERT INTO af_messages (thread_id, seq, message) VALUES (@thread_id, @seq, @message);

-- name: DeleteMessages :exec
DELETE FROM af_messages WHERE thread_id = @thread_id;

-- name: Send :one
INSERT INTO af_inbox (thread_id, key, sender, content)
VALUES (@thread_id, @key, @sender, @content)
ON CONFLICT (thread_id, key) WHERE key <> '' DO NOTHING
RETURNING *;

-- name: InboxByKey :one
SELECT * FROM af_inbox WHERE thread_id = @thread_id AND key = @key;

-- name: Claim :many
UPDATE af_inbox SET status = 'started', claimed_at = now()
WHERE id IN (
    SELECT q.id FROM af_inbox q
    WHERE q.thread_id = @thread_id AND q.status = 'queued'
    ORDER BY q.id
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: Finish :exec
UPDATE af_inbox SET status = @status, error = @error
WHERE thread_id = @thread_id AND status = 'started' AND id = ANY(@ids::bigint[]);

-- name: Reclaim :execrows
UPDATE af_inbox SET status = 'queued'
WHERE thread_id = @thread_id AND status = 'started' AND claimed_at < @before;

-- name: AbandonRuns :execrows
UPDATE af_runs SET status = 'failed', ended_at = now(), error = @error
WHERE thread_id = @thread_id AND status = 'running' AND started_at < @before;

-- name: StartRun :one
INSERT INTO af_runs (thread_id) VALUES (@thread_id) RETURNING *;

-- name: GetRun :one
SELECT r.* FROM af_runs r JOIN af_threads t ON t.id = r.thread_id
WHERE r.id = @id AND t.owner = @owner;

-- name: LockRun :one
SELECT r.id FROM af_runs r JOIN af_threads t ON t.id = r.thread_id
WHERE r.id = @id AND t.owner = @owner
FOR UPDATE OF r;

-- name: AddEvent :one
INSERT INTO af_run_events (run_id, seq, kind, data)
SELECT @run_id, coalesce(max(e.seq), 0) + 1, @kind, @data
FROM af_run_events e WHERE e.run_id = @run_id
RETURNING *;

-- name: Events :many
SELECT * FROM af_run_events WHERE run_id = @run_id AND seq > @after ORDER BY seq;

-- name: EndRun :exec
UPDATE af_runs SET status = @status, ended_at = now(), turns = @turns, tool_calls = @tool_calls,
    input_tokens = @input_tokens, cached_input_tokens = @cached_input_tokens,
    output_tokens = @output_tokens, error = @error
WHERE id = @id;

-- name: AgentThread :one
SELECT id FROM af_threads
WHERE owner = @owner AND agent = @agent
ORDER BY created_at, id
LIMIT 1;

-- name: Agents :many
SELECT DISTINCT ON (agent) agent, title FROM af_threads
WHERE owner = @owner AND agent <> ''
ORDER BY agent, created_at, id;

-- name: LeaseThread :execrows
UPDATE af_threads
SET lease_holder = @worker, lease_until = now() + make_interval(secs => @seconds::float8)
WHERE id = @id AND owner = @owner
  AND (lease_holder = '' OR lease_holder = @worker OR lease_until < now());

-- name: ReleaseThread :exec
UPDATE af_threads SET lease_holder = '', lease_until = NULL
WHERE id = @id AND owner = @owner AND lease_holder = @worker;

-- name: HasQueued :one
SELECT EXISTS (SELECT 1 FROM af_inbox WHERE thread_id = @thread_id AND status = 'queued');

-- name: ThreadRuns :many
SELECT r.* FROM af_runs r JOIN af_threads t ON t.id = r.thread_id
WHERE r.thread_id = @thread_id AND t.owner = @owner
ORDER BY r.started_at DESC, r.id DESC
LIMIT @lim;
