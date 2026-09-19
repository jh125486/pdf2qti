# Item Bank import flakiness

`internal/itembank/chromedp.go`'s `ChromedpImporter` drives Canvas's "New
Quizzes Item Bank" UI via chromedp. Live runs against a real Canvas tenant
surfaced several distinct flake classes over time; this doc tracks them so a
future investigation doesn't have to rediscover the same ground.

## Symptom 1: UI/autosave lags a completed backend action

Two timing races observed on live Canvas runs, both instances of the same
underlying problem — Canvas's UI (and, for the quiz-builder path, its
autosave) can lag behind a backend action that has already actually
succeeded:

- A `SetUploadFiles`/submit-import timeout on the plain bank-import path left
  a stray empty Item Bank behind with no way to detect it (the upload had
  often already succeeded server-side; only the "has imported successfully!"
  poll was still catching up).
- A tight 30s poll on the imported question count failed even though the
  import had already succeeded (a larger bank's rendered count can take
  longer to settle than 30s).
- On the random-quiz-group path, the in-page "verify random group" check
  could be satisfied by Canvas's *optimistic* UI before its backend autosave
  had actually persisted the group — producing a quiz that reported success,
  had a valid URL, and contained zero questions.

Fixed (PRs #72, #73, #75) by re-navigating fresh (never `Reload`, which can
be served from bfcache/client-side router state) and re-checking actual
content before failing or reporting success: `recoverStuckImport`,
`pollBankItemCount`, and `confirmQuizPersisted` all implement this pattern.
`isTimeoutLike` narrowly matches only genuine timeout-shaped errors (a plain
"context deadline exceeded" or a `chromedp.Poll` timeout, not a JS exception
inside the poll predicate) before treating a failure as this class of race
and attempting recovery — a selector/logic error still fails immediately.
`runResilient` separately retries a CDP "context torn down mid-evaluate by a
navigation" error, a related but distinct timing issue.

This class of bug is a UI/autosave lag: the backend action genuinely
happened, and re-checking is the correct fix. It is unrelated to, and not
fixed by, the root cause below.

## Symptom 2: a UI click that plain didn't happen

Confirmed live (against `unt.instructure.com/courses/106252/banks`, not
guessed) as the root cause of the hardest-to-diagnose instance of this
flake class: raw `chromedp.Click(...)` calls — synthetic mouse-event
hit-testing — against Canvas's Instructure UI (InstUI) React components can
**silently fail to deliver the click at all**. chromedp reports no error;
the target element's state (a checkbox's `checked` property, a button's
click handler) simply never changes.

Specifically:

1. The create-bank dialog's "Share with course" checkbox
   (`[role=dialog] input[type=checkbox]`) could be hit-test-clicked without
   its `checked` property ever flipping.
2. Because the checkbox was never actually checked, Canvas's `POST /api/banks`
   call still succeeded (201 Created, confirmed via live network trace) but
   created the bank **unshared/private to the course** — it then never
   appeared in that course's `/courses/{id}/banks` listing.
3. Every downstream step (`findBank`/`bankExistsJS`, opening the bank,
   `stableBankItemCount`, "Add from item bank" in the quiz builder) then
   searched for a bank invisible to the course-scoped UI and hung/timed out
   — exactly the shape of flake Symptom 1's recovery machinery was built to
   paper over, which is why prior PRs (#72, #73, #75) never actually fixed
   this: a timeout-and-retry loop cannot recover from a click that never
   happened in the first place.
4. The top-level "Create Bank" button was also observed, in one live run, to
   silently fail to open the create-bank dialog at all.

This exact failure class — a hit-test click landing on the wrong pixel, or
being swallowed by an InstUI icon/facade wrapper — was already diagnosed and
fixed once before this investigation, for the quiz-builder's per-group
"Edit Bank containing questions N through M" control (`clickEditBankGroupJS`):
a direct JS `.click()` dispatched on the matched DOM element reliably
reaches its handler where a hit-test-based synthetic click does not.

### Fix

Generalized the `clickEditBankGroupJS` pattern into shared helpers
(`clickJS`, `clickSelectorJS`, `clickTextJS`, `clickTextInsensitiveJS`) and
converted every InstUI icon/text button and checkbox click in
`chromedp.go` to JS `.click()` dispatch instead of `chromedp.Click`'s
hit-test. Genuine plain-HTML form interactions (the Canvas/SSO login form's
username/password/submit, and the two places this file clicks a plain
`<input>` only to focus it before typing) were left as real chromedp clicks
— they are not InstUI facade components and have no evidence of the same
failure mode.

Where Canvas exposes a stable `data-automation="..."` attribute on a
control (confirmed live: the create-bank submit button carries
`data-automation="sdk-create-bank-button"`), the click matches on that
attribute rather than text content — more robust to Canvas UI copy changes,
and sidesteps a real ambiguity already recorded live: the top "Create Bank"
button's own `textContent` renders as `"Create BankBank"` (a nested icon
label duplicates the visible text), which broke exact-text matching.

Because this is a "click didn't happen" bug rather than a timing race, the
fix does **not** add another timeout/retry loop. Instead, `Import()` now
re-checks — via the same `findBank`/`bankExistsJS` check used elsewhere —
that a freshly created bank is actually visible in the course's bank list
immediately after the create-bank sequence, and fails fast with a specific,
readable error if not, rather than silently proceeding into the 240s+
timeout/retry cascade that made this bug so hard to diagnose in the first
place.

## Symptom 3: click-before-render races, live-confirmed via headless E2E

A prior investigation session's "how to resume" notes flagged an unexplained
gap: a live *headless* `import-bank --create-random-quiz` run hit the same
"dialog never opens" shape of failure that a *headed*, manually-attached
diagnosis session never reproduced. Root cause was confirmed by actually
running the fixed binary headless against `unt.instructure.com/courses/106252`
repeatedly (not guessed, not diagnosed via a manually-driven browser) and
capturing the page's live state on failure with a temporary in-process
screenshot/DOM-dump helper (removed again once each bug was root-caused —
not shipped). Three distinct click-before-render races, all the same shape
as Symptom 2 but with the target element simply not existing yet rather than
being swallowed by an InstUI hit-test miss:

1. **The top-level "Create Bank" button.** `Import()`'s click on it was a
   one-shot `evaluateBool` right after `Navigate` + a 1s `Sleep` — on a cold
   profile that just had to log in via `ensureSession`, the Banks page's
   React content (including this button) can still be hydrating at that
   point, with no prior render on the page to warm anything up. Fixed by
   polling (`evaluatePollBool`, 15s) instead of a single attempt.
2. **The engine-choice dialog's "New Quizzes/Surveys" radio.** Not a timing
   bug — `selectNewQuizEngineJS` matched a radio/label/button whose trimmed,
   lowercased `innerText` was *exactly* `"new quizzes"`, but Canvas's actual
   live label reads **"New Quizzes/Surveys"**, so the exact-equality check
   never matched anything. The radio was never selected, the dialog was
   never submitted, and every subsequent step (starting with "wait for quiz
   title") hung until the outer timeout — this is what the "wait for quiz
   title: context deadline exceeded" symptom actually was, confirmed via a
   captured DOM dump showing the still-open engine-choice dialog with that
   exact label text. Fixed by matching `startsWith('new quizzes')` instead
   of exact equality.
3. **The quiz builder's "Add from item bank" picker.** The picker dialog's
   shell (`[role="dialog"]`) can render before its bank list has finished
   loading — most visibly for a bank created moments earlier in the same
   run, which the picker's own list fetch may not have caught up to. Fixed
   by polling the bank-name click (`evaluatePollBool`, 15s) instead of a
   one-shot attempt right after the dialog shell appears.
4. **The rename dialog's post-rename reopen.** Even after being converted to
   a poll in an earlier pass (`evaluatePollBoolViaRun`, 5s), live E2E runs
   still hit intermittent "not found in bank list" failures re-opening a
   bank immediately after renaming it — the same Banks list Symptom 2's
   post-create check reads. 5s and then 15s both proved occasionally too
   tight; bumped to 30s to match this file's other bank-list-reading polls
   (`addFromBankReadyJS`, `bankItemCountMatchesJS`), which already use that
   budget for the same class of Canvas-side latency. This one may also be
   inflated by test debris: repeated E2E runs against the same sandbox
   course leave many stray banks behind (see cleanup note below), and a
   longer bank list plausibly renders more slowly.

Verified via 8 live headless `import-bank --create-random-quiz=2` runs
against course 106252 after all of the above: 7 succeeded end-to-end (bank
created, imported, quiz built with the requested question count); the one
failure was symptom 4 before its timeout was bumped from 15s to 30s, and
every run after that fix succeeded.

A later 20-run reliability sweep (after a separate customer-reliability
audit's fixes) hit 19/20, with the one failure environmental (severe
self-inflicted memory pressure from the test harness itself). A follow-up
10-run sweep then hit this same class of failure a fourth time — this time
at symptom 1's post-create-bank visibility check (30s), with the machine
otherwise healthy (722MB free, no resource contention) — confirming via
`evaluatePollBoolViaRun`'s outer-context disambiguation (added by the
reliability audit's F1 fix) that this was a genuine "polled 30s and never
found it," not an outer-budget expiry misattributed as one. Bumped to 60s.
The underlying render-latency variance is not fully understood — the
growing stray-bank list (below) is the leading hypothesis, but this has only
ever been tested against one course on one Canvas tenant. Verifying against
a different, unrelated course/tenant is the top recommended follow-up before
broad customer rollout.

### Stray test banks

Repeated live verification against course 106252 during this investigation
left behind a number of "Fix Verify \<timestamp\>" and "Fix Verify
\<timestamp\> Quiz" Item Banks/quizzes with no cleanup mechanism in this
codebase (there is no delete-bank UI flow implemented) — same situation as
the pre-existing "Audit Probe Bank X1" artifact noted in an earlier session's
notes. Harmless (private test course), but worth a manual sweep before the
course is used for anything else.

## Prior "suggested fix direction" (historical, superseded)

Earlier revisions of this doc (before the root cause above was known)
suggested treating all Item Bank import flakiness as a single UI/autosave
timing race and leaning further on retry/backoff. That direction was
correct for Symptom 1 but was never going to fix Symptom 2 — no amount of
retrying re-delivers a click event that chromedp's hit-test never actually
dispatched. Keep both symptom sections above distinct: Symptom 1 remains a
real, separate, still-relevant class of Canvas UI/autosave lag that the
existing `runResilient`/`isTimeoutLike`/recovery machinery correctly
handles; Symptom 2 required a different, non-timing fix.
