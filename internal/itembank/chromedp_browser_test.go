// Whitebox tests run the in-page JS builders in a real headless Chrome.
package itembank

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// findChromeBinary returns a Chrome/Chromium executable, or "" when none is
// installed, so callers can t.Skip like the pandoc/mmdc-backed tests do.
func findChromeBinary() string {
	for _, p := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// bankListRow builds a <tr> from raw cell HTML.
func bankListRow(cells ...string) string {
	var b strings.Builder
	b.WriteString("<tr>")
	for _, c := range cells {
		b.WriteString("<td>" + c + "</td>")
	}
	b.WriteString("</tr>")
	return b.String()
}

// bankListRowFor models a real Canvas list row: the name button first, then
// Updated, the count, Last Used, Created, and a trailing actions cell.
func bankListRowFor(name, count string) string {
	return bankListRow("<button>"+name+"</button>", "Jan 1", count, "-", "Jan 1", "<button>Edit</button><button>Delete</button>")
}

// bankListPage builds a Canvas-style Item Banks list table document.
func bankListPage(headers []string, rows ...string) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><body><table><thead><tr>")
	for _, h := range headers {
		b.WriteString("<th>" + h + "</th>")
	}
	b.WriteString("</tr></thead><tbody>" + strings.Join(rows, "") + "</tbody></table></body></html>")
	return b.String()
}

// startBankChrome starts a shared headless Chrome for t, skipping when none is
// available or it cannot launch.
func startBankChrome(t *testing.T) context.Context {
	t.Helper()
	chromePath := findChromeBinary()
	if chromePath == "" {
		t.Skip("no Chrome/Chromium binary found; skipping in-browser execution of bank row helpers")
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(t.Context(), append(
		append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...),
		chromedp.ExecPath(chromePath), chromedp.NoSandbox)...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)
	if err := chromedp.Run(browserCtx); err != nil {
		t.Skipf("cannot launch Chrome at %s: %v", chromePath, err)
	}
	return browserCtx
}

// clickTrackedPage builds a bank list page whose "Questions" header holds a
// button, and whose buttons record which one was clicked into window.clicked:
// "header" for the header button, "row-<name>" for a row's name button. Rows
// are (name, count) pairs.
func clickTrackedPage(rows ...[2]string) string {
	hdr := `<button onclick="window.clicked='header'">Questions</button>`
	body := make([]string, 0, len(rows))
	for _, r := range rows {
		body = append(body, bankListRow(`<button onclick="window.clicked='row-`+r[0]+`'">`+r[0]+`</button>`, "Jan 1", r[1], "-", "Jan 1", ""))
	}
	return bankListPage([]string{"Name", "Updated", hdr, "Last Used", "Created", ""}, body...)
}

// collisionPage builds a bank list page where each row is (name, count, action
// label): the name button records "name-<name>", the action button in a later
// cell records "action-<label>". Rows with an empty label get no action.
func collisionPage(rows ...[3]string) string {
	body := make([]string, 0, len(rows))
	for _, r := range rows {
		action := ""
		if r[2] != "" {
			action = `<button onclick="window.clicked='action-` + r[2] + `'">` + r[2] + `</button>`
		}
		body = append(body, bankListRow(`<button onclick="window.clicked='name-`+r[0]+`'">`+r[0]+`</button>`, "Jan 1", r[1], "-", "Jan 1", action))
	}
	return bankListPage([]string{"Name", "Updated", "Questions", "Last Used", "Created", ""}, body...)
}

// TestBankRowFinders_Chrome runs bankListQuestionCountJS, clickBankRowJS and
// bankExistsJS against the same Canvas-shaped DOM, including a bank literally
// named "Questions" beside a header "Questions" button, proving count and
// open resolve the same body-row element.
func TestBankRowFinders_Chrome(t *testing.T) {
	t.Parallel()
	browserCtx := startBankChrome(t)
	for _, tt := range []struct {
		name              string
		page              string
		primary, fallback string
		wantCount         int
		wantExists        bool
		wantClicked       string // "" means nothing clicked
	}{
		{name: "bank named Questions beside header button", page: clickTrackedPage([2]string{"Questions", "12"}), primary: "Questions", wantCount: 12, wantExists: true, wantClicked: "row-Questions"},
		{name: "header button alone is not a bank", page: clickTrackedPage([2]string{"Other", "3"}), primary: "Questions", wantCount: -1, wantExists: false},
		{name: "header button only, no rows", page: clickTrackedPage(), primary: "Questions", wantCount: -1, wantExists: false},
		{name: "Chapter 1 exact", page: clickTrackedPage([2]string{"Chapter 11", "40"}, [2]string{"Chapter 1", "12"}), primary: "Chapter 1", wantCount: 12, wantExists: true, wantClicked: "row-Chapter 1"},
		{name: "Chapter 11 exact", page: clickTrackedPage([2]string{"Chapter 1", "12"}, [2]string{"Chapter 11", "40"}), primary: "Chapter 11", wantCount: 40, wantExists: true, wantClicked: "row-Chapter 11"},
		{name: "Chapter 1 absent when only Chapter 11", page: clickTrackedPage([2]string{"Chapter 11", "40"}), primary: "Chapter 1", wantCount: -1, wantExists: false},
		{name: "fallback name used", page: clickTrackedPage([2]string{"Renamed", "9"}), primary: "Bank", fallback: "Renamed", wantCount: 9, wantExists: false, wantClicked: "row-Renamed"},
		{name: "primary preferred over fallback", page: clickTrackedPage([2]string{"Renamed", "9"}, [2]string{"Bank", "5"}), primary: "Bank", fallback: "Renamed", wantCount: 5, wantExists: true, wantClicked: "row-Bank"},
		{name: "fallback Questions never header", page: clickTrackedPage([2]string{"Other", "1"}), primary: "Bank", fallback: "Questions", wantCount: -1, wantExists: false},
		{name: "collision: name matching another row's edit action", page: collisionPage([3]string{"Other", "5", "Edit bank Other"}, [3]string{"Edit bank Other", "9", ""}), primary: "Edit bank Other", wantCount: 9, wantExists: true, wantClicked: "name-Edit bank Other"},
		{name: "collision: plain name still hits its own row", page: collisionPage([3]string{"Other", "5", "Edit bank Other"}, [3]string{"Edit bank Other", "9", ""}), primary: "Other", wantCount: 5, wantExists: true, wantClicked: "name-Other"},
		{name: "collision: action-only match is not a bank", page: collisionPage([3]string{"Other", "5", "Edit bank Other"}), primary: "Edit bank Other", wantCount: -1, wantExists: false},
		{name: "table without thead skips first row", page: `<!doctype html><html><body><table><tr><th>Name</th><th>Updated</th><th>Questions</th></tr><tr><td><button onclick="window.clicked='row-Bank'">Bank</button></td><td>x</td><td>4</td></tr></table></body></html>`, primary: "Bank", wantCount: 4, wantExists: true, wantClicked: "row-Bank"},
		{name: "theadless header button skipped", page: `<!doctype html><html><body><table><tr><th><button onclick="window.clicked='header'">Questions</button></th></tr></table></body></html>`, primary: "Questions", wantCount: -1, wantExists: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tabCtx, tabCancel := chromedp.NewContext(browserCtx)
			defer tabCancel()
			ctx, cancel := context.WithTimeout(tabCtx, 60*time.Second)
			defer cancel()
			var count int
			var exists, clickedOK bool
			var clicked string
			err := chromedp.Run(ctx,
				chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(tt.page)),
				chromedp.WaitReady("body", chromedp.ByQuery),
				chromedp.Evaluate(bankListQuestionCountJS(tt.primary, tt.fallback), &count),
				chromedp.Evaluate(bankExistsJS(jsString(tt.primary)), &exists),
				chromedp.Evaluate(clickBankRowJS(tt.primary, tt.fallback), &clickedOK),
				chromedp.Evaluate(`window.clicked || ""`, &clicked),
			)
			if err != nil {
				t.Fatalf("running bank row helpers in Chrome: %v", err)
			}
			if count != tt.wantCount {
				t.Errorf("bankListQuestionCountJS(%q, %q) = %d, want %d", tt.primary, tt.fallback, count, tt.wantCount)
			}
			if exists != tt.wantExists {
				t.Errorf("bankExistsJS(%q) = %v, want %v", tt.primary, exists, tt.wantExists)
			}
			if clickedOK != (tt.wantClicked != "") {
				t.Errorf("clickBankRowJS(%q, %q) = %v, want %v", tt.primary, tt.fallback, clickedOK, tt.wantClicked != "")
			}
			if clicked != tt.wantClicked {
				t.Errorf("clickBankRowJS(%q, %q) clicked %q, want %q (never the header button)", tt.primary, tt.fallback, clicked, tt.wantClicked)
			}
			// Count-then-open: when the count resolved a row, the click must
			// have opened that same row.
			if (count >= 0) != (clicked != "") {
				t.Errorf("count %d but clicked %q: counting and opening resolved different elements", count, clicked)
			}
		})
	}
}

// TestBankListQuestionCountJS_Chrome executes the generated JS against
// Canvas-shaped list DOM in a real headless Chrome, the only coverage of the
// in-page logic itself (the _Table test only inspects the generated string).
// It skips when no Chrome binary is available.
func TestBankListQuestionCountJS_Chrome(t *testing.T) {
	t.Parallel()
	chromePath := findChromeBinary()
	if chromePath == "" {
		t.Skip("no Chrome/Chromium binary found; skipping in-browser execution of bankListQuestionCountJS")
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(t.Context(), append(
		append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...),
		chromedp.ExecPath(chromePath), chromedp.NoSandbox)...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)
	// Start the shared browser on browserCtx itself (not a derived timeout
	// ctx, whose cancel would tear the browser down); per-case tabs are
	// children of browserCtx.
	if err := chromedp.Run(browserCtx); err != nil {
		t.Skipf("cannot launch Chrome at %s: %v", chromePath, err)
	}

	std := []string{"Name", "Updated", "Questions", "Last Used", "Created", ""}
	withCheckbox := append([]string{`<input type="checkbox" aria-label="select all">`}, std...)
	checkboxRow := func(name, count string) string {
		return bankListRow(`<input type="checkbox">`, "<button>"+name+"</button>", "Jan 1", count, "-", "Jan 1", "<button>Edit</button>")
	}
	for _, tt := range []struct {
		name              string
		page              string
		primary, fallback string
		want              int
	}{
		{name: "76-row value", page: bankListPage(std, bankListRowFor("Bank", "76")), primary: "Bank", want: 76},
		{name: "Chapter 1 reads its own count", page: bankListPage(std, bankListRowFor("Chapter 11", "40"), bankListRowFor("Chapter 1", "12")), primary: "Chapter 1", want: 12},
		{name: "Chapter 11 reads its own count", page: bankListPage(std, bankListRowFor("Chapter 1", "12"), bankListRowFor("Chapter 11", "40")), primary: "Chapter 11", want: 40},
		{name: "comma value", page: bankListPage(std, bankListRowFor("Bank", "1,076")), primary: "Bank", want: 1076},
		{name: "fallback name used when primary missing", page: bankListPage(std, bankListRowFor("Renamed", "9")), primary: "Bank", fallback: "Renamed", want: 9},
		{name: "primary preferred over fallback", page: bankListPage(std, bankListRowFor("Renamed", "9"), bankListRowFor("Bank", "5")), primary: "Bank", fallback: "Renamed", want: 5},
		{name: "neither name present", page: bankListPage(std, bankListRowFor("Other", "3")), primary: "Bank", fallback: "Renamed", want: -1},
		{name: "leading checkbox column keeps alignment", page: bankListPage(withCheckbox, checkboxRow("Bank", "76")), primary: "Bank", want: 76},
		{name: "header with screen-reader text", page: bankListPage([]string{"Name", "Updated", `Questions <span>Sort ascending</span>`, "Last Used", "Created", ""}, bankListRowFor("Bank", "22")), primary: "Bank", want: 22},
		{name: "header matched case-insensitively", page: bankListPage([]string{"Name", "Updated", "QUESTIONS", "Last Used", "Created", ""}, bankListRowFor("Bank", "8")), primary: "Bank", want: 8},
		{name: "Questions column not third", page: bankListPage([]string{"Name", "Questions", "Updated"}, bankListRow("<button>Bank</button>", "31", "Jan 1")), primary: "Bank", want: 31},
		{name: "non-numeric placeholder", page: bankListPage(std, bankListRowFor("Bank", "-")), primary: "Bank", want: -1},
		{name: "digits with trailing text", page: bankListPage(std, bankListRowFor("Bank", "12abc")), primary: "Bank", want: -1},
		{name: "empty cell", page: bankListPage(std, bankListRowFor("Bank", "")), primary: "Bank", want: -1},
		{name: "bank named Questions, header button ignored", page: bankListPage([]string{"Name", "Updated", `<button>Questions</button>`, "Last Used", "Created", ""}, bankListRowFor("Questions", "5")), primary: "Questions", want: 5},
		{name: "header button alone never matches", page: bankListPage([]string{"Name", "Updated", `<button>Questions</button>`, "Last Used", "Created", ""}, bankListRowFor("Other", "5")), primary: "Questions", want: -1},
		{name: "empty list while loading", page: bankListPage(std), primary: "Bank", want: -1},
		{name: "no table yet", page: "<!doctype html><html><body><p>Loading</p></body></html>", primary: "Bank", want: -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tabCtx, tabCancel := chromedp.NewContext(browserCtx)
			defer tabCancel()
			ctx, cancel := context.WithTimeout(tabCtx, 60*time.Second)
			defer cancel()
			var got int
			err := chromedp.Run(ctx,
				chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(tt.page)),
				chromedp.WaitReady("body", chromedp.ByQuery),
				chromedp.Evaluate(bankListQuestionCountJS(tt.primary, tt.fallback), &got),
			)
			if err != nil {
				t.Fatalf("running bankListQuestionCountJS(%q, %q) in Chrome: %v", tt.primary, tt.fallback, err)
			}
			if got != tt.want {
				t.Fatalf("bankListQuestionCountJS(%q, %q) = %d, want %d", tt.primary, tt.fallback, got, tt.want)
			}
		})
	}
}

// TestEditBankActionJS_Chrome proves the rename lookup clicks a row's "Edit
// bank {title}" action and never the name button of a bank literally titled
// "Edit bank {title}".
func TestEditBankActionJS_Chrome(t *testing.T) {
	t.Parallel()
	browserCtx := startBankChrome(t)
	for _, tt := range []struct {
		name        string
		page        string
		title       string
		wantOK      bool
		wantClicked string
	}{
		{name: "action beside colliding name button", page: collisionPage([3]string{"Edit bank Other", "9", ""}, [3]string{"Other", "5", "Edit bank Other"}), title: "Other", wantOK: true, wantClicked: "action-Edit bank Other"},
		{name: "colliding name button alone is not an action", page: collisionPage([3]string{"Edit bank Other", "9", ""}), title: "Other", wantOK: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tabCtx, tabCancel := chromedp.NewContext(browserCtx)
			defer tabCancel()
			ctx, cancel := context.WithTimeout(tabCtx, 60*time.Second)
			defer cancel()
			var ok bool
			var clicked string
			err := chromedp.Run(ctx,
				chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(tt.page)),
				chromedp.WaitReady("body", chromedp.ByQuery),
				chromedp.Evaluate(editBankActionJS(tt.title), &ok),
				chromedp.Evaluate(`window.clicked || ""`, &clicked),
			)
			if err != nil {
				t.Fatalf("running editBankActionJS in Chrome: %v", err)
			}
			if ok != tt.wantOK || clicked != tt.wantClicked {
				t.Errorf("editBankActionJS(%q) = %v, clicked %q, want %v, %q", tt.title, ok, clicked, tt.wantOK, tt.wantClicked)
			}
		})
	}
}

// TestOpenBankFromListTasks_ClickResult_Chrome proves the open task fails fast
// with errBankRowNotFound when the bank's row is absent, instead of waiting for
// a detail-page selector that will never appear. Only the click action runs;
// the trailing sleep/wait are the unchanged detail-page confirmation.
func TestOpenBankFromListTasks_ClickResult_Chrome(t *testing.T) {
	t.Parallel()
	browserCtx := startBankChrome(t)
	std := []string{"Name", "Updated", "Questions", "Last Used", "Created", ""}
	for _, tt := range []struct {
		name              string
		page              string
		primary, fallback string
		wantNotFound      bool
	}{
		{name: "primary row present", page: bankListPage(std, bankListRowFor("Bank", "5")), primary: "Bank"},
		{name: "fallback row present", page: bankListPage(std, bankListRowFor("Renamed", "5")), primary: "Bank", fallback: "Renamed"},
		{name: "row missing", page: bankListPage(std, bankListRowFor("Other", "5")), primary: "Bank", wantNotFound: true},
		{name: "list still rendering", page: bankListPage(std), primary: "Bank", fallback: "Renamed", wantNotFound: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tabCtx, tabCancel := chromedp.NewContext(browserCtx)
			defer tabCancel()
			ctx, cancel := context.WithTimeout(tabCtx, 60*time.Second)
			defer cancel()
			err := chromedp.Run(ctx,
				chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(tt.page)),
				chromedp.WaitReady("body", chromedp.ByQuery),
				openBankFromListTasks(tt.primary, tt.fallback)[0],
			)
			if gotNotFound := errors.Is(err, errBankRowNotFound); gotNotFound != tt.wantNotFound {
				t.Fatalf("click action error = %v, want errBankRowNotFound=%v", err, tt.wantNotFound)
			}
			if !tt.wantNotFound && err != nil {
				t.Fatalf("click action error = %v, want nil", err)
			}
		})
	}
}
