package extract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/extract"
)

func TestReadMarkdown(t *testing.T) {
	t.Parallel()
	const document = "# Chapter\nPreamble\n\n## Section 18.8 - First\nFirst body\n### Detail\nNested body\n\n## Section 18.9 - Second\nSecond body\n"
	tests := []struct {
		name     string
		texts    []string
		sections []string
		preamble bool
		want     []string
		absent   []string
		errLike  string
	}{
		{name: "all sections exclude preamble", texts: []string{document}, want: []string{"First body", "Nested body", "Second body"}, absent: []string{"Preamble", "# Chapter\n"}},
		{name: "numeric selector", texts: []string{document}, sections: []string{"18.8"}, want: []string{"## Section 18.8 - First", "### Detail\nNested body"}, absent: []string{"Preamble", "Second body"}},
		{name: "exact heading selector", texts: []string{document}, sections: []string{"Section 18.9 - Second"}, want: []string{"Second body"}, absent: []string{"First body"}},
		{name: "document order", texts: []string{document}, sections: []string{"18.9", "18.8"}, want: []string{"First body", "Second body"}},
		{name: "preamble included", texts: []string{document}, sections: []string{"18.9"}, preamble: true, want: []string{"# Chapter\nPreamble", "Second body"}, absent: []string{"First body"}},
		{name: "multiple files retain order", texts: []string{document, "## Extra\nLast file\n"}, want: []string{"First body", "Second body", "Last file"}},
		{name: "unknown selector", texts: []string{document}, sections: []string{"18.10"}, errLike: `unknown section "18.10"; available sections: [Section 18.8 - First Section 18.9 - Second]`},
		{name: "fenced headings preserved", texts: []string{"## Section 1.1 - Code\n```md\n## Section 1.2 - Fake\n```\n~~~md\n## Also fake\n~~~\n## Section 1.3 - End\nEnd\n"}, sections: []string{"1.1"}, want: []string{"## Section 1.2 - Fake", "## Also fake"}, absent: []string{"End"}},
		{name: "list tilde fence keeps headings literal", texts: []string{"## Section 1.1 - Code\n- ~~~md\n  ## Example\n  ~~~\nAfter code\n## Section 1.2 - Following\nFollowing body\n"}, sections: []string{"1.1"}, want: []string{"- ~~~md", "  ## Example", "After code"}, absent: []string{"Following body", "Section 1.2"}},
		{name: "section after list tilde fence stays selectable", texts: []string{"## Section 1.1 - Code\n- ~~~md\n  ## Example\n  ~~~\nAfter code\n## Section 1.2 - Following\nFollowing body\n"}, sections: []string{"1.2"}, want: []string{"## Section 1.2 - Following", "Following body"}, absent: []string{"## Example", "After code"}},
		{name: "numbered list backtick fence keeps headings literal", texts: []string{"## Section 1.1 - Code\n10. ```md\n    ## Example\n    ```\nAfter code\n## Section 1.2 - Following\nFollowing body\n"}, sections: []string{"1.1"}, want: []string{"10. ```md", "    ## Example", "After code"}, absent: []string{"Following body"}},
		{name: "section after numbered list fence stays selectable", texts: []string{"## Section 1.1 - Code\n10. ```md\n    ## Example\n    ```\nAfter code\n## Section 1.2 - Following\nFollowing body\n"}, sections: []string{"1.2"}, want: []string{"## Section 1.2 - Following", "Following body"}, absent: []string{"## Example", "After code"}},
		{name: "nested list fence keeps headings literal", texts: []string{"## Section 1.1 - Code\n- - ~~~md\n    ## Example\n    ~~~\nAfter code\n## Section 1.2 - Following\nFollowing body\n"}, sections: []string{"1.1"}, want: []string{"- - ~~~md", "    ## Example", "After code"}, absent: []string{"Following body"}},
		{name: "section after nested list fence stays selectable", texts: []string{"## Section 1.1 - Code\n- - ~~~md\n    ## Example\n    ~~~\nAfter code\n## Section 1.2 - Following\nFollowing body\n"}, sections: []string{"1.2"}, want: []string{"## Section 1.2 - Following", "Following body"}, absent: []string{"## Example", "After code"}},

		{name: "dedent ends list fence before following section", texts: []string{"## Section 1.1 - Code\n- ~~~md\n  code\n\n## Section 1.2 - Following\nFollowing body\n"}, sections: []string{"1.2"}, want: []string{"## Section 1.2 - Following", "Following body"}, absent: []string{"- ~~~md", "  code"}},
		{name: "dedent excludes following section from list fence", texts: []string{"## Section 1.1 - Code\n- ~~~md\n  code\n\n## Section 1.2 - Following\nFollowing body\n"}, sections: []string{"1.1"}, want: []string{"- ~~~md", "  code"}, absent: []string{"Section 1.2", "Following body"}},
		{name: "blank line preserves active list fence", texts: []string{"## Section 1.1 - Code\n- ~~~md\n  code\n\n  ## Example\n  ~~~\nAfter code\n## Section 1.2 - Following\nFollowing body\n"}, sections: []string{"1.1"}, want: []string{"- ~~~md", "  ## Example", "After code"}, absent: []string{"Following body"}},
		{name: "dedent reprocesses new top level fence", texts: []string{"## Section 1.1 - Code\n- ~~~md\n  code\n\n```md\n## Example\n```\nAfter code\n## Section 1.2 - Following\nFollowing body\n"}, sections: []string{"1.1"}, want: []string{"- ~~~md", "## Example", "After code"}, absent: []string{"Following body"}},
		{name: "section after dedented top level fence stays selectable", texts: []string{"## Section 1.1 - Code\n- ~~~md\n  code\n\n```md\n## Example\n```\nAfter code\n## Section 1.2 - Following\nFollowing body\n"}, sections: []string{"1.2"}, want: []string{"## Section 1.2 - Following", "Following body"}, absent: []string{"## Example", "After code"}},

		{name: "closing hashes and CRLF", texts: []string{"# Title\r\n## Section 1.1 - C# ###\r\nBody\r\n"}, sections: []string{"Section 1.1 - C#"}, want: []string{"Body"}},
		{name: "top level heading ends section", texts: []string{"## Section 1.1 - First\nBody\n# New chapter\nExcluded\n"}, sections: []string{"1.1"}, want: []string{"Body"}, absent: []string{"Excluded", "New chapter"}},
		{name: "empty H1 ends selected section", texts: []string{"## Section 1.1 - First\nBody\n#\nExcluded\n"}, sections: []string{"1.1"}, want: []string{"Body"}, absent: []string{"Excluded"}},
		{name: "empty H2 ends selected section", texts: []string{"## Section 1.1 - First\nBody\n##\nExcluded\n"}, sections: []string{"1.1"}, want: []string{"Body"}, absent: []string{"Excluded"}},
		{name: "blank H2 ends selected section", texts: []string{"## Section 1.1 - First\nBody\n## \t\nExcluded\n"}, sections: []string{"1.1"}, want: []string{"Body"}, absent: []string{"Excluded"}},

		{name: "plain file preamble requested", texts: []string{"Plain notes"}, preamble: true, want: []string{"Plain notes"}},
		{name: "plain file excludes preamble", texts: []string{"Plain notes"}, errLike: "no selected text"},
		{name: "empty input", texts: []string{""}, errLike: "no selected text"},
		{name: "missing file", errLike: "read Markdown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			files := make([]config.ContextFile, 0, len(tt.texts))
			for i, text := range tt.texts {
				path := filepath.Join(dir, string(rune('a'+i))+".md")
				if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
				files = append(files, config.ContextFile{Path: path, Sections: tt.sections, IncludePreamble: tt.preamble})
			}
			if len(files) == 0 {
				files = append(files, config.ContextFile{Path: filepath.Join(dir, "missing.md")})
			}
			got, err := extract.ReadMarkdown(files)
			if tt.errLike != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errLike) {
					t.Fatalf("error = %v; want %q", err, tt.errLike)
				}
				if len(tt.sections) > 0 && !strings.Contains(err.Error(), files[0].Path) {
					t.Fatalf("error lacks path: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			last := -1
			for _, want := range tt.want {
				index := strings.Index(got, want)
				if index < 0 || index <= last {
					t.Fatalf("missing or out of order %q in %q", want, got)
				}
				last = index
			}
			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("unexpected %q in %q", absent, got)
				}
			}
		})
	}
}
