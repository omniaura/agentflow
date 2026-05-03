package demo

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	genprompts "github.com/omniaura/agentflow/cmd/af/gen/prompts"
	"github.com/omniaura/agentflow/pkg/assert/require"
	"github.com/omniaura/agentflow/pkg/ast"
)

func TestInitDemoCreatesRunnableProject(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agentflow-demo")

	require.NoError(t, initDemo(dir))

	expectedFiles := []string{
		"go.mod",
		"README.md",
		"main.go",
		"prompts/assistant.af",
	}
	for _, name := range expectedFiles {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	promptPath := filepath.Join(dir, "prompts", "assistant.af")
	promptContent, err := os.ReadFile(promptPath)
	require.NoError(t, err)
	parsed, err := ast.NewFile("assistant.af", promptContent)
	require.NoError(t, err)
	if len(parsed.Prompts) != 3 {
		t.Fatalf("expected 3 demo prompts, got %d", len(parsed.Prompts))
	}

	cmd := genprompts.CMD()
	cmd.SetArgs([]string{"--dir", filepath.Join(dir, "prompts")})
	require.NoError(t, cmd.Execute())

	if _, err := os.Stat(filepath.Join(dir, "prompts", "assistant_af.go")); err != nil {
		t.Fatalf("expected generated assistant_af.go to exist: %v", err)
	}
}

func TestInitDemoRejectsNonEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("keep me"), 0644))

	err := initDemo(dir)
	if err == nil {
		t.Fatal("expected non-empty directory error")
	}
	if !strings.Contains(err.Error(), "already exists and is not empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInitCommandPrintsNextSteps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	originalOutput := output
	var out bytes.Buffer
	output = &out
	t.Cleanup(func() { output = originalOutput })

	cmd := initCMD()
	cmd.SetArgs([]string{dir})
	require.NoError(t, cmd.Execute())

	text := out.String()
	if !strings.Contains(text, "af gen prompts --dir prompts") {
		t.Fatalf("expected generation hint, got %q", text)
	}
	if !strings.Contains(text, "go run .") {
		t.Fatalf("expected run hint, got %q", text)
	}
}
