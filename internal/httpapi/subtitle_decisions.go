package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/subtitles"
)

// The recovery plan is advisory. It never marks user exclusions as installed
// subtitles, nor invokes online services or writes to the media share.
func subtitleNextAction(review subtitles.Review, decision catalog.SubtitleDecision) string {
	switch decision.Disposition {
	case "skip":
		return "excluded"
	case "review":
		return "manual_review"
	}
	if review.Coverage == "embedded_english" {
		return "covered"
	}
	if review.Coverage == "external_english" {
		for _, sidecar := range review.ExistingAudits {
			if sidecar.Language == "en" && sidecar.Status != "ready" {
				return "inspect_existing"
			}
		}
		return "covered"
	}
	if len(review.Existing) > 0 {
		return "inspect_existing"
	}
	for _, c := range review.Candidates {
		if c.Audit.Status == "ready" {
			return "review_local_candidate"
		}
	}
	return "search_published"
}

func subtitleDecision(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, 503, "CATALOG_UNAVAILABLE", "catalog unavailable")
			return
		}
		partID := r.PathValue("partId")
		part, source, err := deps.Catalog.GetMediaPartWithSource(r.Context(), partID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (!part.Available || !source.Available)) {
			writeError(w, 404, "MEDIA_UNAVAILABLE", "media part not found")
			return
		}
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "media lookup failed")
			return
		}
		if r.Method == http.MethodGet {
			d, err := deps.Catalog.SubtitleDecision(r.Context(), partID)
			if err != nil {
				writeError(w, 500, "CATALOG_ERROR", "decision lookup failed")
				return
			}
			writeJSON(w, 200, d)
			return
		}
		var body struct {
			Disposition string `json:"disposition"`
			Reason      string `json:"reason"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeError(w, 400, "INVALID_JSON", "invalid decision request")
			return
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			writeError(w, 400, "INVALID_JSON", "multiple JSON values are not allowed")
			return
		}
		if !catalog.ValidSubtitleDisposition(body.Disposition, body.Reason) {
			writeError(w, 400, "INVALID_DECISION", "disposition must be active, skip or review; skip/review require a reason up to 256 bytes")
			return
		}
		result, err := deps.Catalog.SaveSubtitleDecision(r.Context(), partID, body.Disposition, body.Reason)
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "cannot persist decision")
			return
		}
		writeJSON(w, 200, result)
	}
}
