package main

import (
	"io/fs"
	"regexp"
	"testing"

	"github.com/omniaura/agentflow/cfg"
	"github.com/omniaura/agentflow/cmd/af/skills"
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

	if _, _, err := cmd.Find([]string{"check"}); err != nil {
		t.Fatalf("expected check subcommand, got error %v", err)
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
	if _, _, err := cmd.Find([]string{"skills", "install"}); err != nil {
		t.Fatalf("expected skills install subcommand, got error %v", err)
	}
	if _, _, err := cmd.Find([]string{"eject"}); err != nil {
		t.Fatalf("expected eject subcommand, got error %v", err)
	}
	if _, _, err := cmd.Find([]string{"survey"}); err != nil {
		t.Fatalf("expected survey subcommand, got error %v", err)
	}
	if _, _, err := cmd.Find([]string{"upgrade"}); err != nil {
		t.Fatalf("expected upgrade subcommand, got error %v", err)
	}
	if len(cmd.Commands()) != 10 {
		t.Fatalf("expected 10 subcommands, got %d", len(cmd.Commands()))
	}
}

// The skills tell agents to run af commands; every one must exist.
func TestSkillsNameRealCommands(t *testing.T) {
	cmd := newRootCommand()
	// Commands appear in code: after a backtick, or at the start of a line
	// in a code block.
	re := regexp.MustCompile("(?m)(?:`|^)(?:go tool )?af ([a-z]+)(?: ([a-z]+))?")
	n := 0
	err := fs.WalkDir(skills.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := skills.FS.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			args := []string{m[1]}
			if m[2] != "" {
				args = append(args, m[2])
			}
			found, _, err := cmd.Find(args)
			if err != nil || found == cmd {
				t.Errorf("%s: %q is not an af command", path, m[0])
			}
			n++
		}
		return nil
	})
	if err != nil || n < 5 {
		t.Fatalf("checked %d commands, err %v", n, err)
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
