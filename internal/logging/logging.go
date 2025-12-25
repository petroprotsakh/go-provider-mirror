package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"golang.org/x/term"
)

type Level int

const (
	LevelQuiet   Level = -1 // Errors only
	LevelNormal  Level = 0  // Default (pretty human output)
	LevelVerbose Level = 1  // Structured text logs
	LevelDebug   Level = 2  // Full debug with slog
)

type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Config holds the logging configuration.
type Config struct {
	Level  Level
	Format Format
	Output io.Writer
}

// Logger wraps slog.Logger with additional functionality.
type Logger struct {
	*slog.Logger
	level  Level
	format Format
	output io.Writer
	isTTY  bool
}

var defaultLogger *Logger

// Init initializes the global logger with the given configuration.
func Init(cfg Config) {
	if cfg.Output == nil {
		cfg.Output = os.Stderr
	}

	// Detect TTY
	isTTY := false
	if f, ok := cfg.Output.(*os.File); ok {
		isTTY = term.IsTerminal(int(f.Fd()))
	}

	var slogLevel slog.Level
	switch cfg.Level {
	case LevelQuiet:
		slogLevel = slog.LevelError
	case LevelNormal:
		slogLevel = slog.LevelInfo
	case LevelVerbose:
		slogLevel = slog.LevelInfo
	case LevelDebug:
		slogLevel = slog.LevelDebug
	default:
		slogLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: slogLevel,
	}

	var handler slog.Handler
	if cfg.Format == FormatJSON {
		handler = slog.NewJSONHandler(cfg.Output, opts)
	} else {
		handler = slog.NewTextHandler(cfg.Output, opts)
	}

	defaultLogger = &Logger{
		Logger: slog.New(handler),
		level:  cfg.Level,
		format: cfg.Format,
		output: cfg.Output,
		isTTY:  isTTY,
	}
}

// Default returns the default logger.
func Default() *Logger {
	if defaultLogger == nil {
		Init(Config{Level: LevelNormal, Format: FormatText})
	}
	return defaultLogger
}

// IsTTY returns true if the output is a terminal.
func (l *Logger) IsTTY() bool {
	return l.isTTY
}

// Level returns the current logging level.
func (l *Logger) Level() Level {
	return l.level
}

// Format returns the current logging format.
func (l *Logger) Format() Format {
	return l.format
}

// IsQuiet returns true if in quiet mode.
func (l *Logger) IsQuiet() bool {
	return l.level == LevelQuiet
}

// IsNormal returns true if in normal (pretty) mode.
func (l *Logger) IsNormal() bool {
	return l.level == LevelNormal && l.format == FormatText
}

// IsVerbose returns true if verbose or higher.
func (l *Logger) IsVerbose() bool {
	return l.level >= LevelVerbose
}

// IsDebug returns true if debug level.
func (l *Logger) IsDebug() bool {
	return l.level >= LevelDebug
}

// UseStructuredLogs returns true if we should use slog-style output.
// This is true for verbose/debug modes or JSON format.
func (l *Logger) UseStructuredLogs() bool {
	return l.level >= LevelVerbose || l.format == FormatJSON
}

// ShowProgress returns true if progress bars should be shown.
// Progress bars are shown when:
// - Not in quiet mode
// - Output format is text (not JSON)
// - Output is a TTY
func (l *Logger) ShowProgress() bool {
	return l.level >= LevelNormal && l.format == FormatText && l.isTTY
}

// --- Pretty printing for normal mode ---

// Print prints a formatted message (only in normal/pretty mode).
func (l *Logger) Print(format string, args ...any) {
	if l.level >= LevelNormal && !l.UseStructuredLogs() {
		_, _ = fmt.Fprintf(l.output, format, args...)
	}
}

// Println prints a line (only in normal/pretty mode).
func (l *Logger) Println(args ...any) {
	if l.level >= LevelNormal && !l.UseStructuredLogs() {
		_, _ = fmt.Fprintln(l.output, args...)
	}
}

// --- Structured logging methods (shadow embedded slog.Logger) ---

// Info logs at info level (only in structured/verbose mode).
func (l *Logger) Info(msg string, args ...any) {
	if l.UseStructuredLogs() {
		l.Logger.Info(msg, args...)
	}
}

// Debug logs at debug level (only in debug mode).
func (l *Logger) Debug(msg string, args ...any) {
	if l.UseStructuredLogs() {
		l.Logger.Debug(msg, args...)
	}
}

// Warn logs at warn level (only in structured/verbose mode).
func (l *Logger) Warn(msg string, args ...any) {
	if l.UseStructuredLogs() {
		l.Logger.Warn(msg, args...)
	}
}

// Error logs at error level (always shown, even in quiet mode).
func (l *Logger) Error(msg string, args ...any) {
	l.Logger.Error(msg, args...)
}

// Verbose logs at info level but only if verbose mode is enabled.
func (l *Logger) Verbose(msg string, args ...any) {
	if l.IsVerbose() {
		l.Logger.Info(msg, args...)
	}
}
