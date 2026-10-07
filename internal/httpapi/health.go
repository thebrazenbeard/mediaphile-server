package httpapi

import (
	"encoding/json"
	"net/http"
)

func health(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		readiness := "ok"
		if deps.Catalog == nil {
			readiness = "degraded"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "liveness": "ok", "readiness": readiness,
			"capabilities": map[string]any{"ffmpeg": deps.FFmpegAvailable, "ffprobe": deps.FFprobeAvailable},
		})
	}
}
