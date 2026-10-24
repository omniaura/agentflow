package afthread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNoAgent means there is no agent by that name for the owner.
var ErrNoAgent = errors.New("afthread: no such agent")

// AgentInfo is an agent other agents may message.
type AgentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Directory finds agents to message. A Store may implement it; Memory
// does, from its threads' Agent fields.
type Directory interface {
	// AgentThread returns the thread that messages to agent go to, for
	// owner, or ErrNoAgent.
	AgentThread(ctx context.Context, owner, agent string) (string, error)
	// Agents lists owner's agents.
	Agents(ctx context.Context, owner string) ([]AgentInfo, error)
}

// Messenger carries messages between an owner's agents: what an agent's
// send_message and list_agents tools call. A message goes to the
// recipient's inbox, marked as from this agent, so the recipient gets it
// between its turns, or in its next run; it answers the same way, which
// makes messaging two-way.
type Messenger struct {
	Store     Store
	Directory Directory
	// Owner is whose agents these are, and From the sending agent.
	Owner, From string
	// CanMessage, when set, says whether from may message to; nil allows
	// every agent of the owner.
	CanMessage func(ctx context.Context, from, to string) bool
	// Notify, when set, is told a thread got a message, so its worker can
	// run, such as by queueing a job.
	Notify func(ctx context.Context, owner, thread string) error
}

// Send queues text for the agent named to and returns the tool result for
// the sender.
func (m *Messenger) Send(ctx context.Context, to, text string) (string, error) {
	to = strings.TrimSpace(to)
	switch {
	case to == "":
		return "", errors.New("name the agent to message")
	case to == m.From:
		return "", errors.New("an agent can't message itself")
	case strings.TrimSpace(text) == "":
		return "", errors.New("the message is empty")
	case m.CanMessage != nil && !m.CanMessage(ctx, m.From, to):
		return "", fmt.Errorf("you may not message the %s agent", to)
	}
	thread, err := m.Directory.AgentThread(ctx, m.Owner, to)
	if errors.Is(err, ErrNoAgent) {
		return "", fmt.Errorf("there is no agent named %q; list_agents shows them", to)
	}
	if err != nil {
		return "", err
	}
	if _, err := m.Store.Send(ctx, m.Owner, thread, Incoming{From: m.From, Content: text}); err != nil {
		return "", err
	}
	if m.Notify != nil {
		if err := m.Notify(ctx, m.Owner, thread); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("Sent to the %s agent. Its reply, if any, arrives as a message from it.", to), nil
}

// List returns the agents this agent may message, as JSON, for the
// list_agents tool.
func (m *Messenger) List(ctx context.Context) (string, error) {
	all, err := m.Directory.Agents(ctx, m.Owner)
	if err != nil {
		return "", err
	}
	out := []AgentInfo{}
	for _, a := range all {
		if a.Name != m.From && (m.CanMessage == nil || m.CanMessage(ctx, m.From, a.Name)) {
			out = append(out, a)
		}
	}
	b, err := json.Marshal(out)
	return string(b), err
}
