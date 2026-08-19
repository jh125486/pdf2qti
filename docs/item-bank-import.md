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

`import-bank` uses Chromedp against existing authenticated Chrome debugging
session. It drives Canvas UI only; no private Canvas endpoints.

```sh
# Start separate profile, sign into Canvas, leave Chrome running.
open -na "Google Chrome" --args --remote-debugging-port=9222 --user-data-dir=/private/tmp/pdf2qti-canvas-profile

pdf2qti import-bank \
  --course-id 147966 \
  --bank-name '2120: Chapter 1 Quiz' \
  --package /absolute/path/ch01_qti.zip \
  --on-existing append
```

Append `--create-random-quiz=N` to create an unpublished New Quiz after import.
The quiz uses exactly `N` randomly selected questions from the imported bank.
Its title is the final Item Bank title with ` Quiz` appended, unless that title
already ends with the whole word `Quiz` (case-insensitive).

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
`imsmanifest.xml` before browser connection. With `--create-random-quiz`, it
also validates the manifest-referenced assessment title and item count, rejects
`N` greater than that count, and checks for an exact quiz-title collision before
importing. It never publishes the quiz or overwrites an existing quiz.

Recorded UNT UI flow (Aug 18 2026):

1. `/courses/{course}/banks` → `Create Bank` → `Item Bank` dialog →
   `Bank Name` → `Create Bank`.
2. Open bank → `More Item Banking Actions` → `Import Content`.
3. Attach ZIP, submit `Import`, wait for visible completion.
4. Quiz builder → `Add from item bank` → bank → `Add this bank to quiz`.
5. Edit bank group → `Randomly select questions` → `Number of questions` → `Done`.

`Add this bank to quiz` initially adds all questions. Configure random count in
bank-group editor afterward.

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

Do not accept `--replace` until behavior is explicitly designed: deletion could
remove shared banks/items and is irreversible from this tool's perspective.

Browser adapter state machine:

1. Open course New Quizzes entry point with authenticated browser profile.
2. Open Item Banks; verify New Quizzes and edit permission are present.
3. Find exact bank name. On absent: create bank. On present: obey
   `--on-existing`.
4. Open bank's import action; attach package; submit.
5. Wait for visible completed/failed state. Never treat file chooser close as
   success.
6. Verify bank title, bank URL, and count increased by expected question count
   when UI exposes a count. Return those facts in audit log.

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
