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
	"os"
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
}

func TestGenerate(t *testing.T) {
	cases := []TestCase{
		{
			Name:     "no_vars_no_title",
			Filename: "no_vars_no_title.af",
			Content:  testdata.NoVarsNoTitle,
		},
		{
			Name:     "single_prompt",
			Filename: "hello1.af",
			Content:  testdata.OneVarNoTitle,
		},
		{
			Name:     "single_prompt_with_title",
			Filename: "hello2.af",
			Content:  testdata.OneVarWithTitle,
		},
		{
			Name:     "two_prompts_with_titles",
			Filename: "hello3.af",
			Content:  testdata.TwoPromptsWithVars,
		},
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			file, err := ast.NewFile(tc.Filename, []byte(tc.Content))
			require.NoError(t, err)
			var buf strings.Builder
			gogen.GenFile(&buf, file)
			got := buf.String()

			goldenPath := "testdata/" + tc.Name + ".golden.go"
			wantBytes, err := os.ReadFile(goldenPath)
			require.NoError(t, err)
			want := string(wantBytes)

			if got != want {
				var sb strings.Builder
				sb.WriteRune('\n')
				require.WantGotBoldQuotes(&sb, want, got)
				t.Error(sb.String())
			}
		})
	}
}
