PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS libraries (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  media_type TEXT NOT NULL CHECK (media_type IN ('movies','tv')),
  root_path TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  last_scan_started_at TEXT,
  last_scan_completed_at TEXT
);

CREATE TABLE IF NOT EXISTS items (
  id TEXT PRIMARY KEY,
  library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  parent_id TEXT REFERENCES items(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('movie','show','season','episode')),
  title TEXT NOT NULL,
  year INTEGER,
  season_number INTEGER,
  episode_number INTEGER,
  unresolved INTEGER NOT NULL DEFAULT 0 CHECK (unresolved IN (0,1)),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_items_library_kind ON items(library_id, kind);
CREATE INDEX IF NOT EXISTS idx_items_parent ON items(parent_id);

CREATE TABLE IF NOT EXISTS editions (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  UNIQUE(item_id, name)
);

CREATE TABLE IF NOT EXISTS media_sources (
  id TEXT PRIMARY KEY,
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  edition_id TEXT REFERENCES editions(id) ON DELETE SET NULL,
  container TEXT NOT NULL,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  bitrate INTEGER NOT NULL DEFAULT 0,
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  video_codec TEXT NOT NULL DEFAULT '',
  audio_codec TEXT NOT NULL DEFAULT '',
  hdr INTEGER NOT NULL DEFAULT 0 CHECK (hdr IN (0,1)),
  available INTEGER NOT NULL DEFAULT 1 CHECK (available IN (0,1))
);
CREATE INDEX IF NOT EXISTS idx_media_sources_item ON media_sources(item_id);

CREATE TABLE IF NOT EXISTS media_parts (
  id TEXT PRIMARY KEY,
  source_id TEXT NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,
  path TEXT NOT NULL UNIQUE,
  size INTEGER NOT NULL DEFAULT 0,
  mod_time_ns INTEGER NOT NULL DEFAULT 0,
  available INTEGER NOT NULL DEFAULT 1 CHECK (available IN (0,1))
);
CREATE INDEX IF NOT EXISTS idx_media_parts_source ON media_parts(source_id);

CREATE TABLE IF NOT EXISTS media_streams (
  id TEXT PRIMARY KEY,
  part_id TEXT NOT NULL REFERENCES media_parts(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('video','audio','subtitle')),
  stream_index INTEGER NOT NULL,
  codec TEXT NOT NULL,
  language TEXT NOT NULL DEFAULT '',
  channels INTEGER NOT NULL DEFAULT 0,
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  frame_rate TEXT NOT NULL DEFAULT '',
  is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0,1)),
  is_forced INTEGER NOT NULL DEFAULT 0 CHECK (is_forced IN (0,1)),
  UNIQUE(part_id, stream_index)
);
CREATE INDEX IF NOT EXISTS idx_media_streams_part ON media_streams(part_id);

CREATE TABLE IF NOT EXISTS principals (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  is_admin INTEGER NOT NULL DEFAULT 0 CHECK (is_admin IN (0,1)),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS auth_sessions (
  id TEXT PRIMARY KEY,
  principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at TEXT,
  revoked_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_principal ON auth_sessions(principal_id);

CREATE TABLE IF NOT EXISTS playback_state (
  principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  resume_ms INTEGER NOT NULL DEFAULT 0,
  play_count INTEGER NOT NULL DEFAULT 0,
  completed INTEGER NOT NULL DEFAULT 0 CHECK (completed IN (0,1)),
  last_played_at TEXT,
  selected_audio_stream_id TEXT REFERENCES media_streams(id) ON DELETE SET NULL,
  selected_subtitle_stream_id TEXT REFERENCES media_streams(id) ON DELETE SET NULL,
  PRIMARY KEY(principal_id, item_id)
);

CREATE TABLE IF NOT EXISTS playback_sessions (
  id TEXT PRIMARY KEY,
  principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  client_id TEXT NOT NULL,
  media_source_id TEXT REFERENCES media_sources(id) ON DELETE SET NULL,
  decision TEXT NOT NULL,
  state TEXT NOT NULL,
  position_ms INTEGER NOT NULL DEFAULT 0,
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ended_at TEXT,
  stop_reason TEXT
);

CREATE TABLE IF NOT EXISTS webhook_subscriptions (
  id TEXT PRIMARY KEY,
  target_url TEXT NOT NULL,
  event_types TEXT NOT NULL,
  secret_hash TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
