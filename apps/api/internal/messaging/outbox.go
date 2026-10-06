package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
)

type Publisher interface {
	Publish(context.Context, string, string, []byte) error
}
type Relay struct {
	DB        *gorm.DB
	Publisher Publisher
}
type event struct {
	ID         string
	Kind       string
	Payload    json.RawMessage
	RelayToken *string
}

func (e event) TableName() string { return "outbox_events" }
func (r *Relay) Flush(ctx context.Context, limit int) error {
	if r.DB == nil || r.Publisher == nil {
		return errors.New("OUTBOX_UNAVAILABLE")
	}
	if limit < 1 || limit > 100 {
		limit = 100
	}
	var events []event
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runtime persistence.RuntimeControl
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&runtime, 1).Error; err != nil {
			return err
		}
		if runtime.Mode != "go" {
			return nil
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("sent_at IS NULL AND available_at<=clock_timestamp() AND (relay_expires_at IS NULL OR relay_expires_at<=clock_timestamp())").Order("available_at,id").Limit(limit).Find(&events).Error; err != nil {
			return err
		}
		for i := range events {
			token := jobs.UUID()
			events[i].RelayToken = &token
			if err := tx.Exec("UPDATE outbox_events SET relay_token=?,relay_expires_at=clock_timestamp()+interval '30 seconds' WHERE id=?", token, events[i].ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, e := range events {
		if err = r.Publisher.Publish(ctx, e.Kind, e.ID, e.Payload); err != nil {
			_ = r.DB.WithContext(ctx).Exec("UPDATE outbox_events SET relay_token=NULL,relay_expires_at=NULL,available_at=clock_timestamp()+interval '5 seconds' WHERE id=? AND relay_token=?", e.ID, *e.RelayToken).Error
			return err
		}
		if err = r.DB.WithContext(ctx).Exec("UPDATE outbox_events SET sent_at=clock_timestamp(),relay_token=NULL,relay_expires_at=NULL WHERE id=? AND relay_token=?", e.ID, *e.RelayToken).Error; err != nil {
			return err
		}
	}
	return nil
}
