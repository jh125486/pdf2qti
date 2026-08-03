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
	reReconcileLine    = regexp.MustCompile(`(?m)^\d+\. \[([^\]]*)\] (.*) — (.*)$`)
	reChapterHeading   = regexp.MustCompile(`(?m)^### (\S+):`)
)

// stubSectionOutlineEntries is the fixed number of placeholder outline entries
// stubSectionOutlineJSON returns per section-outline call — there's no numeric target embedded in
// that prompt to parse (see generateSectionOutline's doc comment for why), so a small constant is
// simplest; total content-entry count for a stub-driven test fixture is fully predictable as
// (sections-after-fallback across all chapters) × this constant.
const stubSectionOutlineEntries = 2

// stubProtoDeckShape answers the prompt shapes distill.GenerateProtoDeck issues (per-section
// outline planning, outline reconciliation, agenda/title framing, batch bullet expansion, summary
// bullets) with a minimal but structurally valid response, so stub LLMs (used only when no real
// provider key is configured) can drive the full pipeline offline. Returns ok=false if prompt
// doesn't match any of the shapes, so callers with their own additional prompt shapes (e.g.
// stubModuleLLM's JSON-merge prompt) can fall through to their own default.
func stubProtoDeckShape(prompt string) (resp string, ok bool) {
	switch {
	case strings.Contains(prompt, "planning a small part of a prototype PowerPoint outline"):
		return stubSectionOutlineJSON(), true
	case strings.Contains(prompt, "reviewing a slide outline that was planned one textbook section at a time"):
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

// stubSectionOutlineJSON builds a minimal but valid per-section outline response: a fixed small
// number of placeholder entries (see stubSectionOutlineEntries), no parsing of the prompt needed.
func stubSectionOutlineJSON() string {
	entries := make([]string, stubSectionOutlineEntries)
	for i := range entries {
		entries[i] = fmt.Sprintf(`{"title":"Slide %d","focus":"placeholder"}`, i+1)
	}
	return fmt.Sprintf(`{"outline":[%s]}`, strings.Join(entries, ","))
}

// stubReconcileOutlineJSON echoes back the joined outline entries the reconcile prompt renders
// (one "N. [tag] Title — Focus" line per entry, the same shape buildExpandBatchPrompt's planned-
// slides list uses), unmerged — a legitimately valid "no duplicates found" reconciliation
// response, since a stub-driven test fixture's synthetic entries never actually duplicate.
func stubReconcileOutlineJSON(prompt string) string {
	matches := reReconcileLine.FindAllStringSubmatch(prompt, -1)
	entries := make([]string, len(matches))
	for i, m := range matches {
		entries[i] = fmt.Sprintf(`{"tag":%q,"title":%q,"focus":%q}`, m[1], m[2], m[3])
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
