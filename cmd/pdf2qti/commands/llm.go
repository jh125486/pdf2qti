package commands

import (
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
