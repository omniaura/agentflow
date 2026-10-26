package afthread

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
)

// ErrLeaseLost means a worker lost its lease on a thread mid-run, so it
// stopped: another worker may have taken the thread.
var ErrLeaseLost = errors.New("afthread: lost the thread's lease")

// Locker leases a thread to one worker at a time, across servers, so only
// one runs its agent. A Store may implement it; Worker needs it. Memory
// does.
type Locker interface {
	// Lock leases the thread to worker for lease from now, unless another
	// worker holds a lease that hasn't expired, and reports whether it
	// did. The worker holding the lease extends it.
	Lock(ctx context.Context, owner, id, worker string, lease time.Duration) (bool, error)
	// Unlock ends worker's lease, if it holds it, and reports whether
	// messages are queued for the thread, checked after the lease ended:
	// a worker that found the thread leased left its messages to this
	// one.
	Unlock(ctx context.Context, owner, id, worker string) (queued bool, err error)
}

// DefaultLease is Worker's lease when Lease is 0.
const DefaultLease = time.Minute

// Worker answers threads' queued messages, running one agent at a time
// per thread, across servers. Call Drain after sending a message, such as
// from the request that sent it or from Messenger's Notify:
//
//	w := &afthread.Worker{Store: store, Agent: agentFor}
//	messenger.Notify = func(ctx context.Context, owner, thread string) error {
//		go w.Drain(context.WithoutCancel(ctx), owner, thread)
//		return nil
//	}
type Worker struct {
	// Store keeps the threads. It must implement Locker.
	Store Store
	// Agent returns the agent that answers on the thread, such as the
	// one its Agent field names.
	Agent func(ctx context.Context, t Thread) (Agent, error)
	// Lease is how long a worker that stopped mid-run keeps others from
	// the thread. A running worker renews it every third of it. 0 means
	// DefaultLease.
	Lease time.Duration
	// OnRun, when set, sees each run that answered messages.
	OnRun func(ctx context.Context, t Thread, res afagent.Result, err error)
}

// Drain runs the thread's agent until no messages are queued for it. When
// another worker holds the thread, Drain returns nil at once: that worker
// answers the messages, since it checks the queue as it lets go.
//
// Messages a crashed worker claimed and never finished are queued again
// first: while Drain holds the lease, no other worker can be answering
// them. If the lease is lost mid-run, the run stops with ErrLeaseLost.
// A run's error fails the messages it claimed, and Drain goes on to the
// ones queued since; it returns the runs' errors joined.
func (w *Worker) Drain(ctx context.Context, owner, id string) error {
	locker, ok := w.Store.(Locker)
	if !ok {
		return fmt.Errorf("afthread: Worker's Store, %T, doesn't implement Locker", w.Store)
	}
	lease := w.Lease
	if lease <= 0 {
		lease = DefaultLease
	}
	worker := rand.Text()
	var failed error
	for {
		held, err := locker.Lock(ctx, owner, id, worker, lease)
		if err != nil || !held {
			return errors.Join(failed, err)
		}
		err = w.run(ctx, locker, owner, id, worker, lease)
		queued, uerr := locker.Unlock(context.WithoutCancel(ctx), owner, id, worker)
		if uerr != nil {
			return errors.Join(failed, err, uerr)
		}
		var stop *stopError
		if errors.As(err, &stop) {
			// Nothing was claimed: trying again would fail the same way.
			return errors.Join(failed, stop.err)
		}
		failed = errors.Join(failed, err)
		if !queued || ctx.Err() != nil {
			return errors.Join(failed, ctx.Err())
		}
	}
}

// stopError is a failure before Run claimed anything, which ends Drain.
type stopError struct{ err error }

func (e *stopError) Error() string { return e.err.Error() }

// run answers the queued messages once, under the lease, renewing it.
func (w *Worker) run(ctx context.Context, locker Locker, owner, id, worker string, lease time.Duration) error {
	if _, err := w.Store.Reclaim(ctx, owner, id, time.Now()); err != nil {
		return &stopError{err}
	}
	t, err := w.Store.Get(ctx, owner, id)
	if err != nil {
		return &stopError{err}
	}
	agent, err := w.Agent(ctx, t)
	if err != nil {
		return &stopError{fmt.Errorf("afthread: thread %s: %w", id, err)}
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var wg sync.WaitGroup
	wg.Go(func() { renew(ctx, cancel, locker, owner, id, worker, lease) })
	defer wg.Wait()
	defer cancel(nil) // Stops renewing, before waiting for it.
	res, err := Run(ctx, w.Store, owner, id, agent)
	if c := context.Cause(ctx); errors.Is(c, ErrLeaseLost) {
		err = errors.Join(err, c)
	}
	if w.OnRun != nil && (err != nil || res.Turns > 0) {
		w.OnRun(ctx, t, res, err)
	}
	return err
}

// renew extends the lease every third of it until ctx ends, and cancels
// ctx with ErrLeaseLost once another worker holds it, or once renewing
// has failed for longer than the lease.
func renew(ctx context.Context, cancel context.CancelCauseFunc, locker Locker, owner, id, worker string, lease time.Duration) {
	tick := time.NewTicker(max(lease/3, 1))
	defer tick.Stop()
	renewed := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		held, err := locker.Lock(ctx, owner, id, worker, lease)
		switch {
		case err == nil && held:
			renewed = time.Now()
		case err == nil, time.Since(renewed) >= lease:
			cancel(ErrLeaseLost)
			return
		}
	}
}
