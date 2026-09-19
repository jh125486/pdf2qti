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

const (
	buildQuizStep          = "build quiz"
	attachPackageStep      = "attach package"
	submitImportStep       = "submit import"
	waitImportCompleteStep = "wait for import completion"
)

// importButtonXPath matches the import dialog's "Import" submit button. It is
// shared between the WaitEnabled check and the click that follows it (via
// clickXPathJS) so both resolve to the exact same element — see the doc
// comment at its use site in Import().
const importButtonXPath = `//*[@role='dialog']//button[contains(normalize-space(), 'Import')]`

// buttonOrLinkTags is the shared button/link tag set clickTextJS matches
// against at every plain "click the element with this text" site in this
// file (the top-level "Create Bank" button, a bank row in the Banks list,
// and the rename dialog's "Edit bank .../Save Changes" controls) — pulled
// out as a shared var instead of repeating the "button" string literal at
// each call site (goconst).
var buttonOrLinkTags = []string{"button", "a"}

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

// isTimeoutLike reports whether err is a plain timeout — chromedp's "context
// deadline exceeded" (an action outliving Import()'s outer workCancel
// budget) or "waiting for function failed: timeout" (a chromedp.Poll
// outliving its own WithPollingTimeout) — as opposed to a selector miss or
// other logic error. Both are symptoms of Canvas's UI/autosave lagging
// behind a backend action that actually already succeeded, the same race
// confirmQuizPersisted already guards against for the random-quiz-group
// path; unlike runResilient's CDP-execution-context-torn-down check, these
// never indicate the action should simply be retried unchanged; they mean
// Canvas should be re-checked from a fresh document instead.
func isTimeoutLike(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// "waiting for function failed" alone also prefixes chromedp.Poll errors
	// from a JavaScript exception or other evaluation failure inside the
	// predicate, not just a timeout — match the concrete timeout suffix so
	// those aren't mistaken for the same recoverable race.
	return strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "waiting for function failed: timeout")
}

// importTimeout scales Import()'s outer browser-context budget with the
// package's expected item count. Packages beyond 20 questions get extra
// headroom proportional to size so a large import doesn't hit the outer
// deadline before Canvas's own UI/autosave catches up.
//
// The 300s base accounts for the worst-case sum of every poll this file runs
// against the workCancel-bound browser context (not sessionCtx-bound retries
// like pollBankItemCount, which have their own independent budget — see its
// doc comment): the "Create Bank" button poll (up to 15s) + the post-create
// bank-visibility confirm poll (up to 60s — live-confirmed repeatedly
// insufficient at both 15s and 30s, see that poll's own doc comment) + the
// always-present import-success poll (60s) + the ExpectedBankName
// title-match poll (up to 30s) + renameBankUI's own edit-dialog-open poll
// (up to 15s) and post-rename reopen poll (up to 30s), which also run
// against this same budget when Canvas renamed the bank after its own
// package title and a rename-back is needed. That worst case (a brand-new
// bank, with ExpectedBankName set, that also needs renaming) sums to
// roughly 210s of pure poll time, plus ensureSession's own up-to-~23s
// cold-login cost (2 settle Sleeps + a 20s loginSucceededJS poll, all
// against this same budget) and ~3s of the file's various other settle
// Sleep()s between steps — call it ~235-255s. 300s leaves real headroom
// (~45-65s) above that worst case.
func importTimeout(expectedItemCount int) time.Duration {
	const base = 300 * time.Second
	if expectedItemCount <= 20 {
		return base
	}
	return base + time.Duration(expectedItemCount-20)*time.Second
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

// Import drives Canvas's browser UI to create or append to req's Item Bank
// and upload req.Package into it, recovering from a UI/CDP timeout by
// re-checking Canvas for actual content before failing (see
// docs/item-bank-import-flake.md) rather than always erroring out.
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
	// sessionCtx retains the browser tab's own context, with no deadline
	// beyond the tab's lifetime, so timeout-recovery helpers below can derive
	// their own fresh bounded context instead of reusing browser's — which,
	// when the failure being recovered from is workCancel's own deadline
	// expiring, is already a dead context that can't navigate or evaluate
	// anything.
	sessionCtx := browser
	browser, workCancel := context.WithTimeout(browser, importTimeout(req.ExpectedItemCount))
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
		// Live-confirmed (headless E2E against a real course): the Banks
		// page's React content, including the top-level "Create Bank"
		// button, can still be hydrating after WaitReady("body") and the 1s
		// settle above — especially on a cold profile that just logged in
		// via ensureSession, with no prior render on this page to warm up.
		// A one-shot click here previously raced that render and missed
		// with no retry, which is exactly the "dialog never opens" headless
		// flake docs/item-bank-import-flake.md couldn't explain. Poll for
		// up to 15s instead of clicking once.
		opened, err := c.evaluatePollBool(browser, run, clickTextJS(buttonOrLinkTags, "Create Bank", false), 15*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("open create bank dialog: %w", err)
		}
		if !opened {
			return Result{}, fmt.Errorf(`open create bank dialog: "Create Bank" button not found`)
		}
		if err := run(browser, chromedp.WaitVisible("[role=dialog] input", chromedp.ByQuery)); err != nil {
			return Result{}, fmt.Errorf("wait for bank-name field: %w", err)
		}
		if err := run(browser, chromedp.SendKeys("[role=dialog] input", req.BankName, chromedp.ByQuery)); err != nil {
			return Result{}, fmt.Errorf("fill bank name: %w", err)
		}
		// The wait above resolves to "[role=dialog] input" (the bank-NAME text
		// field, first in document order), not this checkbox — wait
		// specifically for the checkbox itself before clicking it so the click
		// target always matches an element this step actually confirmed is
		// visible first.
		if err := run(browser, chromedp.WaitVisible(shareWithCourseCheckboxSelector, chromedp.ByQuery)); err != nil {
			return Result{}, fmt.Errorf("wait for share-with-course checkbox: %w", err)
		}
		// Live-confirmed root cause (docs/item-bank-import-flake.md): a
		// hit-test chromedp.Click here silently fails to flip this InstUI
		// checkbox's checked state (no chromedp error, box never checks). The
		// bank is still created (201) but unshared/private to the course, so
		// it never appears in this course's bank list — every downstream step
		// (findBank, opening the bank, the import dialog) then searches for a
		// bank invisible to it and hangs. Dispatch a direct JS .click() instead
		// (same fix as clickEditBankGroupJS). checkCheckboxJS (unlike
		// clickJS/clickSelectorJS) only clicks when the box isn't already
		// checked and returns its final .checked value, not just whether an
		// element was found — a plain JS .click() toggles state, so reusing
		// clickSelectorJS here could silently *uncheck* a box Canvas already
		// defaulted to checked while still reporting a clean "found" success.
		// Check that returned .checked value via evaluateBool instead of
		// discarding it — a miss here is exactly the failure class this whole
		// fix exists to catch, so it must not go silent again at this call
		// site.
		checked, err := c.evaluateBool(browser, run, checkCheckboxJS(shareWithCourseCheckboxSelector))
		if err != nil {
			return Result{}, fmt.Errorf("share bank with course: %w", err)
		}
		if !checked {
			return Result{}, fmt.Errorf(`share bank with course: "Share with course" checkbox not found or not checked`)
		}
		if err := run(browser, chromedp.WaitVisible(createBankSubmitSelector, chromedp.ByQuery)); err != nil {
			return Result{}, fmt.Errorf("wait for create bank submit button: %w", err)
		}
		submitted, err := c.evaluateBool(browser, run, clickSelectorJS(createBankSubmitSelector))
		if err != nil {
			return Result{}, fmt.Errorf("submit create bank: %w", err)
		}
		if !submitted {
			return Result{}, fmt.Errorf("submit create bank: submit button not found")
		}
		if err := run(browser, chromedp.Sleep(500*time.Millisecond)); err != nil {
			return Result{}, fmt.Errorf("submit create bank: %w", err)
		}
		if err := run(browser, chromedp.Navigate(base.String()), chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
			return Result{}, fmt.Errorf("return to Item Banks: %w", err)
		}
		// Verify the bank actually exists in the course's bank list before
		// proceeding — the class of bug above (a click that plain didn't
		// happen) would otherwise silently carry on into the 240s+
		// timeout/retry cascade the rest of Import() uses for a different,
		// unrelated race (UI/autosave lag). Fail fast here instead with a
		// clear, specific error. Polled (not a one-shot evaluate): body-ready
		// doesn't mean the React bank list has actually finished rendering
		// yet, so a bare one-shot check can report "not found" on a run that
		// actually succeeded. Live-confirmed repeatedly: 15s, then 30s, both
		// proved occasionally too tight for this same course-Banks-list read
		// — reproduced a fourth time at 30s with the machine otherwise
		// healthy (722MB free, no resource contention), and with
		// evaluatePollBoolViaRun's ctx.Err() disambiguation (see its own doc
		// comment) confirming this was a genuine "polled and never found it"
		// result, not this function's own outer budget expiring — so the
		// render latency itself, not a resource-contention artifact, is the
		// real cause. Likely compounded by this sandbox course accumulating
		// many stray test banks over repeated runs (see
		// docs/item-bank-import-flake.md's "Stray test banks" note) — a
		// longer list plausibly renders more slowly. Bumped to 60s; the
		// underlying render-latency variance itself is not fully understood
		// and deserves verification against a fresh/different course (see
		// that same doc's customer-reliability follow-up).
		confirmed, confirmErr := c.evaluatePollBool(browser, run, bankExistsJS(name), 60*time.Second)
		if confirmErr != nil {
			return Result{}, fmt.Errorf("verify created Item Bank is visible: %w", confirmErr)
		}
		if !confirmed {
			// Deliberately not pinned to a single cause: any of the create-bank
			// clicks above (dialog open, checkbox, submit) could have silently
			// missed, or Canvas's own render could still be lagging despite the
			// poll. The "Create Bank" step already committed server-side by
			// this point, so a stray, unshared Item Bank may now exist in
			// Canvas and need manual cleanup (same concern noted near
			// recoverStuckImport's doc comment for the timeout-recovery path).
			return Result{}, fmt.Errorf("created Item Bank %q was not found in the course bank list afterward (the bank may not have been created, or a click earlier in the create-bank sequence may have silently missed; a stray, unshared Item Bank may now exist in Canvas and need manual cleanup)", req.BankName)
		}
	}
	// Exact match, not substring: bankExistsJS (the existence check and
	// post-create verify just above) already matches req.BankName exactly, so
	// this open-bank click must too. A substring match here previously let a
	// course containing both "Chapter 1" and "Chapter 11" silently open
	// whichever one happened to render first in the DOM when targeting
	// "Chapter 1" — the exact checks above correctly confirm "Chapter 1"
	// exists, but a substring click could then import into "Chapter 11"
	// instead, reporting success while silently writing to the wrong bank.
	opened, err := c.evaluateBool(browser, run, clickTextJS(buttonOrLinkTags, req.BankName, true))
	if err != nil {
		return Result{}, fmt.Errorf("open Item Bank %q: %w", req.BankName, err)
	}
	if !opened {
		return Result{}, fmt.Errorf("open Item Bank %q: bank not found in list", req.BankName)
	}
	if err := run(browser, chromedp.WaitVisible(`button[data-popover-trigger="true"]`, chromedp.ByQuery)); err != nil {
		return Result{}, fmt.Errorf("wait for Item Bank actions: %w", err)
	}
	// Recorded before the import dialog opens so a later timeout-recovery
	// check (recoverStuckImport) can require the bank's item count to have
	// actually grown, not just be non-zero — an ExistingAppend import onto a
	// bank that already had content would otherwise satisfy a bare "count >
	// 0" check even when this run's own upload never completed. A bank this
	// call just created (found == false) is definitionally empty, no read
	// needed. For an existing bank the read is best-effort: -1 means unknown,
	// and callers below must treat that as "can't confirm", not silently
	// fall back to 0 — a false 0 baseline reintroduces exactly the bug this
	// baseline exists to prevent.
	baselineItemCount := 0
	if found {
		baselineItemCount = -1
		if n, ok := c.stableBankItemCount(browser, run); ok {
			baselineItemCount = n
		}
	}
	// An unknown baseline on an existing bank can't be told apart from 0
	// when computing the append target below (baseline + package count), so
	// a false 0 would let a bad expected-total check pass or fail
	// incorrectly. Abort before mutating anything rather than uploading a
	// package this run can't verify.
	if found && baselineItemCount < 0 && req.ExpectedItemCount > 0 {
		return Result{}, fmt.Errorf("could not read existing Item Bank %q's baseline question count; refusing to import without a way to verify the result", req.BankName)
	}

	triggerClicked, err := c.evaluateBool(browser, run, clickSelectorJS(`button[data-popover-trigger="true"]`))
	if err != nil {
		return Result{}, fmt.Errorf("open import actions: %w", err)
	}
	if !triggerClicked {
		return Result{}, fmt.Errorf("open import actions: popover trigger button not found")
	}
	// Nothing settled between opening the popover and clicking its menu item
	// before — the menu is InstUI-animated in, so wait for it to actually
	// render before trying to click inside it.
	if err := run(browser, chromedp.WaitVisible(`[role="menuitem"]`, chromedp.ByQuery)); err != nil {
		return Result{}, fmt.Errorf("wait for import menu: %w", err)
	}
	menuItemClicked, err := c.evaluateBool(browser, run, clickSelectorJS(`[role="menuitem"]`))
	if err != nil {
		return Result{}, fmt.Errorf("open import dialog: %w", err)
	}
	if !menuItemClicked {
		return Result{}, fmt.Errorf("open import dialog: menu item not found")
	}
	var stage string
	var stageErr error
	if err := run(browser, chromedp.SetUploadFiles("input[type=file]", []string{req.Package}, chromedp.ByQuery)); err != nil {
		stage, stageErr = attachPackageStep, err
	}
	if stageErr == nil {
		// WaitEnabled still uses a real hit-test-free chromedp check (it only
		// polls a DOM property, never clicks); only the click itself switches
		// to JS dispatch, for the same InstUI hit-test-miss reason as the
		// create-bank checkbox above. The click targets the exact same node
		// the WaitEnabled XPath waited on (via clickXPathJS), rather than a
		// separately-derived CSS+text match — a dialog with more than one
		// Import-containing button (e.g. one that later reads "Importing…")
		// could otherwise let the wait and the click resolve to different
		// elements.
		if err := run(browser, chromedp.WaitEnabled(importButtonXPath, chromedp.BySearch)); err != nil {
			stage, stageErr = submitImportStep, err
		}
		if stageErr == nil {
			clicked, err := c.evaluateBool(browser, run, clickXPathJS(importButtonXPath))
			if err != nil {
				stage, stageErr = submitImportStep, err
			} else if !clicked {
				stage, stageErr = submitImportStep, fmt.Errorf("import button not found")
			}
		}
	}
	if stageErr == nil {
		if err := run(browser, chromedp.Poll(`document.body.innerText.includes('has imported successfully!')`, nil, chromedp.WithPollingTimeout(60*time.Second))); err != nil {
			stage, stageErr = waitImportCompleteStep, err
		}
	}
	if stageErr != nil {
		// A plain timeout here (as opposed to a selector/logic error) can mean
		// the upload/import already completed server-side and Canvas's own UI
		// just hasn't caught up — the same race confirmQuizPersisted already
		// guards against on the random-quiz-group path, just upstream of it on
		// this plain bank-import path. Re-navigate fresh and check for actual
		// content before giving up: recovering here also avoids leaving behind
		// the stray, empty Item Bank a bare error would (the "Create Bank" step
		// above already committed server-side by this point).
		if !isTimeoutLike(stageErr) || !c.recoverStuckImport(sessionCtx, run, base.String(), req.BankName, req.ExpectedBankName, baselineItemCount) {
			return Result{}, fmt.Errorf("%s: %w", stage, stageErr)
		}
		// The recovered failure can be workCancel's own deadline expiring, in
		// which case browser is now a dead context — every remaining step
		// below must run against a fresh bounded child of sessionCtx instead.
		workCancel()
		browser, workCancel = context.WithTimeout(sessionCtx, importTimeout(req.ExpectedItemCount))
		defer workCancel()
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
				renameErr = renameBankUI(browser, run, c.evalBool, base.String(), bankTitle, req.BankName)
			}
			if renameErr != nil {
				return Result{}, fmt.Errorf("rename Item Bank %q to %q: %w", bankTitle, req.BankName, renameErr)
			}
			bankTitle = req.BankName
		}
	}
	itemCount := 0
	if req.ExpectedItemCount > 0 {
		// req.ExpectedItemCount is the package's own item count. For a fresh
		// bank (baselineItemCount == 0) that's also the bank's expected final
		// total, but ExistingAppend imports onto a bank that already had
		// content — the bank's rendered total after import is the pre-import
		// baseline plus the package count, not the package count alone. A
		// negative (unknown) baseline can't reach here: the guard above
		// already aborted before upload rather than let this compute the
		// wrong target.
		expectedTotal := req.ExpectedItemCount
		if baselineItemCount > 0 {
			expectedTotal += baselineItemCount
		}
		var countErr error
		if c.bankItemCount != nil {
			itemCount, countErr = c.bankItemCount(browser)
		} else {
			itemCount, countErr = c.pollBankItemCount(sessionCtx, run, base.String(), req.BankName, req.ExpectedBankName, expectedTotal)
		}
		if countErr != nil {
			return Result{}, fmt.Errorf("read imported Item Bank question count: %w", countErr)
		}
		if itemCount != expectedTotal {
			return Result{}, fmt.Errorf("imported Item Bank question count = %d, want %d", itemCount, expectedTotal)
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
	//
	// Re-checked against this diff's inner poll budgets (all run against this
	// same budget-bound browser context, ensureSession included — it costs up
	// to ~23s on a cold login: 2 settle Sleeps + a 20s loginSucceededJS poll):
	// ensureSession (~23s) + "wait for quiz setup" (10s) + "wait for Item Bank
	// action" (30s) + "select Item Bank"'s poll (15s) + "wait for bank group
	// to appear" (30s) + "verify random group" (10s) = ~118s of steady-state
	// poll time, plus confirmQuizPersisted's worst case of 3 retry attempts at
	// two ~2s settles + a 20s bankGroupPresent poll + a 10s randomGroup poll
	// each (~34s/attempt, ~102s for 3), plus this function's own handful of
	// 1-2s settle Sleep()s between steps (~7s) — roughly 118+102+7 ~= 227s
	// worst case. 300s leaves real headroom (~70s) above that; the previous
	// 240s left only ~13s once ensureSession and the corrected
	// confirmQuizPersisted math are actually counted.
	browser, workCancel := context.WithTimeout(browser, 300*time.Second)
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
		// textContent (recorded live: the actual text is "Quiz/Survey"). Routes
		// through evaluateBool (rather than a bare chromedp.Evaluate(..., nil))
		// so a click that silently misses errors out here instead of running
		// on into steps that assume the quiz builder actually opened.
		{"open quiz creator", func(ctx context.Context) error {
			ok, err := c.evaluateBool(ctx, run, clickTextInsensitiveJS("Quiz/Survey"))
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf(`"Quiz/Survey" control not found`)
			}
			return nil
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
		// selectNewQuizEngineJS reports one of three outcomes rather than a
		// plain boolean: Canvas doesn't always prompt for an engine choice
		// (only some courses/quizzes hit this dialog), so "no_dialog" is
		// expected and not an error — but "radio clicked, no submit button
		// found" (clicked_no_submit) leaves the dialog in a half-interacted
		// state and must be a clear error here, not silently tolerated the
		// same way "no dialog" is (see interpretEngineChoiceResult's doc
		// comment for why: the failure mode this whole diff exists to
		// eliminate elsewhere — a silently missed click letting execution
		// fall into an unbounded downstream wait — was still possible here
		// otherwise).
		{"select New Quizzes if prompted", func(ctx context.Context) error {
			var outcome string
			if err := runResilient(ctx, run, chromedp.Evaluate(selectNewQuizEngineJS, &outcome)); err != nil {
				return err
			}
			return interpretEngineChoiceResult(outcome)
		}},
		{"wait for quiz title", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.WaitVisible(quizTitleSelector, chromedp.ByQuery))
		}},
		{"set quiz title", func(ctx context.Context) error {
			return runResilient(ctx, run, chromedp.SetValue(quizTitleSelector, quizTitle, chromedp.ByQuery))
		}},
		{buildQuizStep, func(ctx context.Context) error {
			ok, err := c.evaluateBool(ctx, run, clickTextInsensitiveJS("Build"))
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf(`"Build" control not found`)
			}
			return nil
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
			ok, err := c.evaluateBool(ctx, run, clickTextInsensitiveJS("Add from item bank"))
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf(`"Add from item bank" control not found`)
			}
			return nil
		}},
		{"select Item Bank", func(ctx context.Context) error {
			// Wait for the picker dialog itself to render before searching it
			// for the bank entry — nothing previously waited between opening
			// the picker and clicking inside it.
			if err := runResilient(ctx, run, chromedp.WaitVisible(`[role="dialog"]`, chromedp.ByQuery)); err != nil {
				return err
			}
			// Live-confirmed: the dialog shell can render before its bank
			// list has finished loading — especially for a bank created
			// moments earlier in this same run, which the picker's own list
			// fetch may not have caught up to yet. A one-shot click here
			// previously raced that load; poll for up to 15s instead (same
			// class of render race as Import()'s "Create Bank" button).
			ok, err := c.evaluatePollBool(ctx, run, clickTextJS(append(append([]string{}, buttonOrLinkTags...), `[role="button"]`), strings.TrimSpace(req.BankName), true), 15*time.Second)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("item bank %q not found in picker", req.BankName)
			}
			return nil
		}},
		// Mirrors the Import-button pattern above: wait for the button to
		// actually become enabled before clicking it, and click the exact same
		// node the wait matched (via clickXPathJS on textXPathInsensitive's
		// XPath) rather than a separately-derived JS matcher, for the same
		// reason as the Import-button fix in Import(). A CDP click on a
		// disabled button reports success but adds nothing — this is one of
		// the root causes of quizzes being created with zero content.
		{"add Item Bank to quiz", func(ctx context.Context) error {
			// WaitEnabled still uses a real hit-test-free chromedp check (it
			// only polls a DOM property, never clicks); only the click itself
			// switches to JS dispatch, for the same InstUI hit-test-miss
			// reason as the create-bank checkbox in Import().
			addBankXPath := textXPathInsensitive("Add this bank to quiz")
			if err := runResilient(ctx, run, chromedp.WaitEnabled(addBankXPath, chromedp.BySearch)); err != nil {
				return err
			}
			ok, err := c.evaluateBool(ctx, run, clickXPathJS(addBankXPath))
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf(`"Add this bank to quiz" button not found`)
			}
			return nil
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
				// Deliberately left as chromedp.Click, not JS dispatch: the
				// following SendKeys needs real keyboard focus on this InstUI
				// NumberInput, which a JS .click() doesn't reliably give it.
				// Do not "fix" this into clickJS/evaluateBool without checking
				// that focus still works for the SendKeys below.
				chromedp.Click(questionCountSelector, chromedp.ByQuery),
				chromedp.SendKeys(questionCountSelector, fmt.Sprintf("%d", req.QuestionCount), chromedp.ByQuery),
			})
		}},
		{"save Item Bank group", func(ctx context.Context) error {
			ok, err := c.evaluateBool(ctx, run, clickTextInsensitiveJS("Done"))
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf(`"Done" control not found`)
			}
			return nil
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

// clickBankRowJS opens the Item Banks list entry named primary — or, if not
// found, the entry named fallback. Canvas renames an imported New Quizzes
// Item Bank after the QTI package's own assessment title until the caller's
// rename-back step (later in Import()) runs, so a fresh re-navigate during
// that window can find the bank listed under that title instead of
// req.BankName. Uses a direct JS .click() (like clickEditBankGroupJS above)
// rather than chromedp.Click's real mouse-event dispatch, so a miss on both
// names returns false immediately instead of blocking on a selector that
// will never appear.
func clickBankRowJS(primary, fallback string) string {
	find := func(name string) string {
		return `Array.from(document.querySelectorAll('button')).find(b => b.innerText.trim() === ` + jsString(name) + `)`
	}
	return `(() => {
		let target = ` + find(primary) + `;
		if (!target && ` + jsString(fallback) + `) { target = ` + find(fallback) + `; }
		if (target) { target.click(); return true; }
		return false;
	})()`
}

// reopenBankTasks re-navigates fresh (not Reload — a reload can be served
// from bfcache/client-side router state and so wouldn't disprove the
// optimistic-UI theory) to banksURL and opens the bank listed as either
// bankName or expectedBankName, waiting for its actions button to confirm
// the detail page — not just the list page — actually finished rendering
// before a caller polls anything on it.
func reopenBankTasks(banksURL, bankName, expectedBankName string) chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.Sleep(2 * time.Second),
		chromedp.Navigate(banksURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(time.Second),
		chromedp.Evaluate(clickBankRowJS(bankName, expectedBankName), nil),
		chromedp.Sleep(2 * time.Second),
		chromedp.WaitVisible(`button[aria-haspopup="true"]`, chromedp.ByQuery),
	}
}

// recoverStuckImport re-navigates fresh to the Item Banks list and reopens
// the bank, then checks whether its item count actually grew past
// baselineItemCount (the count read just before the import dialog was
// opened) — used after a timeout on the attach/submit/wait-completion
// sequence above to tell "Canvas actually finished the import, the UI/CDP
// hiccup was spurious" apart from "the import genuinely didn't happen".
// Requiring growth past the baseline (not just count > 0) matters for
// ExistingAppend: a bank that already had content before this run's own
// upload would otherwise satisfy a bare non-zero check even when the
// upload never completed. baselineItemCount == -1 means the pre-upload read
// failed — treated as non-recoverable rather than falling back to 0, since
// a false 0 baseline reintroduces exactly the false-positive this baseline
// exists to prevent. A positive result leaves the browser positioned on the
// bank's page, exactly where the rest of Import() expects to be after a
// normal completion.
func (c ChromedpImporter) recoverStuckImport(sessionCtx context.Context, run chromedpRun, banksURL, bankName, expectedBankName string, baselineItemCount int) bool { //nolint:gocritic // ChromedpImporter is passed by value throughout this file
	if baselineItemCount < 0 {
		return false
	}
	// sessionCtx is the browser tab's own context, not the deadline-bounded
	// one the failed step above used — that deadline expiring is exactly one
	// of the timeout-shaped errors this recovery runs for, so reusing it
	// would try to navigate/evaluate on an already-dead context.
	ctx, cancel := context.WithTimeout(sessionCtx, 60*time.Second)
	defer cancel()
	if err := runResilient(ctx, run, reopenBankTasks(banksURL, bankName, expectedBankName)); err != nil {
		return false
	}
	count, ok := c.stableBankItemCount(ctx, run)
	return ok && count > baselineItemCount
}

// stableBankItemCount reads the bank's rendered item count twice, a short
// settle apart, and only reports it when both reads agree. The count comes
// from document.body.innerText over a virtualized card list that can still
// be mid-render right after a page load — a single transient read (0, or
// partial) would otherwise corrupt a baseline used to compute an
// ExistingAppend import's expected final total, or let recoverStuckImport
// compare against a stale pre-render snapshot.
func (c ChromedpImporter) stableBankItemCount(ctx context.Context, run chromedpRun) (int, bool) { //nolint:gocritic // ChromedpImporter is passed by value throughout this file
	read := func() (int, bool) {
		if c.bankItemCount != nil {
			n, err := c.bankItemCount(ctx)
			return n, err == nil
		}
		var n int
		err := run(ctx, chromedp.Evaluate(bankItemCountJS, &n))
		return n, err == nil
	}
	first, ok := read()
	if !ok {
		return 0, false
	}
	if err := run(ctx, chromedp.Sleep(time.Second)); err != nil {
		return 0, false
	}
	second, ok := read()
	if !ok || second != first {
		return 0, false
	}
	return first, true
}

// pollBankItemCount waits for the imported bank's rendered question count to
// match expected, retrying by re-navigating fresh to the bank's own page
// when the in-page poll times out. bankItemCountMatchesJS's 30s window
// observed too tight for larger banks (60+ questions); this mirrors
// confirmQuizPersisted's re-navigate-and-recheck pattern for the same class
// of "backend already succeeded, UI/count hasn't caught up yet" race, just
// further upstream on the plain bank-import path (no --create-random-quiz).
// It only confirms the count matches — callers still read the actual value
// via bankItemCountJS afterward. Only a timeout-shaped error triggers a
// retry; a selector/logic error, closed-target error, or cancellation
// returns immediately instead of burning the retry budget on an error a
// re-navigate can't fix.
// pollBankItemCount takes sessionCtx — the browser tab's own context, not the
// deadline-bounded one Import()'s other steps use — because a timeout-shaped
// error triggering a retry here can be that outer deadline itself expiring,
// in which case reusing it would try to navigate/evaluate on an already-dead
// context. Each attempt gets its own fresh bounded child instead.
func (c ChromedpImporter) pollBankItemCount(sessionCtx context.Context, run chromedpRun, banksURL, bankName, expectedBankName string, expected int) (int, error) { //nolint:gocritic // ChromedpImporter is passed by value throughout this file
	var lastErr error
	for attempt := range 3 {
		ctx, cancel := context.WithTimeout(sessionCtx, 60*time.Second)
		if attempt > 0 {
			if err := runResilient(ctx, run, reopenBankTasks(banksURL, bankName, expectedBankName)); err != nil {
				cancel()
				if !isTimeoutLike(err) {
					return 0, err
				}
				lastErr = err
				continue
			}
		}
		if err := runResilient(ctx, run, chromedp.Poll(bankItemCountMatchesJS(expected), nil, chromedp.WithPollingTimeout(30*time.Second))); err != nil {
			cancel()
			if !isTimeoutLike(err) {
				return 0, err
			}
			lastErr = err
			continue
		}
		var itemCount int
		err := run(ctx, chromedp.Evaluate(bankItemCountJS, &itemCount))
		cancel()
		if err != nil {
			return 0, err
		}
		return itemCount, nil
	}
	return 0, lastErr
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
// the bank detail page they were on before the rename. evalBool, when
// non-nil, overrides both the click-checking and poll-checking behavior the
// same way ChromedpImporter.evalBool/evaluatePollBool do for
// evaluateBool/evaluatePollBool — renameBankUI is a free function (not a
// ChromedpImporter method) so it takes the hook as a parameter instead: the
// mocked run field tests use never populates chromedp.Evaluate's
// out-parameter, so a test needs some way to report a click's found/clicked
// (or a poll's found) signal as true.
func renameBankUI(ctx context.Context, run chromedpRun, evalBool func(context.Context, string) (bool, error), banksURL, oldTitle, newTitle string) error {
	clickCheck := func(ctx context.Context, js string) (bool, error) { return evaluateBoolViaRun(ctx, run, js) }
	pollCheck := func(ctx context.Context, js string, timeout time.Duration) (bool, error) {
		return evaluatePollBoolViaRun(ctx, run, js, timeout)
	}
	if evalBool != nil {
		clickCheck = evalBool
		pollCheck = func(ctx context.Context, js string, _ time.Duration) (bool, error) { return evalBool(ctx, js) }
	}
	if err := run(ctx, chromedp.Navigate(banksURL), chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
		return fmt.Errorf("open Item Banks: %w", err)
	}
	// The accessible name ("Edit bank {title}") comes from a visually-hidden
	// text node inside the button, not a literal aria-label attribute, so
	// this matches on text content like the rest of this file's helpers do.
	// Live-confirmed (headless E2E): there is no settle between the navigate
	// above and this click, and the Banks list's React content can still be
	// hydrating at this point (same render race as Import()'s "Create Bank"
	// button click) — poll instead of a one-shot click.
	//
	// Exact match, not substring: a substring match on "Edit bank "+oldTitle
	// would let a course containing both "Edit bank Chapter 1" and "Edit bank
	// Chapter 11" controls silently click the wrong one when renaming
	// "Chapter 1" — the same bank-name mismatch bug fixed at Import()'s
	// open-bank click site (see its doc comment).
	opened, err := pollCheck(ctx, clickTextJS(buttonOrLinkTags, "Edit bank "+oldTitle, true), 15*time.Second)
	if err != nil {
		return fmt.Errorf("open edit bank dialog: %w", err)
	}
	if !opened {
		return fmt.Errorf("open edit bank dialog: %q not found", "Edit bank "+oldTitle)
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
		// Deliberately left as chromedp.Click, not JS dispatch: the following
		// SendKeys needs real keyboard focus on this React-controlled input,
		// which a JS .click() doesn't reliably give it. Do not "fix" this into
		// clickJS/evaluateBool without checking that focus still works.
		chromedp.Click("[role=dialog] input", chromedp.ByQuery),
		chromedp.SendKeys("[role=dialog] input", kb.End+clearOldTitle+newTitle, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("set bank name: %w", err)
	}
	saved, err := clickCheck(ctx, clickTextJS([]string{`[role="dialog"] button`, `[role="dialog"] a`}, "Save Changes", true))
	if err != nil {
		return fmt.Errorf("save bank name: %w", err)
	}
	if !saved {
		return fmt.Errorf(`save bank name: "Save Changes" button not found`)
	}
	if err := run(ctx, chromedp.Sleep(500*time.Millisecond)); err != nil {
		return fmt.Errorf("save bank name: %w", err)
	}
	// Settle with a Sleep after the navigate, like the rest of this file's
	// navigate+JS-click sites — WaitReady("body") alone confirms the document
	// is ready, not that the React bank list has actually finished rendering.
	if err := run(ctx, chromedp.Navigate(banksURL), chromedp.WaitReady("body", chromedp.ByQuery), chromedp.Sleep(time.Second)); err != nil {
		return fmt.Errorf("reopen renamed Item Bank: %w", err)
	}
	// Same "body-ready isn't React-ready" race Import()'s post-create-bank
	// check guards against (see evaluatePollBool's use site there): this
	// reopens the SAME bank list right after a rename, so a one-shot check
	// immediately after the Sleep above can just as easily land on a render
	// that hasn't caught up yet and report a false "not found". Poll for the
	// bank's new title to actually be present before attempting the click,
	// rather than a single blind attempt.
	// Live-confirmed: both 5s and 15s were occasionally too tight for
	// Canvas's own render/autosave latency here (intermittent live failure,
	// not reproducible every run — this reopens the same course Banks list
	// Import()'s post-create-bank check reads, and a course accumulating
	// many banks over repeated runs may render that list more slowly).
	// Bumped to 30s to match this file's other bank-list-reading polls
	// (addFromBankReadyJS, bankItemCountMatchesJS) that already use that
	// budget for the same kind of Canvas-side latency.
	visible, err := pollCheck(ctx, bankExistsJS(jsString(newTitle)), 30*time.Second)
	if err != nil {
		return fmt.Errorf("reopen renamed Item Bank: %w", err)
	}
	if !visible {
		return fmt.Errorf("reopen renamed Item Bank: %q not found in bank list", newTitle)
	}
	// Exact match, not substring — same bank-name mismatch bug fixed at
	// Import()'s open-bank click site: a substring match could reopen a
	// different bank whose name merely contains newTitle as a prefix.
	reopened, err := clickCheck(ctx, clickTextJS(buttonOrLinkTags, newTitle, true))
	if err != nil {
		return fmt.Errorf("reopen renamed Item Bank: %w", err)
	}
	if !reopened {
		return fmt.Errorf("reopen renamed Item Bank: %q not found in bank list", newTitle)
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
	return evaluateBoolViaRun(ctx, run, js)
}

// evaluateBoolViaRun is evaluateBool's production (non-hook) path, factored
// out as a free function so call sites with no ChromedpImporter receiver at
// hand (e.g. renameBankUI) can still check a click's found/clicked signal
// instead of discarding it.
func evaluateBoolViaRun(ctx context.Context, run chromedpRun, js string) (bool, error) {
	var ok bool
	err := runResilient(ctx, run, chromedp.Evaluate(js, &ok))
	return ok, err
}

// evaluatePollBool is evaluateBool's polling counterpart: it waits (via
// chromedp.Poll) up to timeout for js to become true, rather than reading it
// once. Used where body-ready doesn't guarantee the page's React content has
// actually finished rendering yet (see its use site in Import()). Tests
// substitute the same evalBool hook evaluateBool uses, one-shot, since
// chromedp.Poll can't be observed through a mocked run field either. See
// evaluatePollBoolViaRun's doc comment for the full (false, nil)-vs-error
// contract this delegates to in production.
func (c ChromedpImporter) evaluatePollBool(ctx context.Context, run chromedpRun, js string, timeout time.Duration) (bool, error) { //nolint:gocritic // ChromedpImporter is passed by value throughout this file
	if c.evalBool != nil {
		return c.evalBool(ctx, js)
	}
	return evaluatePollBoolViaRun(ctx, run, js, timeout)
}

// evaluatePollBoolViaRun is evaluatePollBool's production (non-hook) path,
// factored out as a free function so call sites with no ChromedpImporter
// receiver at hand (e.g. renameBankUI's post-rename reopen) can poll for a
// condition instead of only being able to one-shot evaluateBoolViaRun — the
// same reasoning evaluateBoolViaRun's doc comment gives for its own split
// from evaluateBool.
//
// isTimeoutLike matches both "this specific chromedp.Poll's own
// WithPollingTimeout expired" and "ctx's own outer deadline (e.g. Import()'s
// workCancel budget) expired while this poll was in flight" — chromedp
// surfaces both as the same "context deadline exceeded"-shaped error, with no
// way to tell them apart from the error alone. Collapsing both to a clean
// (false, nil) "not found" (as this used to do unconditionally) is correct
// for the first case — callers build clear "not found" error messages around
// it — but wrong for the second: a caller's own time budget running out
// mid-poll says nothing about whether the element exists, and reporting it as
// a confirmed "not found" produced a live, misleading "Item Bank was not
// found" error that was actually just Import()'s 150s budget expiring.
// Disambiguate by checking ctx.Err() once the poll gives up: if ctx is
// already done — its own deadline expired, or it was explicitly canceled
// (ctx.Err() is context.Canceled rather than DeadlineExceeded, e.g. a Ctrl-C
// or a parent cancellation) — that outer event is what actually happened, so
// surface it as a real, distinguishable error instead of a false-negative
// "not found"; only report the clean (false, nil) when ctx is still live and
// it was genuinely this poll's own timeout that elapsed.
func evaluatePollBoolViaRun(ctx context.Context, run chromedpRun, js string, timeout time.Duration) (bool, error) {
	err := runResilient(ctx, run, chromedp.Poll(js, nil, chromedp.WithPollingTimeout(timeout)))
	if err != nil {
		if isTimeoutLike(err) {
			if ctx.Err() != nil {
				return false, fmt.Errorf("operation's own context ended while polling, not a confirmed not-found (%w)", ctx.Err())
			}
			return false, nil
		}
		return false, err
	}
	return true, nil
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
	bankItemCountJS = `(document.body.innerText.match(/Question Question \d+/g) || []).length`
	// Live-confirmed: the engine-choice dialog's radio label reads "New
	// Quizzes/Surveys", not a bare "New Quizzes" — the exact-equality match
	// this used to use never matched it, so the radio was never selected,
	// the dialog never got submitted, and every downstream step (starting
	// with "wait for quiz title") hung until the outer timeout. Matched via
	// startsWith rather than includes so an unrelated label that merely
	// mentions "new quizzes" elsewhere in its text can't false-match.
	//
	// Returns one of three strings, not a plain boolean, so the call site
	// (CreateRandomQuiz's "select New Quizzes if prompted" step) can tell
	// "no dialog present" (expected, not an error) apart from "radio
	// clicked but never actually submitted" (a real error — see
	// interpretEngineChoiceResult): "no_dialog" when the radio itself isn't
	// found at all; "clicked_no_submit" when the radio was found and
	// clicked but no submit/continue button was found afterward;
	// "submitted" when the radio was clicked and the submit/continue button
	// was also found and clicked. This "submit" search has never been
	// live-validated (the exact-match bug this replaced meant the radio was
	// never actually clicked in any prior live run) — if Canvas's
	// engine-choice dialog turns out to auto-advance on radio selection
	// alone, or labels its control something other than a bare
	// "Submit"/"Continue", a genuinely successful click could otherwise be
	// misreported as "clicked_no_submit". Guard against that: if the radio
	// itself is no longer in the document after being clicked (the dialog
	// closed/advanced on its own), treat that as success too, not a miss.
	selectNewQuizEngineJS = `(() => { const nodes = Array.from(document.querySelectorAll('label,button,[role="radio"]')); const engine = nodes.find(el => el.innerText.trim().toLowerCase().startsWith('new quizzes')); if (!engine) return 'no_dialog'; engine.click(); const submit = Array.from(document.querySelectorAll('button')).find(el => /^(submit|continue)$/i.test(el.innerText.trim())); if (!submit) return engine.isConnected ? 'clicked_no_submit' : 'submitted'; submit.click(); return 'submitted'; })()`
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

// interpretEngineChoiceResult turns selectNewQuizEngineJS's three-way string
// result into an error/no-error decision for CreateRandomQuiz's "select New
// Quizzes if prompted" step. Canvas doesn't always show the engine-choice
// dialog, so "no_dialog" is expected and not an error; "submitted" (radio
// clicked and the submit/continue control also found and clicked) is
// likewise success. Only "clicked_no_submit" — the radio was found and
// clicked, but no submit/continue button was found afterward — is a genuine
// error: silently tolerating it here (as an undifferentiated boolean used to)
// let execution fall straight into the unbounded WaitVisible(quizTitleSelector)
// that follows this step, hanging until CreateRandomQuiz's outer 240s budget
// expired with no clue this half-interacted dialog was the real cause — the
// exact failure shape (silently missed click -> unbounded downstream wait ->
// confusing timeout far from the real cause) the rest of this file's InstUI
// click fixes exist to eliminate. Any other/unrecognized value (including
// empty — e.g. under a test run mock that never populates chromedp.Evaluate's
// out-parameter) is treated permissively as a no-op success, matching this
// step's pre-existing tolerant behavior for "dialog absent".
func interpretEngineChoiceResult(outcome string) error {
	if outcome == "clicked_no_submit" {
		return fmt.Errorf("engine-choice dialog: radio selected but no submit/continue button found")
	}
	return nil
}

func bankTitleMatchesJS(title string) string {
	return `(() => { const h = document.querySelector('h1'); return Boolean(h && h.innerText.trim() === ` + jsString(title) + `); })()`
}

func bankItemCountMatchesJS(count int) string {
	return fmt.Sprintf("(%s) === %d", bankItemCountJS, count)
}

// textXPathInsensitive builds the case-insensitive text-match XPath
// expression used by the "add Item Bank to quiz" step in CreateRandomQuiz,
// which needs to WaitEnabled on the identical node before clicking it — see
// that step's doc comment for why the wait and click must resolve to exactly
// the same node.
func textXPathInsensitive(text string) string {
	lower := strings.ToLower(text)
	translate := "translate(normalize-space(), 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz')"
	return "//*[self::button or self::a or @role='button'][contains(" + translate + ", " + xpathString(lower) + ")]"
}

// clickJS returns a JS snippet that evaluates matchExpr (a JS expression
// yielding a single Element, or null/undefined on no match) and, if found,
// dispatches a real DOM .click() directly on it, instead of going through
// chromedp.Click's synthetic mouse-event hit-testing. Recorded live against
// Canvas's Instructure UI (InstUI) React components: a hit-test click at an
// InstUI control's computed center can silently miss (icon/facade wrappers,
// a checkbox visually covered by its own label) with no chromedp error and
// no visible effect — first diagnosed and fixed for the per-group "Edit Bank
// containing questions..." control (see clickEditBankGroupJS's doc comment
// below), generalized here so every other InstUI button/checkbox click in
// this file goes through the same, proven-reliable dispatch instead of each
// call site hand-rolling its own find-and-click snippet. Every clickJS-
// derived click site in this file routes its result through evaluateBool (or
// a clickCheck-style wrapper around it, e.g. renameBankUI's) and errors out
// on a miss — none pass a bare nil out-param and silently ignore it, since a
// silently-missed click is exactly the failure class this helper exists to
// catch (see docs/item-bank-import-flake.md).
func clickJS(matchExpr string) string {
	return `(() => {
		const target = ` + matchExpr + `;
		if (target) { target.click(); return true; }
		return false;
	})()`
}

// clickSelectorJS returns clickJS matching the first element found by a
// plain CSS selector — used both for a stable data-automation attribute
// (preferred wherever Canvas exposes one on an InstUI control; confirmed live
// for the create-bank submit button, since it is more robust than
// text-content matching to Canvas UI copy/wording changes) and other plain
// selectors with no such attribute, like the Item Bank actions popover
// trigger and its menu item.
func clickSelectorJS(selector string) string {
	return clickJS(`document.querySelector(` + jsString(selector) + `)`)
}

// checkCheckboxJS returns a JS snippet that ensures the checkbox matched by
// selector ends up checked, clicking it only if it isn't already, and
// returns its final .checked value — not just whether an element was found.
// A plain clickJS/clickSelectorJS .click() toggles a checkbox's state, so
// reusing either against a checkbox Canvas may already default to checked
// (the create-bank "Share with course" checkbox) could silently *uncheck* it
// while still reporting a clean "found" success; this checks state first so
// the same call is idempotent regardless of the checkbox's starting state.
// Kept as its own small helper rather than folded into clickSelectorJS/
// clickJS, since every other click site in this file genuinely wants an
// unconditional toggle-by-click and this one specifically must not.
func checkCheckboxJS(selector string) string {
	return `(() => {
		const target = document.querySelector(` + jsString(selector) + `);
		if (!target) return false;
		if (!target.checked) target.click();
		return target.checked;
	})()`
}

// clickTextJS returns clickJS matching the first element among tags (a list
// of tag names or compound CSS selectors, ORed together) whose trimmed
// innerText matches text — exact equality when exact is true, substring
// otherwise. Uses innerText (like every other JS helper in this file — see
// bankExistsJS, clickBankRowJS), not textContent: innerText excludes hidden
// and duplicate text, which is exactly what avoids a nested icon label
// duplicating text into an element's own textContent (recorded live:
// Canvas's top-level "Create Bank" button's textContent actually renders as
// "Create BankBank", which innerText.trim() does not reproduce). Substring
// matching is deliberately forgiving of that kind of artifact regardless.
func clickTextJS(tags []string, text string, exact bool) string {
	selector := strings.Join(tags, ",")
	cmp := "el.innerText.trim().includes(" + jsString(text) + ")"
	if exact {
		cmp = "el.innerText.trim() === " + jsString(text)
	}
	return clickJS(`Array.from(document.querySelectorAll(` + jsString(selector) + `)).find(el => ` + cmp + `)`)
}

// clickTextInsensitiveJS is clickTextJS's case-insensitive-substring
// counterpart, matching among button/a/[role="button"] elements — the same
// element set textXPathInsensitive's XPath matched, kept for the InstUI
// controls (e.g. "Quiz/Survey", "Build", "Done") that render their
// clickable text with a case Canvas doesn't guarantee.
func clickTextInsensitiveJS(text string) string {
	lower := strings.ToLower(text)
	return clickJS(`Array.from(document.querySelectorAll('button,a,[role="button"]')).find(el => el.innerText.trim().toLowerCase().includes(` + jsString(lower) + `))`)
}

// clickXPathJS returns clickJS matching the first element found by
// document.evaluate(xpath, ...) — used where a click must land on exactly the
// same node a chromedp WaitEnabled/WaitVisible XPath expression already
// waited on (see importButtonXPath's use in Import() and the "add Item Bank
// to quiz" step in CreateRandomQuiz), so the two can't resolve to different
// elements in a dialog with more than one matching button.
func clickXPathJS(xpath string) string {
	return clickJS(`document.evaluate(` + jsString(xpath) + `, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null).singleNodeValue`)
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
// via JS .click() rather than textXPathInsensitive's XPath. Instructure's
// radio buttons render their clickable text inside a <label>, not a
// button/a/[role=button] — the tag set textXPathInsensitive's XPath matches
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

// createBankSubmitSelector selects the create-bank dialog's submit button by
// its stable data-automation attribute (confirmed live), rather than by text
// content, so it isn't fragile to a future Canvas UI copy change. This is a
// different button from the top-level "Create Bank" list button that opens
// this dialog (clicked via clickTextJS, substring-matched) — see
// clickTextJS's doc comment for the textContent-duplication artifact
// confirmed live on that button, not this one.
const createBankSubmitSelector = `[data-automation="sdk-create-bank-button"]`

// shareWithCourseCheckboxSelector selects the create-bank dialog's "Share
// with course" checkbox — a distinct element from the "[role=dialog] input"
// selector the bank-NAME text field's own wait uses (that plain selector
// matches the name field first, since it comes first in document order), so
// this exists to give the checkbox's own wait/click sites a selector that
// unambiguously targets it.
const shareWithCourseCheckboxSelector = `[role=dialog] input[type=checkbox]`

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
