package discovery

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHandlePacketReturnsCredentialFreeServerIdentity(t *testing.T) {
	req := []byte(`{"protocol":"mediaphile-discovery","version":1,"type":"discover","clientId":"tv-1"}`)
	out, ok := HandlePacket(req, Info{ServerID: "server-1", Name: "Mediaphile", HTTPPort: 8097, APIVersions: []string{"v1"}, Capabilities: []string{"direct-play", "hls"}})
	if !ok {
		t.Fatal("valid discovery request ignored")
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["serverId"] != "server-1" || got["httpPort"].(float64) != 8097 {
		t.Fatalf("response=%v", got)
	}
	lower := strings.ToLower(string(out))
	if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
		t.Fatalf("credentials leaked: %s", out)
	}
}

func TestHandlePacketIgnoresOtherProtocols(t *testing.T) {
	if _, ok := HandlePacket([]byte(`{"protocol":"plex","type":"discover"}`), Info{}); ok {
		t.Fatal("foreign protocol accepted")
	}
}
