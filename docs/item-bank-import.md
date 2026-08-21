# Canvas New Quizzes Item Bank import

## Decision

Do not add an `import-item-bank` API command yet. Canvas's public New Quiz Items
API permits creation of question items in quizzes, but documents `BankItem` and
`BankEntry` as read-only: banks and their contents must be created or changed in
the UI. Canvas's Content Migrations API accepts QTI uploads, but imports content
into a course; its documented migration types and parameters do not target a New
Quizzes Item Bank.

Current `internal/canvas.Client` is course REST API only (pages/modules). It has
no browser session, upload workflow, or Item Bank API, so pretending this is an
API-only feature would produce an unsupported integration.

Authoritative evidence:

- [New Quiz Items API](https://developerdocs.instructure.com/services/canvas/resources/new_quiz_items): `BankItem` and `BankEntry` can only be retrieved; creation/update occurs via UI.
- [Content Migrations API](https://developerdocs.instructure.com/services/canvas/resources/content_migrations): supports file-backed `qti_converter` migrations at account/course/group/user scope, not an Item Bank target.
- [Canvas Instructor Guide](https://community.canvaslms.com/t5/Instructor-Guide/tkb-p/Instructor): lists “How do I import questions from a QTI package into an item bank in New Quizzes?” as a New Quizzes workflow. Availability still depends on New Quizzes and local permissions.

## Required package contract

Browser work starts only after `approve` emits a QTI 1.2 ZIP with:

- `imsmanifest.xml` at ZIP root;
- assessment XML referenced by manifest;
- QTI 1.2 root namespace;
- `response_lid rcardinality="Single"` for MC/TF and `"Multiple"` for MA.

Last requirement already exists in `internal/qti/qti.go`; package tests must keep
it. Canvas New Quizzes import reports show this attribute is required in practice
for choice questions even though QTI permits omission.

## Browser command

`import-bank` uses Chromedp. It drives Canvas UI only; no private Canvas
endpoints.

By default it launches headless Chrome itself against a persisted profile
directory (`--chrome-profile-dir`, default a `pdf2qti` directory under the OS
user-config dir) — no visible window. If that profile's session has expired
or never existed, it logs in from `CANVAS_USERNAME`/`CANVAS_PASSWORD` before
continuing; if the session is already valid (the common case once a profile
exists), login is skipped entirely. Set credentials via a password manager
rather than a literal value in shell history, e.g.:

```sh
export CANVAS_USERNAME=you@example.edu
export CANVAS_PASSWORD="$(op read op://Private/Canvas/password)"

pdf2qti import-bank \
  --course-id 147966 \
  --bank-name '2120: Chapter 1 Quiz' \
  --package /absolute/path/ch01_qti.zip \
  --on-existing append
```

This only works when Canvas login has no MFA step in front of it — a
scripted credential submit cannot complete a push/TOTP/hardware-key prompt.
If MFA is ever added, use the manual-attach escape hatch instead: launch
Chrome yourself with a separate profile and remote debugging enabled, sign in
by hand, and pass `--browser-url`:

```sh
open -na "Google Chrome" --args --remote-debugging-port=9222 --user-data-dir=/private/tmp/pdf2qti-canvas-profile

pdf2qti import-bank --browser-url http://127.0.0.1:9222 \
  --course-id 147966 --bank-name '2120: Chapter 1 Quiz' \
  --package /absolute/path/ch01_qti.zip --on-existing append
```

Recorded login forms (UNT, Aug 20 2026) — Canvas can present either
depending on how the session got there. Navigating straight to
`/login/canvas` renders Canvas's own React "new_login" UI (not the classic
`pseudonym_session_*` form), which has `data-testid` accessibility hooks:
`input[data-testid="username-input"]`, `input[data-testid="password-input"]`,
`button[data-testid="login-button"]`. Navigating to a protected course URL
while unauthenticated instead redirects through UNT's actual Shibboleth SSO
gateway (`sso.unt.edu`), a plain server-rendered form with no `data-testid`
at all. Both forms happen to use the same plain `#username`/`#password` ids
for their fields, so the implementation selects on those ids rather than
`data-testid` (which only one of the two forms has) — see the constants and
comment above `usernameSelector` in `internal/itembank/chromedp.go`.
Re-verify live (in an incognito window, not a signed-in profile) if login
automation starts failing on a Canvas theme update.

Append `--create-random-quiz=N` to create an unpublished New Quiz after import.
The quiz uses exactly `N` randomly selected questions from the imported bank.
Its title is the final Item Bank title with ` Quiz` appended, unless that title
already ends with the whole word `Quiz` (case-insensitive).

`--bank-name` is the exact final bank name — Canvas renames a New Quizzes
Item Bank to the imported QTI package's assessment title during import even
when importing into an existing, correctly-named bank; the importer detects
that mismatch and renames the bank back to `--bank-name` through the Banks
list's "Edit bank" dialog before returning, so callers (including
`--create-random-quiz`) can rely on `--bank-name` being the bank's actual
final name.

```sh
pdf2qti import-bank \
  --course-id 147966 \
  --bank-name '2120: Chapter 1 Quiz' \
  --package /absolute/path/ch01_qti.zip \
  --on-existing append \
  --create-random-quiz=10
```

Use `--dry-run` first. Default `--on-existing=fail` protects existing banks;
`append` imports into exact matching bank. Command validates ZIP and root
`imsmanifest.xml` before browser connection. It always validates the
manifest-referenced assessment title and item count (needed to detect and
correct the Canvas rename above, regardless of `--create-random-quiz`); with
`--create-random-quiz` it additionally rejects `N` greater than that count and
checks for an exact quiz-title collision before importing. It never publishes
the quiz or overwrites an existing quiz. The profile directory persists a
live Canvas session and is created with `0700` permissions; treat it with the
same care as a bearer token.

Recorded UNT UI flow (Aug 18 2026, updated Aug 21 2026 against live headless
runs):

1. `/courses/{course}/banks` → `Create Bank` → `Item Bank` dialog →
   `Bank Name` → `Create Bank`.
2. Open bank → `More Item Banking Actions` → `Import Content`.
3. Attach ZIP, submit `Import`, wait for visible completion.
4. Quiz builder (button text is `Quiz/Survey`, not `+ Quiz`) → `Add from item
   bank` → bank (matched by exact link text, no `data-bank-id`/`href` to key
   off) → `Add this bank to quiz`. This adds the bank as an "all questions"
   group. This button is waited-for-enabled (`chromedp.WaitEnabled`, the same
   pattern used for the QTI-package `Import` button) before it's clicked: a
   CDP click on a disabled button reports success but adds nothing, which is
   one of the ways this flow previously produced a quiz with zero content.
5. The visible `All / Random` control at the top of the panel is a separate
   *add more content* action (adds another bank/group) — not this group's
   edit toggle. The real per-group control is `Edit Bank containing
   questions N through M.` (a `role="button"` element with a pencil icon).
   The flow polls for this control to actually appear before trying to
   configure it, and errors hard if it never does, rather than proceeding
   against a panel that never rendered. Click it → `Randomly select
   questions` → `Number of questions` → `Done`. The "Number of questions"
   field's label is a `<span>`, not a `<label>`, and its Instructure-generated
   `id` (e.g. `NumberInput___1`) doesn't reliably reflect visual/DOM order
   against sibling fields (points per question, source bank picker), so it's
   located by walking up from the label span to the smallest ancestor
   containing exactly one `<input>`.
6. After clicking `Done`, the tool re-navigates (a fresh `Navigate`, not a
   `Reload`) to the created quiz's URL and re-checks, from that fresh
   document, that the random-selection Item Bank group and question count are
   actually present — retrying a few times if not. The in-page check
   immediately after `Done` alone is not sufficient: Canvas's UI can show the
   group as added optimistically before its backend autosave has actually
   persisted it, and this gap is exactly what produced a quiz that reported
   success, had a valid URL, and contained zero questions.

## Browser-adapter design

```text
commands.ImportBankCmd
  -> itembank.Importer interface
       -> browser adapter (Chromedp or browser MCP)
            -> Canvas New Quizzes Item Banks UI
```

Command inputs:

- `--course-id` (scope used to open New Quizzes)
- `--bank-name` (required; e.g. `2120: Chapter 1`)
- `--package` (required QTI ZIP)
- `--on-existing=fail|append` (default `fail`)
- `--create-random-quiz=N` (optional; create an unpublished random bank group)
- `--dry-run`
- `CANVAS_USERNAME` / `CANVAS_PASSWORD` env vars, or `--username`/`--password`
  (used to log headless Chrome in when the persisted profile's session has
  expired)
- `--chrome-profile-dir` (persisted headless Chrome profile; default a
  `pdf2qti` directory under the OS user-config dir)
- `--browser-url` (manual-attach escape hatch; skips headless launch and login
  entirely, assumes the target session is already authenticated)

Do not accept `--replace` until behavior is explicitly designed: deletion could
remove shared banks/items and is irreversible from this tool's perspective.

Browser adapter state machine:

0. Ensure session: navigate to Canvas; if the login form is present (stale or
   nonexistent profile), fill it from `CANVAS_USERNAME`/`CANVAS_PASSWORD` and
   wait for the redirect back into the app. Skipped when `--browser-url` is
   set (manual-attach mode assumes the session is already authenticated).
1. Open course New Quizzes entry point with authenticated browser profile.
2. Open Item Banks; verify New Quizzes and edit permission are present.
3. Find exact bank name. On absent: create bank. On present: obey
   `--on-existing`.
4. Open bank's import action; attach package; submit.
5. Wait for visible completed/failed state. Never treat file chooser close as
   success.
6. Verify bank title, bank URL, and count increased by expected question count
   when UI exposes a count. If Canvas renamed the bank to the QTI package's
   title, rename it back to `--bank-name` through the Banks list's "Edit
   bank" dialog before returning. Return those facts in audit log.

All selectors belong in one adapter file. Prefer stable accessibility roles/names
over CSS classes. Capture sanitized screenshot plus DOM excerpt on state failure;
never log cookies, bearer tokens, or file contents.

## Discovery checklist

Run manually first with disposable course/bank and real package:

1. Record landing URLs, accessible names, roles, and upload control behavior.
2. Record network requests only to identify whether an officially supported API
   endpoint appears. Do not replay private endpoints in production code without
   Canvas support confirmation.
3. Test empty bank creation, package import, duplicate-name handling, and import
   failure UI.
4. Verify question count and three question types: MC, MA, numerical.
5. Verify inline/display LaTeX renders in editor and learner preview.

Discovery output is instance-specific; local Canvas themes and New Quizzes rollout
can change routes/selectors. Gate adapter implementation on recorded evidence.

## Test plan

Unit tests, no Canvas credentials:

- command rejects missing/non-ZIP package before browser startup;
- command rejects invalid `--on-existing`;
- command sends immutable request data to fake `itembank.Importer`;
- dry run reports action without browser import call;
- adapter-independent result logging includes bank name, URL, imported count.

Browser contract tests, disposable Canvas course:

- creates named bank and imports generated package;
- appends only when explicitly requested;
- failure leaves existing bank untouched;
- imported count equals QTI item count;
- MC/MA/numerical scoring plus inline/display LaTeX work in preview.

These contract tests need credentials and a target Canvas tenant, so they must be
opt-in (`CANVAS_BROWSER_E2E=1`) and never run in normal `go test ./...`.
