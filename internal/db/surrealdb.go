package db

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	rilllog "github.com/jasondostal/rill/internal/log"
	"github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"
)

// ErrInvalidRecordID indicates a record ID failed parsing (malformed input).
// REST handlers should map this to 400 Bad Request.
var ErrInvalidRecordID = errors.New("invalid record id")

// ErrWrongTable indicates a record ID belongs to a different table than expected.
// REST handlers should map this to 400 Bad Request.
var ErrWrongTable = errors.New("wrong table for record id")

// SplitRecordID parses a "table:id" reference into its components.
// Returns ok=false if the input doesn't look like a record reference,
// which lets callers reject malformed input before it reaches SurrealQL.
// SurrealDB record IDs may contain colons in the id portion, so we split
// on the FIRST colon only.
func SplitRecordID(s string) (table, id string, ok bool) {
	if s == "" {
		return "", "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			t, r := s[:i], s[i+1:]
			if t == "" || r == "" {
				return "", "", false
			}
			// Conservative table-name validation: letters, digits, underscore.
			for _, c := range t {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
					(c >= '0' && c <= '9') || c == '_') {
					return "", "", false
				}
			}
			// Conservative id validation: alphanumeric, underscore, hyphen.
			// SurrealDB-generated IDs use these characters exclusively.
			for _, c := range r {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
					(c >= '0' && c <= '9') || c == '_' || c == '-') {
					return "", "", false
				}
			}
			return t, r, true
		}
	}
	return "", "", false
}

// RequireTable validates that a record ID belongs to the expected table.
// Returns an error if parsing fails or the table doesn't match.
// Errors wrap ErrInvalidRecordID or ErrWrongTable so callers can distinguish
// shape errors (bad input → 400) from other failures.
func RequireTable(recordID, expectedTable string) error {
	table, _, ok := SplitRecordID(recordID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrInvalidRecordID, recordID)
	}
	if table != expectedTable {
		return fmt.Errorf("%w: expected %s, got table %q in: %s", ErrWrongTable, expectedTable, table, recordID)
	}
	return nil
}

// RecordID extracts the canonical "table:id" string from a SurrealDB record map,
// handling all three shapes the driver may return: models.RecordID, a string,
// or a map with Table/ID fields.
func RecordID(m map[string]any) string {
	if rid, ok := m["id"].(models.RecordID); ok {
		return fmt.Sprintf("%s:%v", rid.Table, rid.ID)
	}
	if s, ok := m["id"].(string); ok {
		return s
	}
	if obj, ok := m["id"].(map[string]any); ok {
		t, _ := obj["Table"].(string)
		i, _ := obj["ID"].(string)
		if t != "" && i != "" {
			return fmt.Sprintf("%s:%s", t, i)
		}
	}
	return ""
}

// IntField extracts an int from a row field, handling all numeric types
// SurrealDB's driver may return (float64, int64, uint64, int).
// Returns 0 if the field is missing or not numeric.
func IntField(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int64:
		return int(v)
	case uint64:
		// Clamp to MaxInt to avoid silent wraparound on 32-bit platforms
		// or unrealistically huge values. Real counts/IDs never approach this.
		if v > uint64(math.MaxInt) {
			return math.MaxInt
		}
		return int(v)
	case int:
		return v
	}
	return 0
}

// StringField extracts a string from a SurrealDB row map, or "".
func StringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// FloatField extracts a float64 from a SurrealDB row map, or 0.
// Handles the type variants the SurrealDB driver can return for FLOAT
// columns: float64, float32 (CBOR encoding picks float32 for some values),
// and int variants for fields stored as integers.
func FloatField(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int64:
		return float64(v)
	case int:
		return float64(v)
	}
	return 0
}

// TimeField extracts a time.Time from a SurrealDB row map.
// SurrealDB Go driver returns datetime as models.CustomDateTime{Time: time.Time}.
func TimeField(m map[string]any, key string) time.Time {
	v := m[key]
	if v == nil {
		return time.Time{}
	}
	if cd, ok := v.(models.CustomDateTime); ok {
		return cd.Time
	}
	if t, ok := v.(time.Time); ok {
		return t
	}
	if s, ok := v.(string); ok {
		t, err := time.Parse(time.RFC3339, s)
		if err == nil {
			return t
		}
	}
	return time.Time{}
}

// DB wraps a SurrealDB WebSocket connection with reactive reconnect
// behavior. If the underlying WS closes (SurrealDB restart, network
// hiccup), the next failing call triggers a reconnect via dial() and
// retries once against the new connection. Reconnect attempts are
// serialized by reconnectMu and rate-limited to one per second so a
// burst of concurrent failures doesn't storm the upstream.
//
// Without this: if SurrealDB restarts, every subsequent rill query
// returns "connection closed" forever, and the only fix is restarting
// rill. With this: callers see a small latency spike and a Warn log
// line, then service continues.
type DB struct {
	cfg           Config
	mu            sync.RWMutex // protects db pointer during atomic swap
	db            *surrealdb.DB
	reconnectMu   sync.Mutex // serializes reconnect attempts
	lastReconnect time.Time  // for rate-limit + dedupe check
}

// DB returns the underlying surrealdb.DB connection (may be nil). The
// returned pointer can become stale immediately if a reconnect happens —
// callers that need a stable handle should use the higher-level helpers
// (Query, Create, etc.) which handle reconnect transparently.
func (d *DB) DB() *surrealdb.DB { return d.conn() }

// conn is the internal locked-read accessor. All hot-path methods use
// this so the conn pointer is consistent for the duration of a single
// query, even if reconnect swaps it underneath.
func (d *DB) conn() *surrealdb.DB {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.db
}

// isConnClosedErr reports whether err matches the SurrealDB driver's
// connection-closed family — the cases where a reconnect can recover.
// Conservative match: only well-known closed-connection error substrings.
// Don't add timeout / context-cancel here — those are caller-driven and
// reconnecting wouldn't help.
func isConnClosedErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection closed") ||
		strings.Contains(s, "websocket: close") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "use of closed")
}

// reconnect re-establishes the WebSocket and re-applies sign-in + ns/db.
// Serialized via reconnectMu so concurrent failing queries trigger at
// most one reconnect attempt. Rate-limited via lastReconnect so a quick
// burst of failures from N goroutines doesn't fire N reconnects — only
// the first goroutine inside the lock actually dials; the rest fall
// through if the previous attempt was recent.
//
// Returns the error from dial. Caller decides whether to retry the
// failing query.
func (d *DB) reconnect(ctx context.Context) error {
	d.reconnectMu.Lock()
	defer d.reconnectMu.Unlock()

	// If a sibling goroutine reconnected within the last second, the
	// current conn is probably fresh — don't dial again. We don't
	// re-verify with a ping here because the caller will issue its
	// own real query right after this returns; if THAT fails again
	// with conn-closed, the retry budget is spent and the error
	// surfaces. Keeps the lock window short.
	if time.Since(d.lastReconnect) < time.Second {
		return nil
	}

	rilllog.Logger().Warn("db: WebSocket closed, reconnecting", "url", d.cfg.URL)
	if err := d.dial(ctx); err != nil {
		rilllog.Logger().Error("db: reconnect failed", "error", err)
		return err
	}
	d.lastReconnect = time.Now()
	rilllog.Logger().Info("db: reconnect successful")
	return nil
}

// dial performs one connect + sign-in + use cycle and atomically swaps
// the resulting connection into d.db. Used by Connect (initial dial)
// and reconnect (post-failure dial). The old conn, if any, is closed
// AFTER the swap so in-flight callers fail fast and head into retry.
func (d *DB) dial(ctx context.Context) error {
	conn, err := surrealdb.New(d.cfg.URL)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	if _, err := conn.SignIn(ctx, &surrealdb.Auth{
		Username: d.cfg.User,
		Password: d.cfg.Pass,
	}); err != nil {
		_ = conn.Close(ctx)
		return fmt.Errorf("signin: %w", err)
	}
	if err := conn.Use(ctx, d.cfg.NS, d.cfg.DB); err != nil {
		_ = conn.Close(ctx)
		return fmt.Errorf("use ns/db: %w", err)
	}
	d.mu.Lock()
	old := d.db
	d.db = conn
	d.mu.Unlock()
	if old != nil {
		_ = old.Close(ctx)
	}
	return nil
}

// Config holds database connection parameters.
type Config struct {
	URL  string
	User string
	Pass string
	NS   string
	DB   string
}

// ConfigFromEnv reads database configuration from environment variables.
func ConfigFromEnv() Config {
	pass := envOrDefault("SURREAL_PASS", "root")
	if pass == "root" {
		rilllog.Logger().Warn("db: using default database password 'root'. Set SURREAL_PASS to a strong password.")
	}
	return Config{
		URL:  envOrDefault("SURREAL_URL", "ws://localhost:8000"),
		User: envOrDefault("SURREAL_USER", "root"),
		Pass: pass,
		NS:   envOrDefault("SURREAL_NS", "rill"),
		DB:   envOrDefault("SURREAL_DB", "rill"),
	}
}

// hnswDimension reads the desired entity-embedding dimension for the HNSW
// vector index. Default 1536 (openai/text-embedding-3-small). Override via
// RILL_HNSW_DIMENSION for other embedders: voyage-3=1024, nomic-v1.5=768,
// bge-large=2560, text-embedding-3-large=3072.
//
// Important: this must match the embedder actually in use. Mismatch makes
// DEFINE INDEX reject with "Incorrect vector dimension" once any row in the
// table holds an embedding.
func hnswDimension() int {
	if v := os.Getenv("RILL_HNSW_DIMENSION"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 1536
}

// Connect establishes the initial SurrealDB connection. After Connect
// returns successfully, the DB will transparently reconnect on
// connection-closed errors (see DB type docs).
func Connect(cfg Config) (*DB, error) {
	d := &DB{cfg: cfg}
	if err := d.dial(context.Background()); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return d, nil
}

// Ping verifies the database connection is alive. Triggers a reconnect
// if the WS is dead — so the /health endpoint is self-healing.
func (d *DB) Ping(ctx context.Context) error {
	conn := d.conn()
	if conn == nil {
		return fmt.Errorf("database not connected")
	}
	_, err := surrealdb.Query[any](ctx, conn, "RETURN true", nil)
	if err != nil && isConnClosedErr(err) {
		if rerr := d.reconnect(ctx); rerr != nil {
			return rerr
		}
		_, err = surrealdb.Query[any](ctx, d.conn(), "RETURN true", nil)
	}
	return err
}

// SchemaVersion tracks the current DDL version. Bump this when non-idempotent
// changes are added. Idempotent IF NOT EXISTS changes don't need a bump.
const SchemaVersion = "2026-05-26-v9" // v9: document table + doc_about relation (standalone markdown docs, outside the memory pipeline)

// SetupSchema creates the initial tables and indexes.
// After applying DDL, records the schema version so future non-idempotent
// migrations have a baseline to check against.
func (d *DB) SetupSchema(ctx context.Context) error {
	// One canonical schema (internal/db/schema.surql), applied idempotently on
	// every boot. No _migrations version-gating, no inline DDL — the app ships a
	// single schema. Every statement is IF NOT EXISTS, so this is a no-op on an
	// existing database. HNSW index builds can take a while on a populated
	// namespace; that is normal.
	rilllog.Logger().Info("schema: applying canonical schema")
	startT := time.Now()
	if _, err := surrealdb.Query[any](ctx, d.db, schemaSQL, nil); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	rilllog.Logger().Info("schema: applied", "duration", time.Since(startT).Round(time.Millisecond))
	return nil
}

// Query executes a raw SurrealQL query and returns results.
// SurrealDB returns arrays of records for SELECT/CREATE/UPDATE.
//
// Transparently reconnects + retries once on connection-closed errors
// (SurrealDB restart, network blip). Other errors (auth failure, query
// syntax, context cancel) surface immediately without retry.
func (d *DB) Query(ctx context.Context, query string, vars map[string]any) ([]map[string]any, error) {
	conn := d.conn()
	if conn == nil {
		return nil, fmt.Errorf("database not connected")
	}
	results, err := surrealdb.Query[[]map[string]any](ctx, conn, query, vars)
	if err != nil && isConnClosedErr(err) {
		if rerr := d.reconnect(ctx); rerr != nil {
			return nil, fmt.Errorf("query: reconnect failed: %w", rerr)
		}
		results, err = surrealdb.Query[[]map[string]any](ctx, d.conn(), query, vars)
	}
	if err != nil {
		return nil, err
	}
	if results == nil {
		return nil, nil
	}

	var rows []map[string]any
	for _, r := range *results {
		if r.Result != nil {
			rows = append(rows, r.Result...)
		}
	}
	return rows, nil
}

// Create inserts a record into a table and returns the created record.
// Same reconnect semantics as Query.
func (d *DB) Create(ctx context.Context, table string, data map[string]any) (map[string]any, error) {
	conn := d.conn()
	if conn == nil {
		return nil, fmt.Errorf("database not connected")
	}
	q := fmt.Sprintf("CREATE %s CONTENT $data RETURN AFTER", table)
	params := map[string]any{"data": data}
	results, err := surrealdb.Query[[]map[string]any](ctx, conn, q, params)
	if err != nil && isConnClosedErr(err) {
		if rerr := d.reconnect(ctx); rerr != nil {
			return nil, fmt.Errorf("create: reconnect failed: %w", rerr)
		}
		results, err = surrealdb.Query[[]map[string]any](ctx, d.conn(), q, params)
	}
	if err != nil {
		return nil, err
	}
	if results == nil || len(*results) == 0 || (*results)[0].Result == nil {
		return nil, fmt.Errorf("no result from create")
	}
	records := (*results)[0].Result
	if len(records) == 0 {
		return nil, fmt.Errorf("no record created")
	}
	return records[0], nil
}

// CreateWithQuery inserts a record using a SurrealQL query string instead of
// parameterised CONTENT. This is needed when fields require SurrealDB functions
// like type::record() that cannot be passed through the driver's parameter map.
// Same reconnect semantics as Query.
func (d *DB) CreateWithQuery(ctx context.Context, query string, vars map[string]any) (map[string]any, error) {
	conn := d.conn()
	if conn == nil {
		return nil, fmt.Errorf("database not connected")
	}
	results, err := surrealdb.Query[[]map[string]any](ctx, conn, query, vars)
	if err != nil && isConnClosedErr(err) {
		if rerr := d.reconnect(ctx); rerr != nil {
			return nil, fmt.Errorf("create: reconnect failed: %w", rerr)
		}
		results, err = surrealdb.Query[[]map[string]any](ctx, d.conn(), query, vars)
	}
	if err != nil {
		return nil, err
	}
	if results == nil || len(*results) == 0 || (*results)[0].Result == nil {
		return nil, fmt.Errorf("no result from create")
	}
	records := (*results)[0].Result
	if len(records) == 0 {
		return nil, fmt.Errorf("no record created")
	}
	return records[0], nil
}

// Close closes the current database connection. Safe to call once; the
// next Query / Create call after Close will fail with "database not
// connected" rather than attempting a reconnect.
func (d *DB) Close() error {
	d.mu.Lock()
	conn := d.db
	d.db = nil
	d.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close(context.Background())
}

// QueryRecord runs a query that operates on a single SurrealDB record.
// The sqlTemplate must contain exactly one %s placeholder; it is substituted
// with `type::record($_t, $_i)` and the parsed table/id are bound as
// parameters. This is the safe replacement for fmt.Sprintf("UPDATE %s ...", id)
// patterns where id originates from outside the server.
//
// Extra named parameters can be passed via vars; reserved keys "_t" and "_i"
// will be overwritten.
//
// Returns an error if idStr does not parse as a valid "table:id" reference.
func (d *DB) QueryRecord(ctx context.Context, sqlTemplate, idStr string, vars map[string]any) ([]map[string]any, error) {
	table, id, ok := SplitRecordID(idStr)
	if !ok {
		return nil, fmt.Errorf("invalid record id: %q", idStr)
	}
	if n := strings.Count(sqlTemplate, "%s"); n != 1 {
		return nil, fmt.Errorf("QueryRecord template must have exactly one %%s placeholder, got %d", n)
	}
	// Use direct string interpolation for record IDs — SurrealDB's
	// type::record() syntax is rejected in graph traversal statements.
	return d.Query(ctx, fmt.Sprintf(sqlTemplate, fmt.Sprintf("%s:%s", table, id)), vars)
}

// QueryRelate runs a query that references two SurrealDB records — typically
// a RELATE statement. The sqlTemplate must contain exactly two %s placeholders
// substituted with parameterized record references.
func (d *DB) QueryRelate(ctx context.Context, sqlTemplate, fromID, toID string, vars map[string]any) ([]map[string]any, error) {
	ft, fi, ok1 := SplitRecordID(fromID)
	tt, ti, ok2 := SplitRecordID(toID)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("invalid record id(s): from=%q to=%q", fromID, toID)
	}
	if n := strings.Count(sqlTemplate, "%s"); n != 2 {
		return nil, fmt.Errorf("QueryRelate template must have exactly two %%s placeholders, got %d", n)
	}
	// Use direct string interpolation for record IDs — SurrealDB's
	// type::record() syntax is rejected in RELATE statements.
	return d.Query(ctx, fmt.Sprintf(sqlTemplate,
		fmt.Sprintf("%s:%s", ft, fi),
		fmt.Sprintf("%s:%s", tt, ti)), vars)
}

// StrField extracts a string from a row map.
func StrField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
