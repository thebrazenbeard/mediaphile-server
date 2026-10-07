package main

import (
	"log"
	"net/http"
	"os"

	"github.com/thebrazenbeard/mediaphile-server/internal/config"
	"github.com/thebrazenbeard/mediaphile-server/internal/httpapi"
	"github.com/thebrazenbeard/mediaphile-server/internal/netguard"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}
	for _, dir := range []string{cfg.DataDir, cfg.TranscodeDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			log.Fatalf("create %s: %v", dir, err)
		}
	}

	handler := netguard.New(cfg.TrustedProxies, cfg.AllowedCIDRs).Middleware(httpapi.NewRouter())
	server := &http.Server{
		Addr:              cfg.HTTPAddress(),
		Handler:           handler,
		ReadHeaderTimeout: 10 * 1e9,
	}

	log.Printf("Mediaphile Server listening on %s (LAN-only ingress)", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
