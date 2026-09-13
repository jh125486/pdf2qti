# Slide Deck Format

The proto-deck Markdown produced by `slides` and `module` uses a simple, human-editable format.
`ParseProtoDeck` (`internal/distill/protodeck.go`) reads it back into the pieces `pptx` needs to
render a PPTX; `GenerateProtoDeck`/`expandOutline` (`internal/distill/protodeck.go`,
`internal/distill/outline.go`) is what writes it in the first place. Since it's plain Markdown, you
can hand-edit a generated deck (or write one from scratch) before running `pptx` against it, as
long as it follows the rules below.

## Structure

The deck is one Markdown document, split into blocks by `---` separator lines:

- **Deck title** — the first block, a single `# Title` heading before the first `---`.
- **Agenda slide** — the next block: `<!-- meta: 1 agenda -->`, a `# Agenda` heading, and one
  top-level bullet per agenda item.
- **Content slides** — one block per slide: a `<!-- meta: N tag -->` marker, a `# Slide Title`
  heading, and 5-8 bullets.
- **Summary slide** — the final block: `<!-- meta: N summary -->`, a `# Summary` heading, and one
  recap bullet per agenda item.

### Meta markers

Every non-agenda, non-summary slide is tagged with the source chapter it came from:

```
<!-- meta: N tag -->
```

- `N` is the slide's 1-based position in the deck and must be sequential with no gaps or repeats
  (the agenda slide is always `1`) — `ParseProtoDeck`/`validateProtoDeck` treat a numbering gap as
  a hard error, since it indicates broken generation.
- `tag` is `agenda` for the agenda slide, `summary` for the summary slide, or a chapter's
  `Tag` (its `config.Source.ID`) for a content slide — this is what groups slides into PPTX
  sections when a deck spans multiple chapters (`module`).

### Bullets

- A top-level bullet is a Markdown list item: `- text`.
- A sub-bullet is indented two spaces: `  - text`. Only one level of nesting is supported; use a
  sub-bullet only when it directly elaborates the bullet immediately above it (a worked example, a
  concrete instance, or a clarifying detail) — not as a place to put an unrelated top-level point.
- Bullet text is a short phrase or fragment, not a full sentence — 11 words or fewer (LaTeX
  formulas don't count toward the limit). Wrap only key vocabulary/terms in `**bold**` inline where
  they're first introduced; never bold an entire bullet.
- Wrap literal commands, config keys, and identifiers (git commands, YAML keys like `on:`,
  template strings like `${{ secrets.MY_SECRET }}`, Dockerfile instructions) in single backticks —
  `` `git worktree add ../foo branch` `` — instead of `**bold**`. `pptx` renders a backtick span as
  a monospace (Consolas) run rather than bolding it.
- A worked example that's a system of equations or a multi-row matrix (`\begin{aligned}`,
  `\begin{cases}`, `\begin{bmatrix}`, etc.) is broken into one level-1 sub-bullet per row, instead
  of one oversized bullet holding the whole block.

### Mermaid diagram slides

A content-slide block can be a Mermaid diagram instead of a set of bullets. A diagram block
contains exactly one non-empty ` ```mermaid ` fenced code block, exactly one non-empty
`<!-- alt: ... -->` comment line, and exactly one non-empty `> ` blockquote line — and no bullets.
`pptx` renders the diagram to a PNG and places it, with its title and caption, on its own
dedicated slide.

````markdown
<!-- meta: 3 ch01 -->
# Request lifecycle

<!-- alt: A client sends a request to an API, which queries a database and returns a response to the client. -->
> Requests pass through the API before data is read from storage.

```mermaid
flowchart LR
  Client --> API --> Database
  Database --> API --> Client
```
````

The `<!-- alt: ... -->` line is the image's screen-reader description, embedded on the rendered
PNG for PowerPoint accessibility — keep it structural and specific, naming the diagram's actual
nodes, arrows, and direction, since a reader can't see the picture. The `> ` blockquote is the
caption a sighted viewer reads alongside the diagram — keep that one focused on the teaching
takeaway instead. The two rarely make good stand-ins for each other.

A template only needs a `Diagram` slide layout when a deck actually contains a diagram slide —
existing Title/Agenda/Content-only templates stay valid for decks without one. That layout needs
`title`, `body`, and `pic` placeholders: `title` and `body` take the diagram slide's title and
caption exactly like a Content slide, and `pic` is replaced by the rendered diagram image.

### Math

LaTeX is written inline using `\(...\)` for inline math and `\[...\]` for display math (never bare
`_`/`^` outside math mode). `pptx` converts each formula to native PowerPoint math (OMML) via a
pandoc round-trip when rendering, falling back to the escaped LaTeX text if pandoc isn't available
or a formula fails to convert.

### Code spans

Single backticks (`` `code` ``) mark inline code — commands, config keys, template strings —
rendered as a monospace (Consolas) run. A code span's content is always taken literally: it's
extracted before `**bold**`/math parsing runs, so a stray `**` or `\(...\)` inside a code span
never gets reinterpreted as markdown. The one tradeoff is that wrapping a code span itself in
`**bold**` (`` **`code`** ``) does not render bold — code spans aren't expected to double as
emphasized text, so this is left unhandled.

## Example

```markdown
# Chapter 1: Vectors and Matrices

---

<!-- meta: 1 agenda -->
# Agenda

- Vector operations
- Matrix multiplication
- Systems of linear equations

---

<!-- meta: 2 ch01 -->
# Vector Operations

- A **vector** is an ordered list of numbers
- Addition is componentwise: \(\mathbf{u} + \mathbf{v}\)
  - Example: \((1,2) + (3,4) = (4,6)\)
- **Scalar multiplication** scales every component
- Dot product returns a single number, not a vector

---

<!-- meta: 3 ch01 -->
# Matrix Multiplication

- A **matrix** is a rectangular array of numbers
- \((AB)_{ij}\) is the dot product of row \(i\) of \(A\) and column \(j\) of \(B\)
- Multiplication is defined only when inner dimensions match
- Not commutative: \(AB \neq BA\) in general
  - Row 1: \(3x_1-2x_2+2x_3=2\)
  - Row 2: \(x_1+4x_2-x_3=1\)

---

<!-- meta: 4 summary -->
# Summary

- Vectors combine via componentwise **addition** and scalar multiplication
- Matrix multiplication combines rows and columns via the dot product, order-dependent
```
