package fmtcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
)

func TestStdoutModeDoesNotMutateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.af")
	original := []byte(".title   Demo\nBody <! name >")
	require.NoError(t, os.WriteFile(path, original, 0644))

	originalOutput := output
	var out bytes.Buffer
	output = &out
	t.Cleanup(func() { output = originalOutput })

	require.NoError(t, run(&options{}, []string{path}))

	current, err := os.ReadFile(path)
	require.NoError(t, err)
	if !bytes.Equal(current, original) {
		t.Fatalf("expected file to remain unchanged, got %q", string(current))
	}
	if !strings.Contains(out.String(), "<!name>") {
		t.Fatalf("expected formatted stdout, got %q", out.String())
	}
}

func TestWriteUpdatesFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.af")
	require.NoError(t, os.WriteFile(path, []byte(".title   Demo\nBody <! name >"), 0644))

	require.NoError(t, run(&options{write: true}, []string{path}))

	current, err := os.ReadFile(path)
	require.NoError(t, err)
	if string(current) != ".title Demo\n\nBody <!name>\n" {
		t.Fatalf("unexpected file content: %q", string(current))
	}
}

func TestCheckCleanSucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.af")
	require.NoError(t, os.WriteFile(path, []byte(".title Demo\n\nBody <!name>\n"), 0644))

	require.NoError(t, run(&options{check: true}, []string{path}))
}

func TestCheckDirtyFailsAndListsPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.af")
	require.NoError(t, os.WriteFile(path, []byte(".title   Demo\nBody <! name >"), 0644))

	originalOutput := output
	var out bytes.Buffer
	output = &out
	t.Cleanup(func() { output = originalOutput })

	err := run(&options{check: true}, []string{path})
	if err == nil {
		t.Fatal("expected check failure")
	}
	if !strings.Contains(out.String(), path) {
		t.Fatalf("expected changed path in output, got %q", out.String())
	}
}

func TestValidateRejectsWriteAndCheck(t *testing.T) {
	err := (options{write: true, check: true}).validate([]string{"prompt.af"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDirCollectsAFFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.af"), []byte(".title   A"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nested", "b.af"), []byte(".title   B"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte(".title   C"), 0644))

	require.NoError(t, run(&options{write: true, dir: dir}, nil))

	for _, name := range []string{"a.af", filepath.Join("nested", "b.af")} {
		content, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		if strings.Contains(string(content), "   ") {
			t.Fatalf("expected %s to be formatted, got %q", name, string(content))
		}
	}
}
