package stream

import (
	"context"
)

// Message encapsulates a streamed event payload.
type Message struct {
	ID        string
	StreamKey string
	Payload   []byte
}

// Publisher defines an interface for publishing events to a stream.
type Publisher interface {
	Publish(ctx context.Context, streamKey string, payload []byte) (string, error)
	Close() error
}

// Consumer defines an interface for consuming events from a stream.
type Consumer interface {
	Subscribe(ctx context.Context, streamKey string, group string, consumerName string) (<-chan Message, error)
	Ack(ctx context.Context, streamKey string, group string, messageID string) error
	Close() error
}

// Stream names matching system architecture
const (
	StreamRawPaymentEvents = "raw-payment-events"
	StreamNormalizedEvents = "normalized-events"
	StreamClassifications  = "classifications"
	StreamDecisions        = "decisions"
)
