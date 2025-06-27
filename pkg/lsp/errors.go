package lsp

import "github.com/omniaura/agentflow/pkg/errs"

var (
	ErrFailedToOpenDocument   = errs.New("failed to open document")
	ErrFailedToUpdateDocument = errs.New("failed to update document")
)
