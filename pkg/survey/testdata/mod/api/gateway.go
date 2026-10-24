package api

import (
	"context"
	"errors"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

type errFactory struct{}

func (errFactory) New(ctx context.Context) error { return errors.New("x") }

var errUnknown errFactory

type pg struct{}

func (pg) CreateEndpoint(ctx context.Context) error { return nil }

type catalog struct{}

func (catalog) LookupUpstream(ctx context.Context) (int, error) { return 0, nil }

// Retry waits and tries again: afmodel.Retry.
func Retry(ctx context.Context, c anthropic.Client, p anthropic.MessageNewParams) (*anthropic.Message, error) {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		var m *anthropic.Message
		if m, err = c.Messages.New(ctx, p); err == nil {
			return m, nil
		}
		time.Sleep(time.Second << attempt)
	}
	return nil, err
}

// Valid asks again until the answer passes a check: an agent's Check.
func Valid(ctx context.Context, c anthropic.Client, p anthropic.MessageNewParams) error {
	for tries := 0; tries < 3; tries++ {
		m, err := c.Messages.New(ctx, p)
		if err == nil && len(m.Content) > 0 {
			return nil
		}
	}
	return errors.New("no valid answer")
}

// Fallback calls a backup model when the first fails: afmodel.Fallback.
func Fallback(ctx context.Context, c anthropic.Client, p anthropic.MessageNewParams) (*anthropic.Message, error) {
	m, err := c.Messages.New(ctx, p)
	if err != nil {
		p.Model = fallbackModel
		m, err = c.Messages.New(ctx, p)
	}
	return m, err
}

const fallbackModel = "claude-haiku-4-5"

// Providers tries each provider until one answers.
func Providers(ctx context.Context, providers []anthropic.Client, p anthropic.MessageNewParams) (*anthropic.Message, error) {
	var err error
	for i := range len(providers) {
		var m *anthropic.Message
		if m, err = providers[i].Messages.New(ctx, p); err == nil {
			return m, nil
		}
	}
	return nil, err
}

// Usage records what a call cost: afmodel.Meter.
func Usage(m *anthropic.Message, record func(int64, int64)) {
	record(m.Usage.InputTokens, m.Usage.OutputTokens)
}

// Not gateway code: an error factory's New, a database retry, a batch
// loop that goes on past failures, and a catalog lookup.
func NotGateway(ctx context.Context, db pg, cat catalog, candidates []string, fallback int) error {
	if err := ctx.Err(); err != nil {
		return errUnknown.New(ctx)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if db.CreateEndpoint(ctx) == nil {
			break
		}
	}
	for range candidates {
		if _, err := cat.LookupUpstream(ctx); err != nil {
			continue
		}
	}
	if _, err := cat.LookupUpstream(ctx); err != nil {
		return errors.New("fallback")
	}
	return nil
}
