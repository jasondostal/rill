package settings

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

// sqlClient is a tiny SurrealDB HTTP /sql client for the app_setting table.
// Self-contained on purpose: internal/memory imports this package (orient reads
// settings), so settings cannot import memory's client without a cycle.
type sqlClient struct {
	url, ns, db, user, pass string
	http                    *http.Client
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func newSQLClientFromEnv() *sqlClient {
	return &sqlClient{
		url:  strings.TrimRight(envOr("RILL_SURREAL_URL", "http://127.0.0.1:8001"), "/"),
		ns:   envOr("RILL_SURREAL_NS", "rill"),
		db:   envOr("RILL_SURREAL_DB", "main"),
		user: envOr("RILL_SURREAL_USER", "root"),
		pass: envOr("RILL_SURREAL_PASS", "root"),
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

type surrealResult struct {
	Status string          `json:"status"`
	Result json.RawMessage `json:"result"`
	Detail string          `json:"detail,omitempty"`
}

func (c *sqlClient) sql(ctx context.Context, query string) ([]surrealResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/sql", bytes.NewReader([]byte(query)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Surreal-NS", c.ns)
	req.Header.Set("Surreal-DB", c.db)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.user, c.pass)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("surreal request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("surreal HTTP %d", resp.StatusCode)
	}
	var out []surrealResult
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	for _, r := range out {
		if r.Status == "ERR" {
			return out, fmt.Errorf("surreal stmt error: %s", r.Detail)
		}
	}
	return out, nil
}

// esc produces a SurrealQL-safe quoted string literal.
func esc(s string) string { b, _ := json.Marshal(s); return string(b) }

// loadAll reads every stored override as key -> value.
func (c *sqlClient) loadAll(ctx context.Context) (map[string]string, error) {
	res, err := c.sql(ctx, "SELECT k, value FROM app_setting;")
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return out, nil
	}
	var rows []struct {
		K     string `json:"k"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.K != "" {
			out[r.K] = r.Value
		}
	}
	return out, nil
}

// set upserts one override. The record id is the canonical key (backtick-quoted
// so dots are allowed), and `k` mirrors it for SELECT projection.
func (c *sqlClient) set(ctx context.Context, key, value, by string) error {
	stmt := fmt.Sprintf(
		"UPSERT app_setting:`%s` SET k = %s, value = %s, updated_at = time::now(), updated_by = %s;",
		key, esc(key), esc(value), esc(by))
	_, err := c.sql(ctx, stmt)
	return err
}
