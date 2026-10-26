// Package afthreadpg is a Postgres afthread.Store, built on sqlc: the
// tables are in migrations, the queries in queries.sql, and the code sqlc
// generated from them in db. Apply the migrations with your migration tool
// (Migrations embeds them) or with Migrate, or copy the package and make
// the schema your own; afthreadtest checks that a changed copy still
// behaves.
//
//	store := afthreadpg.New(pool)
//	worker := &afthread.Worker{Store: store, Agent: agentFor}
//	worker.Drain(ctx, userID, threadID)
//
// Claims use FOR UPDATE SKIP LOCKED, so workers on many servers can share
// a queue, Send is idempotent through a unique index on the thread and
// key, and a thread's lease lets one worker at a time run its agent. Every
// method checks the thread's owner.
package afthreadpg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omniaura/agentflow/adapters/afthreadpg/db"
	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afthread"
)

// Store is an afthread.Store, afthread.Runs, afthread.ThreadRuns,
// afthread.Directory and afthread.Locker over Postgres. An agent's messages go to the owner's
// oldest thread for it. Leases expire by the database's clock, so servers'
// clocks needn't agree.
type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

var (
	_ afthread.Store      = (*Store)(nil)
	_ afthread.Runs       = (*Store)(nil)
	_ afthread.Directory  = (*Store)(nil)
	_ afthread.Locker     = (*Store)(nil)
	_ afthread.ThreadRuns = (*Store)(nil)
)

// New returns a Store on pool, whose database has the migrations applied.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: db.New(pool)}
}

// uuidOf parses id; an ID that isn't a UUID names no thread.
func uuidOf(id string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(id); err != nil {
		return u, afthread.ErrNotFound
	}
	return u, nil
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return afthread.ErrNotFound
	}
	return err
}

// owned returns the thread's UUID when owner owns it.
func (s *Store) owned(ctx context.Context, q *db.Queries, owner, id string) (pgtype.UUID, error) {
	u, err := uuidOf(id)
	if err != nil {
		return u, err
	}
	if _, err := q.GetThread(ctx, db.GetThreadParams{ID: u, Owner: owner}); err != nil {
		return u, notFound(err)
	}
	return u, nil
}

func thread(t db.AfThread) (afthread.Thread, error) {
	out := afthread.Thread{ID: t.ID.String(), Owner: t.Owner, Agent: t.Agent, Title: t.Title, Created: t.CreatedAt.Time, Updated: t.UpdatedAt.Time}
	if len(t.Metadata) > 0 && string(t.Metadata) != "{}" {
		if err := json.Unmarshal(t.Metadata, &out.Metadata); err != nil {
			return out, fmt.Errorf("afthreadpg: thread %s metadata: %w", out.ID, err)
		}
	}
	return out, nil
}

func (s *Store) Create(ctx context.Context, t afthread.Thread) (afthread.Thread, error) {
	meta, err := json.Marshal(t.Metadata)
	if err != nil {
		return afthread.Thread{}, err
	}
	if t.Metadata == nil {
		meta = []byte("{}")
	}
	var row db.AfThread
	if t.ID == "" {
		row, err = s.q.CreateThread(ctx, db.CreateThreadParams{Owner: t.Owner, Agent: t.Agent, Title: t.Title, Metadata: meta})
	} else {
		var u pgtype.UUID
		if err := u.Scan(t.ID); err != nil {
			return afthread.Thread{}, fmt.Errorf("afthreadpg: thread IDs are UUIDs, got %q", t.ID)
		}
		row, err = s.q.CreateThreadWithID(ctx, db.CreateThreadWithIDParams{ID: u, Owner: t.Owner, Agent: t.Agent, Title: t.Title, Metadata: meta})
	}
	if err != nil {
		return afthread.Thread{}, err
	}
	return thread(row)
}

func (s *Store) Get(ctx context.Context, owner, id string) (afthread.Thread, error) {
	u, err := uuidOf(id)
	if err != nil {
		return afthread.Thread{}, err
	}
	row, err := s.q.GetThread(ctx, db.GetThreadParams{ID: u, Owner: owner})
	if err != nil {
		return afthread.Thread{}, notFound(err)
	}
	return thread(row)
}

func (s *Store) Transcript(ctx context.Context, owner, id string) ([]afagent.Message, error) {
	u, err := s.owned(ctx, s.q, owner, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.Transcript(ctx, u)
	if err != nil {
		return nil, err
	}
	msgs := make([]afagent.Message, len(rows))
	for i, b := range rows {
		if err := json.Unmarshal(b, &msgs[i]); err != nil {
			return nil, fmt.Errorf("afthreadpg: thread %s message %d: %w", id, i+1, err)
		}
	}
	return msgs, nil
}

// write runs fn in a transaction that holds the owner's thread locked, so
// concurrent writes to one transcript can't interleave.
func (s *Store) write(ctx context.Context, owner, id string, fn func(q *db.Queries, thread pgtype.UUID) error) error {
	u, err := uuidOf(id)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockThread(ctx, db.LockThreadParams{ID: u, Owner: owner}); err != nil {
			return notFound(err)
		}
		if err := fn(q, u); err != nil {
			return err
		}
		return q.TouchThread(ctx, u)
	})
}

func insert(ctx context.Context, q *db.Queries, thread pgtype.UUID, from int32, msgs []afagent.Message) error {
	for i, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if err := q.InsertMessage(ctx, db.InsertMessageParams{ThreadID: thread, Seq: from + int32(i) + 1, Message: b}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Append(ctx context.Context, owner, id string, msgs ...afagent.Message) error {
	return s.write(ctx, owner, id, func(q *db.Queries, thread pgtype.UUID) error {
		last, err := q.LastSeq(ctx, thread)
		if err != nil {
			return err
		}
		return insert(ctx, q, thread, last, msgs)
	})
}

func (s *Store) Replace(ctx context.Context, owner, id string, msgs []afagent.Message) error {
	return s.write(ctx, owner, id, func(q *db.Queries, thread pgtype.UUID) error {
		if err := q.DeleteMessages(ctx, thread); err != nil {
			return err
		}
		return insert(ctx, q, thread, 0, msgs)
	})
}

func incoming(r db.AfInbox) afthread.Incoming {
	return afthread.Incoming{ID: r.ID, Key: r.Key, From: r.Sender, Content: r.Content, Status: afthread.Status(r.Status), Error: r.Error, At: r.CreatedAt.Time, Claimed: r.ClaimedAt.Time}
}

func (s *Store) Send(ctx context.Context, owner, id string, in afthread.Incoming) (afthread.Incoming, error) {
	u, err := s.owned(ctx, s.q, owner, id)
	if err != nil {
		return afthread.Incoming{}, err
	}
	row, err := s.q.Send(ctx, db.SendParams{ThreadID: u, Key: in.Key, Sender: in.From, Content: in.Content})
	if errors.Is(err, pgx.ErrNoRows) && in.Key != "" {
		// The key was sent before: that message, unchanged.
		row, err = s.q.InboxByKey(ctx, db.InboxByKeyParams{ThreadID: u, Key: in.Key})
	}
	if err != nil {
		return afthread.Incoming{}, err
	}
	return incoming(row), nil
}

func (s *Store) Claim(ctx context.Context, owner, id string) ([]afthread.Incoming, error) {
	u, err := s.owned(ctx, s.q, owner, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.Claim(ctx, u)
	if err != nil {
		return nil, err
	}
	out := make([]afthread.Incoming, len(rows))
	for i, r := range rows {
		out[i] = incoming(r)
	}
	// RETURNING has no order.
	slices.SortFunc(out, func(a, b afthread.Incoming) int { return int(a.ID - b.ID) })
	return out, nil
}

func (s *Store) Finish(ctx context.Context, owner, id string, ids []int64, failure error) error {
	u, err := s.owned(ctx, s.q, owner, id)
	if err != nil {
		return err
	}
	status, msg := string(afthread.Done), ""
	if failure != nil {
		status, msg = string(afthread.Failed), failure.Error()
	}
	return s.q.Finish(ctx, db.FinishParams{Status: status, Error: msg, ThreadID: u, Ids: ids})
}

func (s *Store) Reclaim(ctx context.Context, owner, id string, before time.Time) (int, error) {
	u, err := s.owned(ctx, s.q, owner, id)
	if err != nil {
		return 0, err
	}
	at := pgtype.Timestamptz{Time: before, Valid: true}
	var n int64
	// Together, so a requeued message's run never stays running.
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		if n, err = q.Reclaim(ctx, db.ReclaimParams{ThreadID: u, Before: at}); err != nil {
			return err
		}
		_, err = q.AbandonRuns(ctx, db.AbandonRunsParams{ThreadID: u, Before: at, Error: afthread.ErrRunAbandoned.Error()})
		return err
	})
	return int(n), err
}

func (s *Store) Lock(ctx context.Context, owner, id, worker string, lease time.Duration) (bool, error) {
	u, err := uuidOf(id)
	if err != nil {
		return false, err
	}
	n, err := s.q.LeaseThread(ctx, db.LeaseThreadParams{Worker: worker, Seconds: lease.Seconds(), ID: u, Owner: owner})
	if err != nil || n == 1 {
		return n == 1, err
	}
	// Leased to another worker, or not the owner's thread.
	_, err = s.owned(ctx, s.q, owner, id)
	return false, err
}

func (s *Store) Unlock(ctx context.Context, owner, id, worker string) (bool, error) {
	u, err := s.owned(ctx, s.q, owner, id)
	if err != nil {
		return false, err
	}
	if err := s.q.ReleaseThread(ctx, db.ReleaseThreadParams{ID: u, Owner: owner, Worker: worker}); err != nil {
		return false, err
	}
	// A statement of its own, after the release: its snapshot then holds
	// every message sent by a worker that found the thread leased.
	return s.q.HasQueued(ctx, u)
}

func runRecord(owner string, r db.AfRun) afthread.RunRecord {
	return afthread.RunRecord{
		ID: r.ID.String(), Owner: owner, Thread: r.ThreadID.String(), Status: afthread.RunStatus(r.Status),
		Started: r.StartedAt.Time, Ended: r.EndedAt.Time,
		RunEnd: afthread.RunEnd{Turns: int(r.Turns), ToolCalls: int(r.ToolCalls), Error: r.Error,
			Usage: afagent.Usage{InputTokens: int(r.InputTokens), CachedInputTokens: int(r.CachedInputTokens), OutputTokens: int(r.OutputTokens)}},
	}
}

func (s *Store) StartRun(ctx context.Context, owner, thread string) (afthread.RunRecord, error) {
	u, err := s.owned(ctx, s.q, owner, thread)
	if err != nil {
		return afthread.RunRecord{}, err
	}
	r, err := s.q.StartRun(ctx, u)
	if err != nil {
		return afthread.RunRecord{}, err
	}
	return runRecord(owner, r), nil
}

func (s *Store) GetRun(ctx context.Context, owner, run string) (afthread.RunRecord, error) {
	u, err := uuidOf(run)
	if err != nil {
		return afthread.RunRecord{}, err
	}
	r, err := s.q.GetRun(ctx, db.GetRunParams{ID: u, Owner: owner})
	if err != nil {
		return afthread.RunRecord{}, notFound(err)
	}
	return runRecord(owner, r), nil
}

func (s *Store) ThreadRuns(ctx context.Context, owner, thread string, limit int) ([]afthread.RunRecord, error) {
	u, err := s.owned(ctx, s.q, owner, thread)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ThreadRuns(ctx, db.ThreadRunsParams{ThreadID: u, Owner: owner, Lim: int32(min(max(limit, 0), math.MaxInt32))})
	if err != nil {
		return nil, err
	}
	out := make([]afthread.RunRecord, len(rows))
	for i, r := range rows {
		out[i] = runRecord(owner, r)
	}
	return out, nil
}

func (s *Store) AddEvent(ctx context.Context, owner, run string, e afthread.RunEvent) (afthread.RunEvent, error) {
	u, err := uuidOf(run)
	if err != nil {
		return afthread.RunEvent{}, err
	}
	var out afthread.RunEvent
	// The run's row lock numbers concurrent events, as tools running in
	// parallel record them, one after another.
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockRun(ctx, db.LockRunParams{ID: u, Owner: owner}); err != nil {
			return notFound(err)
		}
		row, err := q.AddEvent(ctx, db.AddEventParams{RunID: u, Kind: e.Kind, Data: e.Data})
		if err != nil {
			return err
		}
		out = afthread.RunEvent{Seq: row.Seq, Kind: row.Kind, Data: row.Data, At: row.CreatedAt.Time}
		return nil
	})
	return out, err
}

func (s *Store) Events(ctx context.Context, owner, run string, after int64) ([]afthread.RunEvent, error) {
	if _, err := s.GetRun(ctx, owner, run); err != nil {
		return nil, err
	}
	u, _ := uuidOf(run)
	rows, err := s.q.Events(ctx, db.EventsParams{RunID: u, After: after})
	if err != nil {
		return nil, err
	}
	out := make([]afthread.RunEvent, len(rows))
	for i, r := range rows {
		out[i] = afthread.RunEvent{Seq: r.Seq, Kind: r.Kind, Data: r.Data, At: r.CreatedAt.Time}
	}
	return out, nil
}

func (s *Store) EndRun(ctx context.Context, owner, run string, end afthread.RunEnd) error {
	r, err := s.GetRun(ctx, owner, run)
	if err != nil {
		return err
	}
	status := afthread.Succeeded
	if end.Error != "" {
		status = afthread.RunFailed
	}
	u, _ := uuidOf(r.ID)
	return s.q.EndRun(ctx, db.EndRunParams{
		Status: string(status), Turns: int64(end.Turns), ToolCalls: int64(end.ToolCalls),
		InputTokens: int64(end.Usage.InputTokens), CachedInputTokens: int64(end.Usage.CachedInputTokens), OutputTokens: int64(end.Usage.OutputTokens),
		Error: end.Error, ID: u,
	})
}

func (s *Store) AgentThread(ctx context.Context, owner, agent string) (string, error) {
	id, err := s.q.AgentThread(ctx, db.AgentThreadParams{Owner: owner, Agent: agent})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", afthread.ErrNoAgent
	}
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func (s *Store) Agents(ctx context.Context, owner string) ([]afthread.AgentInfo, error) {
	rows, err := s.q.Agents(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]afthread.AgentInfo, len(rows))
	for i, r := range rows {
		out[i] = afthread.AgentInfo{Name: r.Agent, Description: r.Title}
	}
	return out, nil
}
