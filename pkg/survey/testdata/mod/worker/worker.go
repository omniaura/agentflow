// Package worker imports no model SDK: its retries aren't gateway code.
package worker

import (
	"context"
	"time"
)

type client struct{}

func (client) Chat(ctx context.Context) error { return nil }

func Poll(ctx context.Context, c client) {
	for attempt := 0; attempt < 3; attempt++ {
		if c.Chat(ctx) == nil {
			return
		}
		time.Sleep(time.Second)
	}
}
