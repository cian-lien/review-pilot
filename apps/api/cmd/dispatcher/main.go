package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/messaging"
	"reviewpilot.local/api/internal/persistence"
	"syscall"
	"time"
)

func main() {
	cfg, err := config.Load()
	if err != nil || cfg.DatabaseURL == "" || cfg.RabbitMQURL == "" {
		slog.Error("DISPATCHER_CONFIG_MISSING")
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	start, done := context.WithTimeout(ctx, 5*time.Second)
	store, err := persistence.Open(start, cfg.DatabaseURL)
	done()
	if err != nil {
		slog.Error("DATABASE_UNAVAILABLE")
		os.Exit(1)
	}
	defer store.Close()
	rabbit := messaging.NewRabbit(cfg.RabbitMQURL)
	defer rabbit.Close()
	service := jobs.New(store.DB())
	relay := &messaging.Relay{DB: store.DB(), Publisher: rabbit}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		step, stop := context.WithTimeout(ctx, 20*time.Second)
		if err = service.Recover(step); err != nil {
			slog.Warn("RECOVERY_UNAVAILABLE")
		}
		if err = relay.Flush(step, 100); err != nil {
			slog.Warn("OUTBOX_DELIVERY_UNCONFIRMED")
		}
		stop()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
