// Whitebox package: diagramWarnings, fitBox, picXML, and pngDimensions are all unexported, with
// no exported entry point that reaches them without also depending on mmdc being installed (see
// TestRender_MermaidDiagram in pptx_test.go, which skips entirely when mmdc isn't on PATH) — so
// these pure, deterministic pieces of mermaid.go would otherwise have zero coverage on a machine
// without mmdc.
package pptx

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDiagramWarnings_AddAndWarnings(t *testing.T) {
	t.Parallel()

	errUnrenderable := errors.New("mmdc not found on PATH")

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "nil receiver is safe to call add on",
			run: func(t *testing.T) {
				t.Helper()
				var w *diagramWarnings
				w.add("Lifecycle", "flowchart LR\n  A --> B", errUnrenderable) // must not panic
				if got := w.warnings(); got != nil {
					t.Fatalf("got %v, want nil", got)
				}
			},
		},
		{
			name: "nil receiver returns nil warnings",
			run: func(t *testing.T) {
				t.Helper()
				var w *diagramWarnings
				if got := w.warnings(); got != nil {
					t.Fatalf("got %v, want nil", got)
				}
			},
		},
		{
			name: "zero value is ready to use without a constructor",
			run: func(t *testing.T) {
				t.Helper()
				w := &diagramWarnings{}
				w.add("Lifecycle", "flowchart LR\n  A --> B", errUnrenderable)
				got := w.warnings()
				if len(got) != 1 {
					t.Fatalf("got %d warnings, want 1: %v", len(got), got)
				}
				if !strings.Contains(got[0], errUnrenderable.Error()) {
					t.Fatalf("warning %q missing %q", got[0], errUnrenderable.Error())
				}
			},
		},
		{
			name: "warning names which slide failed, not just the error",
			run: func(t *testing.T) {
				t.Helper()
				w := &diagramWarnings{}
				w.add("Order Lifecycle", "flowchart LR\n  A --> B", errUnrenderable)
				got := w.warnings()
				if len(got) != 1 {
					t.Fatalf("got %d warnings, want 1: %v", len(got), got)
				}
				if !strings.Contains(got[0], "Order Lifecycle") {
					t.Fatalf("warning %q missing slide title %q — two different diagrams failing for the "+
						"same reason would otherwise be indistinguishable", got[0], "Order Lifecycle")
				}
			},
		},
		{
			name: "duplicate source is deduped, not reported twice",
			run: func(t *testing.T) {
				t.Helper()
				w := &diagramWarnings{}
				w.add("Lifecycle", "flowchart LR\n  A --> B", errUnrenderable)
				w.add("Lifecycle", "flowchart LR\n  A --> B", errUnrenderable)
				if got := w.warnings(); len(got) != 1 {
					t.Fatalf("got %d warnings, want 1 (deduped): %v", len(got), got)
				}
			},
		},
		{
			name: "different sources are both reported, in call order",
			run: func(t *testing.T) {
				t.Helper()
				w := &diagramWarnings{}
				w.add("Lifecycle", "flowchart LR\n  A --> B", errUnrenderable)
				w.add("Sequence", "sequenceDiagram\n  A->>B: hi", errUnrenderable)
				got := w.warnings()
				if len(got) != 2 {
					t.Fatalf("got %d warnings, want 2: %v", len(got), got)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

func TestFitBox(t *testing.T) {
	t.Parallel()

	box := picBox{x: 100, y: 200, cx: 1000, cy: 1000}

	tests := []struct {
		name           string
		natW, natH     int
		box            picBox
		wantX, wantY   int64
		wantCX, wantCY int64
	}{
		{
			name: "wider-than-tall image is scaled down and letterboxed to fit width",
			natW: 200, natH: 100,
			box:   box,
			wantX: 100, wantY: 200 + (1000-500)/2,
			wantCX: 1000, wantCY: 500,
		},
		{
			name: "image already smaller than box is never upscaled",
			natW: 10, natH: 10,
			box:   picBox{x: 100, y: 200, cx: 500000, cy: 500000},
			wantX: 100 + (500000-int64(10)*emuPerPixel)/2, wantY: 200 + (500000-int64(10)*emuPerPixel)/2,
			wantCX: int64(10) * emuPerPixel, wantCY: int64(10) * emuPerPixel,
		},
		{
			name: "zero natural width falls back to the box unchanged",
			natW: 0, natH: 100,
			box:    box,
			wantX:  box.x,
			wantY:  box.y,
			wantCX: box.cx,
			wantCY: box.cy,
		},
		{
			name: "zero box extent falls back to the box unchanged",
			natW: 200, natH: 100,
			box:    picBox{x: 5, y: 6, cx: 0, cy: 0},
			wantX:  5,
			wantY:  6,
			wantCX: 0,
			wantCY: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotX, gotY, gotCX, gotCY := fitBox(tc.natW, tc.natH, tc.box)
			if gotX != tc.wantX || gotY != tc.wantY || gotCX != tc.wantCX || gotCY != tc.wantCY {
				t.Fatalf("fitBox(%d, %d, %+v) = (%d, %d, %d, %d), want (%d, %d, %d, %d)",
					tc.natW, tc.natH, tc.box, gotX, gotY, gotCX, gotCY, tc.wantX, tc.wantY, tc.wantCX, tc.wantCY)
			}
		})
	}
}

func TestPicXML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       int
		alt      string
		rID      string
		box      picBox
		wantSubs []string
	}{
		{
			name: "renders id, escaped alt, relationship id, and box geometry",
			id:   7,
			alt:  "A client sends a request to an API",
			rID:  "rId3",
			box:  picBox{x: 1, y: 2, cx: 3, cy: 4},
			wantSubs: []string{
				`<p:cNvPr id="7" name="Diagram" descr="A client sends a request to an API"/>`,
				`r:embed="rId3"`,
				`<a:off x="1" y="2"/>`,
				`<a:ext cx="3" cy="4"/>`,
			},
		},
		{
			name: "escapes XML-significant characters in alt text",
			id:   1,
			alt:  `A "quoted" step, X < Y & Y > Z`,
			rID:  "rId1",
			box:  picBox{},
			wantSubs: []string{
				`descr="A &quot;quoted&quot; step, X &lt; Y &amp; Y &gt; Z"`,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := picXML(tc.id, tc.alt, tc.rID, tc.box)
			for _, want := range tc.wantSubs {
				if !strings.Contains(got, want) {
					t.Fatalf("picXML(...) = %q, missing %q", got, want)
				}
			}
		})
	}
}

func TestPngDimensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		png       func() []byte
		wantW     int
		wantH     int
		wantErr   bool
		errSubstr string
	}{
		{
			name: "decodes a well-formed png",
			png: func() []byte {
				img := image.NewRGBA(image.Rect(0, 0, 12, 34))
				img.Set(0, 0, color.RGBA{R: 255, A: 255})
				var buf bytes.Buffer
				if err := png.Encode(&buf, img); err != nil {
					t.Fatalf("encode fixture png: %v", err)
				}
				return buf.Bytes()
			},
			wantW: 12,
			wantH: 34,
		},
		{
			name:      "non-png bytes return an error",
			png:       func() []byte { return []byte("not a png") },
			wantErr:   true,
			errSubstr: "decode png dimensions",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotW, gotH, err := pngDimensions(tc.png())
			if tc.wantErr {
				if err == nil {
					t.Fatal("got nil error, want one")
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("error %q missing %q", err, tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotW != tc.wantW || gotH != tc.wantH {
				t.Fatalf("got (%d, %d), want (%d, %d)", gotW, gotH, tc.wantW, tc.wantH)
			}
		})
	}
}

// TestMermaidRenderer_RenderPNG_CachesDeterministicFailure constructs its own *mermaidRenderer
// rather than going through defaultMermaidRenderer (the package-level singleton every Render call
// shares): that cache persists for the lifetime of the process, so a test asserting "invoked
// exactly once" against the shared instance would pass on a first run and then silently do nothing
// — cache hit, no invocation at all — on every later run in the same process, e.g. `go test
// -count=2`. Not t.Parallel(): mutates PATH via t.Setenv.
func TestMermaidRenderer_RenderPNG_CachesDeterministicFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("writes a POSIX #!/bin/sh script named \"mmdc\": exec.LookPath only resolves it via a PATHEXT extension (.exe/.cmd/.bat) on Windows, which this bare-name file doesn't have")
	}
	dir := t.TempDir()
	counter := filepath.Join(dir, "invocations")
	script := fmt.Sprintf("#!/bin/sh\necho x >> %q\nexit 1\n", counter)
	if err := os.WriteFile(filepath.Join(dir, "mmdc"), []byte(script), 0o700); err != nil { //nolint:gosec // test-local executable stub
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	r := &mermaidRenderer{cache: make(map[string]mermaidRenderResult)}
	const source = "flowchart LR\n  CacheTest1 --> CacheTest2"

	if _, err := r.renderPNG(source); err == nil {
		t.Fatal("got nil error, want the stub's failure")
	}
	if _, err := r.renderPNG(source); err == nil {
		t.Fatal("got nil error on second call, want the cached failure")
	}

	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("read invocation counter: %v", err)
	}
	if got := strings.Count(string(data), "x"); got != 1 {
		t.Fatalf("mmdc invoked %d times for two renderPNG calls with the same failing source, want 1", got)
	}
}
