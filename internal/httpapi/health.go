package httpapi

import (
	"encoding/json"
	"net/http"
)

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":       "ok",
		"liveness":     "ok",
		"readiness":    "ok",
		"capabilities": map[string]any{},
	})
}
