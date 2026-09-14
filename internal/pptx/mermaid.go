package pptx

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/png" // registers the PNG format with image.DecodeConfig, used to sanity-check mmdc's output
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// mermaidRenderTimeout bounds a single mmdc invocation: mmdc drives a headless Chromium, and a
// malformed diagram or a stalled/crashed browser process would otherwise block Render
// indefinitely — defeating the whole point of the graceful-fallback contract below, which assumes
// a render attempt eventually finishes one way or the other.
const mermaidRenderTimeout = 30 * time.Second

// mermaidRenderResult is one renderPNG outcome cached by source: either a successful PNG, or a
// deterministic render failure (mmdc ran and rejected the source, or produced unusable output).
// It deliberately does NOT represent "mmdc missing from PATH" — see renderPNG.
type mermaidRenderResult struct {
	png []byte
	err error
}

// mermaidRenderer shells out to mmdc (mermaid-cli) to rasterize a Mermaid diagram source into a
// PNG, mirroring mathConverter's (math.go) shape: a package-level, mutex-guarded cache keyed by
// the diagram source, re-checking exec.LookPath("mmdc") on every call rather than caching a
// one-time "missing" verdict, so a caller degrades gracefully the moment mmdc becomes available (or
// unavailable) rather than being stuck with whatever the process's first call happened to observe.
//
// Fallback contract: if mmdc is missing, or a given diagram's source fails to render, the slide
// carrying that diagram is still emitted with its title and caption — just without the picture —
// and the failure is recorded on a diagramWarnings collector (see below). fillDiagramSlide (pptx.go)
// never turns a rendering failure into a hard Render error; a broken or unrenderable diagram is no
// more fatal to a deck than an unconvertible math formula is (see mathWarnings for the same idea
// applied to LaTeX-to-OMML conversion).
type mermaidRenderer struct {
	cacheMu sync.Mutex
	cache   map[string]mermaidRenderResult
}

// defaultMermaidRenderer is the package-level renderer used by fillDiagramSlide.
var defaultMermaidRenderer = &mermaidRenderer{cache: make(map[string]mermaidRenderResult)}

// renderPNG rasterizes source (trimmed mermaid diagram markup) to PNG bytes, transparent
// background, via mmdc. A successful render is always cached by source text; a failure is cached
// only when runMmdc reports it as cacheable (see its doc comment) — package-level sharing is safe
// for the same reason mathConverter's cache is: the same source always produces the same outcome
// from a given mmdc install, and a hit from an unrelated Render call is still a correct answer.
// Caching a deterministic failure, not just a success, matters for a deck repeating one broken
// diagram across several slides: without it, every one of those slides would re-run (and re-wait
// out mermaidRenderTimeout for) a render that's already known to fail the same way.
//
// "mmdc not found on PATH" is deliberately NOT cached, unlike a deterministic runMmdc failure:
// that's an environment condition, not a property of source, and re-checking it every call is what
// lets a caller degrade gracefully the moment mmdc becomes available mid-process instead of being
// stuck with whatever the first call happened to observe.
func (r *mermaidRenderer) renderPNG(source string) ([]byte, error) {
	source = strings.TrimSpace(source)

	r.cacheMu.Lock()
	cached, ok := r.cache[source]
	r.cacheMu.Unlock()
	if ok {
		return cached.png, cached.err
	}

	mmdc, err := exec.LookPath("mmdc")
	if err != nil {
		return nil, fmt.Errorf("mmdc not found on PATH: %w", err)
	}

	png, err, cacheable := r.runMmdc(mmdc, source)
	if cacheable {
		r.cacheMu.Lock()
		r.cache[source] = mermaidRenderResult{png: png, err: err}
		r.cacheMu.Unlock()
	}
	return png, err
}

// runMmdc does the actual rasterization: write source to a temp .mmd file, invoke mmdc on it, and
// validate the PNG it produced. Split out of renderPNG so the "mmdc missing" early return above
// stays outside what gets cached (see renderPNG's doc comment).
//
// cacheable is true only for a failure that's a genuine, deterministic property of source given
// this mmdc install — mmdc ran and rejected the diagram, or produced output that isn't a valid PNG
// — since retrying the identical source against the identical mmdc binary would fail the identical
// way. It's false for every other failure path: creating a temp dir, writing the input file, or
// reading mmdc's output file failing are host/environment conditions (disk full, permissions, ...)
// unrelated to source, and a timeout is explicitly NOT a property of source either — a diagram that
// times out once under load might render fine on a later, less contended attempt, so caching that
// as a permanent failure would wrongly and silently downgrade every later slide using it to the
// caption-only fallback for the rest of the process.
func (r *mermaidRenderer) runMmdc(mmdc, source string) (png []byte, err error, cacheable bool) {
	dir, err := os.MkdirTemp("", "pdf2qti-mermaid-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir for mermaid render: %w", err), false
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "in.mmd")
	outPath := filepath.Join(dir, "out.png")
	if err := os.WriteFile(inPath, []byte(source), 0o600); err != nil {
		return nil, fmt.Errorf("write mermaid source: %w", err), false
	}

	ctx, cancel := context.WithTimeout(context.Background(), mermaidRenderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, mmdc, "-i", inPath, "-o", outPath, "-b", "transparent", "-s", "2") //nolint:gosec // mmdc resolved via exec.LookPath, not user input
	// WaitDelay bounds Wait() itself, not just the ctx.Done() signal: mmdc spawns a headless
	// Chromium that inherits the CombinedOutput pipe, so on ctx cancellation, killing only the
	// direct mmdc process leaves that grandchild holding the pipe open — CombinedOutput would
	// otherwise keep blocking on it well past mermaidRenderTimeout, exactly the indefinite hang
	// this timeout exists to prevent. WaitDelay forces the pipe closed (and I/O errors returned)
	// this long after the process is signaled, regardless of what still has it open.
	cmd.WaitDelay = 5 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("run mmdc: timed out after %s: %w", mermaidRenderTimeout, ctx.Err()), false
		}
		return nil, fmt.Errorf("run mmdc: %w: %s", err, strings.TrimSpace(string(out))), true
	}

	png, err = os.ReadFile(outPath) //nolint:gosec // outPath is our own temp file, not user input
	if err != nil {
		return nil, fmt.Errorf("read mermaid output png: %w", err), false
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(png)); err != nil {
		return nil, fmt.Errorf("mmdc output is not a valid png: %w", err), true
	}
	return png, nil, true
}

// diagramWarnings collects diagrams that failed to render to PNG during one Render call, deduped
// by source text — a near-copy of mathWarnings (math.go), for the identical reason: this package's
// tests run many Render calls concurrently via t.Parallel(), so a shared, package-level collector
// would race between them (one call's collection could steal or clear warnings a concurrently-
// running call hadn't reported yet). Scoped to a single Render call instead, threaded down through
// applyDeck -> ... -> fillDiagramSlide. The zero value is ready to use; a nil *diagramWarnings is
// also safe to call add/warnings on.
type diagramWarnings struct {
	mu   sync.Mutex
	seen map[string]bool
	list []string
}

// add records source as having failed to render, with slideTitle and err's message, unless the
// same source was already recorded on this collector — so an author with two different diagrams
// failing for the same underlying reason (e.g. mmdc missing) still gets two distinguishable
// warnings naming which slide needs attention, not two identical, unattributed ones. Deduping
// stays keyed on source rather than slideTitle: an identical diagram repeated verbatim on several
// slides (see mediaBySource in pptx.go) only needs reporting once. Appends in call order, matching
// mathWarnings.add.
func (w *diagramWarnings) add(slideTitle, source string, err error) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seen == nil {
		w.seen = make(map[string]bool)
	}
	if w.seen[source] {
		return
	}
	w.seen[source] = true
	w.list = append(w.list, fmt.Sprintf("diagram on slide %q failed to render, slide emitted without its picture: %v", slideTitle, err))
}

// warnings returns every diagram-render failure recorded so far, in the order first encountered.
func (w *diagramWarnings) warnings() []string {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.list
}

// pngDimensions returns png's pixel width and height, for fitBox to convert to EMU.
func pngDimensions(png []byte) (width, height int, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(png))
	if err != nil {
		return 0, 0, fmt.Errorf("decode png dimensions: %w", err)
	}
	return cfg.Width, cfg.Height, nil
}

// picBox is a picture placeholder's layout-level box, in EMU, as read off a slide layout's
// <a:xfrm> (see picPlaceholderBox in pptx.go).
type picBox struct {
	x, y   int64
	cx, cy int64
}

// emuPerPixel converts a PNG's pixel dimensions to EMU assuming 96 DPI (PowerPoint's own default
// for raster images with no embedded DPI metadata) — 914400 EMU per inch / 96 px per inch.
const emuPerPixel = 9525

// fitBox computes the offset and extent (EMU) to draw a natW x natH (pixels) image centered inside
// box, scaled down (never up — mmdc's "-s 2" output is already large; upscaling would just blur it
// further) to fit both dimensions.
func fitBox(natW, natH int, box picBox) (offX, offY, cx, cy int64) {
	natEMUW := int64(natW) * emuPerPixel
	natEMUH := int64(natH) * emuPerPixel
	if natEMUW <= 0 || natEMUH <= 0 || box.cx <= 0 || box.cy <= 0 {
		return box.x, box.y, box.cx, box.cy
	}

	scale := min(float64(box.cx)/float64(natEMUW), float64(box.cy)/float64(natEMUH), 1.0)

	cx = int64(float64(natEMUW) * scale)
	cy = int64(float64(natEMUH) * scale)
	offX = box.x + (box.cx-cx)/2
	offY = box.y + (box.cy-cy)/2
	return offX, offY, cx, cy
}

// xmlAttrReplacer escapes text for placement inside a double-quoted XML attribute value — the same
// characters xmlTextReplacer escapes for element text (including "{"/"}", to defuse writeEntry's
// later Go text/template pass over the whole part — see xmlTextReplacer's doc comment), plus '"'
// itself, which xmlTextReplacer doesn't need to touch since <a:t> element content never sits
// inside quotes.
var xmlAttrReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "{", "&#123;", "}", "&#125;", `"`, "&quot;")

// picXML renders a <p:pic> element embedding a diagram image: id must be unique within the
// slide's spTree, alt is the screen-reader description (escaped for an XML attribute), rID is the
// slide's own relationship id for the image part, and box is the EMU position/size to draw it at
// (see fitBox).
func picXML(id int, alt, rID string, box picBox) string {
	return fmt.Sprintf(
		`<p:pic><p:nvPicPr><p:cNvPr id="%d" name="Diagram" descr="%s"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr>`+
			`<p:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></p:blipFill>`+
			`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>`,
		id, xmlAttrReplacer.Replace(alt), rID, box.x, box.y, box.cx, box.cy)
}
