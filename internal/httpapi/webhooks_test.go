package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/events"
)

func TestWebhookAdminCreateListDeleteAndRejectPublicTarget(t *testing.T) {
	deps, _ := authAPIDeps(t)
	bus := events.NewBus()
	deps.Events = bus
	deps.Webhooks = events.NewWebhookService(deps.Catalog, bus, events.NewDispatcher(http.DefaultClient))
	h := NewRouter(deps)
	token := bootstrapToken(t, h)

	rejected := postJSON(t, h, "/api/v1/webhooks", map[string]any{"targetUrl": "http://8.8.8.8/hook", "eventTypes": []string{"media.play"}}, token)
	if rejected.Code != http.StatusBadRequest || !strings.Contains(rejected.Body.String(), "WEBHOOK_TARGET_REJECTED") {
		t.Fatalf("public target status=%d body=%s", rejected.Code, rejected.Body.String())
	}

	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer target.Close()
	created := postJSON(t, h, "/api/v1/webhooks", map[string]any{"targetUrl": target.URL, "eventTypes": []string{"media.play"}}, token)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), "\"secret\"") {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	list := httptest.NewRecorder()
	h.ServeHTTP(list, req)
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "secretHash") || strings.Contains(list.Body.String(), "\"secret\"") {
		t.Fatalf("list leaked secret status=%d body=%s", list.Code, list.Body.String())
	}

	var id string
	rows, err := deps.Catalog.ListWebhookSubscriptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
	id = rows[0].ID
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	del := httptest.NewRecorder()
	h.ServeHTTP(del, req)
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", del.Code, del.Body.String())
	}
}
