package afthreadpg_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omniaura/agentflow/adapters/afthreadpg"
	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afthread"
	"github.com/omniaura/agentflow/runtime/afthread/afthreadtest"
)

// testPool connects to AF_TEST_DATABASE_URL, in a schema of its own that
// it drops after the test, and applies the migrations there, in order. It skips
// without a URL.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := emptyPool(t)
	if err := afthreadpg.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

// emptyPool connects to AF_TEST_DATABASE_URL in an empty schema of its own,
// dropped after the test.
func emptyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("AF_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set AF_TEST_DATABASE_URL to test against Postgres")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("afthreadpg_test_%d", rand.Int64())
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), url)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close(context.Background())
		if _, err := c.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Migrate applies every migration once: again, it does nothing, and
// servers starting together don't apply one twice.
func TestMigrate(t *testing.T) {
	pool := emptyPool(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if err := afthreadpg.Migrate(ctx, pool); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	files, _ := filepath.Glob("migrations/*.sql")
	var applied int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM af_migrations").Scan(&applied); err != nil || applied != len(files) || applied == 0 {
		t.Fatalf("%d of %d migrations recorded: %v", applied, len(files), err)
	}
	if err := afthreadpg.Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate again: %v", err)
	}
	// It let go of its lock: another session takes it at once. A new
	// connection, since a pooled one may be the session Migrate used, which
	// would take the lock again whether it held it or not.
	conn, err := pgx.ConnectConfig(ctx, pool.Config().ConnConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtext('afthreadpg.Migrate'))").Scan(&locked); err != nil || !locked {
		t.Fatalf("the migration lock is still held: %v", err)
	}
	conn.Exec(ctx, "SELECT pg_advisory_unlock(hashtext('afthreadpg.Migrate'))")
	// The tables are there.
	if _, err := afthreadpg.New(pool).Create(ctx, afthread.Thread{Owner: "ann"}); err != nil {
		t.Fatal(err)
	}
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
		l := afagent.Loop{Model: model, Tools: []afagent.ToolSpec{{Name: "look"}}, BeforeTurn: before, Concurrency: 2,
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

// Workers on many servers share threads through the lease: concurrent
// requests each send and drain, and every message is answered by one run
// at a time.
func TestWorkers(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := afthreadpg.New(pool)
	th, err := s.Create(ctx, afthread.Thread{Owner: "ann", Agent: "support"})
	if err != nil {
		t.Fatal(err)
	}
	var running, overlaps atomic.Int32
	agent := func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		if running.Add(1) > 1 {
			overlaps.Add(1)
		}
		defer running.Add(-1)
		msgs, err := before(ctx, 1, msgs)
		if err != nil {
			return afagent.Result{}, err
		}
		time.Sleep(2 * time.Millisecond)
		answer := afagent.Message{Role: afagent.Assistant, Content: "ok"}
		return afagent.Result{Final: "ok", Turns: 1, Messages: append(msgs, answer)}, nil
	}
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Go(func() {
			// A worker per request, as on separate servers.
			w := &afthread.Worker{Store: s, Lease: time.Second, Agent: func(context.Context, afthread.Thread) (afthread.Agent, error) { return agent, nil }}
			if _, err := s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: fmt.Sprint(i), Content: fmt.Sprint(i)}); err != nil {
				t.Error(err)
			}
			if err := w.Drain(ctx, "ann", th.ID); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := overlaps.Load(); n != 0 {
		t.Errorf("%d runs overlapped", n)
	}
	var notDone int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM af_inbox WHERE thread_id = $1 AND status <> 'done'", th.ID).Scan(&notDone); err != nil {
		t.Fatal(err)
	}
	got, err := s.Transcript(ctx, "ann", th.ID)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, m := range got {
		if m.Role == afagent.User {
			users++
		}
	}
	if notDone != 0 || users != 30 {
		t.Errorf("%d messages not done, %d in the transcript", notDone, users)
	}
}

// afthread.Server on Postgres: a message sent over HTTP is answered in the
// background, and the run and its events can be read back.
func TestServer(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := afthreadpg.New(pool)
	th, err := s.Create(ctx, afthread.Thread{Owner: "ann", Agent: "support"})
	if err != nil {
		t.Fatal(err)
	}
	turn := 0
	agent := func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		model := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
			if turn++; turn == 1 {
				return afagent.Response{Message: afagent.Message{ToolCalls: []afagent.ToolCall{{ID: "1", Name: "look", Arguments: `{"id":7}`}}}}, nil
			}
			return afagent.Response{Message: afagent.Message{Content: "found it"}}, nil
		})
		l := afagent.Loop{Model: model, Tools: []afagent.ToolSpec{{Name: "look"}}, BeforeTurn: before,
			Dispatch: func(context.Context, afagent.ToolCall) (string, error) { return "row 7", nil }}
		return l.Run(ctx, msgs)
	}
	srv := &afthread.Server{Store: s, Owner: func(*http.Request) (string, error) { return "ann", nil },
		Worker: &afthread.Worker{Store: s, Agent: func(context.Context, afthread.Thread) (afthread.Agent, error) { return agent, nil }}}
	mux := http.NewServeMux()
	srv.Register(mux, "/threads")
	get := func(path string) string {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body)
		}
		return w.Body.String()
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/threads/"+th.ID+"/messages", strings.NewReader(`{"message":"find 7"}`)))
	if w.Code != 202 {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if got := get("/threads/" + th.ID + "/messages"); !strings.Contains(got, `{"role":"assistant","content":"found it"}`) {
		t.Errorf("messages = %s", got)
	}
	var list afthread.ThreadRunList
	json.Unmarshal([]byte(get("/threads/"+th.ID+"/runs")), &list)
	if len(list.Runs) != 1 || list.Runs[0].Status != afthread.Succeeded || list.Runs[0].ToolCalls != 1 {
		t.Fatalf("runs = %+v", list)
	}
	events := get("/threads/" + th.ID + "/runs/" + list.Runs[0].ID + "/events")
	if !strings.Contains(events, `"kind":"tool_called","data":{"name":"look"}`) || strings.Contains(events, "row 7") {
		t.Errorf("events = %s", events)
	}
}
