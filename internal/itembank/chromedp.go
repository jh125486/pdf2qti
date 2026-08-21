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
	// evalBool overrides evaluateBool's default chromedp.Evaluate(js, &ok)
	// path. Production leaves this nil; tests set it because the mocked
	// `run` field never populates out-parameters passed to chromedp actions,
	// so any check that reads a boolean result must go through this hook to
	// be observable under test (same reasoning as quizExists/quizLocation).
	evalBool func(context.Context, string) (bool, error)
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
	// 180s was too tight once persistence confirmation (re-navigate + two
	// polls, up to 3 attempts) was added after the in-page checks below.
	browser, workCancel := context.WithTimeout(browser, 240*time.Second)
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
		label string
		do    func(context.Context) error
	}{
		// The visible "+" is a separate icon, not part of the button's
		// textContent (recorded live: the actual text is "Quiz/Survey").
		{"open quiz creator", func(ctx context.Context) error {
			return runResilient(ctx, run, clickTextInsensitive("Quiz/Survey"))
		}},
		// Clicking navigates to the quiz builder; evaluating JS in the same
		// instant the execution context is torn down for that navigation
		// intermittently fails with a CDP "context not found" error. Let the
		// new document settle before polling its state (same race as the
		// Canvas login submit in ensureSession).
		{"let quiz builder navigation settle", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Sleep(2*time.Second))
		}},
		{"wait for quiz setup", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Poll(quizSetupReadyJS, nil, chromedp.WithPollingTimeout(10*time.Second)))
		}},
		// Not checked via evaluateBool: Canvas doesn't always prompt for an
		// engine choice (only some courses/quizzes hit this dialog), so a
		// false/no-op result here is expected and not an error.
		{"select New Quizzes if prompted", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Evaluate(selectNewQuizEngineJS, nil))
		}},
		{"wait for quiz title", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.WaitVisible(quizTitleSelector, chromedp.ByQuery))
		}},
		{"set quiz title", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.SetValue(quizTitleSelector, quizTitle, chromedp.ByQuery))
		}},
		{buildQuizStep, func(ctx context.Context) error {
			return runResilient(ctx, run, clickTextInsensitive("Build"))
		}},
		// Same navigation race as above: "Build" navigates to the quiz's
		// build page.
		{"let quiz build navigation settle", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Sleep(2*time.Second))
		}},
		{"wait for Item Bank action", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Poll(addFromBankReadyJS, nil, chromedp.WithPollingTimeout(30*time.Second)))
		}},
		{"open Item Bank picker", func(ctx context.Context) error {
			return runResilient(ctx, run, clickTextInsensitive("Add from item bank"))
		}},
		{"select Item Bank", func(ctx context.Context) error {
			return runResilient(ctx, run, clickBank(req.BankName))
		}},
		// Mirrors the Import-button pattern above: wait for the button to
		// actually become enabled before clicking it. A CDP click on a
		// disabled button reports success but adds nothing — this is one of
		// the root causes of quizzes being created with zero content.
		{"add Item Bank to quiz", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Tasks{
				chromedp.WaitEnabled(textXPathInsensitive("Add this bank to quiz"), chromedp.BySearch),
				chromedp.Click(textXPathInsensitive("Add this bank to quiz"), chromedp.BySearch),
			})
		}},
		// The bank group's edit control doesn't render until the panel
		// finishes settling after the bank is added.
		{"let bank group render", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Sleep(1*time.Second))
		}},
		{"wait for bank group to appear", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Poll(bankGroupPresentJS, nil, chromedp.WithPollingTimeout(30*time.Second)))
		}},
		{"edit Item Bank group", func(ctx context.Context) error {
			ok, err := c.evaluateBool(ctx, run, clickEditBankGroupJS)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf(`the per-group "Edit Bank containing questions..." control was not found`)
			}
			return nil
		}},
		{"let edit panel open", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Sleep(2*time.Second))
		}},
		{"enable random questions", func(ctx context.Context) error {
			ok, err := c.evaluateBool(ctx, run, clickRadioLabelJS("Randomly select questions"))
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf(`the "Randomly select questions" option was not found`)
			}
			return nil
		}},
		// The "Number of questions" field only exists after the radio above
		// switches the panel into random-selection mode.
		{"let panel update after enabling random", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Sleep(500*time.Millisecond))
		}},
		{"locate question count field", func(ctx context.Context) error {
			ok, err := c.evaluateBool(ctx, run, markQuestionCountInputJS())
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf(`the "Number of questions" field was not found`)
			}
			return nil
		}},
		{"set question count", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Tasks{
				chromedp.Click(questionCountSelector, chromedp.ByQuery),
				chromedp.SendKeys(questionCountSelector, fmt.Sprintf("%d", req.QuestionCount), chromedp.ByQuery),
			})
		}},
		{"save Item Bank group", func(ctx context.Context) error {
			return runResilient(ctx, run, clickTextInsensitive("Done"))
		}},
		{"verify random group", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.Poll(randomGroupJS(req.BankName, req.QuestionCount), nil, chromedp.WithPollingTimeout(10*time.Second)))
		}},
	}
	for _, step := range steps {
		if err := step.do(browser); err != nil {
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
	if strings.TrimSpace(quizURL) == "" {
		return QuizResult{}, fmt.Errorf("quiz URL is empty after creation; cannot confirm it was persisted")
	}
	// The in-page "verify random group" poll above can be satisfied by
	// Canvas's optimistic UI before its backend autosave has actually
	// persisted the Item Bank group — this is exactly what produced a quiz
	// that reported success, had a valid URL, and contained zero questions.
	// Re-navigate fresh (not Reload) and re-check from scratch before
	// reporting success.
	if err := c.confirmQuizPersisted(browser, run, quizURL, req.BankName, req.QuestionCount); err != nil {
		return QuizResult{}, err
	}
	return QuizResult{QuizURL: quizURL, Title: quizTitle, QuestionCount: req.QuestionCount}, nil
}

// confirmQuizPersisted re-navigates to the just-created quiz (a real
// Navigate, not Reload — a reload can be served from bfcache/client-side
// router state and so wouldn't disprove the optimistic-UI theory) and
// re-checks, from that fresh document, that the Item Bank random-selection
// group is actually present. It retries a few times since Canvas's autosave
// can lag slightly behind the UI action that triggered it.
func (c ChromedpImporter) confirmQuizPersisted(ctx context.Context, run chromedpRun, quizURL, bankName string, count int) error { //nolint:gocritic // ChromedpImporter is passed by value throughout this file
	var lastErr error
	for range 3 {
		if err := runResilient(ctx, run, chromedp.Tasks{
			chromedp.Sleep(2 * time.Second),
			chromedp.Navigate(quizURL),
			chromedp.WaitReady("body", chromedp.ByQuery),
			chromedp.Sleep(2 * time.Second),
		}); err != nil {
			lastErr = err
			continue
		}
		if err := runResilient(ctx, run, chromedp.Poll(bankGroupPresentJS, nil, chromedp.WithPollingTimeout(20*time.Second))); err != nil {
			lastErr = err
			continue
		}
		if err := runResilient(ctx, run, chromedp.Poll(randomGroupJS(bankName, count), nil, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("quiz was created but Canvas did not persist its Item Bank group (re-navigated to %s, no random group for %q with %d questions): %w", quizURL, bankName, count, lastErr)
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

// evaluateBool runs a JS boolean-returning expression and reports its
// result. Production evaluates js directly via chromedp; tests inject
// evalBool because the mocked `run` field never populates out-parameters
// passed to chromedp.Evaluate, so any check reading a boolean result out of
// the page must go through this hook to be observable under test.
func (c ChromedpImporter) evaluateBool(ctx context.Context, run chromedpRun, js string) (bool, error) { //nolint:gocritic // ChromedpImporter is passed by value throughout this file
	if c.evalBool != nil {
		return c.evalBool(ctx, js)
	}
	var ok bool
	err := runResilient(ctx, run, chromedp.Evaluate(js, &ok))
	return ok, err
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

// questionCountMarkerAttr flags the "Number of questions" input once located
// by markQuestionCountInputJS, so a subsequent chromedp.Click/SendKeys can
// select it by a stable attribute instead of Instructure UI's dynamically
// numbered ids (e.g. "NumberInput___1"), which aren't reproducible.
const questionCountMarkerAttr = "data-pdf2qti-question-count"

// questionCountSelector selects the input questionCountMarkerAttr flagged.
const questionCountSelector = `input[` + questionCountMarkerAttr + `]`

// markQuestionCountInputJS finds the "Number of questions" field by its
// label text, not a selector: recorded live, this panel is not a dialog (no
// role="dialog" wrapper) and has multiple plain input[type=text] fields
// (source bank picker, question count, points per question), all with
// Instructure-generated ids whose numeric suffix does not reflect visual
// order. The label is a <span>, not a <label> element. The field itself is
// the sole <input> inside the smallest ancestor that contains exactly one.
func markQuestionCountInputJS() string {
	return `(() => {
		const span = Array.from(document.querySelectorAll('span')).find(s => s.children.length === 0 && s.textContent.trim() === 'Number of questions');
		if (!span) return false;
		let cur = span.parentElement;
		for (let i = 0; i < 5 && cur; i++) {
			const inputs = cur.querySelectorAll('input');
			if (inputs.length === 1) {
				inputs[0].setAttribute('` + questionCountMarkerAttr + `', '1');
				return true;
			}
			cur = cur.parentElement;
		}
		return false;
	})()`
}

const (
	quizTitleSelector = `input[name="name"], input[aria-label*="Assignment Name" i], input[placeholder*="Assignment Name" i]`
	bankTitleJS       = `(() => { const h = document.querySelector('h1'); return h ? h.innerText.trim() : ''; })()`
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
	// bankGroupPresentJS checks for the same structural marker
	// clickEditBankGroupJS keys on — the per-group "Edit Bank containing
	// questions N through M." control that only renders once Canvas has
	// actually added the bank group to the quiz's page.
	bankGroupPresentJS = `Array.from(document.querySelectorAll('[role="button"], button')).some(e => e.textContent.trim().startsWith('Edit Bank containing questions'))`

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

// textXPathInsensitive builds the case-insensitive text-match XPath expression
// shared by clickTextInsensitive and the "add Item Bank to quiz" step, which
// also needs to WaitEnabled on the identical node before clicking it.
func textXPathInsensitive(text string) string {
	lower := strings.ToLower(text)
	translate := "translate(normalize-space(), 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz')"
	return "//*[self::button or self::a or @role='button'][contains(" + translate + ", " + xpathString(lower) + ")]"
}

func clickTextInsensitive(text string) chromedp.Action {
	return chromedp.Click(textXPathInsensitive(text), chromedp.BySearch)
}

// clickBank matches an Item Bank entry in the "Add from item bank" picker by
// its exact name text. Recorded live: entries there carry no data-bank-id or
// /banks/{id}-patterned href to match on, just plain link text.
func clickBank(bankName string) chromedp.Action {
	return chromedp.Click("//*[self::button or self::a or @role='button'][normalize-space()="+xpathString(strings.TrimSpace(bankName))+"]", chromedp.BySearch)
}

// clickEditBankGroupJS matches the per-group "Edit Bank containing questions
// N through M." control that appears on a bank group after it's added to a
// quiz. Recorded live: the "All / Random" button visible at the top of the
// panel is a separate *add more content* action (adds another bank/group),
// not this group's edit toggle — clicking it does not reach the "Randomly
// select questions" configuration this needs. It dispatches a JS .click()
// rather than chromedp.Click's real mouse-event dispatch: recorded live, a
// hit-test-based click at this element's computed center silently misses it
// (no panel opens, no error), while a direct .click() call reliably reaches
// its handler. Returns its own boolean success signal (found-and-clicked or
// not) so callers can go through evaluateBool and error out instead of
// silently no-oping on a selector miss.
const clickEditBankGroupJS = `(() => {
	const target = Array.from(document.querySelectorAll('[role="button"], button')).find(e => e.textContent.trim().startsWith('Edit Bank containing questions'));
	if (target) { target.click(); return true; }
	return false;
})()`

// clickRadioLabelJS clicks the <label> whose text matches (case-insensitive),
// via JS .click() rather than clickTextInsensitive's XPath. Instructure's
// radio buttons render their clickable text inside a <label>, not a
// button/a/[role=button] — the tag set clickTextInsensitive's XPath matches
// — so it never finds these controls at all. Returns its own boolean success
// signal so callers can go through evaluateBool and error out instead of
// silently no-oping on a selector miss.
func clickRadioLabelJS(text string) string {
	lower := strings.ToLower(text)
	return `(() => {
		const target = Array.from(document.querySelectorAll('label')).find(l => l.textContent.trim().toLowerCase().includes(` + jsString(lower) + `));
		if (target) { target.click(); return true; }
		return false;
	})()`
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
