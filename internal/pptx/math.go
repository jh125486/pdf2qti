package pptx

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// reOMath extracts the <m:oMath>...</m:oMath> fragment pandoc produces for a single formula.
var reOMath = regexp.MustCompile(`(?s)<m:oMath\b.*?</m:oMath>`)

// mathConverter converts LaTeX to OOXML Math (OMML) via a pandoc round-trip (markdown -> docx,
// then lifting the <m:oMath> fragment back out) — PowerPoint doesn't understand LaTeX source
// sitting in plain text, but OMML is its native equation format, shared with Word. Successful
// conversions are cached since the same formula often repeats across a deck; pandoc's PATH
// lookup itself is cheap and re-checked on every call rather than cached, so callers degrade
// gracefully (falling back to escaped LaTeX text) whenever pandoc isn't installed or a formula
// fails to convert, without a one-time check baking in a stale answer for the process lifetime.
type mathConverter struct {
	cacheMu sync.Mutex
	cache   map[string]string
}

// defaultMathConverter is the package-level converter used by runsXML.
var defaultMathConverter = &mathConverter{cache: make(map[string]string)}

// toOMML converts a single LaTeX formula (without delimiters) to an <m:oMath> fragment. Leading/
// trailing whitespace is trimmed before wrapping in "$...$": pandoc's markdown reader requires a
// non-space character immediately inside the "$" delimiters to recognize inline math at all (it
// disambiguates from currency, e.g. "$5 and $10") — a space there (routine, since "\( x \)" style
// LaTeX delimiters are often written with padding space) makes pandoc silently treat the whole
// thing as literal text instead: it exits 0 and produces no error, just no <m:oMath> either.
func (c *mathConverter) toOMML(latex string) (string, error) {
	latex = strings.TrimSpace(latex)

	c.cacheMu.Lock()
	cached, ok := c.cache[latex]
	c.cacheMu.Unlock()
	if ok {
		return cached, nil
	}

	pandoc, err := exec.LookPath("pandoc")
	if err != nil {
		return "", fmt.Errorf("pandoc not found on PATH: %w", err)
	}

	cmd := exec.Command(pandoc, "-f", "markdown", "-t", "docx", "-o", "-") //nolint:gosec // pandoc resolved via exec.LookPath, not user input
	cmd.Stdin = strings.NewReader("$" + latex + "$")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run pandoc: %w", err)
	}

	docXML, err := extractDocumentXML(out.Bytes())
	if err != nil {
		return "", err
	}

	omml := reOMath.FindString(docXML)
	if omml == "" {
		return "", fmt.Errorf("no <m:oMath> in pandoc output for %q", latex)
	}

	c.cacheMu.Lock()
	c.cache[latex] = omml
	c.cacheMu.Unlock()
	return omml, nil
}

// extractDocumentXML reads word/document.xml out of a docx (zip) byte stream.
func extractDocumentXML(docx []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		return "", fmt.Errorf("open pandoc docx output: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("open word/document.xml: %w", err)
		}
		data, err := io.ReadAll(rc)
		closeErr := rc.Close()
		if err != nil {
			return "", fmt.Errorf("read word/document.xml: %w", err)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close word/document.xml: %w", closeErr)
		}
		return string(data), nil
	}
	return "", errors.New("word/document.xml not found in pandoc output")
}

// mathRunXML renders a LaTeX formula as inline PowerPoint math (mc:AlternateContent wrapping an
// a14:m/oMathPara/oMath), with the original LaTeX (escaped, delimited) as the mc:Fallback for
// PowerPoint versions predating the math extension. Falls back to plain escaped text entirely if
// conversion fails (pandoc missing, malformed LaTeX, etc.) — same behavior as before this
// feature existed, never a hard error.
func mathRunXML(latex string, delims [2]string) string {
	fallback := runXML(delims[0]+latex+delims[1], false)

	omml, err := defaultMathConverter.toOMML(latex)
	if err != nil {
		return fallback
	}

	return `<mc:AlternateContent xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006">` +
		`<mc:Choice xmlns:a14="http://schemas.microsoft.com/office/drawing/2010/main" Requires="a14">` +
		`<a14:m><m:oMathPara xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math">` + omml + `</m:oMathPara></a14:m>` +
		`</mc:Choice>` +
		`<mc:Fallback>` + fallback + `</mc:Fallback>` +
		`</mc:AlternateContent>`
}
