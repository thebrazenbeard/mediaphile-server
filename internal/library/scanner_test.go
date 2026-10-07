package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/probe"
)

type fakeProber struct {
	calls int
	info  probe.MediaInfo
}

func (f *fakeProber) Probe(context.Context, string) (probe.MediaInfo, error) {
	f.calls++
	return f.info, nil
}

func newScannerRepo(t *testing.T, root string) *catalog.Repository {
	t.Helper()
	db, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := catalog.NewRepository(db)
	if err := repo.CreateLibrary(context.Background(), catalog.Library{ID: "movies", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: root, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestScannerIsIdempotentAndReprobesChangedFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Arrival (2016).mkv")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := newScannerRepo(t, root)
	fp := &fakeProber{info: probe.MediaInfo{Container: "matroska", DurationMS: 1000, VideoCodec: "h264", AudioCodec: "aac", Streams: []probe.Stream{{Index: 0, Kind: probe.StreamVideo, Codec: "h264"}}}}
	scanner := NewScanner(repo, fp)

	first, err := scanner.Scan(context.Background(), "movies")
	if err != nil {
		t.Fatal(err)
	}
	if first.Probed != 1 || fp.calls != 1 {
		t.Fatalf("first=%#v calls=%d", first, fp.calls)
	}

	second, err := scanner.Scan(context.Background(), "movies")
	if err != nil {
		t.Fatal(err)
	}
	if second.Probed != 0 || second.Unchanged != 1 || fp.calls != 1 {
		t.Fatalf("second=%#v calls=%d", second, fp.calls)
	}

	time.Sleep(2 * time.Millisecond)
	if err := os.WriteFile(path, []byte("changed-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := scanner.Scan(context.Background(), "movies")
	if err != nil {
		t.Fatal(err)
	}
	if third.Probed != 1 || fp.calls != 2 {
		t.Fatalf("third=%#v calls=%d", third, fp.calls)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("scanner modified source file: %v", err)
	}
}

func TestScannerMarksMissingPartUnavailable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Arrival (2016).mkv")
	if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := newScannerRepo(t, root)
	fp := &fakeProber{info: probe.MediaInfo{Container: "matroska", VideoCodec: "h264", AudioCodec: "aac"}}
	scanner := NewScanner(repo, fp)
	if _, err := scanner.Scan(context.Background(), "movies"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	result, err := scanner.Scan(context.Background(), "movies")
	if err != nil {
		t.Fatal(err)
	}
	if result.Missing != 1 {
		t.Fatalf("missing=%d want=1", result.Missing)
	}
	parts, err := repo.ListPartsByLibrary(context.Background(), "movies")
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || parts[0].Available {
		t.Fatalf("expected unavailable part: %#v", parts)
	}
}

func TestScannerSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "Outside (2020).mkv")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "Escape (2020).mkv")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable on this host: %v", err)
	}
	repo := newScannerRepo(t, root)
	fp := &fakeProber{info: probe.MediaInfo{Container: "matroska"}}
	result, err := NewScanner(repo, fp).Scan(context.Background(), "movies")
	if err != nil {
		t.Fatal(err)
	}
	if result.Probed != 0 || fp.calls != 0 {
		t.Fatalf("symlink was probed: %#v calls=%d", result, fp.calls)
	}
}
