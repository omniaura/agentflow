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
package lintcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/omniaura/agentflow/pkg/assert"
	"github.com/omniaura/agentflow/pkg/lint"
	"github.com/spf13/cobra"
)

var output io.Writer = os.Stdout

type options struct {
	dir    string
	format string
}

func CMD() *cobra.Command {
	opts := &options{format: "text"}
	cmd := &cobra.Command{
		Use:   "lint <files...>",
		Short: "Lint .af files",
		Args: func(cmd *cobra.Command, args []string) error {
			return opts.validate(args)
		},
		Run: func(cmd *cobra.Command, args []string) {
			assert.NoError(run(opts, args))
		},
	}
	cmd.Flags().StringVar(&opts.dir, "dir", "", "Recursively lint .af files in a directory")
	cmd.Flags().StringVar(&opts.format, "format", "text", "Output format: text or json")
	return cmd
}

func (opts options) validate(args []string) error {
	if opts.format != "text" && opts.format != "json" {
		return fmt.Errorf("unknown lint output format %q", opts.format)
	}
	if opts.dir != "" && len(args) > 0 {
		return fmt.Errorf("--dir cannot be combined with explicit files")
	}
	if opts.dir == "" && len(args) == 0 {
		return fmt.Errorf("provide files or --dir")
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

	var diagnostics []lint.Diagnostic
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		fileDiagnostics, err := lint.Lint(file, src)
		if err != nil {
			return err
		}
		diagnostics = append(diagnostics, fileDiagnostics...)
	}

	if opts.format == "json" {
		encoded, err := json.MarshalIndent(diagnostics, "", "  ")
		if err != nil {
			return err
		}
		if _, err := output.Write(append(encoded, '\n')); err != nil {
			return err
		}
	} else {
		for _, diagnostic := range diagnostics {
			if _, err := fmt.Fprintf(output, "%s:%d:%d: %s %s %s\n", diagnostic.File, diagnostic.Line, diagnostic.Column, diagnostic.Severity, diagnostic.Code, diagnostic.Message); err != nil {
				return err
			}
		}
	}

	if hasErrors(diagnostics) {
		return fmt.Errorf("lint found errors")
	}
	return nil
}

func hasErrors(diagnostics []lint.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == lint.SeverityError {
			return true
		}
	}
	return false
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
