package gogen_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/format"
	"github.com/omniaura/agentflow/pkg/gen/gogen"
)

// Formatting a .af file must never change the code generated from it.
func TestFormatPreservesGeneratedCode(t *testing.T) {
	inputs, err := filepath.Glob("testdata/*/input.golden.af")
	require.NoError(t, err)
	examples, err := filepath.Glob("../../../examples/*/*.af")
	require.NoError(t, err)
	nested, err := filepath.Glob("../../../examples/*/*/*.af")
	require.NoError(t, err)
	inputs = append(append(inputs, examples...), nested...)
	if len(inputs) < 20 {
		t.Fatalf("expected golden inputs and examples, found %d files", len(inputs))
	}

	for _, path := range inputs {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			require.NoError(t, err)
			formatted, err := format.Format(path, src)
			require.NoError(t, err)

			want, wantErr := generate(path, src)
			got, gotErr := generate(path, formatted)
			if (wantErr == nil) != (gotErr == nil) {
				t.Fatalf("formatting changed generation result: before=%v after=%v", wantErr, gotErr)
			}
			if !bytes.Equal(want, got) {
				t.Fatalf("formatting changed generated code\nbefore:\n%s\nafter:\n%s", want, got)
			}
		})
	}
}

func generate(path string, src []byte) ([]byte, error) {
	f, err := ast.NewFile(filepath.Base(path), src)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = gogen.GenFile(&buf, f, "pkg")
	return buf.Bytes(), err
}
