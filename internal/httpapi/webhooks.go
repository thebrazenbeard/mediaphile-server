package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/thebrazenbeard/mediaphile-server/internal/events"
)

func listWebhooks(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Webhooks == nil {
			writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "webhook service is unavailable")
			return
		}
		values, err := deps.Webhooks.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "WEBHOOK_ERROR", "could not list webhooks")
			return
		}
		out := make([]map[string]any, 0, len(values))
		for _, v := range values {
			var eventTypes []string
			_ = json.Unmarshal([]byte(v.EventTypes), &eventTypes)
			out = append(out, map[string]any{"id": v.ID, "targetUrl": v.TargetURL, "eventTypes": eventTypes, "enabled": v.Enabled})
		}
		writeJSON(w, http.StatusOK, map[string]any{"webhooks": out})
	}
}

func createWebhook(deps Dependencies) http.HandlerFunc {
	type request struct {
		TargetURL  string   `json:"targetUrl"`
		EventTypes []string `json:"eventTypes"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Webhooks == nil {
			writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "webhook service is unavailable")
			return
		}
		var in request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
			return
		}
		v, secret, err := deps.Webhooks.Create(r.Context(), in.TargetURL, in.EventTypes)
		if errors.Is(err, events.ErrWebhookTargetRejected) {
			writeError(w, http.StatusBadRequest, "WEBHOOK_TARGET_REJECTED", "webhook target must resolve only to local-network addresses")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "WEBHOOK_ERROR", err.Error())
			return
		}
		var eventTypes []string
		_ = json.Unmarshal([]byte(v.EventTypes), &eventTypes)
		writeJSON(w, http.StatusCreated, map[string]any{"id": v.ID, "targetUrl": v.TargetURL, "eventTypes": eventTypes, "enabled": v.Enabled, "secret": secret})
	}
}

func deleteWebhook(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Webhooks == nil {
			writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "webhook service is unavailable")
			return
		}
		err := deps.Webhooks.Delete(r.Context(), r.PathValue("webhookId"))
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "webhook not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "WEBHOOK_ERROR", "could not delete webhook")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
