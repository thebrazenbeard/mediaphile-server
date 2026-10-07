package stream

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testMediaFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "media.bin")
	if err := os.WriteFile(p, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestServeFileRangesAndHead(t *testing.T) {
	path := testMediaFile(t)
	tests := []struct {
		name, method, rng   string
		wantStatus          int
		wantBody, wantRange string
		checkBody           bool
	}{
		{"full", http.MethodGet, "", http.StatusOK, "0123456789", "", true},
		{"bounded", http.MethodGet, "bytes=2-5", http.StatusPartialContent, "2345", "bytes 2-5/10", true},
		{"open", http.MethodGet, "bytes=7-", http.StatusPartialContent, "789", "bytes 7-9/10", true},
		{"suffix", http.MethodGet, "bytes=-3", http.StatusPartialContent, "789", "bytes 7-9/10", true},
		{"unsatisfiable", http.MethodGet, "bytes=99-100", http.StatusRequestedRangeNotSatisfiable, "", "", false},
		{"head", http.MethodHead, "", http.StatusOK, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/media", nil)
			if tt.rng != "" {
				req.Header.Set("Range", tt.rng)
			}
			rec := httptest.NewRecorder()
			if err := ServeFile(rec, req, path, "application/octet-stream"); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%q", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.checkBody && rec.Body.String() != tt.wantBody {
				t.Fatalf("body=%q want=%q", rec.Body.String(), tt.wantBody)
			}
			if tt.wantRange != "" && rec.Header().Get("Content-Range") != tt.wantRange {
				t.Fatalf("range=%q", rec.Header().Get("Content-Range"))
			}
			if tt.wantStatus != http.StatusRequestedRangeNotSatisfiable && rec.Header().Get("Accept-Ranges") != "bytes" {
				t.Fatalf("accept-ranges=%q", rec.Header().Get("Accept-Ranges"))
			}
		})
	}
}
