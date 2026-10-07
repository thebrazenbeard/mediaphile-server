package events

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/netguard"
)

var ErrWebhookTargetRejected = errors.New("webhook target is outside the local network")

type Dispatcher struct {
	Client     *http.Client
	Retries    int
	RetryDelay time.Duration
}

func NewDispatcher(client *http.Client) *Dispatcher {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
			Proxy: nil, DialContext: dialLAN, TLSHandshakeTimeout: 5 * time.Second,
			DisableKeepAlives: true,
		}}
	}
	// Redirects must not bypass target validation, including injected clients.
	safe := *client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Dispatcher{Client: &safe, Retries: 3, RetryDelay: 100 * time.Millisecond}
}

// dialLAN repeats DNS validation immediately before opening a connection, and
// dials a vetted address rather than resolving a hostname a second time.
func dialLAN(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrWebhookTargetRejected
	}
	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{ip}
	} else {
		addrs, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, ErrWebhookTargetRejected
		}
	}
	if len(addrs) == 0 {
		return nil, ErrWebhookTargetRejected
	}
	for _, a := range addrs {
		if !netguard.IsDefaultAllowed(a) {
			return nil, ErrWebhookTargetRejected
		}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(addrs[0].String(), port))
}

func Signature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (d *Dispatcher) ValidateTarget(ctx context.Context, target string) error {
	u, err := url.Parse(target)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ErrWebhookTargetRejected
	}
	host := u.Hostname()
	if addr, err := netip.ParseAddr(host); err == nil {
		if !netguard.IsDefaultAllowed(addr) {
			return ErrWebhookTargetRejected
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return ErrWebhookTargetRejected
	}
	for _, addr := range addrs {
		if !netguard.IsDefaultAllowed(addr) {
			return ErrWebhookTargetRejected
		}
	}
	return nil
}

func (d *Dispatcher) Deliver(ctx context.Context, target, secret string, event Event) error {
	if err := d.ValidateTarget(ctx, target); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"version": 1, "event": event})
	if err != nil {
		return err
	}
	tries := d.Retries
	if tries < 1 {
		tries = 1
	}
	var last error
	for attempt := 0; attempt < tries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Mediaphile-Signature", Signature(secret, body))
		resp, err := d.Client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			last = fmt.Errorf("webhook HTTP status %d", resp.StatusCode)
		} else {
			last = err
		}
		if attempt+1 < tries && d.RetryDelay > 0 {
			timer := time.NewTimer(d.RetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return last
}

type WebhookService struct {
	repo       *catalog.Repository
	bus        *Bus
	dispatcher *Dispatcher
	ch         <-chan Event
	cancel     func()
	inflight   chan struct{}
}

func NewWebhookService(repo *catalog.Repository, bus *Bus, dispatcher *Dispatcher) *WebhookService {
	if bus == nil {
		bus = NewBus()
	}
	if dispatcher == nil {
		dispatcher = NewDispatcher(nil)
	}
	ch, cancel := bus.Subscribe(64)
	return &WebhookService{repo: repo, bus: bus, dispatcher: dispatcher, ch: ch, cancel: cancel, inflight: make(chan struct{}, 8)}
}

func (s *WebhookService) Create(ctx context.Context, target string, eventTypes []string) (catalog.WebhookSubscription, string, error) {
	if err := s.dispatcher.ValidateTarget(ctx, target); err != nil {
		return catalog.WebhookSubscription{}, "", err
	}
	types := normalizeEventTypes(eventTypes)
	if len(types) == 0 {
		return catalog.WebhookSubscription{}, "", fmt.Errorf("at least one event type is required")
	}
	id, err := randomWebhookToken(12)
	if err != nil {
		return catalog.WebhookSubscription{}, "", err
	}
	secret, err := randomWebhookToken(32)
	if err != nil {
		return catalog.WebhookSubscription{}, "", err
	}
	typeJSON, err := json.Marshal(types)
	if err != nil {
		return catalog.WebhookSubscription{}, "", err
	}
	sum := sha256.Sum256([]byte(secret))
	v := catalog.WebhookSubscription{
		ID: "wh_" + id, TargetURL: target, EventTypes: string(typeJSON), SecretHash: hex.EncodeToString(sum[:]), SecretValue: secret, Enabled: true,
	}
	if err := s.repo.CreateWebhookSubscription(ctx, v); err != nil {
		return catalog.WebhookSubscription{}, "", err
	}
	return v, secret, nil
}

func (s *WebhookService) List(ctx context.Context) ([]catalog.WebhookSubscription, error) {
	return s.repo.ListWebhookSubscriptions(ctx)
}

func (s *WebhookService) Delete(ctx context.Context, id string) error {
	return s.repo.DeleteWebhookSubscription(ctx, id)
}

func (s *WebhookService) Run(ctx context.Context) {
	defer s.cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.ch:
			if !ok {
				return
			}
			subscriptions, err := s.repo.ListWebhookSubscriptions(ctx)
			if err != nil {
				continue
			}
			for _, sub := range subscriptions {
				if !subscriptionMatches(sub.EventTypes, event.Type) {
					continue
				}
				sub := sub
				select {
				case s.inflight <- struct{}{}:
					go func() {
						defer func() { <-s.inflight }()
						_ = s.dispatcher.Deliver(ctx, sub.TargetURL, sub.SecretValue, event)
					}()
				default:
					// A slow webhook must not stall the event loop or playback.
				}
			}
		}
	}
}

func subscriptionMatches(raw, eventType string) bool {
	var types []string
	if json.Unmarshal([]byte(raw), &types) != nil {
		return false
	}
	for _, v := range types {
		if v == eventType {
			return true
		}
	}
	return false
}

func normalizeEventTypes(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func randomWebhookToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
