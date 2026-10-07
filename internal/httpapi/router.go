package httpapi

import (
	"net/http"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

type Dependencies struct {
	Catalog    *catalog.Repository
	ServerID   string
	ServerName string
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
	mux.HandleFunc("GET /api/v1/libraries", libraries(deps))
	mux.HandleFunc("GET /api/v1/items", items(deps))
	mux.HandleFunc("GET /api/v1/items/{itemId}", itemDetail(deps))
	return mux
}
