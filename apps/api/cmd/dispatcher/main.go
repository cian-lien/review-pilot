package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/messaging"
	"reviewpilot.local/api/internal/observability"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/publisher"
	"reviewpilot.local/api/internal/retention"
	"syscall"
	"time"
)

func main() {
	closeTelemetry, telemetryErr := observability.Setup(context.Background(), "dispatcher")
	if telemetryErr != nil {
		slog.Warn("TELEMETRY_UNAVAILABLE")
	}
	defer closeTelemetry(context.Background())
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
	publications := publisher.New(store.DB(), github.New(cfg), cfg)
	relay := &messaging.Relay{DB: store.DB(), Publisher: rabbit, Observer: observability.Current()}
	ticker := time.NewTicker(5 * time.Second)
	nextCleanup := time.Now()
	defer ticker.Stop()
	for {
		step, stop := context.WithTimeout(ctx, 20*time.Second)
		observability.Current().RecordState(step, store.DB())
		if time.Now().After(nextCleanup) {
			if _, err = retention.New(store.DB()).Cleanup(step, time.Now(), 100); err != nil {
				slog.Warn("REVIEW_RETENTION_UNAVAILABLE")
			}
			nextCleanup = time.Now().Add(time.Hour)
		}
		if err = service.Recover(step); err != nil {
			slog.Warn("RECOVERY_UNAVAILABLE")
		}
		if err = relay.Flush(step, 100); err != nil {
			slog.Warn("OUTBOX_DELIVERY_UNCONFIRMED")
		}
		for count := 0; count < 10; count++ {
			handled, err := publications.ProcessNext(step)
			if err != nil {
				slog.Warn("CONNECTION_PUBLICATION_UNAVAILABLE")
			}
			if !handled || step.Err() != nil {
				break
			}
		}
		stop()
		if path := os.Getenv("HEARTBEAT_FILE"); path != "" {
			_ = os.WriteFile(path, []byte("alive\n"), 0600)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
