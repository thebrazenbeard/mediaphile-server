package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticHandlerServesLocalAssetsAndSPAWithoutExternalRuntimeURLs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	index := `<!doctype html><html><body><div id="root"></div><script type="module" src="/assets/app.js"></script></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('local')"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewStatic(dir)
	for _, path := range []string{"/", "/movies/item-1"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "id=\"root\"") {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "http://") || strings.Contains(rec.Body.String(), "https://") {
			t.Fatalf("external runtime URL in %s", rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log('local')" {
		t.Fatalf("asset status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStaticHandlerRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("index"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewStatic(dir)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/..%2Fsecret", nil))
	if rec.Code == http.StatusOK && rec.Body.String() != "index" {
		t.Fatalf("unexpected traversal response: %s", rec.Body.String())
	}
}
