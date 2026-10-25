package subtitles

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cleanSRT = "1\n00:00:00,500 --> 00:00:02,000\nI love you.\n\n2\n00:00:02,500 --> 00:00:04,000\nI look happy.\n"

func TestAuditFlagsBothDirectionsWithoutChangingBytes(t *testing.T) {
	body := []byte(strings.ReplaceAll(cleanSRT, "I love you.", "I Iove you.") + "\n3\n00:00:04,200 --> 00:00:05,500\nWe couId Iook.\n")
	original := string(body)
	report, err := AuditSRT(body, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if report.Cues != 3 {
		t.Fatalf("cues %d", report.Cues)
	}
	if report.Status != "needs_review" {
		t.Fatalf("status %s", report.Status)
	}
	if len(report.OCR) < 3 {
		t.Fatalf("missing reverse OCR findings: %#v", report.OCR)
	}
	if string(body) != original {
		t.Fatal("audit modified source")
	}
}
func TestAuditKeepsValidNamesAndPronouns(t *testing.T) {
	body := []byte(strings.ReplaceAll(cleanSRT, "I love you.", "Inigo said I love you. Impossible!"))
	report, err := AuditSRT(body, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.OCR) != 0 || report.Status != "ready" {
		t.Fatalf("false positives: %#v", report)
	}
}
func TestAuditRejectsMalformedAndOutOfRange(t *testing.T) {
	if _, err := AuditSRT([]byte("nonsense"), 10000); err == nil {
		t.Fatal("accepted non-SRT")
	}
	report, err := AuditSRT([]byte("1\n00:00:00,500 --> 00:20:00,000\nA\n"), 10000)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "needs_review" {
		t.Fatalf("out of range: %#v", report)
	}
}
func TestDiscoverAndInstallExclusive(t *testing.T) {
	dir := t.TempDir()
	movie := filepath.Join(dir, "The Princess Bride (1987).m4v")
	if err := os.WriteFile(movie, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "Subtitles")
	if err := os.Mkdir(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(subdir, "The Princess Bride (1987).en.srt")
	if err := os.WriteFile(source, []byte(cleanSRT), 0600); err != nil {
		t.Fatal(err)
	}
	review, err := Discover(movie, 10000, true)
	if err != nil {
		t.Fatal(err)
	}
	if !review.EnglishEmbedded {
		t.Fatal("embedded stream ignored")
	}
	if len(review.Candidates) != 1 || review.Candidates[0].Audit.Status != "ready" {
		t.Fatalf("candidates: %#v", review)
	}
	result, err := Install(movie, 10000, review.Candidates[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(result.Destination)
	if err != nil || string(content) != cleanSRT {
		t.Fatalf("installed content: %v %s", err, content)
	}
	if _, err := Install(movie, 10000, review.Candidates[0].ID); err == nil {
		t.Fatal("existing sidecar overwritten")
	}
}
func TestDiscoverRefusesBadCandidates(t *testing.T) {
	dir := t.TempDir()
	movie := filepath.Join(dir, "Example.mkv")
	if err := os.WriteFile(movie, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Example.fr.srt"), []byte(cleanSRT), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Discover(movie, 10000, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Candidates) != 0 {
		t.Fatalf("foreign language candidate offered: %#v", r.Candidates)
	}
	if _, err := Install(movie, 10000, "../evil"); err == nil {
		t.Fatal("accepted arbitrary path")
	}
}

func TestDiscoverRefusesSymlinkedSubtitleInbox(t *testing.T) {
	for _, inbox := range []string{"Subtitles", "Subs"} {
		t.Run(inbox, func(t *testing.T) {
			mediaDir := t.TempDir()
			outsideDir := t.TempDir()
			video := filepath.Join(mediaDir, "Example.mkv")
			if err := os.WriteFile(video, []byte("media"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outsideDir, "Example.en.srt"), []byte(cleanSRT), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outsideDir, filepath.Join(mediaDir, inbox)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := Discover(video, 10000, false); err == nil {
				t.Fatal("accepted subtitle inbox symlink escaping the media directory")
			}
		})
	}
}

func TestLibraryRootContainsSubtitleAccessAfterSeasonReplacement(t *testing.T) {
	root := t.TempDir()
	season := filepath.Join(root, "Travelers", "Season 01")
	if err := os.MkdirAll(filepath.Join(season, "Subtitles"), 0700); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(season, "Travelers - S01E01.mkv")
	if err := os.WriteFile(video, []byte("original media"), 0600); err != nil {
		t.Fatal(err)
	}
	name := "Travelers - S01E01.en.srt"
	if err := os.WriteFile(filepath.Join(season, "Subtitles", name), []byte(cleanSRT), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(video)
	if err != nil {
		t.Fatal(err)
	}
	read := func() (Review, error) {
		return DiscoverInLibrary(root, video, info.Size(), info.ModTime().UnixNano(), 10000, false)
	}
	review, err := read()
	if err != nil || len(review.Candidates) != 1 {
		t.Fatalf("valid in-library subtitle not discovered: %v %#v", err, review)
	}
	identifier := review.Candidates[0].ID
	installed, err := InstallInLibrary(root, video, info.Size(), info.ModTime().UnixNano(), 10000, identifier)
	if err != nil {
		t.Fatalf("valid in-library subtitle install failed: %v", err)
	}
	content, err := os.ReadFile(installed.Destination)
	if err != nil || string(content) != cleanSRT {
		t.Fatalf("valid in-library install content: %v %q", err, content)
	}

	// Simulate a writable media-share actor replacing an already-scanned
	// season directory with a symlink to a location outside the library.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, filepath.Base(video)), []byte("original media"), 0600); err != nil {
		t.Fatal(err)
	}
	// Give the replacement the cataloged size and timestamp, so rejection
	// must come from root containment rather than a trivial metadata mismatch.
	if err := os.Chtimes(filepath.Join(outside, filepath.Base(video)), info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(outside, "Subtitles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "Subtitles", name), []byte(cleanSRT), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(season, season+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, season); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := read(); err == nil {
		t.Fatal("read through replacement season symlink outside library")
	}
	if _, err := InstallInLibrary(root, video, info.Size(), info.ModTime().UnixNano(), 10000, identifier); err == nil {
		t.Fatal("installed through replacement season symlink outside library")
	}
	if _, err := os.Stat(filepath.Join(outside, "Travelers - S01E01.en.srt")); !os.IsNotExist(err) {
		t.Fatalf("outside-library sidecar was created or not inspectable: %v", err)
	}
}

func TestAuditFindsLowercaseLPronouns(t *testing.T) {
	payload := []byte(strings.ReplaceAll(cleanSRT, "I love you.", "l'm ready. lt is time."))
	audit, err := AuditSRT(payload, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit.OCR) != 2 {
		t.Fatalf("expected both forward OCR mistakes: %+v", audit.OCR)
	}
}
func TestInstallRequiresQualityReviewAndRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	movie := filepath.Join(dir, "Example.mkv")
	if err := os.WriteFile(movie, []byte("media"), 0600); err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(dir, "Subs")
	if err := os.Mkdir(sourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(sourceDir, "Example.en.srt")
	if err := os.WriteFile(candidate, []byte(strings.ReplaceAll(cleanSRT, "I love you.", "aII Iove")), 0600); err != nil {
		t.Fatal(err)
	}
	review, err := Discover(movie, 10000, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Candidates) != 1 || review.Candidates[0].Audit.Status != "needs_review" {
		t.Fatalf("%+v", review)
	}
	if _, err := Install(movie, 10000, review.Candidates[0].ID); err == nil {
		t.Fatal("installed flagged candidate")
	}
	if _, err := os.Stat(filepath.Join(dir, "Example.en.srt")); !os.IsNotExist(err) {
		t.Fatalf("wrote unsafe sidecar: %v", err)
	}
}
func TestUnsupportedEmbeddedKeepsUnverifiedStatus(t *testing.T) {
	reports := InspectEmbedded(context.Background(), "", "ignored.mkv", []EmbeddedTrack{{Index: 2, Codec: "hdmv_pgs_subtitle", Language: "eng"}, {Index: 3, Codec: "subrip", Language: "fra"}}, 10000)
	if len(reports) != 1 || reports[0].Status != "unverified" || reports[0].Reason == "" {
		t.Fatalf("incorrect image subtitle qualification: %+v", reports)
	}
}

func TestDiscoversExistingEnglishAndFlagsOCR(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "The Princess Bride.m4v")
	if err := os.WriteFile(video, []byte("media"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "The Princess Bride.en.srt"), []byte(strings.ReplaceAll(cleanSRT, "I love you.", "I Iove you.")), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Discover(video, 10000, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Coverage != "external_english" || len(r.ExistingAudits) != 1 || r.ExistingAudits[0].Status != "needs_review" {
		t.Fatalf("existing English OCR ignored: %+v", r)
	}
	if len(r.Candidates) != 0 {
		t.Fatal("already installed subtitle proposed as candidate")
	}
}
