package p2p

import (
	"context"
	"sync"

	"committee-mvp/internal/wire"
)

// StaticNetwork is an in-memory static network abstraction for MVP bootstrap.
type StaticNetwork struct {
	nodeID string
	peers  map[string]string

	mu   sync.RWMutex
	inCh chan wire.Envelope
}

func NewStaticNetwork(nodeID string, peers []string) *StaticNetwork {
	peerMap := make(map[string]string, len(peers))
	for _, p := range peers {
		peerMap[p] = p
	}
	return &StaticNetwork{
		nodeID: nodeID,
		peers:  peerMap,
		inCh:   make(chan wire.Envelope, 128),
	}
}

func (n *StaticNetwork) Start(context.Context) error {
	return nil
}

func (n *StaticNetwork) Stop(context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	select {
	case <-n.inCh:
	default:
	}
	close(n.inCh)
	return nil
}

func (n *StaticNetwork) Publish(msg wire.Envelope) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	select {
	case n.inCh <- msg:
	default:
	}
}

func (n *StaticNetwork) Subscribe() <-chan wire.Envelope {
	return n.inCh
}
