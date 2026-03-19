package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"committee-mvp/internal/committee"
	"committee-mvp/internal/config"
)

func main() {
	configPath := flag.String("config", "configs/devnet.json", "path to node config file")
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
	<-ctx.Done()
	if err := svc.Stop(context.Background()); err != nil {
		log.Printf("service stop failed: %v", err)
	}
}
