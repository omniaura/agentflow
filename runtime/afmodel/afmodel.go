// Package afmodel wraps afagent models with what an LLM gateway adds:
// retrying a provider that is rate limited or overloaded, falling back to
// another provider, and metering each call for billing. The wrappers are
// afagent.Models, so they compose with each other and with adapters:
//
//	model := afmodel.Meter(
//		afmodel.Fallback(
//			afmodel.Retry(claude, errs.ExponentialRetry(3, time.Second, 10*time.Second)),
//			gpt,
//		),
//		recordUsage,
//	)
//
// A call that already streamed text to Request.OnText is never retried or
// sent to another model, so a client never sees an answer start twice.
//
// The provider SDKs retry on their own too; turn that off (for example
// option.WithMaxRetries(0)) when Retry should own the policy.
package afmodel

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"sync/atomic"
	"time"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/go-kit/errs"
)

// statusOverloaded is the status Anthropic answers with when it is
// overloaded.
const statusOverloaded = 529

// Transient reports whether err is a failed model call worth trying again:
// an *afagent.ModelError whose status is one of errs.TransientStatuses
// (408, 425, 429, 500, 502, 503, 504) or Anthropic's 529 (overloaded), or
// a network failure reaching the provider. A canceled or expired context is not transient: the caller
// gave up.
func Transient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var me *afagent.ModelError
	if errors.As(err, &me) {
		return slices.Contains(errs.TransientStatuses, me.Status) || me.Status == statusOverloaded
	}
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, io.ErrUnexpectedEOF)
}

// streamGuard wraps req.OnText to record whether any text reached it.
func streamGuard(req afagent.Request) (afagent.Request, *atomic.Bool) {
	var streamed atomic.Bool
	if on := req.OnText; on != nil {
		req.OnText = func(s string) {
			streamed.Store(true)
			on(s)
		}
	}
	return req, &streamed
}

// Retry retries m's transient failures under policy: up to
// policy.Attempts() calls, waiting policy's backoff, or the provider's
// Retry-After when that is longer. A Retry-After beyond
// policy.MaxRetryAfter ends the retries, as does a call that streamed
// text or a canceled context.
func Retry(m afagent.Model, policy errs.RetryPolicy) afagent.Model {
	return afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		req, streamed := streamGuard(req)
		for attempt := 1; ; attempt++ {
			resp, err := m.Complete(ctx, req)
			if err == nil || !Transient(err) || streamed.Load() || !policy.ShouldRetry(attempt) {
				return resp, err
			}
			delay := policy.Delay(attempt)
			var me *afagent.ModelError
			if errors.As(err, &me) && me.RetryAfter > 0 {
				if policy.MaxRetryAfter > 0 && me.RetryAfter > policy.MaxRetryAfter {
					return resp, err
				}
				delay = max(delay, me.RetryAfter)
			}
			if err := errs.Sleep(ctx, delay); err != nil {
				return afagent.Response{}, err
			}
		}
	})
}

// Fallback calls models in order, moving to the next when one fails
// transiently, and returns the last model's error when all do. A failure
// that isn't transient, such as a bad request, would fail everywhere, so
// it returns at once, as does a call that streamed text.
func Fallback(models ...afagent.Model) afagent.Model {
	return afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		if len(models) == 0 {
			return afagent.Response{}, errors.New("afmodel: Fallback has no models")
		}
		req, streamed := streamGuard(req)
		var resp afagent.Response
		var err error
		for _, m := range models {
			resp, err = m.Complete(ctx, req)
			if err == nil || !Transient(err) || streamed.Load() {
				return resp, err
			}
		}
		return resp, err
	})
}

// Call is one model call, as Meter records it.
type Call struct {
	Request  afagent.Request
	Response afagent.Response
	Err      error
	Duration time.Duration
}

// Meter calls record after each call to m, failed or not, with its usage
// in Call.Response.Usage: for billing, quotas or metrics. Wrap each
// provider to meter them apart.
func Meter(m afagent.Model, record func(ctx context.Context, c Call)) afagent.Model {
	return afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		start := time.Now()
		resp, err := m.Complete(ctx, req)
		record(ctx, Call{Request: req, Response: resp, Err: err, Duration: time.Since(start)})
		return resp, err
	})
}
