package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	Level  string
	Format string
	Output io.Writer
}

func New(cfg Config) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	output := cfg.Output
	if output == nil {
		output = os.Stderr
	}

	opts := &slog.HandlerOptions{Level: level}

	switch strings.ToLower(strings.TrimSpace(cfg.Format)) {
	case "", "text":
		return slog.New(slog.NewTextHandler(output, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(output, opts)), nil
	default:
		return nil, fmt.Errorf("unsupported log format %q", cfg.Format)
	}
}

func parseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "warn", "warning":
		return slog.LevelWarn, nil
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelWarn, fmt.Errorf("unsupported log level %q", value)
	}
}
