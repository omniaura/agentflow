package prompts

import (
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
)

func TestCMDDefaults(t *testing.T) {
	Dir = ""

	cmd := CMD()

	if cmd.Use != "prompts" {
		t.Fatalf("expected command use prompts, got %q", cmd.Use)
	}
	if Dir != "." {
		t.Fatalf("expected default dir ., got %q", Dir)
	}

	dirFlag := cmd.Flags().Lookup("dir")
	if dirFlag == nil {
		t.Fatal("expected dir flag")
	}
	if dirFlag.DefValue != "." {
		t.Fatalf("expected dir default ., got %q", dirFlag.DefValue)
	}
	if dirFlag.Shorthand != "d" {
		t.Fatalf("expected dir shorthand d, got %q", dirFlag.Shorthand)
	}
}

func TestCMDParsesDirFlag(t *testing.T) {
	Dir = ""

	cmd := CMD()
	err := cmd.ParseFlags([]string{"--dir", "./examples"})
	require.NoError(t, err)

	if Dir != "./examples" {
		t.Fatalf("expected parsed dir ./examples, got %q", Dir)
	}
}
