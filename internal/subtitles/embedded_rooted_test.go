package subtitles

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The fake extractor must receive a pinned media handle on stdin. It rejects
// paths and verifies the bytes independently of the subtitle implementation.
const embeddedTestExtractor = `package main
import (
	"fmt"
	"io"
	"os"
)
func main() {
	pathIsPipe := false
	for i, arg := range os.Args {
		if arg == "-i" && i+1 < len(os.Args) && os.Args[i+1] == "pipe:0" {
			pathIsPipe = true
		}
	}
	if !pathIsPipe {
		os.Exit(31)
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil || string(data) != "pinned video contents" {
		os.Exit(32)
	}
	fmt.Print("1\n00:00:00,500 --> 00:00:02,000\nI love you.\n\n2\n00:00:02,500 --> 00:00:04,000\nI look happy.\n")
}
`

func buildEmbeddedTestExtractor(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "extractor.go")
	if err := os.WriteFile(source, []byte(embeddedTestExtractor), 0600); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "extractor")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	command := exec.Command("go", "build", "-o", program, source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fake extractor: %v: %s", err, output)
	}
	return program
}

func embeddedTestMedia(t *testing.T) (root, season, video string, size, modTimeNS int64) {
	t.Helper()
	root = t.TempDir()
	season = filepath.Join(root, "Travelers", "Season 01")
	if err := os.MkdirAll(season, 0700); err != nil {
		t.Fatal(err)
	}
	video = filepath.Join(season, "Travelers - S01E01.mkv")
	if err := os.WriteFile(video, []byte("pinned video contents"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(video)
	if err != nil {
		t.Fatal(err)
	}
	return root, season, video, info.Size(), info.ModTime().UnixNano()
}

func TestInspectEmbeddedInLibraryPipesPinnedMediaForEachTrack(t *testing.T) {
	root, _, video, size, modTimeNS := embeddedTestMedia(t)
	extractor := buildEmbeddedTestExtractor(t)
	tracks := []EmbeddedTrack{
		{Index: 0, Codec: "subrip", Language: "eng"},
		{Index: 1, Codec: "mov_text", Language: "en"},
	}
	reports := InspectEmbeddedInLibrary(context.Background(), extractor, root, video, size, modTimeNS, tracks, 10000)
	if len(reports) != 2 || reports[0].Status != "ready" || reports[1].Status != "ready" {
		t.Fatalf("media handle not correctly pinned and rewound: %+v", reports)
	}
}

func TestInspectEmbeddedInLibraryRefusesReplacedSeasonAncestor(t *testing.T) {
	root, season, video, size, modTimeNS := embeddedTestMedia(t)
	if _, err := DiscoverInLibrary(root, video, size, modTimeNS, 10000, false); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	other := filepath.Join(outside, filepath.Base(video))
	if err := os.WriteFile(other, []byte("outside video contents"), 0600); err != nil {
		t.Fatal(err)
	}
	original := season + "-original"
	if err := os.Rename(season, original); err != nil {
		t.Skipf("directory rename unavailable: %v", err)
	}
	if err := os.Symlink(outside, season); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	track := []EmbeddedTrack{{Index: 0, Codec: "subrip", Language: "eng"}}
	reports := InspectEmbeddedInLibrary(context.Background(), "missing-ffmpeg", root, video, size, modTimeNS, track, 10000)
	if len(reports) != 1 || reports[0].Status != "unverified" || !strings.Contains(reports[0].Reason, "inside the library") {
		t.Fatalf("unsafe replacement did not fail closed: %+v", reports)
	}
}

func TestInspectEmbeddedUsesPinnedHandleAfterPathRebind(t *testing.T) {
	root, season, video, size, modTimeNS := embeddedTestMedia(t)
	pinned, err := openEmbeddedMediaInLibrary(root, video, size, modTimeNS)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, filepath.Base(video)), []byte("outside video contents"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(season, season+"-original"); err != nil {
		t.Skipf("open-handle directory rename unavailable: %v", err)
	}
	if err := os.Symlink(outside, season); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	reports := inspectEmbedded(context.Background(), buildEmbeddedTestExtractor(t), "pipe:0", pinned,
		[]EmbeddedTrack{{Index: 0, Codec: "subrip", Language: "eng"}}, 10000)
	if len(reports) != 1 || reports[0].Status != "ready" {
		t.Fatalf("extraction followed catalog path after handle opening: %+v", reports)
	}
}
