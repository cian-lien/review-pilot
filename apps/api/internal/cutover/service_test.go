//go:build integration

package cutover_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/cutover"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/testsupport"
	"strings"
	"testing"
	"time"
)

func status(err error) int {
	var value *fault.Error
	if errors.As(err, &value) {
		return value.Status
	}
	return 0
}
func TestCutover(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store := testsupport.NewStore(t)
	if err := persistence.ApplyMigrations(ctx, store, os.Getenv("MIGRATION_DIRECTORY")); err != nil {
		t.Fatal(err)
	}
	db := store.DB()
	for _, sql := range []string{"INSERT INTO installations(id) VALUES(101)", "INSERT INTO repositories(installation_id,id,full_name,public,enabled) VALUES(101,202,'acme/web',true,true)"} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	seed := func(number int, status, jobStatus string) (persistence.Run, persistence.Job) {
		r := persistence.Run{ID: jobs.UUID(), InstallationID: 101, RepositoryID: 202, PrNumber: number, BaseSHA: strings.Repeat("a", 40), HeadSHA: strings.Repeat("b", 40), PipelineVersion: "foundation-v1", Status: status}
		snap := contracts.RunSnapshot{ModelConfigVersion: "none", RepositoryConfigVersion: "1", MemorySetVersion: "empty", TrustedRulesSha: ""}
		snap.Identity.Tenant = contracts.TenantRef{InstallationId: 101, RepositoryId: 202}
		snap.Identity.PrNumber = number
		snap.Identity.BaseSha = r.BaseSHA
		snap.Identity.HeadSha = r.HeadSHA
		snap.Identity.PipelineVersion = r.PipelineVersion
		r.Snapshot, _ = json.Marshal(snap)
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
		j := persistence.Job{ID: jobs.UUID(), RunID: r.ID, Kind: "review", Status: jobStatus, MaxAttempts: 5, AvailableAt: time.Now()}
		if err := db.Create(&j).Error; err != nil {
			t.Fatal(err)
		}
		return r, j
	}
	pending, job := seed(7, "Queued", "queued")
	completed, finished := seed(8, "Completed", "completed")
	calls := map[string]int{}
	unknown := false
	completeRemote := false
	service := cutover.New(db, nil)
	service.Inspect = func(_ context.Context, r persistence.Run) (*github.Check, error) {
		calls[r.ID]++
		if unknown {
			return nil, errors.New("REMOTE_UNCERTAIN")
		}
		if completeRemote && r.ID == pending.ID {
			c := github.Check{ID: 99, Name: github.ConnectionCheckName, ExternalID: "reviewpilot:" + r.ID, HeadSHA: r.HeadSHA, Status: "completed"}
			value := "neutral"
			c.Conclusion = &value
			c.App.ID = 1
			return &c, nil
		}
		return nil, nil
	}
	if _, err := service.Switch(ctx, 0, "go"); status(err) != 409 {
		t.Fatal("unconfirmed consumer stop must reject", err)
	}
	service.ConsumersStopped = true
	token := jobs.UUID()
	if err := db.Exec("UPDATE jobs SET status='running',lease_token=?,lease_expires_at=clock_timestamp()+interval '90 seconds' WHERE id=?", token, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	_, err := service.Switch(ctx, 0, "go")
	activeRejected := status(err) == 409
	if !activeRejected {
		t.Fatal("active legacy lease must block", err)
	}
	if err = db.Exec("UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	report, err := service.Switch(ctx, 0, "go")
	if err != nil || report.Mode != "go" || report.Epoch != 1 {
		t.Fatal("go cutover", report, err)
	}
	var task string
	if err = db.Raw("SELECT task_id FROM outbox_events WHERE task_id=?", job.ID).Scan(&task).Error; err != nil || task != job.ID {
		t.Fatal("pending task identity changed", err)
	}
	var count int64
	db.Raw("SELECT count(*) FROM outbox_events WHERE task_id=?", finished.ID).Scan(&count)
	if count != 0 || calls[completed.ID] != 0 {
		t.Fatal("completed run re-notified or reconciled")
	}
	_, err = service.Switch(ctx, 0, "legacy")
	staleEpoch := status(err)
	if staleEpoch != 409 {
		t.Fatal("stale epoch accepted")
	}
	if err = db.Exec("UPDATE jobs SET status='result_ready' WHERE id=?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("UPDATE review_runs SET status='Failed',check_id=NULL WHERE id=?", pending.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("INSERT INTO publication_requests(run_id,state) VALUES(?,'failed')", pending.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("INSERT INTO job_results(task_id,result_id,result_hash,input_hash,payload) VALUES(?,?,?,?,'{}'::jsonb)", job.ID, job.ID+":connection", strings.Repeat("c", 64), strings.Repeat("d", 64)).Error; err != nil {
		t.Fatal(err)
	}
	unknown = true
	before := calls[pending.ID]
	report, err = service.Preflight(ctx)
	if err != nil || report.UnreconciledCheckCount != 1 || report.CanSwitch || calls[pending.ID] <= before {
		t.Fatal("NULL check_id/failed publication bypassed reconciliation", report, err)
	}
	_, err = service.Switch(ctx, 1, "legacy")
	unreconciledBlocks := status(err) == 409
	if !unreconciledBlocks {
		t.Fatal("unknown remote write must block")
	}
	unknown = false
	completeRemote = true
	report, err = service.Switch(ctx, 1, "legacy")
	if err != nil || report.Mode != "legacy" || report.Epoch != 2 {
		t.Fatal("rollback", report, err)
	}
	db.First(&job, "id=?", job.ID)
	db.First(&pending, "id=?", pending.ID)
	if job.Status != "completed" || pending.Status != "Completed" || pending.CheckID == nil || *pending.CheckID != 99 {
		t.Fatal("remote facts not preserved")
	}
	db.Raw("SELECT count(*) FROM job_results WHERE task_id=?", job.ID).Scan(&count)
	if count != 1 {
		t.Fatal("rollback discarded accepted result")
	}
	// The runtime fences Go; the CLI separately requires actual process-stop confirmation.
	envelope := contracts.TaskEnvelope{SchemaVersion: 1, MessageId: jobs.UUID(), TaskId: job.ID, RunId: pending.ID, Tenant: contracts.TenantRef{InstallationId: 101, RepositoryId: 202}, HeadSha: pending.HeadSHA, Kind: contracts.TaskEnvelopeKindConnectionCheck}
	_, err = jobs.New(db).Claim(ctx, envelope, "cutover-fixture")
	if status(err) != 503 {
		t.Fatal("Go worker still enabled after rollback", err)
	}
	result := map[string]any{"switchWithActiveLeaseRejected": activeRejected, "pendingTaskIdPreserved": task == job.ID, "completedRenotified": false, "unreconciledPublishBlocksSwitch": unreconciledBlocks, "staleEpochStatus": staleEpoch, "rollbackLeavesTwoConsumers": false}
	raw, _ := json.Marshal(result)
	if err = os.WriteFile(os.Getenv("SCENARIO_RESULT_PATH"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
