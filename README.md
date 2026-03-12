# pdf2qti

[![Go Reference](https://pkg.go.dev/badge/github.com/jh125486/pdf2qti)](https://pkg.go.dev/github.com/jh125486/pdf2qti)
[![Go Report](https://goreportcard.com/badge/github.com/jh125486/pdf2qti)](https://goreportcard.com/report/github.com/jh125486/pdf2qti)
[![Tests](https://github.com/jh125486/pdf2qti/actions/workflows/test.yaml/badge.svg)](https://github.com/jh125486/pdf2qti/actions/workflows/test.yaml)
[![CodeQL](https://github.com/jh125486/pdf2qti/actions/workflows/codeql.yml/badge.svg)](https://github.com/jh125486/pdf2qti/actions/workflows/codeql.yml)
[![Codecov](https://codecov.io/gh/jh125486/pdf2qti/branch/main/graph/badge.svg)](https://codecov.io/gh/jh125486/pdf2qti)

A CLI tool that converts PDF sources into Canvas-compatible QTI quizzes using LLM-assisted question generation.

## Overview

`pdf2qti` automates the creation of quiz content for Canvas LMS from existing PDF materials. It:

- **Extracts** text from PDF documents
- **Generates** quiz questions (True/False, Multiple Answer, Multiple Choice, Short Answer, Essay, Matching, Numerical) via an LLM provider
- **Renders** a human-reviewable Markdown draft for editing and approval
- **Converts** approved drafts to QTI 1.2 XML ready for Canvas import

## Features

- Seven question types: **True/False (TF)**, **Multiple Answer (MA)**, **Multiple Choice (MC)**, **Short Answer (SA)**, **Essay (ES)**, **Matching (MT)**, **Numerical (NR)**
- Configurable LLM provider, model, temperature, and API key
- Per-source and global-default configuration via a JSON config file (with JSON Schema)
- Validation rules (unique options, sequential numbering, correct-answer density, etc.)
- Audit logging for every operation
- Cross-platform releases for Linux, macOS, and Windows (amd64 and arm64)

## Installation

### Using `go install`

```bash
go install github.com/jh125486/pdf2qti/cmd/pdf2qti@latest
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
      "temperature": 0.7
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
  ]
}
```

### Configuration fields

| Field | Description |
|---|---|
| `version` | Config schema version (must be `1`) |
| `defaults` | Global defaults for `quiz`, `generation`, `validation`, and `workflow` |
| `sources` | Array of PDF sources; each source can override any default |

Each `source` requires at minimum an `id` and a `pdf` path. All other fields inherit from `defaults`.

## Usage

```
pdf2qti [--config <file>] <command>
```

`--config` / `-c` defaults to `quiz_input.json`.

### `generate` — Extract PDF and create a quiz draft

```bash
pdf2qti generate [--skip-approve]
```

For each source in the config, `generate`:
1. Extracts text from the PDF and writes `<outDir>/<id>_context.md`
2. Calls the configured LLM to produce TF, MA, and MC questions
3. Writes a Markdown quiz draft to `<outDir>/<id>_quiz.md` for human review

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

Reads the approved `<outDir>/<id>_quiz.md` and writes a Canvas-compatible QTI 1.2 XML file to `<outDir>/<id>.qti`.

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