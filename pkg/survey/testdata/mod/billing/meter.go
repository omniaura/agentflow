// Package billing imports only afmodel: its Meter callback reads usage.
package billing

import "github.com/omniaura/agentflow/runtime/afmodel"

var _ = afmodel.Meter

type usage struct{ InputTokens, OutputTokens int }

type call struct{ Usage usage }

func Record(c call) int { return c.Usage.InputTokens + c.Usage.OutputTokens }
