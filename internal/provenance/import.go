package provenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

type EvidenceClass string

const (
	FilesystemCatalog    EvidenceClass = "filesystem_catalog"
	ExternalMetadata     EvidenceClass = "external_metadata"
	ScreenplayTranscript EvidenceClass = "screenplay_transcript"
	Subtitle             EvidenceClass = "subtitle"
	AudiovisualReview    EvidenceClass = "audiovisual_review"
	DerivedAnalysis      EvidenceClass = "derived_analysis"
)

var allowedEvidence = map[EvidenceClass]bool{
	FilesystemCatalog: true, ExternalMetadata: true, ScreenplayTranscript: true,
	Subtitle: true, AudiovisualReview: true, DerivedAnalysis: true,
}

type Artifact struct {
	SourceRepository string   `json:"sourceRepository"`
	SourceRevision   string   `json:"sourceRevision"`
	SourceDigest     string   `json:"sourceDigest"`
	Records          []Record `json:"records"`
}

type Record struct {
	SourceRecordID string          `json:"sourceRecordId"`
	TargetItemID   string          `json:"targetItemId"`
	EvidenceClass  EvidenceClass   `json:"evidenceClass"`
	Payload        json.RawMessage `json:"payload"`
	Unresolved     bool            `json:"unresolved"`
	Conflict       bool            `json:"conflict"`
}

type Result struct {
	Imported int `json:"imported"`
}

func Import(ctx context.Context, repo *catalog.Repository, artifact Artifact) (Result, error) {
	if repo == nil {
		return Result{}, fmt.Errorf("catalog is required")
	}
	if artifact.SourceRepository == "" || artifact.SourceRevision == "" || artifact.SourceDigest == "" {
		return Result{}, fmt.Errorf("source repository, revision, and digest are required")
	}
	var out Result
	for _, record := range artifact.Records {
		if record.SourceRecordID == "" || record.TargetItemID == "" {
			return out, fmt.Errorf("sourceRecordId and targetItemId are required")
		}
		if !allowedEvidence[record.EvidenceClass] {
			return out, fmt.Errorf("unsupported evidence class %q", record.EvidenceClass)
		}
		if !json.Valid(record.Payload) {
			return out, fmt.Errorf("record %s payload is invalid JSON", record.SourceRecordID)
		}
		id := knowledgeID(artifact, record)
		if err := repo.UpsertKnowledgeRecord(ctx, catalog.KnowledgeRecord{
			ID: id, ItemID: record.TargetItemID, SourceRepository: artifact.SourceRepository, SourceRevision: artifact.SourceRevision,
			SourceDigest: artifact.SourceDigest, SourceRecordID: record.SourceRecordID, EvidenceClass: string(record.EvidenceClass),
			PayloadJSON: string(record.Payload), Unresolved: record.Unresolved, Conflict: record.Conflict,
		}); err != nil {
			return out, err
		}
		out.Imported++
	}
	return out, nil
}

func knowledgeID(a Artifact, r Record) string {
	sum := sha256.Sum256([]byte(a.SourceRepository + "\x00" + a.SourceRevision + "\x00" + r.SourceRecordID + "\x00" + r.TargetItemID))
	return "kn_" + hex.EncodeToString(sum[:8])
}
