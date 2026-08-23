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
- A worked example that's a system of equations or a multi-row matrix (`\begin{aligned}`,
  `\begin{cases}`, `\begin{bmatrix}`, etc.) is broken into one level-1 sub-bullet per row, instead
  of one oversized bullet holding the whole block.

### Math

LaTeX is written inline using `\(...\)` for inline math and `\[...\]` for display math (never bare
`_`/`^` outside math mode). `pptx` converts each formula to native PowerPoint math (OMML) via a
pandoc round-trip when rendering, falling back to the escaped LaTeX text if pandoc isn't available
or a formula fails to convert.

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
