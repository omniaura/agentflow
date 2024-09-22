package js_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/ditto-assistant/agentflow/pkg/assert/require"
	"github.com/ditto-assistant/agentflow/pkg/ast"
	"github.com/ditto-assistant/agentflow/pkg/gen/js"
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
			Want: "/**\n" +
				" * @returns {string}\n" +
				" */\n" +
				"export function noVarsNoTitle() {\n" +
				"	return `say hello to the user!`;\n}\n",
		},
		{
			Name:     "single prompt",
			Filename: "hello1.af",
			Content:  testdata.OneVarNoTitle,
			Want: "/**\n" +
				" * @param {string} username\n" +
				" * @returns {string}\n" +
				" */\n" +
				"export function hello1(username) {\n" +
				"	return `say hello to ${username}`;\n}\n",
		},
		{
			Name:     "single prompt with title",
			Filename: "hello2.af",
			Content:  testdata.OneVarWithTitle,
			Want: "/**\n" +
				" * @param {string} username\n" +
				" * @returns {string}\n" +
				" */\n" +
				"export function helloUser(username) {\n" +
				"	return `say hello to ${username}`;\n}\n",
		},
		{
			Name:     "two prompts with titles",
			Filename: "hello3.af",
			Content:  testdata.TwoPromptsWithVars,
			// 			Want: `def hello_user(username: str) -> str:
			// 	return f"""say hello to {username}"""
			//
			// def goodbye_user(username: str) -> str:
			// 	return f"""say goodbye to {username}"""`,
			Want: "/**\n" +
				" * @param {string} username\n" +
				" * @returns {string}\n" +
				" */\n" +
				"export function helloUser(username) {\n" +
				"	return `say hello to ${username}`;\n}\n" +
				"\n" +
				"/**\n" +
				" * @param {string} username\n" +
				" * @returns {string}\n" +
				" */\n" +
				"export function goodbyeUser(username) {\n" +
				"	return `say goodbye to ${username}`;\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			file, err := ast.NewFile(tc.Filename, []byte(tc.Content))
			require.NoError(t, err)
			var buf strings.Builder
			js.GenFile(&buf, file)
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
