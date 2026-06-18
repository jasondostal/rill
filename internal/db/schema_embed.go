package db

import _ "embed"

// schemaSQL is the single canonical rill schema (memory + entities + edges +
// version_is + auth/oauth), applied idempotently on boot by SetupSchema. It is
// the ONE source of truth — there is no _migrations version-gating and no
// inline DDL. Every statement is IF NOT EXISTS so re-applying on each boot is a
// no-op on an existing database.
//
//go:embed schema.surql
var schemaSQL string
