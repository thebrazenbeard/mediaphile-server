package subtitles

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validPublishedSRT = "1\n00:00:01,000 --> 00:00:02,000\nHello there.\n"

func publishedStageFixture(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	mediaDir := filepath.Join(base, "media")
	stage := filepath.Join(base, "stage")
	for _, dir := range []string{mediaDir, stage} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	video := filepath.Join(mediaDir, "movie.mkv")
	if err := os.WriteFile(video, []byte("unaltered-video"), 0600); err != nil {
		t.Fatal(err)
	}
	return video, stage
}

func publishedResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestStageOpenSubtitlesExplicitTwoStepAndNoMediaWrite(t *testing.T) {
	video, stage := publishedStageFixture(t)
	calls := 0
	client := &http.Client{Transport: subtitleRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			if req.Method != http.MethodPost || req.URL.String() != "https://api.opensubtitles.com/api/v1/download" ||
				req.Header.Get("Api-Key") != "test-key" || req.Header.Get("Authorization") != "Bearer test-token" {
				t.Fatalf("invalid download request %s %s headers=%v", req.Method, req.URL, req.Header)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil || !strings.Contains(string(body), `"file_id":123`) {
				t.Fatalf("invalid download request body %s: %v", body, err)
			}
			return publishedResponse(200, `{"link":"https://www.opensubtitles.com/download/signed/subfile/example.srt","file_name":"example.srt"}`), nil
		case 2:
			if req.Method != http.MethodGet || req.URL.Host != "www.opensubtitles.com" ||
				req.Header.Get("Api-Key") != "" || req.Header.Get("Authorization") != "" {
				t.Fatalf("credential leak to download host: %s %s %v", req.Method, req.URL, req.Header)
			}
			return publishedResponse(200, validPublishedSRT), nil
		default:
			t.Fatal("unexpected extra network call")
			return nil, nil
		}
	})}
	result, err := StageOpenSubtitles(context.Background(), client, "test-key", "test-token", 123, "part-1", video, stage, 6000)
	if err != nil || calls != 2 {
		t.Fatalf("stage failed: result=%+v err=%v calls=%d", result, err, calls)
	}
	if !result.ReadyForReview || result.FileID != 123 || result.Audit.Cues != 1 || result.SHA256 != result.Audit.SHA256 {
		t.Fatalf("incorrect staged audit %+v", result)
	}
	got, err := os.ReadFile(result.OutputSRT)
	if err != nil || string(got) != validPublishedSRT || !strings.HasPrefix(result.OutputSRT, stage+string(os.PathSeparator)) {
		t.Fatalf("not isolated staging %q %v", result.OutputSRT, err)
	}
	videoData, err := os.ReadFile(video)
	if err != nil || string(videoData) != "unaltered-video" {
		t.Fatalf("media modified %q %v", videoData, err)
	}
}

func TestStageOpenSubtitlesInLibraryRejectsSwappedAncestorBeforeProvider(t *testing.T) {
	base := t.TempDir()
	library := filepath.Join(base, "library")
	season := filepath.Join(library, "Travelers", "Season 01")
	outside := filepath.Join(base, "outside")
	stage := filepath.Join(base, "private-stage")
	for _, dir := range []string{season, outside, stage} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	video := filepath.Join(season, "Travelers - S01E01 - Travelers.mkv")
	media := []byte("same-size-unchanged-looking-media")
	if err := os.WriteFile(video, media, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(video)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &http.Client{Transport: subtitleRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls%2 == 1 {
			return publishedResponse(200, `{"link":"https://www.opensubtitles.com/download/signed/example.srt"}`), nil
		}
		return publishedResponse(200, validPublishedSRT), nil
	})}
	stageOne, err := StageOpenSubtitlesInLibrary(context.Background(), client, "key", "token", 123,
		"part-1", library, video, stage, info.Size(), info.ModTime().UnixNano(), 6000)
	if err != nil || calls != 2 || !stageOne.ReadyForReview {
		t.Fatalf("in-root media unexpectedly blocked: %+v %v providerCalls=%d", stageOne, err, calls)
	}
	if err := os.Rename(season, filepath.Join(base, "saved-season")); err != nil {
		t.Fatal(err)
	}
	outsideVideo := filepath.Join(outside, filepath.Base(video))
	if err := os.WriteFile(outsideVideo, media, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(outsideVideo, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, season); err != nil {
		t.Skipf("cannot create ancestor symlink: %v", err)
	}
	_, err = StageOpenSubtitlesInLibrary(context.Background(), client, "key", "token", 123,
		"part-1", library, video, stage, info.Size(), info.ModTime().UnixNano(), 6000)
	if err == nil || calls != 2 {
		t.Fatalf("outside-library ancestor accepted before provider: err=%v providerCalls=%d", err, calls)
	}
}

func TestStageOpenSubtitlesInLibraryRejectsSiblingStageInsideLibraryBeforeProvider(t *testing.T) {
	base := t.TempDir()
	library := filepath.Join(base, "library")
	season := filepath.Join(library, "Travelers", "Season 01")
	stage := filepath.Join(library, "private-staging")
	for _, dir := range []string{season, stage} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	video := filepath.Join(season, "Travelers - S01E01.mkv")
	if err := os.WriteFile(video, []byte("synthetic-media"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(video)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &http.Client{Transport: subtitleRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return publishedResponse(200, validPublishedSRT), nil
	})}
	_, err = StageOpenSubtitlesInLibrary(context.Background(), client, "key", "token", 123,
		"part-1", library, video, stage, info.Size(), info.ModTime().UnixNano(), 6000)
	if err == nil || calls != 0 {
		t.Fatalf("inside-library sibling stage accepted or contacted provider: err=%v calls=%d", err, calls)
	}
	entries, err := os.ReadDir(stage)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected stage created artifacts: %v %v", entries, err)
	}
}

func TestStageOpenSubtitlesInLibraryRejectsStageRootMovedDuringProviderCall(t *testing.T) {
	base := t.TempDir()
	library := filepath.Join(base, "library")
	season := filepath.Join(library, "Travelers", "Season 01")
	outside := filepath.Join(base, "outside")
	inside := filepath.Join(library, "private-stage")
	for _, dir := range []string{season, filepath.Join(outside, "stage"), filepath.Join(inside, "stage")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	video := filepath.Join(season, "Travelers - S01E01.mkv")
	if err := os.WriteFile(video, []byte("synthetic-media"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(video)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "stage-alias")
	if err := os.Symlink(outside, alias); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	calls := 0
	client := &http.Client{Transport: subtitleRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(inside, alias); err != nil {
				t.Fatal(err)
			}
			return publishedResponse(200, `{"link":"https://www.opensubtitles.com/download/signed/example.srt"}`), nil
		}
		return publishedResponse(200, validPublishedSRT), nil
	})}
	_, err = StageOpenSubtitlesInLibrary(context.Background(), client, "key", "token", 123,
		"part-1", library, video, filepath.Join(alias, "stage"), info.Size(), info.ModTime().UnixNano(), 6000)
	if err == nil || calls != 2 {
		t.Fatalf("relocated staging accepted: err=%v calls=%d", err, calls)
	}
	for _, stage := range []string{filepath.Join(outside, "stage"), filepath.Join(inside, "stage")} {
		entries, err := os.ReadDir(stage)
		if err != nil || len(entries) != 0 {
			t.Fatalf("relocated staging left artifacts at %s: %v %v", stage, entries, err)
		}
	}
}

func TestStageOpenSubtitlesInLibraryRejectsEqualMetadataReplacement(t *testing.T) {
	video, stage := publishedStageFixture(t)
	info, err := os.Stat(video)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &http.Client{Transport: subtitleRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			if err := os.Rename(video, video+".original"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(video, []byte("unaltered-video"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(video, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			return publishedResponse(200, `{"link":"https://www.opensubtitles.com/download/signed/example.srt"}`), nil
		}
		return publishedResponse(200, validPublishedSRT), nil
	})}
	_, err = StageOpenSubtitlesInLibrary(context.Background(), client, "key", "token", 123,
		"part-1", filepath.Dir(video), video, stage, info.Size(), info.ModTime().UnixNano(), 6000)
	if err == nil || calls != 2 {
		t.Fatalf("replaced media accepted: err=%v providerCalls=%d", err, calls)
	}
	entries, err := os.ReadDir(stage)
	if err != nil || len(entries) != 0 {
		t.Fatalf("replaced media left staged output: %v %v", entries, err)
	}
}

func TestStageOpenSubtitlesRejectsUntrustedLinksBeforeGET(t *testing.T) {
	for _, link := range []string{
		"http://www.opensubtitles.com/download/unsafe",
		"https://127.0.0.1/download/private",
		"https://www.opensubtitles.com.evil.invalid/download/file",
		"https://www.opensubtitles.com:444/download/file",
		"https://name@www.opensubtitles.com/download/file",
		"https://www.opensubtitles.com/not-a-download/file",
	} {
		t.Run(link, func(t *testing.T) {
			video, stage := publishedStageFixture(t)
			calls := 0
			client := &http.Client{Transport: subtitleRoundTripper(func(req *http.Request) (*http.Response, error) {
				calls++
				return publishedResponse(200, `{"link":`+strconvQuote(link)+`,"file_name":"test.srt"}`), nil
			})}
			_, err := StageOpenSubtitles(context.Background(), client, "key", "token", 4, "part-1", video, stage, 6000)
			if err == nil || calls != 1 {
				t.Fatalf("untrusted link accepted: %v calls=%d", err, calls)
			}
			entries, err := os.ReadDir(stage)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed stage must leave no output: %v %v", entries, err)
			}
		})
	}
}

func strconvQuote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }

func TestStageOpenSubtitlesRejectsBadPayloadAndQuota(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		quota  bool
	}{
		{"quota", 429, `{"error":"quota"}`, true},
		{"bad-json", 200, "bad-json", false},
		{"invalid-srt", 200, "not a subtitle", false},
		{"oversized-srt", 200, strings.Repeat("x", maxSRTBytes+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			video, stage := publishedStageFixture(t)
			calls := 0
			client := &http.Client{Transport: subtitleRoundTripper(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 && tc.name != "invalid-srt" && tc.name != "oversized-srt" {
					return publishedResponse(tc.status, tc.body), nil
				}
				if calls == 1 {
					return publishedResponse(200, `{"link":"https://www.opensubtitles.com/download/signed/subfile/test.srt"}`), nil
				}
				return publishedResponse(tc.status, tc.body), nil
			})}
			_, err := StageOpenSubtitles(context.Background(), client, "private-key", "private-token", 123, "part-1", video, stage, 6000)
			if err == nil || strings.Contains(err.Error(), "private-key") || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("wrong error: %v", err)
			}
			if tc.quota && !errors.Is(err, ErrSubtitleProviderRateLimited) {
				t.Fatalf("quota not typed: %v", err)
			}
			entries, err := os.ReadDir(stage)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failure left files: %v %v", entries, err)
			}
		})
	}
}

func TestStageOpenSubtitlesInvalidInputNeverContactsProvider(t *testing.T) {
	video, stage := publishedStageFixture(t)
	calls := 0
	client := &http.Client{Transport: subtitleRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected call")
	})}
	for _, tc := range []struct {
		key, bearer string
		id          int64
		root        string
	}{
		{"", "token", 5, stage},
		{"key", "", 5, stage},
		{"key", "token", -1, stage},
		{"key", "token", 5, filepath.Dir(video)},
		{"key", "token", 5, "relative-stage"},
	} {
		if _, err := StageOpenSubtitles(context.Background(), client, tc.key, tc.bearer, tc.id, "part-1", video, tc.root, 6000); err == nil {
			t.Fatalf("accepted invalid input: %+v", tc)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := StageOpenSubtitles(ctx, client, "key", "token", 5, "part-1", video, stage, 6000); err == nil || calls != 0 {
		t.Fatalf("canceled work reached provider: %v %d", err, calls)
	}
}

func TestStageOpenSubtitlesRejectsRedirectsAndTransportSecrets(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		video, stage := publishedStageFixture(t)
		calls := 0
		client := &http.Client{Transport: subtitleRoundTripper(func(r *http.Request) (*http.Response, error) {
			calls++
			if failAt == 3 && calls == 1 {
				return nil, errors.New("secret-token intentionally echoed from transport")
			}
			if calls == failAt {
				resp := publishedResponse(302, "")
				resp.Header.Set("Location", "https://untrusted.invalid/secret")
				return resp, nil
			}
			return publishedResponse(200, `{"link":"https://www.opensubtitles.com/download/signed/subfile/test.srt"}`), nil
		})}
		_, err := StageOpenSubtitles(context.Background(), client, "secret-key", "secret-token", 123, "part-1", video, stage, 6000)
		if err == nil || strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), "secret-token") {
			t.Fatalf("redirect or transport secret escaped: %v", err)
		}
		if calls != failAt && failAt != 3 {
			t.Fatalf("unexpected redirect followed: %d", calls)
		}
		entries, err := os.ReadDir(stage)
		if err != nil || len(entries) != 0 {
			t.Fatalf("failed operation left stage output: %v %v", entries, err)
		}
	}
}
