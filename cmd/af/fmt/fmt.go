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
package fmtcmd

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/omniaura/agentflow/pkg/assert"
	"github.com/omniaura/agentflow/pkg/format"
	"github.com/spf13/cobra"
)

var output io.Writer = os.Stdout

type options struct {
	write bool
	check bool
	dir   string
}

func CMD() *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "fmt <files...>",
		Short: "Format .af files",
		Args: func(cmd *cobra.Command, args []string) error {
			return opts.validate(args)
		},
		Run: func(cmd *cobra.Command, args []string) {
			assert.NoError(run(opts, args))
		},
	}

	cmd.Flags().BoolVar(&opts.write, "write", false, "Write formatted output back to files")
	cmd.Flags().BoolVar(&opts.check, "check", false, "Exit non-zero and list files that are not formatted")
	cmd.Flags().StringVar(&opts.dir, "dir", "", "Recursively format .af files in a directory")
	return cmd
}

func (opts options) validate(args []string) error {
	if opts.write && opts.check {
		return fmt.Errorf("--write and --check are mutually exclusive")
	}
	if opts.dir != "" && len(args) > 0 {
		return fmt.Errorf("--dir cannot be combined with explicit files")
	}
	if opts.dir == "" && len(args) == 0 {
		return fmt.Errorf("provide a file or --dir")
	}
	if opts.dir == "" && !opts.write && !opts.check && len(args) != 1 {
		return fmt.Errorf("stdout mode requires exactly one file")
	}
	if opts.dir != "" && !opts.write && !opts.check {
		return fmt.Errorf("--dir requires --write or --check")
	}
	return nil
}

func run(opts *options, args []string) error {
	files := args
	if opts.dir != "" {
		var err error
		files, err = collectAFFiles(opts.dir)
		if err != nil {
			return err
		}
	}

	changed := make([]string, 0)
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		formatted, err := format.Format(file, src)
		if err != nil {
			return err
		}

		if opts.check {
			if !bytes.Equal(src, formatted) {
				changed = append(changed, file)
			}
			continue
		}

		if opts.write {
			if !bytes.Equal(src, formatted) {
				if err := os.WriteFile(file, formatted, 0644); err != nil {
					return err
				}
			}
			continue
		}

		if _, err := output.Write(formatted); err != nil {
			return err
		}
	}

	if opts.check && len(changed) > 0 {
		for _, file := range changed {
			if _, err := fmt.Fprintln(output, file); err != nil {
				return err
			}
		}
		return fmt.Errorf("%d file(s) need formatting", len(changed))
	}

	return nil
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
	sort.Strings(files)
	return files, err
}
