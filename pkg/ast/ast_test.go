package ast_test

import (
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/assert/require"
	"github.com/ditto-assistant/agentflow/pkg/ast"
)

type FileTestCase struct {
	name string
	def  func() (in []byte, want ast.File, wantErr error)
}

func (tc FileTestCase) Run(t *testing.T) {
	t.Run(tc.name, func(t *testing.T) {
		in, want, wantErr := tc.def()
		got, err := ast.NewFile(tc.name, in)
		if wantErr != nil {
			require.Equal(t, wantErr, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, want, got)
		}
	})
}

func TestNewFile(t *testing.T) {

}
