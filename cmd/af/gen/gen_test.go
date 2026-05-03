package gen

import (
	"testing"

	"github.com/omniaura/agentflow/cfg"
	"github.com/spf13/viper"
)

func TestCMDDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cfg.MaxLineLen = 0

	cmd := CMD()

	if cmd.Use != "gen" {
		t.Fatalf("expected command use gen, got %q", cmd.Use)
	}
	if cfg.MaxLineLen != 80 {
		t.Fatalf("expected default max line len 80, got %d", cfg.MaxLineLen)
	}

	maxLineLenFlag := cmd.PersistentFlags().Lookup("max-line-len")
	if maxLineLenFlag == nil {
		t.Fatal("expected max-line-len flag")
	}
	if maxLineLenFlag.DefValue != "80" {
		t.Fatalf("expected max-line-len default 80, got %q", maxLineLenFlag.DefValue)
	}

	if got := viper.GetInt("max-line-len"); got != 80 {
		t.Fatalf("expected viper max-line-len 80, got %d", got)
	}

	if _, _, err := cmd.Find([]string{"prompts"}); err != nil {
		t.Fatalf("expected prompts subcommand, got error %v", err)
	}
}

func TestCMDParsesMaxLineLenFlag(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cfg.MaxLineLen = 0

	cmd := CMD()
	err := cmd.PersistentFlags().Set("max-line-len", "120")
	if err != nil {
		t.Fatalf("set max-line-len flag: %v", err)
	}

	if cfg.MaxLineLen != 120 {
		t.Fatalf("expected parsed max line len 120, got %d", cfg.MaxLineLen)
	}
	if got := viper.GetInt("max-line-len"); got != 120 {
		t.Fatalf("expected viper max-line-len 120, got %d", got)
	}
}
