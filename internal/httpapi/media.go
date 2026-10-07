package httpapi

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/thebrazenbeard/mediaphile-server/internal/stream"
)

func mediaContent(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, http.StatusServiceUnavailable, "CATALOG_UNAVAILABLE", "catalog is unavailable")
			return
		}
		part, source, err := deps.Catalog.GetMediaPartWithSource(r.Context(), r.PathValue("partId"))
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "MEDIA_UNAVAILABLE", "media part is unavailable")
			} else {
				writeError(w, http.StatusInternalServerError, "CATALOG_ERROR", "could not resolve media part")
			}
			return
		}
		if !part.Available || !source.Available {
			writeError(w, http.StatusNotFound, "MEDIA_UNAVAILABLE", "media part is unavailable")
			return
		}
		if err := stream.ServeFile(w, r, part.Path, contentTypeForContainer(source.Container)); err != nil {
			writeError(w, http.StatusNotFound, "MEDIA_UNAVAILABLE", "media file is unavailable")
		}
	}
}

func contentTypeForContainer(container string) string {
	switch strings.ToLower(container) {
	case "mp4", "mov,mp4,m4a,3gp,3g2,mj2":
		return "video/mp4"
	case "matroska", "mkv":
		return "video/x-matroska"
	case "webm":
		return "video/webm"
	case "mov":
		return "video/quicktime"
	case "avi":
		return "video/x-msvideo"
	default:
		return "application/octet-stream"
	}
}
