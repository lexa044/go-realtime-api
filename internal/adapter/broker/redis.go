package broker

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"

	"github.com/lexa044/realtime-api/internal/domain"
)

// Publisher implements usecase.EventPublisher on top of Redis Pub/Sub.
type Publisher struct {
	rdb *redis.Client
}

func NewPublisher(rdb *redis.Client) *Publisher {
	return &Publisher{rdb: rdb}
}

func (p *Publisher) Publish(ctx context.Context, channel string, event domain.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.rdb.Publish(ctx, channel, data).Err()
}

// Broadcaster is anything that can fan a raw message out to local websocket
// clients — implemented by ws.Hub. Declared here (consumer side) rather
// than in ws, so this package doesn't import ws at all.
type Broadcaster interface {
	Broadcast(message []byte)
}

// Subscriber listens on one or more Redis channels and forwards every
// message verbatim to the local Hub. Every API instance runs its own
// Subscriber against the same Redis channel(s): publishing once therefore
// fans out to every instance — and every connected client cluster-wide —
// with zero direct coupling between instances.
type Subscriber struct {
	rdb         *redis.Client
	broadcaster Broadcaster
	channels    []string
}

func NewSubscriber(rdb *redis.Client, broadcaster Broadcaster, channels ...string) *Subscriber {
	return &Subscriber{rdb: rdb, broadcaster: broadcaster, channels: channels}
}

// Run blocks; call it in its own goroutine. go-redis reconnects internally
// on transient errors. It returns cleanly when ctx is cancelled.
func (s *Subscriber) Run(ctx context.Context) error {
	pubsub := s.rdb.Subscribe(ctx, s.channels...)
	defer pubsub.Close()

	if _, err := pubsub.Receive(ctx); err != nil {
		return err
	}

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			s.broadcaster.Broadcast([]byte(msg.Payload))
		}
	}
}
