package cfg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestInitConfig_NoFile(t *testing.T) {
	// Reset viper state
	viper.Reset()

	// Run from a temp directory with no config file
	tmp := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	InitConfig()

	if ConfigFile != "" {
		t.Errorf("expected no config file, got %q", ConfigFile)
	}
	// Defaults should be set
	if viper.GetInt("max-line-len") != 80 {
		t.Errorf("expected max-line-len default 80, got %d", viper.GetInt("max-line-len"))
	}
	if viper.GetString("gen.dir") != "." {
		t.Errorf("expected gen.dir default '.', got %q", viper.GetString("gen.dir"))
	}
	if viper.GetString("lsp.mode") != "stdio" {
		t.Errorf("expected lsp.mode default 'stdio', got %q", viper.GetString("lsp.mode"))
	}
}

func TestInitConfig_WithFile(t *testing.T) {
	viper.Reset()
	ConfigFile = ""

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, ".agentflow.yaml")
	err := os.WriteFile(cfgPath, []byte("log: warn\nmax-line-len: 120\ngen:\n  dir: ./prompts\nlsp:\n  port: 5000\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	InitConfig()

	if ConfigFile == "" {
		t.Error("expected config file to be loaded")
	}
	if viper.GetString("log") != "warn" {
		t.Errorf("expected log=warn, got %q", viper.GetString("log"))
	}
	if viper.GetInt("max-line-len") != 120 {
		t.Errorf("expected max-line-len=120, got %d", viper.GetInt("max-line-len"))
	}
	if viper.GetString("gen.dir") != "./prompts" {
		t.Errorf("expected gen.dir=./prompts, got %q", viper.GetString("gen.dir"))
	}
	if viper.GetInt("lsp.port") != 5000 {
		t.Errorf("expected lsp.port=5000, got %d", viper.GetInt("lsp.port"))
	}
}

func TestLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"debug", "DEBUG"},
		{"DEBUG", "DEBUG"},
		{"info", "INFO"},
		{"INFO", "INFO"},
		{"warn", "WARN"},
		{"WARN", "WARN"},
		{"error", "ERROR"},
		{"ERROR", "ERROR"},
		{"unknown", "INFO"},
		{"", "INFO"},
	}
	for _, tt := range tests {
		FlagLogLevel = tt.input
		got := LogLevel().String()
		if got != tt.want {
			t.Errorf("LogLevel(%q) = %s, want %s", tt.input, got, tt.want)
		}
	}
}
