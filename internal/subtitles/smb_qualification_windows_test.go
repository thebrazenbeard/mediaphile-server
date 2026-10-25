//go:build windows

package subtitles

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This opt-in fixture writes only to a freshly created temporary directory
// under the explicitly configured SMB share, not to existing Plex media.
// It verifies behavior for the current Windows account; it does not prove
// effective permissions of any other local, SMB or NAS account.
func TestSMBQualificationSyntheticLifecycle(t *testing.T) {
	share := os.Getenv("MEDIAPHILE_QUALIFY_SMB_ROOT")
	if share == "" {
		t.Skip("set MEDIAPHILE_QUALIFY_SMB_ROOT to an authorized writable share root")
	}
	if !filepath.IsAbs(share) {
		t.Fatal("SMB fixture root must be absolute")
	}
	base, err := os.MkdirTemp(share, ".mediaphile-security-qualification-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("synthetic SMB fixture cleanup: %v", err)
		}
	})
	library := filepath.Join(base, "media")
	stage := filepath.Join(base, "private-stage")
	for _, dir := range []string{library, stage} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	video := filepath.Join(library, "qualify.mkv")
	if err := os.WriteFile(video, []byte(strings.Repeat("synthetic-non-media", 300)), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(video)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &http.Client{Transport: subtitleRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return publishedResponse(200, `{"link":"https://www.opensubtitles.com/download/signed/fixture.srt"}`), nil
		}
		if calls == 2 {
			return publishedResponse(200, validPublishedSRT), nil
		}
		return nil, io.EOF
	})}
	result, err := StageOpenSubtitlesInLibrary(context.Background(), client, "synthetic-key", "synthetic-token", 321,
		"qualify-part", library, video, stage, info.Size(), info.ModTime().UnixNano(), 6000)
	if err != nil {
		t.Fatalf("SMB stage failed: %v", err)
	}
	if calls != 2 || !result.ReadyForReview {
		t.Fatalf("SMB stage unexpected: calls=%d result=%+v", calls, result)
	}
	if _, err := ReviewPublishedStageInLibrary(library, stage, "qualify-part", video,
		info.Size(), info.ModTime().UnixNano(), 6000, result.StageID); err != nil {
		t.Fatalf("SMB stage review: %v", err)
	}
	installed, err := InstallPublishedStageInLibrary(library, stage, "qualify-part", video,
		info.Size(), info.ModTime().UnixNano(), 6000, result.StageID, result.SHA256)
	if err != nil {
		t.Fatalf("SMB hardlink no-overwrite publishing unsupported: %v", err)
	}
	if installed.AlreadyInstalled {
		t.Fatal("new SMB synthetic sidecar unexpectedly already present")
	}
	if _, err := InstallPublishedStageInLibrary(library, stage, "qualify-part", video,
		info.Size(), info.ModTime().UnixNano(), 6000, result.StageID, result.SHA256); err != nil {
		t.Fatalf("SMB idempotent atomic publication failed: %v", err)
	}
	media, err := os.ReadFile(video)
	if err != nil || string(media) != strings.Repeat("synthetic-non-media", 300) {
		t.Fatalf("SMB synthetic media changed: %v", err)
	}
	t.Log("SMB functional path passed for current principal only; DACL and cross-principal denial require separate qualification")
}
