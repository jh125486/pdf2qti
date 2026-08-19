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

func TestImportBankCmdDryRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	packagePath := filepath.Join(dir, "quiz.zip")
	writeManifestZIP(t, packagePath)
	cmd := ImportBankCmd{
		CourseID: "147966", BankName: "Test Bank", Package: packagePath, DryRun: true,
	}
	if err := cmd.Run(context.Background(), &CLI{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestImportBankCmdRejectsBadPackage(t *testing.T) {
	t.Parallel()
	cmd := ImportBankCmd{CourseID: "1", BankName: "Test", Package: "not-a-zip.txt", DryRun: true}
	err := cmd.Run(context.Background(), &CLI{})
	if err == nil || !strings.Contains(err.Error(), ".zip") {
		t.Fatalf("Run() error = %v, want .zip validation error", err)
	}
}

func writeManifestZIP(t *testing.T, path string) {
	t.Helper()
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	w, err := zw.Create("imsmanifest.xml")
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

func TestImportBankCmdImporter_Table(t *testing.T) {
	// Not parallel: replaces package-level factory. Validation tests above remain parallel.
	for _, tt := range []struct {
		name    string
		err     error
		wantErr bool
	}{{name: "success"}, {name: "import error", err: errors.New("browser failed"), wantErr: true}} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "quiz.zip")
			writeManifestZIP(t, path)
			fake := &fakeItemBankImporter{err: tt.err}
			original := newItemBankImporter
			newItemBankImporter = func() itembank.Importer { return fake }
			t.Cleanup(func() { newItemBankImporter = original })
			err := (&ImportBankCmd{CourseID: "7", BankName: "Bank", Package: path, OnExisting: "append"}).Run(context.Background(), &CLI{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && (fake.got == nil || fake.got.OnExisting != itembank.ExistingAppend) {
				t.Fatalf("request=%+v", fake.got)
			}
		})
	}
}
