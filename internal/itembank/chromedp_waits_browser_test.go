// Whitebox tests run the condition-wait predicates that replaced the fixed
// settle sleeps against fixture DOMs in a real headless Chrome, including
// dynamic fixtures whose DOM changes after a short in-page setTimeout so the
// poll genuinely transitions false -> true.
package itembank

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// flipAfter is the in-page delay before a dynamic fixture without poll counting mutates its DOM.
// It is only a fixture timer inside the page; the Go side always waits on the
// poll itself, never on a sleep.
const flipAfter = "300"

// afterPolls returns a script that runs js only once the predicate under test
// has been evaluated at least twice (pollPredicate counts evaluations in
// window.__n), so a dynamic fixture's DOM change is guaranteed to happen
// after a false evaluation regardless of machine load.
func afterPolls(js string) string {
	return "<script>var __t=setInterval(function(){if((window.__n||0)>=2){clearInterval(__t);" + js + "}},20)</script>"
}

// afterDelay returns a script that runs js after flipAfter milliseconds, for
// fixtures whose poll is not counted via pollPredicate.
func afterDelay(js string) string {
	return "<script>setTimeout(function(){" + js + "}," + flipAfter + ")</script>"
}

// shortPoll is the timeout for polls expected to fail: long enough for the
// dynamic fixtures' flip, short enough to keep negative cases quick.
const shortPoll = 1500 * time.Millisecond

// fixtures is an httptest server that serves, for any path, a page the test
// registered earlier via fixtureURL (looked up by an opaque id, never echoing
// request input), so a test controls both the origin and the path it needs.
type fixtures struct {
	*httptest.Server
	mu    sync.Mutex
	pages map[string]string
}

func fixtureServer(t *testing.T) *fixtures {
	t.Helper()
	f := &fixtures{pages: map[string]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		page := f.pages[r.URL.Query().Get("id")]
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, page)
	}))
	t.Cleanup(f.Close)
	return f
}

// fixtureURL registers html on srv and returns its URL under path.
func fixtureURL(srv *fixtures, path, html string) string {
	srv.mu.Lock()
	id := strconv.Itoa(len(srv.pages))
	srv.pages[id] = html
	srv.mu.Unlock()
	return srv.URL + path + "?id=" + id
}

// newTab returns a fresh bounded tab context in the shared browser.
func newTab(t *testing.T, browserCtx context.Context) context.Context {
	t.Helper()
	tabCtx, tabCancel := chromedp.NewContext(browserCtx)
	t.Cleanup(tabCancel)
	ctx, cancel := context.WithTimeout(tabCtx, 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// pollPredicate navigates to pageURL and polls expr, counting evaluations in
// window.__n so a caller can tell whether the predicate was ever false first.
func pollPredicate(ctx context.Context, pageURL, expr string, timeout time.Duration) (int, error) {
	counted := "(window.__n = (window.__n || 0) + 1, (" + expr + "))"
	if err := chromedp.Do(ctx, chromedp.Navigate(pageURL), chromedp.WaitReady(chromedp.CSS("body"))); err != nil {
		return 0, err
	}
	pollErr := chromedp.Do(ctx, chromedp.Poll[chromedp.Void](counted, chromedp.WithPollingTimeout(timeout)))
	var n int
	if err := chromedp.Do(ctx, evaluateInto(`window.__n || 0`, &n)); err != nil {
		return 0, err
	}
	return n, pollErr
}

type predicateCase struct {
	name     string
	html     string
	path     string // defaults to "/"
	wantPass bool
	wantFlip bool // the predicate must have been false at least once before passing
}

func runPredicateCases(t *testing.T, expr string, cases []predicateCase) {
	t.Helper()
	browserCtx := startBankChrome(t)
	srv := fixtureServer(t)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := tt.path
			if path == "" {
				path = "/"
			}
			polls, err := pollPredicate(newTab(t, browserCtx), fixtureURL(srv, path, tt.html), expr, shortPoll)
			if tt.wantPass && err != nil {
				t.Fatalf("predicate %q failed on %q: %v", expr, tt.name, err)
			}
			if !tt.wantPass && (err == nil || !isTimeoutLike(err)) {
				t.Fatalf("predicate %q on %q: error = %v, want a poll timeout", expr, tt.name, err)
			}
			if tt.wantFlip && polls < 2 {
				t.Fatalf("predicate evaluated %d time(s), want it false at least once before passing", polls)
			}
		})
	}
}

func TestDialogClosedJS_Chrome(t *testing.T) {
	t.Parallel()
	runPredicateCases(t, dialogClosedJS, []predicateCase{
		{name: "no dialog", html: `<body><p>hi</p></body>`, wantPass: true},
		{name: "dialog open", html: `<body><div role="dialog">x</div></body>`},
		{name: "nested dialog stays open", html: `<body><div class="outer"><div role="dialog"><div role="dialog">inner</div></div></div></body>`},
		{name: "hidden but mounted dialog is not closed", html: `<body><div role="dialog" style="display:none">x</div></body>`},
		{name: "dialog removed after delay", html: `<body><div id="d" role="dialog">x</div>` + afterPolls(`document.getElementById('d').remove()`) + `</body>`, wantPass: true, wantFlip: true},
		{name: "outer closes but nested remains", html: `<body><div id="d" role="dialog">x</div><div role="dialog">other</div>` + afterPolls(`document.getElementById('d').remove()`) + `</body>`},
	})
}

func TestBankDetailRouteJS_Chrome(t *testing.T) {
	t.Parallel()
	runPredicateCases(t, bankDetailRouteJS, []predicateCase{
		{name: "detail route", path: "/courses/7/banks/42", html: `<body>x</body>`, wantPass: true},
		{name: "detail route with query", path: "/courses/7/banks/42", html: `<body>x</body>`, wantPass: true},
		{name: "list route", path: "/courses/7/banks", html: `<body>x</body>`},
		{name: "list route trailing slash", path: "/courses/7/banks/", html: `<body>x</body>`},
		{name: "other route", path: "/courses/7/quizzes/9", html: `<body>x</body>`},
		{name: "route named like banks", path: "/courses/7/bankstatements", html: `<body>x</body>`},
		{name: "client-side route change after delay", path: "/courses/7/banks", html: `<body>x` + afterPolls(`history.pushState({}, '', '/courses/7/banks/42')`) + `</body>`, wantPass: true, wantFlip: true},
	})
}

func TestLoginSucceededJS_Chrome(t *testing.T) {
	t.Parallel()
	browserCtx := startBankChrome(t)
	canvas := fixtureServer(t)
	idp := fixtureServer(t)
	const loginForm = `<body><form><input id="username"><input id="password"></form></body>`
	const interstitial = `<body><p>Redirecting you to Canvas...</p></body>`
	const appPage = `<body><div id="application">Dashboard</div></body>`
	for _, tt := range []struct {
		name     string
		pageURL  string
		origin   string
		wantPass bool
		wantFlip bool
	}{
		{name: "real post-login Canvas page", pageURL: fixtureURL(canvas, "/", appPage), origin: canvas.URL, wantPass: true},
		{name: "Canvas still showing login form", pageURL: fixtureURL(canvas, "/login", loginForm), origin: canvas.URL},
		{name: "SSO interstitial on the IdP origin lacks #username but is not Canvas", pageURL: fixtureURL(idp, "/sso/redirect", interstitial), origin: canvas.URL},
		{name: "IdP login form", pageURL: fixtureURL(idp, "/sso", loginForm), origin: canvas.URL},
		{name: "empty origin falls back to form absence", pageURL: fixtureURL(idp, "/sso/redirect", interstitial), origin: "", wantPass: true},
		{name: "login form removed after delay", pageURL: fixtureURL(canvas, "/login", `<body><form id="f"><input id="username"></form>`+afterPolls(`document.getElementById('f').remove()`)+`</body>`), origin: canvas.URL, wantPass: true, wantFlip: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			polls, err := pollPredicate(newTab(t, browserCtx), tt.pageURL, loginSucceededJS(tt.origin), shortPoll)
			if tt.wantPass && err != nil {
				t.Fatalf("loginSucceededJS(%q) failed: %v", tt.origin, err)
			}
			if !tt.wantPass && (err == nil || !isTimeoutLike(err)) {
				t.Fatalf("loginSucceededJS(%q) error = %v, want a poll timeout", tt.origin, err)
			}
			if tt.wantFlip && polls < 2 {
				t.Fatalf("predicate evaluated %d time(s), want it false at least once", polls)
			}
		})
	}
}

func TestRadioLabelPresentJS_Chrome(t *testing.T) {
	t.Parallel()
	runPredicateCases(t, radioLabelPresentJS("Randomly select questions"), []predicateCase{
		{name: "label present", html: `<body><label>Randomly select questions</label></body>`, wantPass: true},
		{name: "case-insensitive and padded", html: `<body><label>  RANDOMLY SELECT QUESTIONS from bank </label></body>`, wantPass: true},
		{name: "other label only", html: `<body><label>Manually select questions</label></body>`},
		{name: "same text outside a label does not count", html: `<body><span>Randomly select questions</span><div>Randomly select questions</div></body>`},
		{name: "no labels", html: `<body></body>`},
		{name: "label appears after delay", html: `<body>` + afterPolls(`var l=document.createElement('label');l.textContent='Randomly select questions';document.body.appendChild(l)`) + `</body>`, wantPass: true, wantFlip: true},
	})
}

func TestMarkQuestionCountInputJS_Chrome(t *testing.T) {
	t.Parallel()
	runPredicateCases(t, markQuestionCountInputJS(), []predicateCase{
		{name: "span with single input", html: `<body><div><span>Number of questions</span><input></div></body>`, wantPass: true},
		{name: "input in an ancestor container", html: `<body><div><div><span>Number of questions</span></div><input></div></body>`, wantPass: true},
		{name: "label without input", html: `<body><div><span>Number of questions</span></div></body>`},
		{name: "ambiguous: two inputs in every ancestor", html: `<body><div><span>Number of questions</span><input><input></div></body>`},
		{name: "different label", html: `<body><div><span>Points</span><input></div></body>`},
		{name: "field appears after delay", html: `<body><div id="p"></div>` + afterPolls(`document.getElementById('p').innerHTML='<span>Number of questions</span><input>'`) + `</body>`, wantPass: true, wantFlip: true},
	})
}

// listFixture is a Canvas-shaped Item Banks list with one body row.
func listFixture(name, count, script string) string {
	return bankListPage([]string{"Name", "Updated", "Questions", "Last Used", "Created", ""}, bankListRowFor(name, count)) + script
}

// openableListFixture is listFixture whose name button behaves like Canvas's:
// clicking it client-side routes to /banks/42 and renders the detail page's
// actions button, so pollBankItemCount's open-bank step can complete.
func openableListFixture(name, count, script string) string {
	open := `history.pushState({}, '', '/courses/7/banks/42');var b=document.createElement('button');b.setAttribute('aria-haspopup','true');b.textContent='Actions';document.body.appendChild(b)`
	row := bankListRow(`<button onclick="`+open+`">`+name+`</button>`, "Jan 1", count, "-", "Jan 1", "")
	return bankListPage([]string{"Name", "Updated", "Questions", "Last Used", "Created", ""}, row) + script
}

func TestWaitBankRowReady_Chrome(t *testing.T) {
	t.Parallel()
	browserCtx := startBankChrome(t)
	srv := fixtureServer(t)
	for _, tt := range []struct {
		name              string
		html              string
		primary, fallback string
		wantTimeout       bool
	}{
		{name: "row rendered", html: listFixture("Bank", "12", ""), primary: "Bank"},
		{name: "row under fallback name", html: listFixture("Renamed", "9", ""), primary: "Bank", fallback: "Renamed"},
		{name: "row missing times out", html: listFixture("Other", "3", ""), primary: "Bank", wantTimeout: true},
		{name: "header button alone never counts", html: bankListPage([]string{"Name", "Updated", `<button>Questions</button>`, "Last Used", "Created", ""}), primary: "Questions", wantTimeout: true},
		{name: "row hydrates after delay", html: `<body><div id="m"></div>` + afterDelay(`document.getElementById('m').innerHTML=`+"`"+bankListPage([]string{"Name", "Updated", "Questions", "Last Used", "Created", ""}, bankListRowFor("Bank", "76"))+"`"+``) + `</body>`, primary: "Bank"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := newTab(t, browserCtx)
			err := chromedp.Do(ctx,
				chromedp.Navigate(fixtureURL(srv, "/courses/7/banks", tt.html)),
				chromedp.WaitReady(chromedp.CSS("body")),
				waitBankRowReady(tt.primary, tt.fallback, shortPoll),
			)
			if tt.wantTimeout && (err == nil || !isTimeoutLike(err)) {
				t.Fatalf("waitBankRowReady() error = %v, want a poll timeout", err)
			}
			if !tt.wantTimeout && err != nil {
				t.Fatalf("waitBankRowReady() error = %v", err)
			}
		})
	}
}

// TestPollBankItemCount_Chrome drives the real pollBankItemCount (real
// chromedp.Do, real list-row predicate) against list fixtures, replacing the
// reflection-based predicate check the generic Poll API made impossible. Only
// matching totals are exercised here because a mismatch deliberately burns
// the full retry budget (3 x 30s).
func TestPollBankItemCount_Chrome(t *testing.T) {
	t.Parallel()
	browserCtx := startBankChrome(t)
	srv := fixtureServer(t)
	hydrate := func(count string) string {
		return afterDelay(`document.querySelector('tbody td:nth-child(3)').textContent='` + count + `'`)
	}
	for _, tt := range []struct {
		name              string
		html              string
		primary, fallback string
		expected          int
	}{
		{name: "76-question bank reads the list total", html: openableListFixture("Bank", "76", ""), primary: "Bank", expected: 76},
		{name: "fallback name", html: openableListFixture("Chapter 1 Quiz", "51", ""), primary: "Chapter 1", fallback: "Chapter 1 Quiz", expected: 51},
		{name: "count lags then matches", html: openableListFixture("Bank", "3", hydrate("76")), primary: "Bank", expected: 76},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := newTab(t, browserCtx)
			banksURL := fixtureURL(srv, "/courses/7/banks", tt.html)
			if err := chromedp.Do(ctx, chromedp.Navigate(banksURL), chromedp.WaitReady(chromedp.CSS("body"))); err != nil {
				t.Fatalf("navigate fixture: %v", err)
			}
			got, err := ChromedpImporter{}.pollBankItemCount(ctx, chromedp.Do, banksURL, tt.primary, tt.fallback, tt.expected)
			if err != nil {
				t.Fatalf("pollBankItemCount() error = %v", err)
			}
			if got != tt.expected {
				t.Fatalf("pollBankItemCount() = %d, want %d", got, tt.expected)
			}
			if strings.Contains(bankListCountMatchesJS(tt.primary, tt.fallback, tt.expected), "innerText.match") {
				t.Fatal("predicate must not use the old detail-page card regex")
			}
		})
	}
}
