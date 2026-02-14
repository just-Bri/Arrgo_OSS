package logger

import (
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

// Init initializes the default logger with appropriate handler based on environment
func Init(env string, debug bool) {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	if debug || env == "development" {
		opts.Level = slog.LevelDebug
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
