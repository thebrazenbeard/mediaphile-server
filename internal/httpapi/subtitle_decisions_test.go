package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func subtitleDecisionFixture(t *testing.T) (Dependencies, string, string) {
	t.Helper()
	deps, _ := authAPIDeps(t)
	media := filepath.Join(t.TempDir(), "Example (2026).mkv")
	if err := os.WriteFile(media, []byte("movie"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := deps.Catalog.CreateLibrary(ctx, catalog.Library{ID: "lib", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: filepath.Dir(media), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertItem(ctx, catalog.Item{ID: "movie", LibraryID: "lib", Kind: catalog.ItemMovie, Title: "Example"}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertMediaSource(ctx, catalog.MediaSource{ID: "source", ItemID: "movie", Container: "mkv", DurationMS: 6000, Available: true}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertMediaPart(ctx, catalog.MediaPart{ID: "part", SourceID: "source", Path: media, Size: 5, Available: true}); err != nil {
		t.Fatal(err)
	}
	api := NewRouter(deps)
	rec := postJSON(t, api, "/api/v1/setup/bootstrap", map[string]string{"bootstrapSecret": "bootstrap-secret", "username": "owner", "password": "secure-password"}, "")
	if rec.Code != 201 {
		t.Fatalf("bootstrap: %d %s", rec.Code, rec.Body.String())
	}
	var response struct{ Token string }
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return deps, response.Token, media
}

func subtitleDecisionRequest(t *testing.T, h http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSubtitleDecisionsPersistAndDoNotClaimCoverage(t *testing.T) {
	deps, token, _ := subtitleDecisionFixture(t)
	h := NewRouter(deps)
	path := "/api/v1/subtitles/parts/part/decision"
	initial := subtitleDecisionRequest(t, h, "GET", path, nil, token)
	if initial.Code != 200 || !strings.Contains(initial.Body.String(), `"disposition":"active"`) {
		t.Fatalf("initial disposition: %d %s", initial.Code, initial.Body.String())
	}
	set := subtitleDecisionRequest(t, h, "PUT", path, map[string]string{"disposition": "skip", "reason": "instrumental intro"}, token)
	if set.Code != 200 || !strings.Contains(set.Body.String(), `"disposition":"skip"`) {
		t.Fatalf("save: %d %s", set.Code, set.Body.String())
	}
	reread := subtitleDecisionRequest(t, NewRouter(deps), "GET", path, nil, token)
	if reread.Code != 200 || !strings.Contains(reread.Body.String(), `"reason":"instrumental intro"`) {
		t.Fatalf("reread: %d %s", reread.Code, reread.Body.String())
	}
	review := subtitleDecisionRequest(t, h, "GET", "/api/v1/subtitles/parts/part", nil, token)
	if review.Code != 200 || !strings.Contains(review.Body.String(), `"disposition":"skip"`) || !strings.Contains(review.Body.String(), `"coverage":"missing"`) {
		t.Fatalf("skip must not become coverage: %d %s", review.Code, review.Body.String())
	}
	reset := subtitleDecisionRequest(t, h, "PUT", path, map[string]string{"disposition": "active", "reason": ""}, token)
	if reset.Code != 200 || !strings.Contains(reset.Body.String(), `"disposition":"active"`) {
		t.Fatalf("restore: %d %s", reset.Code, reset.Body.String())
	}
}

func TestSubtitleDecisionValidationAndPermissions(t *testing.T) {
	deps, token, _ := subtitleDecisionFixture(t)
	h := NewRouter(deps)
	path := "/api/v1/subtitles/parts/part/decision"
	for _, item := range []map[string]string{
		{"disposition": "covered", "reason": "not real"},
		{"disposition": "skip", "reason": strings.Repeat("x", 400)},
		{"disposition": "skip", "reason": ""},
	} {
		r := subtitleDecisionRequest(t, h, "PUT", path, item, token)
		if r.Code != 400 {
			t.Errorf("invalid %+v gave %d: %s", item, r.Code, r.Body.String())
		}
	}
	denied := subtitleDecisionRequest(t, h, "PUT", path, map[string]string{"disposition": "skip", "reason": "no"}, "")
	if denied.Code != 401 {
		t.Fatalf("without auth %d", denied.Code)
	}
	missing := subtitleDecisionRequest(t, h, "PUT", "/api/v1/subtitles/parts/nope/decision", map[string]string{"disposition": "skip", "reason": "test"}, token)
	if missing.Code != 404 {
		t.Fatalf("unknown part %d: %s", missing.Code, missing.Body.String())
	}
}

func TestImportSubtitleExclusionsAtomicAndIdempotent(t *testing.T) {
	deps, token, video := subtitleDecisionFixture(t)
	h := NewRouter(deps)
	uri := "/api/v1/subtitles/decisions/import"
	bad := subtitleDecisionRequest(t, h, "POST", uri, map[string]any{"exclusions": []map[string]string{
		{"video": video, "reason": "instrumental intro"},
		{"video": filepath.Join(t.TempDir(), "missing.mp4"), "reason": "should rollback"},
	}}, token)
	if bad.Code != 409 {
		t.Fatalf("unknown file must fail atomically: %d %s", bad.Code, bad.Body.String())
	}
	get := subtitleDecisionRequest(t, h, "GET", "/api/v1/subtitles/parts/part/decision", nil, token)
	if !strings.Contains(get.Body.String(), `"disposition":"active"`) {
		t.Fatalf("partial import: %s", get.Body.String())
	}
	ok := subtitleDecisionRequest(t, h, "POST", uri, map[string]any{"exclusions": []map[string]string{{"video": video, "reason": "instrumental intro"}}}, token)
	if ok.Code != 200 || !strings.Contains(ok.Body.String(), `"imported":1`) {
		t.Fatalf("import failed %d %s", ok.Code, ok.Body.String())
	}
	again := subtitleDecisionRequest(t, h, "POST", uri, map[string]any{"exclusions": []map[string]string{{"video": video, "reason": "import again"}}}, token)
	if again.Code != 200 || !strings.Contains(again.Body.String(), `"imported":0`) {
		t.Fatalf("idempotent import %d %s", again.Code, again.Body.String())
	}
	actual := subtitleDecisionRequest(t, h, "GET", "/api/v1/subtitles/parts/part/decision", nil, token)
	if !strings.Contains(actual.Body.String(), `"reason":"instrumental intro"`) {
		t.Fatalf("import overwrote decision %s", actual.Body.String())
	}
	unauth := subtitleDecisionRequest(t, h, "POST", uri, map[string]any{"exclusions": []any{}}, "")
	if unauth.Code != 401 {
		t.Fatalf("unauth import returned %d", unauth.Code)
	}
}
