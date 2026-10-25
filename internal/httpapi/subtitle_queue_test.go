package httpapi

import (
	"context"
	"encoding/json"
	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type planReply struct {
	Planned int `json:"planned"`
	Items   []struct {
		PartID         string `json:"partId"`
		Action         string `json:"action"`
		Coverage       string `json:"coverage"`
		Disposition    string `json:"disposition"`
		CandidateCount int    `json:"candidateCount"`
	} `json:"items"`
	Total int `json:"total"`
}

func TestSubtitleQueueRefreshIsDurableIdempotentAndHonorsDecision(t *testing.T) {
	deps, token, video := subtitleDecisionFixture(t)
	h := NewRouter(deps)
	read := func() planReply {
		r := subtitleDecisionRequest(t, h, "GET", "/api/v1/libraries/lib/subtitles/queue?offset=0&limit=10", nil, token)
		if r.Code != 200 {
			t.Fatalf("queue read %d: %s", r.Code, r.Body.String())
		}
		var p planReply
		if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	refresh := func() planReply {
		r := subtitleDecisionRequest(t, h, "POST", "/api/v1/libraries/lib/subtitles/queue/refresh", map[string]int{"offset": 0, "limit": 10}, token)
		if r.Code != 200 {
			t.Fatalf("queue refresh %d: %s", r.Code, r.Body.String())
		}
		var p planReply
		if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := read(); p.Total != 0 {
		t.Fatalf("queue initially nonempty %+v", p)
	}
	p := refresh()
	if p.Planned != 1 || p.Total != 1 || len(p.Items) != 1 || p.Items[0].Action != "search_published" || p.Items[0].Coverage != "missing" {
		t.Fatalf("unexpected first plan %+v", p)
	}
	if p := read(); p.Total != 1 || p.Items[0].Action != "search_published" {
		t.Fatalf("not persistent %+v", p)
	}
	if p := refresh(); p.Total != 1 {
		t.Fatalf("duplicate queued records %+v", p)
	}
	r := subtitleDecisionRequest(t, h, "PUT", "/api/v1/subtitles/parts/part/decision", map[string]string{"disposition": "skip", "reason": "user approved"}, token)
	if r.Code != 200 {
		t.Fatalf("set skip %d", r.Code)
	}
	p = refresh()
	if p.Items[0].Action != "excluded" || p.Items[0].Disposition != "skip" || p.Items[0].Coverage != "missing" {
		t.Fatalf("skip treated as coverage %+v", p)
	}
	r = subtitleDecisionRequest(t, h, "PUT", "/api/v1/subtitles/parts/part/decision", map[string]string{"disposition": "active", "reason": ""}, token)
	if r.Code != 200 {
		t.Fatalf("unset skip %d", r.Code)
	}
	// Existing media is not rewritten. Only a sidecar fixture is added.
	sidecar := strings.TrimSuffix(video, filepath.Ext(video)) + ".en.srt"
	subtitle := `1
00:00:00,000 --> 00:00:01,000
Hello there.
`
	if err := os.WriteFile(sidecar, []byte(subtitle), 0600); err != nil {
		t.Fatal(err)
	}
	p = refresh()
	if p.Items[0].Action != "covered" || p.Items[0].Coverage != "external_english" || p.Total != 1 {
		t.Fatalf("existing subtitle was not recognized %+v", p)
	}
	if contents, err := os.ReadFile(sidecar); err != nil || string(contents) != subtitle {
		t.Fatalf("sidecar changed unexpectedly: %v", err)
	}
}

func TestSubtitleQueueRefreshValidationAndAuthorization(t *testing.T) {
	deps, token, _ := subtitleDecisionFixture(t)
	h := NewRouter(deps)
	path := "/api/v1/libraries/lib/subtitles/queue/refresh"
	unauthorized := subtitleDecisionRequest(t, h, "POST", path, map[string]int{"limit": 1}, "")
	if unauthorized.Code != 401 {
		t.Fatalf("missing token %d", unauthorized.Code)
	}
	for _, body := range []map[string]int{
		{"limit": 1000}, {"offset": -1, "limit": 1}, {"limit": -1}, {"limit": 0},
	} {
		r := subtitleDecisionRequest(t, h, "POST", path, body, token)
		if r.Code != 400 {
			t.Errorf("invalid %+v returned %d: %s", body, r.Code, r.Body.String())
		}
	}
	unknown := subtitleDecisionRequest(t, h, "POST", "/api/v1/libraries/absent/subtitles/queue/refresh", map[string]int{"limit": 1}, token)
	if unknown.Code != 404 {
		t.Fatalf("missing library %d", unknown.Code)
	}
}

func TestSubtitleQueueOffsetPagesRemainDistinctAndReadOnly(t *testing.T) {
	deps, token, video := subtitleDecisionFixture(t)
	other := filepath.Join(filepath.Dir(video), "Second (2026).mkv")
	if err := os.WriteFile(other, []byte("movie2"), 0600); err != nil {
		t.Fatal(err)
	}
	otherInfo, err := os.Stat(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.Catalog.UpsertMediaPart(context.Background(), catalog.MediaPart{
		ID: "part2", SourceID: "source", Path: other,
		Size: otherInfo.Size(), ModTimeNS: otherInfo.ModTime().UnixNano(), Available: true,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewRouter(deps)
	var ids []string
	for _, offset := range []int{0, 1} {
		r := subtitleDecisionRequest(t, h, "POST", "/api/v1/libraries/lib/subtitles/queue/refresh",
			map[string]int{"limit": 1, "offset": offset}, token)
		if r.Code != 200 {
			t.Fatalf("offset %d got %d: %s", offset, r.Code, r.Body.String())
		}
		var p planReply
		if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.Planned != 1 || p.Total != 2 || len(p.Items) != 1 {
			t.Fatalf("offset %d plan %+v", offset, p)
		}
		ids = append(ids, p.Items[0].PartID)
	}
	if ids[0] == ids[1] {
		t.Fatalf("pagination repeated same video: %v", ids)
	}
	r := subtitleDecisionRequest(t, h, "GET", "/api/v1/libraries/lib/subtitles/queue?limit=10", nil, token)
	var stored planReply
	if err := json.Unmarshal(r.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Total != 2 || len(stored.Items) != 2 {
		t.Fatalf("expected 2 stored rows %+v", stored)
	}
	for _, p := range []string{video, other} {
		sidecar := strings.TrimSuffix(p, filepath.Ext(p)) + ".en.srt"
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("planner wrote media sidecar %s: %v", sidecar, err)
		}
	}
}

func TestSubtitleQueueRefreshPaginatesAvailablePartsOnly(t *testing.T) {
	deps, token, video := subtitleDecisionFixture(t)
	// A retired part sorts before the active part and must not consume a page.
	retired := filepath.Join(filepath.Dir(video), "000-removed.mkv")
	if err := deps.Catalog.UpsertMediaPart(context.Background(), catalog.MediaPart{
		ID: "retired", SourceID: "source", Path: retired, Size: 1, Available: false,
	}); err != nil {
		t.Fatal(err)
	}
	all, err := deps.Catalog.ListPartsByLibrary(context.Background(), "lib")
	if err != nil || len(all) != 2 || all[0].Available || !all[1].Available {
		t.Fatalf("scanner must retain unavailable history: %+v, %v", all, err)
	}
	available, err := deps.Catalog.ListAvailablePartsByLibrary(context.Background(), "lib")
	if err != nil || len(available) != 1 || available[0].ID != "part" {
		t.Fatalf("subtitle planner included retired part: %+v, %v", available, err)
	}
	h := NewRouter(deps)
	r := subtitleDecisionRequest(t, h, "POST", "/api/v1/libraries/lib/subtitles/queue/refresh",
		map[string]int{"offset": 0, "limit": 1}, token)
	if r.Code != 200 {
		t.Fatalf("refresh %d: %s", r.Code, r.Body.String())
	}
	var page planReply
	if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Planned != 1 || len(page.Items) != 1 || page.Items[0].PartID != "part" {
		t.Fatalf("unavailable part consumed first page: %+v", page)
	}
	r = subtitleDecisionRequest(t, h, "POST", "/api/v1/libraries/lib/subtitles/queue/refresh",
		map[string]int{"offset": 1, "limit": 1}, token)
	if r.Code != 200 {
		t.Fatalf("end page %d: %s", r.Code, r.Body.String())
	}
	if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Planned != 0 || len(page.Items) != 0 {
		t.Fatalf("end page ignored available-only total: %+v", page)
	}
	r = subtitleDecisionRequest(t, h, "GET", "/api/v1/libraries/lib/subtitles?offset=0&limit=1", nil, token)
	if r.Code != 200 {
		t.Fatalf("overview %d: %s", r.Code, r.Body.String())
	}
	if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].PartID != "part" {
		t.Fatalf("unavailable part consumed overview page: %+v", page)
	}
	// An available part is also ineligible when its parent media source is
	// retired. Test both flags independently to protect the catalog query.
	source, err := deps.Catalog.GetMediaSource(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	source.Available = false
	if err := deps.Catalog.UpsertMediaSource(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	available, err = deps.Catalog.ListAvailablePartsByLibrary(context.Background(), "lib")
	if err != nil || len(available) != 0 {
		t.Fatalf("retired media source remains in subtitle planner: %+v, %v", available, err)
	}
}

func TestSubtitlePolicyBlocksManualCandidateInstallUntilRestored(t *testing.T) {
	deps, token, video := subtitleDecisionFixture(t)
	h := NewRouter(deps)
	set := subtitleDecisionRequest(t, h, "PUT", "/api/v1/subtitles/parts/part/decision",
		map[string]string{"disposition": "skip", "reason": "approved skip"}, token)
	if set.Code != 200 {
		t.Fatalf("skip %d", set.Code)
	}
	r := subtitleDecisionRequest(t, h, "POST", "/api/v1/subtitles/parts/part/install",
		map[string]string{"candidateId": "00000000000000000000000000000000"}, token)
	if r.Code != 409 || !strings.Contains(r.Body.String(), "SUBTITLE_DECISION_HOLD") {
		t.Fatalf("skipped item install %d %s", r.Code, r.Body.String())
	}
	out := strings.TrimSuffix(video, filepath.Ext(video)) + ".en.srt"
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("skip led to media file write: %v", err)
	}
}

func TestQueueTreatsBadExistingEnglishSRTAsReviewNotCovered(t *testing.T) {
	deps, token, video := subtitleDecisionFixture(t)
	sidecar := strings.TrimSuffix(video, filepath.Ext(video)) + ".en.srt"
	// Parseable but with a red flag: duration extends far beyond media.
	content := `1
00:00:00,000 --> 00:19:01,000
A clearly mistimed subtitle.
`
	if err := os.WriteFile(sidecar, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	h := NewRouter(deps)
	r := subtitleDecisionRequest(t, h, "POST", "/api/v1/libraries/lib/subtitles/queue/refresh",
		map[string]int{"limit": 10, "offset": 0}, token)
	if r.Code != 200 {
		t.Fatalf("refresh %d %s", r.Code, r.Body.String())
	}
	var p planReply
	if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].Action != "inspect_existing" {
		t.Fatalf("invalid sidecar treated as completed %+v", p)
	}
}

func TestSubtitleQueueRefreshFailsClosedWithoutPartialWrites(t *testing.T) {
	deps, token, _ := subtitleDecisionFixture(t)
	h := NewRouter(deps)
	refresh := func(limit int) *httptest.ResponseRecorder {
		return subtitleDecisionRequest(t, h, "POST", "/api/v1/libraries/lib/subtitles/queue/refresh",
			map[string]int{"offset": 0, "limit": limit}, token)
	}
	if r := refresh(1); r.Code != 200 {
		t.Fatalf("initial refresh: %d %s", r.Code, r.Body.String())
	}
	missingPath := filepath.Join(t.TempDir(), "ghost.mkv")
	if err := deps.Catalog.UpsertMediaPart(context.Background(), catalog.MediaPart{
		ID: "absent-on-disk", SourceID: "source", Path: missingPath, Size: 11, Available: true,
	}); err != nil {
		t.Fatal(err)
	}
	held := refresh(2)
	if held.Code != 409 || !strings.Contains(held.Body.String(), "QUEUE_REFRESH_HELD") {
		t.Fatalf("filesystem error should hold entire page: %d %s", held.Code, held.Body.String())
	}
	got := subtitleDecisionRequest(t, h, "GET", "/api/v1/libraries/lib/subtitles/queue?limit=10", nil, token)
	var page planReply
	if err := json.Unmarshal(got.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].PartID != "part" {
		t.Fatalf("failed page created partial records: %+v", page)
	}
}
