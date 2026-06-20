// Package settings is rill's runtime configuration layer. It exposes a curated
// set of config knobs with a clear precedence — env var > DB value > built-in
// default — and a UI/API surface for the ones that are safe to change at
// runtime. Secrets are never returned to callers (only a "configured" flag).
//
// Design notes:
//   - The registry (below) is the single source of truth for what exists, how
//     it's grouped, its type/bounds, and its exposure (editable / read-only /
//     secret). The API and UI are generated from it.
//   - env wins and LOCKS: if a setting's env var is set, the DB value is
//     ignored and the field is not editable (an operator's explicit env config
//     is authoritative). This is the "respect env" contract.
//   - The Service is process-global and nil-safe: getters work (env→default)
//     even before Init(), so tests and early boot never panic.
//   - No import of internal/memory — memory imports settings (orient reads it),
//     so settings carries its own tiny SurrealDB HTTP client to avoid a cycle.
package settings

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Kind is the value type of a setting (drives parsing + the UI control).
type Kind string

const (
	KindInt    Kind = "int"
	KindBool   Kind = "bool"
	KindString Kind = "string"
	KindEnum   Kind = "enum"
)

// Setting is the static metadata for one config knob.
type Setting struct {
	Key     string   `json:"key"`               // canonical key, e.g. "orient.recency_days"
	Env     string   `json:"env"`               // backing env var name
	Group   string   `json:"group"`             // UI grouping
	Label   string   `json:"label"`             // human label
	Desc    string   `json:"desc"`              // one-line explanation
	Kind    Kind     `json:"kind"`              // int|bool|string|enum
	Default string   `json:"default"`           // default in string form
	Options []string `json:"options,omitempty"` // static enum choices
	// OptionsSource tells the UI to populate a dropdown dynamically rather than
	// from Options — e.g. "entity:person" → fetch person entities (value = record
	// id, label = name). Value is still validated as a plain string server-side.
	OptionsSource string `json:"options_source,omitempty"`
	Min           *int   `json:"min,omitempty"`  // int lower bound (inclusive)
	Max           *int   `json:"max,omitempty"`  // int upper bound (inclusive)
	Unit          string `json:"unit,omitempty"` // e.g. "days", "chars", "bytes"
	Editable      bool   `json:"editable"`       // user-settable (when not env-pinned)
	Hot           bool   `json:"hot"`            // applies live (no restart)
	Secret        bool   `json:"secret"`         // value never leaves the server
}

func intp(n int) *int { return &n }

// Registry is the closed set of known settings. Order = display order.
var Registry = []Setting{
	// ---- Orient & Memory (editable, hot — orient re-reads per call) ----
	{Key: "orient.recency_days", Env: "RILL_ORIENT_RECENCY_DAYS", Group: "Orient & Memory",
		Label: "Project recency window", Kind: KindInt, Default: "30", Min: intp(1), Max: intp(3650), Unit: "days",
		Editable: true, Hot: true,
		Desc: "A project drops out of orient if its last edit is older than this. Edges stay valid — only visibility changes."},
	{Key: "orient.mem_chars", Env: "RILL_ORIENT_MEM_CHARS", Group: "Orient & Memory",
		Label: "Recent-memory headline length", Kind: KindInt, Default: "200", Min: intp(0), Max: intp(2000), Unit: "chars",
		Editable: true, Hot: true,
		Desc: "Caps each recent-memory line in orient to keep it lean for small-context models. 0 = no truncation (full summaries)."},
	{Key: "orient.card_chars", Env: "RILL_ORIENT_CARD_CHARS", Group: "Orient & Memory",
		Label: "Entity-card line length", Kind: KindInt, Default: "280", Min: intp(0), Max: intp(2000), Unit: "chars",
		Editable: true, Hot: true,
		Desc: "Caps each Identity/Facts/Decisions line when an entity card renders into orient. 0 = no truncation. get_entity always shows full text."},
	{Key: "orient.owner_entity", Env: "RILL_OWNER_ENTITY", Group: "Orient & Memory",
		Label: "Owner entity", Kind: KindString, Default: "", OptionsSource: "entity:person",
		Editable: true, Hot: true,
		Desc: "The 'you' entity — pinned first in orient's Identity section. Pick a person."},

	// ---- MCP (editable, restart) ----
	{Key: "mcp.compact_tools", Env: "RILL_COMPACT_TOOLS", Group: "MCP",
		Label: "Tool output mode", Kind: KindEnum, Default: "full", Options: []string{"full", "compact", "names"},
		Editable: true, Hot: false,
		Desc: "How MCP tool schemas are presented. compact = trimmed; names = names only. Applies on restart."},

	// ---- System (editable, restart) ----
	{Key: "system.log_format", Env: "RILL_LOG_FORMAT", Group: "System",
		Label: "Log format", Kind: KindEnum, Default: "text", Options: []string{"text", "json"},
		Editable: true, Hot: false, Desc: "Server log output format. Applies on restart."},

	// ---- Limits (read-only display) ----
	{Key: "limits.max_body_bytes", Env: "RILL_MAX_BODY_BYTES", Group: "Limits",
		Label: "Max request body", Kind: KindInt, Default: "10485760", Unit: "bytes",
		Desc: "Largest accepted HTTP request body. Env-only — tied to the SurrealDB decoder limit."},
	{Key: "limits.max_doc_bytes", Env: "RILL_MAX_DOC_BYTES", Group: "Limits",
		Label: "Max document size", Kind: KindInt, Default: "1048576", Unit: "bytes",
		Desc: "Largest accepted standalone document body. Env-only."},

	// ---- Auth & Security (read-only display) ----
	{Key: "auth.mode", Env: "RILL_AUTH_MODE", Group: "Auth & Security",
		Label: "Auth mode", Kind: KindString, Default: "local", Desc: "local | proxy | oidc. Set at startup."},
	{Key: "auth.proxy_header", Env: "RILL_AUTH_PROXY_HEADER", Group: "Auth & Security",
		Label: "Proxy identity header", Kind: KindString, Default: "X-Forwarded-User",
		Desc: "Header a trusted reverse proxy sets to assert the user. Security boundary — env-only."},
	{Key: "auth.trusted_proxy_ips", Env: "RILL_TRUSTED_PROXY_IPS", Group: "Auth & Security",
		Label: "Trusted proxy IPs", Kind: KindString, Default: "",
		Desc: "IPs whose proxy-identity header is honored. A security boundary — env-only, never UI-settable."},
	{Key: "auth.allow_remote_setup", Env: "RILL_ALLOW_REMOTE_SETUP", Group: "Auth & Security",
		Label: "Allow remote setup", Kind: KindBool, Default: "false",
		Desc: "Whether first-run /setup is reachable from non-loopback networks. Env-only."},
	{Key: "auth.public_url", Env: "RILL_PUBLIC_URL", Group: "Auth & Security",
		Label: "Public URL", Kind: KindString, Default: "",
		Desc: "External base URL used for OIDC redirects/links. Set at startup."},
	{Key: "oidc.enabled", Env: "RILL_OIDC_ENABLED", Group: "Auth & Security",
		Label: "OIDC enabled", Kind: KindBool, Default: "false", Desc: "Whether OIDC/SSO login is active."},
	{Key: "oidc.issuer", Env: "RILL_OIDC_ISSUER", Group: "Auth & Security",
		Label: "OIDC issuer", Kind: KindString, Default: "", Desc: "OIDC provider issuer URL."},
	{Key: "oidc.client_id", Env: "RILL_OIDC_CLIENT_ID", Group: "Auth & Security",
		Label: "OIDC client ID", Kind: KindString, Default: "", Desc: "OIDC client identifier."},
	{Key: "oidc.client_secret", Env: "RILL_OIDC_CLIENT_SECRET", Group: "Auth & Security",
		Label: "OIDC client secret", Kind: KindString, Default: "", Secret: true,
		Desc: "OIDC client secret. Never displayed — only whether it is configured."},

	// ---- Database (read-only display; password is secret) ----
	{Key: "db.surreal_url", Env: "RILL_SURREAL_URL", Group: "Database",
		Label: "SurrealDB URL", Kind: KindString, Default: "http://127.0.0.1:8001", Desc: "Memory store connection URL."},
	{Key: "db.surreal_ns", Env: "RILL_SURREAL_NS", Group: "Database",
		Label: "Namespace", Kind: KindString, Default: "rill"},
	{Key: "db.surreal_db", Env: "RILL_SURREAL_DB", Group: "Database",
		Label: "Database", Kind: KindString, Default: "main"},
	{Key: "db.surreal_pass", Env: "RILL_SURREAL_PASS", Group: "Database",
		Label: "SurrealDB password", Kind: KindString, Default: "", Secret: true,
		Desc: "Never displayed — only whether it is configured."},

	// ---- Embedding (read-only display) ----
	{Key: "embedding.model", Env: "EMBEDDING_MODEL", Group: "Embedding",
		Label: "Embedding model", Kind: KindString, Default: "", Desc: "Model used to embed memory summaries."},
	{Key: "embedding.hnsw_dimension", Env: "RILL_HNSW_DIMENSION", Group: "Embedding",
		Label: "Vector dimension", Kind: KindInt, Default: "1536",
		Desc: "HNSW index dimension. Must match the embedder; changing it after data exists breaks recall — env-only."},
	{Key: "embedding.openrouter_key", Env: "OPENROUTER_KEY_FILE", Group: "Embedding",
		Label: "OpenRouter key", Kind: KindString, Default: "", Secret: true,
		Desc: "Path to the embedding API key file. Never displayed — only whether it is configured."},

	// ---- Server (read-only display) ----
	{Key: "server.bind", Env: "RILL_BIND", Group: "Server",
		Label: "Bind address", Kind: KindString, Default: "", Desc: "Listen interface. Empty = all. Set at startup."},
	{Key: "server.port", Env: "RILL_PORT", Group: "Server",
		Label: "Port", Kind: KindString, Default: "9090", Desc: "HTTP listen port. Set at startup."},
	{Key: "server.data_dir", Env: "RILL_DATA_DIR", Group: "Server",
		Label: "Data directory", Kind: KindString, Default: "", Desc: "Where blob data lives. Set at startup."},

	// ---- Appearance (editable, hot; no env backing) ----
	// Theme is the OKLCH knob object (JSON) shared by the web app and the macOS
	// sidecar so a theme change in either follows the user everywhere. Stored as
	// one app_setting row; clients read on load and PATCH on change.
	{Key: "appearance.theme", Group: "Appearance",
		Label: "Theme", Kind: KindString, Default: "",
		Editable: true, Hot: true,
		Desc: "Active OKLCH theme knobs (JSON), synced across the web app and the sidecar. Empty = client default."},

	// ---- Capture defaults (editable, hot; no env backing) ----
	{Key: "capture.default_kind", Group: "Capture",
		Label: "Default capture kind", Kind: KindEnum, Default: "fact",
		Options:  []string{"decision", "preference", "insight", "procedure", "fact", "identity", "rule", "idea"},
		Editable: true, Hot: true,
		Desc: "Kind pre-selected in the sidecar's capture card."},
	{Key: "capture.default_project", Group: "Capture",
		Label: "Default capture project", Kind: KindString, Default: "",
		Editable: true, Hot: true,
		Desc: "Project pre-filled when capturing a memory. Empty = global (no project)."},
}

// byKey indexes the registry for O(1) lookup.
var byKey = func() map[string]Setting {
	m := make(map[string]Setting, len(Registry))
	for _, s := range Registry {
		m[s.Key] = s
	}
	return m
}()

// Resolved is a setting plus its currently-effective value and provenance.
type Resolved struct {
	Setting
	Value      string `json:"value,omitempty"` // omitted for secrets
	Source     string `json:"source"`          // env | db | default
	Configured bool   `json:"configured"`      // for secrets: is a value present
	Locked     bool   `json:"locked"`          // pinned by env → not editable now
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// Service resolves settings against env, a DB-backed override map, and defaults.
// It is safe for concurrent use and safe to call with a nil receiver (falls back
// to env→default), so getters never panic before Init().
type Service struct {
	client *sqlClient
	mu     sync.RWMutex
	dbVals map[string]string // key -> stored value (DB overrides)
}

var (
	global *Service
	gmu    sync.RWMutex
)

// Init builds the global Service from env-derived SurrealDB config and loads any
// stored overrides. Safe to call once at boot. If the DB is unreachable, the
// Service still works (env→default) and just has no overrides.
func Init(ctx context.Context) *Service {
	s := &Service{client: newSQLClientFromEnv(), dbVals: map[string]string{}}
	_ = s.Refresh(ctx) // best-effort; env/default still work without it
	gmu.Lock()
	global = s
	gmu.Unlock()
	return s
}

// Get returns the global Service, or a zero (env-only) Service if Init() hasn't
// run. Never nil.
func Get() *Service {
	gmu.RLock()
	s := global
	gmu.RUnlock()
	if s == nil {
		return &Service{}
	}
	return s
}

// Refresh reloads the DB override map.
func (s *Service) Refresh(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	vals, err := s.client.loadAll(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.dbVals = vals
	s.mu.Unlock()
	return nil
}

// resolve returns the effective value and its source for a key.
func (s *Service) resolve(key string) (val, source string) {
	def := byKey[key].Default
	envName := byKey[key].Env
	if envName != "" {
		if v, ok := os.LookupEnv(envName); ok && v != "" {
			return v, "env"
		}
	}
	if s != nil {
		s.mu.RLock()
		v, ok := s.dbVals[key]
		s.mu.RUnlock()
		if ok && v != "" {
			return v, "db"
		}
	}
	return def, "default"
}

// envPinned reports whether the key's env var is set (and thus locks the field).
func envPinned(key string) bool {
	e := byKey[key].Env
	if e == "" {
		return false
	}
	v, ok := os.LookupEnv(e)
	return ok && v != ""
}

// ---- typed getters (used by consumers) ----

func (s *Service) str(key string) string { v, _ := s.resolve(key); return v }

func (s *Service) intVal(key string) int {
	v, _ := s.resolve(key)
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		d, _ := strconv.Atoi(byKey[key].Default)
		return d
	}
	return n
}

// OrientRecencyDays is the orient project-recency window in days.
func (s *Service) OrientRecencyDays() int {
	n := s.intVal("orient.recency_days")
	if n <= 0 {
		return 30
	}
	return n
}

// OrientMemChars caps recent-memory headline length in orient (<=0 disables).
func (s *Service) OrientMemChars() int { return s.intVal("orient.mem_chars") }

// OrientCardChars caps Identity/Facts/Decisions line length when an entity card
// renders into orient (<=0 disables). get_entity always returns the full card.
func (s *Service) OrientCardChars() int { return s.intVal("orient.card_chars") }

// OwnerEntity is the record id pinned first in orient's Identity section ("").
func (s *Service) OwnerEntity() string { return strings.TrimSpace(s.str("orient.owner_entity")) }

// ---- API surface ----

// List returns every registered setting resolved against env/DB/default.
// Secret values are never included — only Configured.
func (s *Service) List() []Resolved {
	out := make([]Resolved, 0, len(Registry))
	for _, def := range Registry {
		val, source := s.resolve(def.Key)
		r := Resolved{Setting: def, Source: source, Locked: envPinned(def.Key)}
		if def.Secret {
			r.Configured = val != ""
			r.Value = "" // never leak
		} else {
			r.Value = val
		}
		out = append(out, r)
	}
	return out
}

// SettingMeta exposes a single setting's metadata (for validation/handlers).
func SettingMeta(key string) (Setting, bool) { s, ok := byKey[key]; return s, ok }
