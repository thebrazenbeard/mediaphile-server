package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/auth"
	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
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

func TestPublishedStageManualReviewAndInstallRoute(t *testing.T) {
	deps, token, video := subtitleDecisionFixture(t)
	viewerHash, err := auth.HashPassword("viewer-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.CreatePrincipal(context.Background(), catalog.Principal{ID: "subtitle-viewer", Username: "subtitle-viewer", PasswordHash: viewerHash, Admin: false}); err != nil {
		t.Fatal(err)
	}
	_, viewerToken, err := deps.Auth.Login(context.Background(), "subtitle-viewer", "viewer-password")
	if err != nil {
		t.Fatal(err)
	}
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
	staged := subtitleDecisionRequest(t, h, "POST", "/api/v1/subtitles/parts/part/published/stage", map[string]int{"fileId": 123}, token)
	if staged.Code != 201 || calls != 2 || strings.Contains(staged.Body.String(), "outputSrt") || strings.Contains(staged.Body.String(), stage) {
		t.Fatalf("invalid staged reply: %d %s", staged.Code, staged.Body.String())
	}
	var result struct { Stage struct { StageID string `json:"stageId"`; SHA256 string `json:"sha256"` } `json:"stage"` }
	if err := json.Unmarshal(staged.Body.Bytes(), &result); err != nil || len(result.Stage.StageID) != 32 || len(result.Stage.SHA256) != 64 {
		t.Fatalf("invalid stage reference: %+v %v", result, err)
	}
	base := "/api/v1/subtitles/parts/part/published/stages/" + result.Stage.StageID
	unauth := subtitleDecisionRequest(t, h, "GET", base, nil, "")
	if unauth.Code != 401 {
		t.Fatalf("unauthenticated preview returned %d", unauth.Code)
	}
	viewerPreview := subtitleDecisionRequest(t, h, "GET", base, nil, viewerToken)
	if viewerPreview.Code != 403 {
		t.Fatalf("viewer preview returned %d", viewerPreview.Code)
	}
	preview := subtitleDecisionRequest(t, h, "GET", base, nil, token)
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), "Hello.") ||
		preview.Header().Get("Cache-Control") != "no-store" || preview.Header().Get("X-Content-Type-Options") != "nosniff" || calls != 2 {
		t.Fatalf("review did not stay read-only: %d %s calls=%d", preview.Code, preview.Body.String(), calls)
	}
	uri := base + "/install"
	noAuth := subtitleDecisionRequest(t, h, "POST", uri, map[string]any{"expectedSha256": result.Stage.SHA256, "confirmReviewed": true}, "")
	if noAuth.Code != 401 {
		t.Fatalf("unauthenticated install returned %d", noAuth.Code)
	}
	viewerInstall := subtitleDecisionRequest(t, h, "POST", uri, map[string]any{"expectedSha256": result.Stage.SHA256, "confirmReviewed": true}, viewerToken)
	if viewerInstall.Code != 403 {
		t.Fatalf("viewer install returned %d", viewerInstall.Code)
	}
	noReview := subtitleDecisionRequest(t, h, "POST", uri, map[string]any{"expectedSha256": result.Stage.SHA256, "confirmReviewed": false}, token)
	if noReview.Code != 409 {
		t.Fatalf("unreviewed install returned %d", noReview.Code)
	}
	wrongSHA := subtitleDecisionRequest(t, h, "POST", uri, map[string]any{"expectedSha256": strings.Repeat("0", 64), "confirmReviewed": true}, token)
	if wrongSHA.Code != 409 {
		t.Fatalf("wrong digest installed %d", wrongSHA.Code)
	}
	sidecar := filepath.Join(filepath.Dir(video), "Example (2026).en.srt")
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Fatalf("unreviewed or wrong digest created sidecar: %v", err)
	}
	installed := subtitleDecisionRequest(t, h, "POST", uri, map[string]any{"expectedSha256": result.Stage.SHA256, "confirmReviewed": true}, token)
	if installed.Code != 201 || !strings.Contains(installed.Body.String(), `"alreadyInstalled":false`) ||
		strings.Contains(installed.Body.String(), filepath.Dir(video)) || calls != 2 {
		t.Fatalf("manual install unexpected %d %s calls=%d", installed.Code, installed.Body.String(), calls)
	}
	again := subtitleDecisionRequest(t, h, "POST", uri, map[string]any{"expectedSha256": result.Stage.SHA256, "confirmReviewed": true}, token)
	if again.Code != 200 || !strings.Contains(again.Body.String(), `"alreadyInstalled":true`) || calls != 2 {
		t.Fatalf("idempotent retry unexpected %d %s", again.Code, again.Body.String())
	}
	skip := subtitleDecisionRequest(t, h, "PUT", "/api/v1/subtitles/parts/part/decision", map[string]string{"disposition": "skip", "reason": "manual hold"}, token)
	if skip.Code != 200 {
		t.Fatalf("could not hold media: %s", skip.Body.String())
	}
	held := subtitleDecisionRequest(t, h, "POST", uri, map[string]any{"expectedSha256": result.Stage.SHA256, "confirmReviewed": true}, token)
	if held.Code != 409 {
		t.Fatalf("held install returned %d", held.Code)
	}
}

func TestPublishedSubtitleAdminRoutesFailClosedWithoutAuth(t *testing.T) {
	deps, _, _ := subtitleDecisionFixture(t)
	deps.Auth = nil
	h := NewRouter(deps)
	for _, tc := range []struct {
		method string
		path string
		body any
	}{
		{"POST", "/api/v1/subtitles/parts/part/published/search", map[string]any{"query": "movie"}},
		{"POST", "/api/v1/subtitles/parts/part/published/stage", map[string]any{"fileId": 1}},
		{"GET", "/api/v1/subtitles/parts/part/published/stages/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil},
		{"POST", "/api/v1/subtitles/parts/part/published/stages/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/install", map[string]any{"expectedSha256": strings.Repeat("a", 64), "confirmReviewed": true}},
	} {
		r := subtitleDecisionRequest(t, h, tc.method, tc.path, tc.body, "")
		if r.Code != http.StatusServiceUnavailable || !strings.Contains(r.Body.String(), "AUTH_UNAVAILABLE") {
			t.Fatalf("nil auth admitted admin endpoint %s %s: %d %s", tc.method, tc.path, r.Code, r.Body.String())
		}
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
