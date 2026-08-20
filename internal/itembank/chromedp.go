package itembank

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// ChromedpImporter drives only documented Canvas UI actions. When BrowserURL
// is set on a request it attaches to an existing, already-authenticated
// Chrome debugging session (manual escape hatch); otherwise it launches
// headless Chrome against a persisted profile directory and logs in itself
// (see ensureSession) if that profile's session is stale or nonexistent.
type ChromedpImporter struct {
	run               chromedpRun
	findBank          func(context.Context, string) (bool, error)
	location          func(context.Context) (string, error)
	bankTitle         func(context.Context) (string, error)
	bankItemCount     func(context.Context) (int, error)
	renameBank        func(context.Context, string, string) error
	quizLocation      func(context.Context) (string, error)
	quizExists        func(context.Context, string) (bool, error)
	loginPageDetected func(context.Context) (bool, error)
	submitLogin       func(context.Context, string, string) error
}

type chromedpRun func(context.Context, ...chromedp.Action) error

const buildQuizStep = "build quiz"

// runResilient retries action a few times if it fails with a CDP error
// indicating the execution context was torn down mid-evaluate by a
// navigation the previous action triggered — a timing race, not a selector
// or logic bug, and a fixed pre-emptive sleep before the next action isn't
// reliably long enough. Any other error (including everything unit tests
// inject) returns immediately on the first attempt, so this is a no-op
// outside that one specific race.
func runResilient(ctx context.Context, run chromedpRun, action chromedp.Action) error {
	var lastErr error
	for range 4 {
		lastErr = run(ctx, action)
		if lastErr == nil {
			return nil
		}
		msg := lastErr.Error()
		if !strings.Contains(msg, "context with specified id") && !strings.Contains(msg, "navigated or closed") {
			return lastErr
		}
		time.Sleep(500 * time.Millisecond)
	}
	return lastErr
}

// newBrowserContext creates a chromedp browser context. When browserURL is
// set it attaches to that existing debugging session (manual escape hatch,
// assumed already authenticated). Otherwise it launches headless Chrome
// against profileDir, a persisted profile directory: Canvas session cookies
// saved there by a prior successful login carry over, so most runs need no
// login at all.
func newBrowserContext(ctx context.Context, browserURL, profileDir string) (context.Context, context.CancelFunc, error) {
	var allocCtx context.Context
	var allocCancel context.CancelFunc
	if browserURL != "" {
		allocCtx, allocCancel = chromedp.NewRemoteAllocator(ctx, browserURL)
	} else {
		if strings.TrimSpace(profileDir) == "" {
			return nil, nil, fmt.Errorf("chrome profile directory is required for headless Canvas automation")
		}
		opts := append(append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...),
			chromedp.UserDataDir(profileDir),
			chromedp.Flag("headless", "new"),
			// Headless Chrome's default viewport is small enough to trip
			// Canvas's responsive/mobile layout, which hides desktop
			// controls (e.g. "Create Bank") behind a collapsed menu the
			// desktop-oriented selectors in this file don't look for.
			chromedp.WindowSize(1440, 1024),
		)
		allocCtx, allocCancel = chromedp.NewExecAllocator(ctx, opts...)
	}
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	return browserCtx, func() {
		browserCancel()
		allocCancel()
	}, nil
}

// ensureSession makes sure the browser is authenticated with Canvas before
// any UI automation runs. It navigates to base and, only if Canvas presents
// its login form (a stale or nonexistent headless-profile session), fills it
// from username/password and waits for the redirect back into the app. It is
// a no-op once the profile's session is valid, so most invocations pay only
// the cost of the initial navigate+check.
func (c ChromedpImporter) ensureSession(ctx context.Context, run chromedpRun, base, username, password string) error { //nolint:gocritic // ChromedpImporter is passed by value throughout this file
	if err := run(ctx, chromedp.Navigate(base), chromedp.WaitReady("body", chromedp.ByQuery), chromedp.Sleep(time.Second)); err != nil {
		return fmt.Errorf("open Canvas: %w", err)
	}
	var loginPage bool
	var checkErr error
	if c.loginPageDetected != nil {
		loginPage, checkErr = c.loginPageDetected(ctx)
	} else {
		checkErr = run(ctx, chromedp.Evaluate(loginPageDetectedJS, &loginPage))
	}
	if checkErr != nil {
		return fmt.Errorf("check Canvas login state: %w", checkErr)
	}
	if !loginPage {
		return nil
	}
	if username == "" || password == "" {
		return fmt.Errorf("canvas session expired or missing; set CANVAS_USERNAME and CANVAS_PASSWORD to log in automatically")
	}
	if c.submitLogin != nil {
		return c.submitLogin(ctx, username, password)
	}
	if err := run(ctx,
		chromedp.WaitVisible(usernameSelector, chromedp.ByQuery),
		chromedp.SendKeys(usernameSelector, username, chromedp.ByQuery),
		chromedp.SendKeys(passwordSelector, password, chromedp.ByQuery),
		chromedp.Click(loginButtonSelector, chromedp.ByQuery),
		// Submitting triggers a real navigation (often through an SSO
		// redirect chain); evaluating JS in the same instant the execution
		// context is torn down for that navigation intermittently fails
		// with a CDP "target navigated or closed" error. Let the new
		// document settle and become ready before polling its state.
		chromedp.Sleep(2*time.Second),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("submit Canvas login: %w", err)
	}
	if err := runResilient(ctx, run, chromedp.Poll(loginSucceededJS, nil, chromedp.WithPollingTimeout(20*time.Second))); err != nil {
		return fmt.Errorf("log in to Canvas (check CANVAS_USERNAME / CANVAS_PASSWORD): %w", err)
	}
	return nil
}

func (c ChromedpImporter) Import(ctx context.Context, req *Request) (Result, error) { //nolint:gocyclo,gocritic // browser UI state machine; ChromedpImporter passed by value throughout this file
	if req == nil {
		return Result{}, fmt.Errorf("item bank import request is required")
	}
	base, err := url.Parse(req.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return Result{}, fmt.Errorf("invalid Canvas base URL %q", req.BaseURL)
	}
	browser, cancel, err := newBrowserContext(ctx, req.BrowserURL, req.ChromeProfileDir)
	if err != nil {
		return Result{}, err
	}
	defer cancel()
	browser, workCancel := context.WithTimeout(browser, 150*time.Second)
	defer workCancel()
	run := c.run
	if run == nil {
		run = chromedp.Run
	}
	if req.BrowserURL == "" {
		if err := c.ensureSession(browser, run, req.BaseURL, req.Username, req.Password); err != nil {
			return Result{}, err
		}
	}
	base.Path = "/courses/" + url.PathEscape(req.CourseID) + "/banks"
	base.RawQuery = ""

	if err := run(browser, chromedp.Navigate(base.String()), chromedp.WaitReady("body", chromedp.ByQuery), chromedp.Sleep(time.Second)); err != nil {
		return Result{}, fmt.Errorf("open Item Banks: %w", err)
	}
	name := jsString(req.BankName)
	var found bool
	var findErr error
	if c.findBank != nil {
		found, findErr = c.findBank(browser, name)
	} else {
		findErr = run(browser, chromedp.Evaluate(bankExistsJS(name), &found))
	}
	if findErr != nil {
		return Result{}, fmt.Errorf("find Item Bank: %w", findErr)
	}
	if found && req.OnExisting == ExistingFail {
		return Result{}, fmt.Errorf("item bank %q already exists (use --on-existing=append)", req.BankName)
	}
	if !found {
		if err := run(browser, clickText("Create Bank")); err != nil {
			return Result{}, fmt.Errorf("open create bank dialog: %w", err)
		}
		if err := run(browser, chromedp.WaitVisible("[role=dialog] input", chromedp.ByQuery)); err != nil {
			return Result{}, fmt.Errorf("wait for bank-name field: %w", err)
		}
		if err := run(browser, chromedp.SendKeys("[role=dialog] input", req.BankName, chromedp.ByQuery)); err != nil {
			return Result{}, fmt.Errorf("fill bank name: %w", err)
		}
		if err := run(browser, chromedp.Click("[role=dialog] input[type=checkbox]", chromedp.ByQuery)); err != nil {
			return Result{}, fmt.Errorf("share bank with course: %w", err)
		}
		if err := run(browser, clickTextInDialog("Create Bank"), chromedp.Sleep(500*time.Millisecond)); err != nil {
			return Result{}, fmt.Errorf("submit create bank: %w", err)
		}
		if err := run(browser, chromedp.Navigate(base.String()), chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
			return Result{}, fmt.Errorf("return to Item Banks: %w", err)
		}
	}
	if err := run(browser, clickText(req.BankName)); err != nil {
		return Result{}, fmt.Errorf("open Item Bank %q: %w", req.BankName, err)
	}
	if err := run(browser, chromedp.WaitVisible(`button[aria-haspopup="true"]`, chromedp.ByQuery)); err != nil {
		return Result{}, fmt.Errorf("wait for Item Bank actions: %w", err)
	}

	if err := run(browser, chromedp.Click(`button[data-popover-trigger="true"]`, chromedp.ByQuery)); err != nil {
		return Result{}, fmt.Errorf("open import actions: %w", err)
	}
	if err := run(browser, chromedp.Click(`[role="menuitem"]`, chromedp.ByQuery)); err != nil {
		return Result{}, fmt.Errorf("open import dialog: %w", err)
	}
	if err := run(browser, chromedp.SetUploadFiles("input[type=file]", []string{req.Package}, chromedp.ByQuery)); err != nil {
		return Result{}, fmt.Errorf("attach package: %w", err)
	}
	if err := run(browser,
		chromedp.WaitEnabled(`//*[@role='dialog']//button[contains(normalize-space(), 'Import')]`, chromedp.BySearch),
		chromedp.Click(`//*[@role='dialog']//button[contains(normalize-space(), 'Import')]`, chromedp.BySearch),
	); err != nil {
		return Result{}, fmt.Errorf("submit import: %w", err)
	}
	if err := run(browser, chromedp.Poll(`document.body.innerText.includes('has imported successfully!')`, nil, chromedp.WithPollingTimeout(60*time.Second))); err != nil {
		return Result{}, fmt.Errorf("wait for import completion: %w", err)
	}
	bankTitle := req.BankName
	if req.ExpectedBankName != "" {
		var titleErr error
		if c.bankTitle != nil {
			bankTitle, titleErr = c.bankTitle(browser)
		} else {
			titleErr = run(browser,
				chromedp.Poll(bankTitleMatchesJS(req.ExpectedBankName), nil, chromedp.WithPollingTimeout(30*time.Second)),
				chromedp.Evaluate(bankTitleJS, &bankTitle),
			)
		}
		if titleErr != nil {
			return Result{}, fmt.Errorf("read Item Bank title: %w", titleErr)
		}
		bankTitle = strings.TrimSpace(bankTitle)
		if bankTitle == "" {
			return Result{}, fmt.Errorf("imported Item Bank title is empty")
		}
		if bankTitle != req.ExpectedBankName {
			return Result{}, fmt.Errorf("imported Item Bank title = %q, want %q", bankTitle, req.ExpectedBankName)
		}
		// Canvas names a New Quizzes Item Bank after the imported QTI
		// package's assessment title, discarding req.BankName even when
		// importing into an existing, correctly-named bank. Rename it back
		// to what was requested so --bank-name is actually honored.
		if req.BankName != "" && bankTitle != req.BankName {
			var renameErr error
			if c.renameBank != nil {
				renameErr = c.renameBank(browser, bankTitle, req.BankName)
			} else {
				renameErr = renameBankUI(browser, run, base.String(), bankTitle, req.BankName)
			}
			if renameErr != nil {
				return Result{}, fmt.Errorf("rename Item Bank %q to %q: %w", bankTitle, req.BankName, renameErr)
			}
			bankTitle = req.BankName
		}
	}
	itemCount := 0
	if req.ExpectedItemCount > 0 {
		var countErr error
		if c.bankItemCount != nil {
			itemCount, countErr = c.bankItemCount(browser)
		} else {
			countErr = run(browser,
				chromedp.Poll(bankItemCountMatchesJS(req.ExpectedItemCount), nil, chromedp.WithPollingTimeout(30*time.Second)),
				chromedp.Evaluate(bankItemCountJS, &itemCount),
			)
		}
		if countErr != nil {
			return Result{}, fmt.Errorf("read imported Item Bank question count: %w", countErr)
		}
		if itemCount != req.ExpectedItemCount {
			return Result{}, fmt.Errorf("imported Item Bank question count = %d, want %d", itemCount, req.ExpectedItemCount)
		}
	}
	var location string
	var locationErr error
	if c.location != nil {
		location, locationErr = c.location(browser)
	} else {
		locationErr = run(browser, chromedp.Location(&location))
	}
	if locationErr != nil {
		return Result{}, fmt.Errorf("read Item Bank URL: %w", locationErr)
	}
	return Result{BankURL: location, BankID: bankIDFromURL(location), BankName: bankTitle, QuestionCount: itemCount}, nil
}

// PreflightRandomQuiz checks that the derived quiz title is available before
// Item Bank import mutates Canvas.
func (c ChromedpImporter) PreflightRandomQuiz(ctx context.Context, req *QuizRequest) error { //nolint:gocritic // ChromedpImporter is passed by value throughout this file
	if err := validateQuizRequest(req, false); err != nil {
		return err
	}
	base, err := canvasURL(req.BaseURL, req.CourseID, "/quizzes")
	if err != nil {
		return err
	}
	browser, cancel, err := newBrowserContext(ctx, req.BrowserURL, req.ChromeProfileDir)
	if err != nil {
		return err
	}
	defer cancel()
	browser, workCancel := context.WithTimeout(browser, 90*time.Second)
	defer workCancel()
	run := c.run
	if run == nil {
		run = chromedp.Run
	}
	if req.BrowserURL == "" {
		if err := c.ensureSession(browser, run, req.BaseURL, req.Username, req.Password); err != nil {
			return err
		}
	}
	if err := run(browser, chromedp.Navigate(base), chromedp.WaitReady("body", chromedp.ByQuery), chromedp.Sleep(time.Second)); err != nil {
		return fmt.Errorf("open Quizzes: %w", err)
	}
	return c.checkQuizCollision(browser, run, QuizTitle(req.BankName))
}

// CreateRandomQuiz drives Canvas's New Quizzes builder. Canvas does not expose
// creation of Item Bank-backed quiz groups through its public API, so this
// intentionally uses only visible UI controls in an authenticated browser.
func (c ChromedpImporter) CreateRandomQuiz(ctx context.Context, req *QuizRequest) (QuizResult, error) { //nolint:gocyclo,gocritic // browser UI state machine; ChromedpImporter passed by value throughout this file
	if err := validateQuizRequest(req, true); err != nil {
		return QuizResult{}, err
	}
	base, err := canvasURL(req.BaseURL, req.CourseID, "/quizzes")
	if err != nil {
		return QuizResult{}, err
	}
	browser, cancel, err := newBrowserContext(ctx, req.BrowserURL, req.ChromeProfileDir)
	if err != nil {
		return QuizResult{}, err
	}
	defer cancel()
	browser, workCancel := context.WithTimeout(browser, 180*time.Second)
	defer workCancel()
	run := c.run
	if run == nil {
		run = chromedp.Run
	}
	if req.BrowserURL == "" {
		if err := c.ensureSession(browser, run, req.BaseURL, req.Username, req.Password); err != nil {
			return QuizResult{}, err
		}
	}

	quizTitle := QuizTitle(req.BankName)
	if err := run(browser, chromedp.Navigate(base), chromedp.WaitReady("body", chromedp.ByQuery), chromedp.Sleep(time.Second)); err != nil {
		return QuizResult{}, fmt.Errorf("open Quizzes: %w", err)
	}
	if err := c.checkQuizCollision(browser, run, quizTitle); err != nil {
		return QuizResult{}, err
	}
	steps := []struct {
		label  string
		action chromedp.Action
	}{
		// The visible "+" is a separate icon, not part of the button's
		// textContent (recorded live: the actual text is "Quiz/Survey").
		{"open quiz creator", clickTextInsensitive("Quiz/Survey")},
		// Clicking navigates to the quiz builder; evaluating JS in the same
		// instant the execution context is torn down for that navigation
		// intermittently fails with a CDP "context not found" error. Let the
		// new document settle before polling its state (same race as the
		// Canvas login submit in ensureSession).
		{"let quiz builder navigation settle", chromedp.Sleep(2 * time.Second)},
		{"wait for quiz setup", chromedp.Poll(quizSetupReadyJS, nil, chromedp.WithPollingTimeout(10*time.Second))},
		{"select New Quizzes if prompted", chromedp.Evaluate(selectNewQuizEngineJS, nil)},
		{"wait for quiz title", chromedp.WaitVisible(quizTitleSelector, chromedp.ByQuery)},
		{"set quiz title", chromedp.SetValue(quizTitleSelector, quizTitle, chromedp.ByQuery)},
		{buildQuizStep, clickTextInsensitive("Build")},
		// Same navigation race as above: "Build" navigates to the quiz's
		// build page.
		{"let quiz build navigation settle", chromedp.Sleep(2 * time.Second)},
		{"wait for Item Bank action", chromedp.Poll(addFromBankReadyJS, nil, chromedp.WithPollingTimeout(30*time.Second))},
		{"open Item Bank picker", clickTextInsensitive("Add from item bank")},
		{"select Item Bank", clickBank(req.BankName)},
		{"add Item Bank to quiz", clickTextInsensitive("Add this bank to quiz")},
		// Recorded live: there is no "Edit bank group" control. The added
		// bank group instead exposes an "All / Random" toggle directly.
		{"edit Item Bank group", clickTextInsensitive("All / Random")},
		{"enable random questions", clickTextInsensitive("Randomly select questions")},
		{"wait for question count", chromedp.WaitVisible(randomCountSelector, chromedp.ByQuery)},
		{"set question count", chromedp.SetValue(randomCountSelector, fmt.Sprintf("%d", req.QuestionCount), chromedp.ByQuery)},
		{"save Item Bank group", clickTextInsensitive("Done")},
		{"verify random group", chromedp.Poll(randomGroupJS(req.BankName, req.QuestionCount), nil, chromedp.WithPollingTimeout(10*time.Second))},
	}
	for _, step := range steps {
		if err := runResilient(browser, run, step.action); err != nil {
			return QuizResult{}, fmt.Errorf("%s: %w", step.label, err)
		}
	}
	var quizURL string
	var locationErr error
	if c.quizLocation != nil {
		quizURL, locationErr = c.quizLocation(browser)
	} else {
		locationErr = run(browser, chromedp.Location(&quizURL))
	}
	if locationErr != nil {
		return QuizResult{}, fmt.Errorf("read quiz URL: %w", locationErr)
	}
	return QuizResult{QuizURL: quizURL, Title: quizTitle, QuestionCount: req.QuestionCount}, nil
}

func validateQuizRequest(req *QuizRequest, requireBankID bool) error {
	if req == nil {
		return fmt.Errorf("random quiz request is required")
	}
	if req.QuestionCount <= 0 {
		return fmt.Errorf("random quiz question count must be positive: %d", req.QuestionCount)
	}
	if strings.TrimSpace(req.BankName) == "" {
		return fmt.Errorf("random quiz Item Bank name is required")
	}
	if requireBankID && strings.TrimSpace(req.BankID) == "" {
		return fmt.Errorf("random quiz Item Bank ID is required")
	}
	return nil
}

// renameBankUI renames an Item Bank through the Banks list's "Edit bank"
// dialog, then reopens the bank under its new title so callers land back on
// the bank detail page they were on before the rename.
func renameBankUI(ctx context.Context, run chromedpRun, banksURL, oldTitle, newTitle string) error {
	if err := run(ctx, chromedp.Navigate(banksURL), chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
		return fmt.Errorf("open Item Banks: %w", err)
	}
	// The accessible name ("Edit bank {title}") comes from a visually-hidden
	// text node inside the button, not a literal aria-label attribute, so
	// this matches on text content like the rest of this file's helpers do.
	if err := run(ctx, clickText("Edit bank "+oldTitle)); err != nil {
		return fmt.Errorf("open edit bank dialog: %w", err)
	}
	if err := run(ctx, chromedp.WaitVisible("[role=dialog] input", chromedp.ByQuery)); err != nil {
		return fmt.Errorf("wait for bank-name field: %w", err)
	}
	// chromedp.Clear sets the DOM value directly, which this React-controlled
	// input ignores (its own onChange-tracked state is untouched, so a
	// following SendKeys appends to the old title instead of replacing it).
	// Deterministically move to the end and backspace the known old title
	// length instead, then type: real key events React's handlers see.
	// One Backspace per character, not per byte: a byte count would send too
	// few (or too many, when it also overcounts) key events for a
	// non-ASCII title and leave stale characters behind.
	clearOldTitle := strings.Repeat(kb.Backspace, utf8.RuneCountInString(oldTitle))
	if err := run(ctx,
		chromedp.Click("[role=dialog] input", chromedp.ByQuery),
		chromedp.SendKeys("[role=dialog] input", kb.End+clearOldTitle+newTitle, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("set bank name: %w", err)
	}
	if err := run(ctx, clickTextInDialog("Save Changes"), chromedp.Sleep(500*time.Millisecond)); err != nil {
		return fmt.Errorf("save bank name: %w", err)
	}
	if err := run(ctx, chromedp.Navigate(banksURL), chromedp.WaitReady("body", chromedp.ByQuery), clickText(newTitle)); err != nil {
		return fmt.Errorf("reopen renamed Item Bank: %w", err)
	}
	return nil
}

func (c ChromedpImporter) checkQuizCollision(ctx context.Context, run chromedpRun, title string) error { //nolint:gocritic // ChromedpImporter is passed by value throughout this file
	var collision bool
	var err error
	if c.quizExists != nil {
		collision, err = c.quizExists(ctx, title)
	} else {
		err = run(ctx, chromedp.Evaluate(quizExistsJS(title), &collision))
	}
	if err != nil {
		return fmt.Errorf("check quiz title collision: %w", err)
	}
	if collision {
		return fmt.Errorf("quiz %q already exists", title)
	}
	return nil
}

func canvasURL(rawBase, courseID, path string) (string, error) {
	base, err := url.Parse(rawBase)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("invalid Canvas base URL %q", rawBase)
	}
	base.Path = "/courses/" + url.PathEscape(courseID) + path
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func hasQuizSuffix(name string) bool {
	fields := strings.Fields(name)
	return len(fields) > 0 && strings.EqualFold(fields[len(fields)-1], "Quiz")
}

// QuizTitle derives a New Quiz title from its final Item Bank title.
func QuizTitle(name string) string {
	name = strings.TrimSpace(name)
	if hasQuizSuffix(name) {
		return name
	}
	return name + " Quiz"
}

func bankIDFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "banks" {
			return parts[i+1]
		}
	}
	return ""
}

const (
	quizTitleSelector   = `input[name="name"], input[aria-label*="Assignment Name" i], input[placeholder*="Assignment Name" i]`
	randomCountSelector = `[role="dialog"] input[type="number"]`
	bankTitleJS         = `(() => { const h = document.querySelector('h1'); return h ? h.innerText.trim() : ''; })()`
	// Each question card's type label reads e.g. "Multiple Choice | Question
	// Question 18" (recorded live on the bank detail page; Canvas has no
	// data-testid or stable class on these cards). Counting matches of that
	// repeated "Question Question N" fragment in the page's rendered text is
	// more reliable than any single selector, since the question list is
	// virtualized and not every card selector matches every render.
	bankItemCountJS       = `(document.body.innerText.match(/Question Question \d+/g) || []).length`
	selectNewQuizEngineJS = `(() => { const nodes = Array.from(document.querySelectorAll('label,button,[role="radio"]')); const engine = nodes.find(el => el.innerText.trim().toLowerCase() === 'new quizzes'); if (!engine) return false; engine.click(); const submit = Array.from(document.querySelectorAll('button')).find(el => /^(submit|continue)$/i.test(el.innerText.trim())); if (submit) submit.click(); return true; })()`
	quizSetupReadyJS      = `Boolean(document.querySelector('input[name="name"], input[aria-label*="Assignment Name" i], input[placeholder*="Assignment Name" i]')) || document.body.innerText.toLowerCase().includes('new quizzes')`
	addFromBankReadyJS    = `document.body.innerText.toLowerCase().includes('add from item bank')`

	// Recorded live against UNT's Canvas instance (unt.instructure.com), which
	// can present either of two different login forms depending on how the
	// session got there: navigating straight to /login/canvas renders
	// Canvas's own React "new_login" UI (data-testid attributes, Canvas's own
	// test hooks); navigating to a protected course URL while unauthenticated
	// instead redirects through UNT's actual Shibboleth SSO gateway
	// (sso.unt.edu), a plain server-rendered form with no data-testid at all.
	// Both happen to use the same plain #username/#password ids for their
	// fields, so selecting on those ids (rather than data-testid, which only
	// one of the two forms has) works across both.
	usernameSelector    = `#username`
	passwordSelector    = `#password`
	loginButtonSelector = `form button[type="submit"]`

	loginPageDetectedJS = `Boolean(document.querySelector('#username'))`
	loginSucceededJS    = `!document.querySelector('#username')`
)

func bankTitleMatchesJS(title string) string {
	return `(() => { const h = document.querySelector('h1'); return Boolean(h && h.innerText.trim() === ` + jsString(title) + `); })()`
}

func bankItemCountMatchesJS(count int) string {
	return fmt.Sprintf("(%s) === %d", bankItemCountJS, count)
}

func clickTextInsensitive(text string) chromedp.Action {
	lower := strings.ToLower(text)
	translate := "translate(normalize-space(), 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz')"
	return chromedp.Click("//*[self::button or self::a or @role='button'][contains("+translate+", "+xpathString(lower)+")]", chromedp.BySearch)
}

// clickBank matches an Item Bank entry in the "Add from item bank" picker by
// its exact name text. Recorded live: entries there carry no data-bank-id or
// /banks/{id}-patterned href to match on, just plain link text.
func clickBank(bankName string) chromedp.Action {
	return chromedp.Click("//*[self::button or self::a or @role='button'][normalize-space()="+xpathString(strings.TrimSpace(bankName))+"]", chromedp.BySearch)
}

func quizExistsJS(title string) string {
	return `Array.from(document.querySelectorAll('a,button,[role="button"],h2,h3')).some(el => el.innerText.trim() === ` + jsString(title) + `)`
}

func randomGroupJS(bankName string, count int) string {
	return fmt.Sprintf(`(() => { const text = document.body.innerText.toLowerCase(); return text.includes(%q) && text.includes('random') && text.includes(%q); })()`, strings.ToLower(strings.TrimSpace(bankName)), fmt.Sprintf("%d", count))
}

func clickText(text string) chromedp.Action {
	return chromedp.Click("//*[self::button or self::a][contains(normalize-space(), "+xpathString(text)+")]", chromedp.BySearch)
}

func clickTextInDialog(text string) chromedp.Action {
	return chromedp.Click("//*[@role='dialog']//*[self::button or self::a][normalize-space()="+xpathString(text)+"]", chromedp.BySearch)
}

func bankExistsJS(name string) string {
	return `Array.from(document.querySelectorAll('button')).some(b => b.innerText.trim() === ` + name + `)`
}

func jsString(s string) string { return fmt.Sprintf("%q", s) }

func xpathString(s string) string {
	if !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	if !strings.Contains(s, `"`) {
		return `"` + s + `"`
	}
	parts := strings.Split(s, "'")
	quoted := make([]string, 0, len(parts)*2)
	for n, part := range parts {
		if part != "" {
			quoted = append(quoted, "'"+part+"'")
		}
		if n < len(parts)-1 {
			quoted = append(quoted, `"'"`)
		}
	}
	return "concat(" + strings.Join(quoted, ",") + ")"
}
