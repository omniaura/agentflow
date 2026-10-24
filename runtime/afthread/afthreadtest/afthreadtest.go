// Package afthreadtest checks that an afthread.Store behaves as afthread
// expects. Run it against an adapter's test database:
//
//	func TestStore(t *testing.T) {
//		afthreadtest.Run(t, func() afthread.Store { return newStore(t, db) })
//	}
package afthreadtest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

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
