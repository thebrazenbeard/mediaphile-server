package httpapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeSubtitleProviderTransport func(*http.Request) (*http.Response, error)

func (f fakeSubtitleProviderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestPublishedSubtitleSearchIsExplicitAndFailClosed(t *testing.T) {
	deps, token, _ := subtitleDecisionFixture(t)
	path := "/api/v1/subtitles/parts/part/published/search"
	called := 0
	deps.SubtitleProviderClient = &http.Client{Transport: fakeSubtitleProviderTransport(func(r *http.Request) (*http.Response, error) {
		called++
		if !strings.Contains(r.URL.String(), "query=example+2026") {
			t.Errorf("unexpected title query %s", r.URL.String())
		}
		body := `{"data":[{"id":"good","attributes":{"language":"en","release":"Example.WEB","files":[{"file_id":123,"file_name":"Example.en.srt"}]}}]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	h := NewRouter(deps)
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_API_KEY", "")
	disabled := subtitleDecisionRequest(t, h, "POST", path, map[string]string{"query": "Example 2026"}, token)
	if disabled.Code != 503 || called != 0 {
		t.Fatalf("provider misconfigured %d calls %d", disabled.Code, called)
	}
	t.Setenv("MEDIAPHILE_OPENSUBTITLES_API_KEY", "test-key")
	unauth := subtitleDecisionRequest(t, h, "POST", path, map[string]string{"query": "Example 2026"}, "")
	if unauth.Code != 401 || called != 0 {
		t.Fatalf("unauth %d calls %d", unauth.Code, called)
	}
	invalid := subtitleDecisionRequest(t, h, "POST", path, map[string]string{"query": "/volume1/Movies/secret.mkv"}, token)
	if invalid.Code != 400 || called != 0 {
		t.Fatalf("path leak risk %d calls %d", invalid.Code, called)
	}
	badID := subtitleDecisionRequest(t, h, "POST", "/api/v1/subtitles/parts/nope/published/search", map[string]string{"query": "Example 2026"}, token)
	if badID.Code != 404 || called != 0 {
		t.Fatalf("unknown part %d calls %d", badID.Code, called)
	}
	good := subtitleDecisionRequest(t, h, "POST", path, map[string]string{"query": "Example 2026"}, token)
	if good.Code != 200 || called != 1 || !strings.Contains(good.Body.String(), `"fileId":123`) {
		t.Fatalf("explicit search %d %s calls %d", good.Code, good.Body.String(), called)
	}
	if strings.Contains(good.Body.String(), "test-key") {
		t.Fatal("provider secret exposed")
	}
	decision := subtitleDecisionRequest(t, h, "PUT", "/api/v1/subtitles/parts/part/decision", map[string]string{"disposition": "skip", "reason": "user skip"}, token)
	if decision.Code != 200 {
		t.Fatalf("skip failed %d", decision.Code)
	}
	held := subtitleDecisionRequest(t, h, "POST", path, map[string]string{"query": "Example 2026"}, token)
	if held.Code != 409 || called != 1 {
		t.Fatalf("skip did not prevent external search %d %d", held.Code, called)
	}
}
