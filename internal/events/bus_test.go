package events

import (
	"testing"
	"time"
)

func TestBusDoesNotBlockOnSlowSubscriber(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe(1)
	defer cancel()
	b.Publish(Event{Type: "media.play"})
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.Publish(Event{Type: "media.progress"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("publishing blocked on slow subscriber")
	}
	if b.Dropped() == 0 {
		t.Fatal("expected dropped events for saturated subscriber")
	}
	select {
	case <-ch:
	default:
		t.Fatal("subscriber did not receive initial event")
	}
}
