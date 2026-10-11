package jobs

import (
	"encoding/json"
	"gorm.io/gorm"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/observability"
	"reviewpilot.local/api/internal/persistence"
	"time"
)

func ScheduleReview(tx *gorm.DB, j persistence.Job, r persistence.Run, at time.Time) error {
	ctx, span := observability.FromContext(tx.Statement.Context).Start(tx.Statement.Context, "task.created", observability.Fields{RunID: r.ID, TaskID: j.ID})
	defer span.End()
	generation := j.NotificationGeneration + 1
	envelope := contracts.ReviewTaskEnvelope{SchemaVersion: 2, MessageId: UUID(), TaskId: j.ID, RunId: r.ID, Tenant: contracts.TenantRef{InstallationId: int(r.InstallationID), RepositoryId: int(r.RepositoryID)}, HeadSha: r.HeadSHA, Kind: "ai-review", NotificationGeneration: generation}
	if parent := observability.Traceparent(ctx); parent != "" {
		envelope.Traceparent = &parent
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if err = tx.Exec("UPDATE jobs SET notification_generation=? WHERE id=?", generation, j.ID).Error; err != nil {
		return err
	}
	return tx.Exec("INSERT INTO outbox_events(id,task_id,notification_generation,kind,payload,available_at) VALUES(?,?,?,'ai-review',?::jsonb,?)", envelope.MessageId, j.ID, generation, string(body), at).Error
}
