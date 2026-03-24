package logger

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

var defaultLogger *slog.Logger

// Init initializes the default logger with appropriate handler based on environment
func Init(env string, debug bool) {
	var handler slog.Handler

	// Default log level - restricted to Error and Fatal as requested
	// We ignore the 'debug' flag here to ensure we only emit errors by default
	level := slog.LevelError

	// Override from environment if set
	envLevel := strings.ToLower(os.Getenv("GOLOG_LOG_LEVEL"))
	if envLevel == "" {
		envLevel = strings.ToLower(os.Getenv("LOG_LEVEL"))
	}

	if envLevel != "" {
		switch envLevel {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		case "fatal":
			level = slog.Level(12)
		default:
			fmt.Printf("Unknown log level: %s, falling back to default (Error)\n", envLevel)
		}
	}

	// Always print to stdout when initializing so we can verify the level in docker logs
	fmt.Printf("Initializing logger: env=%s, debug_flag=%v, effective_level=%v\n", env, debug, level)

	opts := &slog.HandlerOptions{
		Level: level,
	}

	if debug || env == "development" {
		// Use text handler for development (human-readable)
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		// Use JSON handler for production (structured logging)
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// Default returns the default logger instance
func Default() *slog.Logger {
	if defaultLogger == nil {
		// Fallback to text handler if not initialized
		defaultLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	return defaultLogger
}

// With returns a logger with the given attributes
func With(args ...any) *slog.Logger {
	return Default().With(args...)
}

// Debug logs at debug level
func Debug(msg string, args ...any) {
	Default().Debug(msg, args...)
}

// Info logs at info level
func Info(msg string, args ...any) {
	Default().Info(msg, args...)
}

// Warn logs at warn level
func Warn(msg string, args ...any) {
	Default().Warn(msg, args...)
}

// Error logs at error level
func Error(msg string, args ...any) {
	Default().Error(msg, args...)
}
