package httpapi

import (
	"mime"
	"net/http"
	"path/filepath"

	"github.com/thebrazenbeard/mediaphile-server/internal/stream"
)

func transcodeArtifact(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Transcodes == nil {
			writeError(w, http.StatusServiceUnavailable, "TRANSCODE_UNAVAILABLE", "transcode service is unavailable")
			return
		}
		path, err := deps.Transcodes.ArtifactPath(r.PathValue("sessionId"), r.PathValue("artifact"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_TRANSCODE_PATH", "transcode artifact path is invalid")
			return
		}
		contentType := mime.TypeByExtension(filepath.Ext(path))
		switch filepath.Ext(path) {
		case ".m3u8":
			contentType = "application/vnd.apple.mpegurl"
		case ".ts":
			contentType = "video/mp2t"
		}
		if err := stream.ServeFile(w, r, path, contentType); err != nil {
			writeError(w, http.StatusNotFound, "TRANSCODE_ARTIFACT_UNAVAILABLE", "transcode artifact is unavailable")
		}
	}
}
