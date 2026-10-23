package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPublishedSubtitleCacheSurvivesRouterRecreationAndExpiry(t *testing.T) {
	deps, token, _ := subtitleDecisionFixture(t)
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_API_KEY", "cache-test-key")
	var calls atomic.Int32
	deps.SubtitleProviderClient = &http.Client{Transport: fakeSubtitleProviderTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Host != "api.opensubtitles.com" {
			t.Errorf("unexpected destination %q", r.Host)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"a","attributes":{"language":"en","release":"Example.1080p","files":[{"file_id":101,"file_name":"Example.en.srt"}]}}]}`))}, nil
	})}
	search := func(h http.Handler, query string) (int, map[string]any) {
		r := subtitleDecisionRequest(t, h, "POST", "/api/v1/subtitles/parts/part/published/search", map[string]string{"query": query}, token)
		var body map[string]any
		if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return r.Code, body
	}
	if status, body := search(NewRouter(deps), "Example 2026"); status != 200 || body["cached"] != false || calls.Load() != 1 {
		t.Fatalf("initial lookup %d %+v calls=%d", status, body, calls.Load())
	}
	if status, body := search(NewRouter(deps), "Example 2026"); status != 200 || body["cached"] != true || calls.Load() != 1 {
		t.Fatalf("persisted cache %d %+v calls=%d", status, body, calls.Load())
	}
	var metadata string
	if err := deps.Catalog.DB().QueryRow(`SELECT candidates_json FROM subtitle_provider_cache WHERE part_id='part'`).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(metadata, "cache-test-key") || strings.Contains(metadata, "/volume1") || !strings.Contains(metadata, "Example.1080p") {
		t.Fatalf("cache leaked secrets or lacked metadata: %s", metadata)
	}
	if _, err := deps.Catalog.DB().Exec(`UPDATE subtitle_provider_cache SET expires_at_ms=0`); err != nil {
		t.Fatal(err)
	}
	if status, body := search(NewRouter(deps), "Example 2026"); status != 200 || body["cached"] != false || calls.Load() != 2 {
		t.Fatalf("expired cache should refresh %d %+v calls=%d", status, body, calls.Load())
	}
}

func TestPublishedProvider429CooldownPersistsAndIsSharedAcrossQueries(t *testing.T) {
	deps, token, _ := subtitleDecisionFixture(t)
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_API_KEY", "rate-limit-test-key")
	var calls atomic.Int32
	deps.SubtitleProviderClient = &http.Client{Transport: fakeSubtitleProviderTransport(func(r *http.Request) (*http.Response, error) {
		number := calls.Add(1)
		status := 429
		if number > 1 {
			status = 200
		}
		body := `{"data":[]}`
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	search := func(query string) int {
		return subtitleDecisionRequest(t, NewRouter(deps), "POST", "/api/v1/subtitles/parts/part/published/search", map[string]string{"query": query}, token).Code
	}
	if result := search("Alpha Movie"); result != 429 || calls.Load() != 1 {
		t.Fatalf("initial quota %d calls %d", result, calls.Load())
	}
	if result := search("Bravo Movie"); result != 429 || calls.Load() != 1 {
		t.Fatalf("cooldown bypassed %d calls %d", result, calls.Load())
	}
	if _, err := deps.Catalog.DB().Exec(`UPDATE subtitle_provider_cooldown SET blocked_until_ms=0`); err != nil {
		t.Fatal(err)
	}
	if result := search("Bravo Movie"); result != 200 || calls.Load() != 2 {
		t.Fatalf("cooldown expiry %d calls %d", result, calls.Load())
	}
}

func TestPublishedSearchConcurrentSameKeySingleCall(t *testing.T) {
	deps, token, _ := subtitleDecisionFixture(t)
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_API_KEY", "duplicate-test-key")
	var calls atomic.Int32
	deps.SubtitleProviderClient = &http.Client{Transport: fakeSubtitleProviderTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
	})}
	handler := NewRouter(deps)
	const n = 12
	var wg sync.WaitGroup
	problems := make(chan int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := subtitleDecisionRequest(t, handler, "POST", "/api/v1/subtitles/parts/part/published/search", map[string]string{"query": "Same Movie"}, token)
			if r.Code != 200 {
				problems <- r.Code
			}
		}()
	}
	wg.Wait()
	close(problems)
	for status := range problems {
		t.Errorf("concurrent lookup failed %d", status)
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent duplicate search made %d provider requests", calls.Load())
	}
}

func TestPublishedSearchInvalidProviderResultNeverCached(t *testing.T) {
	deps, token, _ := subtitleDecisionFixture(t)
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_API_KEY", "broken-response-key")
	var calls atomic.Int32
	deps.SubtitleProviderClient = &http.Client{Transport: fakeSubtitleProviderTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{broken"))}, nil
	})}
	for i := 0; i < 2; i++ {
		r := subtitleDecisionRequest(t, NewRouter(deps), "POST", "/api/v1/subtitles/parts/part/published/search", map[string]string{"query": "Invalid Response"}, token)
		if r.Code != 502 {
			t.Fatalf("invalid provider body %d %s", r.Code, r.Body.String())
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("failed provider results cached %d times", calls.Load())
	}
}

func TestPublishedSearchEmptyResultsCachedAndNormalized(t *testing.T) {
	deps, token, _ := subtitleDecisionFixture(t)
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_API_KEY", "normalized-key")
	var calls atomic.Int32
	deps.SubtitleProviderClient = &http.Client{Transport: fakeSubtitleProviderTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
	})}
	h := NewRouter(deps)
	for i, q := range []string{"Example   2026", " example 2026 "} {
		r := subtitleDecisionRequest(t, h, "POST", "/api/v1/subtitles/parts/part/published/search", map[string]string{"query": q}, token)
		if r.Code != 200 {
			t.Fatalf("query %d status %d: %s", i, r.Code, r.Body.String())
		}
		var reply struct {
			Candidates []any `json:"candidates"`
			Cached     bool  `json:"cached"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Candidates == nil || len(reply.Candidates) != 0 || reply.Cached != (i == 1) {
			t.Fatalf("query %d unexpected reply %+v", i, reply)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("repeated empty search triggered provider %d times", calls.Load())
	}
	// No provider cache can bypass a subsequent user skip.
	if skip := subtitleDecisionRequest(t, h, "PUT", "/api/v1/subtitles/parts/part/decision",
		map[string]string{"disposition": "skip", "reason": "not relevant"}, token); skip.Code != 200 {
		t.Fatal(skip.Body.String())
	}
	blocked := subtitleDecisionRequest(t, h, "POST", "/api/v1/subtitles/parts/part/published/search",
		map[string]string{"query": "Example 2026"}, token)
	if blocked.Code != 409 || calls.Load() != 1 {
		t.Fatalf("skip bypassed using cache %d calls %d", blocked.Code, calls.Load())
	}
}
