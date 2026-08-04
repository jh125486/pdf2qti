// Whitebox package: tests construct Client directly to point baseURL/httpClient at a fake
// server and stub RoundTripper, since New always hardcodes the real OpenAI API URL.
package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/jh125486/pdf2qti/internal/config"
	"github.com/jh125486/pdf2qti/internal/distill"
)

func TestNew(t *testing.T) {
	// Not t.Parallel(): subtests use t.Setenv, which forbids parallel ancestry.
	tests := []struct {
		name      string
		cfg       config.Generation
		setEnv    map[string]string
		wantErr   string
		wantModel string
	}{
		{
			name:    "unsupported provider",
			cfg:     config.Generation{Provider: "anthropic"},
			wantErr: `unsupported provider "anthropic"`,
		},
		{
			name:    "missing default env var",
			cfg:     config.Generation{Provider: "openai"},
			wantErr: `environment variable "OPENAI_API_KEY" is not set`,
		},
		{
			name:    "missing custom env var",
			cfg:     config.Generation{Provider: "openai", APIKeyEnv: "MY_KEY"},
			wantErr: `environment variable "MY_KEY" is not set`,
		},
		{
			name:      "default model when unset",
			cfg:       config.Generation{Provider: "openai"},
			setEnv:    map[string]string{"OPENAI_API_KEY": "sk-test"},
			wantModel: "gpt-4o",
		},
		{
			name:      "explicit model and custom env var honored",
			cfg:       config.Generation{Provider: "openai", Model: "gpt-4-turbo", APIKeyEnv: "MY_KEY"},
			setEnv:    map[string]string{"MY_KEY": "sk-test"},
			wantModel: "gpt-4-turbo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Not t.Parallel(): t.Setenv forbids parallel subtests.
			for k, v := range tt.setEnv {
				t.Setenv(k, v)
			}

			c, err := New(tt.cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got err %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.model != tt.wantModel {
				t.Fatalf("got model %q, want %q", c.model, tt.wantModel)
			}
			if c.baseURL != defaultBaseURL {
				t.Fatalf("got baseURL %q, want %q", c.baseURL, defaultBaseURL)
			}
		})
	}
}

// errReadCloser fails on Read, simulating a response body that breaks mid-stream.
type errReadCloser struct{ err error }

func (e errReadCloser) Read([]byte) (int, error) { return 0, e.err }
func (e errReadCloser) Close() error             { return nil }

func TestClient_Complete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		baseURL        string
		roundTripFn    func(*http.Request) (*http.Response, error)
		nanTemperature bool
		serverBody     string
		serverCode     int
		schema         *distill.Schema
		want           string
		wantErr        string
	}{
		{
			name:       "success",
			serverCode: http.StatusOK,
			serverBody: `{"choices":[{"message":{"role":"assistant","content":"hello world"}}]}`,
			want:       "hello world",
		},
		{
			name:           "marshal request error from NaN temperature",
			nanTemperature: true,
			wantErr:        "marshal request",
		},
		{
			name:       "success with json code fence stripped",
			serverCode: http.StatusOK,
			serverBody: "{\"choices\":[{\"message\":{\"role\":\"assistant\",\"content\":\"```json\\n{\\\"a\\\":1}\\n```\"}}]}",
			want:       `{"a":1}`,
		},
		{
			name:       "api error field set",
			serverCode: http.StatusOK,
			serverBody: `{"error":{"message":"rate limited"}}`,
			wantErr:    "openai error (status 200): rate limited",
		},
		{
			name:       "non-200 without error field",
			serverCode: http.StatusInternalServerError,
			serverBody: `{"choices":[]}`,
			wantErr:    "openai returned status 500",
		},
		{
			name:       "no choices",
			serverCode: http.StatusOK,
			serverBody: `{"choices":[]}`,
			wantErr:    "openai response had no choices",
		},
		{
			name:       "malformed json body",
			serverCode: http.StatusOK,
			serverBody: `not json`,
			wantErr:    "parse response (status 200)",
		},
		{
			name:    "request build error from invalid base url",
			baseURL: "://bad url\x7f",
			wantErr: "build request",
		},
		{
			name: "http do error",
			roundTripFn: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("boom")
			},
			wantErr: "call openai",
		},
		{
			name: "read response body error",
			roundTripFn: func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       errReadCloser{err: errors.New("truncated")},
					Header:     make(http.Header),
				}, nil
			},
			wantErr: "read response",
		},
		{
			// A non-nil schema must produce a response_format:{type:"json_schema",...} field in
			// the request body — verified by inspecting the request the stub transport receives,
			// not just that the call succeeds (which it would even if response_format were
			// silently dropped).
			name:        "structured output schema included in request body",
			schema:      &distill.Schema{Name: "test_schema", Definition: map[string]any{"type": "object"}},
			roundTripFn: assertResponseFormatRoundTrip,
			want:        "ok",
		},
		{
			name:       "refusal response is an error",
			serverCode: http.StatusOK,
			serverBody: `{"choices":[{"message":{"role":"assistant","refusal":"I can't help with that"}}]}`,
			wantErr:    "openai refused the request: I can't help with that",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &Client{
				httpClient:  &http.Client{},
				baseURL:     defaultBaseURL,
				apiKey:      "sk-test",
				model:       "gpt-4o",
				temperature: 0.5,
			}
			if tt.nanTemperature {
				c.temperature = math.NaN()
			}

			switch {
			case tt.nanTemperature:
				// No transport needed: json.Marshal fails before any request is sent.
			case tt.baseURL != "":
				c.baseURL = tt.baseURL
			case tt.roundTripFn != nil:
				c.httpClient.Transport = roundTripperFunc(tt.roundTripFn)
			default:
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tt.serverCode)
					_, _ = io.WriteString(w, tt.serverBody)
				}))
				t.Cleanup(srv.Close)
				c.baseURL = srv.URL
			}

			got, err := c.Complete(t.Context(), "prompt", tt.schema)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got err %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// assertResponseFormatRoundTrip is TestClient_Complete's "structured output schema included in
// request body" roundTripFn, pulled out to a named function (rather than an inline closure in the
// table) to keep that test's own cyclomatic complexity under gocyclo's threshold.
func assertResponseFormatRoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var sent struct {
		ResponseFormat struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Name   string `json:"name"`
				Strict bool   `json:"strict"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		return nil, err
	}
	if sent.ResponseFormat.Type != "json_schema" || sent.ResponseFormat.JSONSchema.Name != "test_schema" || !sent.ResponseFormat.JSONSchema.Strict {
		return nil, fmt.Errorf("request body missing expected response_format, got: %s", body)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)),
		Header:     make(http.Header),
	}, nil
}

// newTestClient returns a Client wired to roundTrip, for the two rate-limit retry tests below
// (they can't share TestClient_Complete's table: retrying involves real backoff durations, which
// only synctest.Test's virtualized clock can resolve instantly and deterministically — a
// synctest bubble can't be one case among ordinarily-parallel table subtests).
func newTestClient(roundTrip func(*http.Request) (*http.Response, error)) *Client {
	return &Client{
		httpClient:  &http.Client{Transport: roundTripperFunc(roundTrip)},
		baseURL:     defaultBaseURL,
		apiKey:      "sk-test",
		model:       "gpt-4o",
		temperature: 0.5,
	}
}

// TestClient_Complete_RateLimitedTwiceThenSucceeds: the first two calls are rate limited, the
// third succeeds — Complete must retry with backoff rather than surfacing the 429 immediately.
// Runs in a synctest bubble so the real rateLimitBackoff waits resolve instantly instead of
// actually pausing the test.
func TestClient_Complete_RateLimitedTwiceThenSucceeds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		c := newTestClient(func(*http.Request) (*http.Response, error) { //nolint:bodyclose // closed by Client.doComplete's defer resp.Body.Close(), not visible to this stub's own scope
			calls++
			if calls <= 2 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
					Header:     make(http.Header),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"hello world"}}]}`)),
				Header:     make(http.Header),
			}, nil
		})

		got, err := c.Complete(t.Context(), "prompt", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "hello world" {
			t.Fatalf("got %q, want %q", got, "hello world")
		}
		if calls != 3 {
			t.Fatalf("got %d calls, want 3", calls)
		}
	})
}

// TestClient_Complete_ExceedsMaxRetriesWhenAlwaysRateLimited: every call is rate limited —
// Complete must give up after maxRateLimitRetries rather than retrying forever, and say so.
// Runs in a synctest bubble for the same reason as the sibling test above.
func TestClient_Complete_ExceedsMaxRetriesWhenAlwaysRateLimited(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newTestClient(func(*http.Request) (*http.Response, error) { //nolint:bodyclose // closed by Client.doComplete's defer resp.Body.Close(), not visible to this stub's own scope
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
				Header:     make(http.Header),
			}, nil
		})

		_, err := c.Complete(t.Context(), "prompt", nil)
		if err == nil || !strings.Contains(err.Error(), "exceeded 5 retries") {
			t.Fatalf("got err %v, want containing %q", err, "exceeded 5 retries")
		}
	})
}

func TestStripCodeFence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "no fence", in: "plain text", want: "plain text"},
		{name: "json fence", in: "```json\n{\"a\":1}\n```", want: `{"a":1}`},
		{name: "bare fence", in: "```\nhello\n```", want: "hello"},
		{name: "surrounding whitespace", in: "  ```json\n{}\n```  \n", want: "{}"},
		{name: "fence with no closing marker", in: "```json\nunterminated", want: "unterminated"},
		{name: "single line fence only", in: "```", want: ""},
		{name: "multiline content preserved", in: "```json\nline1\nline2\n```", want: "line1\nline2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stripCodeFence(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
