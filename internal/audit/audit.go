// Package audit provides audit logging for pdf2qti operations.
package audit

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Level is the log level.
type Level string

const (
	// LevelInfo is informational.
	LevelInfo Level = "INFO"
	// LevelWarn is a warning.
	LevelWarn Level = "WARN"
	// LevelError is an error.
	LevelError Level = "ERROR"
)

// Logger writes structured audit logs.
type Logger struct {
	w io.Writer
}

// New creates a new Logger writing to w.
func New(w io.Writer) *Logger {
	if w == nil {
		w = os.Stderr
	}
	return &Logger{w: w}
}

// Log writes a log entry.
func (l *Logger) Log(level Level, msg string, fields ...any) {
	ts := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(l.w, "%s [%s] %s", ts, level, msg)
	for i := 0; i+1 < len(fields); i += 2 {
		fmt.Fprintf(l.w, " %v=%v", fields[i], fields[i+1])
	}
	fmt.Fprintln(l.w)
}

// Info logs an informational message.
func (l *Logger) Info(msg string, fields ...any) {
	l.Log(LevelInfo, msg, fields...)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, fields ...any) {
	l.Log(LevelWarn, msg, fields...)
}

// Error logs an error message.
func (l *Logger) Error(msg string, fields ...any) {
	l.Log(LevelError, msg, fields...)
}
