package cfg

import (
	"log/slog"
	"strings"

	"github.com/spf13/viper"
)

var Version = "0.5.3" // x-release-please-version
var (
	FlagLogLevel string
	// MaxLineLen is the maximum line length for code generation.
	// Prompt bodies are not wrapped.
	MaxLineLen int
)

// ConfigFile is the name of the config file that was loaded, if any.
var ConfigFile string

// InitConfig initializes Viper configuration. It searches for .agentflow.yaml
// in the current directory and the user's home directory.
func InitConfig() {
	viper.SetConfigName(".agentflow")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME")

	// Set defaults matching the CLI flag defaults
	viper.SetDefault("log", "debug")
	viper.SetDefault("max-line-len", 80)
	viper.SetDefault("gen.dir", ".")
	viper.SetDefault("lsp.mode", "stdio")
	viper.SetDefault("lsp.port", 4389)
	viper.SetDefault("lsp.debug", false)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			slog.Warn("Error reading config file", "error", err)
		}
		return
	}
	ConfigFile = viper.ConfigFileUsed()
	slog.Debug("Loaded config file", "path", ConfigFile)
}

func LogLevel() slog.Level {
	switch strings.ToUpper(FlagLogLevel) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func TestMode() {
	MaxLineLen = 80
	FlagLogLevel = "DEBUG"
}
