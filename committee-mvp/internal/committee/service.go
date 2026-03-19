package committee

import (
	"context"
	"log"
	"sync"

	"committee-mvp/internal/config"
	"committee-mvp/internal/p2p"
)

// Service orchestrates committee protocol flow for the MVP runtime.
type Service struct {
	cfg *config.NodeConfig
	net *p2p.StaticNetwork

	wg sync.WaitGroup
}

func NewService(cfg *config.NodeConfig) *Service {
	return &Service{
		cfg: cfg,
		net: p2p.NewStaticNetwork(cfg.NodeID, cfg.StaticNodes),
	}
}

func (s *Service) Start(ctx context.Context) error {
	if err := s.net.Start(ctx); err != nil {
		return err
	}
	log.Printf("committee node started node_id=%s threshold=%d static_peers=%d", s.cfg.NodeID, s.cfg.Threshold, len(s.cfg.StaticNodes))
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-s.net.Subscribe():
				if !ok {
					return
				}
				// Protocol handlers are introduced in the next implementation phase.
			}
		}
	}()
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	if err := s.net.Stop(ctx); err != nil {
		return err
	}
	s.wg.Wait()
	return nil
}
