package pubsub

import "context"

type Event struct {
	ID          string
	Type        string
	WorkspaceID string
	ChannelID   string
	UserID      string
	Payload     []byte
}

type Handler func(ctx context.Context, event Event) error

type Subscription interface {
	ID() string
	Topic() string
}

type PubSub interface {
	Publish(ctx context.Context, topic string, event Event) error
	Subscribe(ctx context.Context, topic string, handler Handler) (Subscription, error)
	Unsubscribe(ctx context.Context, subscriptionID string) error
}
