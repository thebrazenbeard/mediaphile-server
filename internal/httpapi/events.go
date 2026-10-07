package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func streamEvents(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Events == nil {
			writeError(w, http.StatusServiceUnavailable, "EVENTS_UNAVAILABLE", "event service is unavailable")
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "STREAM_UNAVAILABLE", "streaming is unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		principal, ok := principalFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		ch, cancel := deps.Events.Subscribe(32)
		defer cancel()
		for {
			select {
			case <-r.Context().Done():
				return
			case event, ok := <-ch:
				if !ok {
					return
				}
				if event.PrincipalID != "" && event.PrincipalID != principal.ID {
					// Do not broadcast one user's playback activity to another.
					continue
				}
				data, err := json.Marshal(event)
				if err != nil {
					continue
				}
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
