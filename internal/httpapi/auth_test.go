package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/auth"
	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func authAPIDeps(t *testing.T) (Dependencies, *auth.Service) {
	t.Helper()
	db, err := catalog.Open(filepath.Join(t.TempDir(), "http-auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := catalog.NewRepository(db)
	svc := auth.NewWithBootstrap(repo, "bootstrap-secret")
	return Dependencies{Catalog: repo, Auth: svc}, svc
}

func postJSON(t *testing.T, h http.Handler, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBootstrapProtectsBrowseAndLogoutRevokes(t *testing.T) {
	deps, _ := authAPIDeps(t)
	h := NewRouter(deps)
	bad := postJSON(t, h, "/api/v1/setup/bootstrap", map[string]string{"bootstrapSecret": "wrong", "username": "patrick", "password": "password123"}, "")
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad bootstrap status=%d body=%s", bad.Code, bad.Body.String())
	}

	rec := postJSON(t, h, "/api/v1/setup/bootstrap", map[string]string{"bootstrapSecret": "bootstrap-secret", "username": "patrick", "password": "password123"}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Token == "" {
		t.Fatal("missing token")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/libraries", nil)
	noauth := httptest.NewRecorder()
	h.ServeHTTP(noauth, req)
	if noauth.Code != http.StatusUnauthorized {
		t.Fatalf("browse without auth=%d", noauth.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/libraries", nil)
	req.Header.Set("Authorization", "Bearer "+created.Token)
	ok := httptest.NewRecorder()
	h.ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("browse with auth=%d body=%s", ok.Code, ok.Body.String())
	}

	logout := postJSON(t, h, "/api/v1/auth/logout", map[string]string{}, created.Token)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout=%d body=%s", logout.Code, logout.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/libraries", nil)
	req.Header.Set("Authorization", "Bearer "+created.Token)
	revoked := httptest.NewRecorder()
	h.ServeHTTP(revoked, req)
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token browse=%d", revoked.Code)
	}
}

func TestNonAdminCannotCreateLibrary(t *testing.T) {
	deps, svc := authAPIDeps(t)
	hash, err := auth.HashPassword("user-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.CreatePrincipal(context.Background(), catalog.Principal{ID: "user-1", Username: "viewer", PasswordHash: hash, Admin: false}); err != nil {
		t.Fatal(err)
	}
	_, token, err := svc.Login(context.Background(), "viewer", "user-password")
	if err != nil {
		t.Fatal(err)
	}
	h := NewRouter(deps)
	rec := postJSON(t, h, "/api/v1/libraries", map[string]any{"id": "more", "name": "More", "mediaType": "movies", "rootPath": "C:/Media"}, token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
