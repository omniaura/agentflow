package main

import (
	"testing"

	"github.com/omniaura/agentflow/cfg"
	"github.com/spf13/viper"
)

func TestNewRootCommandDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cfg.FlagLogLevel = ""

	cmd := newRootCommand()

	if cmd.Use != "af" {
		t.Fatalf("expected command use af, got %q", cmd.Use)
	}
	if cmd.Version != cfg.Version {
		t.Fatalf("expected version %q, got %q", cfg.Version, cmd.Version)
	}

	logFlag := cmd.PersistentFlags().Lookup("log")
	if logFlag == nil {
		t.Fatal("expected log flag")
	}
	if logFlag.DefValue != "debug" {
		t.Fatalf("expected log default debug, got %q", logFlag.DefValue)
	}
	if cfg.FlagLogLevel != "debug" {
		t.Fatalf("expected cfg.FlagLogLevel debug, got %q", cfg.FlagLogLevel)
	}

	if got := viper.GetString("log"); got != "debug" {
		t.Fatalf("expected viper log debug, got %q", got)
	}

	if _, _, err := cmd.Find([]string{"demo"}); err != nil {
		t.Fatalf("expected demo subcommand, got error %v", err)
	}
	if _, _, err := cmd.Find([]string{"fmt"}); err != nil {
		t.Fatalf("expected fmt subcommand, got error %v", err)
	}
	if _, _, err := cmd.Find([]string{"gen"}); err != nil {
		t.Fatalf("expected gen subcommand, got error %v", err)
	}
	if _, _, err := cmd.Find([]string{"lint"}); err != nil {
		t.Fatalf("expected lint subcommand, got error %v", err)
	}
	if _, _, err := cmd.Find([]string{"lsp"}); err != nil {
		t.Fatalf("expected lsp subcommand, got error %v", err)
	}
	if len(cmd.Commands()) != 5 {
		t.Fatalf("expected 5 subcommands, got %d", len(cmd.Commands()))
	}
}

func TestNewRootCommandParsesLogFlag(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cfg.FlagLogLevel = ""

	cmd := newRootCommand()
	err := cmd.PersistentFlags().Set("log", "warn")
	if err != nil {
		t.Fatalf("set log flag: %v", err)
	}

	if cfg.FlagLogLevel != "warn" {
		t.Fatalf("expected cfg.FlagLogLevel warn, got %q", cfg.FlagLogLevel)
	}
	if got := viper.GetString("log"); got != "warn" {
		t.Fatalf("expected viper log warn, got %q", got)
	}
}
