package prompts

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
	"github.com/spf13/cobra"
)

func TestCollectAFFilesSkipsSymlinks(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "root.af"), []byte(".title Root\nHello"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "nested"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested", "child.af"), []byte(".title Child\nHello"), 0644))
	require.NoError(t, os.Symlink(filepath.Join(root, "nested", "child.af"), filepath.Join(root, "linked.af")))

	files, err := collectAFFiles(root)
	require.NoError(t, err)

	if len(files) != 2 {
		t.Fatalf("expected 2 real .af files, got %d: %#v", len(files), files)
	}
}

func TestShouldPromptForFileSelection(t *testing.T) {
	cmd := &cobra.Command{Use: "prompts"}
	cmd.Flags().String("dir", ".", "")

	files := []string{"one.af", "two.af"}
	originalTTY := isInteractiveTTY
	isInteractiveTTY = func() bool { return true }
	t.Cleanup(func() { isInteractiveTTY = originalTTY })

	if !shouldPromptForFileSelection(cmd, files) {
		t.Fatal("expected interactive prompt when dir flag is not set and tty is interactive")
	}

	require.NoError(t, cmd.Flags().Set("dir", "nested"))
	if shouldPromptForFileSelection(cmd, files) {
		t.Fatal("expected dir flag to disable interactive selection")
	}
}

func TestPromptForAFFilesSelectsSubset(t *testing.T) {
	originalInput := interactiveInput
	originalOutput := interactiveOutput
	interactiveInput = strings.NewReader("2, 1\n")
	var out bytes.Buffer
	interactiveOutput = &out
	t.Cleanup(func() {
		interactiveInput = originalInput
		interactiveOutput = originalOutput
	})

	files := []string{"one.af", "two.af", "three.af"}
	selected, err := promptForAFFiles(files)
	require.NoError(t, err)

	if len(selected) != 2 || selected[0] != "two.af" || selected[1] != "one.af" {
		t.Fatalf("unexpected selection: %#v", selected)
	}
	if !strings.Contains(out.String(), "Select .af files to generate") {
		t.Fatalf("expected prompt output, got %q", out.String())
	}
}

func TestPromptForAFFilesSelectsAllOnBlankInput(t *testing.T) {
	originalInput := interactiveInput
	originalOutput := interactiveOutput
	interactiveInput = strings.NewReader("\n")
	interactiveOutput = &bytes.Buffer{}
	t.Cleanup(func() {
		interactiveInput = originalInput
		interactiveOutput = originalOutput
	})

	files := []string{"one.af", "two.af"}
	selected, err := promptForAFFiles(files)
	require.NoError(t, err)

	if len(selected) != len(files) {
		t.Fatalf("expected all files, got %#v", selected)
	}
}

func TestPromptForAFFilesRejectsInvalidSelection(t *testing.T) {
	originalInput := interactiveInput
	originalOutput := interactiveOutput
	interactiveInput = strings.NewReader("4\n")
	interactiveOutput = &bytes.Buffer{}
	t.Cleanup(func() {
		interactiveInput = originalInput
		interactiveOutput = originalOutput
	})

	_, err := promptForAFFiles([]string{"one.af", "two.af"})
	if err == nil {
		t.Fatal("expected invalid selection error")
	}
	if !strings.Contains(err.Error(), "choose between 1 and 2") {
		t.Fatalf("unexpected error: %v", err)
	}
}
