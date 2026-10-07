package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/events"
	"github.com/thebrazenbeard/mediaphile-server/internal/playback"
)

func TestPlaybackSessionHTTPRoundTrip(t *testing.T) {
	deps, _ := authAPIDeps(t)
	deps.Events = events.NewBus()
	deps.Sessions = playback.NewSessionManager(deps.Catalog, deps.Events)
	ctx := context.Background()
	if err := deps.Catalog.CreateLibrary(ctx, catalog.Library{ID: "movies", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: "C:/media", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertItem(ctx, catalog.Item{ID: "movie", LibraryID: "movies", Kind: catalog.ItemMovie, Title: "Movie"}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertMediaSource(ctx, catalog.MediaSource{ID: "source", ItemID: "movie", Container: "mp4", DurationMS: 10_000, Available: true}); err != nil {
		t.Fatal(err)
	}
	h := NewRouter(deps)
	token := bootstrapToken(t, h)
	rec := postJSON(t, h, "/api/v1/playback/sessions", map[string]any{"itemId": "movie", "clientId": "browser", "sourceId": "source", "decision": "DIRECT_PLAY"}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	reqBody := strings.NewReader(`{"positionMs":2500,"state":"paused"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/playback/sessions/"+created.ID, reqBody)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	up := httptest.NewRecorder()
	h.ServeHTTP(up, req)
	if up.Code != http.StatusOK {
		t.Fatalf("patch=%d %s", up.Code, up.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/users/me/playback/movie", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	state := httptest.NewRecorder()
	h.ServeHTTP(state, req)
	if state.Code != http.StatusOK || !strings.Contains(state.Body.String(), "\"resumeMs\":2500") {
		t.Fatalf("state=%d %s", state.Code, state.Body.String())
	}
}
