// Package afthreadtest checks that an afthread.Store behaves as afthread
// expects. Run it against an adapter's test database:
//
//	func TestStore(t *testing.T) {
//		afthreadtest.Run(t, func() afthread.Store { return newStore(t, db) })
//	}
package afthreadtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afthread"
)

// Run checks the Store newStore returns; each subtest gets a new one.
func Run(t *testing.T, newStore func() afthread.Store) {
	ctx := context.Background()
	create := func(t *testing.T, s afthread.Store, owner string) afthread.Thread {
		t.Helper()
		th, err := s.Create(ctx, afthread.Thread{Owner: owner, Agent: "support", Title: "help", Metadata: map[string]string{"k": "v"}})
		if err != nil {
			t.Fatal(err)
		}
		if th.ID == "" {
			t.Fatal("Create gave no ID")
		}
		return th
	}

	t.Run("threads", func(t *testing.T) {
		s := newStore()
		th := create(t, s, "ann")
		got, err := s.Get(ctx, "ann", th.ID)
		if err != nil || got.Agent != "support" || got.Title != "help" || got.Metadata["k"] != "v" {
			t.Fatalf("Get = %+v, %v", got, err)
		}
		if _, err := s.Get(ctx, "ann", "nope"); !errors.Is(err, afthread.ErrNotFound) {
			t.Errorf("missing thread: %v", err)
		}
	})

	t.Run("owners", func(t *testing.T) {
		s := newStore()
		th := create(t, s, "ann")
		// Another owner can't read, write or queue on it.
		checks := map[string]error{}
		_, checks["Get"] = s.Get(ctx, "bob", th.ID)
		_, checks["Transcript"] = s.Transcript(ctx, "bob", th.ID)
		checks["Append"] = s.Append(ctx, "bob", th.ID, afagent.Message{Role: afagent.User, Content: "x"})
		checks["Replace"] = s.Replace(ctx, "bob", th.ID, nil)
		_, checks["Send"] = s.Send(ctx, "bob", th.ID, afthread.Incoming{Content: "x"})
		_, checks["Claim"] = s.Claim(ctx, "bob", th.ID)
		checks["Finish"] = s.Finish(ctx, "bob", th.ID, []int64{1}, nil)
		_, checks["Reclaim"] = s.Reclaim(ctx, "bob", th.ID, time.Now())
		for name, err := range checks {
			if !errors.Is(err, afthread.ErrNotFound) {
				t.Errorf("%s by another owner: %v", name, err)
			}
		}
		if msgs, _ := s.Transcript(ctx, "ann", th.ID); len(msgs) != 0 {
			t.Errorf("another owner wrote %+v", msgs)
		}
	})

	t.Run("transcript", func(t *testing.T) {
		s := newStore()
		th := create(t, s, "ann")
		a := afagent.Message{Role: afagent.User, Content: "hi"}
		b := afagent.Message{Role: afagent.Assistant, ToolCalls: []afagent.ToolCall{{ID: "1", Name: "look", Arguments: `{"x":1}`}}}
		c := afagent.Message{Role: afagent.Tool, ToolCallID: "1", Content: "no", IsError: true}
		if err := s.Append(ctx, "ann", th.ID, a, b); err != nil {
			t.Fatal(err)
		}
		if err := s.Append(ctx, "ann", th.ID, c); err != nil {
			t.Fatal(err)
		}
		got, err := s.Transcript(ctx, "ann", th.ID)
		if err != nil || len(got) != 3 || got[0].Content != "hi" || got[1].ToolCalls[0] != b.ToolCalls[0] || got[2].ToolCallID != "1" || !got[2].IsError {
			t.Fatalf("Transcript = %+v, %v", got, err)
		}
		summary := afagent.Message{Role: afagent.User, Content: "summary"}
		if err := s.Replace(ctx, "ann", th.ID, []afagent.Message{summary}); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Transcript(ctx, "ann", th.ID); len(got) != 1 || got[0].Content != "summary" {
			t.Fatalf("after Replace = %+v", got)
		}
	})

	t.Run("inbox", func(t *testing.T) {
		s := newStore()
		th := create(t, s, "ann")
		first, err := s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "k1", Content: "one"})
		if err != nil || first.Status != afthread.Queued || first.ID == 0 {
			t.Fatalf("Send = %+v, %v", first, err)
		}
		// The same key again is the same message.
		again, err := s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "k1", Content: "changed"})
		if err != nil || again.ID != first.ID || again.Content != "one" {
			t.Fatalf("Send again = %+v, %v", again, err)
		}
		if _, err := s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "k2", From: "tracker", Content: "two"}); err != nil {
			t.Fatal(err)
		}
		claimed, err := s.Claim(ctx, "ann", th.ID)
		if err != nil || len(claimed) != 2 || claimed[0].Content != "one" || claimed[1].From != "tracker" || claimed[0].ID >= claimed[1].ID || claimed[0].Status != afthread.Started {
			t.Fatalf("Claim = %+v, %v", claimed, err)
		}
		if more, err := s.Claim(ctx, "ann", th.ID); err != nil || len(more) != 0 {
			t.Fatalf("second Claim = %+v, %v", more, err)
		}
		if err := s.Finish(ctx, "ann", th.ID, []int64{claimed[0].ID}, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.Finish(ctx, "ann", th.ID, []int64{claimed[1].ID}, errors.New("model down")); err != nil {
			t.Fatal(err)
		}
		// A finished key is still the same message: a retry doesn't run
		// it again.
		if again, _ := s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "k2"}); again.Status != afthread.Failed || again.Error != "model down" {
			t.Fatalf("finished message = %+v", again)
		}
		if done, _ := s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "k1"}); done.Status != afthread.Done {
			t.Fatalf("done message = %+v", done)
		}
	})

	t.Run("reclaim", func(t *testing.T) {
		s := newStore()
		th := create(t, s, "ann")
		s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "a", Content: "a"})
		s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: "b", Content: "b"})
		claimed, _ := s.Claim(ctx, "ann", th.ID)
		if len(claimed) != 2 || claimed[0].Claimed.IsZero() {
			t.Fatalf("Claim = %+v", claimed)
		}
		s.Finish(ctx, "ann", th.ID, []int64{claimed[0].ID}, nil)
		// A lease that hasn't expired reclaims nothing.
		if n, err := s.Reclaim(ctx, "ann", th.ID, claimed[1].Claimed.Add(-time.Hour)); err != nil || n != 0 {
			t.Fatalf("early Reclaim = %d, %v", n, err)
		}
		// Past it, the unfinished message is queued again; the finished one
		// stays done.
		if n, err := s.Reclaim(ctx, "ann", th.ID, time.Now().Add(time.Second)); err != nil || n != 1 {
			t.Fatalf("Reclaim = %d, %v", n, err)
		}
		again, _ := s.Claim(ctx, "ann", th.ID)
		if len(again) != 1 || again[0].Key != "b" {
			t.Fatalf("Claim after Reclaim = %+v", again)
		}
	})

	t.Run("copies", func(t *testing.T) {
		s := newStore()
		th, _ := s.Create(ctx, afthread.Thread{Owner: "ann", Metadata: map[string]string{"k": "v"}})
		th.Metadata["k"] = "changed"
		in := []afagent.Message{{Role: afagent.Assistant, ToolCalls: []afagent.ToolCall{{ID: "1", Name: "look"}}}}
		s.Append(ctx, "ann", th.ID, in...)
		in[0].ToolCalls[0].Name = "changed"
		got, _ := s.Transcript(ctx, "ann", th.ID)
		got[0].ToolCalls[0].ID = "changed"
		again, _ := s.Transcript(ctx, "ann", th.ID)
		if stored, _ := s.Get(ctx, "ann", th.ID); stored.Metadata["k"] != "v" || again[0].ToolCalls[0] != (afagent.ToolCall{ID: "1", Name: "look"}) {
			t.Fatalf("the store shares data with callers: %+v, %+v", stored.Metadata, again)
		}
	})

	t.Run("runs", func(t *testing.T) {
		s := newStore()
		runs, ok := s.(afthread.Runs)
		if !ok {
			t.Skip("the store doesn't record runs")
		}
		th := create(t, s, "ann")
		r, err := runs.StartRun(ctx, "ann", th.ID)
		if err != nil || r.ID == "" || r.Status != afthread.Running || r.Thread != th.ID {
			t.Fatalf("StartRun = %+v, %v", r, err)
		}
		for _, kind := range []string{"tool_called", "tool_returned", "compacted"} {
			if _, err := runs.AddEvent(ctx, "ann", r.ID, afthread.RunEvent{Kind: kind, Data: []byte(`{"turn":1}`)}); err != nil {
				t.Fatal(err)
			}
		}
		all, err := runs.Events(ctx, "ann", r.ID, 0)
		if err != nil || len(all) != 3 || all[0].Seq >= all[1].Seq || all[2].Kind != "compacted" || !sameJSON(all[0].Data, `{"turn":1}`) {
			t.Fatalf("Events = %+v, %v", all, err)
		}
		// Resume after a seen event, as a client following the run would.
		if rest, _ := runs.Events(ctx, "ann", r.ID, all[1].Seq); len(rest) != 1 || rest[0].Kind != "compacted" {
			t.Fatalf("Events after %d = %+v", all[1].Seq, rest)
		}
		end := afthread.RunEnd{Turns: 3, ToolCalls: 2, Usage: afagent.Usage{InputTokens: 100, OutputTokens: 20}, Error: "model down"}
		if err := runs.EndRun(ctx, "ann", r.ID, end); err != nil {
			t.Fatal(err)
		}
		got, err := runs.GetRun(ctx, "ann", r.ID)
		if err != nil || got.Status != afthread.RunFailed || got.Turns != 3 || got.Usage.InputTokens != 100 || got.Error != "model down" || got.Ended.IsZero() {
			t.Fatalf("GetRun = %+v, %v", got, err)
		}
		// Another owner sees nothing.
		if _, err := runs.GetRun(ctx, "bob", r.ID); !errors.Is(err, afthread.ErrNotFound) {
			t.Errorf("GetRun by another owner: %v", err)
		}
		if _, err := runs.AddEvent(ctx, "bob", r.ID, afthread.RunEvent{Kind: "x"}); !errors.Is(err, afthread.ErrNotFound) {
			t.Errorf("AddEvent by another owner: %v", err)
		}
		if _, err := runs.Events(ctx, "bob", r.ID, 0); !errors.Is(err, afthread.ErrNotFound) {
			t.Errorf("Events by another owner: %v", err)
		}
		if _, err := runs.StartRun(ctx, "bob", th.ID); !errors.Is(err, afthread.ErrNotFound) {
			t.Errorf("StartRun on another owner's thread: %v", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		s := newStore()
		dir, ok := s.(afthread.Directory)
		if !ok {
			t.Skip("the store isn't a directory")
		}
		// An owner of its own: a database store keeps what other subtests
		// created.
		owner := fmt.Sprintf("dir-%d", time.Now().UnixNano())
		first, _ := s.Create(ctx, afthread.Thread{Owner: owner, Agent: "tracker", Title: "Tracks parcels."})
		time.Sleep(2 * time.Millisecond)
		s.Create(ctx, afthread.Thread{Owner: owner, Agent: "tracker", Title: "a later thread"})
		s.Create(ctx, afthread.Thread{Owner: owner, Agent: "billing", Title: "Handles invoices."})
		s.Create(ctx, afthread.Thread{Owner: "bob", Agent: "secret"})
		// An agent's messages go to its oldest thread.
		if id, err := dir.AgentThread(ctx, owner, "tracker"); err != nil || id != first.ID {
			t.Fatalf("AgentThread = %q, %v; want %q", id, err, first.ID)
		}
		if _, err := dir.AgentThread(ctx, owner, "secret"); !errors.Is(err, afthread.ErrNoAgent) {
			t.Errorf("another owner's agent: %v", err)
		}
		agents, err := dir.Agents(ctx, owner)
		if err != nil || len(agents) != 2 || agents[0] != (afthread.AgentInfo{Name: "billing", Description: "Handles invoices."}) || agents[1] != (afthread.AgentInfo{Name: "tracker", Description: "Tracks parcels."}) {
			t.Fatalf("Agents = %+v, %v", agents, err)
		}
	})

	t.Run("locker", func(t *testing.T) {
		s := newStore()
		l, ok := s.(afthread.Locker)
		if !ok {
			t.Skip("the store doesn't lease threads")
		}
		th := create(t, s, "ann")
		if held, err := l.Lock(ctx, "ann", th.ID, "w1", time.Minute); err != nil || !held {
			t.Fatalf("Lock = %v, %v", held, err)
		}
		// Another worker waits; the holder extends its lease.
		if held, err := l.Lock(ctx, "ann", th.ID, "w2", time.Minute); err != nil || held {
			t.Fatalf("Lock by another worker = %v, %v", held, err)
		}
		if held, err := l.Lock(ctx, "ann", th.ID, "w1", time.Minute); err != nil || !held {
			t.Fatalf("Lock by the holder = %v, %v", held, err)
		}
		// Unlocking by another worker leaves the lease; it reports the queue.
		s.Send(ctx, "ann", th.ID, afthread.Incoming{Content: "hi"})
		if queued, err := l.Unlock(ctx, "ann", th.ID, "w2"); err != nil || !queued {
			t.Fatalf("Unlock by another worker = %v, %v", queued, err)
		}
		if held, _ := l.Lock(ctx, "ann", th.ID, "w2", time.Minute); held {
			t.Fatal("another worker's Unlock ended the lease")
		}
		if queued, err := l.Unlock(ctx, "ann", th.ID, "w1"); err != nil || !queued {
			t.Fatalf("Unlock with a queued message = %v, %v", queued, err)
		}
		claimed, _ := s.Claim(ctx, "ann", th.ID)
		// Claimed isn't queued.
		if queued, err := l.Unlock(ctx, "ann", th.ID, "w1"); err != nil || queued {
			t.Fatalf("Unlock with a claimed message = %v, %v", queued, err)
		}
		s.Finish(ctx, "ann", th.ID, []int64{claimed[0].ID}, nil)
		// Released, the thread is free; an expired lease is too.
		if held, err := l.Lock(ctx, "ann", th.ID, "w2", 50*time.Millisecond); err != nil || !held {
			t.Fatalf("Lock after Unlock = %v, %v", held, err)
		}
		time.Sleep(100 * time.Millisecond)
		if held, err := l.Lock(ctx, "ann", th.ID, "w3", time.Minute); err != nil || !held {
			t.Fatalf("Lock after the lease expired = %v, %v", held, err)
		}
		// Another owner can't lease or unlock it.
		if _, err := l.Lock(ctx, "bob", th.ID, "w4", time.Minute); !errors.Is(err, afthread.ErrNotFound) {
			t.Errorf("Lock by another owner: %v", err)
		}
		if _, err := l.Unlock(ctx, "bob", th.ID, "w3"); !errors.Is(err, afthread.ErrNotFound) {
			t.Errorf("Unlock by another owner: %v", err)
		}
		if held, _ := l.Lock(ctx, "ann", th.ID, "w5", time.Minute); held {
			t.Error("another owner's Unlock ended the lease")
		}
	})

	t.Run("concurrent locks", func(t *testing.T) {
		s := newStore()
		l, ok := s.(afthread.Locker)
		if !ok {
			t.Skip("the store doesn't lease threads")
		}
		th := create(t, s, "ann")
		var wg sync.WaitGroup
		var mu sync.Mutex
		var holders []int
		for i := range 20 {
			wg.Go(func() {
				held, err := l.Lock(ctx, "ann", th.ID, fmt.Sprint("w", i), time.Minute)
				if err != nil {
					t.Error(err)
				}
				if held {
					mu.Lock()
					holders = append(holders, i)
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		if len(holders) != 1 {
			t.Fatalf("%d workers hold the lease: %v", len(holders), holders)
		}
	})

	t.Run("concurrent claims", func(t *testing.T) {
		s := newStore()
		th := create(t, s, "ann")
		for i := range 50 {
			if _, err := s.Send(ctx, "ann", th.ID, afthread.Incoming{Key: fmt.Sprint(i), Content: fmt.Sprint(i)}); err != nil {
				t.Fatal(err)
			}
		}
		var mu sync.Mutex
		var ids []int64
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				in, err := s.Claim(ctx, "ann", th.ID)
				if err != nil {
					t.Error(err)
				}
				mu.Lock()
				for _, m := range in {
					ids = append(ids, m.ID)
				}
				mu.Unlock()
			})
		}
		wg.Wait()
		slices.Sort(ids)
		if len(ids) != 50 || len(slices.Compact(slices.Clone(ids))) != 50 {
			t.Fatalf("claimed %d messages, %d distinct", len(ids), len(slices.Compact(ids)))
		}
	})
}

// sameJSON reports whether data holds the JSON value want, however a
// database formatted it (jsonb drops and adds spaces).
func sameJSON(data []byte, want string) bool {
	var a, b any
	return json.Unmarshal(data, &a) == nil && json.Unmarshal([]byte(want), &b) == nil && reflect.DeepEqual(a, b)
}
