-- ============================================================
-- rill — intentional memory schema (SurrealDB 3.0.5)
-- ============================================================
-- Memories are saved intentionally by an author (human handle or agent like claude).
-- The caller declares entities + edges. No auto-extraction in the
-- hot path. hand_notes is human-curated; derived_card is auto-rendered
-- from the graph (edges + memories that touch the entity).
--
-- Namespace: rill
-- Database:  main
-- ============================================================


-- ------------------------------------------------------------
-- memory — the storage unit. Intentional save by an author.
-- ------------------------------------------------------------

DEFINE TABLE IF NOT EXISTS memory SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS summary       ON memory TYPE string
    ASSERT string::len($value) > 0 AND string::len($value) <= 600;
DEFINE FIELD IF NOT EXISTS details       ON memory TYPE option<string>;            -- no length cap
-- OVERWRITE (not IF NOT EXISTS) so existing DBs widen the allowed-kind set on
-- boot — IF NOT EXISTS no-ops on a field that already exists. 'idea' is the
-- "look into / todo" kind added for the macOS sidecar's capture flow.
DEFINE FIELD OVERWRITE kind          ON memory TYPE string
    ASSERT $value IN ['decision', 'preference', 'insight', 'procedure', 'fact', 'identity', 'rule', 'idea'];
DEFINE FIELD IF NOT EXISTS tags          ON memory TYPE array<string> DEFAULT [];
DEFINE FIELD IF NOT EXISTS author        ON memory TYPE string;                    -- '<human-handle>' | 'claude' | named-agent
DEFINE FIELD IF NOT EXISTS project       ON memory TYPE option<string>;            -- nullable; null = global
DEFINE FIELD IF NOT EXISTS valence       ON memory TYPE option<string>
    ASSERT $value = NONE OR $value IN ['positive', 'negative', 'neutral'];

-- Bi-temporal validity (mirrors the edge model)
DEFINE FIELD IF NOT EXISTS valid_from    ON memory TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS valid_until   ON memory TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS superseded_by ON memory TYPE option<record<memory>>;

-- Lifecycle
DEFINE FIELD IF NOT EXISTS is_active     ON memory TYPE bool DEFAULT true;
DEFINE FIELD IF NOT EXISTS pinned        ON memory TYPE bool DEFAULT false;  -- ★ user-pinned (sidecar / web row action)
DEFINE FIELD IF NOT EXISTS open          ON memory TYPE bool DEFAULT false; -- open loop; existing rows read as NONE == closed
DEFINE FIELD IF NOT EXISTS opened_at     ON memory TYPE option<datetime>;   -- set when open flips true; retained on close
DEFINE FIELD IF NOT EXISTS created_at    ON memory TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS updated_at    ON memory TYPE datetime DEFAULT time::now();

-- Vector index over `summary` only (not details). 1536-dim, cosine.
DEFINE FIELD IF NOT EXISTS embedding     ON memory TYPE option<array<float>>;
DEFINE INDEX IF NOT EXISTS idx_memory_emb ON memory FIELDS embedding HNSW DIMENSION 1536 DIST COSINE;

-- FTS over summary + details + tags (separate so we can score them differently)
DEFINE ANALYZER IF NOT EXISTS ascii TOKENIZERS blank,class FILTERS ascii,lowercase;
DEFINE INDEX IF NOT EXISTS idx_memory_fts_summary ON memory FIELDS summary FULLTEXT ANALYZER ascii BM25;
DEFINE INDEX IF NOT EXISTS idx_memory_fts_details ON memory FIELDS details FULLTEXT ANALYZER ascii BM25;

DEFINE INDEX IF NOT EXISTS idx_memory_kind ON memory FIELDS kind;
DEFINE INDEX IF NOT EXISTS idx_memory_project ON memory FIELDS project;
DEFINE INDEX IF NOT EXISTS idx_memory_author ON memory FIELDS author;
DEFINE INDEX IF NOT EXISTS idx_memory_active_valid ON memory FIELDS is_active, valid_until;
DEFINE INDEX IF NOT EXISTS idx_memory_created ON memory FIELDS created_at;
DEFINE INDEX IF NOT EXISTS idx_memory_open ON memory FIELDS is_active, open;

-- ------------------------------------------------------------
-- Entity tables — typed, with hand-curated card and promotion flag
-- ------------------------------------------------------------

-- Helper macro pattern: every entity table has the same shape.
-- SurrealDB doesn't have shared field defs, so we repeat.

DEFINE TABLE IF NOT EXISTS person SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS name           ON person TYPE string;
DEFINE FIELD IF NOT EXISTS aliases        ON person TYPE array<string> DEFAULT [];
DEFINE FIELD IF NOT EXISTS summary        ON person TYPE option<string>;          -- short auto-set blurb
DEFINE FIELD IF NOT EXISTS hand_notes   ON person TYPE option<string>;          -- human-edited markdown (UI surface)
DEFINE FIELD IF NOT EXISTS derived_card ON person TYPE option<string>;          -- system-rendered from graph (view only)
DEFINE FIELD IF NOT EXISTS promoted       ON person TYPE bool DEFAULT false;
DEFINE FIELD IF NOT EXISTS first_seen     ON person TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS last_seen      ON person TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS mention_count  ON person TYPE int DEFAULT 1;
DEFINE FIELD IF NOT EXISTS last_edited_by ON person TYPE option<string>;
DEFINE FIELD IF NOT EXISTS last_edited_at ON person TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS embedding      ON person TYPE option<array<float>>;
DEFINE FIELD IF NOT EXISTS merged_into  ON person TYPE option<record>;       -- set when this entity is merged into another (soft-retire)
DEFINE INDEX IF NOT EXISTS idx_person_name ON person FIELDS name UNIQUE;
DEFINE INDEX IF NOT EXISTS idx_person_promoted ON person FIELDS promoted;
DEFINE INDEX IF NOT EXISTS idx_person_emb ON person FIELDS embedding HNSW DIMENSION 1536 DIST COSINE;
DEFINE INDEX IF NOT EXISTS idx_person_last_edited ON person FIELDS last_edited_at;

DEFINE TABLE IF NOT EXISTS project SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS name           ON project TYPE string;
DEFINE FIELD IF NOT EXISTS aliases        ON project TYPE array<string> DEFAULT [];
DEFINE FIELD IF NOT EXISTS summary        ON project TYPE option<string>;
DEFINE FIELD IF NOT EXISTS hand_notes   ON project TYPE option<string>;          -- human-edited markdown (UI surface)
DEFINE FIELD IF NOT EXISTS derived_card ON project TYPE option<string>;          -- system-rendered from graph (view only)
DEFINE FIELD IF NOT EXISTS promoted       ON project TYPE bool DEFAULT false;
DEFINE FIELD IF NOT EXISTS first_seen     ON project TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS last_seen      ON project TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS mention_count  ON project TYPE int DEFAULT 1;
DEFINE FIELD IF NOT EXISTS last_edited_by ON project TYPE option<string>;
DEFINE FIELD IF NOT EXISTS last_edited_at ON project TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS embedding      ON project TYPE option<array<float>>;
DEFINE FIELD IF NOT EXISTS merged_into  ON project TYPE option<record>;       -- set when this entity is merged into another (soft-retire)
DEFINE INDEX IF NOT EXISTS idx_project_name ON project FIELDS name UNIQUE;
DEFINE INDEX IF NOT EXISTS idx_project_promoted ON project FIELDS promoted;
DEFINE INDEX IF NOT EXISTS idx_project_emb ON project FIELDS embedding HNSW DIMENSION 1536 DIST COSINE;
DEFINE INDEX IF NOT EXISTS idx_project_last_edited ON project FIELDS last_edited_at;

DEFINE TABLE IF NOT EXISTS tool SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS name           ON tool TYPE string;
DEFINE FIELD IF NOT EXISTS aliases        ON tool TYPE array<string> DEFAULT [];
DEFINE FIELD IF NOT EXISTS summary        ON tool TYPE option<string>;
DEFINE FIELD IF NOT EXISTS hand_notes   ON tool TYPE option<string>;          -- human-edited markdown (UI surface)
DEFINE FIELD IF NOT EXISTS derived_card ON tool TYPE option<string>;          -- system-rendered from graph (view only)
DEFINE FIELD IF NOT EXISTS promoted       ON tool TYPE bool DEFAULT false;
DEFINE FIELD IF NOT EXISTS first_seen     ON tool TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS last_seen      ON tool TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS mention_count  ON tool TYPE int DEFAULT 1;
DEFINE FIELD IF NOT EXISTS last_edited_by ON tool TYPE option<string>;
DEFINE FIELD IF NOT EXISTS last_edited_at ON tool TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS embedding      ON tool TYPE option<array<float>>;
DEFINE FIELD IF NOT EXISTS merged_into  ON tool TYPE option<record>;       -- set when this entity is merged into another (soft-retire)
DEFINE INDEX IF NOT EXISTS idx_tool_name ON tool FIELDS name UNIQUE;
DEFINE INDEX IF NOT EXISTS idx_tool_promoted ON tool FIELDS promoted;
DEFINE INDEX IF NOT EXISTS idx_tool_emb ON tool FIELDS embedding HNSW DIMENSION 1536 DIST COSINE;
DEFINE INDEX IF NOT EXISTS idx_tool_last_edited ON tool FIELDS last_edited_at;

DEFINE TABLE IF NOT EXISTS organization SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS name           ON organization TYPE string;
DEFINE FIELD IF NOT EXISTS aliases        ON organization TYPE array<string> DEFAULT [];
DEFINE FIELD IF NOT EXISTS summary        ON organization TYPE option<string>;
DEFINE FIELD IF NOT EXISTS hand_notes   ON organization TYPE option<string>;          -- human-edited markdown (UI surface)
DEFINE FIELD IF NOT EXISTS derived_card ON organization TYPE option<string>;          -- system-rendered from graph (view only)
DEFINE FIELD IF NOT EXISTS promoted       ON organization TYPE bool DEFAULT false;
DEFINE FIELD IF NOT EXISTS first_seen     ON organization TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS last_seen      ON organization TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS mention_count  ON organization TYPE int DEFAULT 1;
DEFINE FIELD IF NOT EXISTS last_edited_by ON organization TYPE option<string>;
DEFINE FIELD IF NOT EXISTS last_edited_at ON organization TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS embedding      ON organization TYPE option<array<float>>;
DEFINE FIELD IF NOT EXISTS merged_into  ON organization TYPE option<record>;       -- set when this entity is merged into another (soft-retire)
DEFINE INDEX IF NOT EXISTS idx_org_name ON organization FIELDS name UNIQUE;
DEFINE INDEX IF NOT EXISTS idx_org_promoted ON organization FIELDS promoted;
DEFINE INDEX IF NOT EXISTS idx_org_emb ON organization FIELDS embedding HNSW DIMENSION 1536 DIST COSINE;
DEFINE INDEX IF NOT EXISTS idx_org_last_edited ON organization FIELDS last_edited_at;

DEFINE TABLE IF NOT EXISTS place SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS name           ON place TYPE string;
DEFINE FIELD IF NOT EXISTS aliases        ON place TYPE array<string> DEFAULT [];
DEFINE FIELD IF NOT EXISTS summary        ON place TYPE option<string>;
DEFINE FIELD IF NOT EXISTS hand_notes   ON place TYPE option<string>;          -- human-edited markdown (UI surface)
DEFINE FIELD IF NOT EXISTS derived_card ON place TYPE option<string>;          -- system-rendered from graph (view only)
DEFINE FIELD IF NOT EXISTS promoted       ON place TYPE bool DEFAULT false;
DEFINE FIELD IF NOT EXISTS first_seen     ON place TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS last_seen      ON place TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS mention_count  ON place TYPE int DEFAULT 1;
DEFINE FIELD IF NOT EXISTS last_edited_by ON place TYPE option<string>;
DEFINE FIELD IF NOT EXISTS last_edited_at ON place TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS embedding      ON place TYPE option<array<float>>;
DEFINE FIELD IF NOT EXISTS merged_into  ON place TYPE option<record>;       -- set when this entity is merged into another (soft-retire)
DEFINE INDEX IF NOT EXISTS idx_place_name ON place FIELDS name UNIQUE;
DEFINE INDEX IF NOT EXISTS idx_place_promoted ON place FIELDS promoted;
DEFINE INDEX IF NOT EXISTS idx_place_emb ON place FIELDS embedding HNSW DIMENSION 1536 DIST COSINE;
DEFINE INDEX IF NOT EXISTS idx_place_last_edited ON place FIELDS last_edited_at;

DEFINE TABLE IF NOT EXISTS preference SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS name           ON preference TYPE string;
DEFINE FIELD IF NOT EXISTS aliases        ON preference TYPE array<string> DEFAULT [];
DEFINE FIELD IF NOT EXISTS summary        ON preference TYPE option<string>;
DEFINE FIELD IF NOT EXISTS hand_notes   ON preference TYPE option<string>;          -- human-edited markdown (UI surface)
DEFINE FIELD IF NOT EXISTS derived_card ON preference TYPE option<string>;          -- system-rendered from graph (view only)
DEFINE FIELD IF NOT EXISTS promoted       ON preference TYPE bool DEFAULT false;
DEFINE FIELD IF NOT EXISTS first_seen     ON preference TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS last_seen      ON preference TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS mention_count  ON preference TYPE int DEFAULT 1;
DEFINE FIELD IF NOT EXISTS last_edited_by ON preference TYPE option<string>;
DEFINE FIELD IF NOT EXISTS last_edited_at ON preference TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS embedding      ON preference TYPE option<array<float>>;
DEFINE FIELD IF NOT EXISTS merged_into  ON preference TYPE option<record>;       -- set when this entity is merged into another (soft-retire)
DEFINE INDEX IF NOT EXISTS idx_pref_name ON preference FIELDS name UNIQUE;
DEFINE INDEX IF NOT EXISTS idx_pref_promoted ON preference FIELDS promoted;
DEFINE INDEX IF NOT EXISTS idx_pref_emb ON preference FIELDS embedding HNSW DIMENSION 1536 DIST COSINE;
DEFINE INDEX IF NOT EXISTS idx_pref_last_edited ON preference FIELDS last_edited_at;

DEFINE TABLE IF NOT EXISTS concept SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS name           ON concept TYPE string;
DEFINE FIELD IF NOT EXISTS aliases        ON concept TYPE array<string> DEFAULT [];
DEFINE FIELD IF NOT EXISTS summary        ON concept TYPE option<string>;
DEFINE FIELD IF NOT EXISTS hand_notes   ON concept TYPE option<string>;          -- human-edited markdown (UI surface)
DEFINE FIELD IF NOT EXISTS derived_card ON concept TYPE option<string>;          -- system-rendered from graph (view only)
DEFINE FIELD IF NOT EXISTS promoted       ON concept TYPE bool DEFAULT false;
DEFINE FIELD IF NOT EXISTS first_seen     ON concept TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS last_seen      ON concept TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS mention_count  ON concept TYPE int DEFAULT 1;
DEFINE FIELD IF NOT EXISTS last_edited_by ON concept TYPE option<string>;
DEFINE FIELD IF NOT EXISTS last_edited_at ON concept TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS embedding      ON concept TYPE option<array<float>>;
DEFINE FIELD IF NOT EXISTS merged_into  ON concept TYPE option<record>;       -- set when this entity is merged into another (soft-retire)
DEFINE INDEX IF NOT EXISTS idx_concept_name ON concept FIELDS name UNIQUE;
DEFINE INDEX IF NOT EXISTS idx_concept_promoted ON concept FIELDS promoted;
DEFINE INDEX IF NOT EXISTS idx_concept_emb ON concept FIELDS embedding HNSW DIMENSION 1536 DIST COSINE;
DEFINE INDEX IF NOT EXISTS idx_concept_last_edited ON concept FIELDS last_edited_at;

-- ------------------------------------------------------------
-- mentions — memory -> entity (provenance, no temporal bounds)
-- ------------------------------------------------------------

DEFINE TABLE IF NOT EXISTS mentions TYPE RELATION
    FROM memory
    TO person | project | tool | organization | place | preference | concept
    SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS weight     ON mentions TYPE float DEFAULT 1.0;
DEFINE FIELD IF NOT EXISTS created_at ON mentions TYPE datetime DEFAULT time::now();

-- ------------------------------------------------------------
-- Relationship edges — bi-temporal, weighted, declared by caller
-- ------------------------------------------------------------

-- Exclusive predicates: only one active edge from a given subject. Setting
-- a new edge of an exclusive type closes any prior with the same subject.
-- Set: works_at, version_is, lives_at, role_at, married_to, employer_of,
-- owns, current_focus, status_is. (Enforced in Go writer; schema permits any.)

-- works_on: broadened from person→project to any→any. Usually a person works on a
-- project, but an organization/tool can too (a team works_on a product, a service
-- works_on a pipeline). OVERWRITE (not IF NOT EXISTS) so existing DBs pick up the
-- widening on boot — IF NOT EXISTS no-ops on a table that already exists.
DEFINE TABLE OVERWRITE works_on TYPE RELATION
    FROM person | project | tool | organization | place | preference | concept
    TO person | project | tool | organization | place | preference | concept
    SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS weight        ON works_on TYPE float DEFAULT 1.0;
DEFINE FIELD IF NOT EXISTS valid_from    ON works_on TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS valid_until   ON works_on TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS superseded_by ON works_on TYPE option<record>;
DEFINE FIELD IF NOT EXISTS source        ON works_on TYPE option<record<memory>>;
DEFINE FIELD IF NOT EXISTS created_at    ON works_on TYPE datetime DEFAULT time::now();
DEFINE INDEX IF NOT EXISTS idx_works_on_valid_from ON works_on FIELDS valid_from;
DEFINE INDEX IF NOT EXISTS idx_works_on_valid_until ON works_on FIELDS valid_until;

-- uses: any → any. "X uses Y" is not person-exclusive — a tool/project can use
-- another tool/project (an app uses rill, rill uses SurrealDB). Broadened to match
-- depends_on/part_of after a person→tool constraint rejected tool→project edges.
-- OVERWRITE (not IF NOT EXISTS) so existing DBs pick up the widening on boot.
DEFINE TABLE OVERWRITE uses TYPE RELATION
    FROM person | project | tool | organization | place | preference | concept
    TO person | project | tool | organization | place | preference | concept
    SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS weight        ON uses TYPE float DEFAULT 1.0;
DEFINE FIELD IF NOT EXISTS valid_from    ON uses TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS valid_until   ON uses TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS superseded_by ON uses TYPE option<record>;
DEFINE FIELD IF NOT EXISTS source        ON uses TYPE option<record<memory>>;
DEFINE FIELD IF NOT EXISTS created_at    ON uses TYPE datetime DEFAULT time::now();
DEFINE INDEX IF NOT EXISTS idx_uses_valid_from ON uses FIELDS valid_from;
DEFINE INDEX IF NOT EXISTS idx_uses_valid_until ON uses FIELDS valid_until;

DEFINE TABLE IF NOT EXISTS prefers TYPE RELATION FROM person TO preference SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS weight        ON prefers TYPE float DEFAULT 1.0;
DEFINE FIELD IF NOT EXISTS valence       ON prefers TYPE string DEFAULT 'positive'
    ASSERT $value IN ['positive', 'negative', 'neutral'];
DEFINE FIELD IF NOT EXISTS valid_from    ON prefers TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS valid_until   ON prefers TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS superseded_by ON prefers TYPE option<record>;
DEFINE FIELD IF NOT EXISTS source        ON prefers TYPE option<record<memory>>;
DEFINE FIELD IF NOT EXISTS created_at    ON prefers TYPE datetime DEFAULT time::now();
DEFINE INDEX IF NOT EXISTS idx_prefers_valid_from ON prefers FIELDS valid_from;
DEFINE INDEX IF NOT EXISTS idx_prefers_valid_until ON prefers FIELDS valid_until;

DEFINE TABLE IF NOT EXISTS works_at TYPE RELATION FROM person TO organization SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS weight        ON works_at TYPE float DEFAULT 1.0;
DEFINE FIELD IF NOT EXISTS role_title    ON works_at TYPE option<string>;
DEFINE FIELD IF NOT EXISTS valid_from    ON works_at TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS valid_until   ON works_at TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS superseded_by ON works_at TYPE option<record>;
DEFINE FIELD IF NOT EXISTS source        ON works_at TYPE option<record<memory>>;
DEFINE FIELD IF NOT EXISTS created_at    ON works_at TYPE datetime DEFAULT time::now();
DEFINE INDEX IF NOT EXISTS idx_works_at_valid_from ON works_at FIELDS valid_from;
DEFINE INDEX IF NOT EXISTS idx_works_at_valid_until ON works_at FIELDS valid_until;

-- depends_on: any entity → any entity. Captures runtime/architectural dependencies
-- regardless of type (e.g. app depends_on database, service depends_on host).
DEFINE TABLE IF NOT EXISTS depends_on TYPE RELATION
    FROM person | project | tool | organization | place | preference | concept
    TO person | project | tool | organization | place | preference | concept
    SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS weight        ON depends_on TYPE float DEFAULT 1.0;
DEFINE FIELD IF NOT EXISTS valid_from    ON depends_on TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS valid_until   ON depends_on TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS superseded_by ON depends_on TYPE option<record>;
DEFINE FIELD IF NOT EXISTS source        ON depends_on TYPE option<record<memory>>;
DEFINE FIELD IF NOT EXISTS created_at    ON depends_on TYPE datetime DEFAULT time::now();
DEFINE INDEX IF NOT EXISTS idx_depends_on_valid_from ON depends_on FIELDS valid_from;
DEFINE INDEX IF NOT EXISTS idx_depends_on_valid_until ON depends_on FIELDS valid_until;

-- part_of: any → any. Captures containment regardless of type (e.g. tool part_of
-- project, person part_of organization). Use this for "X belongs to Y" semantics.
DEFINE TABLE IF NOT EXISTS part_of TYPE RELATION
    FROM person | project | tool | organization | place | preference | concept
    TO person | project | tool | organization | place | preference | concept
    SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS weight        ON part_of TYPE float DEFAULT 1.0;
DEFINE FIELD IF NOT EXISTS valid_from    ON part_of TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS valid_until   ON part_of TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS superseded_by ON part_of TYPE option<record>;
DEFINE FIELD IF NOT EXISTS source        ON part_of TYPE option<record<memory>>;
DEFINE FIELD IF NOT EXISTS created_at    ON part_of TYPE datetime DEFAULT time::now();
DEFINE INDEX IF NOT EXISTS idx_part_of_valid_from ON part_of FIELDS valid_from;
DEFINE INDEX IF NOT EXISTS idx_part_of_valid_until ON part_of FIELDS valid_until;

-- Generic edge for predicates not covered above; subject/object may be any entity.
-- Useful for caller-declared edges like "version_is", "discussed_with", etc.
DEFINE TABLE IF NOT EXISTS assertion TYPE RELATION
    FROM person | project | tool | organization | place | preference | concept
    TO person | project | tool | organization | place | preference | concept
    SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS predicate     ON assertion TYPE string;
DEFINE FIELD IF NOT EXISTS weight        ON assertion TYPE float DEFAULT 1.0;
DEFINE FIELD IF NOT EXISTS valid_from    ON assertion TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS valid_until   ON assertion TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS superseded_by ON assertion TYPE option<record>;
DEFINE FIELD IF NOT EXISTS source        ON assertion TYPE option<record<memory>>;
DEFINE FIELD IF NOT EXISTS created_at    ON assertion TYPE datetime DEFAULT time::now();
DEFINE INDEX IF NOT EXISTS idx_assertion_predicate ON assertion FIELDS predicate;
DEFINE INDEX IF NOT EXISTS idx_assertion_valid_from ON assertion FIELDS valid_from;
DEFINE INDEX IF NOT EXISTS idx_assertion_valid_until ON assertion FIELDS valid_until;

-- ------------------------------------------------------------
-- orient_cache — rendered text blob, invalidated by writes
-- ------------------------------------------------------------

DEFINE TABLE IF NOT EXISTS orient_cache SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS scope             ON orient_cache TYPE string;          -- 'global' | 'project:<name>'
DEFINE FIELD IF NOT EXISTS rendered          ON orient_cache TYPE string DEFAULT '';
DEFINE FIELD IF NOT EXISTS covered_entity_ids ON orient_cache TYPE array<record> DEFAULT [];
DEFINE FIELD IF NOT EXISTS stale             ON orient_cache TYPE bool DEFAULT true;
DEFINE FIELD IF NOT EXISTS rendered_at       ON orient_cache TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS created_at        ON orient_cache TYPE datetime DEFAULT time::now();
DEFINE INDEX IF NOT EXISTS idx_orient_scope ON orient_cache FIELDS scope UNIQUE;

-- ------------------------------------------------------------
-- orient_caller — per-caller "last time you oriented" watermark, drives the
-- "## Since last orient" delta section. Record id mirrors orient_cache's
-- pattern (safeID(caller) as the key); `caller` is duplicated as a field so
-- it projects cleanly out of SELECT.
-- ------------------------------------------------------------

DEFINE TABLE IF NOT EXISTS orient_caller SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS caller           ON orient_caller TYPE string;          -- '<identity.Type>/<identity.Name>' or 'anonymous'
DEFINE FIELD IF NOT EXISTS last_oriented_at ON orient_caller TYPE datetime DEFAULT time::now();
DEFINE INDEX IF NOT EXISTS idx_orient_caller ON orient_caller FIELDS caller UNIQUE;

-- app_setting — runtime config overrides set via the settings UI. Record id is
-- the canonical setting key (e.g. app_setting:`orient.recency_days`); `k`
-- mirrors it for projection. Only editable, non-env-pinned keys are ever
-- written here; env vars take precedence at resolve time.
DEFINE TABLE IF NOT EXISTS app_setting SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS k          ON app_setting TYPE string;
DEFINE FIELD IF NOT EXISTS value      ON app_setting TYPE string DEFAULT '';
DEFINE FIELD IF NOT EXISTS updated_at ON app_setting TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS updated_by ON app_setting TYPE string DEFAULT '';

-- ------------------------------------------------------------
-- Rules are NOT a separate table — they're memories with kind='rule'.
-- orient renders them by querying `memory WHERE kind='rule' AND is_active=true`.


-- ------------------------------------------------------------
-- version_is — bi-temporal version of an entity. The version is a STRING
-- ATTRIBUTE (not a node), so versions never pollute the entity graph.
-- Exclusive: setting a new version closes the prior active row (valid_until
-- = now, superseded_by = new). Current = WHERE entity = X AND valid_until IS
-- NONE. As-of D = valid_from <= D AND (valid_until IS NONE OR valid_until > D).
-- ------------------------------------------------------------
DEFINE TABLE IF NOT EXISTS version_is TYPE NORMAL SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS entity        ON version_is TYPE record;
DEFINE FIELD IF NOT EXISTS version       ON version_is TYPE string;
DEFINE FIELD IF NOT EXISTS valid_from    ON version_is TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS valid_until   ON version_is TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS superseded_by ON version_is TYPE option<record>;
DEFINE FIELD IF NOT EXISTS source        ON version_is TYPE option<record<memory>>;
DEFINE FIELD IF NOT EXISTS created_at    ON version_is TYPE datetime DEFAULT time::now();
DEFINE INDEX IF NOT EXISTS idx_version_is_entity ON version_is FIELDS entity;

-- ------------------------------------------------------------
-- document — standalone markdown documents (primers, reviews,
-- writeups, references). DELIBERATELY OUTSIDE the memory/embedding/
-- orient pipeline: no vector, no chunking, never auto-surfaced in
-- orient. Content is an opaque blob with NO length cap. Fetched by id
-- or by associated entity (doc_about). FULLTEXT on title+content gives
-- a "find a doc" search without embeddings. This is the explicit
-- counter to "stuff monster docs into the vector store."
-- ------------------------------------------------------------
DEFINE TABLE IF NOT EXISTS document SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS title      ON document TYPE string
    ASSERT string::len($value) > 0;
DEFINE FIELD IF NOT EXISTS content    ON document TYPE string DEFAULT '';      -- markdown body, no length cap
DEFINE FIELD IF NOT EXISTS doc_type   ON document TYPE string DEFAULT 'writeup'; -- free-form: primer/review/writeup/reference/...
DEFINE FIELD IF NOT EXISTS project    ON document TYPE option<string>;          -- null = global
DEFINE FIELD IF NOT EXISTS source     ON document TYPE option<string>;          -- optional origin (URL, 'import', ...)
DEFINE FIELD IF NOT EXISTS author     ON document TYPE option<string>;          -- last writer (audit)
DEFINE FIELD IF NOT EXISTS is_active  ON document TYPE bool DEFAULT true;
DEFINE FIELD IF NOT EXISTS created_at ON document TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS updated_at ON document TYPE datetime DEFAULT time::now();

DEFINE INDEX IF NOT EXISTS idx_document_project ON document FIELDS project;
DEFINE INDEX IF NOT EXISTS idx_document_type    ON document FIELDS doc_type;
DEFINE INDEX IF NOT EXISTS idx_document_active  ON document FIELDS is_active;
DEFINE INDEX IF NOT EXISTS idx_document_created ON document FIELDS created_at;
-- FTS reuses the `ascii` analyzer defined on the memory table above.
DEFINE INDEX IF NOT EXISTS idx_document_fts_title   ON document FIELDS title   FULLTEXT ANALYZER ascii BM25;
DEFINE INDEX IF NOT EXISTS idx_document_fts_content ON document FIELDS content FULLTEXT ANALYZER ascii BM25;

-- doc_about — document -> entity association. Additive: a doc can reference
-- many entities, an entity can have many docs. NOT bi-temporal — docs are
-- artifacts, not facts. Soft-deleting a document leaves these edges intact.
DEFINE TABLE IF NOT EXISTS doc_about TYPE RELATION
    FROM document
    TO person | project | tool | organization | place | preference | concept
    SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS created_at ON doc_about TYPE datetime DEFAULT time::now();

-- ============================================================
-- Auth + OAuth — consolidated into the single rill database.
-- (Previously lived in a separate auth DB via Go inline DDL; folded here so
--  there is ONE schema, applied idempotently on boot. No _migrations gating.)
-- ============================================================

DEFINE TABLE IF NOT EXISTS auth_token SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS name       ON auth_token TYPE string;
DEFINE FIELD IF NOT EXISTS token_hash ON auth_token TYPE string;
DEFINE FIELD IF NOT EXISTS revoked    ON auth_token TYPE bool DEFAULT false;
DEFINE FIELD IF NOT EXISTS created_at ON auth_token TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS last_used  ON auth_token TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS expires_at ON auth_token TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS scopes     ON auth_token TYPE option<array<string>>;
DEFINE INDEX IF NOT EXISTS auth_token_hash ON auth_token FIELDS token_hash UNIQUE;

DEFINE TABLE IF NOT EXISTS auth_user SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS username      ON auth_user TYPE string;
DEFINE FIELD IF NOT EXISTS password_hash ON auth_user TYPE string;
DEFINE FIELD IF NOT EXISTS created_at    ON auth_user TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS last_login    ON auth_user TYPE option<datetime>;
DEFINE FIELD IF NOT EXISTS oidc_subject  ON auth_user TYPE option<string>;
DEFINE INDEX IF NOT EXISTS auth_user_username ON auth_user FIELDS username UNIQUE;
DEFINE INDEX IF NOT EXISTS auth_user_oidc_subject ON auth_user FIELDS oidc_subject UNIQUE;

DEFINE TABLE IF NOT EXISTS auth_session SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS user        ON auth_session TYPE record<auth_user>;
DEFINE FIELD IF NOT EXISTS token_hash  ON auth_session TYPE string;
DEFINE FIELD IF NOT EXISTS created_at  ON auth_session TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS expires_at  ON auth_session TYPE datetime;
DEFINE FIELD IF NOT EXISTS last_used   ON auth_session TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS user_agent  ON auth_session TYPE option<string>;
DEFINE FIELD IF NOT EXISTS source_ip   ON auth_session TYPE option<string>;
DEFINE INDEX IF NOT EXISTS auth_session_token ON auth_session FIELDS token_hash UNIQUE;
DEFINE INDEX IF NOT EXISTS auth_session_user ON auth_session FIELDS user;

DEFINE TABLE IF NOT EXISTS auth_audit SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS at            ON auth_audit TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS identity_type ON auth_audit TYPE string;
DEFINE FIELD IF NOT EXISTS identity_name ON auth_audit TYPE string;
DEFINE FIELD IF NOT EXISTS source_ip     ON auth_audit TYPE string;
DEFINE FIELD IF NOT EXISTS method        ON auth_audit TYPE string;
DEFINE FIELD IF NOT EXISTS path          ON auth_audit TYPE string;
DEFINE FIELD IF NOT EXISTS tool          ON auth_audit TYPE option<string>;
DEFINE FIELD IF NOT EXISTS status        ON auth_audit TYPE int;
DEFINE FIELD IF NOT EXISTS outcome       ON auth_audit TYPE string;
DEFINE FIELD IF NOT EXISTS token_id      ON auth_audit TYPE option<string>;
DEFINE FIELD IF NOT EXISTS agent_harness ON auth_audit TYPE option<string>;
DEFINE FIELD IF NOT EXISTS llm_model     ON auth_audit TYPE option<string>;
DEFINE INDEX IF NOT EXISTS auth_audit_at ON auth_audit FIELDS at;

DEFINE TABLE IF NOT EXISTS oauth2_clients SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS client_id          ON oauth2_clients TYPE string;
DEFINE FIELD IF NOT EXISTS client_secret_hash ON oauth2_clients TYPE option<string>;
DEFINE FIELD IF NOT EXISTS redirect_uris      ON oauth2_clients TYPE array<string>;
DEFINE FIELD IF NOT EXISTS client_name        ON oauth2_clients TYPE option<string>;
DEFINE FIELD IF NOT EXISTS created_at         ON oauth2_clients TYPE datetime DEFAULT time::now();
DEFINE INDEX IF NOT EXISTS oauth2_clients_id ON oauth2_clients FIELDS client_id UNIQUE;

DEFINE TABLE IF NOT EXISTS oauth2_pending_auths SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS rill_state                   ON oauth2_pending_auths TYPE string;
DEFINE FIELD IF NOT EXISTS client_state                 ON oauth2_pending_auths TYPE string;
DEFINE FIELD IF NOT EXISTS client_id                    ON oauth2_pending_auths TYPE string;
DEFINE FIELD IF NOT EXISTS client_redirect_uri          ON oauth2_pending_auths TYPE string;
DEFINE FIELD IF NOT EXISTS client_code_challenge        ON oauth2_pending_auths TYPE string;
DEFINE FIELD IF NOT EXISTS client_code_challenge_method ON oauth2_pending_auths TYPE string;
DEFINE FIELD IF NOT EXISTS oidc_code_verifier           ON oauth2_pending_auths TYPE string;
DEFINE FIELD IF NOT EXISTS oidc_nonce                   ON oauth2_pending_auths TYPE option<string>;
DEFINE FIELD IF NOT EXISTS scopes                       ON oauth2_pending_auths TYPE array<string>;
DEFINE FIELD IF NOT EXISTS expires_at                   ON oauth2_pending_auths TYPE datetime;
DEFINE INDEX IF NOT EXISTS oauth2_pending_state ON oauth2_pending_auths FIELDS rill_state UNIQUE;

DEFINE TABLE IF NOT EXISTS oauth2_auth_codes SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS code                  ON oauth2_auth_codes TYPE string;
DEFINE FIELD IF NOT EXISTS client_id             ON oauth2_auth_codes TYPE string;
DEFINE FIELD IF NOT EXISTS user_id               ON oauth2_auth_codes TYPE string;
DEFINE FIELD IF NOT EXISTS redirect_uri          ON oauth2_auth_codes TYPE string;
DEFINE FIELD IF NOT EXISTS code_challenge        ON oauth2_auth_codes TYPE string;
DEFINE FIELD IF NOT EXISTS code_challenge_method ON oauth2_auth_codes TYPE string;
DEFINE FIELD IF NOT EXISTS scopes                ON oauth2_auth_codes TYPE array<string>;
DEFINE FIELD IF NOT EXISTS expires_at            ON oauth2_auth_codes TYPE datetime;
DEFINE INDEX IF NOT EXISTS oauth2_auth_codes_code ON oauth2_auth_codes FIELDS code UNIQUE;

DEFINE TABLE IF NOT EXISTS oauth2_access_tokens SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS token_hash ON oauth2_access_tokens TYPE string;
DEFINE FIELD IF NOT EXISTS client_id  ON oauth2_access_tokens TYPE string;
DEFINE FIELD IF NOT EXISTS user_id    ON oauth2_access_tokens TYPE string;
DEFINE FIELD IF NOT EXISTS scopes     ON oauth2_access_tokens TYPE array<string>;
DEFINE FIELD IF NOT EXISTS expires_at ON oauth2_access_tokens TYPE datetime;
DEFINE FIELD IF NOT EXISTS created_at ON oauth2_access_tokens TYPE datetime DEFAULT time::now();
DEFINE FIELD IF NOT EXISTS revoked    ON oauth2_access_tokens TYPE bool DEFAULT false;
DEFINE INDEX IF NOT EXISTS oauth2_access_tokens_hash ON oauth2_access_tokens FIELDS token_hash UNIQUE;

DEFINE TABLE IF NOT EXISTS oauth2_refresh_tokens SCHEMAFULL;
DEFINE FIELD IF NOT EXISTS token_hash ON oauth2_refresh_tokens TYPE string;
DEFINE FIELD IF NOT EXISTS client_id  ON oauth2_refresh_tokens TYPE string;
DEFINE FIELD IF NOT EXISTS user_id    ON oauth2_refresh_tokens TYPE string;
DEFINE FIELD IF NOT EXISTS scopes     ON oauth2_refresh_tokens TYPE array<string>;
DEFINE FIELD IF NOT EXISTS expires_at ON oauth2_refresh_tokens TYPE datetime;
DEFINE FIELD IF NOT EXISTS revoked    ON oauth2_refresh_tokens TYPE bool DEFAULT false;
DEFINE INDEX IF NOT EXISTS oauth2_refresh_tokens_hash ON oauth2_refresh_tokens FIELDS token_hash UNIQUE;
