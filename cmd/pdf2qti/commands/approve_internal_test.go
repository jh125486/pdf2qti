// Whitebox tests inject per-command package I/O to deterministically exercise
// archive-write and file-close failures without mutating package globals.
package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
)

type approveWriteCloser struct {
	bytes.Buffer
	closeErr   error
	closeCalls int
}

func (f *approveWriteCloser) Close() error {
	f.closeCalls++
	return f.closeErr
}

func TestApproveCmdPackageErrors_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		writeErr  error
		closeErr  error
		want      string
		wantClose int
	}{
		{name: "write error", writeErr: errors.New("package write failed"), want: "write QTI package", wantClose: 1},
		{name: "write and close error", writeErr: errors.New("package write failed"), closeErr: errors.New("package close failed"), want: "also close package", wantClose: 1},
		{name: "close error", closeErr: errors.New("package close failed"), want: "close QTI package", wantClose: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			cfgFile := filepath.Join(dir, "quiz.json")
			cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
			if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "src01_quiz.md"), []byte("# Quiz\n\n## MC\n\n1. Two plus two?\n[ ] 3\n[*] 4\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			file := &approveWriteCloser{closeErr: tt.closeErr}
			cmd := &ApproveCmd{packageOps: packageOps{
				open:  func(string) (io.WriteCloser, error) { return file, nil },
				write: func(io.Writer, string, []byte) error { return tt.writeErr },
			}}
			err := cmd.Run(context.Background(), &CLI{Config: cfgFile})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want %q", err, tt.want)
			}
			if file.closeCalls != tt.wantClose {
				t.Fatalf("Close() calls = %d, want %d", file.closeCalls, tt.wantClose)
			}
		})
	}
}

func TestRunApproveSource_DefaultPackageOps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "quiz.json")
	cfgJSON := `{"version":1,"defaults":{"workflow":{"outDir":"` + dir + `"}},"sources":[{"id":"src01","pdf":"src01.pdf"}]}`
	if err := os.WriteFile(cfgFile, []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src01_quiz.md"), []byte("# Quiz\n\n## MC\n\n1. Two plus two?\n[ ] 3\n[*] 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := runApproveSource(cfg, &cfg.Sources[0], audit.New(io.Discard)); err != nil {
		t.Fatalf("runApproveSource() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "src01.zip")); err != nil {
		t.Fatalf("QTI package: %v", err)
	}
}
