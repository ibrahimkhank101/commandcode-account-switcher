package domain

import (
	"context"
	"sync"
	"time"
)

type EventType string

const (
	EventAccountSwitched EventType = "account.switched"
	EventQuotaExhausted  EventType = "quota.exhausted"
	EventFailoverSuccess EventType = "failover.success"
	EventQuotaRefreshed  EventType = "quota.refreshed"
	EventAccountAdded    EventType = "account.added"
	EventAccountRemoved  EventType = "account.removed"
	EventRequestProxied  EventType = "request.proxied"
)

type Event struct {
	ID           string                 `json:"id"`
	Type         EventType              `json:"type"`
	AccountID    string                 `json:"accountId,omitempty"`
	AccountAlias string                 `json:"accountAlias,omitempty"`
	Message      string                 `json:"message"`
	Payload      map[string]interface{} `json:"payload,omitempty"`
	Timestamp    time.Time              `json:"timestamp"`
}

type EventRepository interface {
	Save(ctx context.Context, event Event) error
	ListRecent(ctx context.Context, limit int) ([]Event, error)
}

type EventBroadcaster interface {
	Publish(event Event)
	Subscribe() (<-chan Event, func())
}

type InMemoryBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

func NewInMemoryBroadcaster() *InMemoryBroadcaster {
	return &InMemoryBroadcaster{
		subscribers: make(map[chan Event]struct{}),
	}
}

func (b *InMemoryBroadcaster) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (b *InMemoryBroadcaster) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subscribers[ch]; ok {
			delete(b.subscribers, ch)
			close(ch)
		}
	}
	return ch, unsubscribe
}
