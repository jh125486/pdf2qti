# pdf2qti

[![Go Reference](https://pkg.go.dev/badge/github.com/jh125486/pdf2qti)](https://pkg.go.dev/github.com/jh125486/pdf2qti)

[![Tests](https://github.com/jh125486/pdf2qti/actions/workflows/test.yaml/badge.svg)](https://github.com/jh125486/pdf2qti/actions/workflows/test.yaml)
[![CodeQL](https://github.com/jh125486/pdf2qti/actions/workflows/codeql.yml/badge.svg)](https://github.com/jh125486/pdf2qti/actions/workflows/codeql.yml)
[![Codecov](https://codecov.io/gh/jh125486/pdf2qti/branch/main/graph/badge.svg)](https://codecov.io/gh/jh125486/pdf2qti)
[![Sonar Coverage](https://sonarcloud.io/api/project_badges/measure?project=jh125486_pdf2qti&metric=coverage)](https://sonarcloud.io/summary/overall?id=jh125486_pdf2qti)

[![Mutation score](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/jh125486/pdf2qti/gh-pages/mutation-score.json)](https://github.com/jh125486/pdf2qti/actions/workflows/mutation-badge.yml)
[![Mutants](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/jh125486/pdf2qti/gh-pages/mutants-count.json)](https://github.com/jh125486/pdf2qti/actions/workflows/mutation-badge.yml)

A CLI tool that converts PDF or Markdown sources into Canvas-compatible QTI quizzes using LLM-assisted question generation.

## Overview

`pdf2qti` automates the creation of course content for Canvas LMS from existing PDF or Markdown materials. It:

- **Extracts** text from PDF documents or selected Markdown sections
- **Distills** each source into structured context JSON (concepts, examples, LaTeX-bearing prose) via an LLM provider — the shared input for everything below
- **Generates** quiz questions (True/False, Multiple Answer, Multiple Choice, Short Answer, Essay, Matching, Numerical) from that context
- **Renders** a human-reviewable Markdown draft for editing and approval, and converts approved drafts to Canvas-importable QTI 1.2 ZIP packages
- **Builds** slide-deck Markdown from the same context and renders it into a PPTX using a template
- **Publishes** Learning Objectives / Materials pages directly to Canvas

## Features

- Seven question types: **True/False (TF)**, **Multiple Answer (MA)**, **Multiple Choice (MC)**, **Short Answer (SA)**, **Essay (ES)**, **Matching (MT)**, **Numerical (NR)**
- Configurable LLM provider, model, API key, and arbitrary provider-specific model parameters (temperature, reasoning effort, etc.)
- Per-source and global-default configuration via a JSON config file (with JSON Schema)
- Validation rules (unique options, sequential numbering, correct-answer density, etc.)
- Audit logging for every operation
- Cross-platform releases for Linux, macOS, and Windows (amd64 and arm64)

## Installation

### Using `go install`

```bash
go install github.com/jh125486/pdf2qti@latest
```

### Download a release

Pre-built binaries are available on the [Releases](https://github.com/jh125486/pdf2qti/releases) page.

## Configuration

`pdf2qti` is driven by a JSON configuration file (default: `quiz_input.json`).  
A JSON Schema is provided in [`quiz_input.schema.json`](quiz_input.schema.json) for editor validation.

### Minimal example

```json
{
  "version": 1,
  "defaults": {
    "quiz": {
      "titleTemplate": "Chapter {{printf \"%02d\" .chapter}} Quiz",
      "counts": { "tf": 5, "ma": 3, "mc": 10 },
      "mcOptions": { "min": 4, "max": 4 },
      "maOptions": { "min": 4, "max": 6 }
    },
    "generation": {
      "provider": "openai",
      "model": "gpt-4o",
      "apiKeyEnv": "OPENAI_API_KEY",
      "instructions": "Prefer applied questions with short scenarios.",
      "modelParams": { "temperature": 0.7 }
    },
    "workflow": {
      "outDir": "out"
    }
  },
  "sources": [
    {
      "id": "ch01",
      "name": "Introduction",
      "chapter": 1,
      "pdf": "pdfs/chapter01.pdf"
    }
  ],
  "modules": [
    {
      "id": "mod01",
      "name": "Module 1: Introduction",
      "sourceIds": ["ch01"]
    }
  ]
}
```

### Configuration fields

| Field | Description |
|---|---|
| `version` | Config schema version (must be `1`) |
| `courseName` | Optional course-wide display name used as the PPTX title-slide subtitle |
| `defaults` | Global defaults for `distill`, `quiz`, `generation`, `validation`, and `workflow` |
| `sources` | Array of PDF or Markdown context sources; each source can override any default |
| `modules` | Array of modules grouping one or more `sources` (by `sourceIds`) for the `module` command |

Each `source` requires an `id` and exactly one of a `pdf` path or a `context` object.
PDF-only configurations remain valid. Distill, generation, quiz, validation, and workflow settings inherit
from `defaults` unless overridden.

Distillation checks source-derived facts against the original extracted PDF or Markdown text.
The verifier collects source quotes across all input chunks, checks each quote against that
original text, then judges whether the collected evidence supports every factual claim in each
context field. This is an LLM judgment with verified quotes, so semantic support still needs
human review for consequential use. Configure `defaults.distill.grounding.mode` or a source's
`distill.grounding.mode` as `fail` (default), `drop`, or `warn`. `fail` stops before writing context
and reports field IDs and reasons; `drop` clears unsupported fields; `warn` retains them. Both
`drop` and `warn` record findings in `verification_warnings` and the CLI log. IDs are JSON pointers
such as `/overview` or `/vocabulary/0` (term and definition together). The check also covers the model's selection
of which configured course objectives a chapter satisfies. Source IDs, book names, and chapter
numbers come from config and are outside this check.

Quiz prompts always require standalone, student-facing questions and answers, without references
to “the source,” “the text,” “the chapter,” “the passage,” or “the author.” Add course-specific
requirements with `defaults.generation.instructions`; a nonempty `sources[].generation.instructions`
replaces those default instructions and is appended to the generation prompt.

Validation rejects those five phrases by default, ignoring case and allowing whitespace between
words. It checks every question type, answer option, and matching answer. Customize
`defaults.validation.forbiddenPhrases` or `sources[].validation.forbiddenPhrases` with an array of
Go regular expressions, for example `["(?i)according\\s+to"]`. Omitted or `null` values inherit
the default rules; any array replaces the inherited rules, and `[]` disables this check. Custom
rules are case-sensitive unless prefixed with `(?i)`. Invalid regular expressions fail config
validation. Matching errors identify the question, option when applicable, matched phrase,
and rule so the draft can be corrected before approval.

Markdown context can combine files and select sections:

```json
{
  "id": "ch18",
  "name": "Data Structures",
  "chapter": 18,
  "context": {
    "files": [
      { "path": "notes/ch18.md", "sections": ["18.8", "18.9"] },
      { "path": "notes/examples.md", "include_preamble": true }
    ]
  }
}
```

Each level-two heading (`##`) starts a section, ending at the next level-one or
level-two heading. Nested headings, code fences, and Markdown formatting are retained.
With the convention `## Section 18.8 - Trees`, select by `"18.8"` or the exact heading
text `"Section 18.8 - Trees"` (without `##`). Omit `sections` or use `[]` to select all
sections. Selection preserves document order; files are concatenated in configured order.
Unknown selectors report the file path and available heading titles; duplicate or blank
selectors are rejected.

Text before the first `##` heading, including a document's `#` title, is preamble and
excluded by default. Set `include_preamble: true` per file to include it, including for
files with no level-two headings. Empty selected input is an error. Paths, like PDF paths,
resolve from the current working directory. See [the runnable Markdown example](docs/examples/markdown-context/quiz_input.json).
Run from the repository root:

```bash
pdf2qti --config docs/examples/markdown-context/quiz_input.json distill --all
```

The example uses the default stub provider; set `defaults.generation` to your LLM provider
as shown above to generate substantive context.

Each `module` requires an `id`, a `name`, and `sourceIds` (one or more `source.id` values it spans).

`defaults.workflow.outDir` is where every generated artifact lands: `<id>_context.json` (`distill`), `<id>_quiz.md`/`<id>.zip` (`generate`/`approve`), and `<id>_slides.md` (`slides`). Point it wherever you want that scratch/output tree to live — e.g. `"ctx"` if you'd rather keep it out of an `out/` you use for something else.

## Usage

```
pdf2qti [--config <file>] [--http-timeout <duration>] <command>
```

Global flags:

- `--agentic-help` — print one JSON document containing the complete command tree, flag and argument metadata, quiz config and generated-question JSON Schemas, and slide Markdown format; exits without loading config or credentials
- `-c`, `--config` — config file path (defaults to `quiz_input.json`)
- `--http-timeout` — per-request LLM HTTP timeout (defaults to `5m`)

### Commands at a glance

| Command | Purpose |
|---|---|
| `distill` | Read PDF or Markdown input and distill it into `<outDir>/<id>_context.json` |
| `generate` | Generate `<outDir>/<id>_quiz.md` from distilled context |
| `validate` | Validate generated or hand-edited quiz Markdown |
| `approve` | Convert approved quiz Markdown into `<outDir>/<id>.zip` QTI |
| `slides` | Generate `<outDir>/<id>_slides.md` slide-deck Markdown |
| `pptx` | Render slide-deck Markdown plus a PPTX template into a `.pptx` |
| `module` | Build one combined slide-deck Markdown doc for a configured module |
| `page` | Render HTML from a distilled context JSON and an HTML template |
| `publish` | Render and publish Canvas Learning Objectives / Materials pages |
| `import-bank` | Import a generated QTI ZIP into a Canvas New Quizzes Item Bank |

### Pipeline overview

`distill` is the shared first step for everything else — it's the only command that reads the
PDF or Markdown input. Every other command reads the `<outDir>/<id>_context.json` it produces.

```
                              ┌─→ generate → validate → approve → <id>.zip
PDF or Markdown → distill → context.json
                              └─→ slides → pptx → <output>.pptx

module <id>  (combines context.json for each source in the module → one slide deck)
```

A typical end-to-end run for one source:

```bash
pdf2qti distill ch01              # ctx/ch01_context.json
pdf2qti generate                  # ctx/ch01_quiz.md   (all sources in config)
pdf2qti approve                   # ctx/ch01.zip

pdf2qti slides ch01                                    # ctx/ch01_slides.md
pdf2qti pptx --slides=ctx/ch01_slides.md \
             --output=out/ch01.pptx template.pptx      # out/ch01.pptx
```

### `distill` — Distill PDF or Markdown into structured context JSON

```bash
pdf2qti distill ch01 [ch02 ...]   # specific sources
pdf2qti distill --all             # every source in the config
```

For each requested source, `distill` extracts PDF text or reads selected Markdown sections, then calls the configured LLM to
distill it into structured context (concepts, worked examples, LaTeX-bearing prose, module
name), and writes `<outDir>/<id>_context.json`. This file is the required input for `generate`,
`slides`, `page`, `publish`, and `module`. Use `--force` to overwrite an existing context file.

### `generate` — Create a quiz draft from a distilled context

```bash
pdf2qti generate [--skip-approve]
```

Requires `<outDir>/<id>_context.json` to already exist (run `distill` first — `generate` errors
out with the exact command to run if it's missing), plus an OpenAI `generation` config and its API
key environment variable. It fails when either is absent; it never writes placeholder questions.
For each source in the config, `generate`:

1. Loads the distilled context and calls the configured LLM once per question type (TF, MA, MC,
   SA, ES, MT, NR) using each type's configured count
2. Writes a Markdown quiz draft to `<outDir>/<id>_quiz.md` for human review

Pass `--skip-approve` to skip the review step and immediately convert the draft to QTI.

### `validate` — Check a quiz draft

```bash
pdf2qti validate
```

Reads `<outDir>/<id>_quiz.md` for each source and reports any validation errors or warnings based on the configured rules (e.g., unique options, sequential numbering, correct-answer density).

### `approve` — Convert a reviewed draft to QTI

```bash
pdf2qti approve
```

Reads approved `<outDir>/<id>_quiz.md` and writes Canvas-compatible QTI 1.2 package `<outDir>/<id>.zip`. Package contains `imsmanifest.xml` and `assessment.xml` at ZIP root. Canvas uses draft title as imported quiz name.

### `slides` — Generate proto-deck slide Markdown from a distilled context

```bash
pdf2qti slides ch01 [ch02 ...]     # specific sources
pdf2qti slides --all               # every source in the config
pdf2qti slides ch01 -o custom.md   # override the output path
```

Calls the configured LLM to turn `<outDir>/<id>_context.json` into a proto-deck: one slide per
`<!-- meta -->`-tagged section with a `#` heading and bullet points, `---` between slides.
LaTeX math is written inline as `\(...\)` and `\begin{bmatrix}...\end{bmatrix}`, `**bold**` marks
key terms, and single backticks (`` `code` ``) mark inline code (commands, config keys, template
strings) rendered in a monospace font — these are hand-editable Markdown, so review/tweak content
before rendering to PPTX. See [`slides.md`](slides.md) for the full format. Writes
`<outDir>/<id>_slides.md` unless `--output` is given.

Slide topics are planned one textbook section at a time (from the context's `sections`), not in a
single whole-chapter pass, so a dense multi-section chapter's coverage doesn't depend on the model
hitting a numeric target across all of it at once. `--min-slides`/`--max-slides` are therefore
advisory, not enforced: left unset (or `0`), each auto-scales to the combined length of the
sources' distilled `text` (roughly one content slide per 400 chars, ±15%/+25%) as a rough sizing
default; if the actual generated deck falls outside the resolved range, generation still succeeds
and a warning is logged rather than failing. Use `--force` to overwrite an existing slide file.

### `pptx` — Render PPTX from slide Markdown and a template

```bash
pdf2qti pptx --slides=ctx/ch01_slides.md \
             --output=out/ch01.pptx \
             -v key=value \
             template.pptx
```

Renders the slide Markdown produced by `slides` (or `module`) into a PPTX by cloning the layout
of the given `<template>` file for each slide. `--slides` and `--output` are required; the
template path is a positional argument. `-v`/`--vars` passes extra `key=value` template
variables (`;`-separated for multiple) available inside the template alongside the distilled
context data.

`slides` and `module` generate bullet-based content slides; they do not generate Mermaid blocks.
You can add a Mermaid block by hand to proto-deck Markdown, and `pptx` renders it as a PNG on its
own dedicated slide. See [`slides.md`](slides.md) for the diagram block format. A template used
with such a deck needs a `Diagram` layout with `title`, `body`, and `pic`
placeholders — `body` gets the visible caption, `pic` is replaced by the rendered diagram, and its
alt text becomes the image's screen-reader description. Requires [`mmdc`](https://github.com/mermaid-js/mermaid-cli)
on `PATH`; if a diagram fails to render, its slide still ships with title and caption, missing only
the picture, and the failure is reported back as a warning rather than failing the whole render.
A template with no `Diagram` layout is still valid for a deck that has no diagram slides.

#### Markdown schema for `pptx` input

`slides` and `module` generate proto-deck Markdown in the shape below. `pptx` also accepts
hand-written blocks in document order; it does not require a summary block or require one to be
last. A `summary` block is rendered as an ordinary content slide. The canonical format reference
is [`slides.md`](slides.md):

1. A first block containing only the deck title as `# Title`
2. `---`
3. An agenda block:
   - `<!-- meta: 1 agenda -->`
   - `# Agenda`
   - 3-8 top-level `- bullet` items
4. Zero or more content-slide blocks:
   - `<!-- meta: N <tag> -->`
   - `# Slide Title`
   - generated decks use 5-8 bullets
5. A final summary block (generated-deck convention):
   - `<!-- meta: N summary -->`
   - `# Summary`
   - generated decks include one top-level recap bullet per agenda item

Rules:

- `slides` and `module` generate 1-based sequential `N` values with no gaps. `pptx` parses
  hand-written blocks in document order and does not reject gaps in their meta numbers.
- `<tag>` is the source/chapter ID; consecutive slides with the same tag share a PPTX section
- Bullets use `- text`; indented `  - text` sub-bullets render one level deep. Deeper indentation
  is not preserved as additional nesting.
- Inline math uses `\(...\)` or `\[...\]`
- Inline code uses single backticks
- Hand-authored diagram slides use exactly one Mermaid fenced block, one `<!-- alt: ... -->` line, and one `> caption` line, with no bullets
- `pptx` enforces 3-8 agenda bullets; generated content slides follow the 5-8 bullet convention

Minimal example:

````markdown
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
- The dot product combines corresponding components
- Vector length measures magnitude

---

<!-- meta: 3 summary -->
# Summary

- Vectors combine via componentwise **addition**
- Matrix multiplication combines rows and columns
- Systems of linear equations can be represented with matrices
````

### `module` — Build a combined slide-deck Markdown doc across chapters

```bash
pdf2qti module mod01
```

Reads the `modules` entry with the given `id` from the config, loads the distilled context
(`<outDir>/<id>_context.json`) for every source in its `sourceIds`, and calls the LLM to
produce one combined proto-deck spanning all of them — useful when a Canvas module covers more
than one chapter/PDF. Writes `<outDir>/<module-id>_module.md` (plus an intermediate
`<module-id>_module.json`). Same `--min-slides`/`--max-slides`/`--force` flags as `slides`. Feed
the result to `pptx` the same way as a single-source slide deck.

### `page` — Render HTML from a distilled context

```bash
pdf2qti page \
  --context out/ch01_context.json \
  --output out/ch01_materials.html \
  internal/page/testdata/materials.html.tmpl
```

Notes:

1. `--context` has no short flag; `-c` is reserved for global `--config`.
2. Omit `--output` to write rendered HTML to stdout.

### `publish` — Render and publish Canvas module pages

```bash
pdf2qti publish \
  --course-id 12345 \
  --canvas-base-url https://school.instructure.com \
  --learning-objectives-template internal/page/testdata/learning_objectives.html.tmpl \
  --materials-template internal/page/testdata/materials.html.tmpl
```

For each selected source context (`<outDir>/<id>_context.json`), `publish`:

1. Renders Learning Objectives HTML
2. Renders Materials HTML
3. Upserts both pages in Canvas
4. Ensures a Canvas module exists for the context module name
5. Adds both pages to that module

Set `CANVAS_TOKEN` (or pass `--canvas-token`) before running.

### `import-bank` — Import a QTI ZIP into a Canvas New Quizzes Item Bank

```bash
pdf2qti import-bank \
  --course-id 147966 \
  --bank-name '2120: Chapter 1 Quiz' \
  --package /absolute/path/ch01.zip \
  --on-existing append \
  --create-random-quiz=10
```

Imports a QTI ZIP emitted by `approve` into a Canvas New Quizzes Item Bank through the Canvas UI.
The command validates that the ZIP contains a root `imsmanifest.xml`, a manifest-referenced QTI
assessment XML file, a non-empty assessment title, and at least one question before opening a
browser workflow.

Important flags:

- `--course-id` — Canvas course ID
- `--bank-name` — exact final Item Bank name
- `--package` — QTI ZIP to import
- `--base-url` — Canvas base URL (default: `https://unt.instructure.com`)
- `--on-existing=fail|append` — fail on existing bank, or append into it
- `--create-random-quiz=N` — optionally create an unpublished random New Quiz after import
- `--dry-run` — validate inputs and report the action without changing Canvas
- `--browser-url` — attach to an already-running Chrome remote-debugging session
- `--chrome-profile-dir` — persisted Chrome profile directory used for the Canvas session
- `--username` / `--password` or `CANVAS_USERNAME` / `CANVAS_PASSWORD` — login credentials used only if the saved browser session is missing/expired

Notes:

- `--dry-run` is the safest first run
- Default `--on-existing=fail` avoids modifying an existing bank accidentally
- Canvas may rename the imported Item Bank to the QTI assessment title during import; `import-bank`
  renames it back to `--bank-name` before returning
- `--create-random-quiz=N` rejects values larger than the package's question count
- See [`docs/item-bank-import.md`](docs/item-bank-import.md) for the full workflow and operational details

## Quiz Draft Format

The Markdown quiz draft uses a simple, human-editable format.  
Questions are grouped into typed sections identified by `## <TYPE>` headings.

### Section types

| Section | Question Type    | Description                                                      |
|---------|------------------|------------------------------------------------------------------|
| `TF`    | True/False       | Two-option question; exactly one correct answer (`True`/`False`) |
| `MA`    | Multiple Answer  | Multiple-option question; one or more correct answers            |
| `MC`    | Multiple Choice  | Multiple-option question; exactly one correct answer             |
| `SA`    | Short Answer     | Fill-in-the-blank; one or more acceptable text answers           |
| `ES`    | Essay            | Open-ended text response; manually graded                        |
| `MT`    | Matching         | Match left-side items to right-side answers                      |
| `NR`    | Numerical        | Numeric answer with optional tolerance                           |

### Option markers

| Marker       | Used in         | Meaning                                      |
|--------------|-----------------|----------------------------------------------|
| `[*] text`   | TF, MA, MC      | Correct answer choice                        |
| `[ ] text`   | TF, MA, MC      | Incorrect answer choice                      |
| `[=] text`   | SA, NR          | Accepted answer (SA) or exact value (NR)     |
| `[~] value`  | NR              | Tolerance around the numeric answer          |
| `[>] L = R`  | MT              | Matching pair: left side `L`, right side `R` |

### Example

```markdown
# Chapter 1 Quiz

## TF

1. The capital of France is Paris.
[*] True
[ ] False

## MA

2. Which of the following are primary colors?
[*] Red
[ ] Green
[*] Blue
[ ] Purple

## MC

3. What is the result of 2 + 2?
[ ] 3
[*] 4
[ ] 5
[ ] 6

## SA

4. The chemical symbol for water is ___.
[=] H2O

## ES

5. Describe the water cycle in your own words.

## MT

6. Match each country to its capital.
[>] France = Paris
[>] Germany = Berlin
[>] Spain = Madrid

## NR

7. What is the value of π rounded to two decimal places?
[=] 3.14
[~] 0.005
```

## Development

### Go test conventions

Use these conventions for new or refactored Go tests in this repository:

1. Prefer table-driven tests for multi-case behavior.
2. Use `t.Parallel()` for top-level tests and subtests where isolation allows.
3. Keep test files focused on their production file/function ownership (`foo.go` with `foo_test.go`).
4. Prefer blackbox package tests (`package <name>_test`).
5. If a whitebox test package is required (for unexported helpers), add a short justification comment at file top.

Use the Makefile to run common development tasks.

```bash
# Run all checks (format, vet, lint, test)
make check

# Run tests with coverage
make test

# Build the binary
make build

# Install the binary
make install
```

## License

See the [LICENSE](LICENSE) file for details.
