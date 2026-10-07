package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func TestMediaContentUsesCatalogPartAndSupportsRange(t *testing.T) {
	deps, _ := authAPIDeps(t)
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "movie.mp4")
	if err := os.WriteFile(mediaPath, []byte("abcdefghij"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := deps.Catalog.CreateLibrary(ctx, catalog.Library{ID: "movies", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: dir, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertItem(ctx, catalog.Item{ID: "movie", LibraryID: "movies", Kind: catalog.ItemMovie, Title: "Movie"}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertMediaSource(ctx, catalog.MediaSource{ID: "source", ItemID: "movie", Container: "mp4", Available: true}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertMediaPart(ctx, catalog.MediaPart{ID: "part", SourceID: "source", Path: mediaPath, Size: 10, Available: true}); err != nil {
		t.Fatal(err)
	}
	h := NewRouter(deps)
	token := bootstrapToken(t, h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/part/content", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=3-6")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "defg" || rec.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("status=%d body=%q type=%q range=%q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"), rec.Header().Get("Content-Range"))
	}
}

func TestUnavailableAndBogusMediaPartCannotBecomePath(t *testing.T) {
	deps, _ := authAPIDeps(t)
	ctx := context.Background()
	if err := deps.Catalog.CreateLibrary(ctx, catalog.Library{ID: "movies", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: "C:/media", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertItem(ctx, catalog.Item{ID: "movie", LibraryID: "movies", Kind: catalog.ItemMovie, Title: "Movie"}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertMediaSource(ctx, catalog.MediaSource{ID: "source", ItemID: "movie", Container: "mp4", Available: true}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertMediaPart(ctx, catalog.MediaPart{ID: "part", SourceID: "source", Path: "C:/definitely-not-readable/movie.mp4", Available: false}); err != nil {
		t.Fatal(err)
	}
	h := NewRouter(deps)
	token := bootstrapToken(t, h)

	for _, id := range []string{"part", "..%2F..%2FWindows%2Fwin.ini"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/media/"+id+"/content", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "MEDIA_UNAVAILABLE") {
			t.Fatalf("id=%q status=%d body=%s", id, rec.Code, rec.Body.String())
		}
	}
}

func bootstrapToken(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := postJSON(t, h, "/api/v1/setup/bootstrap", map[string]string{"bootstrapSecret": "bootstrap-secret", "username": "admin", "password": "password123"}, "")
	return jsonToken(t, rec.Body.Bytes())
}
