package lsp

import (
	"os"
	"testing"

	"github.com/omniaura/agentflow/pkg/assert/require"
)

func TestCMDDefaults(t *testing.T) {
	Mode = ""
	Port = 0
	Debug = true

	cmd := CMD()

	if cmd.Use != "lsp" {
		t.Fatalf("expected command use lsp, got %q", cmd.Use)
	}
	if Mode != "stdio" {
		t.Fatalf("expected default mode stdio, got %q", Mode)
	}
	if Port != 4389 {
		t.Fatalf("expected default port 4389, got %d", Port)
	}
	if Debug {
		t.Fatal("expected debug to default false")
	}

	modeFlag := cmd.Flags().Lookup("mode")
	if modeFlag == nil {
		t.Fatal("expected mode flag")
	}
	if modeFlag.DefValue != "stdio" {
		t.Fatalf("expected mode default stdio, got %q", modeFlag.DefValue)
	}

	portFlag := cmd.Flags().Lookup("port")
	if portFlag == nil {
		t.Fatal("expected port flag")
	}
	if portFlag.DefValue != "4389" {
		t.Fatalf("expected port default 4389, got %q", portFlag.DefValue)
	}

	debugFlag := cmd.Flags().Lookup("debug")
	if debugFlag == nil {
		t.Fatal("expected debug flag")
	}
	if debugFlag.DefValue != "false" {
		t.Fatalf("expected debug default false, got %q", debugFlag.DefValue)
	}
}

func TestCMDParsesFlags(t *testing.T) {
	Mode = ""
	Port = 0
	Debug = false

	cmd := CMD()
	err := cmd.ParseFlags([]string{"--mode", "tcp", "--port", "9393", "--debug"})
	require.NoError(t, err)

	if Mode != "tcp" {
		t.Fatalf("expected parsed mode tcp, got %q", Mode)
	}
	if Port != 9393 {
		t.Fatalf("expected parsed port 9393, got %d", Port)
	}
	if !Debug {
		t.Fatal("expected parsed debug true")
	}
}

func TestGetWorkingDir(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	if got := getWorkingDir(); got != wd {
		t.Fatalf("expected working dir %q, got %q", wd, got)
	}
}

func TestBToMb(t *testing.T) {
	if got := bToMb(5 * 1024 * 1024); got != 5 {
		t.Fatalf("expected 5 MB, got %d", got)
	}
	if got := bToMb(1024 * 1023); got != 0 {
		t.Fatalf("expected 0 MB, got %d", got)
	}
}
