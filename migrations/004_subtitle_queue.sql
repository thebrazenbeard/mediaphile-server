CREATE TABLE IF NOT EXISTS subtitle_queue (
 part_id TEXT PRIMARY KEY REFERENCES media_parts(id) ON DELETE CASCADE,
 action TEXT NOT NULL CHECK(action IN ('excluded','manual_review','covered','inspect_existing','review_local_candidate','search_published')),
 coverage TEXT NOT NULL,
 disposition TEXT NOT NULL CHECK(disposition IN ('active','skip','review')),
 candidate_count INTEGER NOT NULL CHECK(candidate_count >= 0),
 observed_size INTEGER NOT NULL,
 observed_mod_time_ns INTEGER NOT NULL,
 updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_subtitle_queue_action ON subtitle_queue(action, updated_at);
