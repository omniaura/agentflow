package afthread_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afthread"
)

// echo answers with the user messages it was given, failing a run whose
// last message says FAIL.
func echo(running *atomic.Int32, overlaps *atomic.Int32, delay time.Duration) afthread.Agent {
	return func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
		if running.Add(1) > 1 {
			overlaps.Add(1)
		}
		defer running.Add(-1)
		msgs, err := before(ctx, 1, msgs)
		if err != nil {
			return afagent.Result{}, err
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return afagent.Result{}, ctx.Err()
		}
		if msgs[len(msgs)-1].Content == "FAIL" {
			return afagent.Result{}, errors.New("model down")
		}
		var asked []string
		for _, m := range msgs {
			if m.Role == afagent.User {
				asked = append(asked, m.Content)
			}
		}
		answer := afagent.Message{Role: afagent.Assistant, Content: "re: " + strings.Join(asked, ",")}
		return afagent.Result{Final: answer.Content, Turns: 1, Messages: append(msgs, answer)}, nil
	}
}

func TestWorkerDrainsConcurrentSends(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann", Agent: "support"})
	var running, overlaps atomic.Int32
	var runs atomic.Int32
	w := &afthread.Worker{
		Store: s,
		Agent: func(ctx context.Context, t afthread.Thread) (afthread.Agent, error) {
			if t.Agent != "support" {
				return nil, fmt.Errorf("no agent %q", t.Agent)
			}
			return echo(&running, &overlaps, time.Millisecond), nil
		},
		OnRun: func(ctx context.Context, t afthread.Thread, res afagent.Result, err error) { runs.Add(1) },
	}
	// Each request sends, then drains, as a server handling it would.
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
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
	// Every message is answered, none twice, and no run is left holding the
	// thread.
	for _, in := range s.Inbox("ann", th.ID) {
		if in.Status != afthread.Done {
			t.Errorf("message %s is %s", in.Content, in.Status)
		}
	}
	got, _ := s.Transcript(ctx, "ann", th.ID)
	users := 0
	for _, m := range got {
		if m.Role == afagent.User {
			users++
		}
	}
	if users != 40 || int(runs.Load()) != len(got)-users || runs.Load() > 40 {
		t.Errorf("%d user messages, %d runs, transcript of %d", users, runs.Load(), len(got))
	}
	if held, _ := s.Lock(ctx, "ann", th.ID, "after", time.Minute); !held {
		t.Error("the thread is still leased")
	}
}

func TestWorkerKeepsItsLease(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	var running, overlaps, calls atomic.Int32
	w := &afthread.Worker{Store: s, Lease: 30 * time.Millisecond, Agent: func(context.Context, afthread.Thread) (afthread.Agent, error) {
		calls.Add(1)
		return echo(&running, &overlaps, 150*time.Millisecond), nil
	}}
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Content: "slow"})
	done := make(chan error)
	go func() { done <- w.Drain(ctx, "ann", th.ID) }()
	// Long past the first lease, the run renews it: another worker leaves the
	// thread alone.
	time.Sleep(90 * time.Millisecond)
	if err := w.Drain(ctx, "ann", th.ID); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || overlaps.Load() != 0 {
		t.Errorf("%d runs, %d overlapping", calls.Load(), overlaps.Load())
	}
}

// stolen is a store whose leases are taken by another worker after the
// first.
type stolen struct {
	*afthread.Memory
	locks atomic.Int32
}

func (s *stolen) Lock(ctx context.Context, owner, id, worker string, lease time.Duration) (bool, error) {
	if s.locks.Add(1) > 1 {
		return false, nil
	}
	return s.Memory.Lock(ctx, owner, id, worker, lease)
}

func TestWorkerStopsWhenItLosesTheLease(t *testing.T) {
	ctx := context.Background()
	s := &stolen{Memory: &afthread.Memory{}}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	var running, overlaps atomic.Int32
	w := &afthread.Worker{Store: s, Lease: 30 * time.Millisecond, Agent: func(context.Context, afthread.Thread) (afthread.Agent, error) {
		return echo(&running, &overlaps, time.Second), nil
	}}
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Content: "slow"})
	start := time.Now()
	err := w.Drain(ctx, "ann", th.ID)
	if !errors.Is(err, afthread.ErrLeaseLost) || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("Drain = %v after %v", err, time.Since(start))
	}
	if in := s.Inbox("ann", th.ID); in[0].Status != afthread.Failed {
		t.Errorf("message = %+v", in[0])
	}
}

func TestWorkerReclaimsACrashedRun(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Content: "lost"})
	// A worker claimed it and died; its lease has lapsed.
	s.Claim(ctx, "ann", th.ID)
	var running, overlaps atomic.Int32
	w := &afthread.Worker{Store: s, Agent: func(context.Context, afthread.Thread) (afthread.Agent, error) {
		return echo(&running, &overlaps, 0), nil
	}}
	if err := w.Drain(ctx, "ann", th.ID); err != nil {
		t.Fatal(err)
	}
	if in := s.Inbox("ann", th.ID); in[0].Status != afthread.Done {
		t.Errorf("message = %+v", in[0])
	}
}

func TestWorkerFailures(t *testing.T) {
	ctx := context.Background()
	s := &afthread.Memory{}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	var running, overlaps atomic.Int32

	// A failed run fails its messages, and those queued since still run.
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Content: "FAIL"})
	first := true
	w := &afthread.Worker{Store: s, Agent: func(context.Context, afthread.Thread) (afthread.Agent, error) {
		agent := echo(&running, &overlaps, 0)
		if !first {
			return agent, nil
		}
		first = false
		return func(ctx context.Context, msgs []afagent.Message, before afthread.BeforeTurn) (afagent.Result, error) {
			s.Send(ctx, "ann", th.ID, afthread.Incoming{Content: "later"})
			return agent(ctx, msgs, before)
		}, nil
	}}
	if err := w.Drain(ctx, "ann", th.ID); err == nil || !strings.Contains(err.Error(), "model down") {
		t.Fatalf("Drain = %v", err)
	}
	in := s.Inbox("ann", th.ID)
	if len(in) != 2 || in[0].Status != afthread.Failed || in[1].Status != afthread.Done {
		t.Fatalf("inbox = %+v", in)
	}

	// No agent for the thread: the message waits, and the thread is free.
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Content: "waits"})
	w.Agent = func(context.Context, afthread.Thread) (afthread.Agent, error) { return nil, errors.New("no such agent") }
	if err := w.Drain(ctx, "ann", th.ID); err == nil || !strings.Contains(err.Error(), "no such agent") {
		t.Fatalf("Drain without an agent = %v", err)
	}
	if in := s.Inbox("ann", th.ID); in[2].Status != afthread.Queued {
		t.Errorf("message = %+v", in[2])
	}
	if held, _ := s.Lock(ctx, "ann", th.ID, "next", time.Minute); !held {
		t.Error("the thread is still leased")
	}

	// A store that can't lease threads can't have a worker.
	type plain struct{ afthread.Store }
	w.Store = plain{s}
	if err := w.Drain(ctx, "ann", th.ID); err == nil || !strings.Contains(err.Error(), "doesn't implement Locker") {
		t.Errorf("Drain on a plain store = %v", err)
	}
}

// failing is a store whose lease renewals fail.
type failing struct {
	*afthread.Memory
	locks atomic.Int32
}

func (s *failing) Lock(ctx context.Context, owner, id, worker string, lease time.Duration) (bool, error) {
	if s.locks.Add(1) > 1 {
		return false, errors.New("database down")
	}
	return s.Memory.Lock(ctx, owner, id, worker, lease)
}

// A worker that can't renew stops before its lease ends, so no other
// worker takes the thread while it still runs.
func TestWorkerStopsBeforeItsLeaseEnds(t *testing.T) {
	ctx := context.Background()
	s := &failing{Memory: &afthread.Memory{}}
	th, _ := s.Create(ctx, afthread.Thread{Owner: "ann"})
	var running, overlaps atomic.Int32
	lease := 300 * time.Millisecond
	w := &afthread.Worker{Store: s, Lease: lease, Agent: func(context.Context, afthread.Thread) (afthread.Agent, error) {
		return echo(&running, &overlaps, 10*time.Second), nil
	}}
	s.Send(ctx, "ann", th.ID, afthread.Incoming{Content: "slow"})
	start := time.Now()
	if err := w.Drain(ctx, "ann", th.ID); !errors.Is(err, afthread.ErrLeaseLost) {
		t.Fatalf("Drain = %v", err)
	}
	if took := time.Since(start); took >= lease {
		t.Errorf("stopped after %v, past the %v lease", took, lease)
	}
}
