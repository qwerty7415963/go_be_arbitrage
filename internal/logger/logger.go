package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

type Logger struct {
	*slog.Logger
}

func New(level, format string) *Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	var handler slog.Handler
	if strings.ToLower(format) == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return &Logger{
		Logger: slog.New(handler),
	}
}

func NewWithWriter(level, format string, writer io.Writer) *Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	var handler slog.Handler
	if strings.ToLower(format) == "text" {
		handler = slog.NewTextHandler(writer, opts)
	} else {
		handler = slog.NewJSONHandler(writer, opts)
	}

	return &Logger{
		Logger: slog.New(handler),
	}
}

func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	attrs := make([]interface{}, 0, len(fields))
	for k, v := range fields {
		attrs = append(attrs, slog.Any(k, v))
	}
	return &Logger{
		Logger: l.Logger.With(attrs...),
	}
}

func (l *Logger) WithError(err error) *Logger {
	return &Logger{
		Logger: l.Logger.With("error", err),
	}
}

func (l *Logger) WithRequestID(requestID string) *Logger {
	return &Logger{
		Logger: l.Logger.With("request_id", requestID),
	}
}

func (l *Logger) WithModule(module string) *Logger {
	return &Logger{
		Logger: l.Logger.With("module", module),
	}
}
