package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"reviewpilot.local/api/internal/persistence"
	"time"
)

func main() {
	directory := flag.String("directory", "../../packages/persistence/migrations", "SQL directory")
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
	if err := persistence.ApplyMigrations(ctx, store, *directory); err != nil {
		slog.Error("MIGRATION_REJECTED")
		os.Exit(1)
	}
	slog.Info("MIGRATIONS_APPLIED")
}
