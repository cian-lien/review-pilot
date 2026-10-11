package retention

import (
	"context"
	"reviewpilot.local/api/internal/contracts"
	"testing"
)

func TestCleanupBatchRemainsBounded(t *testing.T) {
	for _, item := range []struct{ input, want int }{{0, 100}, {-1, 100}, {1, 1}, {100, 100}, {101, 100}} {
		if BatchLimit(item.input) != item.want {
			t.Fatal("cleanup batch exceeded its bound")
		}
	}
}
func TestDeleteRequiresBothTenantIdentifiers(t *testing.T) {
	for _, tenant := range []contracts.TenantRef{{}, {InstallationId: 1}, {RepositoryId: 1}, {InstallationId: -1, RepositoryId: 2}} {
		if err := New(nil).DeleteRepository(context.Background(), tenant); err == nil || err.Error() != "EXPLICIT_DELETE_SCOPE_REQUIRED" {
			t.Fatal("missing deletion scope was accepted")
		}
	}
}
