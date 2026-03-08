package audit_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/audit"
)

func TestLogger_Info(t *testing.T) {
	var buf bytes.Buffer
	l := audit.New(&buf)
	l.Info("test message", "key", "value")
	out := buf.String()
	if !strings.Contains(out, "[INFO]") {
		t.Errorf("expected [INFO] in output, got: %q", out)
	}
	if !strings.Contains(out, "test message") {
		t.Errorf("expected message in output, got: %q", out)
	}
	if !strings.Contains(out, "key=value") {
		t.Errorf("expected key=value in output, got: %q", out)
	}
}

func TestLogger_Warn(t *testing.T) {
	var buf bytes.Buffer
	l := audit.New(&buf)
	l.Warn("warning msg")
	out := buf.String()
	if !strings.Contains(out, "[WARN]") {
		t.Errorf("expected [WARN] in output, got: %q", out)
	}
}

func TestLogger_Error(t *testing.T) {
	var buf bytes.Buffer
	l := audit.New(&buf)
	l.Error("error msg", "code", 42)
	out := buf.String()
	if !strings.Contains(out, "[ERROR]") {
		t.Errorf("expected [ERROR] in output, got: %q", out)
	}
	if !strings.Contains(out, "code=42") {
		t.Errorf("expected code=42 in output, got: %q", out)
	}
}

func TestLogger_NilWriter(t *testing.T) {
	// Should not panic; falls back to os.Stderr
	l := audit.New(nil)
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestLogger_OddFields(t *testing.T) {
	// Odd number of fields - last field should be ignored
	var buf bytes.Buffer
	l := audit.New(&buf)
	l.Info("msg", "key1", "val1", "orphan")
	out := buf.String()
	if !strings.Contains(out, "key1=val1") {
		t.Errorf("expected key1=val1 in output, got: %q", out)
	}
}

func TestLogger_MultipleFields(t *testing.T) {
	var buf bytes.Buffer
	l := audit.New(&buf)
	l.Log(audit.LevelInfo, "multi", "a", 1, "b", 2, "c", 3)
	out := buf.String()
	if !strings.Contains(out, "a=1") {
		t.Errorf("expected a=1, got: %q", out)
	}
	if !strings.Contains(out, "b=2") {
		t.Errorf("expected b=2, got: %q", out)
	}
	if !strings.Contains(out, "c=3") {
		t.Errorf("expected c=3, got: %q", out)
	}
}
