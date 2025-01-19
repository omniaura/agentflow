/*
Copyright © 2024 Omni Aura peyton@omniaura.co

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package gogen_test

import (
	"strings"
	"testing"

	"github.com/omniaura/agentflow/cfg"
	"github.com/omniaura/agentflow/pkg/assert/require"
	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/gen/gogen"
	"github.com/omniaura/agentflow/pkg/logger"
	"github.com/omniaura/agentflow/tests/testdata"
)

func TestMain(m *testing.M) {
	cfg.TestMode()
	logger.Setup()
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
			Want: "package novarsnotitle\n\n" +
				"func NoVarsNoTitle() string {\n" +
				"\treturn `say hello to the user!`\n}\n",
		},
		{
			Name:     "single prompt",
			Filename: "hello1.af",
			Content:  testdata.OneVarNoTitle,
			Want: "package hello1\n\n" +
				"import \"strings\"\n\n" +
				"func Hello1(username string) string {\n" +
				"\tvar b strings.Builder\n" +
				"\tb.Grow(13 + len(username))\n" +
				"\tb.WriteString(`say hello to `)\n" +
				"\tb.WriteString(username)\n" +
				"\treturn b.String()\n}\n",
		},
		{
			Name:     "single prompt with title",
			Filename: "hello2.af",
			Content:  testdata.OneVarWithTitle,
			Want: "package hello2\n\n" +
				"import \"strings\"\n\n" +
				"func HelloUser(username string) string {\n" +
				"\tvar b strings.Builder\n" +
				"\tb.Grow(13 + len(username))\n" +
				"\tb.WriteString(`say hello to `)\n" +
				"\tb.WriteString(username)\n" +
				"\treturn b.String()\n}\n",
		},
		{
			Name:     "two prompts with titles",
			Filename: "hello3.af",
			Content:  testdata.TwoPromptsWithVars,
			Want: "package hello3\n\n" +
				"import \"strings\"\n\n" +
				"func HelloUser(username string) string {\n" +
				"\tvar b strings.Builder\n" +
				"\tb.Grow(13 + len(username))\n" +
				"\tb.WriteString(`say hello to `)\n" +
				"\tb.WriteString(username)\n" +
				"\treturn b.String()\n}\n" +
				"\n" +
				"func GoodbyeUser(username string) string {\n" +
				"\tvar b strings.Builder\n" +
				"\tb.Grow(15 + len(username))\n" +
				"\tb.WriteString(`say goodbye to `)\n" +
				"\tb.WriteString(username)\n" +
				"\treturn b.String()\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			file, err := ast.NewFile(tc.Filename, []byte(tc.Content))
			require.NoError(t, err)
			var buf strings.Builder
			gogen.GenFile(&buf, file)
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
