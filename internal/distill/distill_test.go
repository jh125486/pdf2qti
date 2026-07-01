package distill_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
)

type stubLLM struct {
	response string
	err      error
}

func (s *stubLLM) Complete(_ context.Context, _ string) (string, error) {
	return s.response, s.err
}

func validLLMResponse() string {
	dc := distill.DistilledContext{ModuleName: "Signals", Text: "thick text", Overview: "<p>Overview</p>", KeyConcepts: []string{"SIGINT"}}
	b, _ := json.Marshal(dc)
	return string(b)
}

func TestDistill_Table(t *testing.T) {
	t.Parallel()

	src := &config.Source{ID: "ch21", Name: "TLPI", Chapter: 21, PDF: "ch21.pdf"}
	tests := []struct {
		name    string
		llm     *stubLLM
		wantErr bool
		check   func(t *testing.T, dc *distill.DistilledContext)
	}{
		{name: "happy path", llm: &stubLLM{response: validLLMResponse()}, check: func(t *testing.T, dc *distill.DistilledContext) {
			t.Helper()
			if dc.SourceID != "ch21" || dc.Book != "TLPI" || dc.Chapter != 21 {
				t.Fatalf("unexpected source metadata: %+v", dc)
			}
		}},
		{name: "llm error", llm: &stubLLM{err: errors.New("llm failed")}, wantErr: true},
		{name: "invalid json", llm: &stubLLM{response: "not json"}, wantErr: true},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dc, err := distill.Distill(context.Background(), src, nil, tt.llm, "text")
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, dc)
			}
		})
	}
}

func TestLoadSave_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		run     func(t *testing.T, dir string) error
		wantErr bool
		errLike string
	}{
		{
			name: "save and load",
			run: func(t *testing.T, dir string) error {
				t.Helper()
				path := filepath.Join(dir, "ctx.json")
				dc := &distill.DistilledContext{SourceID: "ch21", Book: "TLPI", Chapter: 21}
				if err := distill.Save(path, dc); err != nil {
					return err
				}
				loaded, err := distill.Load(path)
				if err != nil {
					return err
				}
				if loaded.SourceID != dc.SourceID {
					t.Fatalf("source id mismatch: %q vs %q", loaded.SourceID, dc.SourceID)
				}
				return nil
			},
		},
		{
			name: "load missing file",
			run: func(_ *testing.T, _ string) error {
				_, err := distill.Load("/no/such/file.json")
				return err
			},
			wantErr: true,
			errLike: "read context",
		},
		{
			name: "load corrupt json",
			run: func(t *testing.T, dir string) error {
				t.Helper()
				path := filepath.Join(dir, "ctx.json")
				if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
					t.Fatal(err)
				}
				_, err := distill.Load(path)
				return err
			},
			wantErr: true,
			errLike: "parse context",
		},
		{
			name: "save bad path",
			run: func(_ *testing.T, _ string) error {
				return distill.Save("/no/such/dir/ctx.json", &distill.DistilledContext{})
			},
			wantErr: true,
			errLike: "write context",
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.run(t, t.TempDir())
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.errLike != "" && (err == nil || !strings.Contains(err.Error(), tt.errLike)) {
				t.Fatalf("expected error containing %q, got %v", tt.errLike, err)
			}
		})
	}
}
