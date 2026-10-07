package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/thebrazenbeard/mediaphile-server/internal/provenance"
)

func importProvenance(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, http.StatusServiceUnavailable, "CATALOG_UNAVAILABLE", "catalog is unavailable")
			return
		}
		var artifact provenance.Artifact
		if err := json.NewDecoder(r.Body).Decode(&artifact); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
			return
		}
		result, err := provenance.Import(r.Context(), deps.Catalog, artifact)
		if err != nil {
			writeError(w, http.StatusBadRequest, "PROVENANCE_IMPORT_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
