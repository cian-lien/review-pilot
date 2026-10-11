package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"regexp"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/repositories"
	"time"
)

type Service struct {
	DB              *gorm.DB
	PipelineVersion string
	Review          *config.ReviewConfig
}

func New(db *gorm.DB, pipeline string) *Service { return &Service{DB: db, PipelineVersion: pipeline} }

type identifier struct {
	ID int `json:"id"`
}
type repository struct {
	ID       int    `json:"id"`
	FullName string `json:"full_name"`
	Private  *bool  `json:"private"`
}
type pull struct {
	Title     string    `json:"title"`
	Number    int       `json:"number"`
	UpdatedAt time.Time `json:"updated_at"`
	Base      struct {
		SHA string `json:"sha"`
	} `json:"base"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
}
type delivery struct {
	Action       string       `json:"action"`
	Installation *identifier  `json:"installation"`
	Repository   *repository  `json:"repository"`
	PullRequest  *pull        `json:"pull_request"`
	Repositories []repository `json:"repositories"`
	Added        []repository `json:"repositories_added"`
	Removed      []identifier `json:"repositories_removed"`
}

var sha = regexp.MustCompile(`^[a-f0-9]{40,64}$`)

func idValid(id int) bool { return id > 0 && id <= 9007199254740991 }
func repoValid(repo repository) bool {
	_, err := github.RepositoryPath(repo.FullName)
	return idValid(repo.ID) && repo.Private != nil && err == nil
}
func invalid() error { return fault.New(400, "INVALID_WEBHOOK") }
func (s *Service) Accept(ctx context.Context, deliveryID, event string, raw []byte) (bool, error) {
	if s == nil || s.DB == nil {
		return false, fault.New(503, "DATABASE_UNAVAILABLE")
	}
	if len(raw) > MaxBody {
		return false, fault.New(413, "BODY_TOO_LARGE")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9-]{1,100}$`).MatchString(deliveryID) || !regexp.MustCompile(`^[a-z_]{1,64}$`).MatchString(event) {
		return false, invalid()
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return false, invalid()
	}
	var input delivery
	if err := json.Unmarshal(raw, &input); err != nil {
		return false, invalid()
	}
	if event == "installation" || event == "installation_repositories" || event == "repository" || event == "pull_request" {
		if input.Installation == nil || !idValid(input.Installation.ID) {
			return false, invalid()
		}
	}
	hash := jobs.Hash(raw)
	accepted := false
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		inserted := tx.Exec("INSERT INTO webhook_deliveries(delivery_id,event,payload_hash) VALUES(?,?,?) ON CONFLICT DO NOTHING", deliveryID, event, hash)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected == 0 {
			var existing struct {
				PayloadHash string
				Event       string
			}
			if err := tx.Raw("SELECT event,payload_hash FROM webhook_deliveries WHERE delivery_id=?", deliveryID).Scan(&existing).Error; err != nil {
				return err
			}
			if existing.PayloadHash != hash || existing.Event != event {
				return fault.New(409, "DELIVERY_CONFLICT")
			}
			return nil
		}
		accepted = true
		switch event {
		case "installation":
			for _, repo := range input.Repositories {
				if !repoValid(repo) {
					return invalid()
				}
			}
			if err := tx.Exec("INSERT INTO installations(id) VALUES(?) ON CONFLICT DO NOTHING", input.Installation.ID).Error; err != nil {
				return err
			}
			var installation persistence.Installation
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&installation, "id=?", input.Installation.ID).Error; err != nil {
				return err
			}
			switch input.Action {
			case "suspend", "deleted":
				if err := tx.Exec("UPDATE installations SET active=false,updated_at=clock_timestamp() WHERE id=?", installation.ID).Error; err != nil {
					return err
				}
				var repos []persistence.Repository
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("installation_id=?", installation.ID).Order("id").Find(&repos).Error; err != nil {
					return err
				}
				for _, repo := range repos {
					if err := disable(tx, int(installation.ID), int(repo.ID)); err != nil {
						return err
					}
				}
			case "unsuspend":
				return tx.Exec("UPDATE installations SET active=true,updated_at=clock_timestamp() WHERE id=?", installation.ID).Error
			case "created":
				for _, repo := range input.Repositories {
					if err := syncRepository(tx, input.Installation.ID, repo); err != nil {
						return err
					}
				}
			}
			return nil
		case "installation_repositories":
			for _, repo := range input.Added {
				if !repoValid(repo) {
					return invalid()
				}
				if err := syncRepository(tx, input.Installation.ID, repo); err != nil {
					return err
				}
			}
			if err := lockInstallation(tx, input.Installation.ID); err != nil {
				return err
			}
			for _, repo := range input.Removed {
				if !idValid(repo.ID) {
					return invalid()
				}
				if err := disable(tx, input.Installation.ID, repo.ID); err != nil {
					return err
				}
			}
			return nil
		case "repository":
			if input.Repository == nil || !repoValid(*input.Repository) {
				return invalid()
			}
			if input.Action == "privatized" || input.Action == "publicized" || input.Action == "renamed" {
				return syncRepository(tx, input.Installation.ID, *input.Repository)
			}
			if input.Action == "deleted" {
				if err := lockInstallation(tx, input.Installation.ID); err != nil {
					return err
				}
				return disable(tx, input.Installation.ID, input.Repository.ID)
			}
			return nil
		case "pull_request":
			if input.Repository == nil || !repoValid(*input.Repository) || input.PullRequest == nil || !idValid(input.PullRequest.Number) || input.PullRequest.UpdatedAt.IsZero() || !sha.MatchString(input.PullRequest.Base.SHA) || !sha.MatchString(input.PullRequest.Head.SHA) {
				return invalid()
			}
			if *input.Repository.Private {
				return syncRepository(tx, input.Installation.ID, *input.Repository)
			}
			if input.Action != "opened" && input.Action != "synchronize" && input.Action != "reopened" && input.Action != "ready_for_review" {
				return nil
			}
			return s.enqueue(tx, input)
		}
		return nil
	})
	return accepted, err
}
func lockInstallation(tx *gorm.DB, id int) error {
	if err := tx.Exec("INSERT INTO installations(id) VALUES(?) ON CONFLICT DO NOTHING", id).Error; err != nil {
		return err
	}
	var installation persistence.Installation
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&installation, "id=?", id).Error
}
func disable(tx *gorm.DB, installationID, repositoryID int) error {
	if err := tx.Exec("UPDATE repositories SET enabled=false,config_version=config_version+1,auth_revision=auth_revision+1 WHERE installation_id=? AND id=?", installationID, repositoryID).Error; err != nil {
		return err
	}
	return repositories.CancelWork(tx, contracts.TenantRef{InstallationId: installationID, RepositoryId: repositoryID})
}
func syncRepository(tx *gorm.DB, installationID int, repo repository) error {
	if err := lockInstallation(tx, installationID); err != nil {
		return err
	}
	if err := tx.Exec("INSERT INTO repositories(installation_id,id,full_name,public) VALUES(?,?,?,?) ON CONFLICT(installation_id,id) DO UPDATE SET full_name=EXCLUDED.full_name,public=EXCLUDED.public,enabled=CASE WHEN EXCLUDED.public THEN repositories.enabled ELSE false END,auth_revision=repositories.auth_revision+1,config_version=CASE WHEN repositories.public=EXCLUDED.public THEN repositories.config_version ELSE repositories.config_version+1 END", installationID, repo.ID, repo.FullName, !*repo.Private).Error; err != nil {
		return err
	}
	if *repo.Private {
		return repositories.CancelWork(tx, contracts.TenantRef{InstallationId: installationID, RepositoryId: repo.ID})
	}
	return nil
}
func (s *Service) enqueue(tx *gorm.DB, input delivery) error {
	tenant := contracts.TenantRef{InstallationId: input.Installation.ID, RepositoryId: input.Repository.ID}
	pr := input.PullRequest
	var installation persistence.Installation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&installation, "id=?", tenant.InstallationId).Error; err == gorm.ErrRecordNotFound {
		return nil
	} else if err != nil {
		return err
	}
	if !installation.Active {
		return nil
	}
	var repo persistence.Repository
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&repo, "installation_id=? AND id=?", tenant.InstallationId, tenant.RepositoryId).Error; err == gorm.ErrRecordNotFound {
		return nil
	} else if err != nil {
		return err
	}
	if !repo.Enabled || !repo.Public {
		return nil
	}
	var current []persistence.PullRequest
	if err := tx.Where("installation_id=? AND repository_id=? AND number=?", tenant.InstallationId, tenant.RepositoryId, pr.Number).Find(&current).Error; err != nil {
		return err
	}
	if len(current) > 0 && current[0].RemoteUpdatedAt.After(pr.UpdatedAt) {
		return nil
	}
	if err := tx.Exec("INSERT INTO pull_requests(installation_id,repository_id,number,base_sha,head_sha,remote_updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(installation_id,repository_id,number) DO UPDATE SET base_sha=EXCLUDED.base_sha,head_sha=EXCLUDED.head_sha,remote_updated_at=EXCLUDED.remote_updated_at WHERE pull_requests.remote_updated_at<EXCLUDED.remote_updated_at", tenant.InstallationId, tenant.RepositoryId, pr.Number, pr.Base.SHA, pr.Head.SHA, pr.UpdatedAt).Error; err != nil {
		return err
	}
	pipeline := s.PipelineVersion
	if s.Review != nil && s.Review.Mode != "disabled" {
		pipeline = "review-v1"
	}
	var previous []persistence.Run
	if err := tx.Where("installation_id=? AND repository_id=? AND pr_number=? AND head_sha=? AND pipeline_version=?", tenant.InstallationId, tenant.RepositoryId, pr.Number, pr.Head.SHA, pipeline).Order("generation DESC").Limit(1).Find(&previous).Error; err != nil {
		return err
	}
	generation := 0
	if len(previous) > 0 {
		generation = previous[0].Generation
		if previous[0].Status == "Cancelled" || previous[0].Status == "Stale" {
			generation++
		} else {
			return nil
		}
	}
	snapshot := contracts.RunSnapshot{ModelConfigVersion: "none", RepositoryConfigVersion: itoa(repo.ConfigVersion), MemorySetVersion: "empty", TrustedRulesSha: ""}
	snapshot.Identity.Tenant = tenant
	snapshot.Identity.PrNumber = pr.Number
	snapshot.Identity.BaseSha = pr.Base.SHA
	snapshot.Identity.HeadSha = pr.Head.SHA
	snapshot.Identity.Generation = generation
	snapshot.Identity.PipelineVersion = pipeline
	body, _ := json.Marshal(snapshot)
	if pipeline == "review-v1" {
		c := s.Review
		review := contracts.ReviewSnapshot{Identity: contracts.ReviewIdentity{Tenant: tenant, PrNumber: pr.Number, BaseSha: pr.Base.SHA, HeadSha: pr.Head.SHA, PipelineVersion: "review-v1", Generation: generation, RepositoryFullName: repo.FullName, PrTitle: contracts.RedactReview(pr.Title)}, ModelConfigVersion: c.Model.ModelConfigVersion, PriceVersion: c.Model.PriceVersion, RepositoryConfigVersion: repo.ConfigVersion, GenerationPromptVersion: "review-generation-v1", VerificationPromptVersion: "review-verification-v1", MemorySetVersion: "none", TrustedRulesSha: "none", Policy: c.Policy, Provenance: contracts.ReviewProvenance{ModelMode: contracts.ReviewProvenanceModelMode(c.Mode), GithubMode: "real", FixtureId: c.Model.FixtureID, ContentExpiresAt: time.Now().Add(30 * 24 * time.Hour)}}
		if c.Mode == "fake" {
			review.Provenance.GithubMode = "fake"
		}
		if len(review.Identity.PrTitle) > 240 {
			review.Identity.PrTitle = "Pull request"
		}
		body, _ = json.Marshal(review)
		var checked contracts.ReviewSnapshot
		if err := contracts.DecodeReview("ReviewSnapshot", body, &checked, 16384); err != nil {
			return err
		}
	}
	runID, taskID := jobs.UUID(), jobs.UUID()
	if err := tx.Exec("INSERT INTO review_runs(id,installation_id,repository_id,pr_number,base_sha,head_sha,pipeline_version,generation,snapshot) VALUES(?,?,?,?,?,?,?,?,?::jsonb)", runID, tenant.InstallationId, tenant.RepositoryId, pr.Number, pr.Base.SHA, pr.Head.SHA, pipeline, generation, string(body)).Error; err != nil {
		return err
	}
	if err := tx.Exec("INSERT INTO jobs(id,run_id,available_at) VALUES(?,?,clock_timestamp()+interval '2 seconds')", taskID, runID).Error; err != nil {
		return err
	}
	var job persistence.Job
	if err := tx.First(&job, "id=?", taskID).Error; err != nil {
		return err
	}
	run := persistence.Run{ID: runID, InstallationID: int64(tenant.InstallationId), RepositoryID: int64(tenant.RepositoryId), HeadSHA: pr.Head.SHA, PipelineVersion: pipeline}
	return jobs.Schedule(tx, job, run, job.AvailableAt)
}
func itoa(value int) string { return fmt.Sprint(value) }
