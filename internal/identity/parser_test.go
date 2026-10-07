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

func TestAmbiguousIdentityRemainsUnresolved(t *testing.T) {
	got := Parse("Mystery File.mkv", catalog.LibraryMovies)
	if got.Resolved || got.Reason == "" {
		t.Fatalf("expected unresolved candidate: %#v", got)
	}
}
