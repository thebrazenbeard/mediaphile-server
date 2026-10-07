package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/auth"
	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/events"
	"github.com/thebrazenbeard/mediaphile-server/internal/httpapi"
	"github.com/thebrazenbeard/mediaphile-server/internal/library"
	"github.com/thebrazenbeard/mediaphile-server/internal/netguard"
	"github.com/thebrazenbeard/mediaphile-server/internal/playback"
	"github.com/thebrazenbeard/mediaphile-server/internal/probe"
)

type fakeProber struct{}

func (fakeProber) Probe(context.Context, string) (probe.MediaInfo, error) {
	return probe.MediaInfo{Container: "mp4", DurationMS: 10_000, Bitrate: 1_000_000, Width: 640, Height: 360, VideoCodec: "h264", AudioCodec: "aac", Streams: []probe.Stream{
		{Index: 0, Kind: probe.StreamVideo, Codec: "h264", Width: 640, Height: 360, Default: true},
		{Index: 1, Kind: probe.StreamAudio, Codec: "aac", Channels: 2, Default: true},
	}}, nil
}

func TestScanBrowseDecideAndRangeStream(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "Arrival (2016).mp4")
	if err := os.WriteFile(mediaPath, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := catalog.NewRepository(db)
	ctx := context.Background()
	if err := repo.CreateLibrary(ctx, catalog.Library{ID: "movies", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: root, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	scanner := library.NewScanner(repo, fakeProber{})
	result, err := scanner.Scan(ctx, "movies")
	if err != nil {
		t.Fatal(err)
	}
	if result.Probed != 1 {
		t.Fatalf("scan=%#v", result)
	}

	authSvc := auth.NewWithBootstrap(repo, "bootstrap-secret")
	bus := events.NewBus()
	sessions := playback.NewSessionManager(repo, bus)
	handler := netguard.New(nil, nil).Middleware(httpapi.NewRouter(httpapi.Dependencies{
		Catalog: repo, Auth: authSvc, Events: bus, Sessions: sessions, FFmpegAvailable: true,
	}))
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()

	bootstrapBody := []byte(`{"bootstrapSecret":"bootstrap-secret","username":"admin","password":"password123"}`)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/setup/bootstrap", bytes.NewReader(bootstrapBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var boot struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&boot); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || boot.Token == "" {
		t.Fatalf("bootstrap status=%d token=%q", resp.StatusCode, boot.Token)
	}

	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/items?libraryId=movies&kind=movie", nil)
	req.Header.Set("Authorization", "Bearer "+boot.Token)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(page.Items) != 1 {
		t.Fatalf("browse status=%d items=%v", resp.StatusCode, page.Items)
	}

	decisionBody, _ := json.Marshal(map[string]any{
		"itemId": page.Items[0].ID,
		"capabilities": map[string]any{
			"clientId": "browser", "containers": []string{"mp4"}, "videoCodecs": []string{"h264"}, "audioCodecs": []string{"aac"},
			"subtitleCodecs": []string{"webvtt"}, "maxWidth": 1920, "maxHeight": 1080, "maxVideoBitrate": 10_000_000, "hls": true, "rangeRequests": true,
		},
	})
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/playback/decide", bytes.NewReader(decisionBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+boot.Token)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var decision struct {
		Mode   string `json:"mode"`
		PartID string `json:"partId"`
		URL    string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decision); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || decision.Mode != "DIRECT_PLAY" || decision.PartID == "" {
		t.Fatalf("decision status=%d value=%#v", resp.StatusCode, decision)
	}

	req, _ = http.NewRequest(http.MethodGet, server.URL+decision.URL, nil)
	req.Header.Set("Authorization", "Bearer "+boot.Token)
	req.Header.Set("Range", "bytes=2-5")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4)
	n, err := resp.Body.Read(data)
	if err != nil && n == 0 {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(data[:n]) != "2345" {
		t.Fatalf("stream status=%d body=%q", resp.StatusCode, data[:n])
	}
}
