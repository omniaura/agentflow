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
package prompts

import (
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/omniaura/agentflow/pkg/assert"
	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/gen/js"
	"github.com/omniaura/agentflow/pkg/gen/py"
	"github.com/omniaura/agentflow/pkg/gen/ts"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var (
	Dir  string
	Lang string
)

func flags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().StringVarP(&Dir,
		"dir", "d", ".", "Directory to read .af files from. Defaults to current directory.")
	cmd.Flags().StringVarP(&Lang,
		"lang", "l", "py", "Language to generate prompts for. Defaults to py.")
	return cmd
}

func CMD() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompts",
		Short: "Generate prompts for specified languages",
		Long: `Generate prompts for specified languages from .af files in the input directory.
The generated prompts will be written next to their corresponding .af files.`,
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			// Use filepath.Walk to recursively find all .af files
			var files []string
			err := filepath.WalkDir(Dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && filepath.Ext(path) == ".af" {
					files = append(files, path)
				}
				return nil
			})
			assert.NoError(err)

			group, _ := errgroup.WithContext(ctx)
			for _, name := range files {
				group.Go(func() error {
					file, err := os.Open(name)
					if err != nil {
						return err
					}
					defer file.Close()
					f, err := io.ReadAll(file)
					if err != nil {
						return err
					}

					fName := filepath.Base(file.Name())
					ff, err := ast.NewFile(fName, f)
					if err != nil {
						return err
					}

					// Generate the output file in the same directory as the input file
					outFileName := fmt.Sprintf("%s.%s", ff.Name, Lang)
					outFilePath := filepath.Join(filepath.Dir(name), outFileName)
					outFile, err := os.OpenFile(outFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
					if err != nil {
						return err
					}
					defer outFile.Close()

					var genErr error
					switch Lang {
					case "py":
						genErr = py.GenFile(outFile, ff)
					case "js":
						genErr = js.GenFile(outFile, ff)
					case "ts":
						genErr = ts.GenFile(outFile, ff)
					default:
						return fmt.Errorf("unsupported language: %s", Lang)
					}

					if genErr != nil {
						return genErr
					}
					slog.Info("Generated", "file", outFilePath)
					return nil
				})
			}

			if err := group.Wait(); err != nil {
				slog.Error("Error", "error", err)
				os.Exit(1)
			}
		},
	}
	return flags(cmd)
}
