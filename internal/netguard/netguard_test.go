package netguard

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestDefaultGuardAddressClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		addr string
		want int
	}{
		{"loopback", "127.0.0.1:1234", http.StatusNoContent},
		{"rfc1918-10", "10.2.3.4:1234", http.StatusNoContent},
		{"rfc1918-172", "172.16.9.8:1234", http.StatusNoContent},
		{"rfc1918-192", "192.168.5.4:1234", http.StatusNoContent},
		{"ipv4-link-local", "169.254.8.9:1234", http.StatusNoContent},
		{"ipv6-loopback", "[::1]:1234", http.StatusNoContent},
		{"ipv6-link-local", "[fe80::1]:1234", http.StatusNoContent},
		{"ipv6-ula", "[fd12:3456::1]:1234", http.StatusNoContent},
		{"public-v4", "8.8.8.8:1234", http.StatusForbidden},
		{"public-v6", "[2001:4860:4860::8888]:1234", http.StatusForbidden},
	}
	g := New(nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := g.Middleware(next)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
			req.RemoteAddr = tt.addr
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestUntrustedForwardedAddressCannotBypassGuard(t *testing.T) {
	g := New(nil, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.Header.Set("X-Forwarded-For", "192.168.1.12")
	rec := httptest.NewRecorder()

	g.Middleware(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusForbidden)
	}
}

func TestTrustedProxyUsesForwardedClientAddress(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	g := New(trusted, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

	t.Run("private client accepted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "192.168.1.12")
		rec := httptest.NewRecorder()
		g.Middleware(next).ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
		}
	})

	t.Run("public client rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "8.8.8.8")
		rec := httptest.NewRecorder()
		g.Middleware(next).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
		}
	})
}
