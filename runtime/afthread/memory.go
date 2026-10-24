package afthread

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
)

// Memory is a Store in memory, for tests and examples. The zero value is
// ready to use.
type Memory struct {
	mu      sync.Mutex
	threads map[string]*memThread
	next    int
}

type memThread struct {
	t          Thread
	transcript []afagent.Message
	inbox      []Incoming
}

func (m *Memory) thread(owner, id string) (*memThread, error) {
	t, ok := m.threads[id]
	if !ok || t.t.Owner != owner {
		return nil, ErrNotFound
	}
	return t, nil
}

func (m *Memory) Create(ctx context.Context, t Thread) (Thread, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.threads == nil {
		m.threads = map[string]*memThread{}
	}
	if t.ID == "" {
		m.next++
		t.ID = fmt.Sprintf("thread-%d", m.next)
	}
	if _, ok := m.threads[t.ID]; ok {
		return Thread{}, fmt.Errorf("afthread: thread %s exists", t.ID)
	}
	now := time.Now()
	t.Created, t.Updated = now, now
	t.Metadata = maps.Clone(t.Metadata)
	m.threads[t.ID] = &memThread{t: t}
	// The caller's copy shares nothing with the stored one.
	t.Metadata = maps.Clone(t.Metadata)
	return t, nil
}

func (m *Memory) Get(ctx context.Context, owner, id string) (Thread, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.thread(owner, id)
	if err != nil {
		return Thread{}, err
	}
	out := t.t
	out.Metadata = maps.Clone(out.Metadata)
	return out, nil
}

func (m *Memory) Transcript(ctx context.Context, owner, id string) ([]afagent.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.thread(owner, id)
	if err != nil {
		return nil, err
	}
	return cloneMessages(t.transcript), nil
}

func (m *Memory) Append(ctx context.Context, owner, id string, msgs ...afagent.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.thread(owner, id)
	if err != nil {
		return err
	}
	t.transcript = append(t.transcript, cloneMessages(msgs)...)
	t.t.Updated = time.Now()
	return nil
}

func (m *Memory) Replace(ctx context.Context, owner, id string, msgs []afagent.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.thread(owner, id)
	if err != nil {
		return err
	}
	t.transcript = cloneMessages(msgs)
	t.t.Updated = time.Now()
	return nil
}

func (m *Memory) Send(ctx context.Context, owner, id string, in Incoming) (Incoming, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.thread(owner, id)
	if err != nil {
		return Incoming{}, err
	}
	if in.Key != "" {
		if i := slices.IndexFunc(t.inbox, func(x Incoming) bool { return x.Key == in.Key }); i >= 0 {
			return t.inbox[i], nil
		}
	}
	in.ID = int64(len(t.inbox) + 1)
	in.Status, in.Error, in.At = Queued, "", time.Now()
	t.inbox = append(t.inbox, in)
	return in, nil
}

func (m *Memory) Claim(ctx context.Context, owner, id string) ([]Incoming, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.thread(owner, id)
	if err != nil {
		return nil, err
	}
	var out []Incoming
	for i := range t.inbox {
		if t.inbox[i].Status == Queued {
			t.inbox[i].Status, t.inbox[i].Claimed = Started, time.Now()
			out = append(out, t.inbox[i])
		}
	}
	return out, nil
}

func (m *Memory) Finish(ctx context.Context, owner, id string, ids []int64, failure error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.thread(owner, id)
	if err != nil {
		return err
	}
	for i := range t.inbox {
		if t.inbox[i].Status == Started && slices.Contains(ids, t.inbox[i].ID) {
			t.inbox[i].Status = Done
			if failure != nil {
				t.inbox[i].Status, t.inbox[i].Error = Failed, failure.Error()
			}
		}
	}
	return nil
}

func (m *Memory) Reclaim(ctx context.Context, owner, id string, before time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.thread(owner, id)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range t.inbox {
		if t.inbox[i].Status == Started && t.inbox[i].Claimed.Before(before) {
			t.inbox[i].Status = Queued
			n++
		}
	}
	return n, nil
}

// cloneMessages copies msgs, their tool calls and native payloads, so the
// store and its callers share nothing they could change.
func cloneMessages(msgs []afagent.Message) []afagent.Message {
	out := slices.Clone(msgs)
	for i := range out {
		out[i].ToolCalls = slices.Clone(out[i].ToolCalls)
		if n := out[i].Native; n != nil {
			out[i].Native = &afagent.Native{Provider: n.Provider, Data: slices.Clone(n.Data)}
		}
	}
	return out
}

// Inbox returns the thread's incoming messages, for tests.
func (m *Memory) Inbox(owner, id string) []Incoming {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.thread(owner, id)
	if err != nil {
		return nil
	}
	return slices.Clone(t.inbox)
}
