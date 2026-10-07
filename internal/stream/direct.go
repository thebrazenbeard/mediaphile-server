package stream

import (
	"net/http"
	"os"
	"path/filepath"
)

func ServeFile(w http.ResponseWriter, r *http.Request, path, contentType string) error {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, "media", info.ModTime(), f)
	return nil
}
