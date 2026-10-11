package jobs

import (
	"encoding/json"
	"gorm.io/gorm"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/persistence"
)

// LockReview preserves the authority lock order used by S1.
func LockReview(tx *gorm.DB, taskID string) (persistence.Job, persistence.Run, bool, error) {
	return locked(tx, taskID)
}

func ReviewSnapshot(r persistence.Run) (contracts.ReviewSnapshot, error) {
	var s contracts.ReviewSnapshot
	if contracts.DecodeReview("ReviewSnapshot", r.Snapshot, &s, 65536) != nil {
		return s, conflict("SNAPSHOT_INVALID")
	}
	i := s.Identity
	if i.Tenant.InstallationId != int(r.InstallationID) || i.Tenant.RepositoryId != int(r.RepositoryID) || i.PrNumber != r.PrNumber ||
		i.BaseSha != r.BaseSHA || i.HeadSha != r.HeadSHA || string(i.PipelineVersion) != r.PipelineVersion || i.Generation != r.Generation || r.PipelineVersion != "review-v1" {
		return s, conflict("SNAPSHOT_INVALID")
	}
	return s, nil
}

func CheckReviewLease(tx *gorm.DB, l contracts.ReviewLease) (persistence.Run, error) {
	runtime, err := control(tx)
	if err != nil {
		return persistence.Run{}, err
	}
	j, r, authorized, err := locked(tx, l.TaskId)
	if err != nil {
		return r, err
	}
	now, err := dbNow(tx)
	if err != nil {
		return r, err
	}
	if !authorized || j.Kind != "review" || j.Status != "running" || j.RunID != l.RunId || j.LeaseToken == nil || *j.LeaseToken != l.LeaseToken ||
		j.LeaseExpiresAt == nil || !j.LeaseExpiresAt.After(now) || j.ExecutionEpoch != runtime.Epoch || l.ExecutionEpoch != runtime.Epoch ||
		r.NeedsReconciliation || r.Status == "Stale" || r.Status == "Cancelled" || r.Status == "Failed" || l.InputHash != Hash(r.Snapshot) ||
		int64(l.Tenant.InstallationId) != r.InstallationID || int64(l.Tenant.RepositoryId) != r.RepositoryID || l.HeadSha != r.HeadSHA {
		return r, conflict("LEASE_LOST")
	}
	snapshot, err := ReviewSnapshot(r)
	if err != nil {
		return r, err
	}
	a, _ := json.Marshal(snapshot)
	b, _ := json.Marshal(l.Snapshot)
	if string(a) != string(b) {
		return r, conflict("SNAPSHOT_INVALID")
	}
	return r, nil
}
