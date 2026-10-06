package github

import (
	"context"
	"reviewpilot.local/api/internal/fault"
)

type User struct {
	ID    int64
	Login string
}

func (g *Gateway) Exchange(ctx context.Context, code, verifier string) (string, int, error) {
	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   *int   `json:"expires_in"`
	}
	err := g.requestURL(ctx, g.OAuthURL+"/login/oauth/access_token", "", "POST", map[string]string{
		"client_id": g.Config.GitHubClientID, "client_secret": g.Config.GitHubClientSecret, "code": code, "code_verifier": verifier, "redirect_uri": g.Config.AppURL + "/auth/github/callback",
	}, &payload)
	if err != nil {
		return "", 0, err
	}
	if payload.AccessToken == "" || (payload.ExpiresIn != nil && *payload.ExpiresIn <= 0) {
		return "", 0, fault.New(400, "OAUTH_EXCHANGE_FAILED")
	}
	lifetime := 28800
	if payload.ExpiresIn != nil && *payload.ExpiresIn < lifetime {
		lifetime = *payload.ExpiresIn
	}
	return payload.AccessToken, lifetime, nil
}
func (g *Gateway) User(ctx context.Context, token string) (User, error) {
	var user User
	err := g.Request(ctx, "/user", token, "GET", nil, &user)
	if err == nil && (user.ID <= 0 || user.ID > 9007199254740991 || user.Login == "") {
		err = fault.New(503, "GITHUB_INVALID_RESPONSE")
	}
	return user, err
}
