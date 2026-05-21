package pubsub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

type MemoryPubSub struct {
	mu            sync.RWMutex
	subscriptions map[string]memorySubscription
}

type memorySubscription struct {
	id      string
	topic   string
	handler Handler
}

func NewMemoryPubSub() *MemoryPubSub {
	return &MemoryPubSub{subscriptions: make(map[string]memorySubscription)}
}

func (p *MemoryPubSub) Publish(ctx context.Context, topic string, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	p.mu.RLock()
	handlers := make([]Handler, 0)
	for _, sub := range p.subscriptions {
		if sub.topic == topic {
			handlers = append(handlers, sub.handler)
		}
	}
	p.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (p *MemoryPubSub) Subscribe(ctx context.Context, topic string, handler Handler) (Subscription, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	id := randomID()
	sub := memorySubscription{id: id, topic: topic, handler: handler}
	p.mu.Lock()
	p.subscriptions[id] = sub
	p.mu.Unlock()
	return sub, nil
}

func (p *MemoryPubSub) Unsubscribe(ctx context.Context, subscriptionID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	p.mu.Lock()
	delete(p.subscriptions, subscriptionID)
	p.mu.Unlock()
	return nil
}

func (s memorySubscription) ID() string    { return s.id }
func (s memorySubscription) Topic() string { return s.topic }

func randomID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "subscription"
	}
	return hex.EncodeToString(buf)
}
