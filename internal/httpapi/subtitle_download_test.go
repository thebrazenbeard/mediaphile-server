package httpapi

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedStageRequiresAdminConfigurationAndActiveDecision(t *testing.T) {
	deps, token, _ := subtitleDecisionFixture(t)
	path := "/api/v1/subtitles/parts/part/published/stage"
	called := 0
	deps.SubtitleProviderClient = &http.Client{Transport: fakeSubtitleProviderTransport(func(*http.Request) (*http.Response, error) {
		called++
		return nil, nil
	})}
	h := NewRouter(deps)
	stage := t.TempDir()
	t.Setenv("MEDIAPHILE_SUBTITLE_STAGE_ROOT", stage)
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_API_KEY", "")
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_TOKEN", "")
	unauth := subtitleDecisionRequest(t, h, "POST", path, map[string]int{"fileId": 123}, "")
	if unauth.Code != 401 {
		t.Fatalf("unauthenticated stage returned %d", unauth.Code)
	}
	missing := subtitleDecisionRequest(t, h, "POST", path, map[string]int{"fileId": 123}, token)
	if missing.Code != 503 || called != 0 {
		t.Fatalf("unconfigured stage returned %d, calls %d", missing.Code, called)
	}
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_API_KEY", "test-key")
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_TOKEN", "test-token")
	bad := subtitleDecisionRequest(t, h, "POST", path, map[string]int{"fileId": -1}, token)
	if bad.Code != 400 || called != 0 {
		t.Fatalf("invalid ID returned %d, calls %d", bad.Code, called)
	}
	unknown := subtitleDecisionRequest(t, h, "POST", "/api/v1/subtitles/parts/not-here/published/stage", map[string]int{"fileId": 123}, token)
	if unknown.Code != 404 || called != 0 {
		t.Fatalf("unknown media returned %d calls %d", unknown.Code, called)
	}
	skip := subtitleDecisionRequest(t, h, "PUT", "/api/v1/subtitles/parts/part/decision", map[string]string{"disposition": "skip", "reason": "approved exclusion"}, token)
	if skip.Code != 200 {
		t.Fatal(skip.Body.String())
	}
	held := subtitleDecisionRequest(t, h, "POST", path, map[string]int{"fileId": 123}, token)
	if held.Code != 409 || called != 0 {
		t.Fatalf("skipped media reached provider: %d calls %d", held.Code, called)
	}
}

func TestPublishedStageEndpointLeavesMediaUntouched(t *testing.T) {
	deps, token, video := subtitleDecisionFixture(t)
	stage := t.TempDir()
	t.Setenv("MEDIAPHILE_SUBTITLE_STAGE_ROOT", stage)
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_API_KEY", "test-key")
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_TOKEN", "test-token")
	calls := 0
	deps.SubtitleProviderClient = &http.Client{Transport: fakeSubtitleProviderTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		body := `{"link":"https://www.opensubtitles.com/download/signed/subfile/test.srt"}`
		if calls == 2 {
			body = "1\n00:00:01,000 --> 00:00:02,000\nHello.\n"
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	h := NewRouter(deps)
	r := subtitleDecisionRequest(t, h, "POST", "/api/v1/subtitles/parts/part/published/stage", map[string]int{"fileId": 123}, token)
	if r.Code != 201 || calls != 2 || !strings.Contains(r.Body.String(), `"readyForReview":true`) || !strings.Contains(r.Body.String(), `"published":false`) {
		t.Fatalf("stage %d %s calls %d", r.Code, r.Body.String(), calls)
	}
	if entries, err := os.ReadDir(stage); err != nil || len(entries) != 1 {
		t.Fatalf("staging output missing: %v %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(video), "Example (2026).en.srt")); !os.IsNotExist(err) {
		t.Fatalf("unexpected NAS-style sidecar created: %v", err)
	}
}
