package netguard

import (
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

var defaultAllowed = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fc00::/7"),
}

type Guard struct {
	trusted []netip.Prefix
	allowed []netip.Prefix
}

func New(trustedProxies, allowed []netip.Prefix) *Guard {
	if len(allowed) == 0 {
		allowed = append([]netip.Prefix(nil), defaultAllowed...)
	}
	return &Guard{
		trusted: append([]netip.Prefix(nil), trustedProxies...),
		allowed: append([]netip.Prefix(nil), allowed...),
	}
}

func (g *Guard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr, ok := parseRemoteAddr(r.RemoteAddr)
		if !ok {
			writeForbidden(w)
			return
		}

		if contains(g.trusted, addr) {
			if forwarded, ok := firstForwardedAddr(r.Header.Get("X-Forwarded-For")); ok {
				addr = forwarded
			}
		}

		if !contains(g.allowed, addr) {
			writeForbidden(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func parseRemoteAddr(remote string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	host = strings.Trim(host, "[]")
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func firstForwardedAddr(header string) (netip.Addr, bool) {
	if header == "" {
		return netip.Addr{}, false
	}
	first := strings.TrimSpace(strings.Split(header, ",")[0])
	addr, err := netip.ParseAddr(first)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func contains(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    "LAN_ONLY",
			"message": "request source is outside the configured local network",
		},
	})
}

func IsDefaultAllowed(addr netip.Addr) bool {
	addr = addr.Unmap()
	return contains(defaultAllowed, addr)
}

// IsLANPrefix validates the entire CIDR, not merely its first address.
func IsLANPrefix(prefix netip.Prefix) bool {
	if !prefix.IsValid() {
		return false
	}
	if prefix.Addr().Is4In6() {
		return false
	}
	for _, allowed := range defaultAllowed {
		if prefix.Bits() >= allowed.Bits() && allowed.Contains(prefix.Masked().Addr()) {
			return true
		}
	}
	return false
}
