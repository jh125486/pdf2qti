# pdf2qti

[![Go Reference](https://pkg.go.dev/badge/github.com/jh125486/pdf2qti)](https://pkg.go.dev/github.com/jh125486/pdf2qti)
[![Tests](https://github.com/jh125486/pdf2qti/actions/workflows/test.yaml/badge.svg)](https://github.com/jh125486/pdf2qti/actions/workflows/test.yaml)
[![CodeQL](https://github.com/jh125486/pdf2qti/actions/workflows/codeql.yml/badge.svg)](https://github.com/jh125486/pdf2qti/actions/workflows/codeql.yml)
[![Codecov](https://codecov.io/gh/jh125486/pdf2qti/branch/main/graph/badge.svg)](https://codecov.io/gh/jh125486/pdf2qti)

A CLI tool that converts PDF sources into Canvas-compatible QTI quizzes using LLM-assisted question generation.

## Overview

`pdf2qti` automates the creation of course content for Canvas LMS from existing PDF materials. It:

- **Extracts** text from PDF documents
- **Distills** each PDF into structured context JSON (concepts, examples, LaTeX-bearing prose) via an LLM provider — the shared input for everything below
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
      "titleTemplate": "Chapter {{.chapter}} Quiz",
      "counts": { "tf": 5, "ma": 3, "mc": 10 },
      "mcOptions": { "min": 4, "max": 4 },
      "maOptions": { "min": 4, "max": 6 }
    },
    "generation": {
      "provider": "openai",
      "model": "gpt-4o",
      "apiKeyEnv": "OPENAI_API_KEY",
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
| `defaults` | Global defaults for `quiz`, `generation`, `validation`, and `workflow` |
| `sources` | Array of PDF sources; each source can override any default |
| `modules` | Array of modules grouping one or more `sources` (by `sourceIds`) for the `module` command |

Each `source` requires at minimum an `id` and a `pdf` path. All other fields inherit from `defaults`.

Each `module` requires an `id`, a `name`, and `sourceIds` (one or more `source.id` values it spans).

`defaults.workflow.outDir` is where every generated artifact lands: `<id>_context.json` (`distill`), `<id>_quiz.md`/`<id>.zip` (`generate`/`approve`), and `<id>_slides.md` (`slides`). Point it wherever you want that scratch/output tree to live — e.g. `"ctx"` if you'd rather keep it out of an `out/` you use for something else.

## Usage

```
pdf2qti [--config <file>] <command>
```

`--config` / `-c` defaults to `quiz_input.json`.

### Pipeline overview

`distill` is the shared first step for everything else — it's the only command that reads the
PDF. Every other command reads the `<outDir>/<id>_context.json` it produces.

```
                              ┌─→ generate → validate → approve → <id>.zip
pdf.pdf → distill → context.json
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

### `distill` — Extract a PDF into structured context JSON

```bash
pdf2qti distill ch01 [ch02 ...]   # specific sources
pdf2qti distill --all             # every source in the config
```

For each requested source, `distill` extracts the PDF text, calls the configured LLM to
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
LaTeX math is written inline as `\(...\)` and `\begin{bmatrix}...\end{bmatrix}` — these are hand
-editable Markdown, so review/tweak content before rendering to PPTX. Writes
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
