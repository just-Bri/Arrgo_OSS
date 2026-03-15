package logger

import (
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

// Init initializes the default logger with appropriate handler based on environment
func Init(env string, debug bool) {
	var handler slog.Handler

	// Default log level
	level := slog.LevelInfo
	if debug || env == "development" {
		level = slog.LevelDebug
	}

	// Override from environment if set
	envLevel := os.Getenv("GOLOG_LOG_LEVEL")
	if envLevel == "" {
		envLevel = os.Getenv("LOG_LEVEL")
	}

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
		// slog doesn't have Fatal level, use a custom level or Error
		// Here we map it to Error, but we could use a higher value if needed
		level = slog.Level(12) 
	}

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
