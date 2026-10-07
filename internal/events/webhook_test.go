package events

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func webhookRepo(t *testing.T) *catalog.Repository {
	t.Helper()
	db, err := catalog.Open(filepath.Join(t.TempDir(), "webhooks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return catalog.NewRepository(db)
}

func TestWebhookSignatureIsVerifiable(t *testing.T) {
	body := []byte(`{"type":"media.play"}`)
	secret := "secret"
	got := Signature(secret, body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("signature=%q want=%q", got, want)
	}
}

func TestWebhookRejectsPublicTarget(t *testing.T) {
	d := NewDispatcher(&http.Client{Timeout: time.Second})
	if err := d.ValidateTarget(context.Background(), "http://8.8.8.8/hook"); err == nil {
		t.Fatal("public target accepted")
	}
}

func TestWebhookFailureRetriesBoundedWithoutBlockingPublish(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer target.Close()
	repo := webhookRepo(t)
	bus := NewBus()
	d := NewDispatcher(target.Client())
	d.Retries = 3
	d.RetryDelay = 5 * time.Millisecond
	service := NewWebhookService(repo, bus, d)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, _, err := service.Create(ctx, target.URL, []string{"media.play"}); err != nil {
		t.Fatal(err)
	}
	go service.Run(ctx)
	start := time.Now()
	bus.Publish(Event{Type: "media.play", ItemID: "movie"})
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("publish blocked on webhook delivery")
	}
	deadline := time.Now().Add(time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d want=3", calls.Load())
	}
}

func TestWebhookRedirectDoesNotEscapeValidatedTarget(t *testing.T) {
	var escaped atomic.Int32
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escaped.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer outside.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, outside.URL, http.StatusFound)
	}))
	defer redirect.Close()
	d := NewDispatcher(nil)
	d.Retries = 1
	if err := d.Deliver(context.Background(), redirect.URL, "key", Event{Type: "media.play"}); err == nil {
		t.Fatal("redirect must not count as successful delivery")
	}
	if escaped.Load() != 0 {
		t.Fatalf("redirect was followed %d times", escaped.Load())
	}
}
