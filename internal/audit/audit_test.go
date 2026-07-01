package audit_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/audit"
)

func TestLogger_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		logFn      func(l *audit.Logger)
		wantTokens []string
	}{
		{name: "info", logFn: func(l *audit.Logger) { l.Info("test message", "key", "value") }, wantTokens: []string{"[INFO]", "test message", "key=value"}},
		{name: "warn", logFn: func(l *audit.Logger) { l.Warn("warning msg") }, wantTokens: []string{"[WARN]", "warning msg"}},
		{name: "error", logFn: func(l *audit.Logger) { l.Error("error msg", "code", 42) }, wantTokens: []string{"[ERROR]", "code=42"}},
		{name: "odd fields", logFn: func(l *audit.Logger) { l.Info("msg", "key1", "val1", "orphan") }, wantTokens: []string{"key1=val1"}},
		{name: "multiple fields", logFn: func(l *audit.Logger) { l.Log(audit.LevelInfo, "multi", "a", 1, "b", 2, "c", 3) }, wantTokens: []string{"a=1", "b=2", "c=3"}},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			l := audit.New(&buf)
			tt.logFn(l)
			out := buf.String()
			for _, tok := range tt.wantTokens {
				if !strings.Contains(out, tok) {
					t.Fatalf("expected output to contain %q, got: %q", tok, out)
				}
			}
		})
	}
}

func TestLogger_NilWriter(t *testing.T) {
	t.Parallel()

	l := audit.New(nil)
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
}
