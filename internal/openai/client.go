// Package openai implements distill.LLM against the OpenAI chat completions API.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jh125486/pdf2qti/internal/config"
)

const defaultBaseURL = "https://api.openai.com/v1"

// Client calls the OpenAI chat completions API.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	model       string
	temperature float64
}

// New builds a Client from a resolved Generation config, reading the API key from the
// environment variable named by cfg.APIKeyEnv. Returns an error if the provider isn't
// "openai" or the key env var is unset/empty.
func New(cfg config.Generation) (*Client, error) { //nolint:gocritic // matches config.Generation-by-value convention used elsewhere (internal/generate.New)
	if cfg.Provider != "openai" {
		return nil, fmt.Errorf("unsupported provider %q (only \"openai\" is implemented)", cfg.Provider)
	}
	keyEnv := cfg.APIKeyEnv
	if keyEnv == "" {
		keyEnv = "OPENAI_API_KEY"
	}
	key := os.Getenv(keyEnv)
	if key == "" {
		return nil, fmt.Errorf("environment variable %q is not set", keyEnv)
	}
	model := cfg.Model
	if model == "" {
		model = "gpt-4o"
	}
	return &Client{
		httpClient:  &http.Client{Timeout: 5 * time.Minute},
		baseURL:     defaultBaseURL,
		apiKey:      key,
		model:       model,
		temperature: cfg.Temperature,
	}, nil
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends prompt as a single user message and returns the model's reply, with any
// surrounding Markdown code-fence stripped (models occasionally wrap JSON responses in
// ```json ... ``` even when told not to).
func (c *Client) Complete(ctx context.Context, prompt string) (string, error) {
	reqBody := chatRequest{
		Model:       c.model,
		Messages:    []chatMessage{{Role: "user", Content: prompt}},
		Temperature: c.temperature,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req) //nolint:gosec // c.baseURL is fixed to defaultBaseURL in New; never derived from external input
	if err != nil {
		return "", fmt.Errorf("call openai: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var out chatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("parse response (status %d): %w", resp.StatusCode, err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("openai error (status %d): %s", resp.StatusCode, out.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai returned status %d: %s", resp.StatusCode, string(data))
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai response had no choices")
	}
	return stripCodeFence(out.Choices[0].Message.Content), nil
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
