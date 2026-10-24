package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// SubtitleExclusionImport is an exact-path admin migration of prior user decisions.
type SubtitleExclusionImport struct {
	Video  string `json:"video"`
	Reason string `json:"reason"`
}

var ErrSubtitleMediaNotFound = errors.New("unrecognized media path")

// ImportSubtitleExclusions atomically imports only known, available catalog media.
// It never opens, mutates, or deletes any media or subtitle files. Existing
// decisions take precedence so rerunning a historical manifest cannot undo a
// more recent user choice.
func (r *Repository) ImportSubtitleExclusions(ctx context.Context, entries []SubtitleExclusionImport) (int, error) {
	if len(entries) < 1 || len(entries) > 250 {
		return 0, errors.New("invalid exclusion count")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	seen := make(map[string]bool, len(entries))
	inserted := 0
	for _, entry := range entries {
		if strings.TrimSpace(entry.Video) != entry.Video || entry.Video == "" || seen[entry.Video] ||
			!ValidSubtitleDisposition("skip", entry.Reason) {
			return 0, errors.New("invalid or duplicate exclusion")
		}
		seen[entry.Video] = true
		var id string
		err = tx.QueryRowContext(ctx, "SELECT id FROM media_parts WHERE path=? AND available=1", entry.Video).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("%w", ErrSubtitleMediaNotFound)
		}
		if err != nil {
			return 0, err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO subtitle_decisions(part_id,disposition,reason) VALUES(?, 'skip', ?) ON CONFLICT(part_id) DO NOTHING`, id, entry.Reason)
		if err != nil {
			return 0, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		inserted += int(changed)
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}
