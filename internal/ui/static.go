package ui

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type staticHandler struct{ root string }

func NewStatic(root string) http.Handler {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = filepath.Clean(root)
	}
	return &staticHandler{root: abs}
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	requestPath := strings.TrimPrefix(r.URL.Path, "/")
	requestPath = filepath.Clean(filepath.FromSlash(requestPath))
	if requestPath == "." {
		requestPath = "index.html"
	}
	if requestPath == ".." || strings.HasPrefix(requestPath, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	target := filepath.Join(h.root, requestPath)
	rel, err := filepath.Rel(h.root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		http.ServeFile(w, r, target)
		return
	}
	index := filepath.Join(h.root, "index.html")
	if info, err := os.Stat(index); err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, index)
}
