package itembank

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestChromedpImporterImport_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		failAt  int
		wantErr string
	}{
		{name: "success"},
		{name: "open banks", failAt: 1, wantErr: "open Item Banks"},
		{name: "find bank", failAt: 2, wantErr: "find Item Bank"},
		{name: "open create dialog", failAt: 3, wantErr: "open create bank dialog"},
		{name: "bank name field", failAt: 4, wantErr: "wait for bank-name field"},
		{name: "fill bank name", failAt: 5, wantErr: "fill bank name"},
		{name: "share course", failAt: 6, wantErr: "share bank with course"},
		{name: "submit create", failAt: 7, wantErr: "submit create bank"},
		{name: "return to banks", failAt: 8, wantErr: "return to Item Banks"},
		{name: "open bank", failAt: 9, wantErr: "open Item Bank"},
		{name: "wait actions", failAt: 10, wantErr: "wait for Item Bank actions"},
		{name: "open actions", failAt: 11, wantErr: "open import actions"},
		{name: "open dialog", failAt: 12, wantErr: "open import dialog"},
		{name: "attach package", failAt: 13, wantErr: "attach package"},
		{name: "submit import", failAt: 14, wantErr: "submit import"},
		{name: "wait completion", failAt: 15, wantErr: "wait for import completion"},
		{name: "read location", failAt: 16, wantErr: "read Item Bank URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			importer := ChromedpImporter{run: func(_ context.Context, _ ...chromedp.Action) error {
				calls++
				if calls == tt.failAt {
					return errors.New("browser failed")
				}
				return nil
			}}
			_, err := importer.Import(context.Background(), &Request{
				BaseURL: "https://canvas.example.edu", BrowserURL: "http://127.0.0.1:9222",
				CourseID: "7", BankName: "Bank", Package: "quiz.zip", OnExisting: ExistingAppend,
			})
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Import() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Import() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestChromedpImporterImport_InvalidRequest_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		req  *Request
		want string
	}{
		{name: "nil", want: "request is required"},
		{name: "invalid base URL", req: &Request{BaseURL: "://bad"}, want: "invalid Canvas base URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := (ChromedpImporter{}).Import(context.Background(), tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Import() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestXPathString_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ input, want string }{
		{"plain", "'plain'"}, {"don't", `"don't"`}, {`say "don't"`, `concat('say "don',"'",'t"')`},
	} {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			if got := xpathString(tt.input); got != tt.want {
				t.Fatalf("xpathString() = %q, want %q", got, tt.want)
			}
		})
	}
}
