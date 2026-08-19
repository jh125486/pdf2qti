// Whitebox tests inject an importer through ImportBankCmd's unexported test seam.
package commands

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/itembank"
)

func writeZIP(t *testing.T, path, entry string) {
	t.Helper()
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	w, err := zw.Create(entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("<manifest/>")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

type fakeItemBankImporter struct {
	err error
	got *itembank.Request
}

func (f *fakeItemBankImporter) Import(_ context.Context, req *itembank.Request) (itembank.Result, error) {
	f.got = req
	return itembank.Result{BankURL: "https://canvas/banks/1"}, f.err
}

func TestImportBankCmdRun_Table(t *testing.T) { //nolint:gocyclo // table covers validation and importer outcomes
	t.Parallel()
	tests := []struct {
		name        string
		packageKind string
		dryRun      bool
		importErr   error
		wantErr     string
		wantImport  bool
	}{
		{name: "dry run", packageKind: "valid", dryRun: true},
		{name: "non ZIP extension", packageKind: "non-zip", dryRun: true, wantErr: ".zip file"},
		{name: "missing ZIP", packageKind: "missing", dryRun: true, wantErr: "read package"},
		{name: "invalid ZIP", packageKind: "invalid", dryRun: true, wantErr: "open package"},
		{name: "missing root manifest", packageKind: "no-manifest", dryRun: true, wantErr: "lacks root imsmanifest.xml"},
		{name: "nested manifest", packageKind: "nested-manifest", dryRun: true, wantErr: "lacks root imsmanifest.xml"},
		{name: "import success", packageKind: "valid", wantImport: true},
		{name: "import error", packageKind: "valid", importErr: errors.New("browser failed"), wantErr: "browser failed", wantImport: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			ext := ".zip"
			if tt.packageKind == "non-zip" {
				ext = ".txt"
			}
			path := filepath.Join(dir, "quiz"+ext)
			switch tt.packageKind {
			case "valid":
				writeZIP(t, path, "imsmanifest.xml")
			case "invalid":
				if err := os.WriteFile(path, []byte("not a ZIP"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "no-manifest":
				writeZIP(t, path, "assessment.xml")
			case "nested-manifest":
				writeZIP(t, path, "nested/imsmanifest.xml")
			}

			fake := &fakeItemBankImporter{err: tt.importErr}
			cmd := &ImportBankCmd{
				CourseID: "7", BankName: "Bank", Package: path,
				BaseURL: "https://canvas.example.edu", BrowserURL: "http://127.0.0.1:9222",
				OnExisting: "append", DryRun: tt.dryRun, importer: fake,
			}
			err := cmd.Run(context.Background(), &CLI{})
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Run() error = %v, want %q", err, tt.wantErr)
			}
			if !tt.wantImport {
				if fake.got != nil {
					t.Fatalf("Import() request = %+v, want no call", fake.got)
				}
				return
			}
			if fake.got == nil {
				t.Fatal("Import() request = nil")
			}
			if fake.got.CourseID != "7" || fake.got.BankName != "Bank" ||
				fake.got.Package != path || fake.got.BaseURL != "https://canvas.example.edu" ||
				fake.got.BrowserURL != "http://127.0.0.1:9222" || fake.got.OnExisting != itembank.ExistingAppend {
				t.Fatalf("Import() request = %+v", fake.got)
			}
		})
	}
}
