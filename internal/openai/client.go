// Package openai implements distill.LLM against the OpenAI chat completions API.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
)

const (
	defaultBaseURL   = "https://api.openai.com/v1"
	providerOpenAI   = "openai"
	defaultModel     = "gpt-4o"
	defaultAPIKeyEnv = "OPENAI_API_KEY" //nolint:gosec // this is an env var *name*, not a credential value
)

// maxCompleteRetries and completeRetryBackoff bound Complete's retry behavior for the two
// transient failure modes it recognizes: a 429 (rate limited) response, and a response truncated
// before completion (see errTruncated). Observed in practice from pdf2qti's per-textbook-section
// outline planning (one call per section, sequentially, each resending a chapter's full grounding
// text): a burst of calls within the same minute can transiently exceed a low-TPM-tier account's
// rate limit, with OpenAI's own error asking for waits as short as tens to hundreds of
// milliseconds — so a short linear backoff (2s, 4s, 6s, ...) comfortably clears a transient
// window without meaningfully slowing down the overwhelmingly common case where no retry is ever
// needed. The same budget covers truncation since it's the same class of "try again, it usually
// works" transient failure, not a distinct one needing its own tuning.
const (
	maxCompleteRetries   = 5
	completeRetryBackoff = 2 * time.Second
)

// errTruncated indicates the model's response was cut off before completion (finish_reason
// "length") rather than genuinely finished — any content returned alongside it is necessarily
// incomplete (and, for a JSON response, un-parseable) rather than a smaller-but-valid answer, so
// doComplete discards it entirely instead of returning it as if it were successful. Reasoning
// models are the common real-world trigger: reasoning tokens count against the same completion
// budget as the visible answer, so a demanding schema or a lot of grounding text can occasionally
// exhaust that budget on reasoning alone and leave nothing for the answer itself — observed in
// practice as pdf2qti's outline-chunk planning calls intermittently coming back with "no '{'
// found in response" even though the request itself succeeded (HTTP 200, no refusal).
var errTruncated = errors.New("openai: response truncated (finish_reason: length)")

// Client calls the OpenAI chat completions API. modelParams, when set, is a raw JSON object
// merged directly into every request body (e.g. {"temperature": 0.7} or {"reasoning_effort":
// "high"}) — see config.Generation's doc comment for why this is a raw blob rather than typed Go
// fields per parameter.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	model       string
	modelParams json.RawMessage
}

// defaultHTTPTimeout is used when httpTimeout is unset (<=0) — e.g. a Client built
// programmatically rather than through the CLI's --http-timeout flag (which itself defaults to
// this same value; see CLI.HTTPTimeout).
const defaultHTTPTimeout = 5 * time.Minute

// New builds a Client from a resolved Generation config, reading the API key from the
// environment variable named by cfg.APIKeyEnv. Returns an error if the provider isn't
// "openai" or the key env var is unset/empty. httpTimeout bounds each individual API call;
// <=0 falls back to defaultHTTPTimeout.
func New(cfg config.Generation, httpTimeout time.Duration) (*Client, error) { //nolint:gocritic // matches config.Generation-by-value convention used elsewhere (internal/generate.New)
	if cfg.Provider != providerOpenAI {
		return nil, fmt.Errorf("unsupported provider %q (only \"openai\" is implemented)", cfg.Provider)
	}
	keyEnv := cfg.APIKeyEnv
	if keyEnv == "" {
		keyEnv = defaultAPIKeyEnv
	}
	key := os.Getenv(keyEnv)
	if key == "" {
		return nil, fmt.Errorf("environment variable %q is not set", keyEnv)
	}
	model := cfg.Model
	if model == "" {
		model = defaultModel
	}
	if httpTimeout <= 0 {
		httpTimeout = defaultHTTPTimeout
	}
	return &Client{
		httpClient:  &http.Client{Timeout: httpTimeout},
		baseURL:     defaultBaseURL,
		apiKey:      key,
		model:       model,
		modelParams: cfg.ModelParams,
	}, nil
}

// sleepCtx waits for d, or returns ctx's error early if ctx is done first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// responseFormat is the request body shape for OpenAI's Structured Outputs (strict JSON Schema
// enforcement): response_format: {type: "json_schema", json_schema: {name, strict, schema}}.
// With strict:true, the API constrains decoding token-by-token so the response is guaranteed to
// be syntactically valid JSON matching schema exactly — eliminating, at the source, the whole
// class of problems distill.unmarshalRepaired's client-side parsing exists to work around (prose
// prepended/appended around the JSON, a field silently omitted or malformed, an escaping mistake
// inside a string value) rather than trying to out-guess every way a model might deviate from a
// prose-described shape.
type responseFormat struct {
	Type       string         `json:"type"`
	JSONSchema jsonSchemaSpec `json:"json_schema"`
}

type jsonSchemaSpec struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// mergeModelParams marshals req, then merges extra's top-level keys on top — extra's keys win on
// collision — letting arbitrary provider-specific parameters (temperature, reasoning_effort,
// top_p, whatever a model adds next) be sent without Client needing a typed Go field for each one
// (see config.Generation.ModelParams's doc comment for why). Uses map[string]json.RawMessage
// rather than map[string]any for both req's own marshaled JSON and extra, so every value's
// original byte representation is preserved exactly rather than round-tripped through Go's
// float64-by-default JSON number decoding. extra may be nil/empty (no merge needed).
func mergeModelParams(req chatRequest, extra json.RawMessage) ([]byte, error) {
	base, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return base, nil
	}

	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	var extraFields map[string]json.RawMessage
	if err := json.Unmarshal(extra, &extraFields); err != nil {
		return nil, fmt.Errorf("invalid modelParams: %w", err)
	}
	maps.Copy(merged, extraFields)
	return json.Marshal(merged)
}

// Complete sends prompt as a single user message and returns the model's reply, with any
// surrounding Markdown code-fence stripped (models occasionally wrap JSON responses in
// ```json ... ``` even when told not to). When schema is non-nil, the request enforces it
// server-side via Structured Outputs (see responseFormat) instead of relying on prompt wording
// alone. Retries with backoff (see maxCompleteRetries, completeRetryBackoff) when OpenAI responds
// 429 (rate limited) or truncates the response (see errTruncated); any other error, including a
// safety refusal, is returned immediately without retrying.
func (c *Client) Complete(ctx context.Context, prompt string, schema *distill.Schema) (string, error) {
	reqBody := chatRequest{
		Model:    c.model,
		Messages: []chatMessage{{Role: "user", Content: prompt}},
	}
	if schema != nil {
		reqBody.ResponseFormat = &responseFormat{
			Type: "json_schema",
			JSONSchema: jsonSchemaSpec{
				Name:   schema.Name,
				Strict: true,
				Schema: schema.Definition,
			},
		}
	}
	body, err := mergeModelParams(reqBody, c.modelParams)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxCompleteRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, completeRetryBackoff*time.Duration(attempt)); err != nil {
				return "", err
			}
		}
		content, statusCode, err := c.doComplete(ctx, body)
		if statusCode != http.StatusTooManyRequests && !errors.Is(err, errTruncated) {
			return content, err
		}
		lastErr = err
	}
	return "", fmt.Errorf("openai: exceeded %d retries: %w", maxCompleteRetries, lastErr)
}

// doComplete makes one HTTP call to the chat completions API. statusCode is the response's HTTP
// status when a response was received at all (0 if the request itself failed before getting one),
// letting Complete decide whether this specific failure is worth retrying.
func (c *Client) doComplete(ctx context.Context, body []byte) (content string, statusCode int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req) //nolint:gosec // c.baseURL is fixed to defaultBaseURL in New; never derived from external input
	if err != nil {
		return "", 0, fmt.Errorf("call openai: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	var out chatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", resp.StatusCode, fmt.Errorf("parse response (status %d): %w", resp.StatusCode, err)
	}
	if out.Error != nil {
		return "", resp.StatusCode, fmt.Errorf("openai error (status %d): %s", resp.StatusCode, out.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, fmt.Errorf("openai returned status %d: %s", resp.StatusCode, string(data))
	}
	if len(out.Choices) == 0 {
		return "", resp.StatusCode, fmt.Errorf("openai response had no choices")
	}
	if refusal := out.Choices[0].Message.Refusal; refusal != "" {
		return "", resp.StatusCode, fmt.Errorf("openai refused the request: %s", refusal)
	}
	if out.Choices[0].FinishReason == "length" {
		return "", resp.StatusCode, fmt.Errorf("%w (%d bytes of content received before the cutoff)", errTruncated, len(out.Choices[0].Message.Content))
	}
	return stripCodeFence(out.Choices[0].Message.Content), resp.StatusCode, nil
}

// stripCodeFence removes a single leading/trailing ``` or ```json fence, if present.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 1 {
		lines = lines[1:]
	}
	s = strings.TrimSpace(strings.Join(lines, "\n"))
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
