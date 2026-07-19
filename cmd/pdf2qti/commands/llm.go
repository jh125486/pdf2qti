package commands

import (
	"fmt"
	"regexp"
	"strconv"
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
	reTargetContentRange = regexp.MustCompile(`TARGET_CONTENT_RANGE: min=(\d+) max=(\d+)`)
	rePlannedSlideLine   = regexp.MustCompile(`(?m)^\d+\. \[`)
	reChapterHeading     = regexp.MustCompile(`(?m)^### (\S+):`)
)

// stubProtoDeckShape answers the three prompt shapes distill.GenerateProtoDeck issues (outline
// planning, batch bullet expansion, summary bullets) with a minimal but structurally valid
// response, so stub LLMs (used only when no real provider key is configured) can drive the full
// pipeline offline. Returns ok=false if prompt doesn't match any of the three shapes, so callers
// with their own additional prompt shapes (e.g. stubModuleLLM's JSON-merge prompt) can fall
// through to their own default.
func stubProtoDeckShape(prompt string, chapterTags []string) (resp string, ok bool) {
	switch {
	case strings.Contains(prompt, "planning a prototype PowerPoint outline"):
		return stubOutlineJSON(prompt, chapterTags), true
	case strings.Contains(prompt, "writing the bullet content for"):
		return stubExpandBatchJSON(prompt), true
	case strings.Contains(prompt, "exactly one summary bullet per agenda item"):
		return stubSummaryJSON(prompt), true
	default:
		return "", false
	}
}

// stubOutlineJSON builds a minimal but valid outline response: one entry per chapter tag, cycled
// to reach the TARGET_CONTENT_RANGE minimum embedded in the prompt.
func stubOutlineJSON(prompt string, chapterTags []string) string {
	n := 6
	if m := reTargetContentRange.FindStringSubmatch(prompt); len(m) == 3 {
		if v, err := strconv.Atoi(m[1]); err == nil && v > 0 {
			n = v
		}
	}
	entries := make([]string, n)
	for i := range entries {
		tag := chapterTags[i%len(chapterTags)]
		entries[i] = fmt.Sprintf(`{"tag":%q,"title":"Slide %d","focus":"placeholder"}`, tag, i+1)
	}
	return fmt.Sprintf(`{"deck_title":"Module Deck","agenda":["Topic 1","Topic 2","Topic 3"],"outline":[%s]}`, strings.Join(entries, ","))
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
