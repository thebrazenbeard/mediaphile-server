package catalog

import (
	"context"
	"path/filepath"
	"testing"
)

func openTestRepo(t *testing.T) *Repository {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewRepository(db)
}

func TestMigrationsApplyAndAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := db.QueryRow("select max(version) from schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("version=%d want=1", version)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if err := db2.QueryRow("select max(version) from schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("second open version=%d want=1", version)
	}
}

func TestCatalogHierarchyAndMediaInventory(t *testing.T) {
	ctx := context.Background()
	repo := openTestRepo(t)
	lib := Library{ID: "lib-tv", Name: "TV", MediaType: LibraryTV, RootPath: "Z:/TV", Enabled: true}
	if err := repo.CreateLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	show := Item{ID: "show-1", LibraryID: lib.ID, Kind: ItemShow, Title: "Example Show"}
	seasonNo := 2
	season := Item{ID: "season-2", LibraryID: lib.ID, ParentID: ptr(show.ID), Kind: ItemSeason, Title: "Season 2", SeasonNumber: &seasonNo}
	episodeNo := 3
	episode := Item{ID: "episode-3", LibraryID: lib.ID, ParentID: ptr(season.ID), Kind: ItemEpisode, Title: "Episode Three", SeasonNumber: &seasonNo, EpisodeNumber: &episodeNo}
	for _, item := range []Item{show, season, episode} {
		if err := repo.UpsertItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.GetItem(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentID == nil || *got.ParentID != season.ID || got.Kind != ItemEpisode {
		t.Fatalf("episode hierarchy not preserved: %#v", got)
	}

	source := MediaSource{ID: "src-1", ItemID: episode.ID, Container: "mkv", DurationMS: 1_000, VideoCodec: "h264", AudioCodec: "aac", Available: true}
	part := MediaPart{ID: "part-1", SourceID: source.ID, Path: "Z:/TV/Example Show/Season 02/ep.mkv", Size: 1234, ModTimeNS: 77, Available: true}
	stream := MediaStream{ID: "stream-1", PartID: part.ID, Kind: StreamVideo, StreamIndex: 0, Codec: "h264", Width: 1920, Height: 1080, Default: true}
	if err := repo.UpsertMediaSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaPart(ctx, part); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaStream(ctx, stream); err != nil {
		t.Fatal(err)
	}

	sources, parts, streams, err := repo.MediaInventory(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || len(parts) != 1 || len(streams) != 1 {
		t.Fatalf("inventory source=%d parts=%d streams=%d", len(sources), len(parts), len(streams))
	}
	if parts[0].Path != part.Path || streams[0].Codec != "h264" {
		t.Fatalf("inventory mismatch: %#v %#v", parts[0], streams[0])
	}
}

func TestUnavailablePartDoesNotDeletePlaybackState(t *testing.T) {
	ctx := context.Background()
	repo := openTestRepo(t)
	if err := repo.CreateLibrary(ctx, Library{ID: "movies", Name: "Movies", MediaType: LibraryMovies, RootPath: "Z:/Movies", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertItem(ctx, Item{ID: "movie-1", LibraryID: "movies", Kind: ItemMovie, Title: "Movie"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaSource(ctx, MediaSource{ID: "source-1", ItemID: "movie-1", Container: "mp4", Available: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaPart(ctx, MediaPart{ID: "part-1", SourceID: "source-1", Path: "Z:/Movies/Movie.mp4", Size: 55, ModTimeNS: 12, Available: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreatePrincipal(ctx, Principal{ID: "user-1", Username: "patrick", PasswordHash: "opaque", Admin: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.PutPlaybackState(ctx, PlaybackState{PrincipalID: "user-1", ItemID: "movie-1", ResumeMS: 4321, PlayCount: 1}); err != nil {
		t.Fatal(err)
	}

	if err := repo.SetMediaPartAvailable(ctx, "part-1", false); err != nil {
		t.Fatal(err)
	}
	state, err := repo.GetPlaybackState(ctx, "user-1", "movie-1")
	if err != nil {
		t.Fatal(err)
	}
	if state.ResumeMS != 4321 || state.PlayCount != 1 {
		t.Fatalf("playback state changed: %#v", state)
	}
	_, parts, _, err := repo.MediaInventory(ctx, "movie-1")
	if err != nil {
		t.Fatal(err)
	}
	if parts[0].Available {
		t.Fatal("part should be unavailable")
	}
}

func TestForeignKeysRejectInvalidHierarchyWithoutPartialRow(t *testing.T) {
	ctx := context.Background()
	repo := openTestRepo(t)
	if err := repo.CreateLibrary(ctx, Library{ID: "tv", Name: "TV", MediaType: LibraryTV, RootPath: "Z:/TV", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	bad := Item{ID: "bad-episode", LibraryID: "tv", ParentID: ptr("missing-season"), Kind: ItemEpisode, Title: "Bad"}
	if err := repo.UpsertItem(ctx, bad); err == nil {
		t.Fatal("expected foreign-key error")
	}
	if _, err := repo.GetItem(ctx, bad.ID); err == nil {
		t.Fatal("invalid child row was persisted")
	}
}

func ptr(s string) *string { return &s }
