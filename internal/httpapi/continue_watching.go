package httpapi

import (
	"net/http"
	"strconv"
)

func continueWatching(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, http.StatusServiceUnavailable, "CATALOG_UNAVAILABLE", "catalog is unavailable")
			return
		}
		principal, ok := principalFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		limit := 12
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 24 {
				writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limit must be between 1 and 24")
				return
			}
			limit = n
		}
		entries, err := deps.Catalog.ListContinueWatching(r.Context(), principal.ID, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CATALOG_ERROR", "could not list playback progress")
			return
		}
		out := make([]any, 0, len(entries))
		for _, e := range entries {
			out = append(out, map[string]any{"item": toItemDTO(e.Item), "resumeMs": e.ResumeMS, "lastPlayedAt": e.LastPlayedAt})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": out})
	}
}
