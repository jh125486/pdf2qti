package render_test

import (
	"strings"
	"testing"

	"github.com/jh125486/pdf2qti/internal/render"
)

func TestRenderDraft_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		draft      *render.QuizDraft
		wantTokens []string
		wantAbsent []string
	}{
		{
			name: "all classic types",
			draft: &render.QuizDraft{
				Title:       "Test Quiz",
				Description: "A test description",
				TFQuestions: []render.Question{
					{Number: 1, Text: "Is the sky blue?", Options: []render.Option{
						{Text: "True", IsCorrect: true},
						{Text: "False", IsCorrect: false},
					}},
				},
				MAQuestions: []render.Question{
					{Number: 2, Text: "Which are colors?", Options: []render.Option{
						{Text: "Red", IsCorrect: true},
						{Text: "Dog", IsCorrect: false},
						{Text: "Blue", IsCorrect: true},
					}},
				},
				MCQuestions: []render.Question{
					{Number: 3, Text: "What is 2+2?", Options: []render.Option{
						{Text: "3", IsCorrect: false},
						{Text: "4", IsCorrect: true},
						{Text: "5", IsCorrect: false},
					}},
				},
			},
			wantTokens: []string{
				"# Test Quiz", "A test description",
				"## TF", "## MA", "## MC",
				"[*] True", "[ ] False",
			},
		},
		{
			name: "no description",
			draft: &render.QuizDraft{
				Title: "No Desc",
				MCQuestions: []render.Question{
					{Number: 1, Text: "Q?", Options: []render.Option{{Text: "A", IsCorrect: true}}},
				},
			},
			wantTokens: []string{"# No Desc"},
			wantAbsent: []string{"## TF", "## MA"},
		},
		{
			name: "new question types",
			draft: &render.QuizDraft{
				Title: "New Types Quiz",
				SAQuestions: []render.Question{
					{Number: 1, Text: "The capital of France is ___.", Options: []render.Option{
						{Text: "Paris", IsCorrect: true},
					}},
				},
				ESQuestions: []render.Question{
					{Number: 2, Text: "Describe the water cycle."},
				},
				MTQuestions: []render.Question{
					{Number: 3, Text: "Match each country to its capital.", Options: []render.Option{
						{Text: "France", IsCorrect: true, MatchText: "Paris"},
						{Text: "Germany", IsCorrect: true, MatchText: "Berlin"},
					}},
				},
				NRQuestions: []render.Question{
					{Number: 4, Text: "What is 2+2?", Options: []render.Option{
						{Text: "4", IsCorrect: true},
						{Text: "0.5", IsCorrect: false},
					}},
				},
			},
			wantTokens: []string{
				"## SA", "[=] Paris",
				"## ES", "Describe the water cycle.",
				"## MT", "[>] France = Paris", "[>] Germany = Berlin",
				"## NR", "[=] 4", "[~] 0.5",
			},
		},
		{
			name:       "empty draft",
			draft:      &render.QuizDraft{Title: "Empty"},
			wantTokens: []string{"# Empty"},
			wantAbsent: []string{"## TF", "## MA", "## MC", "## SA", "## ES", "## MT", "## NR"},
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			md, err := render.RenderDraft(tt.draft)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.wantTokens {
				if !strings.Contains(md, want) {
					t.Errorf("missing %q in rendered output:\n%s", want, md)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(md, absent) {
					t.Errorf("unexpected %q present in rendered output:\n%s", absent, md)
				}
			}
		})
	}
}

func TestParseDraft_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		md      string
		check   func(t *testing.T, d *render.QuizDraft)
		wantErr bool
	}{
		{
			name: "round trip classic types",
			md: mustRender(t, &render.QuizDraft{
				Title:       "Round Trip Quiz",
				Description: "Description here",
				TFQuestions: []render.Question{
					{Number: 1, Text: "TF question one?", Options: []render.Option{
						{Text: "True", IsCorrect: true},
						{Text: "False", IsCorrect: false},
					}},
				},
				MCQuestions: []render.Question{
					{Number: 2, Text: "MC question two?", Options: []render.Option{
						{Text: "A", IsCorrect: true},
						{Text: "B", IsCorrect: false},
					}},
				},
			}),
			check: checkClassicRoundTrip,
		},
		{
			name: "round trip new types",
			md: mustRender(t, &render.QuizDraft{
				Title: "New Types Round Trip",
				SAQuestions: []render.Question{
					{Number: 1, Text: "Capital of Italy?", Options: []render.Option{{Text: "Rome", IsCorrect: true}}},
				},
				ESQuestions: []render.Question{
					{Number: 2, Text: "Explain photosynthesis."},
				},
				MTQuestions: []render.Question{
					{Number: 3, Text: "Match the pairs.", Options: []render.Option{
						{Text: "A", IsCorrect: true, MatchText: "1"},
						{Text: "B", IsCorrect: true, MatchText: "2"},
					}},
				},
				NRQuestions: []render.Question{
					{Number: 4, Text: "Value of pi rounded to 2 decimals?", Options: []render.Option{
						{Text: "3.14", IsCorrect: true},
						{Text: "0.005", IsCorrect: false},
					}},
				},
			}),
			check: checkNewTypesRoundTrip,
		},
		{
			name: "non-numeric question line ignored",
			md:   "# Quiz\n\n## MC\n\nab. Not a real question\n",
			check: func(t *testing.T, d *render.QuizDraft) {
				t.Helper()
				if len(d.MCQuestions) != 0 {
					t.Errorf("expected 0 MC questions, got %d", len(d.MCQuestions))
				}
			},
		},
		{
			name: "unrecognized section ignored",
			md:   "# Quiz\n\n## UNKNOWN\n\n1. Some text\n[*] A\n",
			check: func(t *testing.T, d *render.QuizDraft) {
				t.Helper()
				if len(d.MCQuestions) != 0 || len(d.TFQuestions) != 0 {
					t.Errorf("expected no questions parsed for unknown section, got %+v", d)
				}
			},
		},
		{
			name: "empty markdown",
			md:   "",
			check: func(t *testing.T, d *render.QuizDraft) {
				t.Helper()
				if d.Title != "" {
					t.Errorf("expected empty title, got %q", d.Title)
				}
			},
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, err := render.ParseDraft(tt.md)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, d)
			}
		})
	}
}

func TestExecuteTemplate_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tmpl     string
		data     any
		wantErr  bool
		expected string
	}{
		{name: "simple map", tmpl: "Chapter {{.chapter}}", data: map[string]any{"chapter": 5}, expected: "Chapter 5"},
		{name: "no vars", tmpl: "Hello World", data: nil, expected: "Hello World"},
		{name: "invalid template syntax", tmpl: "{{.unclosed", wantErr: true},
		{name: "execute error", tmpl: "{{call .}}", data: 42, wantErr: true},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := render.ExecuteTemplate(tt.tmpl, tt.data)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func mustRender(t *testing.T, d *render.QuizDraft) string {
	t.Helper()
	md, err := render.RenderDraft(d)
	if err != nil {
		t.Fatalf("render error: %v", err)
	}
	return md
}

func checkClassicRoundTrip(t *testing.T, d *render.QuizDraft) {
	t.Helper()
	if d.Title != "Round Trip Quiz" {
		t.Errorf("title: got %q", d.Title)
	}
	if d.Description != "Description here" {
		t.Errorf("description: got %q", d.Description)
	}
	if len(d.TFQuestions) != 1 || len(d.MCQuestions) != 1 {
		t.Errorf("counts: TF=%d MC=%d", len(d.TFQuestions), len(d.MCQuestions))
	}
}

func checkNewTypesRoundTrip(t *testing.T, d *render.QuizDraft) {
	t.Helper()
	if len(d.SAQuestions) != 1 || len(d.SAQuestions[0].Options) == 0 {
		t.Fatalf("SA: got %+v", d.SAQuestions)
	}
	if d.SAQuestions[0].Options[0].Text != "Rome" {
		t.Errorf("SA: got %+v", d.SAQuestions)
	}
	if len(d.ESQuestions) != 1 {
		t.Errorf("ES count: got %d", len(d.ESQuestions))
	}
	if len(d.MTQuestions) != 1 || len(d.MTQuestions[0].Options) != 2 {
		t.Errorf("MT: got %+v", d.MTQuestions)
	}
	if len(d.NRQuestions) != 1 || len(d.NRQuestions[0].Options) != 2 {
		t.Errorf("NR: got %+v", d.NRQuestions)
		return
	}
	opts := d.NRQuestions[0].Options
	if opts[0].Text != "3.14" || !opts[0].IsCorrect {
		t.Errorf("NR answer: got %+v", opts[0])
	}
	if opts[1].Text != "0.005" || opts[1].IsCorrect {
		t.Errorf("NR tolerance: got %+v", opts[1])
	}
}
