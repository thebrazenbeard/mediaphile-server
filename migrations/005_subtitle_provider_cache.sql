-- Durable, metadata-only OpenSubtitles lookup history. No API keys, NAS paths or SRT bytes.
CREATE TABLE IF NOT EXISTS subtitle_provider_cache (
 part_id TEXT NOT NULL REFERENCES media_parts(id) ON DELETE CASCADE,
 query_hash TEXT NOT NULL,
 candidates_json TEXT NOT NULL,
 expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms >= 0),
 PRIMARY KEY (part_id, query_hash)
);
CREATE TABLE IF NOT EXISTS subtitle_provider_cooldown (
 provider TEXT PRIMARY KEY,
 blocked_until_ms INTEGER NOT NULL CHECK(blocked_until_ms >= 0)
);
