package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"time"
	"syscall"

	"committee-mvp/internal/committee"
	"committee-mvp/internal/config"
)

func main() {
	configPath := flag.String("config", "configs/devnet.json", "path to node config file")
	autoSession := flag.String("session", "", "optional session id for coordinator to trigger signing")
	autoMessage := flag.String("message", "", "optional message for coordinator to trigger signing")
	autoDelay := flag.Duration("auto-delay", 2*time.Second, "delay before coordinator sends sign request")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	svc := committee.NewService(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := svc.Start(ctx); err != nil {
		log.Fatalf("service start failed: %v", err)
	}
	if cfg.NodeID == cfg.CoordinatorID && *autoSession != "" && *autoMessage != "" {
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(*autoDelay):
			}
			if err := svc.SubmitSignRequest(*autoSession, []byte(*autoMessage)); err != nil {
				log.Printf("auto submit sign request failed: %v", err)
				return
			}
			log.Printf("auto submit sign request sent session_id=%s", *autoSession)
		}()
	}
	<-ctx.Done()
	if err := svc.Stop(context.Background()); err != nil {
		log.Printf("service stop failed: %v", err)
	}
}
