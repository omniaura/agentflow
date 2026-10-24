package afthreadpg_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omniaura/agentflow/adapters/afthreadpg"
	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afthread"
	"github.com/omniaura/agentflow/runtime/afthread/afthreadtest"
)

// testPool connects to AF_TEST_DATABASE_URL and applies the migration on
// fresh af_ tables, or skips without one.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("AF_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set AF_TEST_DATABASE_URL to test against Postgres")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migration, err := os.ReadFile("migrations/001_afthread.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS af_run_events, af_runs, af_inbox, af_messages, af_threads CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestStore(t *testing.T) {
	pool := testPool(t)
	afthreadtest.Run(t, func() afthread.Store { return afthreadpg.New(pool) })
}

// A full worker step against Postgres: the transcript, the inbox and the
// run's events land in the tables.
func TestRun(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := afthreadpg.New(pool)
	th, err := s.Create(ctx, afthread.Thread{Owner: "ann", Agent: "support"})
	if err != nil {
		t.Fatal(err)
	}
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "m1", Content: "look up 7 and 8"})
	turn := 0
	model := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		if turn++; turn == 1 {
			// Two calls at once: their events are numbered without clashing.
			return afagent.Response{Message: afagent.Message{ToolCalls: []afagent.ToolCall{{ID: "1", Name: "look", Arguments: `{"id":7}`}, {ID: "2", Name: "look", Arguments: `{"id":8}`}}}, Usage: afagent.Usage{InputTokens: 40}}, nil
		}
		return afagent.Response{Message: afagent.Message{Content: "both found"}, Usage: afagent.Usage{InputTokens: 60, OutputTokens: 4}}, nil
	})
	agent := func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		l := afagent.Loop{Model: model, Tools: []afagent.ToolSpec{{Name: "look"}}, BeforeTurn: before, OnEvent: afthread.Recorder(ctx), Concurrency: 2,
			Dispatch: func(ctx context.Context, call afagent.ToolCall) (string, error) { return "row " + call.ID, nil }}
		return l.Run(ctx, msgs)
	}
	res, err := afthread.Run(ctx, s, "ann", th.ID, agent)
	if err != nil || res.Final != "both found" {
		t.Fatalf("Run = %+v, %v", res, err)
	}
	got, _ := s.Transcript(ctx, "ann", th.ID)
	if len(got) != 5 || got[0].Content != "look up 7 and 8" || len(got[1].ToolCalls) != 2 || got[4].Content != "both found" {
		t.Fatalf("transcript = %+v", got)
	}
	var runID string
	if err := pool.QueryRow(ctx, "SELECT id::text FROM af_runs WHERE thread_id = $1", th.ID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	r, err := s.GetRun(ctx, "ann", runID)
	if err != nil || r.Status != afthread.Succeeded || r.Turns != 2 || r.ToolCalls != 2 || r.Usage.InputTokens != 100 {
		t.Fatalf("run = %+v, %v", r, err)
	}
	events, _ := s.Events(ctx, "ann", runID, 0)
	var kinds []string
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Errorf("event %d has seq %d", i, e.Seq)
		}
		kinds = append(kinds, e.Kind)
	}
	if n := strings.Count(strings.Join(kinds, ","), "tool_returned"); n != 2 || len(events) != 6 {
		t.Fatalf("events = %v", kinds)
	}
	var status string
	pool.QueryRow(ctx, "SELECT status FROM af_inbox WHERE thread_id = $1 AND key = 'm1'", th.ID).Scan(&status)
	if status != "done" {
		t.Errorf("inbox status = %q", status)
	}
}
