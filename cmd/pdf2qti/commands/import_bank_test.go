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
	writeQTIPackage(t, path, "Bank", 3, entry)
}

func writeQTIPackage(t *testing.T, path, title string, itemCount int, entry string) {
	t.Helper()
	entries := map[string]string{entry: "<manifest/>"}
	if entry == "imsmanifest.xml" {
		entries[entry] = `<manifest><resources><resource type="imsqti_xmlv1p2"><file href="quiz.xml"/></resource></resources></manifest>`
		entries["quiz.xml"] = `<questestinterop xmlns="http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1"><assessment title="` + title + `"><section>` + strings.Repeat("<item/>", itemCount) + `</section></assessment></questestinterop>`
	}
	writeCustomZIP(t, path, entries)
}

func writeCustomZIP(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	for name, content := range entries {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

type fakeItemBankImporter struct {
	err    error
	got    *itembank.Request
	result itembank.Result
}

func (f *fakeItemBankImporter) Import(_ context.Context, req *itembank.Request) (itembank.Result, error) {
	f.got = req
	if f.result.BankURL == "" {
		f.result.BankURL = "https://canvas/banks/1"
	}
	return f.result, f.err
}

type fakeRandomQuizCreator struct {
	err            error
	preflightErr   error
	got            *itembank.QuizRequest
	preflightGot   *itembank.QuizRequest
	preflightTitle string
	createTitle    string
}

func (f *fakeRandomQuizCreator) PreflightRandomQuiz(_ context.Context, req *itembank.QuizRequest) error {
	clone := *req
	f.preflightGot = &clone
	f.preflightTitle = itembank.QuizTitle(req.BankName)
	return f.preflightErr
}

func (f *fakeRandomQuizCreator) CreateRandomQuiz(_ context.Context, req *itembank.QuizRequest) (itembank.QuizResult, error) {
	f.got = req
	f.createTitle = itembank.QuizTitle(req.BankName)
	return itembank.QuizResult{QuizURL: "https://canvas/quizzes/2", Title: "Bank Quiz", QuestionCount: req.QuestionCount}, f.err
}

func TestImportBankCmdRun_Table(t *testing.T) { //nolint:gocyclo // table covers validation and importer outcomes
	t.Parallel()
	tests := []struct {
		name          string
		packageKind   string
		dryRun        bool
		importErr     error
		quizCount     int
		quizErr       error
		preflightErr  error
		importedTitle string
		importedCount int
		bankName      string
		packageTitle  string
		wantErr       string
		wantPreflight bool
		wantImport    bool
		wantQuiz      bool
	}{
		{name: "dry run", packageKind: "valid", dryRun: true},
		{name: "dry run random quiz", packageKind: "valid", dryRun: true, quizCount: 2},
		{name: "non ZIP extension", packageKind: "non-zip", dryRun: true, wantErr: ".zip file"},
		{name: "missing ZIP", packageKind: "missing", dryRun: true, wantErr: "read package"},
		{name: "invalid ZIP", packageKind: "invalid", dryRun: true, wantErr: "open package"},
		{name: "missing root manifest", packageKind: "no-manifest", dryRun: true, wantErr: "lacks root imsmanifest.xml"},
		{name: "nested manifest", packageKind: "nested-manifest", dryRun: true, wantErr: "lacks root imsmanifest.xml"},
		{name: "import success", packageKind: "valid", wantImport: true},
		{name: "import error", packageKind: "valid", importErr: errors.New("browser failed"), wantErr: "browser failed", wantImport: true},
		{name: "negative random count", packageKind: "valid", quizCount: -1, wantErr: "must be non-negative"},
		{name: "importer does not honor requested bank name", packageKind: "valid", bankName: "throwaway", packageTitle: "Final Bank", importedTitle: "Final Bank", importedCount: 3, quizCount: 3, wantErr: "Item Bank title", wantPreflight: true, wantImport: true},
		{name: "random quiz success", packageKind: "valid", quizCount: 3, wantPreflight: true, wantImport: true, wantQuiz: true},
		{name: "random quiz error", packageKind: "valid", quizCount: 3, quizErr: errors.New("quiz failed"), wantErr: "quiz failed", wantPreflight: true, wantImport: true, wantQuiz: true},
		{name: "random count exceeds package", packageKind: "valid", quizCount: 4, wantErr: "exceeds package question count"},
		{name: "random quiz preflight error", packageKind: "valid", quizCount: 3, preflightErr: errors.New("quiz exists"), wantErr: "preflight random quiz", wantPreflight: true},
		{name: "random quiz import error", packageKind: "valid", quizCount: 3, importErr: errors.New("browser failed"), wantErr: "browser failed", wantPreflight: true, wantImport: true},
		{name: "imported title mismatch", packageKind: "valid", quizCount: 3, importedTitle: "Wrong", importedCount: 3, wantErr: "Item Bank title", wantPreflight: true, wantImport: true},
		{name: "imported count mismatch", packageKind: "valid", quizCount: 3, importedTitle: "Bank", importedCount: 2, wantErr: "question count", wantPreflight: true, wantImport: true},
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
				title := tt.packageTitle
				if title == "" {
					title = "Bank"
				}
				writeQTIPackage(t, path, title, 3, "imsmanifest.xml")
			case "invalid":
				if err := os.WriteFile(path, []byte("not a ZIP"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "no-manifest":
				writeZIP(t, path, "assessment.xml")
			case "nested-manifest":
				writeZIP(t, path, "nested/imsmanifest.xml")
			}

			fake := &fakeItemBankImporter{err: tt.importErr, result: itembank.Result{
				BankURL: "https://canvas/banks/1", BankID: "1", BankName: "Bank", QuestionCount: 3,
			}}
			if tt.importedTitle != "" {
				fake.result.BankName = tt.importedTitle
			}
			if tt.importedCount != 0 {
				fake.result.QuestionCount = tt.importedCount
			}
			quiz := &fakeRandomQuizCreator{err: tt.quizErr, preflightErr: tt.preflightErr}
			bankName := tt.bankName
			if bankName == "" {
				bankName = "Bank"
			}
			cmd := &ImportBankCmd{
				CourseID: "7", BankName: bankName, Package: path,
				BaseURL: "https://canvas.example.edu", BrowserURL: "http://127.0.0.1:9222",
				OnExisting: "append", DryRun: tt.dryRun, CreateRandomQuiz: tt.quizCount,
				importer: fake, quizCreator: quiz,
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
				if quiz.got != nil {
					t.Fatalf("CreateRandomQuiz() request = %+v, want no call", quiz.got)
				}
				if (quiz.preflightGot != nil) != tt.wantPreflight {
					t.Fatalf("PreflightRandomQuiz() request = %+v, want call %v", quiz.preflightGot, tt.wantPreflight)
				}
				return
			}
			if fake.got == nil {
				t.Fatal("Import() request = nil")
			}
			if fake.got.CourseID != "7" || fake.got.BankName != bankName ||
				fake.got.Package != path || fake.got.BaseURL != "https://canvas.example.edu" ||
				fake.got.BrowserURL != "http://127.0.0.1:9222" || fake.got.OnExisting != itembank.ExistingAppend {
				t.Fatalf("Import() request = %+v", fake.got)
			}
			packageTitle := tt.packageTitle
			if packageTitle == "" {
				packageTitle = "Bank"
			}
			if tt.quizCount > 0 && (fake.got.ExpectedBankName != packageTitle || fake.got.ExpectedItemCount != 3) {
				t.Fatalf("Import() expected package metadata = %+v", fake.got)
			}
			if !tt.wantQuiz {
				if quiz.got != nil {
					t.Fatalf("CreateRandomQuiz() request = %+v, want no call", quiz.got)
				}
				if (quiz.preflightGot != nil) != tt.wantPreflight {
					t.Fatalf("PreflightRandomQuiz() request = %+v, want call %v", quiz.preflightGot, tt.wantPreflight)
				}
				return
			}
			if quiz.preflightGot == nil {
				t.Fatal("PreflightRandomQuiz() request = nil")
			}
			if quiz.got == nil {
				t.Fatal("CreateRandomQuiz() request = nil")
			}
			if quiz.got.BankName != packageTitle || quiz.got.BankURL != "https://canvas/banks/1" || quiz.got.QuestionCount != tt.quizCount {
				t.Fatalf("CreateRandomQuiz() request = %+v", quiz.got)
			}
			wantQuizTitle := itembank.QuizTitle(packageTitle)
			if quiz.preflightTitle != wantQuizTitle || quiz.createTitle != wantQuizTitle {
				t.Fatalf("quiz titles = preflight %q/create %q, want %q", quiz.preflightTitle, quiz.createTitle, wantQuizTitle)
			}
		})
	}
}

func TestInspectQTIPackage_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		entries map[string]string
		want    qtiPackageInfo
		wantErr string
	}{
		{name: "valid", entries: map[string]string{
			"imsmanifest.xml": `<manifest><resources><resource type="imsqti_xmlv1p2"><file href="quiz.xml"/></resource></resources></manifest>`,
			"quiz.xml":        `<questestinterop xmlns="http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1"><assessment title=" Bank "><section><item/><item/></section></assessment></questestinterop>`,
		}, want: qtiPackageInfo{Title: "Bank", ItemCount: 2}},
		{name: "malformed manifest", entries: map[string]string{"imsmanifest.xml": `<manifest>`}, wantErr: "parse package manifest"},
		{name: "no QTI resource", entries: map[string]string{"imsmanifest.xml": `<manifest><resources/></manifest>`}, wantErr: "no QTI 1.2 resource"},
		{name: "multiple QTI resources", entries: map[string]string{"imsmanifest.xml": `<manifest><resources><resource type="imsqti_xmlv1p2"><file href="a.xml"/></resource><resource type="imsqti_xmlv1p2"><file href="b.xml"/></resource></resources></manifest>`}, wantErr: "multiple QTI 1.2 resources"},
		{name: "nested assessment", entries: map[string]string{"imsmanifest.xml": `<manifest><resources><resource type="imsqti_xmlv1p2"><file href="nested/quiz.xml"/></resource></resources></manifest>`}, wantErr: "root-level .xml"},
		{name: "non XML assessment", entries: map[string]string{"imsmanifest.xml": `<manifest><resources><resource type="imsqti_xmlv1p2"><file href="quiz.qti"/></resource></resources></manifest>`}, wantErr: "root-level .xml"},
		{name: "missing assessment", entries: map[string]string{"imsmanifest.xml": `<manifest><resources><resource type="imsqti_xmlv1p2"><file href="quiz.xml"/></resource></resources></manifest>`}, wantErr: "lacks referenced assessment"},
		{name: "malformed assessment", entries: map[string]string{
			"imsmanifest.xml": `<manifest><resources><resource type="imsqti_xmlv1p2"><file href="quiz.xml"/></resource></resources></manifest>`,
			"quiz.xml":        `<questestinterop><assessment title="Bank">`,
		}, wantErr: "parse package entry"},
		{name: "empty title", entries: map[string]string{
			"imsmanifest.xml": `<manifest><resources><resource type="imsqti_xmlv1p2"><file href="quiz.xml"/></resource></resources></manifest>`,
			"quiz.xml":        `<questestinterop><assessment title=" "><item/></assessment></questestinterop>`,
		}, wantErr: "assessment title is empty"},
		{name: "no items", entries: map[string]string{
			"imsmanifest.xml": `<manifest><resources><resource type="imsqti_xmlv1p2"><file href="quiz.xml"/></resource></resources></manifest>`,
			"quiz.xml":        `<questestinterop><assessment title="Bank"/></questestinterop>`,
		}, wantErr: "contains no QTI question items"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "quiz.zip")
			writeCustomZIP(t, path, tt.entries)
			got, err := inspectQTIPackage(path)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("inspectQTIPackage() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("inspectQTIPackage() error = %v, want %q", err, tt.wantErr)
			}
			if tt.wantErr == "" && got != tt.want {
				t.Fatalf("inspectQTIPackage() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
