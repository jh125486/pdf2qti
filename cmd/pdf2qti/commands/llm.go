package commands

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jh125486/pdf2qti/internal/audit"
	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
	"github.com/jh125486/pdf2qti/internal/openai"
)

// selectLLM returns a real provider client for gen if one can be constructed (currently only
// "openai", keyed off gen.APIKeyEnv), falling back to stub with a warning if the provider is
// unsupported or the API key env var isn't set — e.g. in tests, which run without secrets.
func selectLLM(gen config.Generation, logger *audit.Logger, stub distill.LLM) distill.LLM { //nolint:gocritic // matches config.Generation-by-value convention used elsewhere (internal/generate.New)
	client, err := openai.New(gen)
	if err != nil {
		logger.Warn("falling back to stub LLM", "reason", err.Error())
		return stub
	}
	return client
}

var (
	rePlannedSlideLine = regexp.MustCompile(`(?m)^\d+\. \[`)
	reReconcileLine    = regexp.MustCompile(`(?m)^\d+\. \[([^\]]*)\] \(chunk (\d+)\) (.*) — (.*)$`)
	reChapterHeading   = regexp.MustCompile(`(?m)^### (\S+):`)
)

// stubChunkOutlineEntries is the fixed number of placeholder outline entries
// stubChunkOutlineJSON returns per chunk-outline call — ignores the actual per-chunk target
// embedded in the real prompt (see generateChunkOutline's doc comment) in favor of a small
// constant, so a stub-driven test fixture's total content-entry count is fully predictable as
// (chunks across all chapters) × this constant.
const stubChunkOutlineEntries = 2

// stubProtoDeckShape answers the prompt shapes distill.GenerateProtoDeck issues (per-chunk
// outline planning, outline reconciliation, agenda/title framing, batch bullet expansion, summary
// bullets) with a minimal but structurally valid response, so stub LLMs (used only when no real
// provider key is configured) can drive the full pipeline offline. Returns ok=false if prompt
// doesn't match any of the shapes, so callers with their own additional prompt shapes (e.g.
// stubModuleLLM's JSON-merge prompt) can fall through to their own default.
func stubProtoDeckShape(prompt string) (resp string, ok bool) {
	switch {
	case strings.Contains(prompt, "## This excerpt (part "):
		return stubChunkOutlineJSON(), true
	case strings.Contains(prompt, "reviewing a slide outline that was planned in fixed-size chunks of chapter text"):
		return stubReconcileOutlineJSON(prompt), true
	case strings.Contains(prompt, "writing the overall title and agenda for a prototype PowerPoint deck"):
		return stubAgendaJSON(), true
	case strings.Contains(prompt, "writing the bullet content for"):
		return stubExpandBatchJSON(prompt), true
	case strings.Contains(prompt, "exactly one summary bullet per agenda item"):
		return stubSummaryJSON(prompt), true
	default:
		return "", false
	}
}

// stubChunkOutlineJSON builds a minimal but valid per-chunk outline response: a fixed small
// number of placeholder entries (see stubChunkOutlineEntries), no parsing of the prompt needed.
func stubChunkOutlineJSON() string {
	entries := make([]string, stubChunkOutlineEntries)
	for i := range entries {
		entries[i] = fmt.Sprintf(`{"title":"Slide %d","focus":"placeholder"}`, i+1)
	}
	return fmt.Sprintf(`{"outline":[%s]}`, strings.Join(entries, ","))
}

// stubReconcileOutlineJSON echoes back the joined outline entries the reconcile prompt renders
// (one "N. [tag#chunkIndex] Title — Focus" line per entry), unmerged — a legitimately valid "no
// duplicates found" reconciliation response, since a stub-driven test fixture's synthetic entries
// never actually duplicate. Echoes the parsed chunk index back as chunk_indices, so this stub
// exercises the real scoped-grounding path (see groundingText in internal/distill/outline.go)
// rather than always falling back to full chapter text.
func stubReconcileOutlineJSON(prompt string) string {
	matches := reReconcileLine.FindAllStringSubmatch(prompt, -1)
	entries := make([]string, len(matches))
	for i, m := range matches {
		entries[i] = fmt.Sprintf(`{"tag":%q,"title":%q,"focus":%q,"chunk_indices":[%s]}`, m[1], m[3], m[4], m[2])
	}
	return fmt.Sprintf(`{"outline":[%s],"warnings":[]}`, strings.Join(entries, ","))
}

// stubAgendaJSON builds a minimal but valid deck-framing response.
func stubAgendaJSON() string {
	return `{"deck_title":"Module Deck","agenda":["Topic 1","Topic 2","Topic 3"]}`
}

// stubExpandBatchJSON returns one placeholder bullet set per planned slide line in prompt.
func stubExpandBatchJSON(prompt string) string {
	n := len(rePlannedSlideLine.FindAllString(prompt, -1))
	slides := make([]string, n)
	for i := range slides {
		slides[i] = `{"bullets":["placeholder"]}`
	}
	return fmt.Sprintf(`{"slides":[%s]}`, strings.Join(slides, ","))
}

// stubSummaryJSON returns one placeholder bullet per chapter heading in prompt.
func stubSummaryJSON(prompt string) string {
	n := len(reChapterHeading.FindAllString(prompt, -1))
	if n == 0 {
		n = 1
	}
	bullets := make([]string, n)
	for i := range bullets {
		bullets[i] = `"placeholder"`
	}
	return fmt.Sprintf(`{"bullets":[%s]}`, strings.Join(bullets, ","))
}
