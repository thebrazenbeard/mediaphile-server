package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/thebrazenbeard/mediaphile-server/internal/auth"
	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/config"
	"github.com/thebrazenbeard/mediaphile-server/internal/discovery"
	"github.com/thebrazenbeard/mediaphile-server/internal/events"
	"github.com/thebrazenbeard/mediaphile-server/internal/httpapi"
	"github.com/thebrazenbeard/mediaphile-server/internal/library"
	"github.com/thebrazenbeard/mediaphile-server/internal/netguard"
	"github.com/thebrazenbeard/mediaphile-server/internal/playback"
	"github.com/thebrazenbeard/mediaphile-server/internal/probe"
	"github.com/thebrazenbeard/mediaphile-server/internal/transcode"
	"github.com/thebrazenbeard/mediaphile-server/internal/ui"
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

	ffmpegPath, ffmpegErr := exec.LookPath("ffmpeg")
	ffprobePath, ffprobeErr := exec.LookPath("ffprobe")
	eventBus := events.NewBus()
	sessionManager := playback.NewSessionManager(repo, eventBus)
	transcodeManager := transcode.NewManager(cfg.TranscodeDir, ffmpegPath, nil)
	webhookService := events.NewWebhookService(repo, eventBus, events.NewDispatcher(nil))
	go webhookService.Run(context.Background())

	var scanner *library.Scanner
	if ffprobeErr == nil {
		scanner = library.NewScanner(repo, probe.NewFFProbe(ffprobePath))
	}

	serverID := "mediaphile-server"
	serverName := "Mediaphile"
	uiHandler := ui.NewStatic(cfg.UIDir)
	handler := netguard.New(cfg.TrustedProxies, cfg.AllowedCIDRs).Middleware(httpapi.NewRouter(httpapi.Dependencies{
		Catalog: repo, Auth: authService, Events: eventBus, Webhooks: webhookService, Sessions: sessionManager,
		Transcodes: transcodeManager, Scanner: scanner, UI: uiHandler, ServerID: serverID, ServerName: serverName,
		FFmpegAvailable: ffmpegErr == nil, FFprobeAvailable: ffprobeErr == nil,
	}))

	go func() {
		addr := fmt.Sprintf(":%d", cfg.DiscoveryPort)
		info := discovery.Info{ServerID: serverID, Name: serverName, HTTPPort: cfg.HTTPPort, APIVersions: []string{"v1"}, Capabilities: []string{"direct-play", "hls"}}
		if err := discovery.Serve(context.Background(), addr, info); err != nil {
			log.Printf("discovery stopped: %v", err)
		}
	}()

	server := &http.Server{Addr: cfg.HTTPAddress(), Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("Mediaphile Server listening on %s (LAN-only ingress)", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
