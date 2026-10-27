/*
Copyright © 2026 Omni Aura peyton@omniaura.co

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

// Package check implements af check, which verifies that committed _af.go
// files match what this af version generates and that .af files are
// formatted and lint-clean.
package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/omniaura/agentflow/cmd/af/gen/prompts"
	"github.com/omniaura/agentflow/pkg/ast"
	"github.com/omniaura/agentflow/pkg/format"
	"github.com/omniaura/agentflow/pkg/lint"
	"github.com/spf13/cobra"
)

var output io.Writer = os.Stdout

// Finding kinds.
const (
	KindDrift       = "drift"
	KindMissing     = "missing"
	KindOrphan      = "orphan"
	KindUnformatted = "unformatted"
	KindLint        = "lint"
	KindGenError    = "gen_error"
)

type Finding struct {
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Message string `json:"message"`
	Diff    string `json:"diff,omitempty"`
}

type Report struct {
	OK          bool      `json:"ok"`
	Checked     int       `json:"checked"`
	Drift       int       `json:"drift"`
	Unformatted int       `json:"unformatted"`
	LintErrors  int       `json:"lint_errors"`
	GenErrors   int       `json:"gen_errors"`
	Findings    []Finding `json:"findings"`
}

type options struct {
	json          bool
	ignoreVersion bool
}

func CMD() *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "check [dirs|./...]",
		Short: "Check that generated code is up to date and .af files are formatted and lint-clean",
		Long: `Check regenerates every _af.go in memory and compares it with the file on
disk, reporting edited, missing and orphaned outputs. It also runs fmt --check
and lint. It exits non-zero on any finding or when no .af files are found.

Patterns work like the go tool: "dir" checks one directory, "dir/..." walks it
recursively, skipping vendor, testdata and directories starting with "." or
"_". The default is "./...".`,
		Run: func(cmd *cobra.Command, args []string) {
			report, err := Run(args, opts.ignoreVersion)
			if err == nil {
				err = write(output, report, opts.json)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "af check:", err)
				os.Exit(2)
			}
			if !report.OK {
				os.Exit(1)
			}
		},
	}
	cmd.Flags().BoolVar(&opts.json, "json", false, "Print the report as JSON")
	cmd.Flags().BoolVar(&opts.ignoreVersion, "ignore-version", false,
		"Ignore the af version in generated-code headers")
	return cmd
}

// Run checks the .af files matched by patterns.
func Run(patterns []string, ignoreVersion bool) (Report, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	dirs, files, err := expand(patterns)
	if err != nil {
		return Report{}, err
	}

	report := Report{Findings: []Finding{}}
	for _, dir := range dirs {
		afs, generated, err := scanDir(dir)
		if err != nil {
			return Report{}, err
		}
		for _, name := range afs {
			if err := checkAF(&report, name, ignoreVersion); err != nil {
				return Report{}, err
			}
		}
		for _, out := range generated {
			if _, err := os.Stat(strings.TrimSuffix(out, "_af.go") + ".af"); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return Report{}, err
			}
			report.Drift++
			report.Findings = append(report.Findings, Finding{
				Kind: KindOrphan, File: out, Message: "generated file has no .af source",
			})
		}
	}
	for _, name := range files {
		if err := checkAF(&report, name, ignoreVersion); err != nil {
			return Report{}, err
		}
	}
	report.OK = report.Checked > 0 && len(report.Findings) == 0
	return report, nil
}

func checkAF(report *Report, name string, ignoreVersion bool) error {
	src, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	report.Checked++

	if formatted, err := format.Format(name, src); err == nil && !bytes.Equal(src, formatted) {
		report.Unformatted++
		report.Findings = append(report.Findings, Finding{
			Kind: KindUnformatted, File: name, Message: "not formatted; run af fmt --write",
			Diff: unifiedDiff(name, name+" (formatted)", src, formatted),
		})
	}

	if diagnostics, err := lint.Lint(name, src); err == nil {
		for _, d := range diagnostics {
			if d.Severity != lint.SeverityError {
				continue
			}
			report.LintErrors++
			report.Findings = append(report.Findings, Finding{
				Kind: KindLint, File: name, Line: d.Line, Column: d.Column, Message: d.Code + " " + d.Message,
			})
		}
	}

	// Queries a .af file writes go to its _af.sql, which sqlc reads.
	if f, err := ast.NewFile(filepath.Base(name), src); err == nil {
		sqlPath := prompts.SQLPath(name)
		want := prompts.RenderSQL(f)
		disk, err := os.ReadFile(sqlPath)
		switch {
		case want == nil:
		case os.IsNotExist(err):
			report.Drift++
			report.Findings = append(report.Findings, Finding{
				Kind: KindMissing, File: sqlPath, Message: "generated queries are missing; run af gen prompts",
				Diff: unifiedDiff("/dev/null", sqlPath, nil, want),
			})
		case err != nil:
			return err
		case !bytes.Equal(disk, want):
			report.Drift++
			report.Findings = append(report.Findings, Finding{
				Kind: KindDrift, File: sqlPath, Message: "generated queries are out of date; run af gen prompts",
				Diff: unifiedDiff(sqlPath, sqlPath+" (generated)", disk, want),
			})
		}
	}

	outPath, code, err := prompts.Render(name, src)
	if err != nil {
		report.GenErrors++
		for _, msg := range prompts.Diagnose(name, src, err) {
			report.Findings = append(report.Findings, Finding{Kind: KindGenError, File: name, Message: msg})
		}
		return nil
	}

	disk, err := os.ReadFile(outPath)
	if os.IsNotExist(err) {
		report.Drift++
		report.Findings = append(report.Findings, Finding{
			Kind: KindMissing, File: outPath, Message: "generated file is missing; run af gen prompts",
			Diff: unifiedDiff("/dev/null", outPath, nil, code),
		})
		return nil
	} else if err != nil {
		return err
	}
	if ignoreVersion {
		disk = withHeaderVersionOf(disk, code)
	}
	if !bytes.Equal(disk, code) {
		report.Drift++
		report.Findings = append(report.Findings, Finding{
			Kind: KindDrift, File: outPath, Message: "generated file is out of date; run af gen prompts",
			Diff: unifiedDiff(outPath, outPath+" (generated)", disk, code),
		})
	}
	return nil
}

var headerVersion = regexp.MustCompile(`^// Code generated by agentflow v[^;\n]*;`)

// withHeaderVersionOf replaces the version in disk's generated-code header
// with the one in code.
func withHeaderVersionOf(disk, code []byte) []byte {
	want := headerVersion.Find(code)
	if want == nil || !headerVersion.Match(disk) {
		return disk
	}
	return headerVersion.ReplaceAllLiteral(disk, want)
}

// expand resolves patterns into directories to scan and individual .af files.
func expand(patterns []string) (dirs, files []string, err error) {
	seen := map[string]bool{}
	addDir := func(dir string) {
		dir = filepath.Clean(dir)
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	for _, pattern := range patterns {
		if root, ok := strings.CutSuffix(pattern, "..."); ok {
			root = filepath.Clean(strings.TrimSuffix(root, "/"))
			if root == "" {
				root = "."
			}
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					return nil
				}
				if path != root && skipDir(d.Name()) {
					return filepath.SkipDir
				}
				addDir(path)
				return nil
			})
			if err != nil {
				return nil, nil, err
			}
			continue
		}
		info, err := os.Stat(pattern)
		if err != nil {
			return nil, nil, err
		}
		switch {
		case info.IsDir():
			addDir(pattern)
		case filepath.Ext(pattern) == ".af":
			files = append(files, filepath.Clean(pattern))
		default:
			return nil, nil, fmt.Errorf("%s is not a directory or .af file", pattern)
		}
	}
	slices.Sort(dirs)
	slices.Sort(files)
	return dirs, slices.Compact(files), nil
}

func skipDir(name string) bool {
	return name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// scanDir lists the .af files and agentflow-generated _af.go files in dir.
// Symlinks are skipped.
func scanDir(dir string) (afs, generated []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		switch {
		case strings.HasSuffix(e.Name(), "_af.go"):
			ok, err := isGenerated(path)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				generated = append(generated, path)
			}
		case filepath.Ext(e.Name()) == ".af":
			afs = append(afs, path)
		}
	}
	return afs, generated, nil
}

func isGenerated(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	head := make([]byte, 64)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return false, err
	}
	return headerVersion.Match(head[:n]), nil
}

func write(w io.Writer, report Report, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	for _, f := range report.Findings {
		var err error
		switch {
		case f.Kind == KindGenError:
			_, err = fmt.Fprintf(w, "%s\n", f.Message)
		case f.Line > 0:
			_, err = fmt.Fprintf(w, "%s:%d:%d: %s: %s\n", f.File, f.Line, f.Column, f.Kind, f.Message)
		default:
			_, err = fmt.Fprintf(w, "%s: %s: %s\n", f.File, f.Kind, f.Message)
		}
		if err == nil && f.Diff != "" {
			_, err = io.WriteString(w, f.Diff)
		}
		if err != nil {
			return err
		}
	}
	verdict := "ok"
	if !report.OK {
		verdict = "FAIL"
	}
	note := ""
	if report.Checked == 0 {
		note = " (no .af files found)"
	}
	_, err := fmt.Fprintf(w, "%s checked=%d drift=%d unformatted=%d lint_errors=%d gen_errors=%d%s\n",
		verdict, report.Checked, report.Drift, report.Unformatted, report.LintErrors, report.GenErrors, note)
	return err
}
