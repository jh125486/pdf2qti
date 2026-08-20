// Whitebox tests inject Chromedp's unexported runner and exercise selector helpers.
package itembank

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestChromedpImporterImport_Table(t *testing.T) { //nolint:gocyclo // table covers browser state-machine failures
	t.Parallel()
	tests := []struct {
		name          string
		failAt        int
		existing      bool
		findErr       error
		onExisting    Existing
		expectedCalls int
		wantErr       string
		wantURL       string
	}{
		{name: "success", onExisting: ExistingAppend, expectedCalls: 15, wantURL: "https://canvas.example.edu/courses/7/banks/42"},
		{name: "existing bank append", existing: true, onExisting: ExistingAppend, expectedCalls: 8, wantURL: "https://canvas.example.edu/courses/7/banks/42"},
		{name: "existing bank fails", existing: true, onExisting: ExistingFail, expectedCalls: 1, wantErr: `item bank "Bank" already exists`},
		{name: "find bank error", findErr: errors.New("lookup failed"), onExisting: ExistingAppend, expectedCalls: 1, wantErr: "find Item Bank"},
		{name: "open banks", failAt: 1, onExisting: ExistingAppend, expectedCalls: 1, wantErr: "open Item Banks"},
		{name: "find bank", failAt: 2, onExisting: ExistingAppend, expectedCalls: 2, wantErr: "find Item Bank"},
		{name: "open create dialog", failAt: 3, onExisting: ExistingAppend, expectedCalls: 3, wantErr: "open create bank dialog"},
		{name: "bank name field", failAt: 4, onExisting: ExistingAppend, expectedCalls: 4, wantErr: "wait for bank-name field"},
		{name: "fill bank name", failAt: 5, onExisting: ExistingAppend, expectedCalls: 5, wantErr: "fill bank name"},
		{name: "share course", failAt: 6, onExisting: ExistingAppend, expectedCalls: 6, wantErr: "share bank with course"},
		{name: "submit create", failAt: 7, onExisting: ExistingAppend, expectedCalls: 7, wantErr: "submit create bank"},
		{name: "return to banks", failAt: 8, onExisting: ExistingAppend, expectedCalls: 8, wantErr: "return to Item Banks"},
		{name: "open bank", failAt: 9, onExisting: ExistingAppend, expectedCalls: 9, wantErr: "open Item Bank"},
		{name: "wait actions", failAt: 10, onExisting: ExistingAppend, expectedCalls: 10, wantErr: "wait for Item Bank actions"},
		{name: "open actions", failAt: 11, onExisting: ExistingAppend, expectedCalls: 11, wantErr: "open import actions"},
		{name: "open dialog", failAt: 12, onExisting: ExistingAppend, expectedCalls: 12, wantErr: "open import dialog"},
		{name: "attach package", failAt: 13, onExisting: ExistingAppend, expectedCalls: 13, wantErr: "attach package"},
		{name: "submit import", failAt: 14, onExisting: ExistingAppend, expectedCalls: 14, wantErr: "submit import"},
		{name: "wait completion", failAt: 15, onExisting: ExistingAppend, expectedCalls: 15, wantErr: "wait for import completion"},
		{name: "read location", failAt: 16, onExisting: ExistingAppend, expectedCalls: 16, wantErr: "read Item Bank URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			batchSizes := make([]int, 0, 16)
			importer := ChromedpImporter{run: func(_ context.Context, actions ...chromedp.Action) error {
				batchSizes = append(batchSizes, len(actions))
				calls++
				if calls == tt.failAt {
					return errors.New("browser failed")
				}
				return nil
			}, findBank: func(_ context.Context, name string) (bool, error) {
				if name != `"Bank"` {
					t.Fatalf("bank lookup name = %q, want %q", name, `"Bank"`)
				}
				return tt.existing, tt.findErr
			}}
			if !tt.existing && tt.findErr == nil {
				// Exercise production Evaluate path for normal create-bank flow and its
				// failure points; existing-bank cases use injected lookup result below.
				importer.findBank = nil
			}
			if tt.wantURL != "" {
				importer.location = func(context.Context) (string, error) { return tt.wantURL, nil }
			}
			result, err := importer.Import(context.Background(), &Request{
				BaseURL: "https://canvas.example.edu", BrowserURL: "http://127.0.0.1:9222",
				CourseID: "7", BankName: "Bank", Package: "quiz.zip", OnExisting: tt.onExisting,
			})
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Import() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Import() error = %v, want %q", err, tt.wantErr)
			}
			if calls != tt.expectedCalls {
				t.Fatalf("browser action batches = %d, want %d", calls, tt.expectedCalls)
			}
			if tt.wantURL != "" {
				if result.BankURL != tt.wantURL {
					t.Fatalf("Import() result = %+v, want URL %q", result, tt.wantURL)
				}
				if result.BankName != "Bank" {
					t.Fatalf("Import() result bank name = %q, want %q", result.BankName, "Bank")
				}
				if result.BankID != "42" {
					t.Fatalf("Import() result bank ID = %q, want %q", result.BankID, "42")
				}
				if len(batchSizes) < 2 || batchSizes[len(batchSizes)-2] != 2 {
					t.Fatalf("import submit action batch = %v, want penultimate batch size 2", batchSizes)
				}
			}
		})
	}
}

func TestChromedpImporterImport_VerifiesMetadata(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, title, expectedTitle, wantErr string
		count, expectedCount                int
		titleErr, countErr                  error
	}{
		{name: "match", title: "Bank", expectedTitle: "Bank", count: 3, expectedCount: 3},
		{name: "title mismatch", title: "Wrong", expectedTitle: "Bank", wantErr: `title = "Wrong"`},
		{name: "empty title", expectedTitle: "Bank", wantErr: "title is empty"},
		{name: "title read error", expectedTitle: "Bank", titleErr: errors.New("title unavailable"), wantErr: "read Item Bank title"},
		{name: "count mismatch", title: "Bank", expectedTitle: "Bank", count: 2, expectedCount: 3, wantErr: "question count = 2"},
		{name: "count read error", title: "Bank", expectedTitle: "Bank", expectedCount: 3, countErr: errors.New("count unavailable"), wantErr: "read imported Item Bank question count"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			importer := ChromedpImporter{
				run:           func(context.Context, ...chromedp.Action) error { calls++; return nil },
				findBank:      func(context.Context, string) (bool, error) { return true, nil },
				bankTitle:     func(context.Context) (string, error) { return tt.title, tt.titleErr },
				bankItemCount: func(context.Context) (int, error) { return tt.count, tt.countErr },
				location:      func(context.Context) (string, error) { return "https://canvas.example.edu/courses/7/banks/42", nil },
			}
			result, err := importer.Import(context.Background(), &Request{BaseURL: "https://canvas.example.edu", BrowserURL: "http://127.0.0.1:9222", CourseID: "7", BankName: "Bank", Package: "quiz.zip", OnExisting: ExistingAppend, ExpectedBankName: tt.expectedTitle, ExpectedItemCount: tt.expectedCount})
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Import() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Import() error = %v, want %q", err, tt.wantErr)
			}
			if calls != 8 {
				t.Fatalf("browser action batches = %d, want 8", calls)
			}
			if tt.wantErr == "" && (result.BankName != tt.expectedTitle || result.QuestionCount != tt.expectedCount) {
				t.Fatalf("Import() result = %+v", result)
			}
		})
	}
}

func TestChromedpImporterImport_RenamesBankToRequestedName(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                         string
		reqBankName, canvasTitle     string
		renameErr                    error
		wantErr                      string
		wantRenameCalled             bool
		wantOld, wantNew, wantResult string
	}{
		{name: "canvas renamed bank to QTI title, renamed back", reqBankName: "Module 1: Vectors and Matrices", canvasTitle: "Chapter 1 Quiz", wantRenameCalled: true, wantOld: "Chapter 1 Quiz", wantNew: "Module 1: Vectors and Matrices", wantResult: "Module 1: Vectors and Matrices"},
		{name: "already matches, no rename needed", reqBankName: "Bank", canvasTitle: "Bank", wantResult: "Bank"},
		{name: "rename fails", reqBankName: "Module 1: Vectors and Matrices", canvasTitle: "Chapter 1 Quiz", renameErr: errors.New("edit dialog unavailable"), wantErr: `rename Item Bank "Chapter 1 Quiz" to "Module 1: Vectors and Matrices": edit dialog unavailable`, wantRenameCalled: true, wantOld: "Chapter 1 Quiz", wantNew: "Module 1: Vectors and Matrices"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var renameCalled bool
			var gotOld, gotNew string
			importer := ChromedpImporter{
				run:       func(context.Context, ...chromedp.Action) error { return nil },
				findBank:  func(context.Context, string) (bool, error) { return true, nil },
				bankTitle: func(context.Context) (string, error) { return tt.canvasTitle, nil },
				renameBank: func(_ context.Context, oldTitle, newTitle string) error {
					renameCalled = true
					gotOld, gotNew = oldTitle, newTitle
					return tt.renameErr
				},
				location: func(context.Context) (string, error) { return "https://canvas.example.edu/courses/7/banks/42", nil },
			}
			result, err := importer.Import(context.Background(), &Request{
				BaseURL: "https://canvas.example.edu", BrowserURL: "http://127.0.0.1:9222", CourseID: "7",
				BankName: tt.reqBankName, Package: "quiz.zip", OnExisting: ExistingAppend,
				ExpectedBankName: tt.canvasTitle,
			})
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Import() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Import() error = %v, want %q", err, tt.wantErr)
			}
			if renameCalled != tt.wantRenameCalled {
				t.Fatalf("renameBank called = %v, want %v", renameCalled, tt.wantRenameCalled)
			}
			if tt.wantRenameCalled && (gotOld != tt.wantOld || gotNew != tt.wantNew) {
				t.Fatalf("renameBank(%q, %q), want (%q, %q)", gotOld, gotNew, tt.wantOld, tt.wantNew)
			}
			if tt.wantErr == "" && result.BankName != tt.wantResult {
				t.Fatalf("Import() result.BankName = %q, want %q", result.BankName, tt.wantResult)
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

func TestNewBrowserContext_RequiresProfileDirWhenHeadless(t *testing.T) {
	t.Parallel()
	_, _, err := newBrowserContext(t.Context(), "", "")
	if err == nil || !strings.Contains(err.Error(), "chrome profile directory is required") {
		t.Fatalf("newBrowserContext() error = %v, want profile-dir requirement", err)
	}
	ctx, cancel, err := newBrowserContext(t.Context(), "http://127.0.0.1:9222", "")
	if err != nil {
		t.Fatalf("newBrowserContext() with BrowserURL set = %v, want no error", err)
	}
	cancel()
	if ctx == nil {
		t.Fatal("newBrowserContext() returned nil context")
	}
}

func TestRunResilient_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		failTimes int
		errMsg    string
		wantCalls int
		wantErr   string
	}{
		{name: "succeeds first try", wantCalls: 1},
		{name: "retries transient context error then succeeds", failTimes: 2, errMsg: "Cannot find context with specified id (-32000)", wantCalls: 3},
		{name: "retries navigated-or-closed error then succeeds", failTimes: 1, errMsg: "Inspected target navigated or closed (-32000)", wantCalls: 2},
		{name: "exhausts retries on persistent transient error", failTimes: 99, errMsg: "context with specified id", wantCalls: 4, wantErr: "context with specified id"},
		{name: "non-transient error returns immediately", failTimes: 99, errMsg: "browser failed", wantCalls: 1, wantErr: "browser failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			run := func(context.Context, ...chromedp.Action) error {
				calls++
				if calls <= tt.failTimes {
					return errors.New(tt.errMsg)
				}
				return nil
			}
			err := runResilient(t.Context(), run, chromedp.Sleep(0))
			if tt.wantErr == "" && err != nil {
				t.Fatalf("runResilient() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("runResilient() error = %v, want %q", err, tt.wantErr)
			}
			if calls != tt.wantCalls {
				t.Fatalf("run() calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestRenameBankUI_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		failAt        int
		expectedCalls int
		wantErr       string
	}{
		{name: "success", expectedCalls: 6},
		{name: "open banks", failAt: 1, expectedCalls: 1, wantErr: "open Item Banks"},
		{name: "open edit dialog", failAt: 2, expectedCalls: 2, wantErr: "open edit bank dialog"},
		{name: "wait for field", failAt: 3, expectedCalls: 3, wantErr: "wait for bank-name field"},
		{name: "set name", failAt: 4, expectedCalls: 4, wantErr: "set bank name"},
		{name: "save", failAt: 5, expectedCalls: 5, wantErr: "save bank name"},
		{name: "reopen", failAt: 6, expectedCalls: 6, wantErr: "reopen renamed Item Bank"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			run := func(context.Context, ...chromedp.Action) error {
				calls++
				if calls == tt.failAt {
					return errors.New("browser failed")
				}
				return nil
			}
			err := renameBankUI(t.Context(), run, "https://canvas.example.edu/courses/7/banks", "Old Name", "New Name")
			if tt.wantErr == "" && err != nil {
				t.Fatalf("renameBankUI() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("renameBankUI() error = %v, want %q", err, tt.wantErr)
			}
			if calls != tt.expectedCalls {
				t.Fatalf("run() calls = %d, want %d", calls, tt.expectedCalls)
			}
		})
	}
}

func TestBankTitleMatchesJS_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, title string }{
		{name: "plain title", title: "My Bank"},
		{name: "title with quote", title: `Bank "One"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := bankTitleMatchesJS(tt.title); !strings.Contains(got, jsString(tt.title)) {
				t.Fatalf("bankTitleMatchesJS(%q) = %q, want it to embed the title", tt.title, got)
			}
		})
	}
}

func TestBankItemCountMatchesJS_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		count int
	}{
		{name: "zero", count: 0},
		{name: "eighteen", count: 18},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := bankItemCountMatchesJS(tt.count)
			if !strings.Contains(got, fmt.Sprintf("%d", tt.count)) {
				t.Fatalf("bankItemCountMatchesJS(%d) = %q, want it to embed the count", tt.count, got)
			}
			if !strings.Contains(got, bankItemCountJS) {
				t.Fatalf("bankItemCountMatchesJS(%d) = %q, want it to embed bankItemCountJS", tt.count, got)
			}
		})
	}
}

func TestQuizExistsJS_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, title string }{
		{name: "plain title", title: "Chapter 1 Quiz"},
		{name: "title with quote", title: `Chapter "1" Quiz`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := quizExistsJS(tt.title); !strings.Contains(got, jsString(tt.title)) {
				t.Fatalf("quizExistsJS(%q) = %q, want it to embed the title", tt.title, got)
			}
		})
	}
}

func TestChromedpImporterEnsureSession_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                  string
		loginPage             bool
		loginPageErr          error
		username, password    string
		submitLoginErr        error
		wantErr               string
		wantSubmitLoginCalled bool
	}{
		{name: "already authenticated, no login attempted", loginPage: false},
		{name: "login page detection error", loginPageErr: errors.New("evaluate failed"), wantErr: "check Canvas login state"},
		{name: "login page but no credentials", loginPage: true, wantErr: "set CANVAS_USERNAME and CANVAS_PASSWORD"},
		{name: "login page, credentials submitted", loginPage: true, username: "eagle", password: "hunter2", wantSubmitLoginCalled: true},
		{name: "login submission fails", loginPage: true, username: "eagle", password: "hunter2", submitLoginErr: errors.New("invalid credentials"), wantErr: "invalid credentials", wantSubmitLoginCalled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var submitLoginCalled bool
			var gotUser, gotPass string
			importer := ChromedpImporter{
				run:               func(context.Context, ...chromedp.Action) error { return nil },
				loginPageDetected: func(context.Context) (bool, error) { return tt.loginPage, tt.loginPageErr },
				submitLogin: func(_ context.Context, user, pass string) error {
					submitLoginCalled = true
					gotUser, gotPass = user, pass
					return tt.submitLoginErr
				},
			}
			err := importer.ensureSession(t.Context(), func(context.Context, ...chromedp.Action) error { return nil },
				"https://canvas.example.edu", tt.username, tt.password)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("ensureSession() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("ensureSession() error = %v, want %q", err, tt.wantErr)
			}
			if submitLoginCalled != tt.wantSubmitLoginCalled {
				t.Fatalf("submitLogin called = %v, want %v", submitLoginCalled, tt.wantSubmitLoginCalled)
			}
			if tt.wantSubmitLoginCalled && (gotUser != tt.username || gotPass != tt.password) {
				t.Fatalf("submitLogin(%q, %q), want (%q, %q)", gotUser, gotPass, tt.username, tt.password)
			}
		})
	}
}

func TestChromedpImporterEnsureSession_ProductionPaths(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		failAt  int
		wantErr string
	}{
		{name: "already authenticated via real Evaluate", wantErr: ""},
		{name: "login state check fails", failAt: 2, wantErr: "check Canvas login state"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			run := func(context.Context, ...chromedp.Action) error {
				calls++
				if calls == tt.failAt {
					return errors.New("browser failed")
				}
				return nil
			}
			// No loginPageDetected/submitLogin stubs: exercises the real
			// chromedp.Evaluate/fill/poll paths. The out-param stays false
			// since run() is stubbed rather than a real browser, so this
			// always takes the "already authenticated" branch once past
			// the login-state check.
			importer := ChromedpImporter{}
			err := importer.ensureSession(t.Context(), run, "https://canvas.example.edu", "eagle", "hunter2")
			if tt.wantErr == "" && err != nil {
				t.Fatalf("ensureSession() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("ensureSession() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestChromedpImporterEnsureSession_ProductionLoginFill(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		failAt  int
		wantErr string
	}{
		{name: "fills and submits via real chromedp actions", failAt: 0},
		{name: "submit fails", failAt: 2, wantErr: "submit Canvas login"},
		{name: "poll for success fails", failAt: 3, wantErr: "log in to Canvas"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			// loginPageDetected is stubbed to force the login-form branch;
			// everything after that (fill, submit, poll) runs through the
			// real chromedp.Action construction via the run stub below, so
			// this exercises that code without a real browser.
			run := func(context.Context, ...chromedp.Action) error {
				calls++
				if calls == tt.failAt {
					return errors.New("browser failed")
				}
				return nil
			}
			importer := ChromedpImporter{
				loginPageDetected: func(context.Context) (bool, error) { return true, nil },
			}
			err := importer.ensureSession(t.Context(), run, "https://canvas.example.edu", "eagle", "hunter2")
			if tt.wantErr == "" && err != nil {
				t.Fatalf("ensureSession() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("ensureSession() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestChromedpImporterImport_HeadlessEnsuresSession(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name             string
		chromeProfileDir string
		loginPage        bool
		username         string
		password         string
		wantErr          string
	}{
		{name: "missing profile dir fails before any browser action", wantErr: "chrome profile directory is required"},
		{name: "already authenticated profile proceeds headlessly", chromeProfileDir: "/tmp/canvas-profile"},
		{name: "expired session logs in then proceeds", chromeProfileDir: "/tmp/canvas-profile", loginPage: true, username: "eagle", password: "hunter2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var loginChecked, loggedIn bool
			importer := ChromedpImporter{
				run:      func(context.Context, ...chromedp.Action) error { return nil },
				findBank: func(context.Context, string) (bool, error) { return true, nil },
				loginPageDetected: func(context.Context) (bool, error) {
					loginChecked = true
					return tt.loginPage, nil
				},
				submitLogin: func(context.Context, string, string) error {
					loggedIn = true
					return nil
				},
				location: func(context.Context) (string, error) { return "https://canvas.example.edu/courses/7/banks/42", nil },
			}
			_, err := importer.Import(t.Context(), &Request{
				BaseURL: "https://canvas.example.edu", ChromeProfileDir: tt.chromeProfileDir,
				Username: tt.username, Password: tt.password,
				CourseID: "7", BankName: "Bank", Package: "quiz.zip", OnExisting: ExistingAppend,
			})
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Import() error = %v", err)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Import() error = %v, want %q", err, tt.wantErr)
				}
				if loginChecked {
					t.Fatal("ensureSession ran despite missing profile dir")
				}
				return
			}
			if !loginChecked {
				t.Fatal("headless Import() never checked Canvas login state")
			}
			if loggedIn != tt.loginPage {
				t.Fatalf("submitLogin called = %v, want %v", loggedIn, tt.loginPage)
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

func TestBankIDAndQuizTitle_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, rawURL, bank, wantID, wantTitle string
	}{
		{name: "bank URL", rawURL: "https://canvas.example/courses/7/banks/42", bank: "Chapter 1", wantID: "42", wantTitle: "Chapter 1 Quiz"},
		{name: "quiz suffix", rawURL: "https://canvas.example/courses/7/banks/42?x=1", bank: "Chapter 1 Quiz", wantID: "42", wantTitle: "Chapter 1 Quiz"},
		{name: "case insensitive suffix", bank: "Exam QUIZ", wantTitle: "Exam QUIZ"},
		{name: "invalid URL", rawURL: "://bad", bank: "Quizlet", wantTitle: "Quizlet Quiz"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := bankIDFromURL(tt.rawURL); got != tt.wantID {
				t.Fatalf("bankIDFromURL(%q) = %q, want %q", tt.rawURL, got, tt.wantID)
			}
			if got := QuizTitle(tt.bank); got != tt.wantTitle {
				t.Fatalf("QuizTitle(%q) = %q, want %q", tt.bank, got, tt.wantTitle)
			}
		})
	}
}

func TestChromedpImporterCreateRandomQuiz_Table(t *testing.T) { //nolint:gocyclo // table covers browser state-machine failures
	t.Parallel()
	tests := []struct {
		name          string
		count         int
		failAt        int
		locationErr   error
		collision     bool
		collisionErr  error
		wantErr       string
		wantURL       string
		wantTitle     string
		expectedCalls int
	}{
		{name: "append title", count: 3, wantURL: "https://canvas.example.edu/courses/7/quizzes/9", wantTitle: "Bank Quiz", expectedCalls: 22},
		{name: "existing quiz suffix", count: 2, wantURL: "https://canvas.example.edu/courses/7/quizzes/9", wantTitle: "Bank Quiz", expectedCalls: 22},
		{name: "invalid request", wantErr: "request is required"},
		{name: "invalid count", count: 0, wantErr: "must be positive"},
		{name: "invalid base URL", count: 2, wantErr: "invalid Canvas base URL"},
		{name: "open quizzes", count: 2, failAt: 1, expectedCalls: 1, wantErr: "open Quizzes"},
		{name: "set title", count: 2, failAt: 7, expectedCalls: 7, wantErr: "set quiz title"},
		{name: "build quiz", count: 2, failAt: 8, expectedCalls: 8, wantErr: "build quiz"},
		{name: "save group", count: 2, failAt: 21, expectedCalls: 21, wantErr: "save Item Bank group"},
		{name: "verify group", count: 2, failAt: 22, expectedCalls: 22, wantErr: "verify random group"},
		{name: "read URL", count: 2, locationErr: errors.New("location unavailable"), expectedCalls: 22, wantErr: "read quiz URL"},
		{name: "collision", count: 2, collision: true, expectedCalls: 1, wantErr: "already exists"},
		{name: "collision check error", count: 2, collisionErr: errors.New("lookup unavailable"), expectedCalls: 1, wantErr: "check quiz title collision"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			creator := ChromedpImporter{
				run: func(_ context.Context, _ ...chromedp.Action) error {
					calls++
					if calls == tt.failAt {
						return errors.New("browser failed")
					}
					return nil
				},
				quizLocation: func(context.Context) (string, error) {
					return "https://canvas.example.edu/courses/7/quizzes/9", tt.locationErr
				},
				quizExists: func(context.Context, string) (bool, error) { return tt.collision, tt.collisionErr },
			}
			req := &QuizRequest{BaseURL: "https://canvas.example.edu", BrowserURL: "http://127.0.0.1:9222", CourseID: "7", BankID: "42", BankName: "Bank", QuestionCount: tt.count}
			if tt.name == "existing quiz suffix" {
				req.BankName = "Bank Quiz"
			}
			if tt.name == "invalid request" {
				req = nil
			}
			if tt.name == "invalid base URL" {
				req.BaseURL = "://bad"
			}
			result, err := creator.CreateRandomQuiz(context.Background(), req)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("CreateRandomQuiz() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("CreateRandomQuiz() error = %v, want %q", err, tt.wantErr)
			}
			if calls != tt.expectedCalls {
				t.Fatalf("browser action batches = %d, want %d", calls, tt.expectedCalls)
			}
			if tt.wantURL != "" {
				if result.QuizURL != tt.wantURL || result.Title != tt.wantTitle {
					t.Fatalf("CreateRandomQuiz() result = %+v, want URL %q/title %q", result, tt.wantURL, tt.wantTitle)
				}
			}
		})
	}
}

func TestChromedpImporterPreflightRandomQuiz_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, baseURL, bankName, wantErr string
		count, failAt                    int
		collision                        bool
		collisionErr                     error
		wantCalls                        int
	}{
		{name: "available", baseURL: "https://canvas.example.edu", bankName: "Bank", count: 3, wantCalls: 1},
		{name: "nil request", wantErr: "request is required"},
		{name: "invalid count", baseURL: "https://canvas.example.edu", bankName: "Bank", wantErr: "must be positive"},
		{name: "empty bank", baseURL: "https://canvas.example.edu", count: 3, wantErr: "name is required"},
		{name: "invalid URL", baseURL: "://bad", bankName: "Bank", count: 3, wantErr: "invalid Canvas base URL"},
		{name: "open error", baseURL: "https://canvas.example.edu", bankName: "Bank", count: 3, failAt: 1, wantCalls: 1, wantErr: "open Quizzes"},
		{name: "collision", baseURL: "https://canvas.example.edu", bankName: "Bank", count: 3, collision: true, wantCalls: 1, wantErr: "already exists"},
		{name: "collision error", baseURL: "https://canvas.example.edu", bankName: "Bank", count: 3, collisionErr: errors.New("lookup failed"), wantCalls: 1, wantErr: "check quiz title collision"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			creator := ChromedpImporter{
				run: func(context.Context, ...chromedp.Action) error {
					calls++
					if calls == tt.failAt {
						return errors.New("browser failed")
					}
					return nil
				},
				quizExists: func(context.Context, string) (bool, error) { return tt.collision, tt.collisionErr },
			}
			var req *QuizRequest
			if tt.name != "nil request" {
				req = &QuizRequest{BaseURL: tt.baseURL, BrowserURL: "http://127.0.0.1:9222", CourseID: "7", BankName: tt.bankName, QuestionCount: tt.count}
			}
			err := creator.PreflightRandomQuiz(context.Background(), req)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("PreflightRandomQuiz() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("PreflightRandomQuiz() error = %v, want %q", err, tt.wantErr)
			}
			if calls != tt.wantCalls {
				t.Fatalf("browser action batches = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}
