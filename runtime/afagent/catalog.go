package afagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// ToolCatalog is the immutable, per-run set of declared and host-provided
// tools. Dispatch uses exactly the names offered to the model.
type ToolCatalog struct {
	Tools      []ToolSpec
	declared   map[string]bool
	additional map[string]bool
}

// PrepareTools snapshots a catalog and applies allow once per name. Additional
// tools are sorted by name for stable provider prompts. Name collisions are
// refused even when allow would hide one of them; an untrusted dynamic source
// cannot shadow a generated tool or a different connection's tool.
func PrepareTools(ctx context.Context, declared, additional []ToolSpec, allow func(context.Context, string) bool) (*ToolCatalog, error) {
	c := &ToolCatalog{declared: map[string]bool{}, additional: map[string]bool{}}
	seen := map[string]bool{}
	add := func(t ToolSpec, dynamic bool) error {
		if t.Name == "" || seen[t.Name] {
			return fmt.Errorf("duplicate or empty tool name %q", t.Name)
		}
		seen[t.Name] = true
		if dynamic {
			trimmed := bytes.TrimSpace(t.Schema)
			if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
				return fmt.Errorf("tool %q has an invalid JSON object schema", t.Name)
			}
		}
		if allow != nil && !allow(ctx, t.Name) {
			return nil
		}
		t.Schema = bytes.Clone(t.Schema)
		c.Tools = append(c.Tools, t)
		if dynamic {
			c.additional[t.Name] = true
		} else {
			c.declared[t.Name] = true
		}
		return nil
	}
	for _, t := range declared {
		if err := add(t, false); err != nil {
			return nil, err
		}
	}
	extra := append([]ToolSpec(nil), additional...)
	sort.SliceStable(extra, func(i, j int) bool { return extra[i].Name < extra[j].Name })
	for _, t := range extra {
		if err := add(t, true); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Dispatch rejects any name that was not offered before reaching either host
// callback. The host callbacks remain responsible for their authorization and
// argument validation.
func (c *ToolCatalog) Dispatch(ctx context.Context, call ToolCall, declared, additional func(context.Context, ToolCall) (string, error)) (string, error) {
	if c.declared[call.Name] && declared != nil {
		return declared(ctx, call)
	}
	if c.additional[call.Name] && additional != nil {
		return additional(ctx, call)
	}
	return "", fmt.Errorf("tool %s is not available in this conversation", call.Name)
}
