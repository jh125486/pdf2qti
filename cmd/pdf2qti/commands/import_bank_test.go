package commands_test

import (
	"archive/zip"
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	commands "github.com/jh125486/pdf2qti/cmd/pdf2qti/commands"
)

func TestImportBankCmdDryRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	packagePath := filepath.Join(dir, "quiz.zip")
	writeManifestZIP(t, packagePath)
	cmd := commands.ImportBankCmd{
		CourseID: "147966", BankName: "Test Bank", Package: packagePath, DryRun: true,
	}
	if err := cmd.Run(context.Background(), &commands.CLI{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestImportBankCmdRejectsBadPackage(t *testing.T) {
	t.Parallel()
	cmd := commands.ImportBankCmd{CourseID: "1", BankName: "Test", Package: "not-a-zip.txt", DryRun: true}
	err := cmd.Run(context.Background(), &commands.CLI{})
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
	mustWriteFile(t, path, data.Bytes())
}
