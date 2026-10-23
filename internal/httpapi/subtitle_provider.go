package httpapi

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/thebrazenbeard/mediaphile-server/internal/subtitles"
)

// publishedSubtitleSearch performs one explicitly requested metadata-only
// provider search; the server never downloads or publishes a subtitle here.
func publishedSubtitleSearch(deps Dependencies) http.HandlerFunc {
	// Serialize lookups in this router process to avoid concurrent duplicate
	// provider requests. The persistent SQLite cache survives restarts.
	var lookupMu sync.Mutex
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, 503, "CATALOG_UNAVAILABLE", "catalog unavailable")
			return
		}
		partID := r.PathValue("partId")
		part, source, err := deps.Catalog.GetMediaPartWithSource(r.Context(), partID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (!part.Available || !source.Available)) {
			writeError(w, 404, "MEDIA_UNAVAILABLE", "media part not found")
			return
		}
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "media lookup failed")
			return
		}
		decision, err := deps.Catalog.SubtitleDecision(r.Context(), partID)
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "subtitle decision unavailable")
			return
		}
		if decision.Disposition != "active" {
			writeError(w, 409, "SUBTITLE_DECISION_HOLD", "video is skipped or held for review")
			return
		}
		var input struct {
			Query  string `json:"query"`
			IMDbID string `json:"imdbId"`
			Year   int    `json:"year"`
		}

		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&input); err != nil {
			writeError(w, 400, "INVALID_JSON", "invalid search input")
			return
		}
		var extra any
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			writeError(w, 400, "INVALID_JSON", "multiple JSON objects not allowed")
			return
		}
		query := subtitles.PublishedQuery{Query: input.Query, IMDbID: input.IMDbID, Year: input.Year}
		if strings.TrimSpace(query.Query) == "" && query.IMDbID == "" {
			query.Query = strings.TrimSuffix(filepath.Base(part.Path), filepath.Ext(part.Path))
		}
		if err := subtitles.ValidatePublishedQuery(query); err != nil {
			writeError(w, 400, "INVALID_SEARCH", "invalid title or IMDb search")
			return
		}
		key := os.Getenv("MEDIAPHILE_OPENSUBTITLES_API_KEY")
		if key == "" {
			writeError(w, 503, "SUBTITLE_PROVIDER_NOT_CONFIGURED", "OpenSubtitles API key is not configured")
			return
		}
		// API credentials are checked before cache access. Cached public metadata
		// is not a substitute for provider configuration or access controls.
		queryHash := providerQueryHash(query)
		lookupMu.Lock()
		defer lookupMu.Unlock()
		if r.Context().Err() != nil {
			writeError(w, 408, "SUBTITLE_SEARCH_CANCELLED", "provider search cancelled")
			return
		}
		cachedJSON, hit, err := deps.Catalog.ProviderCachedCandidates(r.Context(), partID, queryHash, time.Now().UnixMilli())
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "provider cache unavailable")
			return
		}
		if hit {
			var cached []subtitles.PublishedCandidate
			if err := json.Unmarshal(cachedJSON, &cached); err != nil {
				writeError(w, 500, "CATALOG_ERROR", "cached provider metadata invalid")
				return
			}
			writePublishedChoices(w, partID, cached, true)
			return
		}
		blocked, err := deps.Catalog.SubtitleProviderBlocked(r.Context(), "opensubtitles", time.Now().UnixMilli())
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "provider cooldown unavailable")
			return
		}
		if blocked {
			writeError(w, 429, "SUBTITLE_PROVIDER_RATE_LIMITED", "provider cooldown active; retry later")
			return
		}
		choices, err := subtitles.SearchOpenSubtitles(r.Context(), deps.SubtitleProviderClient, key, query)
		if errors.Is(err, subtitles.ErrSubtitleProviderRateLimited) {
			// A real HTTP 429 pauses *all* lookups of this provider for one
			// hour. Never cache failure bodies or make an immediate retry.
			if pauseErr := deps.Catalog.PauseSubtitleProvider(r.Context(), "opensubtitles", time.Now().Add(time.Hour).UnixMilli()); pauseErr != nil {
				writeError(w, 503, "CATALOG_ERROR", "could not persist provider cooldown")
				return
			}
			writeError(w, 429, "SUBTITLE_PROVIDER_RATE_LIMITED", "provider rate limit reached; retry later")
			return
		}
		if err != nil {
			writeError(w, 502, "SUBTITLE_PROVIDER_UNAVAILABLE", "provider lookup failed")
			return
		}
		if choices == nil {
			choices = []subtitles.PublishedCandidate{}
		}
		blob, err := json.Marshal(choices)
		if err != nil {
			writeError(w, 502, "SUBTITLE_PROVIDER_UNAVAILABLE", "provider results could not be encoded")
			return
		}
		if err := deps.Catalog.SaveProviderCandidates(r.Context(), partID, queryHash, blob, time.Now().Add(24*time.Hour).UnixMilli()); err != nil {
			writeError(w, 503, "CATALOG_ERROR", "provider results could not be cached")
			return
		}
		writePublishedChoices(w, partID, choices, false)
	}
}

// providerQueryHash normalizes query spelling without storing credentials or
// local media paths; IMDb lookups intentionally ignore fallback title/year.
func providerQueryHash(q subtitles.PublishedQuery) string {
	identity := ""
	if q.IMDbID != "" {
		numeric := strings.TrimLeft(strings.TrimPrefix(q.IMDbID, "tt"), "0")
		identity = "imdb:" + numeric
	} else {
		title := strings.ToLower(strings.Join(strings.Fields(q.Query), " "))
		identity = fmt.Sprintf("title:%s;year:%d", title, q.Year)
	}
	sum := sha256.Sum256([]byte("opensubtitles:v1:" + identity))
	return hex.EncodeToString(sum[:])
}

func writePublishedChoices(w http.ResponseWriter, partID string, choices []subtitles.PublishedCandidate, cached bool) {
	if choices == nil {
		choices = []subtitles.PublishedCandidate{}
	}
	writeJSON(w, 200, map[string]any{
		"partId": partID, "provider": "opensubtitles", "candidates": choices,
		"downloaded": false, "published": false, "cached": cached,
		"note": "Provider metadata only; release identity and audio synchronization are unverified",
	})
}
