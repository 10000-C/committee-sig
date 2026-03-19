package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"committee-mvp/internal/wire"
)

type peerWriter struct {
	conn net.Conn
	enc  *json.Encoder
	mu   sync.Mutex
}

func (w *peerWriter) write(msg wire.Envelope) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enc.Encode(msg); err != nil {
		return err
	}
	return nil
}

// StaticNetwork is a TCP static network abstraction for MVP bootstrap.
type StaticNetwork struct {
	nodeID     string
	listenAddr string
	peers      map[string]string

	listener net.Listener

	mu      sync.RWMutex
	writers map[string]*peerWriter
	inCh    chan wire.Envelope
	closed  chan struct{}
	stopWg  sync.WaitGroup
	once    sync.Once
}

func NewStaticNetwork(nodeID, listenAddr string, peers map[string]string) *StaticNetwork {
	peerMap := make(map[string]string, len(peers))
	for id, addr := range peers {
		peerMap[id] = addr
	}
	return &StaticNetwork{
		nodeID:     nodeID,
		listenAddr: listenAddr,
		peers:      peerMap,
		writers:    make(map[string]*peerWriter, len(peerMap)),
		inCh:       make(chan wire.Envelope, 512),
		closed:     make(chan struct{}),
	}
}


func (n *StaticNetwork) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", n.listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", n.listenAddr, err)
	}
	n.listener = ln

	n.stopWg.Add(1)
	go func() {
		defer n.stopWg.Done()
		n.acceptLoop()
	}()

	for peerID, addr := range n.peers {
		if peerID == n.nodeID {
			continue
		}
		n.stopWg.Add(1)
		go func(pid, paddr string) {
			defer n.stopWg.Done()
			n.dialLoop(ctx, pid, paddr)
		}(peerID, addr)
	}
	return nil
}

func (n *StaticNetwork) Stop(context.Context) error {
	n.once.Do(func() {
		close(n.closed)
		if n.listener != nil {
			_ = n.listener.Close()
		}
		n.mu.Lock()
		for id, w := range n.writers {
			_ = w.conn.Close()
			delete(n.writers, id)
		}
		n.mu.Unlock()
		n.stopWg.Wait()
		close(n.inCh)
	})
	return nil
}

func (n *StaticNetwork) Publish(msg wire.Envelope) {
	select {
	case <-n.closed:
		return
	default:
	}
	if msg.To == "" || msg.To == n.nodeID {
		n.pushInbound(msg)
	}

	n.mu.RLock()
	defer n.mu.RUnlock()
	if msg.To != "" {
		if msg.To == n.nodeID {
			return
		}
		if w, ok := n.writers[msg.To]; ok {
			if err := w.write(msg); err != nil {
				log.Printf("p2p send failed to=%s err=%v", msg.To, err)
			}
		}
		return
	}
	for peerID, w := range n.writers {
		if peerID == n.nodeID {
			continue
		}
		if err := w.write(msg); err != nil {
			log.Printf("p2p broadcast failed to=%s err=%v", peerID, err)
		}
	}

}

func (n *StaticNetwork) pushInbound(msg wire.Envelope) {
	select {
	case <-n.closed:
		return
	case n.inCh <- msg:
	default:
	}
}

func (n *StaticNetwork) Subscribe() <-chan wire.Envelope {
	return n.inCh
}

func (n *StaticNetwork) acceptLoop() {
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			select {
			case <-n.closed:
				return
			default:
			}
			log.Printf("p2p accept failed err=%v", err)
			continue
		}
		n.stopWg.Add(1)
		go func(c net.Conn) {
			defer n.stopWg.Done()
			n.readLoop(c)
		}(conn)
	}
}

func (n *StaticNetwork) dialLoop(ctx context.Context, peerID, peerAddr string) {
	backoff := 250 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.closed:
			return
		default:
		}

		conn, err := net.DialTimeout("tcp", peerAddr, 2*time.Second)
		if err != nil {
			time.Sleep(backoff)
			if backoff < 3*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 250 * time.Millisecond

		writer := &peerWriter{conn: conn, enc: json.NewEncoder(conn)}
		n.setWriter(peerID, writer)
		n.stopWg.Add(1)
		go func(c net.Conn) {
			defer n.stopWg.Done()
			n.readLoop(c)
		}(conn)

		for {
			select {
			case <-ctx.Done():
				n.clearWriter(peerID, conn)
				_ = conn.Close()
				return
			case <-n.closed:
				n.clearWriter(peerID, conn)
				_ = conn.Close()
				return
			default:
				if !n.isConnAlive(conn) {
					n.clearWriter(peerID, conn)
					_ = conn.Close()
					goto REDIAL
				}
				time.Sleep(500 * time.Millisecond)
			}
		}
	REDIAL:
	}
}

func (n *StaticNetwork) readLoop(conn net.Conn) {
	defer func() {
		_ = conn.Close()
	}()
	dec := json.NewDecoder(conn)
	for {
		var env wire.Envelope
		if err := dec.Decode(&env); err != nil {
			return
		}
		n.pushInbound(env)
	}
}

func (n *StaticNetwork) setWriter(peerID string, w *peerWriter) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if old, ok := n.writers[peerID]; ok {
		_ = old.conn.Close()
	}
	n.writers[peerID] = w
}

func (n *StaticNetwork) clearWriter(peerID string, conn net.Conn) {
	n.mu.Lock()
	defer n.mu.Unlock()
	w, ok := n.writers[peerID]
	if !ok {
		return
	}
	if w.conn == conn {
		delete(n.writers, peerID)
	}
}

func (n *StaticNetwork) isConnAlive(conn net.Conn) bool {
	if conn == nil {
		return false
	}
	if err := conn.SetReadDeadline(time.Now()); err != nil {
		return false
	}
	_ = conn.SetReadDeadline(time.Time{})
	return true
}
