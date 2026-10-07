ALTER TABLE webhook_subscriptions ADD COLUMN secret_value TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS knowledge_records (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source_repository TEXT NOT NULL,
  source_revision TEXT NOT NULL,
  source_digest TEXT NOT NULL,
  source_record_id TEXT NOT NULL,
  evidence_class TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  unresolved INTEGER NOT NULL DEFAULT 0 CHECK (unresolved IN (0,1)),
  conflict INTEGER NOT NULL DEFAULT 0 CHECK (conflict IN (0,1)),
  imported_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(source_repository, source_revision, source_record_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_knowledge_item ON knowledge_records(item_id);
