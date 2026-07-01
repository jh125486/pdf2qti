package extract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/extract"
)

func TestExtractText_Table(t *testing.T) {
	t.Parallel()

	//nolint:gosec // Fixture strings and temp filenames are test data, not credentials.
	tests := []struct {
		name      string
		prepare   func(t *testing.T, dir string) string
		wantErr   bool
		wantToken string
	}{
		{
			name: "missing file",
			prepare: func(_ *testing.T, dir string) string {
				return filepath.Join(dir, "nonexistent.pdf")
			},
			wantErr: true,
		},
		{
			name: "empty file",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				path := filepath.Join(dir, "empty.pdf")
				if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantToken: "PDF content extracted from",
		},
		{
			name: "extract paren text",
			prepare: func(t *testing.T, dir string) string {
				t.Helper()
				path := filepath.Join(dir, "fake.pdf")
				if err := os.WriteFile(path, []byte("BT (Hello World from PDF) Tj ET"), 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantToken: "Hello World from PDF",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := tt.prepare(t, dir)
			result, err := extract.ExtractText(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantToken != "" && !strings.Contains(result, tt.wantToken) {
				t.Fatalf("expected output to contain %q, got %q", tt.wantToken, result)
			}
		})
	}
}
