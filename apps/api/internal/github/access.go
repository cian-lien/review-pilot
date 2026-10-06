package github

import (
	"context"
	"fmt"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/fault"
)

type Installation struct {
	ID          int     `json:"id"`
	AppID       int64   `json:"app_id"`
	SuspendedAt *string `json:"suspended_at"`
}
type Repository struct {
	ID          int    `json:"id"`
	FullName    string `json:"full_name"`
	Private     *bool  `json:"private"`
	Permissions struct {
		Pull     bool
		Maintain bool
		Admin    bool
	} `json:"permissions"`
}

func (r Repository) Public() bool { return r.Private != nil && !*r.Private }
func (g *Gateway) UserInstallations(ctx context.Context, token string) ([]Installation, error) {
	result := []Installation{}
	for page := 1; page <= 50; page++ {
		var payload struct{ Installations []Installation }
		if err := g.Request(ctx, fmt.Sprintf("/user/installations?per_page=100&page=%d", page), token, "GET", nil, &payload); err != nil {
			return nil, err
		}
		result = append(result, payload.Installations...)
		if len(payload.Installations) < 100 {
			return result, nil
		}
	}
	return nil, fault.New(503, "GITHUB_PAGINATION_LIMIT")
}
func (g *Gateway) InstallationRepositories(ctx context.Context, token string, id int) ([]Repository, error) {
	result := []Repository{}
	for page := 1; page <= 50; page++ {
		var payload struct{ Repositories []Repository }
		if err := g.Request(ctx, fmt.Sprintf("/user/installations/%d/repositories?per_page=100&page=%d", id, page), token, "GET", nil, &payload); err != nil {
			return nil, err
		}
		for _, repo := range payload.Repositories {
			if repo.ID <= 0 || repo.ID > 9007199254740991 || repo.Private == nil {
				return nil, fault.New(503, "GITHUB_INVALID_RESPONSE")
			}
			if _, err := RepositoryPath(repo.FullName); err != nil {
				return nil, fault.New(503, "GITHUB_INVALID_RESPONSE")
			}
		}
		result = append(result, payload.Repositories...)
		if len(payload.Repositories) < 100 {
			return result, nil
		}
	}
	return nil, fault.New(503, "GITHUB_PAGINATION_LIMIT")
}
func (g *Gateway) UserRepository(ctx context.Context, token, name string) (Repository, error) {
	path, err := RepositoryPath(name)
	if err != nil {
		return Repository{}, err
	}
	var repo Repository
	err = g.Request(ctx, "/repos/"+path, token, "GET", nil, &repo)
	if err == nil && (repo.ID <= 0 || repo.Private == nil) {
		err = fault.New(503, "GITHUB_INVALID_RESPONSE")
	}
	return repo, err
}
func (g *Gateway) RequireInstallation(ctx context.Context, token string, id int) error {
	items, err := g.UserInstallations(ctx, token)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID == id && item.AppID == g.Config.GitHubAppID && item.SuspendedAt == nil {
			return nil
		}
	}
	return fault.New(403, "INSTALLATION_ACCESS_DENIED")
}
func (g *Gateway) Access(ctx context.Context, token string, tenant contracts.TenantRef, manage bool) (Repository, error) {
	if tenant.InstallationId <= 0 || tenant.RepositoryId <= 0 || tenant.InstallationId > 9007199254740991 || tenant.RepositoryId > 9007199254740991 {
		return Repository{}, fault.New(400, "INVALID_TENANT")
	}
	if err := g.RequireInstallation(ctx, token, tenant.InstallationId); err != nil {
		return Repository{}, err
	}
	repos, err := g.InstallationRepositories(ctx, token, tenant.InstallationId)
	if err != nil {
		return Repository{}, err
	}
	for _, repo := range repos {
		if repo.ID != tenant.RepositoryId || !repo.Public() {
			continue
		}
		current, err := g.UserRepository(ctx, token, repo.FullName)
		if err != nil {
			return Repository{}, err
		}
		if current.ID != tenant.RepositoryId || !current.Public() || !current.Permissions.Pull {
			break
		}
		if manage && !current.Permissions.Maintain && !current.Permissions.Admin {
			return Repository{}, fault.New(403, "MAINTAINER_REQUIRED")
		}
		return current, nil
	}
	return Repository{}, fault.New(403, "REPOSITORY_ACCESS_DENIED")
}
func (g *Gateway) RequireAccess(ctx context.Context, token string, tenant contracts.TenantRef, manage bool) error {
	_, err := g.Access(ctx, token, tenant, manage)
	return err
}
