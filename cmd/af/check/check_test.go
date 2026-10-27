package check

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/cfg"
	"github.com/omniaura/agentflow/cmd/af/gen/prompts"
	"github.com/omniaura/agentflow/pkg/assert/require"
)

const greetAF = ".title Greet\n\nHello <!name>\n"

// newPkg creates dir/pkg with greet.af and its up-to-date greet_af.go.
func newPkg(t *testing.T) string {
	t.Helper()
	cfg.TestMode()
	dir := filepath.Join(t.TempDir(), "pkg")
	require.NoError(t, os.Mkdir(dir, 0755))
	writeFile(t, filepath.Join(dir, "greet.af"), greetAF)
	writeGenerated(t, filepath.Join(dir, "greet.af"))
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func writeGenerated(t *testing.T, name string) {
	t.Helper()
	src, err := os.ReadFile(name)
	require.NoError(t, err)
	out, code, err := prompts.Render(name, src)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(out, code, 0644))
}

func run(t *testing.T, patterns ...string) Report {
	t.Helper()
	report, err := Run(patterns, false)
	require.NoError(t, err)
	return report
}

func kinds(r Report) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, f.Kind)
	}
	return out
}

func TestCheckCleanPackage(t *testing.T) {
	dir := newPkg(t)
	r := run(t, dir)
	if !r.OK || r.Checked != 1 || len(r.Findings) != 0 {
		t.Fatalf("expected clean report, got %+v", r)
	}
}

func TestCheckDetectsFindings(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		kind  string
		count func(Report) int
	}{
		{"edited .af", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "greet.af"), ".title Greet\n\nHi <!name>\n")
		}, KindDrift, func(r Report) int { return r.Drift }},
		{"hand-edited _af.go", func(t *testing.T, dir string) {
			path := filepath.Join(dir, "greet_af.go")
			b, err := os.ReadFile(path)
			require.NoError(t, err)
			writeFile(t, path, strings.Replace(string(b), "Hello ", "Howdy ", 1))
		}, KindDrift, func(r Report) int { return r.Drift }},
		{"missing _af.go", func(t *testing.T, dir string) {
			require.NoError(t, os.Remove(filepath.Join(dir, "greet_af.go")))
		}, KindMissing, func(r Report) int { return r.Drift }},
		{"orphan _af.go", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "old.af"), ".title Old\n\nBye\n")
			writeGenerated(t, filepath.Join(dir, "old.af"))
			require.NoError(t, os.Remove(filepath.Join(dir, "old.af")))
		}, KindOrphan, func(r Report) int { return r.Drift }},
		{"unformatted .af", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "greet.af"), ".title Greet\nHello <!   name  >")
		}, KindUnformatted, func(r Report) int { return r.Unformatted }},
		{"lint error", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "greet.af"), ".title Greet\n\n<?count >= 3>\n</count>\n")
		}, KindLint, func(r Report) int { return r.LintErrors }},
		{"generation error", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "greet.af"), ".title Greet\n\n<*items>x\n")
		}, KindGenError, func(r Report) int { return r.GenErrors }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newPkg(t)
			tt.setup(t, dir)
			r := run(t, dir)
			if r.OK {
				t.Fatalf("expected failure, got %+v", r)
			}
			found := false
			for _, k := range kinds(r) {
				found = found || k == tt.kind
			}
			if !found {
				t.Fatalf("expected a %s finding, got %v", tt.kind, kinds(r))
			}
			if tt.count(r) == 0 {
				t.Fatalf("expected %s to be counted, got %+v", tt.kind, r)
			}
		})
	}
}

func TestCheckDriftIncludesUnifiedDiff(t *testing.T) {
	dir := newPkg(t)
	writeFile(t, filepath.Join(dir, "greet.af"), ".title Greet\n\nHi <!name>\n")
	r := run(t, dir)
	var buf bytes.Buffer
	require.NoError(t, write(&buf, r, false))
	out := buf.String()
	for _, want := range []string{
		filepath.Join(dir, "greet_af.go") + ": drift:",
		"--- " + filepath.Join(dir, "greet_af.go") + "\n",
		"+++ " + filepath.Join(dir, "greet_af.go") + " (generated)\n",
		`-	b.WriteString("Hello ")`,
		`+	b.WriteString("Hi ")`,
		"FAIL checked=1 drift=1 unformatted=0 lint_errors=0 gen_errors=0\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestCheckZeroFilesFails(t *testing.T) {
	r := run(t, t.TempDir())
	if r.OK || r.Checked != 0 {
		t.Fatalf("expected a failing zero-file report, got %+v", r)
	}
	var buf bytes.Buffer
	require.NoError(t, write(&buf, r, false))
	if want := "FAIL checked=0 drift=0 unformatted=0 lint_errors=0 gen_errors=0 (no .af files found)\n"; buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}

func TestCheckRecursivePatternSkipsLikeGoTool(t *testing.T) {
	cfg.TestMode()
	root := t.TempDir()
	for _, sub := range []string{"a", "a/b", "vendor/v", "testdata", ".hidden", "_skip"} {
		dir := filepath.Join(root, sub)
		require.NoError(t, os.MkdirAll(dir, 0755))
		writeFile(t, filepath.Join(dir, "greet.af"), greetAF)
		if !skipDir(filepath.Base(dir)) || sub == "testdata" {
			writeGenerated(t, filepath.Join(dir, "greet.af"))
		}
	}

	if r := run(t, filepath.Join(root, "...")); !r.OK || r.Checked != 2 {
		t.Fatalf("expected a and a/b only, got %+v", r)
	}
	if r := run(t, filepath.Join(root, "a")); !r.OK || r.Checked != 1 {
		t.Fatalf("expected a only without ..., got %+v", r)
	}
	// An explicitly named skipped directory is still checked.
	if r := run(t, filepath.Join(root, "testdata")); !r.OK || r.Checked != 1 {
		t.Fatalf("expected the named testdata dir to be checked, got %+v", r)
	}
}

func TestCheckDefaultsToRecursiveCurrentDir(t *testing.T) {
	dir := newPkg(t)
	t.Chdir(filepath.Dir(dir))
	if r := run(t); !r.OK || r.Checked != 1 {
		t.Fatalf("expected ./... to find pkg/greet.af, got %+v", r)
	}
}

func TestCheckIgnoreVersion(t *testing.T) {
	dir := newPkg(t)
	path := filepath.Join(dir, "greet_af.go")
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	writeFile(t, path, strings.Replace(string(b), "agentflow v"+cfg.Version+";", "agentflow v0.0.1;", 1))

	if r := run(t, dir); r.OK || r.Drift != 1 {
		t.Fatalf("expected header version drift, got %+v", r)
	}
	r, err := Run([]string{dir}, true)
	require.NoError(t, err)
	if !r.OK {
		t.Fatalf("expected --ignore-version to pass, got %+v", r)
	}
}

func TestCheckIgnoresHandWrittenAFGoFiles(t *testing.T) {
	dir := newPkg(t)
	writeFile(t, filepath.Join(dir, "helpers_af.go"), "package pkg\n")
	if r := run(t, dir); !r.OK {
		t.Fatalf("expected non-generated _af.go to be ignored, got %+v", r)
	}
}

func TestCheckJSON(t *testing.T) {
	dir := newPkg(t)
	require.NoError(t, os.Remove(filepath.Join(dir, "greet_af.go")))
	var buf bytes.Buffer
	require.NoError(t, write(&buf, run(t, dir), true))

	var got Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	if got.OK || got.Checked != 1 || got.Drift != 1 || len(got.Findings) != 1 || got.Findings[0].Kind != KindMissing {
		t.Fatalf("unexpected JSON report: %s", buf.String())
	}
}

func TestUnifiedDiff(t *testing.T) {
	a := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
	b := "one\ntwo\nTHREE\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven"
	want := `--- a
+++ b
@@ -1,6 +1,6 @@
 one
 two
-three
+THREE
 four
 five
 six
@@ -8,3 +8,4 @@
 eight
 nine
 ten
+eleven
\ No newline at end of file
`
	if got := unifiedDiff("a", "b", []byte(a), []byte(b)); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := unifiedDiff("a", "b", []byte(a), []byte(a)); got != "" {
		t.Fatalf("expected no diff for equal input, got %q", got)
	}
}
