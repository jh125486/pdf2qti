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
	run      chromedpRun
	findBank func(context.Context, string) (bool, error)
	location func(context.Context) (string, error)
}

type chromedpRun func(context.Context, ...chromedp.Action) error

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
	return Result{BankURL: location}, nil
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
