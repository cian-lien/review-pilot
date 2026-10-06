package persistence

import (
	"encoding/json"
	"time"
)

// Explicit table names preserve the S1 SQL schema; no AutoMigrate is used.
type Installation struct {
	ID        int64
	Active    bool
	UpdatedAt time.Time
}

func (Installation) TableName() string { return "installations" }

type Repository struct {
	InstallationID int64 `gorm:"primaryKey"`
	ID             int64 `gorm:"primaryKey"`
	FullName       string
	Public         bool
	Enabled        bool
	AuthRevision   int
	ConfigVersion  int
}

func (Repository) TableName() string { return "repositories" }

type PullRequest struct {
	InstallationID  int64  `gorm:"primaryKey"`
	RepositoryID    int64  `gorm:"primaryKey"`
	Number          int    `gorm:"primaryKey"`
	BaseSHA         string `gorm:"column:base_sha"`
	HeadSHA         string `gorm:"column:head_sha"`
	RemoteUpdatedAt time.Time
}

func (PullRequest) TableName() string { return "pull_requests" }

type Run struct {
	ID              string
	InstallationID  int64
	RepositoryID    int64
	PrNumber        int
	BaseSHA         string `gorm:"column:base_sha"`
	HeadSHA         string `gorm:"column:head_sha"`
	PipelineVersion string
	Generation      int
	Snapshot        json.RawMessage `gorm:"type:jsonb"`
	Status          string
	CheckID         *int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (Run) TableName() string { return "review_runs" }

type Job struct {
	ID                     string
	RunID                  string
	Kind                   string
	Status                 string
	Attempts               int
	MaxAttempts            int
	AvailableAt            time.Time
	LeaseToken             *string
	LeaseExpiresAt         *time.Time
	WorkerID               *string
	LastErrorCode          *string
	NotificationGeneration int
	ExecutionEpoch         int
}

func (Job) TableName() string { return "jobs" }

type SessionRecord struct {
	TokenHash          string `gorm:"primaryKey"`
	UserID             int64
	EncryptedUserToken string
	ExpiresAt          time.Time
	CreatedAt          time.Time
	CSRFToken          *string `gorm:"column:csrf_token"`
}

func (SessionRecord) TableName() string { return "sessions" }

type RuntimeControl struct {
	ID    int
	Mode  string
	Epoch int
}

func (RuntimeControl) TableName() string { return "runtime_control" }
