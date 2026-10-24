package afthread_test

import (
	"testing"

	"github.com/omniaura/agentflow/runtime/afthread"
	"github.com/omniaura/agentflow/runtime/afthread/afthreadtest"
)

func TestMemory(t *testing.T) {
	afthreadtest.Run(t, func() afthread.Store { return &afthread.Memory{} })
}
