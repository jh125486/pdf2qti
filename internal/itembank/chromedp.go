package itembank

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// ChromedpImporter drives only documented Canvas UI actions in an existing,
// authenticated Chrome instance started with remote debugging enabled.
type ChromedpImporter struct {
	run           chromedpRun
	findBank      func(context.Context, string) (bool, error)
	location      func(context.Context) (string, error)
	bankTitle     func(context.Context) (string, error)
	bankItemCount func(context.Context) (int, error)
	renameBank    func(context.Context, string, string) error
	quizLocation  func(context.Context) (string, error)
	quizExists    func(context.Context, string) (bool, error)
}

type chromedpRun func(context.Context, ...chromedp.Action) error

const buildQuizStep = "build quiz"

func (c ChromedpImporter) Import(ctx context.Context, req *Request) (Result, error) { //nolint:gocyclo // browser UI state machine
	if req == nil {
		return Result{}, fmt.Errorf("item bank import request is required")
	}
	allocator, cancel := chromedp.NewRemoteAllocator(ctx, req.BrowserURL)
	defer cancel()
	browser, cancel := chromedp.NewContext(allocator)
	defer cancel()
	browser, cancel = context.WithTimeout(browser, 90*time.Second)
	defer cancel()
	run := c.run
	if run == nil {
		run = chromedp.Run
	}

	base, err := url.Parse(req.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return Result{}, fmt.Errorf("invalid Canvas base URL %q", req.BaseURL)
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
func (c ChromedpImporter) PreflightRandomQuiz(ctx context.Context, req *QuizRequest) error {
	if err := validateQuizRequest(req, false); err != nil {
		return err
	}
	base, err := canvasURL(req.BaseURL, req.CourseID, "/quizzes")
	if err != nil {
		return err
	}
	allocator, cancel := chromedp.NewRemoteAllocator(ctx, req.BrowserURL)
	defer cancel()
	browser, cancel := chromedp.NewContext(allocator)
	defer cancel()
	browser, cancel = context.WithTimeout(browser, 30*time.Second)
	defer cancel()
	run := c.run
	if run == nil {
		run = chromedp.Run
	}
	if err := run(browser, chromedp.Navigate(base), chromedp.WaitReady("body", chromedp.ByQuery), chromedp.Sleep(time.Second)); err != nil {
		return fmt.Errorf("open Quizzes: %w", err)
	}
	return c.checkQuizCollision(browser, run, QuizTitle(req.BankName))
}

// CreateRandomQuiz drives Canvas's New Quizzes builder. Canvas does not expose
// creation of Item Bank-backed quiz groups through its public API, so this
// intentionally uses only visible UI controls in an authenticated browser.
func (c ChromedpImporter) CreateRandomQuiz(ctx context.Context, req *QuizRequest) (QuizResult, error) { //nolint:gocyclo // browser UI state machine
	if err := validateQuizRequest(req, true); err != nil {
		return QuizResult{}, err
	}
	base, err := canvasURL(req.BaseURL, req.CourseID, "/quizzes")
	if err != nil {
		return QuizResult{}, err
	}
	allocator, cancel := chromedp.NewRemoteAllocator(ctx, req.BrowserURL)
	defer cancel()
	browser, cancel := chromedp.NewContext(allocator)
	defer cancel()
	browser, cancel = context.WithTimeout(browser, 120*time.Second)
	defer cancel()
	run := c.run
	if run == nil {
		run = chromedp.Run
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
		{"open quiz creator", clickTextInsensitive("+ Quiz")},
		{"wait for quiz setup", chromedp.Poll(quizSetupReadyJS, nil, chromedp.WithPollingTimeout(10*time.Second))},
		{"select New Quizzes if prompted", chromedp.Evaluate(selectNewQuizEngineJS, nil)},
		{"wait for quiz title", chromedp.WaitVisible(quizTitleSelector, chromedp.ByQuery)},
		{"set quiz title", chromedp.SetValue(quizTitleSelector, quizTitle, chromedp.ByQuery)},
		{buildQuizStep, clickTextInsensitive("Build")},
		{"wait for Item Bank action", chromedp.Poll(addFromBankReadyJS, nil, chromedp.WithPollingTimeout(30*time.Second))},
		{"open Item Bank picker", clickTextInsensitive("Add from item bank")},
		{"select Item Bank", clickBank(req.BankID, req.BankName)},
		{"add Item Bank to quiz", clickTextInsensitive("Add this bank to quiz")},
		{"edit Item Bank group", clickTextInsensitive("Edit bank group")},
		{"enable random questions", clickTextInsensitive("Randomly select questions")},
		{"wait for question count", chromedp.WaitVisible(randomCountSelector, chromedp.ByQuery)},
		{"set question count", chromedp.SetValue(randomCountSelector, fmt.Sprintf("%d", req.QuestionCount), chromedp.ByQuery)},
		{"save Item Bank group", clickTextInsensitive("Done")},
		{"verify random group", chromedp.Poll(randomGroupJS(req.BankName, req.QuestionCount), nil, chromedp.WithPollingTimeout(10*time.Second))},
	}
	for _, step := range steps {
		if err := run(browser, step.action); err != nil {
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
	editSelector := "//button[@aria-label=" + xpathString("Edit bank "+oldTitle) + "]"
	if err := run(ctx, chromedp.Click(editSelector, chromedp.BySearch)); err != nil {
		return fmt.Errorf("open edit bank dialog: %w", err)
	}
	if err := run(ctx, chromedp.WaitVisible("[role=dialog] input", chromedp.ByQuery)); err != nil {
		return fmt.Errorf("wait for bank-name field: %w", err)
	}
	if err := run(ctx,
		chromedp.Click("[role=dialog] input", chromedp.ByQuery, chromedp.NodeVisible),
		chromedp.SetValue("[role=dialog] input", "", chromedp.ByQuery),
		chromedp.SendKeys("[role=dialog] input", newTitle, chromedp.ByQuery),
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

func (c ChromedpImporter) checkQuizCollision(ctx context.Context, run chromedpRun, title string) error {
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
	quizTitleSelector     = `input[name="name"], input[aria-label*="Assignment Name" i], input[placeholder*="Assignment Name" i]`
	randomCountSelector   = `[role="dialog"] input[type="number"]`
	bankTitleJS           = `(() => { const h = document.querySelector('h1'); return h ? h.innerText.trim() : ''; })()`
	bankItemCountJS       = `(() => { const selectors = ['[data-testid*="item-bank-item"]', '[data-testid*="item-bank-question"]', '[data-testid*="question"]', '[class*="item-bank-item"]']; for (const selector of selectors) { const count = document.querySelectorAll(selector).length; if (count > 0) return count; } const match = document.body.innerText.match(/(\d+)\s+(?:questions|items)\b/i); return match ? Number(match[1]) : 0; })()`
	selectNewQuizEngineJS = `(() => { const nodes = Array.from(document.querySelectorAll('label,button,[role="radio"]')); const engine = nodes.find(el => el.innerText.trim().toLowerCase() === 'new quizzes'); if (!engine) return false; engine.click(); const submit = Array.from(document.querySelectorAll('button')).find(el => /^(submit|continue)$/i.test(el.innerText.trim())); if (submit) submit.click(); return true; })()`
	quizSetupReadyJS      = `Boolean(document.querySelector('input[name="name"], input[aria-label*="Assignment Name" i], input[placeholder*="Assignment Name" i]')) || document.body.innerText.toLowerCase().includes('new quizzes')`
	addFromBankReadyJS    = `document.body.innerText.toLowerCase().includes('add from item bank')`
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

func clickBank(bankID, bankName string) chromedp.Action {
	return chromedp.Click("//*[self::button or self::a or @role='button'][@data-bank-id="+xpathString(bankID)+" or contains(@href, "+xpathString("/banks/"+bankID)+")][contains(translate(normalize-space(), 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), "+xpathString(strings.ToLower(strings.TrimSpace(bankName)))+")]", chromedp.BySearch)
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
