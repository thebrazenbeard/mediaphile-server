package playback

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/events"
)

func sessionRepo(t *testing.T) (*catalog.Repository, func()) {
	t.Helper()
	db, err := catalog.Open(filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	repo := catalog.NewRepository(db)
	ctx := context.Background()
	if err := repo.CreateLibrary(ctx, catalog.Library{ID: "movies", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: "C:/media", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertItem(ctx, catalog.Item{ID: "movie", LibraryID: "movies", Kind: catalog.ItemMovie, Title: "Movie"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaSource(ctx, catalog.MediaSource{ID: "source", ItemID: "movie", Container: "mp4", DurationMS: 10_000, Available: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreatePrincipal(ctx, catalog.Principal{ID: "u1", Username: "one", PasswordHash: "x", Admin: false}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreatePrincipal(ctx, catalog.Principal{ID: "u2", Username: "two", PasswordHash: "x", Admin: false}); err != nil {
		t.Fatal(err)
	}
	return repo, func() { _ = db.Close() }
}

func TestSessionProgressPersistsAndCompletesAtNinetyPercent(t *testing.T) {
	repo, closeFn := sessionRepo(t)
	defer closeFn()
	bus := events.NewBus()
	ch, cancel := bus.Subscribe(10)
	defer cancel()
	m := NewSessionManager(repo, bus)
	ctx := context.Background()
	s, err := m.Create(ctx, "u1", "movie", "client", "source", DirectPlay)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Update(ctx, "u1", s.ID, 4000, "playing"); err != nil {
		t.Fatal(err)
	}
	state, err := repo.GetPlaybackState(ctx, "u1", "movie")
	if err != nil {
		t.Fatal(err)
	}
	if state.ResumeMS != 4000 || state.Completed {
		t.Fatalf("unexpected state %#v", state)
	}
	if _, err := m.Update(ctx, "u1", s.ID, 9000, "playing"); err != nil {
		t.Fatal(err)
	}
	state, err = repo.GetPlaybackState(ctx, "u1", "movie")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Completed || state.ResumeMS != 0 || state.PlayCount != 1 {
		t.Fatalf("completion state %#v", state)
	}
	types := map[string]bool{}
	for len(ch) > 0 {
		types[(<-ch).Type] = true
	}
	if !types["media.play"] || !types["media.progress"] || !types["media.completed"] {
		t.Fatalf("events=%v", types)
	}
}

func TestSessionOwnershipAndStopReason(t *testing.T) {
	repo, closeFn := sessionRepo(t)
	defer closeFn()
	m := NewSessionManager(repo, events.NewBus())
	ctx := context.Background()
	s, err := m.Create(ctx, "u1", "movie", "client", "source", DirectPlay)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Update(ctx, "u2", s.ID, 1000, "playing"); !errors.Is(err, ErrSessionForbidden) {
		t.Fatalf("err=%v", err)
	}
	if err := m.End(ctx, "u1", s.ID, "user_stop"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetPlaybackSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StopReason == nil || *got.StopReason != "user_stop" || got.EndedAt == nil {
		t.Fatalf("session=%#v", got)
	}
}

func TestPlaybackStateSurvivesManagerRestart(t *testing.T) {
	repo, closeFn := sessionRepo(t)
	defer closeFn()
	ctx := context.Background()
	m1 := NewSessionManager(repo, events.NewBus())
	s, err := m1.Create(ctx, "u1", "movie", "client", "source", DirectPlay)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m1.Update(ctx, "u1", s.ID, 3500, "paused"); err != nil {
		t.Fatal(err)
	}
	m2 := NewSessionManager(repo, events.NewBus())
	state, err := m2.PlaybackState(ctx, "u1", "movie")
	if err != nil {
		t.Fatal(err)
	}
	if state.ResumeMS != 3500 {
		t.Fatalf("resume=%d", state.ResumeMS)
	}
}
