package lintcmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
	"github.com/omniaura/agentflow/pkg/lint"
)

func TestRunExplicitFileSuccess(t *testing.T) {
	path := writeFile(t, t.TempDir(), "ok.af", ".title Ok\nBody\n")
	require.NoError(t, run(&options{format: "text"}, []string{path}))
}

func TestRunDirtyFileFailsAndPrintsDiagnostics(t *testing.T) {
	path := writeFile(t, t.TempDir(), "bad.af", ".title Bad\n</x>\n")
	originalOutput := output
	var out bytes.Buffer
	output = &out
	t.Cleanup(func() { output = originalOutput })

	err := run(&options{format: "text"}, []string{path})
	if err == nil {
		t.Fatal("expected lint failure")
	}
	if !strings.Contains(out.String(), "AF002") {
		t.Fatalf("expected AF002 output, got %q", out.String())
	}
}

func TestRunDirWalksAFFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.af", ".title A\nBody\n")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0755))
	writeFile(t, filepath.Join(dir, "nested"), "b.af", ".title B\nBody\n")
	writeFile(t, dir, "ignore.txt", "No title\n")

	require.NoError(t, run(&options{format: "text", dir: dir}, nil))
}

func TestRunJSONOutput(t *testing.T) {
	path := writeFile(t, t.TempDir(), "bad.af", ".title Bad\n</x>\n")
	originalOutput := output
	var out bytes.Buffer
	output = &out
	t.Cleanup(func() { output = originalOutput })

	_ = run(&options{format: "json"}, []string{path})
	var diagnostics []lint.Diagnostic
	require.NoError(t, json.Unmarshal(out.Bytes(), &diagnostics))
	if len(diagnostics) != 1 || diagnostics[0].Code != "AF002" {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}
