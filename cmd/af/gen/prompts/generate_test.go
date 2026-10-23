package prompts

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omniaura/agentflow/pkg/assert/require"
)

const (
	validAF   = ".title Greet\nHello <!name>\n"
	invalidAF = ".title Broken\n<*items>x\n"
	staleGo   = "// pre-existing output\npackage pkg\n"
)

// writeAF writes name.af into a package directory (so the generated package
// name is valid) and returns its path.
func writeAF(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name+".af")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func newPkgDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pkg")
	require.NoError(t, os.Mkdir(dir, 0755))
	return dir
}

func assertNoTmpFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	require.NoError(t, err)
	if len(matches) > 0 {
		t.Fatalf("expected no .tmp files, found %v", matches)
	}
}

func TestGenerateLeavesFailedOutputUntouched(t *testing.T) {
	dir := newPkgDir(t)
	good := writeAF(t, dir, "good", validAF)
	bad := writeAF(t, dir, "bad", invalidAF)
	goodOut := filepath.Join(dir, "good_af.go")
	badOut := filepath.Join(dir, "bad_af.go")
	require.NoError(t, os.WriteFile(goodOut, []byte(staleGo), 0644))
	require.NoError(t, os.WriteFile(badOut, []byte(staleGo), 0644))

	var errOut bytes.Buffer
	err := generate([]string{good, bad}, &errOut)
	if err == nil {
		t.Fatal("expected generate to fail")
	}

	gotBad, err := os.ReadFile(badOut)
	require.NoError(t, err)
	if string(gotBad) != staleGo {
		t.Fatalf("expected failed file's output to be untouched, got %q", gotBad)
	}

	gotGood, err := os.ReadFile(goodOut)
	require.NoError(t, err)
	if !strings.Contains(string(gotGood), "type Greet struct") {
		t.Fatalf("expected valid file to be regenerated, got %q", gotGood)
	}

	if want := bad + ":2:3: prompt Broken: expected <*path []Type as alias>"; !strings.Contains(errOut.String(), want) {
		t.Fatalf("expected error %q, got %q", want, errOut.String())
	}
	assertNoTmpFiles(t, dir)
}

func TestGenerateDoesNotCreateOutputForFailedFile(t *testing.T) {
	dir := newPkgDir(t)
	bad := writeAF(t, dir, "bad", invalidAF)

	if err := generate([]string{bad}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected generate to fail")
	}
	if _, err := os.Stat(filepath.Join(dir, "bad_af.go")); !os.IsNotExist(err) {
		t.Fatalf("expected no output file, stat err = %v", err)
	}
	assertNoTmpFiles(t, dir)
}

func TestGenerateFallsBackToLintPositions(t *testing.T) {
	dir := newPkgDir(t)
	// The generator reports unclosed loops without a position; lint has one.
	bad := writeAF(t, dir, "bad", ".title Broken\n<*items []string as it>x\n")

	var errOut bytes.Buffer
	if err := generate([]string{bad}, &errOut); err == nil {
		t.Fatal("expected generate to fail")
	}
	if want := bad + ":2:1: unclosed loop <*items>"; !strings.Contains(errOut.String(), want) {
		t.Fatalf("expected error %q, got %q", want, errOut.String())
	}
}

func TestGenerateSkipsUnchangedOutput(t *testing.T) {
	dir := newPkgDir(t)
	good := writeAF(t, dir, "good", validAF)
	out := filepath.Join(dir, "good_af.go")

	require.NoError(t, generate([]string{good}, &bytes.Buffer{}))
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, os.Chtimes(out, old, old))

	require.NoError(t, generate([]string{good}, &bytes.Buffer{}))
	info, err := os.Stat(out)
	require.NoError(t, err)
	if !info.ModTime().Equal(old) {
		t.Fatalf("expected mtime %v to be preserved, got %v", old, info.ModTime())
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf("expected mode 0644, got %v", info.Mode().Perm())
	}
	assertNoTmpFiles(t, dir)
}

func TestGenerateSortsErrorOutput(t *testing.T) {
	dir := newPkgDir(t)
	a := writeAF(t, dir, "a", invalidAF)
	b := writeAF(t, dir, "b", invalidAF)

	var errOut bytes.Buffer
	if err := generate([]string{b, a}, &errOut); err == nil {
		t.Fatal("expected generate to fail")
	}
	lines := strings.Split(strings.TrimSpace(errOut.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], a) || !strings.HasPrefix(lines[1], b) {
		t.Fatalf("expected sorted errors for a then b, got %q", lines)
	}
}

func TestCMDExitsOneOnFailure(t *testing.T) {
	if dir := os.Getenv("AF_GEN_HELPER_DIR"); dir != "" {
		cmd := CMD()
		cmd.SetArgs([]string{"--dir", dir})
		cmd.Execute()
		os.Exit(0)
	}

	dir := newPkgDir(t)
	writeAF(t, dir, "bad", invalidAF)

	cmd := exec.Command(os.Args[0], "-test.run=^TestCMDExitsOneOnFailure$")
	cmd.Env = append(os.Environ(), "AF_GEN_HELPER_DIR="+dir)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got err=%v output=%s", err, out)
	}
	if !strings.Contains(string(out), filepath.Join(dir, "bad.af")+":2:3:") {
		t.Fatalf("expected positioned error in output, got %s", out)
	}
}

func TestCMDAcceptsDirArgument(t *testing.T) {
	if dir := os.Getenv("AF_GEN_ARG_DIR"); dir != "" {
		cmd := CMD()
		cmd.SetArgs([]string{dir})
		cmd.Execute()
		os.Exit(0)
	}

	dir := newPkgDir(t)
	writeAF(t, dir, "good", validAF)

	cmd := exec.Command(os.Args[0], "-test.run=^TestCMDAcceptsDirArgument$")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "AF_GEN_ARG_DIR="+dir)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err)
	if _, err := os.Stat(filepath.Join(dir, "good_af.go")); err != nil {
		t.Fatalf("expected output in the argument directory, got %v (output: %s)", err, out)
	}
}

func TestPackageDirForCurrentDirectory(t *testing.T) {
	dir := newPkgDir(t)
	t.Chdir(dir)
	t.Setenv("GOPACKAGE", "")
	if got := packageDir("greet.af"); got != "pkg" {
		t.Fatalf("expected package dir pkg, got %q", got)
	}
	t.Setenv("GOPACKAGE", "api")
	if got := packageDir("greet.af"); got != "api" {
		t.Fatalf("expected $GOPACKAGE api, got %q", got)
	}
}

func TestPackageDirUsesExistingPackageClause(t *testing.T) {
	writeGo := func(t *testing.T, path, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	}
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"no go files", nil, "v1"},
		{"hand-written file", map[string]string{"api.go": "package api\n"}, "api"},
		{"hand-written file wins over stale generated file", map[string]string{
			"a_af.go": "// Code generated by agentflow v0.11.0; DO NOT EDIT.\n\npackage v1\n",
			"api.go":  "package api\n",
		}, "api"},
		{"only generated file", map[string]string{"a_af.go": "package api\n"}, "api"},
		{"test and build-ignored files skipped", map[string]string{
			"api_test.go": "package api_test\n",
			"gen.go":      "//go:build ignore\n\npackage main\n",
		}, "v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "v1")
			require.NoError(t, os.Mkdir(dir, 0755))
			for name, content := range tt.files {
				writeGo(t, filepath.Join(dir, name), content)
			}
			t.Setenv("GOPACKAGE", "")
			if got := packageDir(filepath.Join(dir, "prompts.af")); got != tt.want {
				t.Fatalf("packageDir() = %q, want %q", got, tt.want)
			}
			// Running from inside the directory, as go generate does, agrees.
			t.Chdir(dir)
			if got := packageDir("prompts.af"); got != tt.want {
				t.Fatalf("packageDir() from the directory = %q, want %q", got, tt.want)
			}
		})
	}
}
