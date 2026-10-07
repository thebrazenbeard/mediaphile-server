package provenance

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func TestImportPreservesEvidenceClassConflictAndSourceBinding(t *testing.T) {
	db, err := catalog.Open(filepath.Join(t.TempDir(), "prov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := catalog.NewRepository(db)
	ctx := context.Background()
	if err := repo.CreateLibrary(ctx, catalog.Library{ID: "movies", Name: "Movies", MediaType: catalog.LibraryMovies, RootPath: "C:/media", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertItem(ctx, catalog.Item{ID: "movie", LibraryID: "movies", Kind: catalog.ItemMovie, Title: "Movie"}); err != nil {
		t.Fatal(err)
	}
	artifact := Artifact{
		SourceRepository: "thebrazenbeard/mediaphile", SourceRevision: "abc123", SourceDigest: "sha256:deadbeef",
		Records: []Record{
			{SourceRecordID: "r1", TargetItemID: "movie", EvidenceClass: DerivedAnalysis, Payload: json.RawMessage(`{"claim":"interpretation"}`), Conflict: true},
			{SourceRecordID: "r2", TargetItemID: "movie", EvidenceClass: ScreenplayTranscript, Payload: json.RawMessage(`{"quote":"line"}`)},
		},
	}
	if _, err := Import(ctx, repo, artifact); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ListKnowledgeForItem(ctx, "movie")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("records=%d want=2", len(got))
	}
	byClass := map[string]catalog.KnowledgeRecord{}
	for _, v := range got {
		byClass[v.EvidenceClass] = v
	}
	derived := byClass[string(DerivedAnalysis)]
	if !derived.Conflict || derived.SourceRepository != "thebrazenbeard/mediaphile" || derived.SourceRevision != "abc123" || derived.SourceDigest != "sha256:deadbeef" {
		t.Fatalf("derived provenance changed: %#v", derived)
	}
	if byClass[string(ScreenplayTranscript)].Conflict {
		t.Fatal("screenplay evidence incorrectly marked conflicting")
	}
}
