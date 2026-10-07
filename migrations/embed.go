package migrations

import _ "embed"

// Initial is the first durable Mediaphile Server schema migration.
//
//go:embed 001_initial.sql
var Initial string
