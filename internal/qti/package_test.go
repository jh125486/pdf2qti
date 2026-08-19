package qti_test

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/qti"
)

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestWritePackage(t *testing.T) {
	t.Parallel()

	assessment, err := qti.BuildAssessment(sampleDraft())
	if err != nil {
		t.Fatal(err)
	}
	assessmentXML, err := qti.Marshal(assessment)
	if err != nil {
		t.Fatal(err)
	}

	var packageBytes bytes.Buffer
	if err := qti.WritePackage(&packageBytes, "signals.xml", assessmentXML); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(packageBytes.Bytes()), int64(packageBytes.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]string, len(zr.File))
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		files[f.Name] = string(content)
	}
	if len(files) != 2 {
		t.Fatalf("expected exactly two ZIP root files, got %v", files)
	}
	if !strings.Contains(files["imsmanifest.xml"], `type="imsqti_xmlv1p2"`) ||
		!strings.Contains(files["imsmanifest.xml"], `href="signals.xml"`) {
		t.Fatalf("manifest does not reference QTI assessment: %s", files["imsmanifest.xml"])
	}
	if !strings.Contains(files["signals.xml"], `xmlns="http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1"`) {
		t.Fatalf("assessment has no QTI namespace: %s", files["signals.xml"])
	}
}

func TestWritePackageErrors_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, filename, want string
	}{
		{name: "non XML", filename: "quiz.qti", want: "root-level .xml"},
		{name: "nested XML", filename: "nested/quiz.xml", want: "root-level .xml"},
		{name: "empty", filename: "", want: "root-level .xml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := qti.WritePackage(io.Discard, tt.filename, []byte("assessment"))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("WritePackage() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestWritePackageEscapesManifest(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := qti.WritePackage(&out, "a&b.xml", []byte("assessment")); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `a&amp;b.xml`) {
		t.Fatalf("manifest = %s", data)
	}
}

func TestWritePackageWriterError(t *testing.T) {
	t.Parallel()
	err := qti.WritePackage(errWriter{}, "quiz.xml", []byte("assessment"))
	if err == nil || !strings.Contains(err.Error(), "close QTI package") {
		t.Fatalf("WritePackage() error = %v, want close error", err)
	}
}
