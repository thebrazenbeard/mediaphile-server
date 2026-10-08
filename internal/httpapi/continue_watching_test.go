package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/auth"
	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func TestContinueWatchingIsPrivateOrderedAndPlayable(t *testing.T) {
	db, err := catalog.Open(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := catalog.NewRepository(db)
	ctx := context.Background()
	if err := repo.CreateLibrary(ctx, catalog.Library{ID: "movies", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: "C:/media", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ id, title string }{{"first", "First"}, {"second", "Second"}, {"finished", "Finished"}, {"missing", "Missing"}} {
		if err := repo.UpsertItem(ctx, catalog.Item{ID: v.id, LibraryID: "movies", Kind: catalog.ItemMovie, Title: v.title}); err != nil {
			t.Fatal(err)
		}
		if v.id != "missing" {
			if err := repo.UpsertMediaSource(ctx, catalog.MediaSource{ID: "src-" + v.id, ItemID: v.id, Container: "mp4", Available: true}); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpsertMediaPart(ctx, catalog.MediaPart{ID: "part-" + v.id, SourceID: "src-" + v.id, Path: "C:/private/" + v.id + ".mp4", Size: 100, Available: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
	hash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"alice", "bob"} {
		if err := repo.CreatePrincipal(ctx, catalog.Principal{ID: u, Username: u, PasswordHash: hash, Admin: false}); err != nil {
			t.Fatal(err)
		}
	}
	t1, t2 := "2026-10-06 09:00:00", "2026-10-07 09:00:00"
	for _, v := range []catalog.PlaybackState{
		{PrincipalID: "alice", ItemID: "first", ResumeMS: 1000, LastPlayedAt: &t1},
		{PrincipalID: "alice", ItemID: "second", ResumeMS: 2000, LastPlayedAt: &t2},
		{PrincipalID: "alice", ItemID: "finished", ResumeMS: 9000, Completed: true, LastPlayedAt: &t2},
		{PrincipalID: "alice", ItemID: "missing", ResumeMS: 1000, LastPlayedAt: &t2},
		{PrincipalID: "bob", ItemID: "finished", ResumeMS: 500, LastPlayedAt: &t2},
	} {
		if err := repo.PutPlaybackState(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	svc := auth.NewWithBootstrap(repo, "secret")
	h := NewRouter(Dependencies{Catalog: repo, Auth: svc})
	_, aliceToken, err := svc.Login(ctx, "alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	_, bobToken, err := svc.Login(ctx, "bob", "password123")
	if err != nil {
		t.Fatal(err)
	}
	type entry struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
		ResumeMS int64 `json:"resumeMs"`
	}
	fetch := func(token, limit string) (int, []entry, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/continue-watching?limit="+limit, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var result struct {
			Items []entry `json:"items"`
		}
		if rec.Code == 200 && json.Unmarshal(rec.Body.Bytes(), &result) != nil {
			t.Fatalf("invalid response %s", rec.Body.String())
		}
		return rec.Code, result.Items, rec.Body.String()
	}
	status, entries, body := fetch(aliceToken, "2")
	if status != 200 || len(entries) != 2 || entries[0].Item.ID != "second" || entries[1].Item.ID != "first" {
		t.Fatalf("alice status=%d entries=%+v body=%s", status, entries, body)
	}
	if entries[0].ResumeMS != 2000 {
		t.Fatalf("resume=%d", entries[0].ResumeMS)
	}
	if containsSecretPath(body) {
		t.Fatalf("filesystem path leaked: %s", body)
	}
	status, entries, body = fetch(bobToken, "10")
	if status != 200 || len(entries) != 1 || entries[0].Item.ID != "finished" {
		t.Fatalf("bob status=%d entries=%+v body=%s", status, entries, body)
	}
	status, _, _ = fetch("", "10")
	if status != 401 {
		t.Fatalf("anonymous status=%d", status)
	}
	status, _, _ = fetch(aliceToken, "1000")
	if status != 400 {
		t.Fatalf("invalid limit status=%d", status)
	}
}

func containsSecretPath(value string) bool {
	return strings.Contains(value, "C:/private") || strings.Contains(value, "C:\\\\private")
}
