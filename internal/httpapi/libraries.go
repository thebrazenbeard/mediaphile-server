package httpapi

import (
	"net/http"
)

type libraryDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Enabled   bool   `json:"enabled"`
}

func serverInfo(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          deps.ServerID,
			"name":        deps.ServerName,
			"apiVersions": []string{"v1"},
			"initialized": false,
		})
	}
}

func libraries(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, http.StatusServiceUnavailable, "CATALOG_UNAVAILABLE", "catalog is unavailable")
			return
		}
		values, err := deps.Catalog.ListLibraries(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CATALOG_ERROR", "could not list libraries")
			return
		}
		out := make([]libraryDTO, 0, len(values))
		for _, v := range values {
			out = append(out, libraryDTO{ID: v.ID, Name: v.Name, MediaType: string(v.MediaType), Enabled: v.Enabled})
		}
		writeJSON(w, http.StatusOK, map[string]any{"libraries": out})
	}
}
