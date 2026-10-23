package subtitles

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type subtitleRoundTripper func(*http.Request) (*http.Response, error)

func (f subtitleRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenSubtitlesSearchReadOnlyAndSafeCandidateFilter(t *testing.T) {
	calls := 0
	transport := subtitleRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.String() != "https://api.opensubtitles.com/api/v1/subtitles?imdb_id=133093&languages=en" {
			t.Errorf("unexpected search request %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Api-Key") != "test-not-real" || !strings.Contains(r.Header.Get("User-Agent"), "Mediaphile") {
			t.Errorf("missing provider headers")
		}
		body := `{"total_count":4,"data":[
 {"id":"a","attributes":{"language":"en","release":"Matched.1080p.WEB","moviehash_match":true,"from_trusted":true,"files":[{"file_id":123,"file_name":"Good.srt"}]}},
 {"id":"b","attributes":{"language":"es","release":"Spanish","files":[{"file_id":124,"file_name":"Bad.srt"}]}},
 {"id":"c","attributes":{"language":"en","release":"Machine","machine_translated":true,"files":[{"file_id":125,"file_name":"Machine.srt"}]}},
 {"id":"d","attributes":{"language":"en","release":"Alternate.WEB","files":[{"file_id":126,"file_name":"AlsoGood.srt"}]}}
 ]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	got, err := SearchOpenSubtitles(context.Background(), &http.Client{Transport: transport}, "test-not-real", PublishedQuery{IMDbID: "tt0133093"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(got) != 2 {
		t.Fatalf("calls=%d candidates=%+v", calls, got)
	}
	if got[0].FileID != 123 || got[0].Release != "Matched.1080p.WEB" || !got[0].Trusted || !got[0].MoviehashMatch {
		t.Fatalf("candidate metadata wrong %+v", got[0])
	}
	if got[1].FileID != 126 || got[0].Provider != "opensubtitles" {
		t.Fatalf("wrong candidate records %+v", got)
	}
}

func TestOpenSubtitlesQueryValidationDoesNotCallProvider(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: subtitleRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("should not be called")
	})}
	for _, q := range []PublishedQuery{{}, {Query: "/volume1/Movies/secret.mkv"}, {Query: strings.Repeat("a", 250)}, {IMDbID: "bad"}, {Query: "valid", Year: 1000}} {
		_, err := SearchOpenSubtitles(context.Background(), client, "dummy", q)
		if err == nil {
			t.Errorf("accepted invalid query %+v", q)
		}
	}
	_, err := SearchOpenSubtitles(context.Background(), client, "", PublishedQuery{Query: "Movie"})
	if err == nil || calls != 0 {
		t.Fatalf("invalid auth triggered transport %d %v", calls, err)
	}
}

func TestOpenSubtitlesSearchFailClosedResponses(t *testing.T) {
	const secret = "test-key-that-must-never-appear"
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"quota", 429, `{"error":"quota"}`, ErrSubtitleProviderRateLimited},
		{"forbidden", 403, `{"error":"forbidden"}`, nil},
		{"redirect", 302, "", nil},
		{"invalid-json", 200, "{bad", nil},
		{"oversized", 200, strings.Repeat("z", 1<<20+10), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: subtitleRoundTripper(func(req *http.Request) (*http.Response, error) {
				calls++
				h := make(http.Header)
				if tc.status == 302 {
					h.Set("Location", "https://untrusted.invalid/steal")
				}
				return &http.Response{StatusCode: tc.status, Header: h, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			_, err := SearchOpenSubtitles(context.Background(), client, secret, PublishedQuery{Query: "Movie"})
			if err == nil || calls != 1 {
				t.Fatalf("expected one failed request, calls=%d err=%v", calls, err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), tc.body) && len(tc.body) > 50 {
				t.Fatalf("provider exposed credential/response: %v", err)
			}
		})
	}
}
func TestOpenSubtitlesTransportErrorMustNotExposeAPIKey(t *testing.T) {
	secret := "secret-not-for-logs"
	client := &http.Client{Transport: subtitleRoundTripper(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("proxy rejected " + r.Header.Get("Api-Key"))
	})}
	_, err := SearchOpenSubtitles(context.Background(), client, secret, PublishedQuery{Query: "Movie"})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("secret escaped into diagnostics: %v", err)
	}
}
func TestOpenSubtitlesCancellationAndEmptySuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	client := &http.Client{Transport: subtitleRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
	})}
	_, err := SearchOpenSubtitles(ctx, client, "secret", PublishedQuery{Query: "Movie"})
	if err == nil || calls != 0 {
		t.Fatalf("cancel not respected: %v %d", err, calls)
	}
	got, err := SearchOpenSubtitles(context.Background(), client, "secret", PublishedQuery{Query: "Movie", Year: 2026})
	if err != nil || len(got) != 0 || calls != 1 {
		t.Fatalf("empty success: %+v %v calls %d", got, err, calls)
	}
}
