package distill_test

import (
	"encoding/json"
	"testing"

	"github.com/jh125486/pdf2qti/internal/distill"
)

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
		{name: "vocabulary is neither array nor object", in: `{"vocabulary":"nope"}`, wantErr: true},
		{name: "sections is neither array nor object", in: `{"sections":"nope"}`, wantErr: true},
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
