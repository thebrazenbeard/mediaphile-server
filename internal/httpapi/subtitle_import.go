package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

// subtitleImportExclusions handles historical Lappy exclusion manifests.
// It maps exact paths to known media in a single database transaction; no
// access to media storage or subtitle installation is permitted.
func subtitleImportExclusions(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, 503, "CATALOG_UNAVAILABLE", "catalog unavailable")
			return
		}
		var payload struct {
			Exclusions []catalog.SubtitleExclusionImport `json:"exclusions"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 262144))
		if err := dec.Decode(&payload); err != nil {
			writeError(w, 400, "INVALID_JSON", "invalid exclusion manifest")
			return
		}
		if len(payload.Exclusions) < 1 || len(payload.Exclusions) > 250 {
			writeError(w, 400, "INVALID_MANIFEST", "exclusions must contain 1-250 items")
			return
		}
		imported, err := deps.Catalog.ImportSubtitleExclusions(r.Context(), payload.Exclusions)
		if errors.Is(err, catalog.ErrSubtitleMediaNotFound) || errors.Is(err, sql.ErrNoRows) {
			writeError(w, 409, "UNKNOWN_MEDIA", "manifest contains a media path not in this catalog; no entries were imported")
			return
		}
		if err != nil {
			writeError(w, 400, "INVALID_MANIFEST", "manifest entries invalid; no entries imported")
			return
		}
		writeJSON(w, 200, map[string]any{"imported": imported, "total": len(payload.Exclusions)})
	}
}
