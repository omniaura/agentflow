package py_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/assert/require"
	"github.com/ditto-assistant/agentflow/pkg/ast"
	"github.com/ditto-assistant/agentflow/pkg/gen/py"
	"github.com/ditto-assistant/agentflow/pkg/logger"
	"github.com/ditto-assistant/agentflow/tests/testdata"
)

func TestMain(m *testing.M) {
	logger.SetupLevel(slog.LevelDebug)
	m.Run()
}

type TestCase struct {
	Name     string
	Filename string
	Content  string
	Want     string
}

func TestGenerate(t *testing.T) {
	cases := []TestCase{
		{
			Name:     "no vars no title",
			Filename: "no_vars_no_title.af",
			Content:  testdata.NoVarsNoTitle,
			Want: `def no_vars_no_title() -> str:
	return """say hello to the user!"""`,
		},
		{
			Name:     "single prompt",
			Filename: "hello1.af",
			Content:  testdata.OneVarNoTitle,
			Want: `def hello_1(username: str) -> str:
	return f"""say hello to {username}"""`,
		},
		{
			Name:     "single prompt with title",
			Filename: "hello2.af",
			Content:  testdata.OneVarWithTitle,
			Want: `def hello_user(username: str) -> str:
	return f"""say hello to {username}"""`,
		},
		{
			Name:     "two prompts with titles",
			Filename: "hello3.af",
			Content:  testdata.TwoPromptsWithVars,
			Want: `def hello_user(username: str) -> str:
	return f"""say hello to {username}"""

def goodbye_user(username: str) -> str:
	return f"""say goodbye to {username}"""`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			file, err := ast.NewFile(tc.Filename, []byte(tc.Content))
			require.NoError(t, err)
			var buf strings.Builder
			py.GenFile(&buf, file)
			got := buf.String()
			if got != tc.Want {
				var sb strings.Builder
				sb.WriteRune('\n')
				require.WantGotBoldQuotes(&sb, tc.Want, got)
				t.Error(sb.String())
			}
		})
	}
}
