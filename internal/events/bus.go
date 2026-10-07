package events

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Event struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Timestamp   time.Time `json:"timestamp"`
	PrincipalID string    `json:"principalId,omitempty"`
	ClientID    string    `json:"clientId,omitempty"`
	ItemID      string    `json:"itemId,omitempty"`
	SessionID   string    `json:"sessionId,omitempty"`
	PositionMS  int64     `json:"positionMs,omitempty"`
}

type Bus struct {
	mu       sync.RWMutex
	subs     map[uint64]chan Event
	next     atomic.Uint64
	eventSeq atomic.Uint64
	dropped  atomic.Uint64
}

func NewBus() *Bus { return &Bus{subs: map[uint64]chan Event{}} }

func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	id := b.next.Add(1)
	ch := make(chan Event, buffer)
	b.mu.Lock()
	b.subs[id] = ch
	b.mu.Unlock()
	var once sync.Once
	cancel := func() { once.Do(func() { b.mu.Lock(); delete(b.subs, id); close(ch); b.mu.Unlock() }) }
	return ch, cancel
}

func (b *Bus) Publish(e Event) {
	if e.ID == "" {
		e.ID = fmt.Sprintf("evt-%d", b.eventSeq.Add(1))
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
			b.dropped.Add(1)
		}
	}
}

func (b *Bus) Dropped() uint64 { return b.dropped.Load() }
