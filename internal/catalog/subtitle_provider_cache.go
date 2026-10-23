package catalog

import (
	"context"
	"database/sql"
	"errors"
)

// ProviderCachedCandidates reads public subtitle metadata, never audio, subtitles,
// credentials, or media paths. A hit may contain an empty candidate list.
func (r *Repository) ProviderCachedCandidates(ctx context.Context, partID, queryHash string, nowMS int64) ([]byte, bool, error) {
	var jsonText string
	var expires int64
	err := r.db.QueryRowContext(ctx, `SELECT candidates_json,expires_at_ms
 FROM subtitle_provider_cache WHERE part_id=? AND query_hash=?`, partID, queryHash).Scan(&jsonText, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if expires <= nowMS {
		return nil, false, nil
	}
	return []byte(jsonText), true, nil
}

func (r *Repository) SaveProviderCandidates(ctx context.Context, partID, queryHash string, payload []byte, expiresMS int64) error {
	if partID == "" || len(queryHash) != 64 || len(payload) < 2 || len(payload) > 65536 || expiresMS < 1 {
		return errors.New("invalid provider cache entry")
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO subtitle_provider_cache(part_id,query_hash,candidates_json,expires_at_ms)
 VALUES(?,?,?,?) ON CONFLICT(part_id,query_hash) DO UPDATE SET candidates_json=excluded.candidates_json,
 expires_at_ms=excluded.expires_at_ms`, partID, queryHash, string(payload), expiresMS)
	return err
}

func (r *Repository) SubtitleProviderBlocked(ctx context.Context, provider string, nowMS int64) (bool, error) {
	var until int64
	err := r.db.QueryRowContext(ctx, `SELECT blocked_until_ms FROM subtitle_provider_cooldown WHERE provider=?`, provider).Scan(&until)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return until > nowMS, nil
}

func (r *Repository) PauseSubtitleProvider(ctx context.Context, provider string, untilMS int64) error {
	if provider != "opensubtitles" || untilMS <= 0 {
		return errors.New("invalid provider cooldown")
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO subtitle_provider_cooldown(provider,blocked_until_ms) VALUES(?,?)
 ON CONFLICT(provider) DO UPDATE SET blocked_until_ms=MAX(blocked_until_ms,excluded.blocked_until_ms)`, provider, untilMS)
	return err
}
