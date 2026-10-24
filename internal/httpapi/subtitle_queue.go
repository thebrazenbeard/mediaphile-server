package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

type subtitleQueueReply struct {
	Planned int                         `json:"planned"`
	Items   []catalog.SubtitleQueueItem `json:"items"`
	Total   int                         `json:"total"`
	Offset  int                         `json:"offset"`
	Limit   int                         `json:"limit"`
}

func subtitleQueuePagination(r *http.Request) (int, int, error) {
	limit, offset := 25, 0
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 50 {
			return 0, 0, errors.New("invalid limit")
		}
		limit = n
	}
	if s := r.URL.Query().Get("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return 0, 0, errors.New("invalid offset")
		}
		offset = n
	}
	return limit, offset, nil
}

func librarySubtitleQueue(deps Dependencies, refresh bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, 503, "CATALOG_UNAVAILABLE", "catalog unavailable")
			return
		}
		libraryID := r.PathValue("libraryId")
		if _, err := deps.Catalog.GetLibrary(r.Context(), libraryID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, 404, "LIBRARY_NOT_FOUND", "library not found")
			} else {
				writeError(w, 500, "CATALOG_ERROR", "library lookup failed")
			}
			return
		}
		limit, offset, err := subtitleQueuePagination(r)
		if err != nil {
			writeError(w, 400, "INVALID_PAGINATION", err.Error())
			return
		}
		planned := 0
		var refreshed []catalog.SubtitleQueueItem
		refreshedTotal := 0
		if refresh {
			var body struct {
				Offset *int `json:"offset"`
				Limit  *int `json:"limit"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&body); err != nil {
				writeError(w, 400, "INVALID_JSON", "invalid queue refresh request")
				return
			}
			var extra any
			if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
				writeError(w, 400, "INVALID_JSON", "multiple JSON objects are not allowed")
				return
			}
			if body.Limit != nil {
				limit = *body.Limit
			}
			if body.Offset != nil {
				offset = *body.Offset
			}
			if limit < 1 || limit > 50 || offset < 0 {
				writeError(w, 400, "INVALID_PAGINATION", "limit must be 1-50 and offset must be nonnegative")
				return
			}
			parts, err := deps.Catalog.ListPartsByLibrary(r.Context(), libraryID)
			if err != nil {
				writeError(w, 500, "CATALOG_ERROR", "cannot list media parts")
				return
			}
			// Bound the number of filesystem inspections in any single call.
			batch := []catalog.SubtitleQueueItem{}
			if offset < len(parts) {
				end := offset + limit
				if end > len(parts) {
					end = len(parts)
				}
				for _, part := range parts[offset:end] {
					if !part.Available {
						continue
					}
					reviewed, err := collectSubtitlePart(deps, r, part.ID, false)
					if err != nil {
						writeError(w, 409, "QUEUE_REFRESH_HELD", "could not inspect all media; no queue records changed")
						return
					}
					batch = append(batch, catalog.SubtitleQueueItem{
						PartID: part.ID, FileName: filepath.Base(part.Path), Action: reviewed.NextAction,
						Coverage: reviewed.Review.Coverage, Disposition: reviewed.Decision.Disposition,
						CandidateCount: len(reviewed.Review.Candidates), ObservedSize: part.Size, ObservedModTimeNS: part.ModTimeNS,
					})
				}
			}
			if err := deps.Catalog.UpsertSubtitleQueueBatch(r.Context(), libraryID, batch); err != nil {
				writeError(w, 409, "QUEUE_REFRESH_HELD", "queue refresh could not commit atomically")
				return
			}
			planned = len(batch)
			refreshed = batch
			refreshedTotal = len(parts)
		}
		if refresh {
			writeJSON(w, 200, subtitleQueueReply{Planned: planned, Items: refreshed, Total: refreshedTotal, Limit: limit, Offset: offset})
			return
		}
		rows, total, err := deps.Catalog.SubtitleQueue(r.Context(), libraryID, limit, offset)
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "queue read failed")
			return
		}
		writeJSON(w, 200, subtitleQueueReply{Planned: planned, Items: rows, Total: total, Limit: limit, Offset: offset})
	}
}
