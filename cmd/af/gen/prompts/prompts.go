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
	"bytes"
	"errors"
	"fmt"
	"go/build"
	"go/parser"
	gotoken "go/token"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/omniaura/agentflow/pkg/assert"
	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/decl"
	"github.com/omniaura/agentflow/pkg/eject"
	"github.com/omniaura/agentflow/pkg/gen/gogen"
	"github.com/omniaura/agentflow/pkg/lint"
	"github.com/omniaura/agentflow/pkg/sqlcgo"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	viper.BindPFlag("gen.dir", cmd.Flags().Lookup("dir"))
	cmd.Flags().StringVar(&Sqlc, "sqlc", "auto", "Run sqlc generate after writing changed .sql queries: auto (when sqlc is on PATH) or never")
	return cmd
}

func CMD() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompts [dir]",
		Short: "Generate prompts",
		Long: `Generate prompts from .af files in the input directory.
The generated prompts will be written next to their corresponding .af files.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 1 {
				if cmd.Flags().Changed("dir") {
					assert.NoError(fmt.Errorf("pass the directory as an argument or with --dir, not both"))
				}
				Dir = args[0]
			}
			files, err := resolveAFFiles(cmd)
			assert.NoError(err)
			if err := generate(files, os.Stderr); err != nil {
				slog.Error("Error", "error", err)
				os.Exit(1)
			}
		},
	}
	return flags(cmd)
}

// generate writes each .af file's _af.go only after the whole file has been
// generated, so a failure leaves any existing output byte-identical. Every
// failure is reported to errOut before generate returns an error.
func generate(files []string, errOut io.Writer) error {
	files = slices.Sorted(slices.Values(files))
	if err := generateSQL(files, errOut); err != nil {
		return err
	}
	failures := make([][]string, len(files))
	var group errgroup.Group
	for i, name := range files {
		group.Go(func() error {
			src, err := genFile(name)
			if err != nil {
				failures[i] = Diagnose(name, src, err)
			}
			return nil
		})
	}
	group.Wait()

	failed := 0
	for _, msgs := range failures {
		if len(msgs) == 0 {
			continue
		}
		failed++
		for _, msg := range msgs {
			fmt.Fprintln(errOut, msg)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d .af files failed to generate", failed, len(files))
	}
	return nil
}

// genFile generates the _af.go for one .af file. It returns the .af source,
// when it could be read, so errors can be mapped to positions.
func genFile(name string) ([]byte, error) {
	src, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	outFilePath, code, err := Render(name, src)
	if err != nil {
		return src, err
	}
	changed, err := writeFileAtomic(outFilePath, code)
	if err != nil {
		return src, err
	}
	if changed {
		slog.Info("Generated", "file", outFilePath)
	} else {
		slog.Info("Unchanged", "file", outFilePath)
	}
	return src, nil
}

// OutputPath returns the _af.go path generated for the .af file name.
func OutputPath(name string) string {
	return strings.TrimSuffix(name, ".af") + "_af.go"
}

// Render generates the Go source for the .af file name with contents src,
// and returns it with the path it belongs at. Nothing is written.
func Render(name string, src []byte) (outPath string, code []byte, err error) {
	ff, err := ast.NewFile(filepath.Base(name), src)
	if err != nil {
		return "", nil, err
	}
	var opts gogen.Options
	if len(ff.Tools) > 0 || len(ff.Routes) > 0 || len(ff.Outputs) > 0 || len(ff.Agents) > 0 {
		// Generated code imports the runtime, or the copy af eject made.
		if opts.Imports, err = eject.Imports(filepath.Dir(name)); err != nil {
			return "", nil, err
		}
	}
	if sqlTools := slices.ContainsFunc(ff.Tools, func(t *decl.Tool) bool { return t.SQL != "" }); len(ff.Routes) > 0 || sqlTools {
		repo, err := sqlcgo.Find(filepath.Dir(name))
		if err != nil {
			off := 0
			if len(ff.Routes) > 0 {
				off = ff.Routes[0].QueryOffset
			} else if q := sqlQueries(ff); len(q) > 0 {
				off = q[0].offset
			}
			return "", nil, &gogen.RouteError{Offset: off, Err: err}
		}
		if err := checkSQLRoutes(name, ff, repo); err != nil {
			return "", nil, err
		}
		opts.Queries = repo.Lookup
	}
	var buf bytes.Buffer
	if err := gogen.GenFileOptions(&buf, ff, packageDir(name), opts); err != nil {
		return "", nil, err
	}
	return OutputPath(name), buf.Bytes(), nil
}

// packageDir returns the name the generated package is named after. Like the
// go tool, it prefers the package clause of the Go files already in the
// directory, so af gen and af check agree wherever they run. Otherwise it uses
// $GOPACKAGE (set by go generate) for the current directory, then the
// directory name.
func packageDir(name string) string {
	dir := filepath.Dir(name)
	if pkg := goPackageName(dir); pkg != "" {
		return pkg
	}
	if dir == "." {
		if pkg := os.Getenv("GOPACKAGE"); pkg != "" {
			return pkg
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Base(dir)
}

// goPackageName returns the package declared by the Go files in dir that
// match the current build context, or "" if there are none. Hand-written
// files win over generated _af.go files, which may be stale. Test files are
// ignored.
func goPackageName(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	generated := ""
	for _, e := range entries {
		n := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		if ok, err := build.Default.MatchFile(dir, n); err != nil || !ok {
			continue
		}
		f, err := parser.ParseFile(gotoken.NewFileSet(), filepath.Join(dir, n), nil, parser.PackageClauseOnly)
		if err != nil {
			continue
		}
		if !strings.HasSuffix(n, "_af.go") {
			return f.Name.Name
		}
		if generated == "" {
			generated = f.Name.Name
		}
	}
	return generated
}

// Diagnose formats a generation error as file:line:col: message lines. It
// prefers the generator's own position, then lint errors, then the bare error.
func Diagnose(name string, src []byte, err error) []string {
	var re *gogen.RouteError
	if src != nil && errors.As(err, &re) {
		line, col := lint.Position(src, re.Offset)
		return []string{fmt.Sprintf("%s:%d:%d: %v", name, line, col, re.Err)}
	}
	var pe *gogen.PosError
	if src != nil && errors.As(err, &pe) {
		line, col := lint.Position(src, pe.Offset)
		return []string{fmt.Sprintf("%s:%d:%d: prompt %s: %v", name, line, col, pe.Prompt, pe.Err)}
	}
	if src != nil {
		var msgs []string
		diagnostics, lintErr := lint.Lint(name, src)
		if lintErr == nil {
			for _, d := range diagnostics {
				if d.Severity == lint.SeverityError {
					msgs = append(msgs, fmt.Sprintf("%s:%d:%d: %s", d.File, d.Line, d.Column, d.Message))
				}
			}
		}
		if len(msgs) > 0 {
			return msgs
		}
	}
	return []string{fmt.Sprintf("%s: %v", name, err)}
}

// writeFileAtomic replaces path with data via a temp file and rename in the
// same directory. It skips the write when the contents are unchanged so mtimes
// stay stable for build caching.
func writeFileAtomic(path string, data []byte) (changed bool, err error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return false, err
	}
	if err = tmp.Chmod(0644); err != nil {
		return false, err
	}
	if err = tmp.Close(); err != nil {
		return false, err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return false, err
	}
	return true, nil
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
