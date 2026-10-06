package config

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	AppURL                      string
	APIBindAddr                 string
	InternalBindAddr            string
	DatabaseURL                 string
	RabbitMQURL                 string
	InternalAPIURL              string
	ReviewWorkerServiceToken    string
	EvolutionWorkerServiceToken string
	GitHubAppID                 int64
	GitHubAppSlug               string
	GitHubClientID              string
	GitHubClientSecret          string
	GitHubPrivateKeyPath        string
	GitHubWebhookSecret         string
	SessionSecret               string
	PipelineVersion             string
	Missing                     []string
}

func Load() (Config, error) {
	c := Config{
		AppURL:                      value("APP_URL", "http://localhost:8080"),
		APIBindAddr:                 value("API_BIND_ADDR", "127.0.0.1:4000"),
		InternalBindAddr:            value("INTERNAL_BIND_ADDR", "127.0.0.1:4001"),
		DatabaseURL:                 os.Getenv("DATABASE_URL"),
		RabbitMQURL:                 os.Getenv("RABBITMQ_URL"),
		InternalAPIURL:              value("INTERNAL_API_URL", "http://127.0.0.1:4001"),
		ReviewWorkerServiceToken:    os.Getenv("REVIEW_WORKER_SERVICE_TOKEN"),
		EvolutionWorkerServiceToken: os.Getenv("EVOLUTION_WORKER_SERVICE_TOKEN"),
		GitHubAppSlug:               os.Getenv("GITHUB_APP_SLUG"),
		GitHubClientID:              os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret:          os.Getenv("GITHUB_CLIENT_SECRET"),
		GitHubPrivateKeyPath:        os.Getenv("GITHUB_PRIVATE_KEY_PATH"),
		GitHubWebhookSecret:         os.Getenv("GITHUB_WEBHOOK_SECRET"),
		SessionSecret:               os.Getenv("SESSION_SECRET"),
		PipelineVersion:             value("REVIEW_PIPELINE_VERSION", "foundation-v1"),
	}
	origin, err := url.Parse(c.AppURL)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" ||
		(origin.Path != "" && origin.Path != "/") ||
		(origin.Scheme != "https" && !(origin.Scheme == "http" && (origin.Hostname() == "localhost" || origin.Hostname() == "127.0.0.1" || origin.Hostname() == "::1"))) {
		return Config{}, errors.New("Invalid APP_URL")
	}
	c.AppURL = strings.TrimSuffix(c.AppURL, "/")
	for name, bind := range map[string]string{"API_BIND_ADDR": c.APIBindAddr, "INTERNAL_BIND_ADDR": c.InternalBindAddr} {
		if _, _, err := net.SplitHostPort(bind); err != nil {
			return Config{}, errors.New("Invalid " + name)
		}
	}
	if id := os.Getenv("GITHUB_APP_ID"); id != "" {
		c.GitHubAppID, err = strconv.ParseInt(id, 10, 64)
		if err != nil || c.GitHubAppID <= 0 || c.GitHubAppID > 9007199254740991 {
			return Config{}, errors.New("Invalid GITHUB_APP_ID")
		}
	}
	if c.SessionSecret != "" {
		key, err := base64.StdEncoding.DecodeString(c.SessionSecret)
		if err != nil || len(key) != 32 || base64.StdEncoding.EncodeToString(key) != c.SessionSecret {
			return Config{}, errors.New("Invalid SESSION_SECRET")
		}
	}
	if c.GitHubAppSlug != "" && !regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(c.GitHubAppSlug) {
		return Config{}, errors.New("Invalid GITHUB_APP_SLUG")
	}
	for _, name := range []string{"DATABASE_URL", "RABBITMQ_URL", "REVIEW_WORKER_SERVICE_TOKEN", "EVOLUTION_WORKER_SERVICE_TOKEN", "GITHUB_APP_ID", "GITHUB_APP_SLUG", "GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "GITHUB_PRIVATE_KEY_PATH", "GITHUB_WEBHOOK_SECRET", "SESSION_SECRET"} {
		if os.Getenv(name) == "" {
			c.Missing = append(c.Missing, name)
		}
	}
	return c, nil
}

func value(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
