package py_test

import (
	"strings"
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/assert/require"
	"github.com/ditto-assistant/agentflow/pkg/ast"
	"github.com/ditto-assistant/agentflow/pkg/gen/py"
	"github.com/ditto-assistant/agentflow/tests/testdata"
)

type TestCase struct {
	Name string
	File ast.File
	Want string
}

func TestGenerate(t *testing.T) {
	cases := []TestCase{
		{
			Name: "single prompt",
			File: testdata.File1,
			Want: `def hello_1(username: str) -> str:
	return f"""say hello to {username}"""`,
		},
		{
			Name: "single prompt with title",
			File: testdata.File2,
			Want: `def hello_user(username: str) -> str:
	return f"""say hello to {username}"""`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			var buf strings.Builder
			py.GenFile(&buf, tc.File)
			got := buf.String()
			if got != tc.Want {
				var sb strings.Builder
				sb.WriteRune('\n')
				require.WantGot(&sb, tc.Want, got)
				t.Error(sb.String())
			}
		})
	}
}
