package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// restClient is the CLI's HTTP client for rill's /api surface.
//
// Reads RILL_HOST (default http://localhost:9090) and RILL_TOKEN.
// Every request attaches Authorization: Bearer <RILL_TOKEN> so the server's
// auth middleware accepts the call. The CLI never speaks to SurrealDB directly.
type restClient struct {
	host  string
	token string
	http  *http.Client
}

const defaultRillHost = "http://localhost:9090"

func newRESTClient() *restClient {
	host := strings.TrimRight(os.Getenv("RILL_HOST"), "/")
	if host == "" {
		host = defaultRillHost
	}
	if u, err := url.Parse(host); err == nil && u.Scheme != "" && u.Host != "" && u.Path != "" {
		// Most common cause: user pasted the MCP connector URL with a
		// trailing /mcp path. The CLI hits /api/*, so a path-bearing host
		// produces /mcp/api/orient (404). Strip + warn.
		stripped := u.Scheme + "://" + u.Host
		fmt.Fprintf(os.Stderr, "rill: RILL_HOST has a path (%q) — stripping to %q. Set RILL_HOST without a trailing path to silence.\n", host, stripped)
		host = stripped
	}
	return &restClient{
		host:  host,
		token: os.Getenv("RILL_TOKEN"),
		http:  &http.Client{Timeout: 60 * time.Second},
	}
}

// requireToken returns an error if RILL_TOKEN isn't set. Read-only endpoints
// would still be rejected by auth middleware, so we fail loudly up front.
func (c *restClient) requireToken() error {
	if c.token == "" {
		return fmt.Errorf("RILL_TOKEN is not set — create one at %s/settings and export RILL_TOKEN=rill_v1_...", c.host)
	}
	return nil
}

// do executes a JSON request. body is marshaled if non-nil; out is decoded if non-nil.
// Non-2xx responses are returned as an error including the server's error JSON.
func (c *restClient) do(ctx context.Context, method, path string, body, out any) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return parseAPIError(method, path, resp.StatusCode, raw)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response from %s %s: %w", method, path, err)
	}
	return nil
}

// apiError mirrors the server's REST error envelope (internal/server/error.go).
type apiError struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}

// parseAPIError renders a server error response into a clean CLI error: the
// server's message, its stable code in parens, and the request_id (present on
// internal errors, so a 500 can be correlated to the server log). Falls back to
// the raw body + status when the response isn't the JSON envelope.
func parseAPIError(method, path string, status int, raw []byte) error {
	var e apiError
	if err := json.Unmarshal(raw, &e); err == nil && e.Error != "" {
		msg := e.Error
		if e.Code != "" {
			msg = fmt.Sprintf("%s (%s)", e.Error, e.Code)
		}
		if e.RequestID != "" {
			msg = fmt.Sprintf("%s [req %s]", msg, e.RequestID)
		}
		return fmt.Errorf("%s %s: %s", method, path, msg)
	}
	body := strings.TrimSpace(string(raw))
	if body == "" {
		body = http.StatusText(status)
	}
	return fmt.Errorf("%s %s: %d %s", method, path, status, body)
}

func (c *restClient) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// getRaw fetches a non-JSON endpoint (e.g. /export.md) and returns the raw body.
func (c *restClient) getRaw(ctx context.Context, path string) ([]byte, error) {
	if err := c.requireToken(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, parseAPIError(http.MethodGet, path, resp.StatusCode, raw)
	}
	return raw, nil
}

func (c *restClient) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *restClient) patch(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPatch, path, body, out)
}

func (c *restClient) del(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodDelete, path, nil, out)
}

// pathWithQuery builds "path?k=v&..." skipping empty values.
func pathWithQuery(path string, params map[string]string) string {
	if len(params) == 0 {
		return path
	}
	v := url.Values{}
	for k, val := range params {
		if val != "" {
			v.Set(k, val)
		}
	}
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}
