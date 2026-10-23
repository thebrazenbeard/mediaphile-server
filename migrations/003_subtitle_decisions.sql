CREATE TABLE IF NOT EXISTS subtitle_decisions (
  part_id TEXT PRIMARY KEY REFERENCES media_parts(id) ON DELETE CASCADE,
  disposition TEXT NOT NULL CHECK(disposition IN ('active','skip','review')),
  reason TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK ((disposition = 'active' AND reason = '') OR (disposition <> 'active' AND length(trim(reason)) > 0))
);
