package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/thebrazenbeard/mediaphile-server/internal/subtitles"
)

// publishedSubtitleSearch performs one explicitly requested metadata-only
// provider search; the server never downloads or publishes a subtitle here.
func publishedSubtitleSearch(deps Dependencies) http.HandlerFunc {
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
		choices, err := subtitles.SearchOpenSubtitles(r.Context(), deps.SubtitleProviderClient, key, query)
		if errors.Is(err, subtitles.ErrSubtitleProviderRateLimited) {
			writeError(w, 429, "SUBTITLE_PROVIDER_RATE_LIMITED", "provider rate limit reached; try later")
			return
		}
		if err != nil {
			writeError(w, 502, "SUBTITLE_PROVIDER_UNAVAILABLE", "provider lookup failed")
			return
		}
		writeJSON(w, 200, map[string]any{
			"partId": partID, "provider": "opensubtitles", "candidates": choices,
			"downloaded": false, "published": false,
			"note": "Provider metadata only; release identity and audio synchronization are unverified",
		})
	}
}

// publishedSubtitleStage downloads a single admin-selected provider file into
// a configured staging directory. This endpoint never installs a media sidecar.
func publishedSubtitleStage(deps Dependencies) http.HandlerFunc {
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
		key := os.Getenv("MEDIAPHILE_OPENSUBTITLES_API_KEY")
		bearer := os.Getenv("MEDIAPHILE_OPENSUBTITLES_TOKEN")
		root := os.Getenv("MEDIAPHILE_SUBTITLE_STAGE_ROOT")
		if key == "" || bearer == "" || root == "" {
			writeError(w, 503, "SUBTITLE_STAGE_NOT_CONFIGURED", "provider download or staging is not configured")
			return
		}
		var input struct {
			FileID int64 `json:"fileId"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, 400, "INVALID_JSON", "invalid selected file")
			return
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			writeError(w, 400, "INVALID_JSON", "multiple JSON objects are not allowed")
			return
		}
		if input.FileID < 1 || input.FileID > 2147483647 {
			writeError(w, 400, "INVALID_FILE_ID", "provider file ID is invalid")
			return
		}
		item, err := deps.Catalog.GetItem(r.Context(), source.ItemID)
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "subtitle media item unavailable")
			return
		}
		library, err := deps.Catalog.GetLibrary(r.Context(), item.LibraryID)
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "subtitle library unavailable")
			return
		}
		staged, err := subtitles.StageOpenSubtitlesInLibrary(r.Context(), deps.SubtitleProviderClient, key, bearer,
			input.FileID, partID, library.RootPath, part.Path, root, part.Size, part.ModTimeNS, source.DurationMS)
		if errors.Is(err, subtitles.ErrSubtitleProviderRateLimited) {
			writeError(w, 429, "SUBTITLE_PROVIDER_RATE_LIMITED", "provider rate limit reached; try later")
			return
		}
		if err != nil {
			writeError(w, 502, "SUBTITLE_STAGE_FAILED", "subtitle staging failed; no media changes were made")
			return
		}
		writeJSON(w, 201, map[string]any{
			"partId": partID, "stage": staged, "downloaded": true, "published": false,
			"note": "Candidate staged for release, timing, and dialogue review; not installed",
		})
	}
}

// reviewedPublishedStage exposes the full bounded text of one explicitly
// staged file. It never contacts the provider or changes media files.
func reviewedPublishedStage(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, 503, "CATALOG_UNAVAILABLE", "catalog unavailable")
			return
		}
		partID, stageID := r.PathValue("partId"), r.PathValue("stageId")
		part, source, err := deps.Catalog.GetMediaPartWithSource(r.Context(), partID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (!part.Available || !source.Available)) {
			writeError(w, 404, "MEDIA_UNAVAILABLE", "media part not found")
			return
		}
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "media lookup failed")
			return
		}
		root := os.Getenv("MEDIAPHILE_SUBTITLE_STAGE_ROOT")
		if root == "" {
			writeError(w, 503, "SUBTITLE_STAGE_NOT_CONFIGURED", "private staging root not configured")
			return
		}
		item, err := deps.Catalog.GetItem(r.Context(), source.ItemID)
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "subtitle media item unavailable")
			return
		}
		library, err := deps.Catalog.GetLibrary(r.Context(), item.LibraryID)
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "subtitle library unavailable")
			return
		}
		review, err := subtitles.ReviewPublishedStageInLibrary(library.RootPath, root, partID, part.Path,
			part.Size, part.ModTimeNS, source.DurationMS, stageID)
		if err != nil {
			writeError(w, 409, "SUBTITLE_STAGE_REVIEW_BLOCKED", "staged subtitle is missing, changed, or not bound to this media part")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		writeJSON(w, 200, review)
	}
}

// manuallyInstallPublishedStage is the only transition from staged provider
// bytes to a media sidecar. A separate explicit admin request must confirm the
// displayed SHA and human review, and the server rechecks all policy/bytes.
func manuallyInstallPublishedStage(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Catalog == nil {
			writeError(w, 503, "CATALOG_UNAVAILABLE", "catalog unavailable")
			return
		}
		partID, stageID := r.PathValue("partId"), r.PathValue("stageId")
		var input struct {
			ExpectedSHA256 string `json:"expectedSha256"`
			ConfirmReviewed bool   `json:"confirmReviewed"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, 400, "INVALID_JSON", "invalid review confirmation")
			return
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			writeError(w, 400, "INVALID_JSON", "multiple JSON objects are not allowed")
			return
		}
		if !input.ConfirmReviewed {
			writeError(w, 409, "SUBTITLE_REVIEW_REQUIRED", "explicit subtitle review confirmation required")
			return
		}
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
		root := os.Getenv("MEDIAPHILE_SUBTITLE_STAGE_ROOT")
		if root == "" {
			writeError(w, 503, "SUBTITLE_STAGE_NOT_CONFIGURED", "private staging root not configured")
			return
		}
		item, err := deps.Catalog.GetItem(r.Context(), source.ItemID)
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "subtitle media item unavailable")
			return
		}
		library, err := deps.Catalog.GetLibrary(r.Context(), item.LibraryID)
		if err != nil {
			writeError(w, 500, "CATALOG_ERROR", "subtitle library unavailable")
			return
		}
		installed, err := subtitles.InstallPublishedStageInLibrary(library.RootPath, root, partID, part.Path,
			part.Size, part.ModTimeNS, source.DurationMS, stageID, input.ExpectedSHA256)
		if err != nil {
			writeError(w, 409, "SUBTITLE_PUBLISH_BLOCKED", "staged subtitle changed, review failed, or destination is occupied")
			return
		}
		status := 201
		if installed.AlreadyInstalled {
			status = 200
		}
		writeJSON(w, status, installed)
	}
}
