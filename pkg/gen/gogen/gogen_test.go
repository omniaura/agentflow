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
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/cfg"
	"github.com/omniaura/agentflow/pkg/assert/require"
	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/gen/gogen"
	"github.com/omniaura/agentflow/pkg/logger"
)

var update = flag.Bool("update", false, "update golden files")

func TestMain(m *testing.M) {
	cfg.TestMode()
	logger.Setup()
	m.Run()
}

func TestGenerate(t *testing.T) {
	testdataDir := "testdata"

	// Walk the testdata directory to find all test cases
	entries, err := os.ReadDir(testdataDir)
	require.NoError(t, err)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		testCaseName := entry.Name()
		t.Run(testCaseName, func(t *testing.T) {
			testCaseDir := filepath.Join(testdataDir, testCaseName)

			// Read input file
			inputPath := filepath.Join(testCaseDir, "input.golden.af")
			inputBytes, err := os.ReadFile(inputPath)
			require.NoError(t, err)

			// Create AST file with the test case name as filename
			filename := testCaseName + ".af"
			file, err := ast.NewFile(filename, inputBytes)
			require.NoError(t, err)

			// Generate output
			var buf strings.Builder
			gogen.GenFile(&buf, file, testCaseName)
			got := buf.String()

			// Read expected output
			outputPath := filepath.Join(testCaseDir, "output.golden.go")

			if *update {
				// Update the golden file
				err := os.WriteFile(outputPath, []byte(got), 0644)
				require.NoError(t, err)
				return
			}

			wantBytes, err := os.ReadFile(outputPath)
			require.NoError(t, err)
			want := string(wantBytes)

			// Compare
			if got != want {
				var sb strings.Builder
				sb.WriteRune('\n')
				require.WantGotBoldQuotes(&sb, want, got)
				t.Error(sb.String())
			}
		})
	}
}
