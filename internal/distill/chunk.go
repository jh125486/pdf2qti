package distill

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
)

// maxDirectChars is the largest chapterText we'll send to the LLM in a single distill call.
// Above this, Distill map-reduces the text through condenseChunks first. Sized conservatively
// (chars/token ~3, well below the observed ~3.9 for this material) to leave headroom for the
// prompt template, course objectives, and a thick completion (the "text" field alone can run
// several thousand tokens) under a 30K-TPM-class rate limit.
const maxDirectChars = 70000

// chunkChars is the target size of each chunk fed to condenseChunk. Sized so a chunk's prompt
// plus its (much shorter) digest completion comfortably fits a 30K-TPM-class rate limit even
// stacked with per-minute overhead from adjacent calls.
const chunkChars = 45000

// condenseChunks map-reduces a large chapterText into a shorter, still information-dense text
// suitable for the main distill prompt: it splits text into chunkChars-ish pieces on paragraph
// boundaries, asks the LLM to extract the testable substance of each piece, and joins the
// resulting digests back into one string.
func condenseChunks(ctx context.Context, llm LLM, text string) (string, error) {
	return condenseChunksSize(ctx, llm, text, chunkChars)
}

// condenseChunksSize is condenseChunks with an explicit chunk size, split out for testability.
func condenseChunksSize(ctx context.Context, llm LLM, text string, size int) (string, error) {
	chunks := splitIntoChunks(text, size)
	digests := make([]string, len(chunks))
	for i, chunk := range chunks {
		prompt, err := buildChunkPrompt(i+1, len(chunks), chunk)
		if err != nil {
			return "", fmt.Errorf("build chunk prompt %d/%d: %w", i+1, len(chunks), err)
		}
		digest, err := llm.Complete(ctx, prompt, nil)
		if err != nil {
			return "", fmt.Errorf("condense chunk %d/%d: %w", i+1, len(chunks), err)
		}
		digests[i] = digest
	}
	return strings.Join(digests, "\n\n"), nil
}

// splitIntoChunks packs text's paragraphs (split on blank lines) into pieces no larger than
// size, without splitting a paragraph across chunks — unless a single paragraph itself exceeds
// size, in which case it's hard-split at size boundaries so no chunk ever exceeds size.
func splitIntoChunks(text string, size int) []string {
	paras := strings.Split(text, "\n\n")

	var chunks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			chunks = append(chunks, cur.String())
			cur.Reset()
		}
	}
	for _, p := range paras {
		switch {
		case len(p) > size:
			flush()
			for len(p) > size {
				chunks = append(chunks, p[:size])
				p = p[size:]
			}
			if p != "" {
				cur.WriteString(p)
			}
		case cur.Len()+len(p)+2 > size:
			flush()
			cur.WriteString(p)
		default:
			if cur.Len() > 0 {
				cur.WriteString("\n\n")
			}
			cur.WriteString(p)
		}
	}
	flush()
	return chunks
}

// chunkPromptTmpl is the LLM prompt template for map-reduce pre-condensing of one chunk of a
// large chapter, ahead of the main distill synthesis prompt.
var chunkPromptTmpl = template.Must(template.New("chunk").Parse(`You are pre-processing part {{.Index}} of {{.Total}} of a textbook chapter. A later step will
synthesize your notes from ALL parts into a final structured summary — your notes are that
step's ONLY source for this excerpt's content, so do not lose technical detail. This is NOT a
summarization task: your output should be comprehensive and thorough, close in length to the
excerpt itself, not a shortened paraphrase of it. Omitting a fact here means it is permanently
lost from the final result.

Write dense plain-prose notes (no JSON, no meta-commentary about this being a partial excerpt)
covering everything in this excerpt that a professor would need to write quiz questions or
slides from later:
- Every testable fact: definitions, formulas, named theorems/laws, constants
- Worked examples, written out in full with their key numeric/algebraic steps (do not abbreviate
  or drop any worked example present in the excerpt)
- Section or subsection titles and what each covers

## Exclude
This excerpt is raw text extracted from an interactive zyBooks PDF and contains embedded
interactive elements that are NOT real textbook content — skip them entirely, do not summarize or
reference them: "Participation Activity" widgets (fill-in-the-blank/self-check exercises,
frequently numbered like "1.2.3"), "Animation content"/"Animation captions" blocks, "Static
figure" captions, and "Check"/"Show answer" UI prompts. A numbered "Worked Example" or "WE#" with
a fully worked-out solution is real content and must be kept — the distinction is that Worked
Examples are complete and self-contained on the page, while Participation Activities reference
interactive state (sliders, draggable widgets, blanks to fill in) that doesn't exist in this
static text and reads as incomplete or confusing out of context.

## Excerpt
{{.Text}}`))

// chunkPromptData is the data passed to chunkPromptTmpl.
type chunkPromptData struct {
	Index int
	Total int
	Text  string
}

// buildChunkPrompt renders the LLM prompt for condensing one chunk.
func buildChunkPrompt(index, total int, text string) (string, error) {
	var buf bytes.Buffer
	if err := chunkPromptTmpl.Execute(&buf, chunkPromptData{Index: index, Total: total, Text: text}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
