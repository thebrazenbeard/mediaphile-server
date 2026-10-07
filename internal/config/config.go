package config

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/thebrazenbeard/mediaphile-server/internal/netguard"
)

type Config struct {
	ListenAddr     string
	HTTPPort       int
	DiscoveryPort  int
	DataDir        string
	TranscodeDir   string
	UIDir          string
	TrustedProxies []netip.Prefix
	AllowedCIDRs   []netip.Prefix
}

func Default() Config {
	return Config{
		ListenAddr:    "0.0.0.0",
		HTTPPort:      8097,
		DiscoveryPort: 8098,
		DataDir:       filepath.Clean("./data"),
		TranscodeDir:  filepath.Clean("./transcode"),
		UIDir:         filepath.Clean("./ui"),
	}
}

func Load() (Config, error) {
	cfg := Default()
	if v := strings.TrimSpace(os.Getenv("MEDIAPHILE_LISTEN_ADDR")); v != "" {
		cfg.ListenAddr = v
	}
	if v := strings.TrimSpace(os.Getenv("MEDIAPHILE_HTTP_PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("invalid MEDIAPHILE_HTTP_PORT %q", v)
		}
		cfg.HTTPPort = port
	}
	if v := strings.TrimSpace(os.Getenv("MEDIAPHILE_DISCOVERY_PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("invalid MEDIAPHILE_DISCOVERY_PORT %q", v)
		}
		cfg.DiscoveryPort = port
	}
	if v := strings.TrimSpace(os.Getenv("MEDIAPHILE_DATA_DIR")); v != "" {
		cfg.DataDir = filepath.Clean(v)
	}
	if v := strings.TrimSpace(os.Getenv("MEDIAPHILE_TRANSCODE_DIR")); v != "" {
		cfg.TranscodeDir = filepath.Clean(v)
	}
	if v := strings.TrimSpace(os.Getenv("MEDIAPHILE_UI_DIR")); v != "" {
		cfg.UIDir = filepath.Clean(v)
	}

	var err error
	if cfg.TrustedProxies, err = parsePrefixes(os.Getenv("MEDIAPHILE_TRUSTED_PROXIES")); err != nil {
		return Config{}, fmt.Errorf("trusted proxies: %w", err)
	}
	if cfg.AllowedCIDRs, err = parsePrefixes(os.Getenv("MEDIAPHILE_ALLOWED_CIDRS")); err != nil {
		return Config{}, fmt.Errorf("allowed CIDRs: %w", err)
	}
	for _, prefix := range cfg.AllowedCIDRs {
		if !netguard.IsDefaultAllowed(prefix.Addr()) ||
			!netguard.IsDefaultAllowed(prefix.Masked().Addr()) ||
			!netguard.IsLANPrefix(prefix) {
			return Config{}, fmt.Errorf("MEDIAPHILE_ALLOWED_CIDRS cannot expand access outside LAN: %s", prefix)
		}
	}
	return cfg, nil
}

func (c Config) HTTPAddress() string { return fmt.Sprintf("%s:%d", c.ListenAddr, c.HTTPPort) }

func parsePrefixes(raw string) ([]netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		out = append(out, prefix)
	}
	return out, nil
}
