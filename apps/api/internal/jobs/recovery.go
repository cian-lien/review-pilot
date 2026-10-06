package jobs

import (
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/observability"
	"reviewpilot.local/api/internal/persistence"
	"time"
)

// Schedule is transaction-only: the generation and notification become visible together.
func Schedule(tx *gorm.DB, j persistence.Job, r persistence.Run, available time.Time) error {
	ctx, span := observability.FromContext(tx.Statement.Context).Start(tx.Statement.Context, "task.created", observability.Fields{RunID: r.ID, TaskID: j.ID})
	defer span.End()
	generation := j.NotificationGeneration + 1
	messageID := UUID()
	envelope := contracts.TaskEnvelope{SchemaVersion: 1, MessageId: messageID, TaskId: j.ID, RunId: r.ID, Tenant: contracts.TenantRef{InstallationId: int(r.InstallationID), RepositoryId: int(r.RepositoryID)}, HeadSha: r.HeadSHA, Kind: contracts.TaskEnvelopeKindConnectionCheck, NotificationGeneration: generation}
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
	return tx.Exec("INSERT INTO outbox_events(id,task_id,notification_generation,kind,payload,available_at) VALUES(?,?,?,'connection-check',?::jsonb,?)", messageID, j.ID, generation, string(body), available).Error
}
func (s *Service) Recover(ctx context.Context) error {
	return s.transaction(ctx, func(tx *gorm.DB) error {
		if _, err := control(tx); err != nil {
			return err
		}
		var rows []persistence.Job
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("kind='review' AND ((status='running' AND lease_expires_at<=clock_timestamp()) OR (status='queued' AND available_at<=clock_timestamp() AND NOT EXISTS(SELECT 1 FROM outbox_events o WHERE o.task_id=jobs.id AND (o.sent_at IS NULL OR o.sent_at>clock_timestamp()-interval '5 seconds'))))").Order("available_at,id").Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		for _, j := range rows {
			var r persistence.Run
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&r, "id=?", j.RunID).Error; err != nil {
				return err
			}
			if j.Attempts >= j.MaxAttempts {
				if err := tx.Exec("UPDATE jobs SET status='failed',lease_token=NULL,lease_expires_at=NULL WHERE id=?", j.ID).Error; err != nil {
					return err
				}
				if err := tx.Exec("UPDATE review_runs SET status='Failed',updated_at=clock_timestamp() WHERE id=?", r.ID).Error; err != nil {
					return err
				}
				continue
			}
			if err := tx.Exec("UPDATE jobs SET status='queued',lease_token=NULL,lease_expires_at=NULL WHERE id=?", j.ID).Error; err != nil {
				return err
			}
			if err := tx.Exec("UPDATE review_runs SET status='Queued',updated_at=clock_timestamp() WHERE id=?", r.ID).Error; err != nil {
				return err
			}
			now, err := dbNow(tx)
			if err != nil {
				return err
			}
			if err = Schedule(tx, j, r, now); err != nil {
				return err
			}
		}
		return nil
	})
}
