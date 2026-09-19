// Whitebox tests inject Chromedp's unexported runner and exercise selector helpers.
package itembank

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestChromedpImporterImport_Table(t *testing.T) { //nolint:gocyclo // table covers browser state-machine failures
	t.Parallel()
	// These are the exact JS expressions particular Import() click/check
	// sites send, computed once so the table below and the mock evalBool
	// hook agree on them by construction (not by retyping a literal that
	// could quietly drift from the real call site).
	wantVisibilityJS := bankExistsJS(`"Bank"`)
	wantCreateDialogJS := clickTextJS(buttonOrLinkTags, "Create Bank", false)
	wantCheckboxJS := checkCheckboxJS(shareWithCourseCheckboxSelector)
	wantSubmitCreateJS := clickSelectorJS(createBankSubmitSelector)
	wantOpenBankJS := clickTextJS(buttonOrLinkTags, "Bank", true)
	wantPopoverTriggerJS := clickSelectorJS(`button[data-popover-trigger="true"]`)
	wantMenuItemJS := clickSelectorJS(`[role="menuitem"]`)
	wantImportClickJS := clickXPathJS(importButtonXPath)
	tests := []struct {
		name          string
		failAt        int    // fails the Nth plain (non-evaluateBool) run() call.
		failClickJS   string // fails the evaluateBool call sending this exact JS.
		failClickErr  bool   // failClickJS's evaluateBool returns an error instead of a clean "not found".
		existing      bool
		findErr       error
		onExisting    Existing
		expectedCalls int
		wantErr       string
		wantURL       string
		// visibilityNotFound and visibilityErr fold
		// TestChromedpImporterImport_AbortsWhenCreatedBankNotVisible and
		// TestChromedpImporterImport_CreatedBankVisibilityCheckErrors into
		// this table (per repo convention: one test function per exported
		// func) — they exercise the post-create visibility check specifically,
		// the same evalBool mechanism failClickJS uses for the click sites.
		visibilityNotFound bool
		visibilityErr      error
	}{
		{name: "success", onExisting: ExistingAppend, expectedCalls: 13, wantURL: "https://canvas.example.edu/courses/7/banks/42"},
		{name: "existing bank append", existing: true, onExisting: ExistingAppend, expectedCalls: 9, wantURL: "https://canvas.example.edu/courses/7/banks/42"},
		{name: "existing bank fails", existing: true, onExisting: ExistingFail, expectedCalls: 1, wantErr: `item bank "Bank" already exists`},
		{name: "find bank error", findErr: errors.New("lookup failed"), onExisting: ExistingAppend, expectedCalls: 1, wantErr: "find Item Bank"},
		{name: "open banks", failAt: 1, onExisting: ExistingAppend, expectedCalls: 1, wantErr: "open Item Banks"},
		{name: "find bank", failAt: 2, onExisting: ExistingAppend, expectedCalls: 2, wantErr: "find Item Bank"},
		{name: "open create dialog", failClickJS: wantCreateDialogJS, onExisting: ExistingAppend, expectedCalls: 2, wantErr: "open create bank dialog"},
		// Exercises the "an evaluateBool-style click check returns an error"
		// path (as opposed to a clean "not found" false/nil) — previously
		// untested anywhere in this table.
		{name: "open create dialog errors", failClickJS: wantCreateDialogJS, failClickErr: true, onExisting: ExistingAppend, expectedCalls: 2, wantErr: "open create bank dialog"},
		{name: "bank name field", failAt: 3, onExisting: ExistingAppend, expectedCalls: 3, wantErr: "wait for bank-name field"},
		{name: "fill bank name", failAt: 4, onExisting: ExistingAppend, expectedCalls: 4, wantErr: "fill bank name"},
		{name: "wait checkbox", failAt: 5, onExisting: ExistingAppend, expectedCalls: 5, wantErr: "wait for share-with-course checkbox"},
		{name: "share course", failClickJS: wantCheckboxJS, onExisting: ExistingAppend, expectedCalls: 5, wantErr: "share bank with course"},
		{name: "wait submit button", failAt: 6, onExisting: ExistingAppend, expectedCalls: 6, wantErr: "wait for create bank submit button"},
		{name: "submit create", failClickJS: wantSubmitCreateJS, onExisting: ExistingAppend, expectedCalls: 6, wantErr: "submit create bank"},
		{name: "return to banks", failAt: 8, onExisting: ExistingAppend, expectedCalls: 8, wantErr: "return to Item Banks"},
		{name: "created bank not visible", onExisting: ExistingAppend, visibilityNotFound: true, expectedCalls: 8, wantErr: "was not found in the course bank list"},
		{name: "created bank visibility check errors", onExisting: ExistingAppend, visibilityErr: errors.New("evaluate failed"), expectedCalls: 8, wantErr: "verify created Item Bank is visible"},
		{name: "open bank", existing: true, failClickJS: wantOpenBankJS, onExisting: ExistingAppend, expectedCalls: 1, wantErr: "open Item Bank"},
		{name: "wait actions", existing: true, failAt: 2, onExisting: ExistingAppend, expectedCalls: 2, wantErr: "wait for Item Bank actions"},
		{name: "open actions", existing: true, failClickJS: wantPopoverTriggerJS, onExisting: ExistingAppend, expectedCalls: 5, wantErr: "open import actions"},
		{name: "wait import menu", existing: true, failAt: 6, onExisting: ExistingAppend, expectedCalls: 6, wantErr: "wait for import menu"},
		{name: "open dialog", existing: true, failClickJS: wantMenuItemJS, onExisting: ExistingAppend, expectedCalls: 6, wantErr: "open import dialog"},
		{name: "attach package", existing: true, failAt: 7, onExisting: ExistingAppend, expectedCalls: 7, wantErr: "attach package"},
		{name: "submit import", existing: true, failClickJS: wantImportClickJS, onExisting: ExistingAppend, expectedCalls: 8, wantErr: "submit import"},
		{name: "wait completion", existing: true, failAt: 9, onExisting: ExistingAppend, expectedCalls: 9, wantErr: "wait for import completion"},
		{name: "read location", existing: true, failAt: 10, onExisting: ExistingAppend, expectedCalls: 10, wantErr: "read Item Bank URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			importer := ChromedpImporter{run: func(_ context.Context, actions ...chromedp.Action) error {
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
			},
				// evaluateBool/evaluatePollBool's mocked run() field never
				// populates chromedp.Evaluate's out-parameter, so every
				// evaluateBool-routed click in this table must go through this
				// hook (not the production Evaluate path) to succeed by
				// default — it only forces failure for the one specific JS
				// expression a row is testing, so an unrelated earlier click
				// in the same row's flow isn't affected.
				evalBool: func(_ context.Context, js string) (bool, error) {
					if js == wantVisibilityJS {
						if tt.visibilityErr != nil {
							return false, tt.visibilityErr
						}
						return !tt.visibilityNotFound, nil
					}
					if tt.failClickJS != "" && js == tt.failClickJS {
						if tt.failClickErr {
							return false, errors.New("browser failed")
						}
						return false, nil
					}
					return true, nil
				},
			}
			if !tt.existing && tt.findErr == nil {
				// Exercise production Evaluate path for normal create-bank flow and its
				// failure points; existing-bank cases use injected lookup result below.
				importer.findBank = nil
			}
			if tt.wantURL != "" {
				importer.location = func(context.Context) (string, error) { return tt.wantURL, nil }
			}
			result, err := importer.Import(t.Context(), &Request{
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
			}
		})
	}
}

func TestImportTimeout_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name              string
		expectedItemCount int
		want              time.Duration
	}{
		{name: "no expected count", expectedItemCount: 0, want: 300 * time.Second},
		{name: "small bank", expectedItemCount: 20, want: 300 * time.Second},
		{name: "large bank scales", expectedItemCount: 60, want: 340 * time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := importTimeout(tt.expectedItemCount); got != tt.want {
				t.Fatalf("importTimeout(%d) = %v, want %v", tt.expectedItemCount, got, tt.want)
			}
		})
	}
}

func TestIsTimeoutLike_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "context deadline exceeded", err: errors.New("context deadline exceeded"), want: true},
		{name: "poll timeout", err: errors.New("waiting for function failed: timeout"), want: true},
		{name: "poll JS exception is not a timeout", err: errors.New("waiting for function failed: SyntaxError: Unexpected token"), want: false},
		{name: "unrelated error", err: errors.New("browser failed"), want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isTimeoutLike(tt.err); got != tt.want {
				t.Fatalf("isTimeoutLike(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestChromedpImporterRecoverStuckImport_UnknownBaselineNeverRecovers covers a
// bug found in review: a failed pre-upload baseline read must not silently
// fall back to treating the bank as empty. Defaulting to 0 would let an
// ExistingAppend onto a bank that already had content report "recovered" as
// soon as that pre-existing content became visible again, even though this
// run's own upload never completed.
func TestChromedpImporterRecoverStuckImport_UnknownBaselineNeverRecovers(t *testing.T) {
	t.Parallel()
	importer := ChromedpImporter{
		run:           func(context.Context, ...chromedp.Action) error { return nil },
		bankItemCount: func(context.Context) (int, error) { return 5, nil }, // bank has content
	}
	if got := importer.recoverStuckImport(t.Context(), importer.run, "https://canvas.example.edu/courses/7/banks", "Bank", "", -1); got {
		t.Fatal("recoverStuckImport() = true with an unknown (-1) baseline, want false")
	}
}

// TestChromedpImporterStableBankItemCount_Table covers a bug found in
// review: the rendered item count comes from a virtualized card list that
// can still be mid-render, so a single read can return a transient 0 or
// partial count. stableBankItemCount must require two reads to agree before
// trusting the value, and report unknown (not a guess) when they don't.
func TestChromedpImporterStableBankItemCount_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		reads      []int
		readErrAt  int // 1-indexed read that fails, 0 means none fail
		sleepErr   error
		wantOK     bool
		wantResult int
	}{
		{name: "agreeing reads succeed", reads: []int{4, 4}, wantOK: true, wantResult: 4},
		{name: "still-rendering mismatch reports unknown", reads: []int{0, 4}, wantOK: false},
		{name: "first read errors", reads: []int{0, 0}, readErrAt: 1, wantOK: false},
		{name: "second read errors", reads: []int{4, 0}, readErrAt: 2, wantOK: false},
		{name: "settle sleep fails", reads: []int{4, 4}, sleepErr: errors.New("navigate interrupted"), wantOK: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reads := 0
			importer := ChromedpImporter{
				run: func(_ context.Context, _ ...chromedp.Action) error { return tt.sleepErr },
				bankItemCount: func(context.Context) (int, error) {
					reads++
					if reads == tt.readErrAt {
						return 0, errors.New("read failed")
					}
					return tt.reads[reads-1], nil
				},
			}
			got, ok := importer.stableBankItemCount(t.Context(), importer.run)
			if ok != tt.wantOK {
				t.Fatalf("stableBankItemCount() ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK && got != tt.wantResult {
				t.Fatalf("stableBankItemCount() = %d, want %d", got, tt.wantResult)
			}
		})
	}
}

// TestChromedpImporterImport_RecoversFromUploadTimeout covers the two flakes
// documented in docs/item-bank-import-flake.md: a plain timeout on
// attach/submit/wait-completion recovers by re-checking the bank for actual
// content rather than always erroring out and leaving a stray empty bank
// behind; a non-timeout error (or a timeout with no content found) still
// fails as before.
func TestChromedpImporterImport_RecoversFromUploadTimeout(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name          string
		failAt        int
		failErr       error
		recoveredJS   int // bankItemCount returned by recoverStuckImport's check
		recoverErr    error
		expectedCalls int
		wantErr       string
		wantRecovered bool
	}{
		{name: "non-timeout error still fails immediately", failAt: 11, failErr: errors.New("browser failed"), expectedCalls: 11, wantErr: "attach package"},
		{name: "timeout but bank still empty on recheck", failAt: 11, failErr: errors.New("context deadline exceeded"), recoveredJS: 0, expectedCalls: 13, wantErr: "attach package"},
		{name: "timeout but recheck navigation fails", failAt: 11, failErr: errors.New("context deadline exceeded"), recoverErr: errors.New("navigate failed"), expectedCalls: 12, wantErr: "attach package"},
		{name: "attach timeout recovers", failAt: 11, failErr: errors.New("context deadline exceeded"), recoveredJS: 3, expectedCalls: 13, wantRecovered: true},
		{name: "submit timeout recovers", failAt: 12, failErr: errors.New("waiting for function failed: timeout"), recoveredJS: 3, expectedCalls: 14, wantRecovered: true},
		{name: "completion wait timeout recovers", failAt: 13, failErr: errors.New("waiting for function failed: timeout"), recoveredJS: 3, expectedCalls: 15, wantRecovered: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			importer := ChromedpImporter{
				run: func(_ context.Context, _ ...chromedp.Action) error {
					calls++
					if calls == tt.failAt {
						return tt.failErr
					}
					if calls == tt.failAt+1 && tt.recoverErr != nil {
						return tt.recoverErr
					}
					return nil
				},
				// This test's bank is freshly created (found == false), so its
				// baseline is hardcoded to 0 by Import() itself, not read through
				// this hook; the only call recoverStuckImport's post-recovery
				// check needs to see growth past that 0 baseline.
				bankItemCount: func(context.Context) (int, error) { return tt.recoveredJS, nil },
				location:      func(context.Context) (string, error) { return "https://canvas.example.edu/courses/7/banks/42", nil },
				// The post-create-bank visibility check needs a "found"
				// result; the mocked run field never populates
				// chromedp.Evaluate's out-parameter to provide one.
				evalBool: func(context.Context, string) (bool, error) { return true, nil },
			}
			result, err := importer.Import(t.Context(), &Request{
				BaseURL: "https://canvas.example.edu", BrowserURL: "http://127.0.0.1:9222",
				CourseID: "7", BankName: "Bank", Package: "quiz.zip", OnExisting: ExistingAppend,
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
			if tt.wantRecovered && result.BankURL == "" {
				t.Fatalf("Import() result = %+v, want recovered result with BankURL set", result)
			}
		})
	}
}

// clickJSWant builds clickJS's exact expected output for matchExpr, mirroring
// its template verbatim (including whitespace) so a golden-string comparison
// actually exercises the template, not just a loose substring check.
func clickJSWant(matchExpr string) string {
	return `(() => {
		const target = ` + matchExpr + `;
		if (target) { target.click(); return true; }
		return false;
	})()`
}

// TestClickJS_Table covers clickJS, the shared JS-.click()-dispatch helper
// this fix generalizes from clickEditBankGroupJS: it must embed the match
// expression verbatim and dispatch a direct .click() rather than a
// synthetic hit-test event.
func TestClickJS_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, matchExpr string }{
		{name: "document body", matchExpr: "document.body"},
		{name: "querySelector expression", matchExpr: `document.querySelector("[role=dialog] input[type=checkbox]")`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := clickJS(tt.matchExpr)
			if want := clickJSWant(tt.matchExpr); got != want {
				t.Fatalf("clickJS(%q) = %q, want %q", tt.matchExpr, got, want)
			}
		})
	}
}

// TestClickSelectorJS_Table covers the create-bank submit button and the
// Item Bank actions popover trigger's data-automation-/attribute-based click
// sites, asserting the exact generated JS (a golden string) so a semantics
// change — e.g. swapping querySelector for querySelectorAll, or losing the
// selector escaping jsString provides — shows up in the diff. The
// share-with-course checkbox is not covered here: it clicks through
// checkCheckboxJS (see TestCheckCheckboxJS_Table), not clickSelectorJS,
// specifically because a checkbox needs check-before-click semantics this
// plain unconditional-click helper doesn't provide.
func TestClickSelectorJS_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, selector string }{
		{name: "data-automation submit button", selector: createBankSubmitSelector},
		{name: "popover trigger", selector: `button[data-popover-trigger="true"]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := clickSelectorJS(tt.selector)
			want := clickJSWant(`document.querySelector(` + jsString(tt.selector) + `)`)
			if got != want {
				t.Fatalf("clickSelectorJS(%q) = %q, want %q", tt.selector, got, want)
			}
		})
	}
}

// TestCheckCheckboxJS_Table covers checkCheckboxJS, the checkbox-specific
// click helper introduced because a plain JS .click() (what clickJS/
// clickSelectorJS dispatch) toggles a checkbox's state — reusing either
// against the create-bank "Share with course" checkbox could silently
// uncheck it if Canvas ever defaults it checked. Asserts the exact generated
// JS (a golden string): it must check target.checked before clicking, and
// return the final .checked value, not just a found/not-found boolean.
func TestCheckCheckboxJS_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, selector string }{
		{name: "share with course checkbox", selector: shareWithCourseCheckboxSelector},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkCheckboxJS(tt.selector)
			want := `(() => {
		const target = document.querySelector(` + jsString(tt.selector) + `);
		if (!target) return false;
		if (!target.checked) target.click();
		return target.checked;
	})()`
			if got != want {
				t.Fatalf("checkCheckboxJS(%q) = %q, want %q", tt.selector, got, want)
			}
		})
	}
}

// TestClickTextJS_Table covers the text-based click sites this fix converted
// to JS dispatch (the top-level "Create Bank" button, the Import dialog's
// submit button, and the Banks-list row/rename-dialog buttons), including
// exact vs. substring matching. Asserts the exact generated JS (a golden
// string), which would catch e.g. a regression back to textContent (the
// nested-icon-label "Create BankBank" artifact clickTextJS's doc comment
// describes) since the golden string hardcodes innerText.
func TestClickTextJS_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		tags  []string
		text  string
		exact bool
	}{
		{name: "substring match", tags: buttonOrLinkTags, text: "Create Bank"},
		{name: "exact match in dialog", tags: []string{`[role="dialog"] button`}, text: "Save Changes", exact: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := clickTextJS(tt.tags, tt.text, tt.exact)
			cmp := "el.innerText.trim().includes(" + jsString(tt.text) + ")"
			if tt.exact {
				cmp = "el.innerText.trim() === " + jsString(tt.text)
			}
			selector := strings.Join(tt.tags, ",")
			want := clickJSWant(`Array.from(document.querySelectorAll(` + jsString(selector) + `)).find(el => ` + cmp + `)`)
			if got != want {
				t.Fatalf("clickTextJS(%v, %q, %v) = %q, want %q", tt.tags, tt.text, tt.exact, got, want)
			}
		})
	}
}

// findByInnerText mirrors the JS `Array.from(...).find(el => cmp)` predicate
// clickTextJS generates (exact ("===") vs. substring ("includes")), against a
// plain Go slice standing in for document order — used by
// TestClickTextJS_ExactVsSubstringBankNameMatch below to prove, without a
// JS/browser runtime, that the matching MODE (not just the generated string
// shape TestClickTextJS_Table's golden-string check covers) behaves as
// clickTextJS's own doc comment claims: exact equality never matches a
// longer name merely containing it, while substring matching can.
func findByInnerText(candidates []string, text string, exact bool) (string, bool) {
	for _, c := range candidates {
		if exact {
			if c == text {
				return c, true
			}
			continue
		}
		if strings.Contains(c, text) {
			return c, true
		}
	}
	return "", false
}

// TestClickTextJS_ExactVsSubstringBankNameMatch covers F2: opening a specific
// Item Bank by name must use exact matching, not substring, or a course
// containing both "Chapter 1" and "Chapter 11" can silently open/click the
// wrong one when targeting "Chapter 1" — exactly the bug fixed at Import()'s
// open-bank click site (clickTextJS(..., req.BankName, true)) and the two
// bank-name click sites in renameBankUI, all of which used to pass exact:
// false. This proves the matching semantics directly: exact mode only ever
// finds the bank literally named "Chapter 1", never "Chapter 11", regardless
// of list order; substring mode is shown to be capable of matching the wrong
// one when the false-positive candidate sorts first — the exact failure
// shape a live import previously exhibited.
func TestClickTextJS_ExactVsSubstringBankNameMatch(t *testing.T) {
	t.Parallel()
	target := "Chapter 1"
	for _, tt := range []struct {
		name       string
		candidates []string
	}{
		{name: "target first in document order", candidates: []string{"Chapter 1", "Chapter 11"}},
		{name: "false-positive candidate first in document order", candidates: []string{"Chapter 11", "Chapter 1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := findByInnerText(tt.candidates, target, true)
			if !ok || got != "Chapter 1" {
				t.Fatalf("exact match against %v = (%q, %v), want (\"Chapter 1\", true)", tt.candidates, got, ok)
			}
		})
	}
	// Demonstrates the failure mode substring matching is vulnerable to: when
	// the false-positive candidate happens to render first, a substring match
	// finds the WRONG bank instead of the requested one.
	t.Run("substring match can find the wrong bank", func(t *testing.T) {
		t.Parallel()
		got, ok := findByInnerText([]string{"Chapter 11", "Chapter 1"}, target, false)
		if !ok || got != "Chapter 11" {
			t.Fatalf("substring match against [Chapter 11, Chapter 1] = (%q, %v), want (\"Chapter 11\", true) demonstrating the false-match risk", got, ok)
		}
	})
	// clickTextJS itself must generate exact (===) matching for a bank-name
	// target, per the golden-string assertion in TestClickTextJS_Table — this
	// asserts the concrete call site's own generated comparison operator
	// directly, so a regression back to substring for a bank-name click site
	// is caught here specifically, not just in the generic table.
	if got := clickTextJS(buttonOrLinkTags, target, true); !strings.Contains(got, "el.innerText.trim() === "+jsString(target)) {
		t.Fatalf("clickTextJS(..., %q, true) = %q, want an exact-equality (===) comparison", target, got)
	}
}

// TestClickTextInsensitiveJS covers the quiz-builder click sites ("Quiz/
// Survey", "Build", "Add from item bank", "Done", "Add this bank to quiz")
// that route through this case-insensitive JS matcher, asserting the exact
// generated JS (a golden string) so a textContent-vs-innerText regression
// would be caught here too.
func TestClickTextInsensitiveJS(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, text string }{
		{name: "Quiz/Survey", text: "Quiz/Survey"},
		{name: "Build", text: "Build"},
		{name: "Done", text: "Done"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := clickTextInsensitiveJS(tt.text)
			lower := strings.ToLower(tt.text)
			want := clickJSWant(`Array.from(document.querySelectorAll('button,a,[role="button"]')).find(el => el.innerText.trim().toLowerCase().includes(` + jsString(lower) + `))`)
			if got != want {
				t.Fatalf("clickTextInsensitiveJS(%q) = %q, want %q", tt.text, got, want)
			}
		})
	}
}

// TestChromedpImporterPollBankItemCount_Table covers symptom 2 directly: a
// bankItemCountMatchesJS poll timeout retries by re-navigating fresh to the
// bank and rechecking, instead of failing on the first 30s window. Tested as
// a standalone unit (rather than through Import()) because the mocked `run`
// field never populates chromedp.Evaluate's out-parameter, so Import()'s
// final numeric question-count comparison can't be exercised this way — same
// reasoning as evaluateBool's doc comment.
func TestChromedpImporterPollBankItemCount_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name          string
		pollFailCount int // number of leading poll attempts that time out
		wantErr       string
		expectedCalls int
	}{
		{name: "succeeds first try", pollFailCount: 0, expectedCalls: 2},
		{name: "succeeds after one retry", pollFailCount: 1, expectedCalls: 4},
		{name: "succeeds after two retries", pollFailCount: 2, expectedCalls: 6},
		{name: "exhausts retries and fails", pollFailCount: 3, wantErr: "waiting for function failed: timeout", expectedCalls: 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pollAttempts := 0
			calls := 0
			importer := ChromedpImporter{
				run: func(_ context.Context, actions ...chromedp.Action) error {
					calls++
					// runResilient always calls run with a single chromedp.Action, so
					// len(actions) can't distinguish the re-navigate batch (a
					// chromedp.Tasks bundling 5 sub-actions) from the lone Poll call;
					// type-assert instead.
					if _, isNavigate := actions[0].(chromedp.Tasks); !isNavigate {
						pollAttempts++
						if pollAttempts <= tt.pollFailCount {
							return errors.New("waiting for function failed: timeout")
						}
					}
					return nil
				},
			}
			_, err := importer.pollBankItemCount(t.Context(), importer.run, "https://canvas.example.edu/courses/7/banks", "Bank", "", 3)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("pollBankItemCount() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("pollBankItemCount() error = %v, want %q", err, tt.wantErr)
			}
			if calls != tt.expectedCalls {
				t.Fatalf("browser action batches = %d, want %d", calls, tt.expectedCalls)
			}
		})
	}
}

// TestChromedpImporterImport_AbortsOnUnknownBaseline covers a bug found in
// review: an unverifiable baseline on an existing bank must not silently
// fall back to a fresh-bank expected total (which can pass or fail
// incorrectly depending on whether the totals happen to coincide). Import()
// must abort before ever uploading the package.
func TestChromedpImporterImport_AbortsOnUnknownBaseline(t *testing.T) {
	t.Parallel()
	calls := 0
	importer := ChromedpImporter{
		run:      func(context.Context, ...chromedp.Action) error { calls++; return nil },
		findBank: func(context.Context, string) (bool, error) { return true, nil }, // existing bank
		// The mocked run field never populates chromedp.Evaluate's
		// out-parameter, so the "open bank" click (routed through
		// evaluateBool) needs this hook to succeed and reach the baseline
		// read below.
		evalBool: func(context.Context, string) (bool, error) { return true, nil },
		// Two disagreeing reads make stableBankItemCount report unknown.
		bankItemCount: func() func(context.Context) (int, error) {
			reads := 0
			return func(context.Context) (int, error) {
				reads++
				return reads, nil // 1, then 2: never agree
			}
		}(),
	}
	_, err := importer.Import(t.Context(), &Request{
		BaseURL: "https://canvas.example.edu", BrowserURL: "http://127.0.0.1:9222", CourseID: "7",
		BankName: "Bank", Package: "quiz.zip", OnExisting: ExistingAppend, ExpectedItemCount: 3,
	})
	if err == nil || !strings.Contains(err.Error(), "could not read existing Item Bank") {
		t.Fatalf("Import() error = %v, want baseline-unknown error", err)
	}
	// Aborts right after the failed baseline read (navigate, wait for
	// actions, settle sleep between the two stabilizing reads — the open-bank
	// click and both bankItemCount reads go through hooks, not run()), before
	// ever opening the import actions menu or touching the upload dialog.
	if calls != 3 {
		t.Fatalf("browser action batches = %d, want 3 (aborted before upload)", calls)
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
			bankItemCountCalls := 0
			importer := ChromedpImporter{
				run:       func(context.Context, ...chromedp.Action) error { calls++; return nil },
				findBank:  func(context.Context, string) (bool, error) { return true, nil },
				bankTitle: func(context.Context) (string, error) { return tt.title, tt.titleErr },
				// The mocked run field never populates chromedp.Evaluate's
				// out-parameter, so every evaluateBool-routed click (open
				// bank, popover trigger, menu item, Import button) needs this
				// hook to succeed and reach the metadata checks below.
				evalBool: func(context.Context, string) (bool, error) { return true, nil },
				// The first two calls are stableBankItemCount's paired pre-upload
				// baseline read (it reads twice and requires agreement); this test
				// models a bank with no pre-existing content, so both return 0. The
				// final single read (the third call) is the expected total
				// unmodified.
				bankItemCount: func(context.Context) (int, error) {
					bankItemCountCalls++
					if bankItemCountCalls <= 2 {
						return 0, nil
					}
					return tt.count, tt.countErr
				},
				location: func(context.Context) (string, error) { return "https://canvas.example.edu/courses/7/banks/42", nil },
			}
			result, err := importer.Import(t.Context(), &Request{BaseURL: "https://canvas.example.edu", BrowserURL: "http://127.0.0.1:9222", CourseID: "7", BankName: "Bank", Package: "quiz.zip", OnExisting: ExistingAppend, ExpectedBankName: tt.expectedTitle, ExpectedItemCount: tt.expectedCount})
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Import() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Import() error = %v, want %q", err, tt.wantErr)
			}
			// open banks, wait for Item Bank actions, baseline settle sleep,
			// wait for import menu, attach package, wait for Import button
			// enabled, wait for import completion — every click in between
			// (open bank, popover trigger, menu item, Import button) goes
			// through the evalBool hook above, not run().
			if calls != 7 {
				t.Fatalf("browser action batches = %d, want 7", calls)
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
				// The mocked run field never populates chromedp.Evaluate's
				// out-parameter, so every evaluateBool-routed click (open
				// bank, popover trigger, menu item, Import button) needs this
				// hook to succeed and reach the rename logic under test.
				evalBool: func(context.Context, string) (bool, error) { return true, nil },
				renameBank: func(_ context.Context, oldTitle, newTitle string) error {
					renameCalled = true
					gotOld, gotNew = oldTitle, newTitle
					return tt.renameErr
				},
				location: func(context.Context) (string, error) { return "https://canvas.example.edu/courses/7/banks/42", nil },
			}
			result, err := importer.Import(t.Context(), &Request{
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
			_, err := (ChromedpImporter{}).Import(t.Context(), tt.req)
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
		name           string
		failAt         int
		missFirstClick bool // evalBool's first call (the open-edit-dialog click/poll) reports not-found.
		expectedCalls  int
		wantErr        string
	}{
		{name: "success", expectedCalls: 5},
		{name: "open banks", failAt: 1, expectedCalls: 1, wantErr: "open Item Banks"},
		// The open-edit-dialog click now polls (see
		// TestRenameBankUI_ReopenChecksBeforeClicking's doc comment for why:
		// same render race as Import()'s "Create Bank" button), and a poll's
		// wait/retry loop isn't observable through the mocked run field
		// (which just returns nil regardless of what Action it's given) —
		// only the evalBool hook can report a poll miss, so this case can no
		// longer be exercised by leaving the hook nil.
		{name: "open edit dialog click miss", missFirstClick: true, expectedCalls: 1, wantErr: "open edit bank dialog"},
		{name: "wait for field", failAt: 2, expectedCalls: 2, wantErr: "wait for bank-name field"},
		{name: "set name", failAt: 3, expectedCalls: 3, wantErr: "set bank name"},
		{name: "save sleep", failAt: 4, expectedCalls: 4, wantErr: "save bank name"},
		{name: "reopen navigate", failAt: 5, expectedCalls: 5, wantErr: "reopen renamed Item Bank"},
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
			// The mocked run field never populates chromedp.Evaluate's
			// out-parameter, so every click (open edit dialog, save, reopen)
			// needs this hook to succeed; run() still controls the plain
			// WaitVisible/SendKeys/Sleep/Navigate steps by call index.
			first := true
			evalBool := func(context.Context, string) (bool, error) {
				if tt.missFirstClick && first {
					first = false
					return false, nil
				}
				return true, nil
			}
			err := renameBankUI(t.Context(), run, evalBool, "https://canvas.example.edu/courses/7/banks", "Old Name", "New Name")
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

// TestRenameBankUI_ReopenChecksBeforeClicking covers the fix for renameBankUI
// re-opening the just-renamed bank with a blind Sleep+click: it now checks
// (via evaluatePollBoolViaRun in production — see
// TestChromedpImporterEvaluatePollBool_Table for that poll's own
// timeout-mapping behavior) that the new title is actually present before
// attempting the click, and reports a clear "not found" error — not a
// generic click-miss error — when that check never sees it. This test hooks
// evalBool directly, so it exercises the check-before-click wiring as a
// one-shot, not the underlying poll's retry/timeout behavior.
func TestRenameBankUI_ReopenChecksBeforeClicking(t *testing.T) {
	t.Parallel()
	run := func(context.Context, ...chromedp.Action) error { return nil }
	evalBool := func(_ context.Context, js string) (bool, error) {
		if js == bankExistsJS(jsString("New Name")) {
			return false, nil // the reopen check never sees the renamed bank appear.
		}
		return true, nil
	}
	err := renameBankUI(t.Context(), run, evalBool, "https://canvas.example.edu/courses/7/banks", "Old Name", "New Name")
	if err == nil || !strings.Contains(err.Error(), `reopen renamed Item Bank: "New Name" not found in bank list`) {
		t.Fatalf("renameBankUI() error = %v, want a reopen-not-found error", err)
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

func TestBankGroupPresentJS(t *testing.T) {
	t.Parallel()
	if !strings.Contains(bankGroupPresentJS, "Edit Bank containing questions") {
		t.Fatalf("bankGroupPresentJS = %q, want it to check for the per-group edit control", bankGroupPresentJS)
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
				// The mocked run field never populates chromedp.Evaluate's
				// out-parameter, so every evaluateBool-routed click (open
				// bank, popover trigger, menu item, Import button) needs this
				// hook to succeed and reach a normal Import() completion.
				evalBool: func(context.Context, string) (bool, error) { return true, nil },
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

// TestSelectNewQuizEngineJS is a golden-string test for selectNewQuizEngineJS
// — F5 notes this JS previously had zero test coverage at all, despite being
// the site of the one genuine (non-timing) logic bug fixed in this whole
// diff: an exact-equality `=== 'new quizzes'` match that could never match
// Canvas's real label "New Quizzes/Surveys". Asserts the fix directly
// (case-insensitive startsWith against the lowercase target, not includes or
// exact equality — includes would risk false-matching an unrelated label that
// merely mentions "new quizzes" elsewhere in its text), plus F5's own
// three-outcome contract: the JS must return the literal strings 'no_dialog',
// 'clicked_no_submit', and 'submitted' (not a bare boolean) so the call site
// can tell "no dialog present" (not an error) apart from "radio clicked but
// never actually submitted" (a real error — see interpretEngineChoiceResult).
func TestSelectNewQuizEngineJS(t *testing.T) {
	t.Parallel()
	if !strings.Contains(selectNewQuizEngineJS, "startsWith('new quizzes')") {
		t.Fatalf("selectNewQuizEngineJS = %q, want a case-insensitive startsWith('new quizzes') match", selectNewQuizEngineJS)
	}
	if strings.Contains(selectNewQuizEngineJS, "=== 'new quizzes'") || strings.Contains(selectNewQuizEngineJS, `=== "new quizzes"`) {
		t.Fatalf("selectNewQuizEngineJS = %q, must not use exact equality against 'new quizzes' (never matches Canvas's real \"New Quizzes/Surveys\" label)", selectNewQuizEngineJS)
	}
	for _, want := range []string{"'no_dialog'", "'clicked_no_submit'", "'submitted'"} {
		if !strings.Contains(selectNewQuizEngineJS, want) {
			t.Fatalf("selectNewQuizEngineJS = %q, want it to return the literal %s", selectNewQuizEngineJS, want)
		}
	}
}

// TestInterpretEngineChoiceResult_Table covers F5's three-outcome handling
// for CreateRandomQuiz's "select New Quizzes if prompted" step: "no_dialog"
// (Canvas didn't prompt at all — not an error) and "submitted" (radio clicked
// and submit/continue also clicked — success) are both fine; only
// "clicked_no_submit" (the radio was found and clicked but no submit/
// continue control was found afterward, leaving the dialog half-interacted)
// must be a clear, specific error rather than silently falling through into
// the unbounded WaitVisible(quizTitleSelector) that follows this step. An
// empty/unrecognized value (what a test run mock leaves outcome as, since it
// never populates chromedp.Evaluate's out-parameter) is treated permissively
// as success, matching this step's pre-existing tolerant "dialog absent"
// behavior and keeping TestChromedpImporterCreateRandomQuiz_Table's run-call
// counts unaffected by this change.
func TestInterpretEngineChoiceResult_Table(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, outcome, wantErr string
	}{
		{name: "no dialog present", outcome: "no_dialog"},
		{name: "radio clicked and submitted", outcome: "submitted"},
		{name: "radio clicked but no submit button found", outcome: "clicked_no_submit", wantErr: "no submit/continue button found"},
		{name: "empty (test mock never populates the out-parameter)", outcome: ""},
		{name: "unrecognized value treated permissively", outcome: "something_else"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := interpretEngineChoiceResult(tt.outcome)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("interpretEngineChoiceResult(%q) error = %v, want nil", tt.outcome, err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("interpretEngineChoiceResult(%q) error = %v, want %q", tt.outcome, err, tt.wantErr)
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
		name         string
		count        int
		failAt       int // fails exactly this one call.
		failFrom     int // fails this call and every call after it.
		locationErr  error
		locationURL  string
		collision    bool
		collisionErr error
		// evalBoolFailOn/evalBoolErrOn match a substring of the JS passed to
		// evaluateBool; when matched the mock evalBool returns (false, nil)
		// or (false, err) respectively instead of the (true, nil) default.
		evalBoolFailOn string
		evalBoolErrOn  string
		wantErr        string
		wantURL        string
		wantTitle      string
		wantEmptyURL   bool
		expectedCalls  int
	}{
		{name: "append title", count: 3, wantURL: "https://canvas.example.edu/courses/7/quizzes/9", wantTitle: "Bank Quiz", expectedCalls: 19},
		{name: "existing quiz suffix", count: 2, wantURL: "https://canvas.example.edu/courses/7/quizzes/9", wantTitle: "Bank Quiz", expectedCalls: 19},
		{name: "invalid request", wantErr: "request is required"},
		{name: "invalid count", count: 0, wantErr: "must be positive"},
		{name: "invalid base URL", count: 2, wantErr: "invalid Canvas base URL"},
		{name: "open quizzes", count: 2, failAt: 1, expectedCalls: 1, wantErr: "open Quizzes"},
		{name: "set title", count: 2, failAt: 6, expectedCalls: 6, wantErr: "set quiz title"},
		// "Build" is now clicked via evaluateBool (JS dispatch), not a plain
		// run() call, so its miss is injected the same way as the other
		// InstUI click sites below (evalBoolFailOn), not failAt.
		{name: "build quiz", count: 2, evalBoolFailOn: "build", expectedCalls: 6, wantErr: "build quiz"},
		{name: "add bank not enabled", count: 2, failAt: 10, expectedCalls: 10, wantErr: "add Item Bank to quiz"},
		{name: "bank group never appears", count: 2, failAt: 12, expectedCalls: 12, wantErr: "wait for bank group to appear"},
		{name: "edit control missing", count: 2, evalBoolFailOn: "Edit Bank containing questions", expectedCalls: 12, wantErr: "Edit Bank containing questions"},
		{name: "random option missing", count: 2, evalBoolFailOn: "randomly select questions", expectedCalls: 13, wantErr: "Randomly select questions"},
		{name: "question count field missing", count: 2, evalBoolFailOn: "Number of questions", expectedCalls: 14, wantErr: "Number of questions"},
		{name: "evalBool error propagates", count: 2, evalBoolErrOn: "Edit Bank containing questions", expectedCalls: 12, wantErr: "evalBool boom"},
		{name: "random option evalBool error", count: 2, evalBoolErrOn: "randomly select questions", expectedCalls: 13, wantErr: "evalBool boom"},
		{name: "question count evalBool error", count: 2, evalBoolErrOn: "Number of questions", expectedCalls: 14, wantErr: "evalBool boom"},
		// "Done" is likewise clicked via evaluateBool now.
		{name: "save group", count: 2, evalBoolFailOn: "done", expectedCalls: 15, wantErr: "save Item Bank group"},
		{name: "verify group", count: 2, failAt: 16, expectedCalls: 16, wantErr: "verify random group"},
		{name: "read URL", count: 2, locationErr: errors.New("location unavailable"), expectedCalls: 16, wantErr: "read quiz URL"},
		{name: "empty quiz URL", count: 2, locationURL: "  ", expectedCalls: 16, wantErr: "quiz URL is empty", wantEmptyURL: true},
		{name: "persistence not confirmed", count: 2, failFrom: 17, expectedCalls: 19, wantErr: "did not persist", wantEmptyURL: true},
		{name: "persistence retries then succeeds", count: 3, failAt: 17, wantURL: "https://canvas.example.edu/courses/7/quizzes/9", wantTitle: "Bank Quiz", expectedCalls: 20},
		{name: "persistence bank-group check fails then retries succeed", count: 3, failAt: 18, wantURL: "https://canvas.example.edu/courses/7/quizzes/9", wantTitle: "Bank Quiz", expectedCalls: 21},
		{name: "persistence random-group check fails then retries succeed", count: 3, failAt: 19, wantURL: "https://canvas.example.edu/courses/7/quizzes/9", wantTitle: "Bank Quiz", expectedCalls: 22},
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
					if tt.failAt != 0 && calls == tt.failAt {
						return errors.New("browser failed")
					}
					if tt.failFrom != 0 && calls >= tt.failFrom {
						return errors.New("browser failed")
					}
					return nil
				},
				quizLocation: func(context.Context) (string, error) {
					url := "https://canvas.example.edu/courses/7/quizzes/9"
					if tt.locationURL != "" {
						url = tt.locationURL
					}
					return url, tt.locationErr
				},
				quizExists: func(context.Context, string) (bool, error) { return tt.collision, tt.collisionErr },
				evalBool: func(_ context.Context, js string) (bool, error) {
					lower := strings.ToLower(js)
					if tt.evalBoolErrOn != "" && strings.Contains(lower, strings.ToLower(tt.evalBoolErrOn)) {
						return false, errors.New("evalBool boom")
					}
					if tt.evalBoolFailOn != "" && strings.Contains(lower, strings.ToLower(tt.evalBoolFailOn)) {
						return false, nil
					}
					return true, nil
				},
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
			result, err := creator.CreateRandomQuiz(t.Context(), req)
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
			if tt.wantEmptyURL && result.QuizURL != "" {
				t.Fatalf("CreateRandomQuiz() result.QuizURL = %q, want empty on failure", result.QuizURL)
			}
		})
	}
}

func TestChromedpImporterEvaluateBool_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		runErr  error
		wantErr string
	}{
		{name: "no evalBool hook, run succeeds"},
		{name: "no evalBool hook, run fails", runErr: errors.New("run boom"), wantErr: "run boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := ChromedpImporter{}
			run := func(context.Context, ...chromedp.Action) error { return tt.runErr }
			_, err := c.evaluateBool(t.Context(), run, "true")
			if tt.wantErr == "" && err != nil {
				t.Fatalf("evaluateBool() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("evaluateBool() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestChromedpImporterEvaluatePollBool_Table covers evaluatePollBool's own
// production-path logic (as opposed to Import()'s use of it): isTimeoutLike
// mapping a plain timeout to a clean (false, nil) — matching a one-shot
// evaluateBool call that simply found nothing — versus a genuine non-timeout
// error surfacing as-is, plus the trivial found-immediately case. Exercises
// the no-hook (production) path directly via a mocked run field, mirroring
// TestChromedpImporterEvaluateBool_Table above.
func TestChromedpImporterEvaluatePollBool_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		runErr  error
		ctxDone bool // caller's own outer context is already done when the poll gives up.
		want    bool
		wantErr string
	}{
		{name: "found immediately", want: true},
		{name: "timeout maps to not-found, not an error", runErr: errors.New("waiting for function failed: timeout"), want: false},
		{name: "context deadline exceeded also maps to not-found when outer ctx is still live", runErr: errors.New("context deadline exceeded"), want: false},
		{name: "non-timeout error surfaces as-is", runErr: errors.New("evaluate boom"), want: false, wantErr: "evaluate boom"},
		// F1: when the poll gives up because the CALLER's own outer budget
		// (e.g. Import()'s 150s workCancel deadline) expired mid-poll — not
		// this poll's own WithPollingTimeout — ctx itself is already done at
		// that point. That must NOT collapse to the same clean (false, nil)
		// "confirmed absent" result a genuine poll timeout gets: a caller
		// can't otherwise tell "genuinely not found" apart from "ran out of
		// time entirely", which produced a live, misleading "Item Bank was
		// not found" error that was actually just the outer budget expiring.
		{name: "outer ctx already done when poll gives up is a real error, not a clean not-found", runErr: errors.New("waiting for function failed: timeout"), ctxDone: true, want: false, wantErr: "context ended while polling"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := ChromedpImporter{}
			ctx := t.Context()
			if tt.ctxDone {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			run := func(context.Context, ...chromedp.Action) error { return tt.runErr }
			got, err := c.evaluatePollBool(ctx, run, "true", time.Second)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("evaluatePollBool() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("evaluatePollBool() error = %v, want %q", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("evaluatePollBool() = %v, want %v", got, tt.want)
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
			err := creator.PreflightRandomQuiz(t.Context(), req)
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
