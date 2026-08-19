// Whitebox tests inject archive writers to cover ZIP entry and close failures
// that archive/zip cannot produce deterministically through its public writer.
package qti

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeArchive struct {
	createCalls int
	createErrAt int
	entryErrAt  int
	closeErr    error
}

func (f *fakeArchive) Create(_ string) (io.Writer, error) {
	f.createCalls++
	if f.createErrAt == f.createCalls {
		return nil, errors.New("create failed")
	}
	if f.entryErrAt == f.createCalls {
		return errorWriter{}, nil
	}
	return io.Discard, nil
}

func (f *fakeArchive) Close() error { return f.closeErr }

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("entry write failed") }

func TestWritePackageArchiveErrors_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		createErrAt int
		entryErrAt  int
		closeErr    error
		want        string
	}{
		{name: "manifest create", createErrAt: 1, closeErr: errors.New("ignored close"), want: `create ZIP entry "imsmanifest.xml"`},
		{name: "manifest write", entryErrAt: 1, closeErr: errors.New("ignored close"), want: `write ZIP entry "imsmanifest.xml"`},
		{name: "assessment create", createErrAt: 2, want: `create ZIP entry "quiz.xml"`},
		{name: "assessment write", entryErrAt: 2, want: `write ZIP entry "quiz.xml"`},
		{name: "archive close", closeErr: errors.New("close failed"), want: "close QTI package"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			archive := &fakeArchive{createErrAt: tt.createErrAt, entryErrAt: tt.entryErrAt, closeErr: tt.closeErr}
			err := writePackage(io.Discard, "quiz.xml", []byte("assessment"), func(io.Writer) archiveWriter { return archive })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("writePackage() error = %v, want %q", err, tt.want)
			}
		})
	}
}
