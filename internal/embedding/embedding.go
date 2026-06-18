// Package embedding generates vector embeddings via OpenRouter API.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Client generates embeddings from text.
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// Config holds embedding configuration.
type Config struct {
	APIKey  string
	Model   string
	BaseURL string // OpenAI-compatible /embeddings endpoint base. Default: OpenRouter.
}

// defaultBaseURL is the fallback when no env var is set. Any
// OpenAI-compatible endpoint (OpenAI, OpenRouter, vLLM, LM Studio,
// Ollama with the OpenAI-compat shim, etc.) works.
const defaultBaseURL = "https://openrouter.ai/api/v1"

// NewClient creates a new embedding client.
func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	return &Client{
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// NewClientFromEnv creates a client from environment variables and key file.
func NewClientFromEnv() *Client {
	return NewClient(Config{
		APIKey:  LoadAPIKey(),
		Model:   DefaultModel(),
		BaseURL: BaseURL(),
	})
}

// LoadAPIKey reads the API key from env, with provider-agnostic precedence:
//
//	EMBEDDING_API_KEY → LLM_API_KEY → OPENROUTER_API_KEY → key file
//
// The key file path can be overridden by OPENROUTER_KEY_FILE, otherwise
// defaults to $HOME/.openrouter-key (kept for backward compatibility).
func LoadAPIKey() string {
	key, _ := LoadAPIKeyWithSource()
	return key
}

// LoadAPIKeyWithSource is the same as LoadAPIKey but also returns the
// source identifier (env var name or "file:<path>") for boot-time logging.
// Important for the silent-shadowing trap: when LLM_API_KEY is set but
// EMBEDDING_API_KEY isn't, the embedder silently routes through the LLM
// provider's API — visible at boot via this source line.
func LoadAPIKeyWithSource() (string, string) {
	for _, k := range []string{"EMBEDDING_API_KEY", "LLM_API_KEY", "OPENROUTER_API_KEY"} {
		if v := os.Getenv(k); v != "" {
			return v, k
		}
	}
	keyPath := os.Getenv("OPENROUTER_KEY_FILE")
	if keyPath == "" {
		keyPath = os.ExpandEnv("$HOME/.openrouter-key")
	}
	// keyPath is operator-controlled (env var or default $HOME path).
	if b, err := os.ReadFile(keyPath); err == nil { // #nosec G304,G703 -- operator-supplied path (env var or default)
		return strings.TrimSpace(string(b)), "file:" + keyPath
	}
	return "", ""
}

// BaseURL returns the embeddings endpoint base, falling back to OpenRouter.
// Precedence: EMBEDDING_BASE_URL → LLM_BASE_URL → default.
func BaseURL() string {
	for _, k := range []string{"EMBEDDING_BASE_URL", "LLM_BASE_URL"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return defaultBaseURL
}

// DefaultModel returns the default embedding model.
func DefaultModel() string {
	if m := os.Getenv("EMBEDDING_MODEL"); m != "" {
		return m
	}
	return "openai/text-embedding-3-small"
}

type embeddingRequest struct {
	Model    string          `json:"model"`
	Input    any             `json:"input"`
	Provider *providerPolicy `json:"provider,omitempty"`
}

// providerPolicy carries OpenRouter per-request routing preferences. We set
// data_collection="deny" so OpenRouter only routes to upstream providers that
// do not store or train on request data — rill summaries are highly personal
// (therapy, family, health), so the text must not be retained. Only sent to
// OpenRouter base URLs; other OpenAI-compatible endpoints (OpenAI direct, vLLM,
// LM Studio, local) would reject the unknown field, and a local endpoint needs
// no such guarantee. Account-level prompt logging must also be disabled in the
// OpenRouter dashboard — this param governs upstream routing, not OpenRouter's
// own logging.
type providerPolicy struct {
	DataCollection string `json:"data_collection,omitempty"`
}

// isOpenRouter reports whether base targets the OpenRouter gateway, which
// understands the provider routing/privacy object.
func isOpenRouter(base string) bool {
	return strings.Contains(strings.ToLower(base), "openrouter.ai")
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// Embed generates an embedding vector for a single text.
func (c *Client) Embed(ctx context.Context, text string) ([]float64, error) {
	vecs, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embed: no embeddings returned")
	}
	return vecs[0], nil
}

// Model returns the configured embedding model identifier.
func (c *Client) Model() string {
	return c.model
}

// EmbedBatch generates embeddings for multiple texts in a single API call.
// Returns one vector per input, in the same order. Empty input returns nil
// without making a network request.
func (c *Client) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body := embeddingRequest{
		Model: c.model,
		Input: texts,
	}
	if isOpenRouter(c.baseURL) {
		body.Provider = &providerPolicy{DataCollection: "deny"}
	}

	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("embed: marshal: %w", err)
	}

	resp, err := doWithRetry(ctx, c.http, "POST", c.baseURL+"/embeddings", b, c.apiKey)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed: %s: %s", resp.Status, string(body))
	}

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed: decode: %w", err)
	}

	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("embed: expected %d embeddings, got %d", len(texts), len(result.Data))
	}

	out := make([][]float64, len(result.Data))
	for i, d := range result.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

// doWithRetry sends an HTTP POST with exponential backoff on transient
// failures (network errors, 429 rate-limited, 5xx server errors). Respects
// the server's Retry-After header on 429 when present.
//
// 3 attempts total, base delay 1s, doubling per attempt: ~1s + ~2s = 3s
// max added latency on the worst case before giving up.
//
// Duplicated from internal/llm — both packages talk to OpenRouter-style
// endpoints and have the same retry needs; not worth a separate package yet.
func doWithRetry(ctx context.Context, client *http.Client, method, url string, body []byte, apiKey string) (*http.Response, error) {
	const maxAttempts = 3
	const baseDelay = time.Second

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := baseDelay << (attempt - 1)
			if ra, ok := lastErr.(*retryAfterErr); ok && ra.delay > 0 {
				if ra.delay < 30*time.Second {
					delay = ra.delay
				} else {
					delay = 30 * time.Second
				}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests ||
			(resp.StatusCode >= 500 && resp.StatusCode < 600) {
			ra := parseRetryAfter(resp.Header.Get("Retry-After"))
			// #nosec G104 -- intentional drain+close before retry; we're already
			// on the error path and don't care if the discard/close fails.
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			lastErr = &retryAfterErr{status: resp.StatusCode, delay: ra}
			continue
		}

		return resp, nil
	}
	return nil, fmt.Errorf("after %d attempts: %w", maxAttempts, lastErr)
}

type retryAfterErr struct {
	status int
	delay  time.Duration
}

func (e *retryAfterErr) Error() string {
	return fmt.Sprintf("http %d (retry-after=%s)", e.status, e.delay)
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}

// ToFloat32 converts an embedding from any commonly-returned shape
// ([]float64 from SurrealDB driver, []float32 already-typed, or []any
// from JSON decoding) into []float32. Returns false if v is not a
// recognized embedding shape.
func ToFloat32(v any) ([]float32, bool) {
	switch val := v.(type) {
	case []float32:
		return val, true
	case []float64:
		out := make([]float32, len(val))
		for i, f := range val {
			out[i] = float32(f)
		}
		return out, true
	case []any:
		out := make([]float32, len(val))
		for i, f := range val {
			switch fv := f.(type) {
			case float64:
				out[i] = float32(fv)
			case float32:
				out[i] = fv
			default:
				return nil, false
			}
		}
		return out, true
	}
	return nil, false
}
