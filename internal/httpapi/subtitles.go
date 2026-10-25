package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/subtitles"
)

type subtitlePartReview struct {
	PartID     string                    `json:"partId"`
	FileName   string                    `json:"fileName"`
	Review     subtitles.Review          `json:"review"`
	Embedded   []subtitles.EmbeddedAudit `json:"embedded"`
	Decision   catalog.SubtitleDecision  `json:"decision"`
	NextAction string                    `json:"nextAction"`
}

func collectSubtitlePart(deps Dependencies, r *http.Request, partID string, inspect bool) (subtitlePartReview, error) {
	part, source, err := deps.Catalog.GetMediaPartWithSource(r.Context(), partID)
	if err != nil {
		return subtitlePartReview{}, err
	}
	if !part.Available || !source.Available {
		return subtitlePartReview{}, sql.ErrNoRows
	}
	_, _, streams, err := deps.Catalog.MediaInventory(r.Context(), source.ItemID)
	if err != nil {
		return subtitlePartReview{}, err
	}
	embedded := []subtitles.EmbeddedTrack{}
	for _, s := range streams {
		if s.PartID != partID || s.Kind != catalog.StreamSubtitle {
			continue
		}
		embedded = append(embedded, subtitles.EmbeddedTrack{Index: s.StreamIndex, Codec: s.Codec, Language: s.Language})
	}
	hasEnglish := false
	for _, s := range embedded {
		if subtitles.EnglishTrack(s.Language) {
			hasEnglish = true
			break
		}
	}
	review, err := subtitles.Discover(part.Path, source.DurationMS, hasEnglish)
	if err != nil {
		return subtitlePartReview{}, err
	}
	decision, err := deps.Catalog.SubtitleDecision(r.Context(), partID)
	if err != nil {
		return subtitlePartReview{}, err
	}
	item := subtitlePartReview{PartID: partID, FileName: filepath.Base(part.Path), Review: review, Embedded: []subtitles.EmbeddedAudit{}, Decision: decision}
	item.NextAction = subtitleNextAction(review, decision)
	if inspect {
		ffmpeg := ""
		if deps.FFmpegAvailable {
			ffmpeg = deps.FFmpegBinary
		}
		item.Embedded = subtitles.InspectEmbedded(r.Context(), ffmpeg, part.Path, embedded, source.DurationMS)
	}
	return item, nil
}
func reviewSubtitlePart(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, 503, "CATALOG_UNAVAILABLE", "catalog unavailable")
			return
		}
		partID := r.PathValue("partId")
		item, err := collectSubtitlePart(deps, r, partID, true)
		if err == sql.ErrNoRows {
			writeError(w, 404, "MEDIA_UNAVAILABLE", "media part not found")
			return
		}
		if err != nil {
			writeError(w, 500, "SUBTITLE_REVIEW_FAILED", err.Error())
			return
		}
		writeJSON(w, 200, item)
	}
}
func librarySubtitleOverview(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, 503, "CATALOG_UNAVAILABLE", "catalog unavailable")
			return
		}
		libraryID := r.PathValue("libraryId")
		if _, err := deps.Catalog.GetLibrary(r.Context(), libraryID); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, 404, "LIBRARY_NOT_FOUND", "library not found")
			} else {
				writeError(w, 500, "CATALOG_ERROR", "library lookup failed")
			}
			return
		}
		limit, offset := 25, 0
		if q := r.URL.Query().Get("limit"); q != "" {
			v, err := strconv.Atoi(q)
			if err != nil || v < 1 || v > 50 {
				writeError(w, 400, "INVALID_LIMIT", "limit must be 1-50")
				return
			}
			limit = v
		}
		if q := r.URL.Query().Get("offset"); q != "" {
			v, err := strconv.Atoi(q)
			if err != nil || v < 0 {
				writeError(w, 400, "INVALID_OFFSET", "offset must be nonnegative")
				return
			}
			offset = v
		}
		parts, err := deps.Catalog.ListAvailablePartsByLibrary(r.Context(), libraryID)
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "cannot list media parts")
			return
		}
		entries := []subtitlePartReview{}
		if offset < len(parts) {
			end := offset + limit
			if end > len(parts) {
				end = len(parts)
			}
			for _, part := range parts[offset:end] {
				if !part.Available {
					continue
				}
				review, err := collectSubtitlePart(deps, r, part.ID, false)
				if err != nil {
					writeError(w, 500, "SUBTITLE_REVIEW_FAILED", err.Error())
					return
				}
				entries = append(entries, review)
			}
		}
		writeJSON(w, 200, map[string]any{"items": entries, "total": len(parts), "offset": offset, "limit": limit})
	}
}
func installSubtitleCandidate(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, 503, "CATALOG_UNAVAILABLE", "catalog unavailable")
			return
		}
		var request struct {
			CandidateID string `json:"candidateId"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&request); err != nil {
			writeError(w, 400, "INVALID_JSON", "invalid candidate request")
			return
		}
		part, source, err := deps.Catalog.GetMediaPartWithSource(r.Context(), r.PathValue("partId"))
		if err != nil || !part.Available || !source.Available {
			writeError(w, 404, "MEDIA_UNAVAILABLE", "media part not available")
			return
		}
		decision, err := deps.Catalog.SubtitleDecision(r.Context(), part.ID)
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "subtitle policy unavailable")
			return
		}
		if decision.Disposition != "active" {
			writeError(w, 409, "SUBTITLE_DECISION_HOLD", "video is skipped or held for review; restore active before installing")
			return
		}
		installed, err := subtitles.Install(part.Path, source.DurationMS, request.CandidateID)
		if err != nil {
			writeError(w, 409, "SUBTITLE_INSTALL_BLOCKED", err.Error())
			return
		}
		writeJSON(w, 201, installed)
	}
}
