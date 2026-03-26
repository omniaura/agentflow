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
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/omniaura/agentflow/pkg/assert"
	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/gen/gogen"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"
)

var (
	Dir string

	interactiveInput  io.Reader = os.Stdin
	interactiveOutput io.Writer = os.Stdout
	isInteractiveTTY            = func() bool {
		file, ok := interactiveInput.(*os.File)
		return ok && term.IsTerminal(int(file.Fd()))
	}
)

func flags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().StringVarP(&Dir,
		"dir", "d", ".", "Directory to read .af files from. Defaults to current directory.")
	return cmd
}

func CMD() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompts",
		Short: "Generate prompts",
		Long: `Generate prompts from .af files in the input directory.
The generated prompts will be written next to their corresponding .af files.`,
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			files, err := resolveAFFiles(cmd)
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
					// Place the output file in the same directory as the .af file, with _af.go suffix
					outFileName := ff.Name + "_af.go"
					outFilePath := filepath.Join(filepath.Dir(name), outFileName)

					// Compute package name from directory.
					// When .af files are in the root scan directory, filepath.Base(".")
					// returns "." which is not a valid Go package name. Fall back to
					// $GOPACKAGE which go generate sets automatically.
					dirName := filepath.Base(filepath.Dir(name))
					if dirName == "." {
						if pkg := os.Getenv("GOPACKAGE"); pkg != "" {
							dirName = pkg
						}
					}

					outFile, err := os.OpenFile(outFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
					if err != nil {
						return err
					}
					defer outFile.Close()

					err = gogen.GenFile(outFile, ff, dirName)
					if err != nil {
						return err
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

func collectAFFiles(dir string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !d.IsDir() && filepath.Ext(path) == ".af" {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

func resolveAFFiles(cmd *cobra.Command) ([]string, error) {
	files, err := collectAFFiles(Dir)
	if err != nil {
		return nil, err
	}

	if !shouldPromptForFileSelection(cmd, files) {
		return files, nil
	}

	selected, err := promptForAFFiles(files)
	if err != nil {
		return nil, err
	}

	if len(selected) == 0 {
		return files, nil
	}

	return selected, nil
}

func shouldPromptForFileSelection(cmd *cobra.Command, files []string) bool {
	if len(files) <= 1 {
		return false
	}
	if cmd.Flags().Changed("dir") {
		return false
	}
	return isInteractiveTTY()
}

func promptForAFFiles(files []string) ([]string, error) {
	_, err := fmt.Fprintf(interactiveOutput, "Select .af files to generate (comma-separated numbers, blank or 'all' for all):\n")
	if err != nil {
		return nil, err
	}

	for i, file := range files {
		if _, err := fmt.Fprintf(interactiveOutput, "  %d. %s\n", i+1, file); err != nil {
			return nil, err
		}
	}

	if _, err := fmt.Fprint(interactiveOutput, "> "); err != nil {
		return nil, err
	}

	selection, err := bufio.NewReader(interactiveInput).ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}

	selection = strings.TrimSpace(selection)
	if selection == "" || strings.EqualFold(selection, "all") {
		return files, nil
	}

	chosen := make([]string, 0, len(files))
	seen := make(map[int]struct{}, len(files))

	for _, part := range strings.Split(selection, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		index, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid selection %q: enter comma-separated numbers", part)
		}
		if index < 1 || index > len(files) {
			return nil, fmt.Errorf("invalid selection %q: choose between 1 and %d", part, len(files))
		}
		if _, ok := seen[index]; ok {
			continue
		}
		seen[index] = struct{}{}
		chosen = append(chosen, files[index-1])
	}

	if len(chosen) == 0 {
		return nil, fmt.Errorf("no files selected")
	}

	return chosen, nil
}
