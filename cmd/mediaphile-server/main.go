package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/thebrazenbeard/mediaphile-server/internal/auth"
	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
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

	db, err := catalog.Open(filepath.Join(cfg.DataDir, "mediaphile.db"))
	if err != nil {
		log.Fatalf("open catalog: %v", err)
	}
	defer db.Close()
	repo := catalog.NewRepository(db)
	authService, bootstrapSecret, err := auth.New(repo)
	if err != nil {
		log.Fatalf("initialize authentication: %v", err)
	}
	if !authService.Initialized(context.Background()) {
		log.Printf("Mediaphile bootstrap secret: %s", bootstrapSecret)
	}
	_, ffmpegErr := exec.LookPath("ffmpeg")

	handler := netguard.New(cfg.TrustedProxies, cfg.AllowedCIDRs).Middleware(httpapi.NewRouter(httpapi.Dependencies{
		Catalog: repo, Auth: authService, FFmpegAvailable: ffmpegErr == nil,
	}))
	server := &http.Server{Addr: cfg.HTTPAddress(), Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	log.Printf("Mediaphile Server listening on %s (LAN-only ingress)", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
