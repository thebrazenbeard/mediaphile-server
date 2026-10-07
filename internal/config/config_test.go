package config

import "testing"

func TestRejectsPublicCIDRAndBroadRanges(t *testing.T) {
	for _, bad := range []string{"0.0.0.0/0", "8.8.8.0/24", "10.0.0.0/7", "::/0"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("MEDIAPHILE_ALLOWED_CIDRS", bad)
			if _, err := Load(); err == nil {
				t.Fatalf("unsafe CIDR accepted: %s", bad)
			}
		})
	}
	t.Setenv("MEDIAPHILE_ALLOWED_CIDRS", "192.168.1.0/24,fd00::/8")
	if _, err := Load(); err != nil {
		t.Fatalf("LAN CIDRs rejected: %v", err)
	}
}
