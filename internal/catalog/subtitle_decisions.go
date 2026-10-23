package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// SubtitleDecision is policy, not evidence that subtitles actually exist.
// Absent rows always default to active and do not change media coverage.
type SubtitleDecision struct {
	PartID      string `json:"partId"`
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

func ValidSubtitleDisposition(disposition, reason string) bool {
	if reason != strings.TrimSpace(reason) || len(reason) > 256 {
		return false
	}
	switch disposition {
	case "active":
		return reason == ""
	case "skip", "review":
		return reason != ""
	default:
		return false
	}
}

func (r *Repository) SubtitleDecision(ctx context.Context, partID string) (SubtitleDecision, error) {
	v := SubtitleDecision{PartID: partID, Disposition: "active"}
	err := r.db.QueryRowContext(ctx, "SELECT disposition,reason,updated_at FROM subtitle_decisions WHERE part_id=?", partID).Scan(&v.Disposition, &v.Reason, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	return v, err
}

func (r *Repository) SaveSubtitleDecision(ctx context.Context, partID, disposition, reason string) (SubtitleDecision, error) {
	if !ValidSubtitleDisposition(disposition, reason) {
		return SubtitleDecision{}, errors.New("invalid subtitle decision")
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO subtitle_decisions(part_id,disposition,reason)
VALUES(?,?,?)
ON CONFLICT(part_id) DO UPDATE SET disposition=excluded.disposition,reason=excluded.reason,updated_at=CURRENT_TIMESTAMP
`, partID, disposition, reason)
	if err != nil {
		return SubtitleDecision{}, err
	}
	return r.SubtitleDecision(ctx, partID)
}
