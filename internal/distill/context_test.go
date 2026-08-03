package distill_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jh125486/pdf2qti/internal/distill"
)

func TestSave_WriteError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Pre-create a directory at the target path, so Save's os.WriteFile fails.
	path := filepath.Join(dir, "context.json")
	if err := os.Mkdir(path, 0o750); err != nil {
		t.Fatal(err)
	}

	err := distill.Save(path, &distill.DistilledContext{SourceID: "src01"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestObjective_UnmarshalJSON_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    distill.Objective
		wantErr bool
	}{
		{name: "numeric co", in: `{"co":1,"text":"a"}`, want: distill.Objective{CO: 1, Text: "a"}},
		{name: "string co", in: `{"co":"1","text":"a"}`, want: distill.Objective{CO: 1, Text: "a"}},
		{name: "string co with whitespace", in: `{"co":" 2 ","text":"b"}`, want: distill.Objective{CO: 2, Text: "b"}},
		{name: "string co with CO prefix", in: `{"co":"CO1","text":"a"}`, want: distill.Objective{CO: 1, Text: "a"}},
		{name: "non-numeric string co", in: `{"co":"not a number","text":"a"}`, wantErr: true},
		{name: "co is an object", in: `{"co":{},"text":"a"}`, wantErr: true},
		{name: "malformed json", in: `{`, wantErr: true},
		{
			// The outer document is syntactically complete (so UnmarshalJSON is actually
			// invoked, unlike the "malformed json" case above which fails at the top-level
			// scan before ever reaching it), but "text" doesn't match its expected type.
			name:    "well-formed but wrong field type",
			in:      `{"co":1,"text":123}`,
			wantErr: true,
		},
		{name: "non-integer float co", in: `{"co":1.5,"text":"a"}`, wantErr: true},
		{
			// Regex finds digits, but they overflow int, so strconv.Atoi itself fails —
			// distinct from "non-numeric string co" above, where the regex finds no digits at
			// all.
			name:    "string co digits overflow int",
			in:      `{"co":"CO99999999999999999999","text":"a"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got distill.Objective
			err := json.Unmarshal([]byte(tt.in), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDistilledContext_UnmarshalJSON_VocabularyAndSectionsTolerance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		in           string
		wantVocabLen int
		wantSectLen  int
		wantErr      bool
	}{
		{
			name:         "vocabulary and sections as arrays",
			in:           `{"vocabulary":[{"term":"Vector","definition":"a"}],"sections":[{"title":"Intro","summary":"s"}]}`,
			wantVocabLen: 1,
			wantSectLen:  1,
		},
		{
			// LLM responses sometimes emit a flat {term: definition} / {title: summary} object
			// instead of the requested array of objects.
			name:         "vocabulary and sections as flat objects",
			in:           `{"vocabulary":{"Vector":"a","Matrix":"b"},"sections":{"Intro":"s1","Ops":"s2"}}`,
			wantVocabLen: 2,
			wantSectLen:  2,
		},
		{
			// The internal-consistency verify step (see verify.go) has been observed returning
			// bare title strings for sections instead of {title, summary} objects.
			name:         "vocabulary and sections as bare string arrays",
			in:           `{"vocabulary":["Vector","Matrix"],"sections":["Intro","Ops","Summary"]}`,
			wantVocabLen: 2,
			wantSectLen:  3,
		},
		{name: "vocabulary is neither array nor object", in: `{"vocabulary":"nope"}`, wantErr: true},
		{name: "sections is neither array nor object", in: `{"sections":"nope"}`, wantErr: true},
		{
			// An array, but neither of objects nor of strings — distinct from the "neither
			// array nor object" cases above.
			name:    "vocabulary is an array of numbers",
			in:      `{"vocabulary":[1,2,3]}`,
			wantErr: true,
		},
		{
			name:    "sections is an array of numbers",
			in:      `{"sections":[1,2,3]}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var dc distill.DistilledContext
			err := json.Unmarshal([]byte(tt.in), &dc)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(dc.Vocabulary) != tt.wantVocabLen {
				t.Fatalf("got %d vocabulary entries, want %d", len(dc.Vocabulary), tt.wantVocabLen)
			}
			if len(dc.Sections) != tt.wantSectLen {
				t.Fatalf("got %d sections, want %d", len(dc.Sections), tt.wantSectLen)
			}
		})
	}
}
