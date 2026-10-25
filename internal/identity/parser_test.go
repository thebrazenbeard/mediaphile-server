package identity

import (
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func TestParseMovie(t *testing.T) {
	got := Parse("Arrival (2016).mkv", catalog.LibraryMovies)
	if !got.Resolved || got.Kind != catalog.ItemMovie || got.Title != "Arrival" || got.Year == nil || *got.Year != 2016 {
		t.Fatalf("unexpected candidate: %#v", got)
	}
}

func TestParseEpisode(t *testing.T) {
	got := Parse("Example Show/Season 02/Example Show - S02E03 - Third Thing.mkv", catalog.LibraryTV)
	if !got.Resolved || got.Kind != catalog.ItemEpisode || got.ShowTitle != "Example Show" || got.Season != 2 || got.Episode != 3 || got.Title != "Third Thing" {
		t.Fatalf("unexpected candidate: %#v", got)
	}
}

func TestParseHistoricalBasenameOnlyEpisodes(t *testing.T) {
	tests := []struct {
		path, show, title string
		season, episode   int
	}{
		{"iZombie Season 01/S01E01 - Pilot.mkv", "iZombie", "Pilot", 1, 1},
		{"iZombie/Season 04/S04E01 - Are You Ready for Some Zombies.mkv", "iZombie", "Are You Ready for Some Zombies", 4, 1},
		{"iZombie/S01E03.mkv", "iZombie", "Episode 3", 1, 3},
		{"iZombie/Season 02/S02E09 - Cape Town.mkv", "iZombie", "Cape Town", 2, 9},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := Parse(tt.path, catalog.LibraryTV)
			if !got.Resolved || got.Kind != catalog.ItemEpisode || got.ShowTitle != tt.show || got.Title != tt.title || got.Season != tt.season || got.Episode != tt.episode {
				t.Fatalf("unexpected candidate: %#v", got)
			}
		})
	}
}

func TestBasenameOnlyEpisodeWithoutUnambiguousShowStaysUnresolved(t *testing.T) {
	for _, path := range []string{
		"S01E01 - Pilot.mkv",
		"Season 01/S01E01 - Pilot.mkv",
		"iZombie Season 02/S01E01 - Pilot.mkv",
		"iZombie/Season 02/S01E01 - Pilot.mkv",
		"iZombie/Extras/S01E01 - Pilot.mkv",
		"Some Show/Downloads/S01E01 - Pilot.mkv",
		"iZombie/Extras/Season 01/S01E01 - Pilot.mkv",
		"Shows/iZombie/Season 01/S01E01 - Pilot.mkv",
	} {
		t.Run(path, func(t *testing.T) {
			got := Parse(path, catalog.LibraryTV)
			if got.Resolved || got.Kind != catalog.ItemEpisode || got.Reason == "" {
				t.Fatalf("ambiguous identity should remain unresolved: %#v", got)
			}
		})
	}
}

func TestAmbiguousIdentityRemainsUnresolved(t *testing.T) {
	got := Parse("Mystery File.mkv", catalog.LibraryMovies)
	if got.Resolved || got.Reason == "" {
		t.Fatalf("expected unresolved candidate: %#v", got)
	}
}
