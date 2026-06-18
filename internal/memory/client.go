package memory

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
)

// Client is a tiny SurrealDB HTTP client purpose-built for memory.
//
// Why not reuse internal/db: that package is wired to v2 schema setup
// and entity/fact pipelines we explicitly don't want in v3. Keeping
// memoryv3 self-contained makes the package independently testable and
// keeps the cutover clean — when v3 is ready, prod swaps to it without
// half the v2 code still loaded.
type Client struct {
	url      string
	ns       string
	db       string
	user     string
	password string
	http     *http.Client
}

// Config is a memoryv3 SurrealDB connection.
type Config struct {
	URL      string // e.g. http://127.0.0.1:8001
	NS       string // e.g. rill_v3
	DB       string // e.g. main
	User     string
	Password string
	Timeout  time.Duration
}

// ConfigFromEnv builds a config from RILL_SURREAL_* env vars with sensible defaults.
func ConfigFromEnv() Config {
	return Config{
		URL:      envOr("RILL_SURREAL_URL", "http://127.0.0.1:8001"),
		NS:       envOr("RILL_SURREAL_NS", "rill"),
		DB:       envOr("RILL_SURREAL_DB", "main"),
		User:     envOr("RILL_SURREAL_USER", "root"),
		Password: envOr("RILL_SURREAL_PASS", "root"),
		Timeout:  60 * time.Second,
	}
}

// NewClient builds a Client from a Config.
func NewClient(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &Client{
		url:      strings.TrimRight(cfg.URL, "/"),
		ns:       cfg.NS,
		db:       cfg.DB,
		user:     cfg.User,
		password: cfg.Password,
		http:     &http.Client{Timeout: cfg.Timeout},
	}
}

// surrealResult is one element of the array returned by /sql.
type surrealResult struct {
	Status string          `json:"status"`
	Result json.RawMessage `json:"result"`
	Time   string          `json:"time"`
	Detail string          `json:"detail,omitempty"`
}

// SQL runs a SurrealQL query (possibly multi-statement) against the configured
// namespace+database. Returns one surrealResult per statement.
//
// If raiseOnStmtErr is true, any per-statement "ERR" status fails the call.
func (c *Client) SQL(ctx context.Context, query string, raiseOnStmtErr bool) ([]surrealResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/sql",
		bytes.NewReader([]byte(query)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Surreal-NS", c.ns)
	req.Header.Set("Surreal-DB", c.db)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.user, c.password)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("surreal request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("surreal read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("surreal HTTP %d: %s", resp.StatusCode, truncate(string(body), 500))
	}

	var results []surrealResult
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("surreal parse response: %w (body=%s)", err, truncate(string(body), 500))
	}

	if raiseOnStmtErr {
		// In a failed/cancelled transaction SurrealDB marks EVERY statement ERR:
		// the triggering statement carries the real cause; the others just say
		// "...not executed due to a failed/cancelled transaction". Find the real
		// cause so the classified error is meaningful (a plain non-tx error is
		// simply the first — and only — ERR).
		var realErr, anyErr string
		for _, r := range results {
			if r.Status != "ERR" {
				continue
			}
			var raw any
			_ = json.Unmarshal(r.Result, &raw)
			msg := fmt.Sprint(raw)
			if anyErr == "" {
				anyErr = msg
			}
			if !strings.Contains(msg, "not executed due to a") {
				realErr = msg
				break
			}
		}
		if realErr == "" {
			realErr = anyErr
		}
		if realErr != "" {
			return results, classifyStmtErr(realErr)
		}
	}
	return results, nil
}

// First runs a query and returns the parsed result of the LAST statement
// (most common pattern: LET ... ; SELECT ...) decoded into v.
func (c *Client) First(ctx context.Context, query string, v any) error {
	results, err := c.SQL(ctx, query, true)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return nil
	}
	last := results[len(results)-1]
	if v == nil {
		return nil
	}
	if len(last.Result) == 0 || string(last.Result) == "null" {
		return nil
	}
	return json.Unmarshal(last.Result, v)
}

// Ping checks the server is reachable.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.SQL(ctx, "RETURN 1;", true)
	return err
}

// EscapeStr produces a SurrealQL-safe quoted string literal.
// Caller is responsible for using $vars-style binds where possible;
// this is for the inline cases (record ids, simple constants).
func EscapeStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// EscapeDatetime returns a SurrealQL `d"..."` datetime literal.
func EscapeDatetime(t time.Time) string {
	return "d" + EscapeStr(t.UTC().Format(time.RFC3339Nano))
}

func envOr(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
