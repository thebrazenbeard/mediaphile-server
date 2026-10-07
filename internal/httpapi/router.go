package httpapi

import "net/http"

func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", health)
	return mux
}
