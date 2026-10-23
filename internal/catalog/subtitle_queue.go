package catalog

import (
	"context"
	"errors"
	"path/filepath"
)

type SubtitleQueueItem struct {
	PartID            string `json:"partId"`
	FileName          string `json:"fileName"`
	Action            string `json:"action"`
	Coverage          string `json:"coverage"`
	Disposition       string `json:"disposition"`
	CandidateCount    int    `json:"candidateCount"`
	ObservedSize      int64  `json:"observedSize"`
	ObservedModTimeNS int64  `json:"observedModTimeNs,string"`
	UpdatedAt         string `json:"updatedAt"`
}

func ValidQueueAction(s string) bool {
	switch s {
	case "excluded", "manual_review", "covered", "inspect_existing", "review_local_candidate", "search_published":
		return true
	default:
		return false
	}
}

// UpsertSubtitleQueueBatch is all-or-none to avoid partial refreshes after
// filesystem errors. Refresh is an observation, never subtitle publication.
func (r *Repository) UpsertSubtitleQueueBatch(ctx context.Context, libraryID string, batch []SubtitleQueueItem) error {
	if len(batch) > 50 {
		return errors.New("queue refresh batch too large")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := map[string]bool{}
	for _, row := range batch {
		if row.PartID == "" || seen[row.PartID] || !ValidQueueAction(row.Action) ||
			(row.Disposition != "active" && row.Disposition != "skip" && row.Disposition != "review") ||
			row.CandidateCount < 0 || row.ObservedSize < 0 || row.ObservedModTimeNS < 0 {
			return errors.New("invalid subtitle queue entry")
		}
		seen[row.PartID] = true
		var valid int
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_parts p JOIN media_sources s ON s.id=p.source_id
 JOIN items i ON i.id=s.item_id WHERE p.id=? AND i.library_id=? AND p.available=1 AND s.available=1`, row.PartID, libraryID).Scan(&valid)
		if err != nil {
			return err
		}
		if valid != 1 {
			return errors.New("part is not available in requested library")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO subtitle_queue(part_id,action,coverage,disposition,candidate_count,observed_size,observed_mod_time_ns)
 VALUES(?,?,?,?,?,?,?)
 ON CONFLICT(part_id) DO UPDATE SET action=excluded.action,coverage=excluded.coverage,
 disposition=excluded.disposition,candidate_count=excluded.candidate_count,
 observed_size=excluded.observed_size,observed_mod_time_ns=excluded.observed_mod_time_ns,updated_at=CURRENT_TIMESTAMP`,
			row.PartID, row.Action, row.Coverage, row.Disposition, row.CandidateCount, row.ObservedSize, row.ObservedModTimeNS)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) SubtitleQueue(ctx context.Context, libraryID string, limit, offset int) ([]SubtitleQueueItem, int, error) {
	if limit < 1 || limit > 50 || offset < 0 {
		return nil, 0, errors.New("invalid pagination")
	}
	var total int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_queue q
 JOIN media_parts p ON p.id=q.part_id JOIN media_sources s ON s.id=p.source_id
 JOIN items i ON i.id=s.item_id
 WHERE i.library_id=? AND p.available=1 AND s.available=1`, libraryID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT q.part_id,p.path,q.action,q.coverage,q.disposition,
 q.candidate_count,q.observed_size,q.observed_mod_time_ns,q.updated_at
 FROM subtitle_queue q JOIN media_parts p ON p.id=q.part_id
 JOIN media_sources s ON s.id=p.source_id JOIN items i ON i.id=s.item_id
 WHERE i.library_id=? AND p.available=1 AND s.available=1
 ORDER BY p.path, p.id LIMIT ? OFFSET ?`, libraryID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []SubtitleQueueItem{}
	for rows.Next() {
		var row SubtitleQueueItem
		if err = rows.Scan(&row.PartID, &row.FileName, &row.Action, &row.Coverage, &row.Disposition,
			&row.CandidateCount, &row.ObservedSize, &row.ObservedModTimeNS, &row.UpdatedAt); err != nil {
			return nil, 0, err
		}
		row.FileName = filepath.Base(row.FileName)
		items = append(items, row)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Queue refreshes recompute policy together with sidecar evidence.
