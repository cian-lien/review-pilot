package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reviewpilot.local/api/internal/budgets"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/retention"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "REVIEW_ADMIN_FAILED")
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("operation required")
	}
	flags := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	callID := flags.String("call-id", "", "")
	revision := flags.Int("expected-revision", -1, "")
	outcome := flags.String("outcome", "", "")
	receipt := flags.String("receipt-ref", "", "")
	actual := flags.String("actual-cost-micros", "", "")
	currency := flags.String("currency", "", "")
	retry := flags.Bool("retry-stage", false, "")
	installation := flags.Int("delete-installation", 0, "")
	repository := flags.Int("delete-repository", 0, "")
	if err := flags.Parse(os.Args[2:]); err != nil || flags.NArg() != 0 {
		return fmt.Errorf("invalid operator arguments")
	}
	if os.Args[1] != "reconcile-model" && os.Args[1] != "cleanup" {
		return fmt.Errorf("unknown operation")
	}
	if os.Args[1] == "cleanup" && ((*installation == 0) != (*repository == 0)) {
		return fmt.Errorf("both delete identifiers required")
	}
	cfg, err := config.Load()
	if err != nil || cfg.DatabaseURL == "" {
		return fmt.Errorf("operator database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := persistence.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	if os.Args[1] == "reconcile-model" {
		return budgets.New(store.DB()).Reconcile(ctx, budgets.ReconcileRequest{CallID: *callID, ExpectedRevision: *revision, Outcome: *outcome, ReceiptRef: *receipt, ActualCostMicros: *actual, Currency: *currency, RetryStage: *retry})
	}
	service := retention.New(store.DB())
	if *installation != 0 || *repository != 0 {
		return service.DeleteRepository(ctx, contracts.TenantRef{InstallationId: *installation, RepositoryId: *repository})
	}
	counts, err := service.Cleanup(ctx, time.Now(), 100)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(counts)
}
