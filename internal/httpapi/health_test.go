package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/netguard"
)

func TestHealthBehindLANGuard(t *testing.T) {
	h := netguard.New(nil, nil).Middleware(NewRouter())
	req := httptest.NewRequest(http.MethodGet, "http://mediaphile.local/api/v1/health", nil)
	req.RemoteAddr = "192.168.1.9:5555"
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=200 body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type=%q", got)
	}
}

func TestHealthRejectsPublicSource(t *testing.T) {
	h := netguard.New(nil, nil).Middleware(NewRouter())
	req := httptest.NewRequest(http.MethodGet, "http://mediaphile.local/api/v1/health", nil)
	req.RemoteAddr = "8.8.8.8:5555"
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=403", rec.Code)
	}
}
