package subtitles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func promotionFixture(t *testing.T) (video, stageRoot, stageID, digest string) {
	t.Helper()
	video, stageRoot = publishedStageFixture(t)
	stageID = strings.Repeat("a", 32)
	folder := filepath.Join(stageRoot, "published-subtitle-"+stageID)
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	payload := []byte(validPublishedSRT)
	sum := sha256.Sum256(payload)
	digest = hex.EncodeToString(sum[:])
	mediaInfo, err := os.Stat(video)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := fingerprintMediaPath(video)
	if err != nil {
		t.Fatal(err)
	}
	manifest := publishedStageManifest{Version: 1, PartID: "part-1", Video: filepath.Clean(video),
		VideoSize: mediaInfo.Size(), VideoModTimeNano: mediaInfo.ModTime().UnixNano(), DurationMS: 6000,
		VideoFingerprint: fingerprint,
		Provider: "opensubtitles", FileID: 123, SHA256: digest, CreatedAtUnix: time.Now().Unix()}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "manifest.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "candidate.en.srt"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	return
}

func TestPublishedStageRequiresBoundIdentityAndManualInstall(t *testing.T) {
	t.Setenv("MEDIAPHILE_SUBTITLE_PUBLISH_MODE", "")
	video, stage, stageID, digest := promotionFixture(t)
	for _, tc := range []struct{ part, id string }{
		{"wrong-part", stageID}, {"part-1", "../" + stageID}, {"part-1", strings.Repeat("f", 32)},
	} {
		if _, err := ReviewPublishedStage(stage, tc.part, video, 6000, tc.id); err == nil {
			t.Fatalf("unbound staged reference accepted: %+v", tc)
		}
	}
	review, err := ReviewPublishedStage(stage, "part-1", video, 6000, stageID)
	if err != nil || review.Text != validPublishedSRT || review.SHA256 != digest || review.Audit.Status != "ready" || review.Published {
		t.Fatalf("staged review mismatch: %+v %v", review, err)
	}
	destination := filepath.Join(filepath.Dir(video), "movie.en.srt")
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("review unexpectedly installed sidecar: %v", err)
	}
	if _, err := InstallPublishedStage(stage, "part-1", video, 6000, stageID, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong expected digest installed")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("bad digest installed sidecar: %v", err)
	}
	first, err := InstallPublishedStage(stage, "part-1", video, 6000, stageID, digest)
	if err != nil || first.AlreadyInstalled || first.DestinationName != "movie.en.srt" || first.SHA256 != digest {
		t.Fatalf("first manual install: %+v %v", first, err)
	}
	again, err := InstallPublishedStage(stage, "part-1", video, 6000, stageID, digest)
	if err != nil || !again.AlreadyInstalled || again.Source != "existing_identical_unattributed" {
		t.Fatalf("idempotent retry: %+v %v", again, err)
	}
	actual, err := os.ReadFile(destination)
	if err != nil || string(actual) != validPublishedSRT {
		t.Fatalf("installed bytes: %q %v", actual, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destination)
		if err != nil || info.Mode().Perm() != 0644 {
			t.Fatalf("published sidecar must be readable by playback services: mode=%v err=%v", info, err)
		}
	}
	ents, err := os.ReadDir(filepath.Dir(video))
	if err != nil || len(ents) != 2 {
		t.Fatalf("temporary publication debris: %v %v", ents, err)
	}
}

func TestPublishedStageInLibraryRejectsSiblingStagingDirectory(t *testing.T) {
	video, stage, stageID, digest := promotionFixture(t)
	info, err := os.Stat(video)
	if err != nil {
		t.Fatal(err)
	}
	// Both paths are under the cataloged root, but the staging directory is
	// not under this video's immediate parent. The old parent-only check let
	// the stage be read and published into the library.
	library := filepath.Dir(filepath.Dir(video))
	if _, err := ReviewPublishedStageInLibrary(library, stage, "part-1", video,
		info.Size(), info.ModTime().UnixNano(), 6000, stageID); err == nil {
		t.Fatal("inside-library staged text was disclosed")
	}
	if _, err := InstallPublishedStageInLibrary(library, stage, "part-1", video,
		info.Size(), info.ModTime().UnixNano(), 6000, stageID, digest); err == nil {
		t.Fatal("inside-library staged candidate was published")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(video), "movie.en.srt")); !os.IsNotExist(err) {
		t.Fatalf("rejected stage affected media: %v", err)
	}
	// The legitimate configuration with staging outside the library remains
	// reviewable without installing anything.
	validLibrary := filepath.Dir(video)
	if review, err := ReviewPublishedStageInLibrary(validLibrary, stage, "part-1", video,
		info.Size(), info.ModTime().UnixNano(), 6000, stageID); err != nil || review.SHA256 != digest {
		t.Fatalf("outside-library stage unexpectedly rejected: %+v %v", review, err)
	}
}

func TestPublishedStagePublicationModePolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode string
		want os.FileMode
		allowed bool
	}{
		{name: "group-readable", mode: "0640", want: 0640, allowed: true},
		{name: "private", mode: "0600", want: 0600, allowed: true},
		{name: "world-writable-rejected", mode: "0666", allowed: false},
		{name: "executable-rejected", mode: "0755", allowed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MEDIAPHILE_SUBTITLE_PUBLISH_MODE", tc.mode)
			video, stage, stageID, digest := promotionFixture(t)
			_, err := InstallPublishedStage(stage, "part-1", video, 6000, stageID, digest)
			if tc.allowed && err != nil {
				t.Fatalf("allowed publication mode rejected: %v", err)
			}
			if !tc.allowed && err == nil {
				t.Fatal("unsafe publication mode accepted")
			}
			destination := filepath.Join(filepath.Dir(video), "movie.en.srt")
			info, statErr := os.Stat(destination)
			if !tc.allowed {
				if !os.IsNotExist(statErr) {
					t.Fatalf("unsafe publication mode had filesystem effect: %v", statErr)
				}
				return
			}
			if statErr != nil {
				t.Fatal(statErr)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != tc.want {
				t.Fatalf("publication mode=%04o, want %04o", info.Mode().Perm(), tc.want)
			}
		})
	}
}

func TestPublishedStageRejectsTamperStaleAndSymlinks(t *testing.T) {
	video, stage, stageID, digest := promotionFixture(t)
	candidate := filepath.Join(stage, "published-subtitle-"+stageID, "candidate.en.srt")
	if err := os.WriteFile(candidate, []byte(strings.ReplaceAll(validPublishedSRT, "Hello", "Hallo")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPublishedStage(stage, "part-1", video, 6000, stageID, digest); err == nil {
		t.Fatal("tampered staged bytes accepted")
	}
	if err := os.Remove(candidate); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(video, candidate); err == nil {
		if _, err := ReviewPublishedStage(stage, "part-1", video, 6000, stageID); err == nil {
			t.Fatal("symlink candidate accepted")
		}
	}
	otherVideo, otherStage, otherID, _ := promotionFixture(t)
	if _, err := ReviewPublishedStage(otherStage, "part-1", otherVideo, 6000, otherID); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(otherStage, "published-subtitle-"+otherID, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest publishedStageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.CreatedAtUnix = time.Now().Add(-31 * 24 * time.Hour).Unix()
	data, _ = json.Marshal(manifest)
	if err := os.WriteFile(manifestPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReviewPublishedStage(otherStage, "part-1", otherVideo, 6000, otherID); err == nil {
		t.Fatal("expired stage accepted")
	}
}

func TestPublishedStageNeverOverwritesDifferentSidecarOrChangedMedia(t *testing.T) {
	video, stage, stageID, digest := promotionFixture(t)
	destination := filepath.Join(filepath.Dir(video), "movie.en.srt")
	if err := os.WriteFile(destination, []byte("pre-existing subtitles"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPublishedStage(stage, "part-1", video, 6000, stageID, digest); err == nil {
		t.Fatal("overwrote an existing sidecar")
	}
	data, _ := os.ReadFile(destination)
	if string(data) != "pre-existing subtitles" {
		t.Fatalf("preexisting file changed: %q", data)
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video, []byte("different media contents"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPublishedStage(stage, "part-1", video, 6000, stageID, digest); err == nil {
		t.Fatal("changed media accepted")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("created sidecar for changed media: %v", err)
	}
}

func TestPublishedStageConcurrentManualInstallIsAtomicAndDuplicateSafe(t *testing.T) {
	video, stage, stageID, digest := promotionFixture(t)
	var wait sync.WaitGroup
	type outcome struct { installed PublishedInstall; err error }
	outcomes := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			installed, err := InstallPublishedStage(stage, "part-1", video, 6000, stageID, digest)
			outcomes <- outcome{installed: installed, err: err}
		}()
	}
	wait.Wait()
	close(outcomes)
	fresh, existing := 0, 0
	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("concurrent install error: %v", outcome.err)
		}
		if outcome.installed.AlreadyInstalled {
			existing++
		} else {
			fresh++
		}
	}
	if fresh != 1 || existing != 1 {
		t.Fatalf("expected one atomic publish and one idempotent observation; got new=%d existing=%d", fresh, existing)
	}
	payload, err := os.ReadFile(filepath.Join(filepath.Dir(video), "movie.en.srt"))
	if err != nil || string(payload) != validPublishedSRT {
		t.Fatalf("published sidecar invalid: %q %v", payload, err)
	}
	entries, err := os.ReadDir(filepath.Dir(video))
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary artifacts remain: %v %v", entries, err)
	}
}

func TestPublishedStageRejectsSameSizeRestoredMtimeMediaReplacement(t *testing.T) {
	video, stage, stageID, digest := promotionFixture(t)
	before, err := os.Stat(video)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video, []byte(strings.Repeat("X", len(old))), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(video, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	updated, err := os.Stat(video)
	if err != nil || updated.Size() != before.Size() || updated.ModTime().UnixNano() != before.ModTime().UnixNano() {
		t.Fatalf("test precondition not met: %v", err)
	}
	if _, err := InstallPublishedStage(stage, "part-1", video, 6000, stageID, digest); err == nil {
		t.Fatal("same-size/restored-mtime content replacement bypassed fingerprint")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(video), "movie.en.srt")); !os.IsNotExist(err) {
		t.Fatalf("published to changed media: %v", err)
	}
}

func TestPublishedStageLibraryRootRejectsReplacedSeriesAncestor(t *testing.T) {
	t.Setenv("MEDIAPHILE_SUBTITLE_PUBLISH_MODE", "")
	video, stage, stageID, digest := promotionFixture(t)
	libraryRoot := filepath.Dir(video)
	series := filepath.Join(libraryRoot, "Travelers")
	if err := os.Mkdir(series, 0700); err != nil {
		t.Fatal(err)
	}
	season := filepath.Join(series, "Season 01")
	if err := os.Mkdir(season, 0700); err != nil {
		t.Fatal(err)
	}
	nestedVideo := filepath.Join(season, filepath.Base(video))
	if err := os.Rename(video, nestedVideo); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(stage, "published-subtitle-"+stageID, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest publishedStageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Video = filepath.Clean(nestedVideo)
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(nestedVideo)
	if err != nil {
		t.Fatal(err)
	}
	review := func() (PublishedReview, error) {
		return ReviewPublishedStageInLibrary(libraryRoot, stage, "part-1", nestedVideo,
			info.Size(), info.ModTime().UnixNano(), 6000, stageID)
	}
	install := func() (PublishedInstall, error) {
		return InstallPublishedStageInLibrary(libraryRoot, stage, "part-1", nestedVideo,
			info.Size(), info.ModTime().UnixNano(), 6000, stageID, digest)
	}
	if got, err := review(); err != nil || got.SHA256 != digest {
		t.Fatalf("in-root stage review: %+v %v", got, err)
	}
	if got, err := install(); err != nil || got.AlreadyInstalled {
		t.Fatalf("in-root atomic publish: %+v %v", got, err)
	}

	// Replace an ancestor ABOVE the immediate Season 01 media parent.
	// The immediate parent remains a real directory, so an Lstat of only
	// filepath.Dir(video) is insufficient to detect the escape.
	outside := t.TempDir()
	outsideSeason := filepath.Join(outside, "Season 01")
	if err := os.Mkdir(outsideSeason, 0700); err != nil {
		t.Fatal(err)
	}
	videoBytes, err := os.ReadFile(nestedVideo)
	if err != nil {
		t.Fatal(err)
	}
	outsideVideo := filepath.Join(outsideSeason, filepath.Base(video))
	if err := os.WriteFile(outsideVideo, videoBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(outsideVideo, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(series, series+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, series); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := review(); err == nil {
		t.Fatal("previewed staged subtitle through outside-library ancestor")
	}
	if _, err := install(); err == nil {
		t.Fatal("published staged subtitle through outside-library ancestor")
	}
	if _, err := os.Stat(filepath.Join(outsideSeason, "movie.en.srt")); !os.IsNotExist(err) {
		t.Fatalf("outside-library staged sidecar exists: %v", err)
	}
}
