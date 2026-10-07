package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/auth"
	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func TestWatchStateFiltersArePrincipalScoped(t *testing.T) {
	deps, _ := authAPIDeps(t)
	ctx := context.Background()
	if err := deps.Catalog.CreateLibrary(ctx, catalog.Library{ID: "film", Name: "Films", MediaType: catalog.LibraryMovies, RootPath: "C:/private/media", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ id, title string }{{"a", "Alpha"}, {"b", "Beta"}, {"c", "Charlie"}, {"d", "Delta"}} {
		if err := deps.Catalog.UpsertItem(ctx, catalog.Item{ID: v.id, LibraryID: "film", Kind: catalog.ItemMovie, Title: v.title}); err != nil {
			t.Fatal(err)
		}
	}
	h := NewRouter(deps)
	ownerToken := bootstrapToken(t, h)
	owner, err := deps.Auth.Authenticate(ctx, ownerToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.PutPlaybackState(ctx, catalog.PlaybackState{PrincipalID: owner.ID, ItemID: "b", ResumeMS: 500}); err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.PutPlaybackState(ctx, catalog.PlaybackState{PrincipalID: owner.ID, ItemID: "c", PlayCount: 1, Completed: true}); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("viewer-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.CreatePrincipal(ctx, catalog.Principal{ID: "viewer", Username: "viewer", PasswordHash: hash}); err != nil {
		t.Fatal(err)
	}
	_, viewerToken, err := deps.Auth.Login(ctx, "viewer", "viewer-password")
	if err != nil {
		t.Fatal(err)
	}

	check := func(name, token, state string, want, absent []string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/items?libraryId=film&kind=movie&watchState="+state, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s status=%d body=%s", name, rec.Code, rec.Body.String())
		}
		for _, id := range want {
			if !strings.Contains(rec.Body.String(), `"id":"`+id+`"`) {
				t.Errorf("%s missing %s: %s", name, id, rec.Body.String())
			}
		}
		for _, id := range absent {
			if strings.Contains(rec.Body.String(), `"id":"`+id+`"`) {
				t.Errorf("%s leaked %s: %s", name, id, rec.Body.String())
			}
		}
	}
	check("owner in-progress", ownerToken, "in_progress", []string{"b"}, []string{"a", "c", "d"})
	check("owner watched", ownerToken, "watched", []string{"c"}, []string{"a", "b", "d"})
	check("owner unplayed", ownerToken, "unplayed", []string{"a", "d"}, []string{"b", "c"})
	check("other user unplayed", viewerToken, "unplayed", []string{"a", "b", "c", "d"}, nil)
	check("all", ownerToken, "all", []string{"a", "b", "c", "d"}, nil)
}

func TestWatchStateRejectsInvalidInputAndUnauthenticated(t *testing.T) {
	deps, _ := authAPIDeps(t)
	h := NewRouter(deps)
	for _, tc := range []struct {
		state, token string
		status       int
	}{{"invalid", "", http.StatusUnauthorized}, {"invalid", "bootstrap", http.StatusUnauthorized}} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/items?watchState="+tc.state, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("status=%d want=%d", rec.Code, tc.status)
		}
	}
	token := bootstrapToken(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items?watchState=invalid", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_WATCH_STATE") {
		t.Fatalf("invalid state status=%d body=%s", rec.Code, rec.Body.String())
	}
}
