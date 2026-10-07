package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func TestPlaybackDecisionEndpoint(t *testing.T) {
	deps, _ := authAPIDeps(t)
	ctx := context.Background()
	if err := deps.Catalog.CreateLibrary(ctx, catalog.Library{ID: "movies", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: "C:/media", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertItem(ctx, catalog.Item{ID: "movie", LibraryID: "movies", Kind: catalog.ItemMovie, Title: "Movie"}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertMediaSource(ctx, catalog.MediaSource{ID: "source", ItemID: "movie", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, Bitrate: 5_000_000, Available: true}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertMediaPart(ctx, catalog.MediaPart{ID: "part", SourceID: "source", Path: "C:/media/movie.mp4", Size: 100, Available: true}); err != nil {
		t.Fatal(err)
	}
	deps.FFmpegAvailable = true
	h := NewRouter(deps)
	bootstrap := postJSON(t, h, "/api/v1/setup/bootstrap", map[string]string{"bootstrapSecret": "bootstrap-secret", "username": "admin", "password": "password123"}, "")
	token := jsonToken(t, bootstrap.Body.Bytes())
	rec := postJSON(t, h, "/api/v1/playback/decide", map[string]any{
		"itemId":       "movie",
		"capabilities": map[string]any{"clientId": "browser", "containers": []string{"mp4"}, "videoCodecs": []string{"h264"}, "audioCodecs": []string{"aac"}, "subtitleCodecs": []string{"webvtt"}, "maxWidth": 1920, "maxHeight": 1080, "maxVideoBitrate": 10_000_000, "hls": true, "rangeRequests": true},
	}, token)
	if rec.Code != http.StatusOK || !containsAll(rec.Body.String(), "\"mode\":\"DIRECT_PLAY\"", "/api/v1/media/part/content") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func jsonToken(t *testing.T, data []byte) string {
	t.Helper()
	var v struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v.Token
}

func containsAll(s string, values ...string) bool {
	for _, v := range values {
		if !strings.Contains(s, v) {
			return false
		}
	}
	return true
}
