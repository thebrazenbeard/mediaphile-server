package catalog

import (
	"context"
	"database/sql"
)

type ContinueWatchingEntry struct {
	Item         Item
	ResumeMS     int64
	LastPlayedAt *string
}

func (r *Repository) ListContinueWatching(ctx context.Context, principalID string, limit int) ([]ContinueWatchingEntry, error) {
	if principalID == "" {
		return nil, sql.ErrNoRows
	}
	if limit < 1 || limit > 24 {
		limit = 12
	}
	rows, err := r.db.QueryContext(ctx, `
 SELECT i.id,i.library_id,i.parent_id,i.kind,i.title,i.year,i.season_number,i.episode_number,i.unresolved,
 ps.resume_ms,ps.last_played_at
 FROM playback_state ps
 JOIN items i ON i.id=ps.item_id
 JOIN libraries l ON l.id=i.library_id
 WHERE ps.principal_id=? AND ps.resume_ms>0 AND ps.completed=0
 AND i.kind IN ('movie','episode') AND l.enabled=1
 AND EXISTS (
   SELECT 1 FROM media_sources ms
   JOIN media_parts mp ON mp.source_id=ms.id
   WHERE ms.item_id=i.id AND ms.available=1 AND mp.available=1
 )
 ORDER BY COALESCE(ps.last_played_at,'') DESC,i.id ASC
 LIMIT ?`, principalID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ContinueWatchingEntry, 0)
	for rows.Next() {
		var e ContinueWatchingEntry
		var parent, played sql.NullString
		var year, season, episode sql.NullInt64
		var unresolved int
		if err := rows.Scan(&e.Item.ID, &e.Item.LibraryID, &parent, &e.Item.Kind, &e.Item.Title, &year, &season, &episode, &unresolved, &e.ResumeMS, &played); err != nil {
			return nil, err
		}
		e.Item.ParentID = nullStringPtr(parent)
		e.Item.Year = nullIntPtr(year)
		e.Item.SeasonNumber = nullIntPtr(season)
		e.Item.EpisodeNumber = nullIntPtr(episode)
		e.Item.Unresolved = unresolved != 0
		e.LastPlayedAt = nullStringPtr(played)
		out = append(out, e)
	}
	return out, rows.Err()
}
