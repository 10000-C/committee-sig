package p2p

import (
	"context"

	"committee-mvp/internal/wire"
)

// Network defines the minimal transport API used by the committee service.
type Network interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Publish(msg wire.Envelope)
	Subscribe() <-chan wire.Envelope
}
