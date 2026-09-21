package config

import (
	"log/slog"
	"strings"

	"github.com/spf13/viper"
)

const DefaultLogLevel = "info"

// LogLevelConfig holds logging configuration.
type LogLevelConfig struct {
	Level string
}

// ParseLogLevel converts a level string to slog.Level.
// Returns slog.LevelInfo for unknown values.
func ParseLogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func loadLogLevelConfig(v *viper.Viper) LogLevelConfig {
	return LogLevelConfig{
		Level: strings.ToLower(strings.TrimSpace(v.GetString("log_level"))),
	}
}
