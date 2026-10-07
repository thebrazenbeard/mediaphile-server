package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func TestMediaCookieAllowsNativeVideoButNotAPI(t *testing.T) {
	deps, _ := authAPIDeps(t)
	path := filepath.Join(t.TempDir(), "media.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, err := range []error{
		deps.Catalog.CreateLibrary(ctx, catalog.Library{ID: "movies", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: filepath.Dir(path), Enabled: true}),
		deps.Catalog.UpsertItem(ctx, catalog.Item{ID: "m", LibraryID: "movies", Kind: catalog.ItemMovie, Title: "Media"}),
		deps.Catalog.UpsertMediaSource(ctx, catalog.MediaSource{ID: "s", ItemID: "m", Container: "mp4", Available: true}),
		deps.Catalog.UpsertMediaPart(ctx, catalog.MediaPart{ID: "p", SourceID: "s", Path: path, Size: 10, Available: true}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	h := NewRouter(deps)
	login := postJSON(t, h, "/api/v1/setup/bootstrap", map[string]string{
		"bootstrapSecret": "bootstrap-secret", "username": "admin", "password": "pass12345",
	}, "")
	if login.Code != http.StatusCreated {
		t.Fatalf("bootstrap=%d", login.Code)
	}
	var mediaCookie *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == mediaCookieName {
			mediaCookie = c
		}
	}
	if mediaCookie == nil || !mediaCookie.HttpOnly || mediaCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("missing secure media cookie attributes: %#v", mediaCookie)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/p/content", nil)
	req.AddCookie(mediaCookie)
	req.Header.Set("Range", "bytes=2-4")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "234" {
		t.Fatalf("native browser media failed: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/libraries", nil)
	req.AddCookie(mediaCookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("cookie elevated to API auth: %d", rec.Code)
	}
}
