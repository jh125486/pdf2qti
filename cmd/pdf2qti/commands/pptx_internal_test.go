// Whitebox package: logOutput is unexported, and capturing it is the only way to observe that
// PPTXCmd.Run actually logs pptx.Render's formula-conversion warnings (the blackbox
// TestPPTXCmdRun_Table in pptx_test.go can only observe the rendered output file, not log lines).
package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPPTXCmdRun_LogsRenderWarnings is a justified exception to the single-table-function
// convention used elsewhere in this package: it needs whitebox access to redirect logOutput,
// which the main blackbox table test (TestPPTXCmdRun_Table in pptx_test.go) doesn't have. Not
// t.Parallel(): stubs PATH via t.Setenv to force pandoc unavailable, deterministically producing
// a formula-conversion warning without depending on whether pandoc happens to be installed on
// the machine running the test.
func TestPPTXCmdRun_LogsRenderWarnings(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no pandoc on PATH at all

	var logBuf strings.Builder
	origLogOutput := logOutput
	logOutput = &logBuf
	t.Cleanup(func() { logOutput = origLogOutput })

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "quiz.json")
	cfgJSON := `{"version":1,"courseName":"Test University","sources":[{"id":"src01","pdf":"unused.pdf"}]}`
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	const md = `# Module 1

---

<!-- meta: 1 agenda -->
# Agenda

- Topic A
- Topic B
- Topic C

---

<!-- meta: 2 src01 -->
# Topic A

- See \(x^2 + y^2 = z^2\) for details
`
	slidesPath := filepath.Join(dir, "src01_slides.md")
	if err := os.WriteFile(slidesPath, []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := &PPTXCmd{
		Slides:   slidesPath,
		Template: "../../../internal/pptx/testdata/template.pptx",
		Output:   filepath.Join(dir, "out.pptx"),
	}
	if err := cmd.Run(t.Context(), &CLI{Config: cfgPath}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := logBuf.String()
	if !strings.Contains(got, "pptx render warning") {
		t.Fatalf("expected a logged pptx render warning, got log output: %q", got)
	}
	if !strings.Contains(got, "x^2 + y^2 = z^2") {
		t.Fatalf("expected the logged warning to name the failed formula, got: %q", got)
	}
}
