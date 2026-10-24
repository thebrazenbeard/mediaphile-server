package subtitles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PublishedQuery describes an explicit provider search. It contains no NAS path.
type PublishedQuery struct {
	Query  string
	IMDbID string
	Year   int
}
type PublishedCandidate struct {
	Provider        string `json:"provider"`
	SubtitleID      string `json:"subtitleId"`
	FileID          int64  `json:"fileId"`
	Release         string `json:"release"`
	FileName        string `json:"fileName"`
	Language        string `json:"language"`
	Trusted         bool   `json:"trusted"`
	HearingImpaired bool   `json:"hearingImpaired"`
	MoviehashMatch  bool   `json:"moviehashMatch"`
}

// ErrSubtitleProviderRateLimited indicates a provider quota response. The caller
// should schedule a later attempt, not retry within the same request.
var ErrSubtitleProviderRateLimited = errors.New("subtitle provider rate limited")
var imdbPattern = regexp.MustCompile(`^(?:tt)?[0-9]{1,10}$`)

type opensubtitlesSearchResponse struct {
	Data []struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			Language          string `json:"language"`
			Release           string `json:"release"`
			MoviehashMatch    bool   `json:"moviehash_match"`
			FromTrusted       bool   `json:"from_trusted"`
			HearingImpaired   bool   `json:"hearing_impaired"`
			ForeignPartsOnly  bool   `json:"foreign_parts_only"`
			MachineTranslated bool   `json:"machine_translated"`
			AITranslated      bool   `json:"ai_translated"`
			Files             []struct {
				FileID   int64  `json:"file_id"`
				FileName string `json:"file_name"`
			} `json:"files"`
		} `json:"attributes"`
	} `json:"data"`
}

// ValidatePublishedQuery rejects path disclosure and malformed identifiers.
func ValidatePublishedQuery(q PublishedQuery) error {
	_, err := providerRequestParams(q)
	return err
}
func providerRequestParams(q PublishedQuery) (url.Values, error) {
	vals := url.Values{"languages": []string{"en"}}
	if q.IMDbID != "" {
		if !imdbPattern.MatchString(q.IMDbID) {
			return nil, errors.New("invalid IMDb identifier")
		}
		id := strings.TrimPrefix(q.IMDbID, "tt")
		id = strings.TrimLeft(id, "0")
		if id == "" {
			return nil, errors.New("empty IMDb identifier")
		}
		vals.Set("imdb_id", id)
		// An identifier is more reliable than concatenating possibly conflicting
		// release/year filters; only one query strategy is used.
		return vals, nil
	}
	query := strings.TrimSpace(q.Query)
	if len(query) < 2 || len(query) > 180 || strings.ContainsAny(query, "/\\\r\n\x00") {
		return nil, errors.New("invalid subtitle title query")
	}
	if q.Year != 0 && (q.Year < 1900 || q.Year > 2100) {
		return nil, errors.New("invalid title year")
	}
	vals.Set("query", strings.ToLower(query))
	if q.Year != 0 {
		vals.Set("year", strconv.Itoa(q.Year))
	}
	return vals, nil
}

// SearchOpenSubtitles is an opt-in, metadata-only lookup. It performs at most
// one GET against the fixed public API host and NEVER downloads subtitle bytes.
// API keys are supplied by the caller, not persisted in source or logs.
func SearchOpenSubtitles(ctx context.Context, client *http.Client, apiKey string, q PublishedQuery) ([]PublishedCandidate, error) {
	if ctx == nil {
		return nil, errors.New("missing context")
	}
	if len(apiKey) < 1 || len(apiKey) > 256 || strings.ContainsAny(apiKey, "\r\n") {
		return nil, errors.New("provider API key is not configured")
	}
	params, err := providerRequestParams(q)
	if err != nil {
		return nil, err
	}
	endpoint := "https://api.opensubtitles.com/api/v1/subtitles?" + params.Encode()
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(runCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("invalid provider search")
	}
	req.Header.Set("Api-Key", apiKey)
	req.Header.Set("User-Agent", "Mediaphile v1.0")
	req.Header.Set("Accept", "application/json")
	requestClient := http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if client != nil {
		requestClient.Transport = client.Transport
	}
	if err := runCtx.Err(); err != nil {
		return nil, err
	}
	resp, err := requestClient.Do(req)
	if err != nil {
		if cause := runCtx.Err(); cause != nil {
			return nil, cause
		}
		return nil, errors.New("provider search transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrSubtitleProviderRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider search failed with HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		return nil, errors.New("provider response could not be read")
	}
	if len(raw) > 1<<20 {
		return nil, errors.New("provider response exceeds size limit")
	}
	var result opensubtitlesSearchResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, errors.New("provider response is invalid JSON")
	}
	seen := map[int64]bool{}
	items := make([]PublishedCandidate, 0)
	for _, item := range result.Data {
		a := item.Attributes
		if a.Language != "en" && a.Language != "eng" {
			continue
		}
		if a.MachineTranslated || a.AITranslated || a.ForeignPartsOnly {
			continue
		}
		if item.Type != "" && item.Type != "subtitle" {
			continue
		}
		for _, f := range a.Files {
			if f.FileID <= 0 || seen[f.FileID] {
				continue
			}
			seen[f.FileID] = true
			items = append(items, PublishedCandidate{
				Provider: "opensubtitles", SubtitleID: item.ID, FileID: f.FileID,
				Release: cleanProviderLabel(a.Release), FileName: cleanProviderLabel(f.FileName),
				Language: "en", Trusted: a.FromTrusted, HearingImpaired: a.HearingImpaired,
				MoviehashMatch: a.MoviehashMatch,
			})
			if len(items) >= 30 {
				return items, nil
			}
		}
	}
	return items, nil
}
func cleanProviderLabel(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < ' ' || r == 127 {
			return ' '
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	letters := []rune(value)
	if len(letters) > 180 {
		value = string(letters[:180])
	}
	return value
}
