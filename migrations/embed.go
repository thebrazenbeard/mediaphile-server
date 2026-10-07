package migrations

import _ "embed"

// Initial is the first durable Mediaphile Server schema migration.
//
//go:embed 001_initial.sql
var Initial string

// KnowledgeWebhooks adds provenance records and durable webhook delivery secrets.
//
//go:embed 002_knowledge_webhooks.sql
var KnowledgeWebhooks string
