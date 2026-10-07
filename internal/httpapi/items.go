package httpapi

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

type itemDTO struct {
	ID            string  `json:"id"`
	LibraryID     string  `json:"libraryId"`
	ParentID      *string `json:"parentId,omitempty"`
	Kind          string  `json:"kind"`
	Title         string  `json:"title"`
	Year          *int    `json:"year,omitempty"`
	SeasonNumber  *int    `json:"seasonNumber,omitempty"`
	EpisodeNumber *int    `json:"episodeNumber,omitempty"`
	Unresolved    bool    `json:"unresolved"`
}

type sourceDTO struct {
	ID         string `json:"id"`
	Container  string `json:"container"`
	DurationMS int64  `json:"durationMs"`
	Bitrate    int64  `json:"bitrate"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	VideoCodec string `json:"videoCodec,omitempty"`
	AudioCodec string `json:"audioCodec,omitempty"`
	HDR        bool   `json:"hdr"`
	Available  bool   `json:"available"`
}

type partDTO struct {
	ID        string `json:"id"`
	SourceID  string `json:"sourceId"`
	Size      int64  `json:"size"`
	Available bool   `json:"available"`
}

type streamDTO struct {
	ID        string `json:"id"`
	PartID    string `json:"partId"`
	Kind      string `json:"kind"`
	Index     int    `json:"index"`
	Codec     string `json:"codec"`
	Language  string `json:"language,omitempty"`
	Channels  int    `json:"channels,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	FrameRate string `json:"frameRate,omitempty"`
	Default   bool   `json:"default"`
	Forced    bool   `json:"forced"`
}

func items(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, http.StatusServiceUnavailable, "CATALOG_UNAVAILABLE", "catalog is unavailable")
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 200 {
				writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limit must be between 1 and 200")
				return
			}
			limit = n
		}
		var afterTitle, afterID string
		if raw := r.URL.Query().Get("cursor"); raw != "" {
			var err error
			afterTitle, afterID, err = decodeCursor(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_CURSOR", "cursor is invalid")
				return
			}
		}
		q := catalog.ItemQuery{
			LibraryID:  r.URL.Query().Get("libraryId"),
			Kind:       catalog.ItemKind(r.URL.Query().Get("kind")),
			ParentID:   r.URL.Query().Get("parentId"),
			Search:     r.URL.Query().Get("q"),
			AfterTitle: afterTitle, AfterID: afterID, Limit: limit + 1,
		}
		values, err := deps.Catalog.ListItems(r.Context(), q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CATALOG_ERROR", "could not list items")
			return
		}
		next := ""
		if len(values) > limit {
			last := values[limit-1]
			next = encodeCursor(last.Title, last.ID)
			values = values[:limit]
		}
		out := make([]itemDTO, 0, len(values))
		for _, v := range values {
			out = append(out, toItemDTO(v))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": out, "nextCursor": next})
	}
}

func itemDetail(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, http.StatusServiceUnavailable, "CATALOG_UNAVAILABLE", "catalog is unavailable")
			return
		}
		id := r.PathValue("itemId")
		item, err := deps.Catalog.GetItem(r.Context(), id)
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "item not found")
			} else {
				writeError(w, http.StatusInternalServerError, "CATALOG_ERROR", "could not load item")
			}
			return
		}
		sources, parts, streams, err := deps.Catalog.MediaInventory(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CATALOG_ERROR", "could not load media inventory")
			return
		}
		sourceOut := make([]sourceDTO, 0, len(sources))
		for _, v := range sources {
			sourceOut = append(sourceOut, sourceDTO{ID: v.ID, Container: v.Container, DurationMS: v.DurationMS, Bitrate: v.Bitrate, Width: v.Width, Height: v.Height, VideoCodec: v.VideoCodec, AudioCodec: v.AudioCodec, HDR: v.HDR, Available: v.Available})
		}
		partOut := make([]partDTO, 0, len(parts))
		for _, v := range parts {
			partOut = append(partOut, partDTO{ID: v.ID, SourceID: v.SourceID, Size: v.Size, Available: v.Available})
		}
		streamOut := make([]streamDTO, 0, len(streams))
		for _, v := range streams {
			streamOut = append(streamOut, streamDTO{ID: v.ID, PartID: v.PartID, Kind: string(v.Kind), Index: v.StreamIndex, Codec: v.Codec, Language: v.Language, Channels: v.Channels, Width: v.Width, Height: v.Height, FrameRate: v.FrameRate, Default: v.Default, Forced: v.Forced})
		}
		knowledge, err := deps.Catalog.ListKnowledgeForItem(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CATALOG_ERROR", "could not load knowledge records")
			return
		}
		knowledgeOut := make([]map[string]any, 0, len(knowledge))
		for _, v := range knowledge {
			var payload any
			if err := json.Unmarshal([]byte(v.PayloadJSON), &payload); err != nil {
				payload = map[string]any{"raw": v.PayloadJSON}
			}
			knowledgeOut = append(knowledgeOut, map[string]any{
				"id": v.ID, "sourceRepository": v.SourceRepository, "sourceRevision": v.SourceRevision,
				"sourceDigest": v.SourceDigest, "sourceRecordId": v.SourceRecordID, "evidenceClass": v.EvidenceClass,
				"payload": payload, "unresolved": v.Unresolved, "conflict": v.Conflict, "importedAt": v.ImportedAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"item": toItemDTO(item), "sources": sourceOut, "parts": partOut, "streams": streamOut, "knowledge": knowledgeOut})
	}
}

func toItemDTO(v catalog.Item) itemDTO {
	return itemDTO{ID: v.ID, LibraryID: v.LibraryID, ParentID: v.ParentID, Kind: string(v.Kind), Title: v.Title, Year: v.Year, SeasonNumber: v.SeasonNumber, EpisodeNumber: v.EpisodeNumber, Unresolved: v.Unresolved}
}

func encodeCursor(title, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.ToLower(title) + "\x00" + id))
}

func decodeCursor(raw string) (string, string, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(string(data), "\x00")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid cursor")
	}
	return parts[0], parts[1], nil
}
