package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

type libraryDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Enabled   bool   `json:"enabled"`
}

func serverInfo(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		initialized := false
		if deps.Auth != nil {
			initialized = deps.Auth.Initialized(r.Context())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": deps.ServerID, "name": deps.ServerName, "apiVersions": []string{"v1"}, "initialized": initialized,
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

func createLibrary(deps Dependencies) http.HandlerFunc {
	type request struct {
		ID        string              `json:"id"`
		Name      string              `json:"name"`
		MediaType catalog.LibraryType `json:"mediaType"`
		RootPath  string              `json:"rootPath"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, http.StatusServiceUnavailable, "CATALOG_UNAVAILABLE", "catalog is unavailable")
			return
		}
		var in request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
			return
		}
		if in.ID == "" || in.Name == "" || in.RootPath == "" || (in.MediaType != catalog.LibraryMovies && in.MediaType != catalog.LibraryTV) {
			writeError(w, http.StatusBadRequest, "INVALID_LIBRARY", "id, name, rootPath and valid mediaType are required")
			return
		}
		v := catalog.Library{ID: in.ID, Name: in.Name, MediaType: in.MediaType, RootPath: in.RootPath, Enabled: true}
		if err := deps.Catalog.CreateLibrary(r.Context(), v); err != nil {
			writeError(w, http.StatusConflict, "LIBRARY_ERROR", "could not create library")
			return
		}
		writeJSON(w, http.StatusCreated, libraryDTO{ID: v.ID, Name: v.Name, MediaType: string(v.MediaType), Enabled: true})
	}
}
