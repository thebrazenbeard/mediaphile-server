package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/transcode"
)

func TestTranscodeArtifactIsSessionScoped(t *testing.T) {
	deps, _ := authAPIDeps(t)
	root := t.TempDir()
	deps.Transcodes = transcode.NewManager(root, "ffmpeg", nil)
	sessionDir := filepath.Join(root, "session-1")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "master.m3u8"), []byte("#EXTM3U\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewRouter(deps)
	token := bootstrapToken(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transcode/session-1/master.m3u8", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "#EXTM3U\n" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/transcode/session-1/..%252Fsecret", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "secret contents") {
		t.Fatalf("traversal accepted: %d %s", rec.Code, rec.Body.String())
	}
}
