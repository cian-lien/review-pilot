package testsupport

import (
	"context"
	"gorm.io/gorm"
	"net/url"
	"os"
	"reviewpilot.local/api/internal/persistence"
	"testing"
	"time"
)

func NewDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/reviewpilot_migration_test" {
		t.Fatal("Dedicated TEST_DATABASE_URL from migration harness required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := persistence.Open(ctx, dsn)
	if err != nil {
		t.Fatal("Dedicated database unavailable")
	}
	t.Cleanup(func() { _ = store.Close() })
	return store.DB()
}
