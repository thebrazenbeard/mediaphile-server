package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/thebrazenbeard/mediaphile-server/internal/playback"
)

func playbackDecide(deps Dependencies) http.HandlerFunc {
	type request struct {
		ItemID       string                `json:"itemId"`
		Capabilities playback.Capabilities `json:"capabilities"`
		Selection    playback.Selection    `json:"selection"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, http.StatusServiceUnavailable, "CATALOG_UNAVAILABLE", "catalog is unavailable")
			return
		}
		var in request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ItemID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "itemId and capabilities are required")
			return
		}
		sources, parts, streams, err := deps.Catalog.MediaInventory(r.Context(), in.ItemID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CATALOG_ERROR", "could not load media inventory")
			return
		}
		partBySource := map[string]string{}
		for _, part := range parts {
			if part.Available && partBySource[part.SourceID] == "" {
				partBySource[part.SourceID] = part.ID
			}
		}
		streamsByPart := map[string][]playback.Stream{}
		for _, stream := range streams {
			streamsByPart[stream.PartID] = append(streamsByPart[stream.PartID], playback.Stream{ID: stream.ID, Kind: string(stream.Kind), Codec: stream.Codec})
		}
		input := make([]playback.Source, 0, len(sources))
		for _, source := range sources {
			partID := partBySource[source.ID]
			input = append(input, playback.Source{
				ID: source.ID, PartID: partID, Container: source.Container, VideoCodec: source.VideoCodec, AudioCodec: source.AudioCodec,
				Width: source.Width, Height: source.Height, Bitrate: source.Bitrate, Available: source.Available && partID != "", Streams: streamsByPart[partID],
			})
		}
		decision := playback.Decide(input, in.Capabilities, in.Selection, deps.FFmpegAvailable)
		writeJSON(w, http.StatusOK, decision)
	}
}
