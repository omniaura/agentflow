package afthread

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
)

var _ Locker = (*Memory)(nil)

// Memory is a Store in memory, for tests and examples. The zero value is
// ready to use.
type Memory struct {
	mu      sync.Mutex
	threads map[string]*memThread
	next    int
	runs    map[string]*memRun
	nextRun int
}

type memRun struct {
	r      RunRecord
	events []RunEvent
}

type memThread struct {
	t          Thread
	transcript []afagent.Message
	inbox      []Incoming
	// holder holds the thread's lease until until.
	holder string
	until  time.Time
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

func (m *Memory) Lock(ctx context.Context, owner, id, worker string, lease time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.thread(owner, id)
	if err != nil {
		return false, err
	}
	now := time.Now()
	if t.holder != "" && t.holder != worker && now.Before(t.until) {
		return false, nil
	}
	t.holder, t.until = worker, now.Add(lease)
	return true, nil
}

func (m *Memory) Unlock(ctx context.Context, owner, id, worker string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.thread(owner, id)
	if err != nil {
		return false, err
	}
	if t.holder == worker {
		t.holder, t.until = "", time.Time{}
	}
	return slices.ContainsFunc(t.inbox, func(in Incoming) bool { return in.Status == Queued }), nil
}

func (m *Memory) StartRun(ctx context.Context, owner, thread string) (RunRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.thread(owner, thread); err != nil {
		return RunRecord{}, err
	}
	if m.runs == nil {
		m.runs = map[string]*memRun{}
	}
	m.nextRun++
	r := RunRecord{ID: fmt.Sprintf("run-%d", m.nextRun), Owner: owner, Thread: thread, Status: Running, Started: time.Now()}
	m.runs[r.ID] = &memRun{r: r}
	return r, nil
}

func (m *Memory) run(owner, id string) (*memRun, error) {
	r, ok := m.runs[id]
	if !ok || r.r.Owner != owner {
		return nil, ErrNotFound
	}
	return r, nil
}

func (m *Memory) AddEvent(ctx context.Context, owner, run string, e RunEvent) (RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.run(owner, run)
	if err != nil {
		return RunEvent{}, err
	}
	e.Seq, e.At, e.Data = int64(len(r.events)+1), time.Now(), slices.Clone(e.Data)
	r.events = append(r.events, e)
	return e, nil
}

func (m *Memory) Events(ctx context.Context, owner, run string, after int64) ([]RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.run(owner, run)
	if err != nil {
		return nil, err
	}
	var out []RunEvent
	for _, e := range r.events {
		if e.Seq > after {
			e.Data = slices.Clone(e.Data)
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *Memory) EndRun(ctx context.Context, owner, run string, end RunEnd) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.run(owner, run)
	if err != nil {
		return err
	}
	r.r.RunEnd, r.r.Ended, r.r.Status = end, time.Now(), Succeeded
	if end.Error != "" {
		r.r.Status = RunFailed
	}
	return nil
}

func (m *Memory) GetRun(ctx context.Context, owner, run string) (RunRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.run(owner, run)
	if err != nil {
		return RunRecord{}, err
	}
	return r.r, nil
}

// AgentThread returns the owner's oldest thread whose Agent is agent.
func (m *Memory) AgentThread(ctx context.Context, owner, agent string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var found *memThread
	for _, t := range m.threads {
		if t.t.Owner == owner && t.t.Agent == agent && (found == nil || t.t.Created.Before(found.t.Created) || t.t.Created.Equal(found.t.Created) && t.t.ID < found.t.ID) {
			found = t
		}
	}
	if found == nil {
		return "", ErrNoAgent
	}
	return found.t.ID, nil
}

// Agents lists the owner's agents by their threads, described by the
// oldest thread's title, in name order.
func (m *Memory) Agents(ctx context.Context, owner string) ([]AgentInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldest := map[string]*memThread{}
	for _, t := range m.threads {
		if t.t.Owner != owner || t.t.Agent == "" {
			continue
		}
		if o := oldest[t.t.Agent]; o == nil || t.t.Created.Before(o.t.Created) {
			oldest[t.t.Agent] = t
		}
	}
	out := make([]AgentInfo, 0, len(oldest))
	for name, t := range oldest {
		out = append(out, AgentInfo{Name: name, Description: t.t.Title})
	}
	slices.SortFunc(out, func(a, b AgentInfo) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
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
