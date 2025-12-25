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

// Print prints a message (only in normal mode, not quiet).
func (l *Logger) Print(format string, args ...any) {
	if l.level >= LevelNormal && !l.UseStructuredLogs() {
		_, _ = fmt.Fprintf(l.output, format, args...)
	}
}

// Println prints a message with newline (only in normal mode).
func (l *Logger) Println(args ...any) {
	if l.level >= LevelNormal && !l.UseStructuredLogs() {
		_, _ = fmt.Fprintln(l.output, args...)
	}
}

// --- Convenience functions that use the default logger ---

// Info logs at info level.
// In normal mode, this is a no-op (Print/Println are used for pretty output).
// In verbose/debug/JSON modes, this uses slog.
func Info(msg string, args ...any) {
	l := Default()
	if l.UseStructuredLogs() {
		l.Info(msg, args...)
	}
}

// Debug logs at debug level (shown only in debug mode).
func Debug(msg string, args ...any) {
	l := Default()
	if l.UseStructuredLogs() {
		l.Debug(msg, args...)
	}
}

// Warn logs at warn level.
func Warn(msg string, args ...any) {
	l := Default()
	if l.UseStructuredLogs() {
		l.Warn(msg, args...)
	}
}

// Error logs at error level (always shown, even in quiet mode).
func Error(msg string, args ...any) {
	Default().Error(msg, args...)
}

// Verbose logs only if verbose mode is enabled.
func Verbose(msg string, args ...any) {
	l := Default()
	if l.IsVerbose() {
		l.Info(msg, args...)
	}
}

// Print prints a formatted message in normal mode.
func Print(format string, args ...any) {
	Default().Print(format, args...)
}

// Println prints a line in normal mode.
func Println(args ...any) {
	Default().Println(args...)
}
