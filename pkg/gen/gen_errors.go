package gen

import "github.com/ditto-assistant/agentflow/pkg/errs"

var (
	ErrNoPrompts    = errs.New("File has no prompts")
	ErrMissingTitle = errs.New("Missing title for prompt")
)
