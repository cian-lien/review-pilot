package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/cutover"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/persistence"
	"time"
)

func main() {
	directory := flag.String("directory", "../../packages/persistence/migrations", "SQL directory")
	preflight := flag.Bool("preflight", false, "read-only runtime preflight (no migrations)")
	target := flag.String("target", "", "explicit go/legacy cutover; never default")
	epoch := flag.Int64("expected-epoch", -1, "required CAS epoch for explicit switch")
	stopped := flag.Bool("consumers-stopped", false, "operator confirms consumers AND dispatcher actually stopped")
	flag.Parse()
	if os.Getenv("DATABASE_URL") == "" {
		slog.Error("DATABASE_NOT_CONFIGURED")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := persistence.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("DATABASE_UNAVAILABLE")
		os.Exit(1)
	}
	defer store.Close()
	if *preflight || *target != "" {
		cfg, err := config.Load()
		if err != nil {
			slog.Error("CONFIG_INVALID")
			os.Exit(1)
		}
		service := cutover.New(store.DB(), github.New(cfg))
		service.ConsumersStopped = *stopped
		var report cutover.Report
		if *target != "" {
			if *epoch < 0 || !*stopped {
				slog.Error("CUTOVER_CONFIRMATION_REQUIRED")
				os.Exit(1)
			}
			report, err = service.Switch(ctx, *epoch, *target)
		} else {
			report, err = service.Preflight(ctx)
		}
		if err != nil {
			slog.Error("CUTOVER_REJECTED")
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(report)
		return
	}
	if err := persistence.ApplyMigrations(ctx, store, *directory); err != nil {
		slog.Error("MIGRATION_REJECTED")
		os.Exit(1)
	}
	slog.Info("MIGRATIONS_APPLIED")
}
