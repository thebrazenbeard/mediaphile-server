package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func apiTestRepo(t *testing.T) *catalog.Repository {
	t.Helper()
	db, err := catalog.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return catalog.NewRepository(db)
}

func seedBrowseCatalog(t *testing.T, repo *catalog.Repository) {
	t.Helper()
	ctx := context.Background()
	if err := repo.CreateLibrary(ctx, catalog.Library{ID: "movies", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: "C:/secret/movies", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateLibrary(ctx, catalog.Library{ID: "tv", Name: "TV Shows", MediaType: catalog.LibraryTV, RootPath: "C:/secret/tv", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	y2016, y2017 := 2016, 2017
	if err := repo.UpsertItem(ctx, catalog.Item{ID: "arrival", LibraryID: "movies", Kind: catalog.ItemMovie, Title: "Arrival", Year: &y2016}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertItem(ctx, catalog.Item{ID: "blade", LibraryID: "movies", Kind: catalog.ItemMovie, Title: "Blade Runner 2049", Year: &y2017}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaSource(ctx, catalog.MediaSource{ID: "source-arrival", ItemID: "arrival", Container: "mkv", DurationMS: 1000, VideoCodec: "h264", AudioCodec: "aac", Available: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaPart(ctx, catalog.MediaPart{ID: "part-arrival", SourceID: "source-arrival", Path: "C:/secret/movies/Arrival (2016).mkv", Size: 100, ModTimeNS: 1, Available: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaStream(ctx, catalog.MediaStream{ID: "video-arrival", PartID: "part-arrival", Kind: catalog.StreamVideo, StreamIndex: 0, Codec: "h264", Width: 1920, Height: 1080, Default: true}); err != nil {
		t.Fatal(err)
	}
	show := catalog.Item{ID: "show", LibraryID: "tv", Kind: catalog.ItemShow, Title: "Example Show"}
	seasonNo := 1
	season := catalog.Item{ID: "season", LibraryID: "tv", ParentID: strptr("show"), Kind: catalog.ItemSeason, Title: "Season 1", SeasonNumber: &seasonNo}
	episodeNo := 2
	episode := catalog.Item{ID: "episode", LibraryID: "tv", ParentID: strptr("season"), Kind: catalog.ItemEpisode, Title: "Second", SeasonNumber: &seasonNo, EpisodeNumber: &episodeNo}
	for _, item := range []catalog.Item{show, season, episode} {
		if err := repo.UpsertItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBrowseLibrariesAndItems(t *testing.T) {
	repo := apiTestRepo(t)
	seedBrowseCatalog(t, repo)
	h := NewRouter(Dependencies{Catalog: repo, ServerID: "test-server", ServerName: "Mediaphile Test"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/libraries", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Movies") || strings.Contains(rec.Body.String(), "C:/secret") {
		t.Fatalf("libraries response status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/items?libraryId=movies&kind=movie&q=Arrival&limit=1", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Arrival") || strings.Contains(rec.Body.String(), "C:/secret") {
		t.Fatalf("items response status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBrowseCursorIsOpaqueAndMalformedCursorRejected(t *testing.T) {
	repo := apiTestRepo(t)
	seedBrowseCatalog(t, repo)
	h := NewRouter(Dependencies{Catalog: repo})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/items?libraryId=movies&kind=movie&limit=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"nextCursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == "" || strings.Contains(page.NextCursor, "Arrival") {
		t.Fatalf("unexpected page: %#v", page)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/items?libraryId=movies&cursor=not-valid-base64!", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_CURSOR") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestItemDetailDoesNotLeakFilesystemPath(t *testing.T) {
	repo := apiTestRepo(t)
	seedBrowseCatalog(t, repo)
	h := NewRouter(Dependencies{Catalog: repo})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items/arrival", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "C:/secret") || !strings.Contains(body, "part-arrival") || !strings.Contains(body, "h264") {
		t.Fatalf("detail leaked path or omitted inventory: %s", body)
	}
}

func TestMissingItemReturnsStableError(t *testing.T) {
	repo := apiTestRepo(t)
	h := NewRouter(Dependencies{Catalog: repo})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items/missing", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "NOT_FOUND") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerInfo(t *testing.T) {
	h := NewRouter(Dependencies{ServerID: "server-1", ServerName: "Living Room"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/server", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "server-1") || !strings.Contains(rec.Body.String(), "\"v1\"") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func strptr(s string) *string { return &s }
