package netguard

import (
    "net/http"
    "net/http/httptest"
    "net/netip"
    "testing"
)

func TestInvalidForwardedClientAddress(t *testing.T) {
    g := New([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, nil)
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusNoContent)
    })
    request := httptest.NewRequest(http.MethodGet, "http://media.test/", nil)
    request.RemoteAddr = "127.0.0.1:5566"
    request.Header.Set("X-Forwarded-For", "not-an-address")
    rec := httptest.NewRecorder()
    g.Middleware(next).ServeHTTP(rec, request)
    if rec.Code != http.StatusForbidden {
        t.Fatalf("invalid forwarded address accepted: status %d", rec.Code)
    }
}
