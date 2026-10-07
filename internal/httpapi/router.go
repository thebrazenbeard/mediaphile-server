package httpapi

import (
	"net/http"

	"github.com/thebrazenbeard/mediaphile-server/internal/auth"
	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/events"
	"github.com/thebrazenbeard/mediaphile-server/internal/playback"
	"github.com/thebrazenbeard/mediaphile-server/internal/transcode"
)

type Dependencies struct {
	Catalog         *catalog.Repository
	Auth            *auth.Service
	Events          *events.Bus
	Sessions        *playback.SessionManager
	Transcodes      *transcode.Manager
	ServerID        string
	ServerName      string
	FFmpegAvailable bool
}

func NewRouter(values ...Dependencies) http.Handler {
	var deps Dependencies
	if len(values) != 0 {
		deps = values[0]
	}
	if deps.ServerID == "" {
		deps.ServerID = "mediaphile-server"
	}
	if deps.ServerName == "" {
		deps.ServerName = "Mediaphile"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", health)
	mux.HandleFunc("GET /api/v1/server", serverInfo(deps))
	mux.HandleFunc("POST /api/v1/setup/bootstrap", bootstrap(deps))
	mux.HandleFunc("POST /api/v1/auth/login", login(deps))
	mux.Handle("POST /api/v1/auth/logout", requirePrincipal(deps, false, logout(deps)))
	mux.Handle("GET /api/v1/libraries", requirePrincipal(deps, false, libraries(deps)))
	mux.Handle("POST /api/v1/libraries", requirePrincipal(deps, true, createLibrary(deps)))
	mux.Handle("GET /api/v1/items", requirePrincipal(deps, false, items(deps)))
	mux.Handle("GET /api/v1/items/{itemId}", requirePrincipal(deps, false, itemDetail(deps)))
	mux.Handle("POST /api/v1/playback/decide", requirePrincipal(deps, false, playbackDecide(deps)))
	mux.Handle("POST /api/v1/playback/sessions", requirePrincipal(deps, false, createPlaybackSession(deps)))
	mux.Handle("PATCH /api/v1/playback/sessions/{sessionId}", requirePrincipal(deps, false, updatePlaybackSession(deps)))
	mux.Handle("DELETE /api/v1/playback/sessions/{sessionId}", requirePrincipal(deps, false, deletePlaybackSession(deps)))
	mux.Handle("GET /api/v1/users/me/playback/{itemId}", requirePrincipal(deps, false, getPlaybackState(deps)))
	mux.Handle("PUT /api/v1/users/me/playback/{itemId}", requirePrincipal(deps, false, putPlaybackState(deps)))
	mux.Handle("GET /api/v1/media/{partId}/content", requirePrincipal(deps, false, mediaContent(deps)))
	mux.Handle("HEAD /api/v1/media/{partId}/content", requirePrincipal(deps, false, mediaContent(deps)))
	mux.Handle("GET /api/v1/transcode/{sessionId}/{artifact...}", requirePrincipal(deps, false, transcodeArtifact(deps)))
	return mux
}
